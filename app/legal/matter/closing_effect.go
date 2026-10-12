package matter

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// sqlCloseMatter closes an open or closing matter whose trust ledgers all hold $0.00. A matter
// already closed is left as it is, so a re-run changes nothing.
const sqlCloseMatter = `UPDATE matter.matter m SET status = 'closed',
  close_date = COALESCE(m.close_date, $3::date),
  updated_by = CASE WHEN m.status <> 'closed' THEN $2::uuid ELSE m.updated_by END,
  updated_at = CASE WHEN m.status <> 'closed' THEN $4 ELSE m.updated_at END,
  row_version = m.row_version + CASE WHEN m.status <> 'closed' THEN 1 ELSE 0 END
WHERE m.id = $1::uuid AND m.archived_at IS NULL AND m.status IN ('open','closing','closed')
  AND NOT EXISTS (SELECT 1 FROM trust.ledger_balance b WHERE b.matter_id = m.id AND b.balance_cents <> 0)`

// closingPayload is the platform.closing payload (spec/gates.json).
type closingPayload struct {
	MatterID          string `json:"matter_id"`
	TrustBalanceCents *int64 `json:"trust_balance_cents"`
}

// ClosingEffect is the platform.closing (G13) effect: the matter closes, dated today in the
// business zone. now gives the instant; loc the business zone (America/Toronto).
func ClosingEffect(now func() time.Time, loc *time.Location) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return closeMatter(ctx, tx, g, now, loc)
	}
}

func closeMatter(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time, loc *time.Location) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p closingPayload
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.TrustBalanceCents == nil {
		return fmt.Errorf("approve %s (gate %s): matter and trust balance: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-025
	// By-law 9: a matter holding client money in trust never closes. The approved balance must
	// be $0.00, and the statement re-checks every ledger of the matter in the gate's tx.
	if *p.TrustBalanceCents != 0 {
		return fmt.Errorf("approve %s (gate %s): trust balance %d cents: %w", g.Kind, g.GateID, *p.TrustBalanceCents, gates.ErrNotApprovable)
	}
	at := now()
	// rule: ARC-I-012
	return gates.ExecOne(ctx, tx, g, sqlCloseMatter, p.MatterID, by, at.In(loc).Format(time.DateOnly), at)
}
