package docs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// The document write points publish EventRevisionRequested (ai_f_doc_11.go) and
// EventCourtFormChanged (ai_f_doc_06_write.go) (spec/eng/a2-interfaces.json, Q-EVENTS-WIRE-4).

// SubjectCourtForm is the outbox subject kind of a court form version.
const SubjectCourtForm = "docs.court_form"

// ErrNoWritePoint means a write point has no database or no emitter, so its event would be
// lost; it refuses instead of committing without the event.
var ErrNoWritePoint = errors.New("docs: write point not wired")

// ErrNoRecord means the record a write names does not exist in the state the write needs.
var ErrNoRecord = errors.New("docs: record not found")

// RevisionRequested is the body of EventRevisionRequested: ids only, never the comments.
type RevisionRequested struct {
	DocumentID  string `json:"document_id"`
	VersionID   string `json:"version_id"`
	MatterID    string `json:"matter_id,omitempty"`
	RequestedBy string `json:"requested_by"`
}

// CourtFormChanged is the body of EventCourtFormChanged.
type CourtFormChanged struct {
	CourtFormID string `json:"court_form_id"`
	RuleSet     string `json:"rule_set"`
	FormNumber  string `json:"form_number"`
	VersionDate string `json:"version_date"`
}

// Revisions records a reviewer's request for changes to a document's current version.
type Revisions struct {
	DB     db.DB
	Events outbox.Emitter
}

// requestRevisionSQL sends a document back to draft, only while versionID is its current
// version, and returns its matter.
const requestRevisionSQL = `UPDATE docs.document
   SET status = 'draft', updated_by = NULLIF($3, '')::uuid, updated_at = now(),
       row_version = row_version + 1
 WHERE id = $1::uuid AND current_version_id = $2::uuid AND archived_at IS NULL
RETURNING COALESCE(matter_id::text, '')`

// Request returns document documentID to draft for changes to versionID and publishes
// EventRevisionRequested in the same tx. A document whose current version is not versionID
// is ErrNoRecord.
func (r *Revisions) Request(ctx context.Context, documentID, versionID, by string) error {
	if strings.TrimSpace(documentID) == "" || strings.TrimSpace(versionID) == "" || strings.TrimSpace(by) == "" {
		return fmt.Errorf("request revision: %w", ErrInvalid)
	}
	if r.DB == nil || r.Events == nil {
		return fmt.Errorf("request revision: %w", ErrNoWritePoint)
	}
	err := r.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ev := RevisionRequested{DocumentID: documentID, VersionID: versionID, RequestedBy: by}
		err := tx.QueryRow(ctx, requestRevisionSQL, documentID, versionID, by).Scan(&ev.MatterID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoRecord
		}
		if err != nil {
			return err
		}
		// rule: ARC-I-030
		return r.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectKind, ID: documentID}, EventRevisionRequested, ev)
	})
	if err != nil {
		return fmt.Errorf("request revision %s: %w", documentID, err)
	}
	return nil
}

const documentMatterSQL = `SELECT COALESCE(matter_id::text, '') FROM docs.document WHERE id = $1::uuid AND archived_at IS NULL`

// MatterOf returns the matter of document documentID, or empty for a firm document. A
// missing document is ErrNoRecord. The revision route checks access on this matter.
func (r *Revisions) MatterOf(ctx context.Context, documentID string) (string, error) {
	if r.DB == nil {
		return "", fmt.Errorf("document matter: %w", ErrNoWritePoint)
	}
	var matterID string
	err := r.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, documentMatterSQL, documentID).Scan(&matterID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoRecord
		}
		return err
	})
	if err != nil {
		return "", fmt.Errorf("document matter %s: %w", documentID, err)
	}
	return matterID, nil
}

// CourtFormVersion is one published version of a court form.
type CourtFormVersion struct {
	Jurisdiction string // default "ON"
	RuleSet      string
	FormNumber   string
	Title        string
	SourceURL    string
	VersionDate  time.Time // the form's version date (a calendar date)
	Checksum     []byte
	CheckedAt    time.Time
}

// CourtForms records court form versions found by the form watch.
type CourtForms struct {
	DB     db.DB
	Events outbox.Emitter
}

// recordCourtFormSQL stores a version once; a version already recorded returns no row.
const recordCourtFormSQL = `INSERT INTO docs.court_form
  (jurisdiction, rule_set, form_number, title, source_url, version_date, checksum, checked_at)
VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8)
ON CONFLICT (rule_set, form_number, version_date) DO NOTHING
RETURNING id::text`

// Record stores v and publishes EventCourtFormChanged in the same tx. It reports whether v
// was new; a version already recorded publishes nothing.
func (c *CourtForms) Record(ctx context.Context, v CourtFormVersion) (bool, error) {
	if v.RuleSet == "" || v.FormNumber == "" || v.Title == "" || v.SourceURL == "" ||
		v.VersionDate.IsZero() || len(v.Checksum) == 0 || v.CheckedAt.IsZero() {
		return false, fmt.Errorf("record court form: %w", ErrInvalid)
	}
	if c.DB == nil || c.Events == nil {
		return false, fmt.Errorf("record court form: %w", ErrNoWritePoint)
	}
	if v.Jurisdiction == "" {
		v.Jurisdiction = "ON"
	}
	day := v.VersionDate.Format(time.DateOnly)
	added := false
	err := c.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, recordCourtFormSQL, v.Jurisdiction, v.RuleSet, v.FormNumber, v.Title,
			v.SourceURL, day, v.Checksum, v.CheckedAt).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		added = true
		// rule: ARC-I-030
		body := CourtFormChanged{CourtFormID: id, RuleSet: v.RuleSet, FormNumber: v.FormNumber, VersionDate: day}
		return c.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectCourtForm, ID: id}, EventCourtFormChanged, body)
	})
	if err != nil {
		return false, fmt.Errorf("record court form %s %s: %w", v.RuleSet, v.FormNumber, err)
	}
	return added, nil
}
