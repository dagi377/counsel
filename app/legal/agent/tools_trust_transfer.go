package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolPrepareTrustTransfer prepares a matter-to-matter trust transfer and opens its
// trust.transfer gate (G11t). It moves no money: a person other than the preparer authorizes
// the transfer, and the gate's effect posts it (docs/QUESTIONS.md Q-TRU-FIX, Q-TRANSFER-STORE).
const ToolPrepareTrustTransfer = "prepare_trust_transfer"

// GateTrustTransfer is the Counsel gate kind a trust transfer proposal opens (G11t).
const GateTrustTransfer = withdraw.TransferGateKind

// ErrTransferAccounts: the two matters share no single trust account, so the move is not a
// ledger-to-ledger transfer (a move between bank accounts is a withdrawal) (422).
var ErrTransferAccounts = fmt.Errorf("agent: matters share no single trust account: %w", ErrInvalidInput)

// TrustTransfers is the part of the trust module that records a transfer request: it writes
// the trust.trust_transfer row pending authorization and opens its trust.transfer gate in one
// tx (withdraw.TransferRequests satisfies it). It never authorizes or posts.
type TrustTransfers interface {
	Request(ctx context.Context, r withdraw.TransferRequest) (withdraw.TransferRequested, error)
}

// PrepareTrustTransferHandler is prepare_trust_transfer's handler, for the boot HandlerSet.
func PrepareTrustTransferHandler() Handler {
	return Handler{Name: ToolPrepareTrustTransfer, Build: buildPrepareTrustTransfer}
}

// buildPrepareTrustTransfer reads Deps: CapabilityCheck, roles.Walls, TrustAvailable,
// MatterTrustAccounts, TrustTransfers and *agenttools.Provenance.
func buildPrepareTrustTransfer(d Deps) (registry.Tool, error) {
	wrap := func(err error) (registry.Tool, error) {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolPrepareTrustTransfer, err)
	}
	// rule: ARC-U-027
	// Fail closed: a missing port fails boot instead of building a tool that skips a check.
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return wrap(err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return wrap(err)
	}
	avail, err := DepOf[TrustAvailable](d)
	if err != nil {
		return wrap(err)
	}
	accounts, err := DepOf[MatterTrustAccounts](d)
	if err != nil {
		return wrap(err)
	}
	transfers, err := DepOf[TrustTransfers](d)
	if err != nil {
		return wrap(err)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil {
		return wrap(err)
	}
	if pv == nil {
		return wrap(ErrMissingDep)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolPrepareTrustTransfer, Kind: Write, GateKind: GateTrustTransfer}})
	if err != nil {
		return wrap(err)
	}
	s := specs[0]
	t := prepareTransfer{can: can, walls: walls, avail: avail, accounts: accounts,
		transfers: transfers, capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return wrap(err)
	}
	// rule: ARC-U-027
	// The amount is a figure the run read (a trust balance or a bill), never one the model
	// made up.
	tool, err = pv.SourcedWrite(tool, "/amount_cents")
	if err != nil {
		return wrap(err)
	}
	return tool, nil
}

type prepareTransfer struct {
	can        CapabilityCheck
	walls      roles.Walls
	avail      TrustAvailable
	accounts   MatterTrustAccounts
	transfers  TrustTransfers
	capability string
}

type prepareTransferIn struct {
	FromMatterID string `json:"from_matter_id"`
	ToMatterID   string `json:"to_matter_id"`
	AmountCents  int64  `json:"amount_cents"`
	Reason       string `json:"reason"`
}

func (t prepareTransfer) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	fail := func(err error) (registry.ToolResult, error) {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolPrepareTrustTransfer, err)
	}
	user := actor(p)
	// rule: ARC-I-027
	// The preparer is the user the run acts for; a call without one records nothing.
	if user == "" {
		return fail(ErrForbidden)
	}
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return fail(fmt.Errorf("capability: %w", err))
	}
	if !ok {
		return fail(ErrForbidden)
	}
	var in prepareTransferIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return fail(ErrInvalidInput)
	}
	// Integer cents, more than zero, between two different matters.
	if in.AmountCents <= 0 || in.FromMatterID == "" || in.ToMatterID == "" || in.FromMatterID == in.ToMatterID {
		return fail(ErrInvalidInput)
	}
	// rule: ARC-I-036
	// Both matters must be visible to the acting user: a walled matter is neither a source
	// nor a destination.
	for _, m := range []string{in.FromMatterID, in.ToMatterID} {
		if err := visibleTo(ctx, t.walls, user, m); err != nil {
			return fail(err)
		}
	}
	account, err := t.sharedAccount(ctx, in.FromMatterID, in.ToMatterID)
	if err != nil {
		return fail(err)
	}
	// rule: ARC-I-025
	// By-Law 9: a transfer never overdraws the source client ledger. Refused before any gate.
	available, err := t.avail.Available(ctx, account, in.FromMatterID)
	if err != nil {
		return fail(fmt.Errorf("available balance: %w", err))
	}
	if in.AmountCents > available {
		return fail(withdraw.ErrTrustOverdraw)
	}
	// rule: ARC-I-035
	// The run scope must hold trust.transfer: a gate kind the run's feature does not declare
	// is refused before any transfer row exists.
	if err := agenttools.CheckGateKind(ctx, p, GateTrustTransfer); err != nil {
		return fail(err)
	}
	// rule: ARC-I-039
	// The transfer row (pending authorization) and its trust.transfer gate are written in one
	// tx. The calling principal is the gate's preparer and the acting user the row's, so
	// neither decides it; the gate effect posts the transfer for another signatory.
	got, err := t.transfers.Request(ctx, withdraw.TransferRequest{
		TrustAccountID: account, FromMatterID: in.FromMatterID, ToMatterID: in.ToMatterID,
		AmountCents: in.AmountCents, Reason: in.Reason, PreparedBy: user, Preparer: p,
		Subject: registry.Subject{Kind: roles.SubjectMatter, ID: in.FromMatterID},
	})
	if err != nil {
		return fail(fmt.Errorf("request transfer: %w", err))
	}
	gateID := got.GateID
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeGate, Ref: string(gateID)})
	if err != nil {
		return fail(err)
	}
	return registry.ToolResult{Output: out, GateID: string(gateID)}, nil
}

// sharedAccount returns the one trust account that holds a ledger for both matters. None, or
// more than one, fails closed: the transfer's ledgers would be ambiguous.
func (t prepareTransfer) sharedAccount(ctx context.Context, from, to string) (string, error) {
	fa, err := t.accounts.TrustAccountsOf(ctx, from)
	if err != nil {
		return "", fmt.Errorf("trust accounts: %w", err)
	}
	ta, err := t.accounts.TrustAccountsOf(ctx, to)
	if err != nil {
		return "", fmt.Errorf("trust accounts: %w", err)
	}
	var shared []string
	for _, a := range fa {
		if slices.Contains(ta, a) && !slices.Contains(shared, a) {
			shared = append(shared, a)
		}
	}
	if len(shared) != 1 {
		return "", ErrTransferAccounts
	}
	return shared[0], nil
}
