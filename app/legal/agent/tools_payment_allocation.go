package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolProposePaymentAllocation proposes how a received payment is split over bills, a trust
// ledger and unapplied money. The split takes effect only when a person approves its gate.
const ToolProposePaymentAllocation = "propose_payment_allocation"

// GatePaymentAllocation is the Counsel gate kind a payment allocation opens (spec G-B3).
const GatePaymentAllocation = "billing.payment_allocation"

// SubjectPayment is the gate subject kind of a received payment.
const SubjectPayment = "billing.payment"

// allocUnapplied is the allocation target with no id: money held as unapplied. The other
// targets (bill, trust_ledger) name their id.
const allocUnapplied = "unapplied"

// ProposePaymentAllocationHandler is propose_payment_allocation's handler, for the boot
// HandlerSet.
func ProposePaymentAllocationHandler() Handler {
	return Handler{Name: ToolProposePaymentAllocation, Build: buildProposePaymentAllocation}
}

// buildProposePaymentAllocation reads Deps: CapabilityCheck, roles.Walls, payment.Service,
// agenttools.GateOpener and *agenttools.Provenance.
func buildProposePaymentAllocation(d Deps) (registry.Tool, error) {
	wrap := func(err error) (registry.Tool, error) {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposePaymentAllocation, err)
	}
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return wrap(err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return wrap(err)
	}
	payments, err := DepOf[payment.Service](d)
	if err != nil {
		return wrap(err)
	}
	open, err := DepOf[agenttools.GateOpener](d)
	if err != nil {
		return wrap(err)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil {
		return wrap(err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolProposePaymentAllocation, Kind: Write, GateKind: GatePaymentAllocation}})
	if err != nil {
		return wrap(err)
	}
	s := specs[0]
	t := proposeAllocation{can: can, walls: walls, payments: payments, open: agenttools.ScopedOpener(open), capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return wrap(err)
	}
	// rule: ARC-U-027
	// Every allocated amount must be a figure the run read; the model never invents money.
	tool, err = pv.SourcedWrite(tool, "/allocations/*/amount_cents")
	if err != nil {
		return wrap(err)
	}
	return tool, nil
}

type proposeAllocation struct {
	can        CapabilityCheck
	walls      roles.Walls
	payments   payment.Service
	open       agenttools.GateOpener
	capability string
}

// allocationIn is one allocation of the input and of the G-B3 payload.
type allocationIn struct {
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id,omitempty"`
	AmountCents int64  `json:"amount_cents"`
}

type proposeAllocationIn struct {
	PaymentID   string         `json:"payment_id"`
	Allocations []allocationIn `json:"allocations"`
}

func (t proposeAllocation) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	fail := func(err error) (registry.ToolResult, error) {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposePaymentAllocation, err)
	}
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return fail(fmt.Errorf("capability: %w", err))
	}
	if !ok {
		return fail(ErrForbidden)
	}
	var in proposeAllocationIn
	if err := json.Unmarshal(raw, &in); err != nil || len(in.Allocations) == 0 {
		return fail(ErrInvalidInput)
	}
	pay, err := t.payments.Get(ctx, in.PaymentID)
	if err != nil {
		return fail(fmt.Errorf("payment: %w", err))
	}
	// rule: ARC-I-036
	// A payment with no matter cannot be checked against the walls, so it is not shown.
	if pay.Capture.MatterID == "" {
		return fail(ErrNotVisible)
	}
	if err := visibleTo(ctx, t.walls, user, pay.Capture.MatterID); err != nil {
		return fail(err)
	}
	if pay.Status != payment.StatusCaptured {
		return fail(fmt.Errorf("payment: %w", payment.ErrReversed))
	}
	var sum int64
	for i, a := range in.Allocations {
		if a.AmountCents <= 0 || (a.TargetType == allocUnapplied) != (a.TargetID == "") {
			return fail(fmt.Errorf("allocation %d: %w", i, ErrInvalidInput))
		}
		sum += a.AmountCents
	}
	// The split never moves more than the payment brought in.
	if sum > pay.Capture.AmountCents {
		return fail(fmt.Errorf("allocations exceed the payment: %w", ErrInvalidInput))
	}
	// rule: ARC-I-035
	// A run whose feature does not declare the allocation gate proposes nothing.
	if err := agenttools.CheckGateKind(ctx, p, GatePaymentAllocation); err != nil {
		return fail(err)
	}
	body, err := json.Marshal(proposeAllocationIn{PaymentID: in.PaymentID, Allocations: in.Allocations})
	if err != nil {
		return fail(fmt.Errorf("payload: %w", err))
	}
	// rule: ARC-I-039
	// The calling principal is the preparer; an agent principal can never decide the gate.
	gateID, err := t.open(ctx, GatePaymentAllocation, registry.Subject{Kind: SubjectPayment, ID: in.PaymentID}, body, p)
	if err != nil {
		return fail(fmt.Errorf("open gate: %w", err))
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeGate, Ref: string(gateID)})
	if err != nil {
		return fail(err)
	}
	return registry.ToolResult{Output: out, GateID: string(gateID)}, nil
}
