package conflict

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateEffects are the approval effects of the conflict gate kinds. Each runs in the deciding
// tx, refuses an agent approver or the preparer, and is idempotent per gate. files is the
// document bucket (the docs storage port) that ID destruction purges stored copies from;
// while nil, a destruction that has a stored copy to purge fails closed.
func GateEffects(files docs.ObjectDeleter) map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		GateClearance:     gates.Effect(applyClearance),
		GateConsentText:   gates.Effect(applyConsentText),
		GateWall:          gates.Effect(applyWall),
		GateLateral:       gates.Effect(applyLateral),
		GateClientID:      gates.Effect(applyClientID),
		GateIDDestruction: IDDestructionEffect(files),
	}
}

// G2: the check is cleared on its latest complete run, and a clearance signed by the approver
// records the gate. A run with a hit still undisposed or disposed as a conflict is not cleared.
const (
	clearanceSQL = `INSERT INTO conflict.clearance (check_id, run_id, outcome, rationale, approval_id, signed_by, signed_at)
SELECT c.id, r.id, 'clear', 'Cleared under approval gate ' || $3::text, $3::uuid, $2::uuid, now()
  FROM conflict.conflict_check c
  JOIN LATERAL (SELECT id FROM conflict.check_run WHERE check_id = c.id AND complete
                 ORDER BY version DESC LIMIT 1) r ON true
 WHERE c.id = $1::uuid AND c.status = 'review'
   AND NOT EXISTS (SELECT 1 FROM conflict.hit h WHERE h.run_id = r.id
                    AND (h.disposition IS NULL OR h.disposition = 'conflict'))
   AND NOT EXISTS (SELECT 1 FROM conflict.clearance x WHERE x.approval_id = $3::uuid)`
	clearanceCheckSQL = `UPDATE conflict.conflict_check SET status = 'cleared', updated_by = $2::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND status = 'review'
  AND EXISTS (SELECT 1 FROM conflict.clearance x WHERE x.check_id = $1::uuid AND x.approval_id = $3::uuid)`
	clearanceProofSQL = `SELECT 1 FROM conflict.clearance x JOIN conflict.conflict_check c ON c.id = x.check_id
 WHERE x.check_id = $1::uuid AND x.approval_id = $2::uuid AND c.status = 'cleared'`
)

type clearancePayload struct {
	CheckID string `json:"check_id"`
}

func applyClearance(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p clearancePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"check_id": p.CheckID}); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: clearanceSQL, Args: []any{p.CheckID, by, g.GateID}},
		{SQL: clearanceCheckSQL, Args: []any{p.CheckID, by, g.GateID}},
	}, gates.Step{SQL: clearanceProofSQL, Args: []any{p.CheckID, g.GateID}})
}

// G2a: the consent letter's approved version is final. A document whose current version is
// not the approved one is refused, so only the text that was read is released.
const (
	consentTextSQL = `UPDATE docs.document d SET status = 'final', updated_by = $2::uuid, updated_at = now(),
  row_version = row_version + 1
  FROM docs.document_version v
 WHERE v.id = $1::uuid AND d.id = v.document_id AND d.current_version_id = v.id AND d.status = 'draft'`
	consentTextProofSQL = `SELECT 1 FROM docs.document d JOIN docs.document_version v ON v.document_id = d.id
 WHERE v.id = $1::uuid AND d.current_version_id = v.id AND d.status = 'final'`
)

type consentTextPayload struct {
	CheckID           string `json:"check_id"`
	DocumentVersionID string `json:"document_version_id"`
}

func applyConsentText(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p consentTextPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"check_id": p.CheckID, "document_version_id": p.DocumentVersionID}); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: consentTextSQL, Args: []any{p.DocumentVersionID, by}},
	}, gates.Step{SQL: consentTextProofSQL, Args: []any{p.DocumentVersionID}})
}

