package comms

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateGuestAccess is the G-GUEST kind whose effect this module supplies (comms.portal_access).
const GateGuestAccess = "platform.guest_access"

// The guest gets portal access to the matter, invited by the approver; while an unrevoked guest
// row exists a re-run inserts nothing.
const sqlGrantGuest = `INSERT INTO comms.portal_access (contact_id, matter_id, role, invited_at, invited_by)
SELECT $1::uuid, $2::uuid, 'guest', $4, $3::uuid
WHERE NOT EXISTS (SELECT 1 FROM comms.portal_access
  WHERE contact_id = $1::uuid AND matter_id = $2::uuid AND role = 'guest' AND revoked_at IS NULL)`

// GuestAccessEffect is the platform.guest_access (G-GUEST) effect: the guest contact gets
// portal access to the matter at now().
func GuestAccessEffect(now func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return grantGuest(ctx, tx, g, now)
	}
}

func grantGuest(ctx context.Context, tx gates.Execer, g registry.Approved, now func() time.Time) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p struct {
		MatterID       string `json:"matter_id"`
		GuestContactID string `json:"guest_contact_id"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.MatterID == "" || p.GuestContactID == "" {
		return fmt.Errorf("approve %s (gate %s): no matter or guest: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-012
	if _, err := tx.Exec(ctx, sqlGrantGuest, p.GuestContactID, p.MatterID, by, now()); err != nil {
		return fmt.Errorf("approve %s (gate %s): %w", g.Kind, g.GateID, err)
	}
	return nil
}
