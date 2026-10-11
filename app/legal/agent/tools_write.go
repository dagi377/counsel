package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// Write tool names.
const (
	ToolPrepareTrustWithdrawal = "prepare_trust_withdrawal"
)

// CapTrustRequest is the capability prepare_trust_withdrawal needs.
const CapTrustRequest = "trust.request"

// GateTrustWithdrawal is the Counsel gate kind a trust withdrawal proposal opens.
const GateTrustWithdrawal = "trust.withdrawal"

// ErrPayeeNotContact: a trust withdrawal's payee is not the id of one of the matter's
// contacts (422).
var ErrPayeeNotContact = fmt.Errorf("agent: payee is not a matter contact: %w", ErrInvalidInput)

// errNotProposed means the trust module left a request in a state other than requested.
var errNotProposed = errors.New("agent: withdrawal not left proposed")

// WriteDeps are what the write tools propose through. Each is the owning module's API.
type WriteDeps struct {
	// Can reports whether the acting-for user holds a capability.
	Can func(ctx context.Context, userID, capability string) (bool, error)
	// Walls hides walled matters from the acting-for user.
	Walls roles.Walls
	// TrustAccount returns the trust account that holds a matter's ledger.
	TrustAccount func(ctx context.Context, matterID string) (string, error)
	// Withdrawals records a withdrawal request and opens its Counsel gate.
	Withdrawals Withdrawals
	// Provenance holds the figures the run's read tools returned: amount_cents must be one of
	// them (a get_trust_balance or get_bill figure from the same run). Required: without it
	// prepare_trust_withdrawal refuses every call.
	Provenance *agenttools.Provenance
	// Contacts lists a matter's contacts: payee must be one of their ids. Required: without
	// it prepare_trust_withdrawal refuses every call.
	Contacts MatterContacts
}

// Withdrawals is the part of the trust withdrawal module the agent may reach: it requests and
// reads the available balance. Authorize and RecordExecution are not in it, so no agent code
// can call them (withdraw.Service satisfies it).
type Withdrawals interface {
	Request(ctx context.Context, r withdraw.Request) (withdraw.Withdrawal, error)
	Available(ctx context.Context, trustAccountID, matterID string) (int64, error)
}

// WriteTools runs the Counsel write tools. A write tool only records a proposal and opens a
// Counsel gate; it never authorizes or executes the effect. The run then parks and resumes
// only on Counsel's gate.decided event for that gate.
type WriteTools struct {
	deps    WriteDeps
	specs   []ToolSpec
	schemas map[string]*jsonschema.Schema

	mu      sync.Mutex
	waiting map[string]parked // by Counsel gate id
}

type parked struct {
	runID    string
	preparer string
}

// writeSpecs name the write tools; capability and input schema come from the a2 definitions.
var writeSpecs = []ToolSpec{
	{Name: ToolPrepareTrustWithdrawal, Kind: Write, GateKind: GateTrustWithdrawal},
}

// NewWriteTools returns the write tools over deps.
func NewWriteTools(deps WriteDeps) (*WriteTools, error) {
	specs, sch, err := a2Specs(writeSpecs)
	if err != nil {
		return nil, fmt.Errorf("write tools: %w", err)
	}
	return &WriteTools{deps: deps, specs: specs, schemas: sch, waiting: map[string]parked{}}, nil
}

// Specs returns the write tool specs.
func (t *WriteTools) Specs() []ToolSpec {
	return append([]ToolSpec(nil), t.specs...)
}

// Call runs one write tool: it records a proposal, opens its Counsel gate and parks the run.
func (t *WriteTools) Call(ctx context.Context, c Call) (Result, error) {
	if c.Tool != ToolPrepareTrustWithdrawal {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrUnknownTool)
	}
	var v any
	if err := json.Unmarshal(c.Input, &v); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	if err := t.schemas[c.Tool].Validate(v); err != nil {
		// Validation detail can echo client data; keep it out of the error.
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	ok, err := t.deps.Can(ctx, c.ActingFor, CapTrustRequest)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: capability: %w", c.Tool, err)
	}
	if !ok {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrForbidden)
	}
	return t.prepareTrustWithdrawal(ctx, c)
}

