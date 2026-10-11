// Package gatefake is an in-memory gate.Engine for tests. It needs no network, Postgres
// or Docker: the tx argument is passed through to effects untouched. Payload schemas are
// not validated; the payload only has to be a JSON object.
package gatefake

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/jackc/pgx/v5"
)

var _ gate.Engine = (*Fake)(nil)

var slugRe = regexp.MustCompile(`^[a-z]+\.[a-z_]+$`)

// Caps is an in-memory gate.Authorizer: user id -> capability ids held.
type Caps map[string][]string

// Can reports whether p's user holds capability.
func (c Caps) Can(_ context.Context, p gate.Principal, capability string) (bool, error) {
	return slices.Contains(c[p.UserID], capability), nil
}

// Fake is an in-memory gate engine.
type Fake struct {
	mu     sync.Mutex
	now    func() time.Time
	auth   gate.Authorizer
	policy gate.SelfApprovalPolicy
	kinds  map[string]gate.GateKind
	gates  map[gate.GateID]gate.Gate
	seq    int
}

// New returns an empty engine that reads time from now and checks approvers with auth.
func New(now func() time.Time, auth gate.Authorizer) *Fake {
	return &Fake{now: now, auth: auth, kinds: map[string]gate.GateKind{}, gates: map[gate.GateID]gate.Gate{}}
}

// Register adds a gate kind.
func (f *Fake) Register(k gate.GateKind) error {
	switch {
	case !slugRe.MatchString(k.Slug), k.Approver == "", k.Effect == nil,
		k.MinApprovers < 1, k.MinApprovers > 3,
		k.Risk == gate.RiskIrreversible && k.MinApprovers < 2,
		k.TTL < time.Hour, k.TTL > 720*time.Hour, k.CoolingOff < 0:
		return fmt.Errorf("register %q: %w", k.Slug, gate.ErrInvalidKind)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.kinds[k.Slug]; ok {
		return fmt.Errorf("register %q: %w", k.Slug, gate.ErrDuplicateKind)
	}
	f.kinds[k.Slug] = k
	return nil
}

// RegisterCore adds a kernel-owned core.* gate kind (gate.CoreRegistrar).
func (f *Fake) RegisterCore(k gate.GateKind) error {
	if !strings.HasPrefix(k.Slug, gate.CorePrefix) {
		return fmt.Errorf("register core %q: %w", k.Slug, gate.ErrInvalidKind)
	}
	return f.Register(k)
}

var _ gate.CoreRegistrar = (*Fake)(nil)

// SetSelfApprovalPolicy installs the self-approval policy.
func (f *Fake) SetSelfApprovalPolicy(p gate.SelfApprovalPolicy) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policy = p
}

// PayloadHash is the real engine's RFC 8785 hash (gate.PayloadHash) of an object payload,
// so fake and engine agree on every payload's hash.
func PayloadHash(payload json.RawMessage) (string, error) {
	var v map[string]any
	if err := json.Unmarshal(payload, &v); err != nil {
		return "", fmt.Errorf("payload: %w", gate.ErrInvalidPayload)
	}
	return gate.PayloadHash(payload)
}

