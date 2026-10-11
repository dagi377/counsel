package withdraw

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/registry"
)

// FeeEffect is the billing.trust_fee_withdrawal (GateFeeKind) effect: a withdrawal that pays
// a bill is authorised under the same rules as any trust withdrawal (Effect.Apply: the
// approver is a signatory and not the preparer, the bill is re-checked as delivered, on the
// matter and unpaid, and the ledger is locked and re-checked). It runs only while the
// By-law 9 rules are verified. Execution then goes through the execution route, which reads
// the approved payload of either gate kind (PGApprovals).
func FeeEffect(e *Effect) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		// rule: BIL-L-035 (verify)
		if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
			return failed(CodeNotAuthorized, ErrUnverified)
		}
		if e == nil {
			return failed(CodeNotAuthorized, fmt.Errorf("fee withdrawal effect: not configured: %w", ErrNotAuthorized))
		}
		// rule: ARC-I-027
		// Apply refuses an agent approver and the preparer, and re-checks the bill.
		return e.Apply(ctx, tx, g)
	}
}
