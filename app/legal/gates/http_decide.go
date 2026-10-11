package gates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Problem codes the decision and withdraw routes write.
const (
	CodeAgentCannotDecide      = "agent_cannot_decide"
	CodeSelfApproval           = "self_approval"
	CodeStepUpRequired         = "step_up_required"
	CodeCoolingOff             = "cooling_off"
	CodeGateExpired            = "gate_expired"
	CodeGateNotOpen            = "gate_not_open"
	CodeInsufficientTrustFunds = "insufficient_trust_funds"
	CodePayloadChanged         = "payload_changed"
	CodeAlreadyDecided         = "already_decided"
	CodeNotPreparer            = "not_preparer"
)

// StepUpWindow is how fresh step-up auth must be to decide a high or irreversible gate.
const StepUpWindow = 15 * time.Minute

// Engine is the part of the gate engine the decision and withdraw routes use.
type Engine interface {
	Decide(ctx context.Context, tx pgx.Tx, id gate.GateID, version int, d gate.Decision, by gate.Principal) error
	Withdraw(ctx context.Context, tx pgx.Tx, id gate.GateID, by gate.Principal) error
	Get(ctx context.Context, id gate.GateID) (gate.Gate, error)
}

// DecideHTTP serves POST /gates/{gate_id}/decisions and POST /gates/{gate_id}/withdraw. The
// decision route is the one route that sets gate-owned state: the engine runs an approved
// gate's effect in the same call and transaction. Every dependency is required; a route
// answers 503 and calls nothing while one is missing.
type DecideHTTP struct {
	Gates    Engine
	Kinds    *Catalog
	Access   Access
	DB       TxRunner
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
	// StepUp returns when the request's session last passed step-up auth; zero means never.
	StepUp func(r *http.Request) time.Time
	// Now is the decision clock.
	Now func() time.Time
}

// Routes mounts the decision and withdraw routes with their write declarations.
func (h *DecideHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The decision route is the HTTP-00 exemption: it records the decision and, through the
	// engine, the effect. Withdraw only closes the preparer's own pending gate.
	r.Method(http.MethodPost, "/gates/{gate_id}/decisions", server.Declare(h.action(true, h.decide), "gate.decided"))
	r.Method(http.MethodPost, "/gates/{gate_id}/withdraw", server.Declare(h.action(false, h.withdraw), "gate.withdrawn"))
}

func (h *DecideHTTP) ready() bool {
	return h.Gates != nil && h.Kinds != nil && h.Access != nil && h.DB != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil && h.StepUp != nil && h.Now != nil
}

// read reuses the read routes' problem, capability and view helpers.
func (h *DecideHTTP) read() *HTTP {
	return &HTTP{Kinds: h.Kinds, Access: h.Access, Problems: h.Problems}
}

// decideStep runs on a loaded, visible gate whose version matches If-Match.
type decideStep func(w http.ResponseWriter, r *http.Request, p registry.Principal, g gate.Gate, k SpecKind, body []byte)

