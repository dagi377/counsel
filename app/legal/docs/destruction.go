package docs

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// MatterDocuments lists a matter's live documents that may be destroyed: documents on legal
// hold are left out by the implementation.
type MatterDocuments interface {
	MatterDocuments(ctx context.Context, tx pgx.Tx, matterID string) ([]Document, error)
}

// Proposer builds file destruction proposals. It never purges anything: it only opens
// DestructionGate (G14), and the purge runs as that gate's effect once a lawyer approves it.
type Proposer struct {
	docs    MatterDocuments
	gates   DestructionGates
	classes map[string]RetentionClass // document kind -> retention class
}

var _ Destruction = (*Proposer)(nil)

// NewProposer returns a Proposer over the registered kinds and retention classes.
func NewProposer(docs MatterDocuments, gates DestructionGates) *Proposer {
	return &Proposer{docs: docs, gates: gates, classes: classByKind(documentKinds, retentionClasses)}
}

// classByKind maps each document kind to its retention class.
func classByKind(kinds []DocumentKind, classes []RetentionClass) map[string]RetentionClass {
	byID := map[string]RetentionClass{}
	for _, c := range classes {
		byID[c.ID] = c
	}
	out := map[string]RetentionClass{}
	for _, k := range kinds {
		if c, ok := byID[k.RetentionClass]; ok {
			out[k.ID] = c
		}
	}
	return out
}

// dateOnly drops the clock time so retention runs on calendar dates.
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// pastRetention returns the sorted ids of the matter's documents whose retention has run out
// on today. Never-purge classes, classes without the destruction gate and unknown kinds
// are kept.
func pastRetention(ds []Document, matterID string, classes map[string]RetentionClass, closedOn, today time.Time) []string {
	closed, now := dateOnly(closedOn), dateOnly(today)
	var ids []string
	for _, d := range ds {
		c, ok := classes[d.Kind]
		if d.MatterID != matterID || !ok {
			continue
		}
		// CF-15: RC-FIN and RC-AUDIT are never purged in v1.
		if c.NeverPurge || c.ID == RetentionFinancial || c.ID == RetentionAudit {
			continue
		}
		// CF-16: only classes destroyed through G14 are proposed.
		if c.DestructionGate != DestructionGate {
			continue
		}
		if !closed.AddDate(c.MinYears, 0, 0).After(now) {
			ids = append(ids, d.ID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// ProposeDestruction lists the closed matter's documents past retention on today and opens
// DestructionGate for them. Nothing is purged until a lawyer approves that gate.
func (p *Proposer) ProposeDestruction(ctx context.Context, tx pgx.Tx, matterID string, closedOn, today time.Time, preparer string) (Proposal, error) {
	if matterID == "" || preparer == "" {
		return Proposal{}, fmt.Errorf("propose destruction: %w", ErrInvalid)
	}
	if closedOn.IsZero() || dateOnly(closedOn).After(dateOnly(today)) {
		return Proposal{}, fmt.Errorf("propose destruction %s: %w", matterID, ErrMatterOpen)
	}
	ds, err := p.docs.MatterDocuments(ctx, tx, matterID)
	if err != nil {
		return Proposal{}, fmt.Errorf("propose destruction %s: documents: %w", matterID, err)
	}
	ids := pastRetention(ds, matterID, p.classes, closedOn, today)
	if len(ids) == 0 {
		return Proposal{}, fmt.Errorf("propose destruction %s: %w", matterID, ErrNothingToPurge)
	}
	// CF-16: auto-purge waits on G14; a lawyer approves before anything is destroyed.
	gateID, err := p.gates.OpenDestruction(ctx, tx, matterID, ids, preparer)
	if err != nil {
		return Proposal{}, fmt.Errorf("propose destruction %s: open gate: %w", matterID, err)
	}
	return Proposal{MatterID: matterID, GateID: gateID, DocumentIDs: ids, ProposedOn: today}, nil
}

// PGMatterDocuments reads a matter's documents from docs.document, skipping archived
// documents and documents on legal hold.
type PGMatterDocuments struct{}

var _ MatterDocuments = PGMatterDocuments{}

// MatterDocuments lists the matter's live documents not on legal hold.
func (PGMatterDocuments) MatterDocuments(ctx context.Context, tx pgx.Tx, matterID string) ([]Document, error) {
	rows, err := tx.Query(ctx,
		`SELECT id::text, doc_type, coalesce(folder_id::text, '') FROM docs.document
		 WHERE matter_id = $1 AND archived_at IS NULL AND NOT legal_hold`, matterID)
	if err != nil {
		return nil, fmt.Errorf("matter documents: %w", err)
	}
	defer rows.Close()
	var out []Document
	for rows.Next() {
		d := Document{MatterID: matterID}
		var folder string
		if err := rows.Scan(&d.ID, &d.Kind, &folder); err != nil {
			return nil, fmt.Errorf("matter documents: scan: %w", err)
		}
		d.FolderID = FolderID(folder)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("matter documents: %w", err)
	}
	return out, nil
}
