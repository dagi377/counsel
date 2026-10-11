package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

// Write tool names.
const (
	ToolPrepareTrustWithdrawal = "prepare_trust_withdrawal"
)

// CapTrustRequest is the capability prepare_trust_withdrawal needs.
const CapTrustRequest = "trust.request"

// GateTrustWithdrawal is the Counsel gate kind a trust withdrawal proposal opens.
const GateTrustWithdrawal = "trust.withdrawal"

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
	Withdrawals withdraw.Service
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

var writeSpecs = []ToolSpec{
	{
		Name: ToolPrepareTrustWithdrawal, Kind: Write, Capability: CapTrustRequest, GateKind: GateTrustWithdrawal,
		InputSchema: json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$defs":{"uuid":{"type":"string","format":"uuid"},"cents":{"type":"integer","minimum":0,"maximum":100000000000}},"type":"object","additionalProperties":false,"required":["matter_id","amount_cents","purpose_category","payee"],"properties":{"matter_id":{"$ref":"#/$defs/uuid"},"amount_cents":{"$ref":"#/$defs/cents"},"bill_id":{"$ref":"#/$defs/uuid"},"purpose_category":{"enum":["fees","disbursement","client_payment","third_party","refund"]},"payee":{"type":"string","maxLength":200},"method":{"enum":["eft","cheque","wire"]}}}`),
	},
}

// NewWriteTools returns the write tools over deps.
func NewWriteTools(deps WriteDeps) (*WriteTools, error) {
	t := &WriteTools{deps: deps, specs: writeSpecs, schemas: map[string]*jsonschema.Schema{}, waiting: map[string]parked{}}
	for _, s := range writeSpecs {
		c := jsonschema.NewCompiler()
		url := "tool://" + s.Name + "/input.json"
		if err := c.AddResource(url, bytes.NewReader(s.InputSchema)); err != nil {
			return nil, fmt.Errorf("write tool %s schema: %w", s.Name, err)
		}
		sch, err := c.Compile(url)
		if err != nil {
			return nil, fmt.Errorf("write tool %s schema: %w", s.Name, err)
		}
		t.schemas[s.Name] = sch
	}
	return t, nil
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
	visible, err := t.deps.Walls.Visible(ctx, c.ActingFor, in.MatterID)
	if err != nil {
		return Result{}, fmt.Errorf("call %s: walls: %w", c.Tool, err)
	}
	if !visible {
		return Result{}, fmt.Errorf("call %s: %w", c.Tool, ErrNotVisible)
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
	out, err := json.Marshal(map[string]string{"status": "proposed", "withdrawal_id": w.ID, "gate_id": w.GateID})
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