// G2b: a proposed wall is activated on the matter with the screened users, or an active wall
// is lifted. The wall row records the gate as its approval.
const (
	wallActivateSQL = `UPDATE matter.ethical_wall SET status = 'active', effective_from = now(), approval_id = $2::uuid
WHERE id = $1::uuid AND status = 'proposed'`
	wallMatterSQL = `INSERT INTO matter.ethical_wall_matter (wall_id, matter_id)
SELECT w.id, $2::uuid FROM matter.ethical_wall w WHERE w.id = $1::uuid AND w.approval_id = $3::uuid
ON CONFLICT DO NOTHING`
	wallUsersSQL = `INSERT INTO matter.ethical_wall_screened_user (wall_id, user_id)
SELECT w.id, u FROM matter.ethical_wall w, unnest($2::uuid[]) AS u WHERE w.id = $1::uuid AND w.approval_id = $3::uuid
ON CONFLICT DO NOTHING`
	wallLiftSQL = `UPDATE matter.ethical_wall SET status = 'lifted', lifted_at = now(), approval_id = $2::uuid
WHERE id = $1::uuid AND status = 'active'`
	wallProofSQL = `SELECT 1 FROM matter.ethical_wall WHERE id = $1::uuid AND approval_id = $2::uuid AND status = $3`
)

type wallPayload struct {
	WallID          string   `json:"wall_id"`
	MatterID        string   `json:"matter_id"`
	ScreenedUserIDs []string `json:"screened_user_ids"`
	Action          string   `json:"action"`
}

func applyWall(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	if _, err := gates.Approver(g); err != nil {
		return err
	}
	var p wallPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"wall_id": p.WallID, "matter_id": p.MatterID, "action": p.Action}); err != nil {
		return err
	}
	switch p.Action {
	case "activate":
		if len(p.ScreenedUserIDs) == 0 {
			return fmt.Errorf("%s gate %s payload: no screened users: %w", g.Kind, g.GateID, gates.ErrNotApplied)
		}
		return gates.Apply(ctx, tx, g, []gates.Step{
			{SQL: wallActivateSQL, Args: []any{p.WallID, g.GateID}},
			{SQL: wallMatterSQL, Args: []any{p.WallID, p.MatterID, g.GateID}},
			{SQL: wallUsersSQL, Args: []any{p.WallID, p.ScreenedUserIDs, g.GateID}},
		}, gates.Step{SQL: wallProofSQL, Args: []any{p.WallID, g.GateID, "active"}})
	case "lift":
		return gates.Apply(ctx, tx, g, []gates.Step{
			{SQL: wallLiftSQL, Args: []any{p.WallID, g.GateID}},
		}, gates.Step{SQL: wallProofSQL, Args: []any{p.WallID, g.GateID, "lifted"}})
	default:
		return fmt.Errorf("%s gate %s payload: action %q: %w", g.Kind, g.GateID, p.Action, gates.ErrNotApplied)
	}
}

// G2c: the lateral hire's disclosure is cleared by the approver and the lateral check is
// cleared. The hire never clears their own disclosure (the table refuses it as well).
const (
	lateralSQL = `UPDATE conflict.lateral_disclosure SET cleared_by = $2::uuid, cleared_at = now()
WHERE user_id = $1::uuid AND cleared_at IS NULL`
	lateralCheckSQL = `UPDATE conflict.conflict_check SET status = 'cleared', updated_by = $2::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND trigger = 'lateral_hire' AND status = 'review'`
	lateralProofSQL = `SELECT 1 FROM conflict.conflict_check c
 WHERE c.id = $2::uuid AND c.trigger = 'lateral_hire' AND c.status = 'cleared'
   AND EXISTS (SELECT 1 FROM conflict.lateral_disclosure l WHERE l.user_id = $1::uuid AND l.cleared_at IS NOT NULL)
   AND NOT EXISTS (SELECT 1 FROM conflict.lateral_disclosure l WHERE l.user_id = $1::uuid AND l.cleared_at IS NULL)`
)

type lateralPayload struct {
	UserID  string `json:"user_id"`
	CheckID string `json:"check_id"`
}

func applyLateral(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p lateralPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"user_id": p.UserID, "check_id": p.CheckID}); err != nil {
		return err
	}
	if p.UserID == by {
		return fmt.Errorf("%s gate %s: the lateral hire clears their own disclosure: %w", g.Kind, g.GateID, gates.ErrPreparerApproves)
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: lateralSQL, Args: []any{p.UserID, by}},
		{SQL: lateralCheckSQL, Args: []any{p.CheckID, by}},
	}, gates.Step{SQL: lateralProofSQL, Args: []any{p.UserID, p.CheckID}})
}

