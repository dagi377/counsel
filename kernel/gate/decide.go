package gate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/jackc/pgx/v5"
)

// mfaWindow is how fresh step-up auth must be for a high or irreversible decision.
const mfaWindow = 15 * time.Minute

// amountField is the payload field an authority limit is compared with (integer cents).
const amountField = "amount_cents"

// decisionRecommend is the gate_decision value of an approval above the decider's authority
// limit: recorded, never counted as an approval, and the gate stays pending
// (migrations/0300_gate_payload_recommend.sql). A caller never sends it as a Verdict.
const decisionRecommend = "recommend"

// recommendReason is stored when a recommendation carries no reason of its own; the table
// requires a reason on every decision other than approve.
const recommendReason = "approval above the decider's authority limit; recorded as a recommendation"

// DecideHooks are the inputs to Decide that only the app can supply.
type DecideHooks struct {
	// Preparers returns more users who count as preparers of g: every editor of the
	// underlying item and its requester. The gate's own requesters are always included.
	Preparers func(ctx context.Context, tx pgx.Tx, g Gate) ([]string, error)
	// Payload returns the payload of g. Its hash must equal the gate's stored hash. Decide
	// uses it only for a gate version opened before payloads were stored
	// (gate.approval_request.payload_jcs is NULL); otherwise it reads the stored bytes.
	Payload func(ctx context.Context, tx pgx.Tx, g Gate) (json.RawMessage, error)
	// LimitKinds maps a gate kind slug to the core.authority_limit kind whose value_int
	// (cents) caps the payload amount a decider may approve. Kinds not listed have no cap.
	LimitKinds map[string]string
}

// SetDecideHooks installs the app's Decide inputs on e.
func (e *PG) SetDecideHooks(h DecideHooks) {
	h.LimitKinds = clonedMap(h.LimitKinds)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.decide = h
}

func (e *PG) hooks() DecideHooks {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.decide
}

func clonedMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func init() {
	problems = append(problems, []struct {
		err    error
		status int
		code   string
		rule   string
	}{
		{ErrAgentApprover, http.StatusForbidden, "agent_cannot_decide", "ARC-I-017"},
		{ErrSelfApproval, http.StatusForbidden, "sod_violation", "ARC-I-016"},
		{ErrMFARequired, http.StatusUnauthorized, "step_up_required", "ARC-I-017"},
		{ErrNotAuthorized, http.StatusForbidden, "not_authorized", "ARC-I-018"},
		{ErrReasonRequired, http.StatusUnprocessableEntity, "reason_required", ""},
		{ErrStaleVersion, http.StatusConflict, "stale_version", "ARC-I-015"},
		{ErrNotPending, http.StatusConflict, "not_pending", ""},
		{ErrExpired, http.StatusConflict, "gate_expired", "ARC-I-022"},
		{ErrEffectFailed, http.StatusConflict, "effect_failed", "ARC-I-024"},
	}...)
}

// decideIn is everything one decision is checked against, already loaded.
type decideIn struct {
	gate      Gate
	kind      GateKind
	preparers []string // every user in the gate's preparer set
	version   int      // the version the decider saw
	d         Decision
	by        Principal
	now       time.Time
	amount    *int64 // payload amount in cents, nil when the payload has none
	limit     *int64 // decider's authority limit in cents, nil when uncapped
	policy    SelfApprovalPolicy
}

// verdictOut is the checked result of one decision.
type verdictOut struct {
	selfApproved bool
	recommend    bool // approval above the authority limit; not stored as an approval
}

