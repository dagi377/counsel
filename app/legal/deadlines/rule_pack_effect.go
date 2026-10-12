package deadlines

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateRulePack is the G-RULE kind whose effect this module supplies (cal.rule_pack is its table).
const GateRulePack = "platform.rule_pack"

const (
	// Only a verified (or already active) version of the pack activates.
	sqlActivateRulePack = `UPDATE cal.rule_pack SET status = 'active'
WHERE code = $1 AND version = $2 AND province = $3 AND status IN ('verified','active')`
	// The pack's other active versions in the province retire; a re-run finds none.
	sqlRetireRulePacks = `UPDATE cal.rule_pack SET status = 'retired'
WHERE code = $1 AND version <> $2 AND province = $3 AND status = 'active'`
)

// PlatformRulePackEffect is the platform.rule_pack (G-RULE) effect: the approved version of the pack
// becomes the active one in its province and any other active version retires.
func PlatformRulePackEffect(_ func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return activateRulePack(ctx, tx, g)
	}
}

func activateRulePack(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	if _, err := gates.Approver(g); err != nil {
		return err
	}
	var p struct {
		Pack     string `json:"pack"`
		Version  int    `json:"version"`
		Province string `json:"province"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.Pack == "" || p.Version < 1 || p.Province == "" {
		return fmt.Errorf("approve %s (gate %s): pack: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	v := strconv.Itoa(p.Version)
	// rule: ARC-I-012
	if err := gates.ExecOne(ctx, tx, g, sqlActivateRulePack, p.Pack, v, p.Province); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, sqlRetireRulePacks, p.Pack, v, p.Province); err != nil {
		return fmt.Errorf("approve %s (gate %s): retire: %w", g.Kind, g.GateID, err)
	}
	return nil
}