// G4: the client's open identity record is verified by the approver on the approved date
// (By-law 7.1). With no record yet one is written verified; exempt records the method as
// the exemption reason.
const (
	clientIDUpdateSQL = `UPDATE party.identity_record SET status = 'verified', verified_by = $2::uuid, verified_on = $3::date,
  obligation = CASE WHEN $4 THEN 'exempt' ELSE obligation END,
  exemption_reason = CASE WHEN $4 THEN $5 ELSE exemption_reason END,
  updated_by = $2::uuid, updated_at = now(), row_version = row_version + 1
WHERE contact_id = $1::uuid AND subject_kind = 'client' AND status = 'open'`
	clientIDInsertSQL = `INSERT INTO party.identity_record (contact_id, subject_kind, obligation, exemption_reason, status,
  verified_by, verified_on, created_by, updated_by)
SELECT $1::uuid, 'client', CASE WHEN $4 THEN 'exempt' ELSE 'identify_and_verify' END,
       CASE WHEN $4 THEN $5 END, 'verified', $2::uuid, $3::date, $2::uuid, $2::uuid
 WHERE NOT EXISTS (SELECT 1 FROM party.identity_record r
                    WHERE r.contact_id = $1::uuid AND r.subject_kind = 'client'
                      AND r.verified_by = $2::uuid AND r.verified_on = $3::date)`
	clientIDProofSQL = `SELECT 1 FROM party.identity_record
 WHERE contact_id = $1::uuid AND subject_kind = 'client' AND status = 'verified'
   AND verified_by = $2::uuid AND verified_on = $3::date`
)

type clientIDPayload struct {
	ContactID  string `json:"contact_id"`
	Method     string `json:"method"`
	VerifiedOn string `json:"verified_on"`
	Exempt     bool   `json:"exempt"`
}

func applyClientID(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p clientIDPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"contact_id": p.ContactID, "method": p.Method, "verified_on": p.VerifiedOn}); err != nil {
		return err
	}
	args := []any{p.ContactID, by, p.VerifiedOn, p.Exempt, p.Method}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: clientIDUpdateSQL, Args: args},
		{SQL: clientIDInsertSQL, Args: args},
	}, gates.Step{SQL: clientIDProofSQL, Args: []any{p.ContactID, by, p.VerifiedOn}})
}

// G14: ID verification past its retention is destroyed. The artefacts go: the stored copy
// behind each ID document's file_ref is deleted from the document bucket (and the purge
// recorded on the document), then each document's number, place, expiry, file reference and
// vendor reference are redacted, the record's source-of-funds note is cleared, and both are
// stamped destroyed_at. The identity_record row itself is never deleted, so the conflict-check
// history (who was identified, when, by whom) remains. Every record must belong to the
// contact and be past retention_until; one that is not leaves the gate unapplied. Two
// distinct people approve.
const (
	// idFilesSQL lists the stored copies still to purge, as JSON text.
	idFilesSQL = `SELECT COALESCE(json_agg(json_build_object('id', d.id, 'file_ref', d.file_ref) ORDER BY d.id), '[]'::json)::text
  FROM party.id_document d JOIN party.identity_record r ON r.id = d.identity_record_id
 WHERE r.id = ANY($1::text[]::uuid[]) AND r.contact_id = $2::uuid
   AND r.retention_until < ` + todaySQL + ` AND r.destroyed_at IS NULL AND d.destroyed_at IS NULL
   AND d.file_ref IS NOT NULL AND d.file_ref <> ''`
	idFilePurgedSQL = `UPDATE party.id_document SET file_purged_at = now(), file_purge_approval_id = $2::uuid
WHERE id = ANY($1::text[]::uuid[]) AND file_purged_at IS NULL AND destroyed_at IS NULL`
	idDocDestroySQL = `UPDATE party.id_document d SET number_ciphertext = '\x'::bytea, number_last4 = NULL,
  issuer_place = NULL, expires_on = NULL, file_ref = NULL, auth_vendor_ref = NULL, destroyed_at = now()
  FROM party.identity_record r
 WHERE d.identity_record_id = r.id AND r.id = ANY($1::text[]::uuid[]) AND r.contact_id = $2::uuid
   AND r.retention_until < ` + todaySQL + ` AND r.destroyed_at IS NULL AND d.destroyed_at IS NULL`
	idRecordDestroySQL = `UPDATE party.identity_record SET source_of_funds = NULL, destroyed_at = now(),
  destroyed_by = $3::uuid, destruction_approval_id = $4::uuid, updated_by = $3::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = ANY($1::text[]::uuid[]) AND contact_id = $2::uuid AND retention_until < ` + todaySQL + `
  AND destroyed_at IS NULL`
	idDestroyProofSQL = `SELECT 1 WHERE (SELECT count(*) FROM party.identity_record r
 WHERE r.id = ANY($1::text[]::uuid[]) AND r.contact_id = $2::uuid AND r.destroyed_at IS NOT NULL
   AND NOT EXISTS (SELECT 1 FROM party.id_document d WHERE d.identity_record_id = r.id
                    AND (d.destroyed_at IS NULL OR d.file_ref IS NOT NULL))
) = cardinality($1::text[])`
)