// checkDecision applies the decision rules without touching the database.
func checkDecision(ctx context.Context, in decideIn) (verdictOut, error) {
	var out verdictOut
	g := in.gate
	// rule: ARC-I-017
	// Only a human session decides; an agent run never approves, whoever it acts for.
	switch in.by.Kind {
	case PrincipalUser:
	case PrincipalAgent:
		return out, fmt.Errorf("decide %s: %w", g.ID, ErrAgentApprover)
	default:
		return out, fmt.Errorf("decide %s: principal kind %q: %w", g.ID, in.by.Kind, ErrNotAuthorized)
	}
	if in.by.UserID == "" {
		return out, fmt.Errorf("decide %s: no deciding user: %w", g.ID, ErrNotAuthorized)
	}
	if g.Status != StatusPending {
		return out, fmt.Errorf("decide %s: %w", g.ID, ErrNotPending)
	}
	if in.version != g.Version {
		return out, fmt.Errorf("decide %s: version %d, current %d: %w", g.ID, in.version, g.Version, ErrStaleVersion)
	}
	if !g.ExpiresAt.IsZero() && !in.now.Before(g.ExpiresAt) {
		return out, fmt.Errorf("decide %s: %w", g.ID, ErrExpired)
	}
	if in.d.PayloadHash != g.PayloadHash {
		return out, fmt.Errorf("decide %s: %w", g.ID, ErrPayloadChanged)
	}
	switch in.d.Verdict {
	case VerdictApprove:
	case VerdictReject, VerdictChangesRequested:
		if in.d.Reason == "" {
			return out, fmt.Errorf("decide %s: %w", g.ID, ErrReasonRequired)
		}
	default:
		return out, fmt.Errorf("decide %s: verdict %q: %w", g.ID, in.d.Verdict, ErrInvalidPayload)
	}
	// rule: ARC-I-017
	// Every decision carries step-up auth; high and irreversible need it within 15 minutes.
	mfa := in.d.MFAVerifiedAt
	if mfa.IsZero() || mfa.After(in.now) {
		return out, fmt.Errorf("decide %s: %w", g.ID, ErrMFARequired)
	}
	if (in.kind.Risk == RiskHigh || in.kind.Risk == RiskIrreversible) && in.now.Sub(mfa) > mfaWindow {
		return out, fmt.Errorf("decide %s: step-up older than %s: %w", g.ID, mfaWindow, ErrMFARequired)
	}
	// rule: ARC-I-016
	// The preparer set (creator, every editor, the user an agent run acted for, the item's
	// requester) never decides, unless the app's self-approval policy allows an approval.
	if slices.Contains(in.preparers, in.by.UserID) {
		if in.d.Verdict != VerdictApprove || in.policy == nil {
			return out, fmt.Errorf("decide %s: %w", g.ID, ErrSelfApproval)
		}
		ok, _ := in.policy(ctx, g, in.by)
		if !ok || in.d.SelfApprovalReason == "" {
			return out, fmt.Errorf("decide %s: %w", g.ID, ErrSelfApproval)
		}
		out.selfApproved = true
	}
	// rule: ARC-I-020
	// An approval above the decider's limit only recommends; cents compare as integers.
	if in.d.Verdict == VerdictApprove && in.amount != nil && in.limit != nil && *in.amount > *in.limit {
		out.recommend = true
	}
	return out, nil
}

// payloadAmount returns the integer cents in the payload's amount field, or nil.
func payloadAmount(payload json.RawMessage) (*int64, error) {
	var doc map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("payload: %w", ErrInvalidPayload)
	}
	raw, ok := doc[amountField]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("payload %s: %w", amountField, ErrInvalidPayload)
	}
	n, ok := v.(json.Number)
	if !ok {
		return nil, fmt.Errorf("payload %s: not a number: %w", amountField, ErrInvalidPayload)
	}
	c, err := n.Int64()
	if err != nil {
		return nil, fmt.Errorf("payload %s: %w", amountField, ErrInvalidPayload)
	}
	return &c, nil
}

// storedPayload returns the payload stored with a gate version, checked against the
// version's hash. ok is false when the row predates stored payloads (jcs is NULL).
func storedPayload(jcs, sha, hash []byte) (p json.RawMessage, ok bool, err error) {
	if jcs == nil {
		return nil, false, nil
	}
	sum := sha256.Sum256(jcs)
	if !bytes.Equal(sum[:], sha) || !bytes.Equal(sha, hash) {
		return nil, true, ErrPayloadChanged
	}
	return json.RawMessage(jcs), true, nil
}

// decisionRow is the gate_decision value and reason one checked decision is stored with.
func decisionRow(d Decision, out verdictOut) (decision string, reason any) {
	switch {
	case out.recommend && d.Reason != "":
		return decisionRecommend, d.Reason
	case out.recommend:
		return decisionRecommend, recommendReason
	case d.Reason != "":
		return string(d.Verdict), d.Reason
	case out.selfApproved:
		return string(d.Verdict), d.SelfApprovalReason
	}
	return string(d.Verdict), nil
}