func (t *WriteTools) prepareTrustWithdrawal(ctx context.Context, c Call) (Result, error) {
	var in struct {
		MatterID    string `json:"matter_id"`
		AmountCents int64  `json:"amount_cents"`
		Purpose     string `json:"purpose_category"`
		Payee       string `json:"payee"`
		Method      string `json:"method"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrInvalidInput)
	}
	// rule: ARC-U-027
	// Fail closed: without the run's figures or the matter's contacts the amount and payee
	// cannot be checked, so nothing is recorded.
	if t.deps.Provenance == nil {
		return Result{}, fmt.Errorf("call %s: provenance: %w", c.Tool, ErrMissingDep)
	}
	if t.deps.Contacts == nil {
		return Result{}, fmt.Errorf("call %s: matter contacts: %w", c.Tool, ErrMissingDep)
	}
	visible, err := t.deps.Walls.Visible(ctx, c.ActingFor, in.MatterID)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: walls: %w", c.Tool, err)
	}
	if !visible {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrNotVisible)
	}
	// rule: ARC-U-027
	// The amount is a figure the run read (trust balance or bill), never one the model made
	// up; an unsourced amount is refused before any withdrawal row exists.
	if err := t.deps.Provenance.Check(c.RunID, []string{"/amount_cents"}, c.Input); err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	// rule: BIL-L-037
	// The payee is one of the matter's contacts by id, never free text the model wrote (so
	// never "cash" or "bearer"); anything else is refused before any withdrawal row exists.
	contacts, err := t.deps.Contacts.MatterContactIDs(ctx, in.MatterID)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: matter contacts: %w", c.Tool, err)
	}
	if in.Payee == "" || !slices.Contains(contacts, in.Payee) {
		return Result{}, fmt.Errorf("call %s: payee: %w", c.Tool, ErrPayeeNotContact)
	}
	method := withdraw.MethodCheque
	switch in.Method {
	case "", string(withdraw.MethodCheque):
	case string(withdraw.MethodEFT):
		method = withdraw.MethodEFT
	default:
		// The trust module has no wire method; a person picks it on the manual path.
		return Result{}, fmt.Errorf("call %s: method: %w", c.Tool, ErrInvalidInput)
	}
	account, err := t.deps.TrustAccount(ctx, in.MatterID)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: trust account: %w", c.Tool, err)
	}
	// rule: ARC-U-027
	// An overdraw is refused here, before the trust module records anything.
	available, err := t.deps.Withdrawals.Available(ctx, account, in.MatterID)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: available balance: %w", c.Tool, err)
	}
	if in.AmountCents > available {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, withdraw.ErrTrustOverdraw)
	}
	// rule: ARC-U-027
	// The agent only requests: the withdrawal is recorded as requested (proposed) and its
	// Counsel gate is opened. Authorize and RecordExecution are never reached from here.
	w, err := t.deps.Withdrawals.Request(ctx, withdraw.Request{
		TrustAccountID: account,
		MatterID:       in.MatterID,
		AmountCents:    in.AmountCents,
		Payee:          in.Payee,
		Method:         method,
		Reason:         in.Purpose,
		PreparedBy:     c.ActingFor,
		IdempotencyKey: idempotencyKey(c),
	})
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	if w.Status != withdraw.StatusRequested || w.GateID == "" {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, errNotProposed)
	}
	// rule: ARC-I-045
	// The Counsel gate is the only authority; no Paploo gate is opened. The run parks on the
	// Counsel gate id and resumes only on its gate.decided event.
	t.mu.Lock()
	t.waiting[w.GateID] = parked{runID: c.RunID, preparer: c.ActingFor}
	t.mu.Unlock()
	out, err := json.Marshal(map[string]string{"outcome": string(agenttools.OutcomeGate), "ref": w.GateID,
		"status": "proposed", "withdrawal_id": w.ID, "gate_id": w.GateID})
	if err != nil {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, err)
	}
	return Result{Output: out, GateID: w.GateID}, nil
}

// idempotencyKey makes a retried call of the same run and input propose once.
func idempotencyKey(c Call) string {
	h := sha256.New()
	h.Write([]byte(c.RunID))
	h.Write([]byte{0})
	h.Write([]byte(c.Tool))
	h.Write([]byte{0})
	h.Write(c.Input)
	return "agent:" + hex.EncodeToString(h.Sum(nil))
}

// Decided resumes the run parked on a Counsel gate. The acting-for user is the preparer and
// never decides the run's own gate, whatever the firm's self-approval setting.
func (t *WriteTools) Decided(ctx context.Context, ev GateDecided) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("decided: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	p, ok := t.waiting[ev.GateID]
	if !ok {
		return "", fmt.Errorf("decided %s: %w", ev.GateID, ErrUnknownGate)
	}
	// rule: ARC-I-045
	if ev.By == p.preparer {
		return "", fmt.Errorf("decided %s: %w", ev.GateID, ErrSelfApproval)
	}
	delete(t.waiting, ev.GateID)
	return p.runID, nil
}
