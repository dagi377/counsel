package trust

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/registry"
)

// cashGatePayload is the trust.cash_over_limit gate payload (G6c).
type cashGatePayload struct {
	MatterID       string `json:"matter_id"`
	AmountCents    *int64 `json:"amount_cents"`
	AggregateCents *int64 `json:"aggregate_cents"`
	Exception      string `json:"exception"`
}

// CashGateEffect is the trust.cash_over_limit EffectFunc. The approved gate is itself the
// By-law 9 s.6 evidence: the receipt that relies on it reads it back (approvedCashGate) for the
// same matter, amount and exception code, so the effect changes no row. It checks, in the
// deciding tx, that the approval is one a receipt may rely on: a human approver who is not the
// preparer, a known exception code, and an amount that is part of the stated aggregate. It
// writes nothing, so a repeat is a no-op.
func CashGateEffect(ctx context.Context, _ pgx.Tx, g registry.Approved) error {
	// rule: BIL-L-039 (verify)
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return fmt.Errorf("cash gate %s: %w", g.GateID, ErrUnverified)
	}
	if len(g.Approvers) == 0 {
		return fmt.Errorf("cash gate %s: no approver: %w", g.GateID, ErrInvalid)
	}
	prep := g.Preparer.UserID
	if g.Preparer.Kind == registry.PrincipalAgent {
		prep = g.Preparer.ActingFor
	}
	for _, a := range g.Approvers {
		// rule: ARC-I-019
		if a.Kind != registry.PrincipalUser || a.UserID == "" {
			return fmt.Errorf("cash gate %s: %w", g.GateID, ErrAgentApproval)
		}
		// rule: ARC-I-027
		if prep != "" && a.UserID == prep {
			return fmt.Errorf("cash gate %s: %w", g.GateID, ErrSelfApproval)
		}
	}
	var p cashGatePayload
	if err := json.Unmarshal(g.Payload, &p); err != nil {
		return fmt.Errorf("cash gate %s: payload: %w", g.GateID, ErrInvalid)
	}
	switch {
	case p.MatterID == "" || p.AmountCents == nil || p.AggregateCents == nil:
		return fmt.Errorf("cash gate %s: matter, amount and aggregate are required: %w", g.GateID, ErrInvalid)
	case *p.AmountCents <= 0 || *p.AggregateCents < *p.AmountCents:
		return fmt.Errorf("cash gate %s: amount %d of aggregate %d: %w", g.GateID, *p.AmountCents, *p.AggregateCents, ErrInvalid)
	case !validCashException(p.Exception):
		// rule: BIL-L-039 (verify)
		// Cash at or over the limit is accepted only under a By-law 9 s.6 exception; an
		// approval without one would let a receipt through that the rule forbids.
		return fmt.Errorf("cash gate %s: exception %q: %w", g.GateID, p.Exception, ErrCashLimit)
	}
	return nil
}

// CashGateEffects returns the effect handler keyed by gate kind.
func CashGateEffects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{CashGateKind: CashGateEffect}
}
