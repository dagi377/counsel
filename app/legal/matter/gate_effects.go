package matter

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateEffects are the approval effects of the matter gate kinds this package applies. Each
// runs in the deciding tx, refuses an agent approver or the preparer, and is idempotent per
// gate. The payload shapes are in spec/gates.json; the Build* functions below fill them.
func GateEffects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		matterkind.GatePartyConflict: gates.Effect(applyPartyConflict),
		GateKeyFact:                  gates.Effect(applyKeyFact),
		GateContactMerge:             gates.Effect(applyContactMerge),
		GatePracticeConfig:           gates.Effect(applyPracticeConfig),
		GateBulkChange:               gates.Effect(applyBulkChange),
		GateRenumber:                 gates.Effect(applyRenumber),
		GateWidenAccess:              gates.Effect(applyWidenAccess),
		GateApplyTemplate:            gates.Effect(applyTemplate),
	}
}

// Matter gate kinds with no other home in this package.
const (
	GateBulkChange = "matter.bulk_change"
	GateRenumber   = "matter.renumber"
)

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

// ids returns the distinct non-empty ids, sorted, or ErrNotApplied naming field.
func ids(g registry.Approved, field string, in []string) ([]string, error) {
	out := slices.Compact(slices.Sorted(slices.Values(in)))
	if len(out) == 0 || out[0] == "" {
		return nil, fmt.Errorf("%s gate %s payload: no %s: %w", g.Kind, g.GateID, field, gates.ErrNotApplied)
	}
	return out, nil
}

func refuse(g registry.Approved, why string) error {
	return fmt.Errorf("%s gate %s payload: %s: %w", g.Kind, g.GateID, why, gates.ErrNotApplied)
}

// G-CM: contacts are merged into a survivor, archived (the "delete" of a contact; contacts
// are never deleted) or related by a confirmed relationship. A merged contact keeps its row
// with merged_into_id, so conflict searches still find it.
const (
	contactMergeSQL = `UPDATE party.contact SET state = 'merged', merged_into_id = $2::uuid, updated_by = $3::uuid,
  updated_at = now(), row_version = row_version + 1
WHERE id = ANY($1::text[]::uuid[]) AND id <> $2::uuid AND state IN ('proposed','active')
  AND EXISTS (SELECT 1 FROM party.contact s WHERE s.id = $2::uuid AND s.state IN ('proposed','active'))`
	contactMergeProofSQL = `SELECT 1 WHERE (SELECT count(*) FROM party.contact
 WHERE id = ANY($1::text[]::uuid[]) AND id <> $2::uuid AND state = 'merged' AND merged_into_id = $2::uuid)
 = cardinality($1::text[]) - 1`
	// A contact that is a matter's client or holds an open party role stays (fail closed).
	contactArchiveSQL = `UPDATE party.contact c SET state = 'archived', archived_at = now(), updated_by = $2::uuid,
  updated_at = now(), row_version = row_version + 1
WHERE c.id = ANY($1::text[]::uuid[]) AND c.state IN ('proposed','active')
  AND NOT EXISTS (SELECT 1 FROM matter.matter m WHERE m.client_id = c.id)
  AND NOT EXISTS (SELECT 1 FROM matter.party_role r WHERE r.contact_id = c.id AND r.archived_at IS NULL)`
	contactArchiveProofSQL = `SELECT 1 WHERE (SELECT count(*) FROM party.contact
 WHERE id = ANY($1::text[]::uuid[]) AND state = 'archived') = cardinality($1::text[])`
	contactRelateSQL = `INSERT INTO party.contact_relationship (from_contact_id, to_contact_id, kind, source_ref,
  confirmed_by, confirmed_at, created_by)
VALUES ($1::uuid, $2::uuid, $3, $4, $5::uuid, now(), $5::uuid)
ON CONFLICT (from_contact_id, to_contact_id, kind) DO NOTHING`
	contactRelateProofSQL = `SELECT 1 FROM party.contact_relationship
 WHERE from_contact_id = $1::uuid AND to_contact_id = $2::uuid AND kind = $3 AND archived_at IS NULL`
)