// action wraps a step with the checks both routes share, in order: 503, 401, (decide only:
// agent 403, no approver capability 403), 400 malformed id, 428 missing If-Match, 400
// missing Idempotency-Key, then idempotency replay, 404 (missing or walled), 412 stale.
func (h *DecideHTTP) action(decide bool, fn decideStep) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rd := h.read()
		if !h.ready() {
			rd.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
			return
		}
		p, ok := h.Principal(r)
		if !ok {
			rd.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
			return
		}
		if decide {
			// rule: ARC-I-017
			// Agent principals never decide.
			if p.Kind != registry.PrincipalUser {
				rd.fail(w, r, http.StatusForbidden, CodeAgentCannotDecide, "Only a person can decide a gate")
				return
			}
			// rule: ARC-I-051
			held, err := rd.heldKinds(r.Context(), p)
			if err != nil {
				rd.writeErr(w, r, err)
				return
			}
			if len(held) == 0 {
				rd.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
				return
			}
		}
		id := chi.URLParam(r, "gate_id")
		if !idPattern.MatchString(id) {
			rd.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "gate_id is not valid")
			return
		}
		// rule: ARC-I-006
		if len(r.Header.Values("If-Match")) == 0 {
			rd.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
			return
		}
		// rule: ARC-I-007
		// The key is required before any service or gate call.
		if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
			rd.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
			return
		}
		// rule: ARC-I-008
		idem.New(h.Idem, func(*http.Request) string { return p.UserID + p.RunID }, idem.DefaultTTL)(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					rd.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
					return
				}
				ctx := r.Context()
				g, err := h.Gates.Get(ctx, gate.GateID(id))
				if errors.Is(err, gate.ErrNotFound) {
					rd.fail(w, r, http.StatusNotFound, CodeNotFound, "No such gate")
					return
				}
				if err != nil {
					rd.writeErr(w, r, err)
					return
				}
				k, err := h.Kinds.Spec(g.Kind)
				if err != nil {
					rd.writeErr(w, r, err)
					return
				}
				// rule: ARC-I-018
				// The capability is re-evaluated at decision time; a walled subject is 404.
				subject := registry.Subject{Kind: g.Subject.Kind, ID: g.Subject.ID}
				// A decision needs the approver capability at the decide level.
				check := h.Access.Can
				if decide {
					check = func(ctx context.Context, p registry.Principal, c string, s registry.Subject) (bool, error) {
						return canDecide(ctx, h.Access, p, c, s)
					}
				}
				allowed, err := check(ctx, p, k.Approver, subject)
				switch {
				case errors.Is(err, identity.ErrNotVisible):
					rd.fail(w, r, http.StatusNotFound, CodeNotFound, "No such gate")
					return
				case err != nil:
					rd.writeErr(w, r, err)
					return
				case decide && !allowed:
					rd.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
					return
				}
				if err := server.CheckIfMatch(r, int64(g.Version)); err != nil {
					rd.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
					return
				}
				fn(w, r, p, g, k, body)
			}),
		).ServeHTTP(w, r)
	}
}

// decisionBody is the openapi GateDecisionInput.
type decisionBody struct {
	Decision                    string `json:"decision"`
	Version                     *int   `json:"version"`
	PayloadHash                 string `json:"payload_hash"`
	Reason                      string `json:"reason,omitempty"`
	SolePractitionerDeclaration string `json:"sole_practitioner_declaration,omitempty"`
}

var verdicts = map[string]gate.Verdict{
	"approve": gate.VerdictApprove,
	"return":  gate.VerdictChangesRequested,
	"reject":  gate.VerdictReject,
}

// decodeStrict decodes body strictly; an empty body leaves v unchanged.
func decodeStrict(body []byte, v any) error {
	if strings.TrimSpace(string(body)) == "" {
		return nil
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

func (b decisionBody) valid() bool {
	v, ok := verdicts[b.Decision]
	return ok && b.Version != nil && strings.HasPrefix(b.PayloadHash, "sha256:") && len(b.PayloadHash) == 71 &&
		len(b.Reason) <= 1000 && len(b.SolePractitionerDeclaration) <= 500 &&
		(v == gate.VerdictApprove || strings.TrimSpace(b.Reason) != "")
}

// decide serves POST /gates/{gate_id}/decisions. Approve runs the kind's effect inside the
// engine's Decide (gate.WithEffect), in this request's transaction.
func (h *DecideHTTP) decide(w http.ResponseWriter, r *http.Request, p registry.Principal, g gate.Gate, k SpecKind, raw []byte) {
	rd := h.read()
	var b decisionBody
	if err := decodeStrict(raw, &b); err != nil || !b.valid() {
		rd.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	verdict := verdicts[b.Decision]
	// rule: ARC-I-015
	if *b.Version != g.Version {
		rd.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "version does not match the current version")
		return
	}
	// rule: ARC-I-017
	// Every decision needs step-up auth; high and irreversible need it within the window.
	now := h.Now()
	mfa := h.StepUp(r)
	if mfa.IsZero() || mfa.After(now) ||
		((k.Risk == registry.RiskHigh || k.Risk == registry.RiskIrreversible) && now.Sub(mfa) > StepUpWindow) {
		h.stepUp(w, r)
		return
	}
	// rule: ARC-I-016
	// The preparer never decides, except an approval under the sole-practitioner exception,
	// which the engine's self-approval policy decides.
	if preparerID(g.Preparer) == p.UserID && (verdict != gate.VerdictApprove || strings.TrimSpace(b.SolePractitionerDeclaration) == "") {
		rd.fail(w, r, http.StatusForbidden, CodeSelfApproval, "The preparer cannot decide this gate")
		return
	}
	d := gate.Decision{Verdict: verdict, Reason: b.Reason, PayloadHash: b.PayloadHash, MFAVerifiedAt: mfa,
		SelfApprovalReason: b.SolePractitionerDeclaration}
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return h.Gates.Decide(ctx, tx, g.ID, g.Version, d, p)
	})
	if err != nil {
		h.writeEngineErr(w, r, err)
		return
	}
	h.writeGate(w, r, g.ID)
}

