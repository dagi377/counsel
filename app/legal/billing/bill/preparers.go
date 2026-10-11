package bill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// billEditorsSQL is every user who created or last edited the bill a gate decides.
const billEditorsSQL = `SELECT coalesce(created_by::text, ''), coalesce(updated_by::text, '')
  FROM billing.bill WHERE id = $1::uuid`

// Preparers maps the bill gate kinds to the editor lookup that adds the bill's creator and
// last editor to the gate's preparer set (ARC-I-016).
func Preparers() map[string]registry.PreparersFunc {
	return map[string]registry.PreparersFunc{GateBillApproval: BillEditors, GateBillVoid: BillEditors}
}

// BillEditors returns the creator and last editor of the bill named by payload.bill_id. A
// payload with no bill id fails closed.
func BillEditors(ctx context.Context, tx pgx.Tx, _ registry.Subject, payload json.RawMessage) ([]string, error) {
	// rule: ARC-I-016
	var p struct {
		BillID string `json:"bill_id"`
	}
	if err := json.Unmarshal(payload, &p); err != nil || p.BillID == "" {
		return nil, fmt.Errorf("bill editors: payload has no bill_id: %w", ErrInvalid)
	}
	var created, updated string
	err := tx.QueryRow(ctx, billEditorsSQL, p.BillID).Scan(&created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("bill editors %s: %w", p.BillID, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("bill editors %s: %w", p.BillID, err)
	}
	var out []string
	for _, u := range []string{created, updated} {
		if u != "" {
			out = append(out, u)
		}
	}
	return out, nil
}