// ContactMergePayload is the matter.contact_merge payload.
type ContactMergePayload struct {
	ContactIDs []string `json:"contact_ids"`
	Action     string   `json:"action"`
	// SurvivorID is the contact a merge keeps; it is one of ContactIDs.
	SurvivorID string `json:"survivor_id,omitempty"`
	// RelationshipKind is the party.contact_relationship kind of a relate, from
	// ContactIDs[0] to ContactIDs[1].
	RelationshipKind string `json:"relationship_kind,omitempty"`
}

// BuildContactMerge fills and checks a matter.contact_merge payload.
func BuildContactMerge(p ContactMergePayload) (json.RawMessage, error) {
	g := registry.Approved{Kind: GateContactMerge}
	if err := p.check(g); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

func (p ContactMergePayload) check(g registry.Approved) error {
	if _, err := ids(g, "contact_ids", p.ContactIDs); err != nil {
		return err
	}
	switch p.Action {
	case "merge":
		if p.SurvivorID == "" || !slices.Contains(p.ContactIDs, p.SurvivorID) || len(p.ContactIDs) < 2 {
			return refuse(g, "a merge names a survivor among two or more contacts")
		}
	case "relate":
		if len(p.ContactIDs) != 2 || p.ContactIDs[0] == p.ContactIDs[1] || p.RelationshipKind == "" {
			return refuse(g, "a relate names two contacts and a relationship_kind")
		}
	case "delete":
	default:
		return refuse(g, "no action")
	}
	return nil
}

func applyContactMerge(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p ContactMergePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := p.check(g); err != nil {
		return err
	}
	all, _ := ids(g, "contact_ids", p.ContactIDs)
	switch p.Action {
	case "merge":
		// rule: MIG-014
		// Merging is a person's approved action, never automatic.
		return gates.Apply(ctx, tx, g, []gates.Step{{SQL: contactMergeSQL, Args: []any{all, p.SurvivorID, by}}},
			gates.Step{SQL: contactMergeProofSQL, Args: []any{all, p.SurvivorID}})
	case "delete":
		return gates.Apply(ctx, tx, g, []gates.Step{{SQL: contactArchiveSQL, Args: []any{all, by}}},
			gates.Step{SQL: contactArchiveProofSQL, Args: []any{all}})
	default:
		a := []any{p.ContactIDs[0], p.ContactIDs[1], p.RelationshipKind, "gate:" + g.GateID, by}
		return gates.Apply(ctx, tx, g, []gates.Step{{SQL: contactRelateSQL, Args: a}},
			gates.Step{SQL: contactRelateProofSQL, Args: a[:3]})
	}
}

// G-CF: a practice area's configuration takes the approved fields; config_version moves on
// and approval_id records the gate, which keys the change.
const (
	practiceConfigSQL = `UPDATE matter.practice_area SET
  default_billing_method = COALESCE($2::jsonb->>'default_billing_method', default_billing_method),
  default_retention_class = COALESCE($2::jsonb->>'default_retention_class', default_retention_class),
  default_roles = CASE WHEN $2::jsonb ? 'default_roles'
    THEN ARRAY(SELECT jsonb_array_elements_text($2::jsonb->'default_roles')) ELSE default_roles END,
  default_skills = CASE WHEN $2::jsonb ? 'default_skills'
    THEN ARRAY(SELECT jsonb_array_elements_text($2::jsonb->'default_skills')) ELSE default_skills END,
  intake_questions = COALESCE($2::jsonb->'intake_questions', intake_questions),
  closing_checklist_template = COALESCE($2::jsonb->'closing_checklist_template', closing_checklist_template),
  config_version = config_version + 1, approval_id = $3::uuid, updated_by = $4::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND archived_at IS NULL AND approval_id IS DISTINCT FROM $3::uuid`
	practiceConfigProofSQL = `SELECT 1 FROM matter.practice_area WHERE id = $1::uuid AND approval_id = $2::uuid`
)

// practiceConfigFields are the practice area fields a G-CF change may set.
var practiceConfigFields = []string{"closing_checklist_template", "default_billing_method", "default_retention_class",
	"default_roles", "default_skills", "intake_questions"}

// PracticeConfigPayload is the matter.practice_config payload; PracticeArea is the
// practice area id.
type PracticeConfigPayload struct {
	PracticeArea string                     `json:"practice_area"`
	Change       map[string]json.RawMessage `json:"change"`
}

// BuildPracticeConfig fills and checks a matter.practice_config payload.
func BuildPracticeConfig(practiceAreaID string, change map[string]json.RawMessage) (json.RawMessage, error) {
	p := PracticeConfigPayload{PracticeArea: practiceAreaID, Change: change}
	if err := p.check(registry.Approved{Kind: GatePracticeConfig}); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

func (p PracticeConfigPayload) check(g registry.Approved) error {
	if p.PracticeArea == "" {
		return refuse(g, "no practice_area")
	}
	if len(p.Change) == 0 {
		return refuse(g, "no change")
	}
	for _, k := range slices.Sorted(maps.Keys(p.Change)) {
		if !slices.Contains(practiceConfigFields, k) {
			return refuse(g, "change sets "+strconv.Quote(k))
		}
	}
	return nil
}

func applyPracticeConfig(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p PracticeConfigPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := p.check(g); err != nil {
		return err
	}
	change, err := json.Marshal(p.Change)
	if err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{{SQL: practiceConfigSQL, Args: []any{p.PracticeArea, string(change), g.GateID, by}}},
		gates.Step{SQL: practiceConfigProofSQL, Args: []any{p.PracticeArea, g.GateID}})
}

// G-BK: the approved fields are set on every listed matter. Only rows that differ are
// written, and the proof needs every matter to carry the values, so a re-run is a no-op.
const (
	bulkChangeSQL = `UPDATE matter.matter SET
  responsible_lawyer_id = COALESCE(($2::jsonb->>'responsible_lawyer_id')::uuid, responsible_lawyer_id),
  responsible_staff_id = COALESCE(($2::jsonb->>'responsible_staff_id')::uuid, responsible_staff_id),
  retention_class = COALESCE($2::jsonb->>'retention_class', retention_class),
  billing_method = COALESCE($2::jsonb->>'billing_method', billing_method),
  updated_by = $3::uuid, updated_at = now(), row_version = row_version + 1
WHERE id = ANY($1::text[]::uuid[]) AND archived_at IS NULL AND status NOT IN ('closed','archived')
  AND (responsible_lawyer_id, responsible_staff_id, retention_class, billing_method) IS DISTINCT FROM
      (COALESCE(($2::jsonb->>'responsible_lawyer_id')::uuid, responsible_lawyer_id),
       COALESCE(($2::jsonb->>'responsible_staff_id')::uuid, responsible_staff_id),
       COALESCE($2::jsonb->>'retention_class', retention_class), COALESCE($2::jsonb->>'billing_method', billing_method))`
	bulkChangeProofSQL = `SELECT 1 WHERE (SELECT count(*) FROM matter.matter
 WHERE id = ANY($1::text[]::uuid[])
   AND responsible_lawyer_id = COALESCE(($2::jsonb->>'responsible_lawyer_id')::uuid, responsible_lawyer_id)
   AND responsible_staff_id IS NOT DISTINCT FROM COALESCE(($2::jsonb->>'responsible_staff_id')::uuid, responsible_staff_id)
   AND retention_class = COALESCE($2::jsonb->>'retention_class', retention_class)
   AND billing_method = COALESCE($2::jsonb->>'billing_method', billing_method)) = cardinality($1::text[])`
)

// BulkChange is the change of a matter.bulk_change payload; empty fields are unchanged.
type BulkChange struct {
	ResponsibleLawyerID string `json:"responsible_lawyer_id,omitempty"`
	ResponsibleStaffID  string `json:"responsible_staff_id,omitempty"`
	RetentionClass      string `json:"retention_class,omitempty"`
	BillingMethod       string `json:"billing_method,omitempty"`
}

// BulkChangePayload is the matter.bulk_change payload.
type BulkChangePayload struct {
	MatterIDs []string   `json:"matter_ids"`
	Change    BulkChange `json:"change"`
}

// BuildBulkChange fills and checks a matter.bulk_change payload.
func BuildBulkChange(matterIDs []string, c BulkChange) (json.RawMessage, error) {
	p := BulkChangePayload{MatterIDs: matterIDs, Change: c}
	if _, err := p.check(registry.Approved{Kind: GateBulkChange}); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

func (p BulkChangePayload) check(g registry.Approved) ([]string, error) {
	all, err := ids(g, "matter_ids", p.MatterIDs)
	if err != nil {
		return nil, err
	}
	if p.Change == (BulkChange{}) {
		return nil, refuse(g, "no change")
	}
	return all, nil
}

func applyBulkChange(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p BulkChangePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	all, err := p.check(g)
	if err != nil {
		return err
	}
	change, err := json.Marshal(p.Change)
	if err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{{SQL: bulkChangeSQL, Args: []any{all, string(change), by}}},
		gates.Step{SQL: bulkChangeProofSQL, Args: []any{all, string(change)}})
}

// G-NUM: each listed matter takes the number the builder computed from the scheme. The
// number it had is kept in legacy_number (unless one is already there), so references to
// the old number still resolve.
const (
	renumberSQL = `UPDATE matter.matter m SET legacy_number = COALESCE(m.legacy_number, m.number), number = n.value,
  updated_by = $2::uuid, updated_at = now(), row_version = m.row_version + 1
  FROM jsonb_each_text($1::jsonb) n
 WHERE m.id = n.key::uuid AND m.number <> n.value`
	renumberProofSQL = `SELECT 1 WHERE (SELECT count(*) FROM matter.matter m JOIN jsonb_each_text($1::jsonb) n
 ON m.id = n.key::uuid AND m.number = n.value) = (SELECT count(*) FROM jsonb_object_keys($1::jsonb))`
)

// RenumberPayload is the matter.renumber payload: Numbers maps each matter id to its new
// number under Scheme.
type RenumberPayload struct {
	MatterIDs []string          `json:"matter_ids"`
	Scheme    string            `json:"scheme"`
	Numbers   map[string]string `json:"numbers"`
}

// BuildRenumber numbers matterIDs, in the order given, under scheme: "{yyyy}" is year and
// "{seq}" (or "{seq:N}", zero-padded to N digits) counts up from first.
func BuildRenumber(matterIDs []string, scheme string, year, first int) (json.RawMessage, error) {
	if !strings.Contains(scheme, "{seq") {
		return nil, refuse(registry.Approved{Kind: GateRenumber}, "scheme has no {seq}")
	}
	nums := make(map[string]string, len(matterIDs))
	for i, id := range matterIDs {
		n := strings.ReplaceAll(scheme, "{yyyy}", strconv.Itoa(year))
		seq := strconv.Itoa(first + i)
		if a := strings.Index(n, "{seq:"); a >= 0 {
			if b := strings.Index(n[a:], "}"); b > 0 {
				w, err := strconv.Atoi(n[a+5 : a+b])
				if err != nil || w < 1 || w > 12 {
					return nil, refuse(registry.Approved{Kind: GateRenumber}, "scheme width")
				}
				n = n[:a] + fmt.Sprintf("%0*d", w, first+i) + n[a+b+1:]
			}
		}
		nums[id] = strings.ReplaceAll(n, "{seq}", seq)
	}
	p := RenumberPayload{MatterIDs: matterIDs, Scheme: scheme, Numbers: nums}
	if err := p.check(registry.Approved{Kind: GateRenumber}); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

func (p RenumberPayload) check(g registry.Approved) error {
	all, err := ids(g, "matter_ids", p.MatterIDs)
	if err != nil {
		return err
	}
	if p.Scheme == "" {
		return refuse(g, "no scheme")
	}
	if !slices.Equal(all, slices.Sorted(maps.Keys(p.Numbers))) || len(all) != len(p.MatterIDs) {
		return refuse(g, "numbers must name each matter once")
	}
	seen := map[string]bool{}
	for _, n := range p.Numbers {
		if n == "" || seen[n] {
			return refuse(g, "numbers must be distinct and non-empty")
		}
		seen[n] = true
	}
	return nil
}

func applyRenumber(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	// rule: ARC-I-016
	// G-NUM is irreversible: two distinct people approve.
	by, err := gates.Approvers(g, 2)
	if err != nil {
		return err
	}
	var p RenumberPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := p.check(g); err != nil {
		return err
	}
	nums, err := json.Marshal(p.Numbers)
	if err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{{SQL: renumberSQL, Args: []any{string(nums), by}}},
		gates.Step{SQL: renumberProofSQL, Args: []any{string(nums)}})
}

// G-ACC: the matter's access is set as approved. The PATCH route opens it with restricted
// (true: restricted, false: open to the firm) or team (members added); the spec form names
// visibility (team: team_only, firm: firm_open) and grant_user_ids. Members are added, never
// removed, granted by the approver (who cannot grant themselves).
const (
	visibilitySQL = `UPDATE matter.matter SET visibility = $2, updated_by = $3::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND archived_at IS NULL AND visibility <> $2`
	visibilityProofSQL = `SELECT 1 FROM matter.matter WHERE id = $1::uuid AND visibility = $2`
	grantSQL           = `INSERT INTO matter.matter_member (matter_id, user_id, matter_role, granted_by)
SELECT $1::uuid, t.user_id::uuid, t.role, $3::uuid
  FROM jsonb_to_recordset($2::jsonb) AS t(user_id text, role text)
 WHERE t.user_id::uuid <> $3::uuid
   AND EXISTS (SELECT 1 FROM matter.matter m WHERE m.id = $1::uuid AND m.archived_at IS NULL)
   AND NOT EXISTS (SELECT 1 FROM matter.matter_member x
                    WHERE x.matter_id = $1::uuid AND x.user_id = t.user_id::uuid AND x.ends_on IS NULL)`
	grantProofSQL = `SELECT 1 WHERE (SELECT count(*) FROM jsonb_to_recordset($2::jsonb) AS t(user_id text, role text)
 WHERE EXISTS (SELECT 1 FROM matter.matter_member x
                WHERE x.matter_id = $1::uuid AND x.user_id = t.user_id::uuid AND x.ends_on IS NULL))
 = jsonb_array_length($2::jsonb)`
)

// AccessMember is one member a widen_access gate adds.
type AccessMember struct {
	UserID string `json:"user_id"`
	Role   string `json:"role,omitempty"`
}

// WidenAccessPayload is the matter.widen_access payload: the route form (version with
// restricted or team) or the spec form (visibility and grant_user_ids).
type WidenAccessPayload struct {
	MatterID     string         `json:"matter_id"`
	Version      int            `json:"version,omitempty"`
	Restricted   *bool          `json:"restricted,omitempty"`
	Team         []AccessMember `json:"team,omitempty"`
	Visibility   string         `json:"visibility,omitempty"`
	GrantUserIDs []string       `json:"grant_user_ids,omitempty"`
}

// BuildWidenAccess fills and checks a matter.widen_access payload.
func BuildWidenAccess(p WidenAccessPayload) (json.RawMessage, error) {
	if _, _, err := p.plan(registry.Approved{Kind: GateWidenAccess}); err != nil {
		return nil, err
	}
	return json.Marshal(p)
}

// matterRoles are the matter_member roles a team entry may name; any other is "member".
var matterRoles = []string{"responsible", "originating", "member", "staff", "reviewer"}

// plan returns the visibility to set ("" for none) and the members to add, as JSON.
func (p WidenAccessPayload) plan(g registry.Approved) (string, string, error) {
	if p.MatterID == "" {
		return "", "", refuse(g, "no matter_id")
	}
	vis := ""
	switch {
	case p.Restricted != nil && *p.Restricted:
		vis = "restricted"
	case p.Restricted != nil:
		vis = "firm_open"
	case p.Visibility == "team":
		vis = "team_only"
	case p.Visibility == "firm":
		vis = "firm_open"
	case p.Visibility != "":
		return "", "", refuse(g, "visibility "+strconv.Quote(p.Visibility))
	}
	var members []AccessMember
	for _, m := range p.Team {
		if !slices.Contains(matterRoles, m.Role) {
			m.Role = "member"
		}
		members = append(members, m)
	}
	for _, u := range p.GrantUserIDs {
		members = append(members, AccessMember{UserID: u, Role: "member"})
	}
	for _, m := range members {
		if m.UserID == "" {
			return "", "", refuse(g, "a member has no user_id")
		}
	}
	if vis == "" && len(members) == 0 {
		return "", "", refuse(g, "no access change")
	}
	if len(members) == 0 {
		return vis, "", nil
	}
	raw, err := json.Marshal(members)
	return vis, string(raw), err
}

func applyWidenAccess(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p WidenAccessPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	vis, members, err := p.plan(g)
	if err != nil {
		return err
	}
	var steps []gates.Step
	var proofs []gates.Step
	if vis != "" {
		steps = append(steps, gates.Step{SQL: visibilitySQL, Args: []any{p.MatterID, vis, by}})
		proofs = append(proofs, gates.Step{SQL: visibilityProofSQL, Args: []any{p.MatterID, vis}})
	}
	if members != "" {
		steps = append(steps, gates.Step{SQL: grantSQL, Args: []any{p.MatterID, members, by}})
		proofs = append(proofs, gates.Step{SQL: grantProofSQL, Args: []any{p.MatterID, members}})
	}
	if err := gates.Apply(ctx, tx, g, steps, proofs[0]); err != nil {
		return err
	}
	if len(proofs) == 2 {
		return gates.Apply(ctx, tx, g, nil, proofs[1])
	}
	return nil
}

// G-AT: an active matter template is applied: the matter records it, takes the template's
// preset fields where it has none of its own, gets the template's folders, and its team
// roles join the matter. The work plan follows from the matter.template_applied event
// (F-WRK-02), not from this effect. applied_template_id keys the change.
const (
	templateFoldersSQL = `INSERT INTO docs.folder (matter_id, name, from_template)
SELECT $1::uuid, f.name, true
  FROM matter.matter_template t,
       LATERAL (SELECT COALESCE(e->>'name', e#>>'{}') AS name FROM jsonb_array_elements(t.folder_structure) e) f
 WHERE t.id = $2::uuid AND t.status = 'active' AND f.name IS NOT NULL AND f.name <> ''
   AND EXISTS (SELECT 1 FROM matter.matter m WHERE m.id = $1::uuid AND m.archived_at IS NULL
                AND m.applied_template_id IS DISTINCT FROM $2::uuid)
   AND NOT EXISTS (SELECT 1 FROM docs.folder x WHERE x.matter_id = $1::uuid AND x.parent_id IS NULL
                    AND lower(x.name) = lower(f.name) AND x.archived_at IS NULL)`
	templateTeamSQL = `INSERT INTO matter.matter_member (matter_id, user_id, matter_role, granted_by)
SELECT $1::uuid, (e->>'user_id')::uuid,
       CASE WHEN e->>'matter_role' IN ('responsible','originating','member','staff','reviewer') THEN e->>'matter_role' ELSE 'member' END,
       $3::uuid
  FROM matter.matter_template t, jsonb_array_elements(t.team_roles) e
 WHERE t.id = $2::uuid AND t.status = 'active' AND e ? 'user_id' AND (e->>'user_id')::uuid <> $3::uuid
   AND EXISTS (SELECT 1 FROM matter.matter m WHERE m.id = $1::uuid AND m.archived_at IS NULL
                AND m.applied_template_id IS DISTINCT FROM $2::uuid)
   AND NOT EXISTS (SELECT 1 FROM matter.matter_member x
                    WHERE x.matter_id = $1::uuid AND x.user_id = (e->>'user_id')::uuid AND x.ends_on IS NULL)`
	templateApplySQL = `UPDATE matter.matter m SET applied_template_id = t.id, custom_fields = t.preset_fields || m.custom_fields,
  updated_by = $3::uuid, updated_at = now(), row_version = m.row_version + 1
  FROM matter.matter_template t
 WHERE m.id = $1::uuid AND t.id = $2::uuid AND t.status = 'active' AND m.archived_at IS NULL
   AND m.practice_area_id = t.practice_area_id AND m.applied_template_id IS DISTINCT FROM $2::uuid`
	templateProofSQL = `SELECT 1 FROM matter.matter WHERE id = $1::uuid AND applied_template_id = $2::uuid`
)

// ApplyTemplatePayload is the matter.apply_template payload.
type ApplyTemplatePayload struct {
	MatterID   string `json:"matter_id"`
	TemplateID string `json:"template_id"`
}

// BuildApplyTemplate fills and checks a matter.apply_template payload.
func BuildApplyTemplate(matterID, templateID string) (json.RawMessage, error) {
	if matterID == "" || templateID == "" {
		return nil, refuse(registry.Approved{Kind: GateApplyTemplate}, "no matter_id or template_id")
	}
	return json.Marshal(ApplyTemplatePayload{MatterID: matterID, TemplateID: templateID})
}

func applyTemplate(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p ApplyTemplatePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"matter_id": p.MatterID, "template_id": p.TemplateID}); err != nil {
		return err
	}
	a := []any{p.MatterID, p.TemplateID, by}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: templateFoldersSQL, Args: a},
		{SQL: templateTeamSQL, Args: a},
		{SQL: templateApplySQL, Args: a},
	}, gates.Step{SQL: templateProofSQL, Args: a[:2]})
}
