package docs

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/registry"
)

// GateEffects are the approval effects of the document gate kinds. Each runs in the deciding
// tx, refuses an agent approver or the preparer, and is idempotent per gate.
func GateEffects() map[string]registry.EffectFunc {
	return map[string]registry.EffectFunc{
		GateEngagementLetter: gates.Effect(applyEngagementLetter),
		GateOutgoing:         gates.Effect(applyOutgoing),
		GateFilingPrivilege:  gates.Effect(applyFilingPrivilege),
		GateTemplatePublish:  gates.Effect(applyTemplatePublish),
		GatePrecedent:        gates.Effect(applyPrecedent),
	}
}

// G5: the matter's draft engagement letter on the approved document version is approved,
// stamped with the gate and the approved version's hash.
const (
	engagementSQL = `UPDATE docs.engagement_letter l SET status = 'approved', approval_id = $3::uuid, approved_hash = v.sha256
  FROM docs.document_version v
 WHERE l.matter_id = $1::uuid AND l.document_version_id = $2::uuid AND v.id = l.document_version_id
   AND l.status = 'draft'`
	engagementProofSQL = `SELECT 1 FROM docs.engagement_letter
 WHERE matter_id = $1::uuid AND document_version_id = $2::uuid AND approval_id = $3::uuid`
)

type engagementPayload struct {
	MatterID          string `json:"matter_id"`
	DocumentVersionID string `json:"document_version_id"`
}

func applyEngagementLetter(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	if _, err := gates.Approver(g); err != nil {
		return err
	}
	var p engagementPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"matter_id": p.MatterID, "document_version_id": p.DocumentVersionID}); err != nil {
		return err
	}
	args := []any{p.MatterID, p.DocumentVersionID, g.GateID}
	return gates.Apply(ctx, tx, g, []gates.Step{{SQL: engagementSQL, Args: args}},
		gates.Step{SQL: engagementProofSQL, Args: args})
}

// G8: each approved document version is released: its document, on the matter and still at
// that version, becomes final and may leave the firm by the approved channel. Every version
// must be released, so a document edited since the review fails the whole approval.
const (
	outgoingSQL = `UPDATE docs.document d SET status = 'final', updated_by = $3::uuid, updated_at = now(),
  row_version = row_version + 1
  FROM docs.document_version v
 WHERE v.id = ANY($2::uuid[]) AND d.id = v.document_id AND d.matter_id = $1::uuid
   AND d.current_version_id = v.id AND d.status = 'draft'`
	outgoingProofSQL = `SELECT 1 WHERE cardinality($2::uuid[]) = (
  SELECT count(*) FROM docs.document d JOIN docs.document_version v ON v.document_id = d.id
   WHERE v.id = ANY($2::uuid[]) AND d.matter_id = $1::uuid AND d.current_version_id = v.id
     AND d.status IN ('final','sent'))`
)

type outgoingPayload struct {
	MatterID           string   `json:"matter_id"`
	DocumentVersionIDs []string `json:"document_version_ids"`
	Channel            string   `json:"channel"`
}

func applyOutgoing(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p outgoingPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"matter_id": p.MatterID, "channel": p.Channel}); err != nil {
		return err
	}
	if len(p.DocumentVersionIDs) == 0 {
		return fmt.Errorf("%s gate %s payload: no document versions: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: outgoingSQL, Args: []any{p.MatterID, p.DocumentVersionIDs, by}},
	}, gates.Step{SQL: outgoingProofSQL, Args: []any{p.MatterID, p.DocumentVersionIDs}})
}

// G-D1: the filed document's privilege is set as approved: privileged, or confidential when
// the reviewer lifts privilege.
const (
	privilegeSQL = `UPDATE docs.document SET privilege = $2, updated_by = $3::uuid, updated_at = now(),
  row_version = row_version + 1
WHERE id = $1::uuid AND privilege <> $2 AND archived_at IS NULL`
	privilegeProofSQL = `SELECT 1 FROM docs.document WHERE id = $1::uuid AND privilege = $2 AND archived_at IS NULL`
)

type privilegePayload struct {
	DocumentID string `json:"document_id"`
	Privileged *bool  `json:"privileged"`
}

func applyFilingPrivilege(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p privilegePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"document_id": p.DocumentID}); err != nil {
		return err
	}
	if p.Privileged == nil {
		return fmt.Errorf("%s gate %s payload: no privileged: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	level := "confidential"
	if *p.Privileged {
		level = "privileged"
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: privilegeSQL, Args: []any{p.DocumentID, level, by}},
	}, gates.Step{SQL: privilegeProofSQL, Args: []any{p.DocumentID, level}})
}

// G-D2: the approved template version is signed off by the approver and the template is
// approved for use.
const (
	templateVersionSQL = `UPDATE docs.doc_template_version SET approved_by = $3::uuid, approved_at = now()
WHERE template_id = $1::uuid AND number = $2 AND approved_by IS NULL`
	templateSQL = `UPDATE docs.doc_template SET status = 'approved'
WHERE id = $1::uuid AND status = 'draft'
  AND EXISTS (SELECT 1 FROM docs.doc_template_version v WHERE v.template_id = $1::uuid AND v.number = $2 AND v.approved_by = $3::uuid)`
	templateProofSQL = `SELECT 1 FROM docs.doc_template t JOIN docs.doc_template_version v ON v.template_id = t.id
 WHERE t.id = $1::uuid AND t.status = 'approved' AND v.number = $2 AND v.approved_by = $3::uuid`
)

type templatePayload struct {
	TemplateID string `json:"template_id"`
	Version    int    `json:"version"`
}

func applyTemplatePublish(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p templatePayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"template_id": p.TemplateID}); err != nil {
		return err
	}
	if p.Version < 1 {
		return fmt.Errorf("%s gate %s payload: no version: %w", g.Kind, g.GateID, gates.ErrNotApplied)
	}
	args := []any{p.TemplateID, p.Version, by}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: templateVersionSQL, Args: args},
		{SQL: templateSQL, Args: args},
	}, gates.Step{SQL: templateProofSQL, Args: args})
}

// G-D3: the proposed precedent drawn from the approved version's document is approved as
// current by the approver.
const (
	precedentSQL = `UPDATE docs.precedent p SET status = 'approved', approved_by = $2::uuid
  FROM docs.document_version v
 WHERE v.id = $1::uuid AND p.source_document_id = v.document_id AND p.status = 'proposed'`
	precedentProofSQL = `SELECT 1 FROM docs.precedent p JOIN docs.document_version v ON p.source_document_id = v.document_id
 WHERE v.id = $1::uuid AND p.status = 'approved' AND p.approved_by = $2::uuid`
)

type precedentPayload struct {
	DocumentVersionID string `json:"document_version_id"`
}

func applyPrecedent(ctx context.Context, tx gates.Execer, g registry.Approved) error {
	// rule: ARC-I-019
	by, err := gates.Approver(g)
	if err != nil {
		return err
	}
	var p precedentPayload
	if err := gates.Decode(g, &p); err != nil {
		return err
	}
	if err := gates.Need(g, map[string]string{"document_version_id": p.DocumentVersionID}); err != nil {
		return err
	}
	return gates.Apply(ctx, tx, g, []gates.Step{
		{SQL: precedentSQL, Args: []any{p.DocumentVersionID, by}},
	}, gates.Step{SQL: precedentProofSQL, Args: []any{p.DocumentVersionID, by}})
}