// withdraw serves POST /gates/{gate_id}/withdraw. Only the preparer withdraws.
func (h *DecideHTTP) withdraw(w http.ResponseWriter, r *http.Request, p registry.Principal, g gate.Gate, _ SpecKind, raw []byte) {
	rd := h.read()
	var b struct{}
	if err := decodeStrict(raw, &b); err != nil {
		rd.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	if preparerID(g.Preparer) != preparerID(p) {
		rd.fail(w, r, http.StatusForbidden, CodeNotPreparer, "Only the preparer can withdraw this gate")
		return
	}
	err := h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		return h.Gates.Withdraw(ctx, tx, g.ID, p)
	})
	if err != nil {
		h.writeEngineErr(w, r, err)
		return
	}
	h.writeGate(w, r, g.ID)
}

func (h *DecideHTTP) writeGate(w http.ResponseWriter, r *http.Request, id gate.GateID) {
	out, err := h.Gates.Get(r.Context(), id)
	if err != nil {
		h.read().writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.Version))
	writeJSON(w, http.StatusOK, h.read().view(out))
}

// StepUpChallenge is the RFC 9470 challenge a decision without fresh step-up auth answers.
const StepUpChallenge = `Bearer error="insufficient_user_authentication"`

// stepUp answers 401 step_up_required with the RFC 9470 challenge, matching kernel/gate's
// problem table for ErrMFARequired.
func (h *DecideHTTP) stepUp(w http.ResponseWriter, r *http.Request) {
	// rule: ARC-I-017
	w.Header().Set("WWW-Authenticate", StepUpChallenge)
	h.read().fail(w, r, http.StatusUnauthorized, CodeStepUpRequired, "Step-up authentication is required")
}

// engineProblems maps engine and effect sentinels to problems, first match wins. Effect
// errors come before ErrEffectFailed so their own code shows.
var engineProblems = []struct {
	err    error
	status int
	code   string
}{
	{trust.ErrInsufficientFunds, http.StatusConflict, CodeInsufficientTrustFunds},
	{gate.ErrAgentApprover, http.StatusForbidden, CodeAgentCannotDecide},
	{ErrAgentApproves, http.StatusForbidden, CodeAgentCannotDecide},
	{gate.ErrSelfApproval, http.StatusForbidden, CodeSelfApproval},
	{ErrPreparerApproves, http.StatusForbidden, CodeSelfApproval},
	{gate.ErrNotAuthorized, http.StatusForbidden, CodeMissingCapability},
	{ErrMissingCapability, http.StatusForbidden, CodeMissingCapability},
	{gate.ErrNotPreparer, http.StatusForbidden, CodeNotPreparer},
	{gate.ErrStaleVersion, http.StatusPreconditionFailed, server.CodeVersionConflict},
	{gate.ErrPayloadChanged, http.StatusConflict, CodePayloadChanged},
	{gate.ErrAlreadyDecided, http.StatusConflict, CodeAlreadyDecided},
	{ErrCoolingOff, http.StatusConflict, CodeCoolingOff},
	{gate.ErrExpired, http.StatusConflict, CodeGateExpired},
	{gate.ErrNotPending, http.StatusConflict, CodeGateNotOpen},
	{gate.ErrReasonRequired, http.StatusBadRequest, server.CodeValidationFailed},
	{gate.ErrNotFound, http.StatusNotFound, CodeNotFound},
}

// writeEngineErr maps a Decide or Withdraw error; the error text never reaches the body.
func (h *DecideHTTP) writeEngineErr(w http.ResponseWriter, r *http.Request, err error) {
	rd := h.read()
	if errors.Is(err, gate.ErrMFARequired) {
		h.stepUp(w, r)
		return
	}
	for _, pr := range engineProblems {
		if errors.Is(err, pr.err) {
			rd.fail(w, r, pr.status, pr.code, http.StatusText(pr.status))
			return
		}
	}
	rd.writeErr(w, r, err)
}
