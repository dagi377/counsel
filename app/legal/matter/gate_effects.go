package matter

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateEffects are the approval effects of the matter gate kinds this package applies. Each
// runs in the deciding tx, refuses an agent approver or the preparer, and is idempotent per
// gate. matter.contact_merge, practice_config, bulk_change, renumber, widen_access and
// apply_template are not here: their payloads leave the change open (Q-GE2-LEGAL).
func GateEffects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		matterkind.GatePartyConflict: gates.Effect(applyPartyConflict),
		GateKeyFact:                  gates.Effect(applyKeyFact),
	}
}

// G2 on a matter: once its conflict check is cleared the party joins the matter in the
// approved role, approved by the approver. A proposed role row is approved in place.
const (
	partyRoleUpdateSQL = `UPDATE matter.party_role SET approved_by = $5::uuid, approved_at = now(), conflict_check_id = $4::uuid
WHERE matter_id = $1::uuid AND contact_id = $2::uuid AND role = $3 AND approved_by IS NULL AND archived_at IS NULL
  AND EXISTS (SELECT 1 FROM conflict.conflict_check c WHERE c.id = $4::uuid AND c.status = 'cleared')`
	partyRoleInsertSQL = `INSERT INTO matter.party_role (matter_id, contact_id, role, conflict_check_id, approved_by, approved_at, created_by)
SELECT $1::uuid, $2::uuid, $3, $4::uuid, $5::uuid, now(), $5::uuid
 WHERE EXISTS (SELECT 1 FROM conflict.conflict_check c WHERE c.id = $4::uuid AND c.status = 'cleared')
   AND NOT EXISTS (SELECT 1 FROM matter.party_role r
                    WHERE r.matter_id = $1::uuid AND r.contact_id = $2::uuid AND r.role = $3 AND r.archived_at IS NULL)`
	partyRoleProofSQL = `SELECT 1 FROM matter.party_role
 WHERE matter_id = $1::uuid AND contact_id = $2::uuid AND role = $3 AND conflict_check_id = $4::uuid
   AND approved_by IS NOT NULL AND archived_at IS NULL`
)

type partyConflictPayload struct {
	MatterID  string `json:"matter_id"`
	ContactID string `json:"contact_id"`
	Role      string `json:"role"`
	CheckID   string `json:"check_id"`
}

func applyPartyConflict(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p partyConflictPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"matter_id": p.MatterID, "contact_id": p.ContactID, "role": p.Role, "check_id": p.CheckID}); err != nil {
		return err
	}
	args := []any{p.MatterID, p.ContactID, p.Role, p.CheckID, by}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: partyRoleUpdateSQL, Args: args},
		{SQL: partyRoleInsertSQL, Args: args},
	}, gates.Step{SQL: partyRoleProofSQL, Args: args[:4]})
}

// G-KF: the matter's key fact takes the approved value, confirmed by the approver. Facts are
// versioned by supersession: the current fact is parked as superseded by itself, the new
// fact (sourced to the gate) is written, and the parked one is pointed at it. The gate's
// source ref keys the change, so a re-run writes nothing.
const (
	keyFactParkSQL = `UPDATE matter.fact SET superseded_by = id
WHERE matter_id = $1::uuid AND key = $2 AND superseded_by IS NULL
  AND NOT ($3 = ANY(source_refs))`
	keyFactInsertSQL = `INSERT INTO matter.fact (matter_id, key, value, source_refs, confirmed_by, confirmed_at, created_by)
SELECT $1::uuid, $2, $3::jsonb, ARRAY[$4], $5::uuid, now(), $5::uuid
 WHERE EXISTS (SELECT 1 FROM matter.matter m WHERE m.id = $1::uuid AND m.archived_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM matter.fact f WHERE f.matter_id = $1::uuid AND f.key = $2 AND $4 = ANY(f.source_refs))`
	keyFactLinkSQL = `UPDATE matter.fact o SET superseded_by = n.id
  FROM matter.fact n
 WHERE o.matter_id = $1::uuid AND o.key = $2 AND o.superseded_by = o.id
   AND n.matter_id = $1::uuid AND n.key = $2 AND $3 = ANY(n.source_refs) AND n.superseded_by IS NULL`
	keyFactProofSQL = `SELECT 1 FROM matter.fact
 WHERE matter_id = $1::uuid AND key = $2 AND value = $3::jsonb AND $4 = ANY(source_refs) AND confirmed_by = $5::uuid`
)

type keyFactPayload struct {
	MatterID string          `json:"matter_id"`
	Field    string          `json:"field"`
	NewValue json.RawMessage `json:"new_value"`
}

func applyKeyFact(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p keyFactPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"matter_id": p.MatterID, "field": p.Field}); err != nil {
		return err
	}
	if len(p.NewValue) == 0 {
		return fmt.Errorf("%s gate %s payload: no new_value: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	ref := "gate:" + g.GateID
	args := []any{p.MatterID, p.Field, string(p.NewValue), ref, by}
	keyed := []any{p.MatterID, p.Field, ref}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: keyFactParkSQL, Args: keyed},
		{SQL: keyFactInsertSQL, Args: args},
		{SQL: keyFactLinkSQL, Args: keyed},
	}, gates.Step{SQL: keyFactProofSQL, Args: args})
}