// todaySQL is today's date in the firm's time zone.
const todaySQL = `(now() AT TIME ZONE 'America/Toronto')::date`

type idDestructionPayload struct {
	ContactID string   `json:"contact_id"`
	RecordIDs []string `json:"record_ids"`
}

// idDestructionTx is the part of pgx.Tx the ID destruction effect uses.
type idDestructionTx interface {
	gates.Execer
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// storedFile is an ID document copy still in the document bucket.
type storedFile struct {
	ID      string `json:"id"`
	FileRef string `json:"file_ref"`
}

// IDDestructionEffect is the conflict.id_destruction approval effect; files is the document
// bucket it purges the stored copies from.
func IDDestructionEffect(files docs.ObjectDeleter) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		return applyIDDestruction(ctx, tx, files, g)
	}
}

func applyIDDestruction(ctx context.Context, tx idDestructionTx, files docs.ObjectDeleter, g registry.Approved) error {
	// rule: ARC-I-019
	// rule: ARC-I-016
	// G14 is irreversible: two distinct people approve, neither the preparer nor an agent.
	by, err := gates.Approvers(g, 2)
	if err != nil {
		return err
	}
	var p idDestructionPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"contact_id": p.ContactID}); err != nil {
		return err
	}
	ids := slices.Compact(slices.Sorted(slices.Values(p.RecordIDs)))
	if len(ids) == 0 || ids[0] == "" {
		return fmt.Errorf("%s gate %s payload: no record_ids: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	if err := purgeIDFiles(ctx, tx, files, g, ids, p.ContactID); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: idDocDestroySQL, Args: []any{ids, p.ContactID}},
		{SQL: idRecordDestroySQL, Args: []any{ids, p.ContactID, by, g.GateID}},
	}, gates.Step{SQL: idDestroyProofSQL, Args: []any{ids, p.ContactID}})
}

// purgeIDFiles deletes the stored copy behind every file_ref still held by the records'
// documents from the document bucket and records each purge on its document. A delete of a
// copy already gone succeeds, and a re-run after the redaction finds no file_ref, so the purge
// is idempotent. A failed delete fails the effect, so nothing is redacted while a copy may
// remain. The key is never put in an error or a log.
func purgeIDFiles(ctx context.Context, tx idDestructionTx, files docs.ObjectDeleter, g registry.Approved, ids []string, contactID string) error {
	var raw string
	if err := tx.QueryRow(ctx, idFilesSQL, ids, contactID).Scan(&raw); err != nil {
		return fmt.Errorf("%s gate %s stored copies: %w", g.Kind, g.GateID, err)
	}
	var stored []storedFile
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return fmt.Errorf("%s gate %s stored copies: %w", g.Kind, g.GateID, err)
	}
	if len(stored) == 0 {
		return nil
	}
	if files == nil {
		return fmt.Errorf("%s gate %s: %d stored copies to purge and no document bucket: %w", g.Kind, g.GateID, len(stored), gates.ErrNotApplied)
	}
	purged := make([]string, 0, len(stored))
	for _, f := range stored {
		if err := files.DeleteObject(ctx, f.FileRef); err != nil {
			// The bucket error may name the key; only the document id is reported.
			return fmt.Errorf("%s gate %s: purge stored copy of id document %s failed: %w", g.Kind, g.GateID, f.ID, gates.ErrNotApplied)
		}
		purged = append(purged, f.ID)
	}
	if _, err := tx.Exec(ctx, idFilePurgedSQL, purged, g.GateID); err != nil {
		return fmt.Errorf("%s gate %s record purge: %w", g.Kind, g.GateID, err)
	}
	return nil
}