// preparerUser is the user who counts as preparer: an agent's acting-for user.
func preparerUser(p gate.Principal) string {
	if p.Kind == gate.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

func (f *Fake) create(kind gate.GateKind, version int, supersedes gate.GateID, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.Gate, error) {
	hash, err := PayloadHash(payload)
	if err != nil {
		return gate.Gate{}, err
	}
	f.seq++
	now := f.now()
	g := gate.Gate{
		ID:          gate.GateID(fmt.Sprintf("gate-%d", f.seq)),
		Kind:        kind.Slug,
		Version:     version,
		Supersedes:  supersedes,
		Subject:     subject,
		Payload:     slices.Clone(payload),
		PayloadHash: hash,
		Preparer:    preparer,
		Status:      gate.StatusPending,
		CreatedAt:   now,
		ExpiresAt:   now.Add(kind.TTL),
	}
	f.gates[g.ID] = g
	return g, nil
}

// Open creates version 1 of a gate.
func (f *Fake) Open(_ context.Context, _ pgx.Tx, kind string, subject gate.Subject, payload json.RawMessage, preparer gate.Principal) (gate.GateID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.kinds[kind]
	if !ok {
		return "", fmt.Errorf("open %q: %w", kind, gate.ErrUnknownKind)
	}
	g, err := f.create(k, 1, "", subject, payload, preparer)
	if err != nil {
		return "", fmt.Errorf("open %q: %w", kind, err)
	}
	return g.ID, nil
}

// pending loads a gate that must still be pending, expiring it when its TTL has passed.
func (f *Fake) pending(id gate.GateID) (gate.Gate, error) {
	g, ok := f.gates[id]
	if !ok {
		return gate.Gate{}, gate.ErrNotFound
	}
	if g.Status == gate.StatusPending && !f.now().Before(g.ExpiresAt) {
		g.Status = gate.StatusExpired
		f.gates[id] = g
		return gate.Gate{}, gate.ErrExpired
	}
	if g.Status != gate.StatusPending {
		return gate.Gate{}, gate.ErrNotPending
	}
	return g, nil
}

// Revise supersedes a pending gate with a new payload version.
func (f *Fake) Revise(_ context.Context, _ pgx.Tx, id gate.GateID, payload json.RawMessage, by gate.Principal) (gate.GateID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, err := f.pending(id)
	if err != nil {
		return "", fmt.Errorf("revise %s: %w", id, err)
	}
	if preparerUser(by) != preparerUser(g.Preparer) {
		return "", fmt.Errorf("revise %s: %w", id, gate.ErrNotPreparer)
	}
	next, err := f.create(f.kinds[g.Kind], g.Version+1, g.ID, g.Subject, payload, by)
	if err != nil {
		return "", fmt.Errorf("revise %s: %w", id, err)
	}
	g.Status = gate.StatusSuperseded
	f.gates[id] = g
	return next.ID, nil
}

// Withdraw closes a pending gate on the preparer's request.
func (f *Fake) Withdraw(_ context.Context, _ pgx.Tx, id gate.GateID, by gate.Principal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, err := f.pending(id)
	if err != nil {
		return fmt.Errorf("withdraw %s: %w", id, err)
	}
	if preparerUser(by) != preparerUser(g.Preparer) {
		return fmt.Errorf("withdraw %s: %w", id, gate.ErrNotPreparer)
	}
	g.Status = gate.StatusWithdrawn
	f.gates[id] = g
	return nil
}

// Decide records one decision; the effect runs when the gate is fully approved.
func (f *Fake) Decide(ctx context.Context, tx pgx.Tx, id gate.GateID, version int, d gate.Decision, by gate.Principal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, err := f.pending(id)
	if err != nil {
		return fmt.Errorf("decide %s: %w", id, err)
	}
	k := f.kinds[g.Kind]
	switch {
	case version != g.Version:
		return fmt.Errorf("decide %s: %w", id, gate.ErrStaleVersion)
	case by.Kind != gate.PrincipalUser:
		return fmt.Errorf("decide %s: %w", id, gate.ErrAgentApprover)
	case d.MFAVerifiedAt.IsZero() || d.MFAVerifiedAt.After(f.now()):
		return fmt.Errorf("decide %s: %w", id, gate.ErrMFARequired)
	case d.PayloadHash != g.PayloadHash:
		return fmt.Errorf("decide %s: %w", id, gate.ErrPayloadChanged)
	case d.Verdict != gate.VerdictApprove && d.Reason == "":
		return fmt.Errorf("decide %s: %w", id, gate.ErrReasonRequired)
	}
	for _, a := range g.Approvers {
		if a.UserID == by.UserID {
			return fmt.Errorf("decide %s: %w", id, gate.ErrAlreadyDecided)
		}
	}
	ok, err := f.auth.Can(ctx, by, k.Approver)
	if err != nil {
		return fmt.Errorf("decide %s: authorize: %w", id, err)
	}
	if !ok {
		return fmt.Errorf("decide %s: %w", id, gate.ErrNotAuthorized)
	}
	switch d.Verdict {
	case gate.VerdictReject:
		if k.OnReject != nil {
			if err := k.OnReject(ctx, tx, gate.Rejected{GateID: string(g.ID), Kind: g.Kind, Version: g.Version,
				Subject: g.Subject, Payload: g.Payload, Preparer: g.Preparer, RejectedBy: by, Reason: d.Reason}); err != nil {
				return fmt.Errorf("decide %s: on reject: %w", id, err)
			}
		}
		g.Status = gate.StatusRejected
		f.gates[id] = g
		return nil
	case gate.VerdictChangesRequested:
		g.Status = gate.StatusChangesRequested
		f.gates[id] = g
		return nil
	case gate.VerdictApprove:
	default:
		return fmt.Errorf("decide %s: verdict %q: %w", id, d.Verdict, gate.ErrInvalidPayload)
	}
	if by.UserID == preparerUser(g.Preparer) {
		allowed := false
		if f.policy != nil && d.SelfApprovalReason != "" {
			allowed, _ = f.policy(ctx, g, by)
		}
		if !allowed {
			return fmt.Errorf("decide %s: %w", id, gate.ErrSelfApproval)
		}
		g.SelfApproved = true
	}
	g.Approvers = append(slices.Clone(g.Approvers), by)
	if len(g.Approvers) < k.MinApprovers {
		f.gates[id] = g
		return nil
	}
	now := f.now()
	if g.SelfApproved && k.CoolingOff > 0 {
		g.Status = gate.StatusApprovedCooling
		g.EffectiveAt = now.Add(k.CoolingOff)
		f.gates[id] = g
		return nil
	}
	g.EffectiveAt = now
	return f.runEffect(ctx, tx, k, g)
}

// runEffect runs k's effect for g and stores g as approved. On failure nothing is stored.
func (f *Fake) runEffect(ctx context.Context, tx pgx.Tx, k gate.GateKind, g gate.Gate) error {
	if err := k.Effect(ctx, tx, gate.Approved{
		GateID: string(g.ID), Kind: g.Kind, Version: g.Version, Subject: g.Subject,
		Payload: g.Payload, Preparer: g.Preparer, Approvers: slices.Clone(g.Approvers),
	}); err != nil {
		return fmt.Errorf("effect %s: %w: %w", g.ID, gate.ErrEffectFailed, err)
	}
	g.Status = gate.StatusApproved
	g.EffectRan = true
	f.gates[g.ID] = g
	return nil
}

// RunDue runs effects whose cooling-off has ended and expires pending gates past TTL.
func (f *Fake) RunDue(ctx context.Context, tx pgx.Tx) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	ids := make([]gate.GateID, 0, len(f.gates))
	for id := range f.gates {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	ran := 0
	for _, id := range ids {
		g := f.gates[id]
		switch {
		case g.Status == gate.StatusPending && !now.Before(g.ExpiresAt):
			g.Status = gate.StatusExpired
			f.gates[id] = g
		case g.Status == gate.StatusApprovedCooling && !now.Before(g.EffectiveAt):
			if err := f.runEffect(ctx, tx, f.kinds[g.Kind], g); err != nil {
				return ran, fmt.Errorf("run due: %w", err)
			}
			ran++
		}
	}
	return ran, nil
}

// Get returns one gate.
func (f *Fake) Get(_ context.Context, id gate.GateID) (gate.Gate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	g, ok := f.gates[id]
	if !ok {
		return gate.Gate{}, fmt.Errorf("get %s: %w", id, gate.ErrNotFound)
	}
	g.Approvers = slices.Clone(g.Approvers)
	return g, nil
}
