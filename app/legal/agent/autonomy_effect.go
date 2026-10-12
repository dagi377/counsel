package agent

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateSkillAutonomy is the G-SK kind whose effect this module supplies (agent.skill is its table).
const GateSkillAutonomy = "platform.skill_autonomy"

// autonomyLevels are agent.skill.autonomy values, lowest first.
var autonomyLevels = []string{"off", "suggest", "draft", "act_with_approval"}

// Every version of the skill takes the approved autonomy, never above its autonomy_cap. Setting
// the same level again matches the same rows and changes nothing.
const sqlSkillAutonomy = `UPDATE agent.skill SET autonomy = $2
WHERE key = $1
  AND array_position(ARRAY['off','suggest','draft','act_with_approval'], $2::text)
   <= array_position(ARRAY['off','suggest','draft','act_with_approval'], autonomy_cap)`

// SkillAutonomyEffect is the platform.skill_autonomy (G-SK) effect: the skill runs at the
// approved autonomy from now on.
func SkillAutonomyEffect(_ func() time.Time) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return setSkillAutonomy(ctx, tx, g)
	}
}

func setSkillAutonomy(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	if _, err := gates.Approver(g); err != nil {
		return err
	}
	var p struct {
		Skill    string `json:"skill"`
		Autonomy string `json:"autonomy"`
	}
	if err := gates.DecodePayload(g, &p); err != nil {
		return err
	}
	if p.Skill == "" || !slices.Contains(autonomyLevels, p.Autonomy) {
		return fmt.Errorf("approve %s (gate %s): skill or autonomy: %w", g.Kind, g.GateID, gates.ErrNotApprovable)
	}
	// rule: ARC-I-012
	// No row moves when the skill is unknown or the level is above its cap.
	return gates.ExecOne(ctx, tx, g, sqlSkillAutonomy, p.Skill, p.Autonomy)
}