// Decide records one decision on gate id at version, in tx. The payload is the one stored
// with the gate version (the app hook serves only versions opened before payloads were
// stored). An approval above the decider's authority limit is recorded as a
// recommendation: it returns nil, never counts as an approval and the gate stays pending.
// When the gate reaches its approvals and the kind has no cooling-off, the effect runs in tx.
func (e *PG) Decide(ctx context.Context, tx pgx.Tx, id GateID, version int, d Decision, by Principal) error {
	clk, err := clock.From(ctx)
	if err != nil {
		return fmt.Errorf("decide %s: %w", id, err)
	}
	g := Gate{ID: id}
	var (
		hash, jcs, sha []byte
		status         string
	)
	err = tx.QueryRow(ctx, `
SELECT gate_code, version, status, payload_hash, payload_type, payload_id::text, requested_by::text, expires_at,
       payload_jcs, payload_sha256
  FROM gate.approval_request WHERE id = $1 FOR UPDATE`, string(id)).
		Scan(&g.Kind, &g.Version, &status, &hash, &g.Subject.Kind, &g.Subject.ID, &g.Preparer.UserID, &g.ExpiresAt,
			&jcs, &sha)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("decide %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("decide %s: select: %w", id, err)
	}
	g.Status, g.Preparer.Kind = Status(status), PrincipalUser
	g.PayloadHash = "sha256:" + hex.EncodeToString(hash)
	k, _, _, err := e.kinds.Lookup(g.Kind)
	if err != nil {
		return fmt.Errorf("decide %s: %w", id, err)
	}
	h := e.hooks()
	preparers, err := e.preparerSet(ctx, tx, g, h)
	if err != nil {
		return err
	}
	stored, ok, err := storedPayload(jcs, sha, hash)
	switch {
	case err != nil:
		return fmt.Errorf("decide %s: stored payload: %w", id, err)
	case ok:
		g.Payload = stored
	case h.Payload != nil:
		p, err := h.Payload(ctx, tx, g)
		if err != nil {
			return fmt.Errorf("decide %s: payload: %w", id, err)
		}
		if sum, err := payloadSum(p); err != nil || !bytes.Equal(sum, hash) {
			return fmt.Errorf("decide %s: %w", id, ErrPayloadChanged)
		}
		g.Payload = p
	}
	in := decideIn{gate: g, kind: k, preparers: preparers, version: version, d: d, by: by, now: clk.Now()}
	e.mu.RLock()
	in.policy = e.policy
	e.mu.RUnlock()
	if lk, ok := h.LimitKinds[g.Kind]; ok && d.Verdict == VerdictApprove {
		// rule: ARC-I-020
		if g.Payload == nil {
			return fmt.Errorf("decide %s: kind has an authority limit but no payload hook: %w", id, ErrInvalidPayload)
		}
		if in.amount, err = payloadAmount(g.Payload); err != nil {
			return fmt.Errorf("decide %s: %w", id, err)
		}
		if in.limit, err = authorityLimit(ctx, tx, by.UserID, lk, clk.Today(ctx)); err != nil {
			return fmt.Errorf("decide %s: %w", id, err)
		}
	}
	out, err := checkDecision(ctx, in)
	if err != nil {
		return err
	}
	decision, reason := decisionRow(d, out)
	effectiveAt := clk.Now()
	if out.selfApproved {
		effectiveAt = effectiveAt.Add(k.CoolingOff)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO gate.gate_decision
  (approval_id, decided_by, decision, reason, payload_hash, self_approval_exception, effective_at, mfa_verified_at)
VALUES ($1, $2, $3, $4, $5, $6, greatest($7::timestamptz, now()), $8)`,
		string(id), by.UserID, decision, reason, hash, out.selfApproved, effectiveAt, d.MFAVerifiedAt); err != nil {
		// rule: ARC-I-023
		// A second decision by the same human on this gate version is a 409, not a 500.
		return AlreadyDecided(id, fmt.Errorf("decide %s: insert: %w", id, err))
	}
	// rule: ARC-I-020
	// A recommendation is recorded and the gate stays pending for a decider with a higher limit.
	if out.recommend {
		return nil
	}
	if err := tx.QueryRow(ctx, `SELECT status FROM gate.approval_request WHERE id = $1`, string(id)).Scan(&status); err != nil {
		return fmt.Errorf("decide %s: reread: %w", id, err)
	}
	if Status(status) != StatusApproved || k.CoolingOff > 0 {
		return nil // more approvals needed, or RunDue runs the effect after cooling-off
	}
	return e.runDecidedEffect(ctx, tx, g, k)
}

// preparerSet is every user who counts as preparer of g: the requester of every version
// in its supersede chain (an agent's acting-for user is stored as requester) plus the
// app's editors of the underlying item.
func (e *PG) preparerSet(ctx context.Context, tx pgx.Tx, g Gate, h DecideHooks) ([]string, error) {
	// rule: ARC-I-016
	rows, err := tx.Query(ctx, `
WITH RECURSIVE chain AS (
  SELECT id, supersedes_id, requested_by FROM gate.approval_request WHERE id = $1
  UNION ALL
  SELECT p.id, p.supersedes_id, p.requested_by
    FROM gate.approval_request p JOIN chain c ON p.id = c.supersedes_id)
SELECT DISTINCT requested_by::text FROM chain`, string(g.ID))
	if err != nil {
		return nil, fmt.Errorf("decide %s: preparers: %w", g.ID, err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("decide %s: preparers: %w", g.ID, err)
	}
	if h.Preparers != nil {
		more, err := h.Preparers(ctx, tx, g)
		if err != nil {
			return nil, fmt.Errorf("decide %s: app preparers: %w", g.ID, err)
		}
		out = append(out, more...)
	}
	return out, nil
}

// authorityLimit returns the user's cap in cents for limit kind lk on day, or nil when
// the user has no cap.
func authorityLimit(ctx context.Context, tx pgx.Tx, userID, lk string, day clock.Date) (*int64, error) {
	var v *int64
	err := tx.QueryRow(ctx, `
SELECT value_int FROM core.authority_limit
 WHERE user_id = $1 AND kind = $2 AND valid_from <= $3::date AND (valid_to IS NULL OR valid_to >= $3::date)
 ORDER BY valid_from DESC, created_at DESC LIMIT 1`, userID, lk, day.String()).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("authority limit: %w", err)
	}
	return v, nil
}

// runDecidedEffect runs the effect of an approved gate once, under a savepoint so a
// failing handler applies nothing.
func (e *PG) runDecidedEffect(ctx context.Context, tx pgx.Tx, g Gate, k GateKind) error {
	// rule: ARC-I-024
	tag, err := tx.Exec(ctx, `
INSERT INTO gate.effect_execution (approval_id, handler) VALUES ($1, $2)
ON CONFLICT (approval_id) DO NOTHING`, string(g.ID), k.Slug)
	if err != nil {
		return fmt.Errorf("decide %s: effect claim: %w", g.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return nil // already ran
	}
	approvers, err := approversOf(ctx, tx, g.ID)
	if err != nil {
		return err
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return fmt.Errorf("decide %s: savepoint: %w", g.ID, err)
	}
	effErr := k.Effect(ctx, sp, Approved{
		GateID: string(g.ID), Kind: g.Kind, Version: g.Version, Subject: g.Subject,
		Payload: g.Payload, Preparer: g.Preparer, Approvers: approvers,
	})
	outcome, code := "ok", ""
	if effErr != nil {
		// rule: ARC-I-024
		// The handler re-checked its preconditions and refused: undo all of it.
		_ = sp.Rollback(ctx)
		outcome, code = "failed", "effect_failed"
	} else if err := sp.Commit(ctx); err != nil {
		return fmt.Errorf("decide %s: release savepoint: %w", g.ID, err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE gate.effect_execution SET finished_at = now(), outcome = $2, error_code = $3 WHERE approval_id = $1`,
		string(g.ID), outcome, nonEmptyCode(code)); err != nil {
		return fmt.Errorf("decide %s: effect outcome: %w", g.ID, err)
	}
	if effErr != nil {
		return fmt.Errorf("decide %s: %w: %w", g.ID, ErrEffectFailed, effErr)
	}
	return nil
}

func nonEmptyCode(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func approversOf(ctx context.Context, tx pgx.Tx, id GateID) ([]Principal, error) {
	rows, err := tx.Query(ctx, `
SELECT decided_by::text FROM gate.gate_decision
 WHERE approval_id = $1 AND decision = 'approve' ORDER BY decided_at, id`, string(id))
	if err != nil {
		return nil, fmt.Errorf("decide %s: approvers: %w", id, err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("decide %s: approvers: %w", id, err)
	}
	out := make([]Principal, len(ids))
	for i, u := range ids {
		out[i] = Principal{Kind: PrincipalUser, UserID: u}
	}
	return out, nil
}
