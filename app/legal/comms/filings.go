package comms

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/outbox"
)

// FilingStore keeps court filings in legal.court_filing (migrations/6000_court_filing.sql).
// comms.comm_filing is a different record: it files a message to a matter.
type FilingStore struct {
	db db.DB
	// Events receives the filing event RecordFiling emits in its tx. Boot sets it; while nil,
	// a filed filing is refused (its receipt would never reach the deadline matcher).
	Events outbox.Emitter
}

// EventFilingReceiptReceived is the outbox event type of a filing recorded as filed: the
// court's e-filing receipt is in. F-DL-04 (app/legal/deadlines) starts on it.
const EventFilingReceiptReceived = "filing.receipt_received"

// SubjectCourtFiling is the outbox subject kind of a court filing.
const SubjectCourtFiling = "legal.court_filing"

// FilingReceipt is the body of EventFilingReceiptReceived: ids and the filed date only.
type FilingReceipt struct {
	FilingID   string `json:"filing_id"`
	MatterID   string `json:"matter_id"`
	DocumentID string `json:"document_id,omitempty"`
	FiledOn    string `json:"filed_on"`
}

// EventClaimIssued is the outbox event type IssueClaim emits when the court issues a filed
// claim. EventAffidavitOfServiceReceived is the type RecordAffidavitOfService emits. F-DL-04
// (app/legal/deadlines) starts on both.
const (
	EventClaimIssued                = "claim.issued"
	EventAffidavitOfServiceReceived = "service.affidavit_received"
)

// ClaimIssued is the body of EventClaimIssued: ids, the court file number and the date.
type ClaimIssued struct {
	FilingID    string `json:"filing_id"`
	MatterID    string `json:"matter_id"`
	DocumentID  string `json:"document_id,omitempty"`
	CourtFileNo string `json:"court_file_no"`
	IssuedOn    string `json:"issued_on"`
}

// filingEvent returns the receipt event of a recorded filing, or ok false when the filing
// is not filed (a draft or a rejection has no receipt).
func filingEvent(f Filing, filedOn *clock.Date) (FilingReceipt, bool) {
	if f.Status != FilingFiled || filedOn == nil {
		return FilingReceipt{}, false
	}
	return FilingReceipt{FilingID: f.ID, MatterID: f.MatterID, DocumentID: f.DocumentID, FiledOn: filedOn.String()}, true
}

var _ Filings = (*FilingStore)(nil)

// NewFilingStore returns a store on pool.
func NewFilingStore(pool db.DB) *FilingStore { return &FilingStore{db: pool} }

// filingRow is a validated filing ready to insert. FiledOn is a business date, or nil.
type filingRow struct {
	MatterID   string
	DocumentID string
	Court      string
	Reference  string
	Status     FilingStatus
	FiledBy    string
	FiledOn    *clock.Date
}

// normalizeFiling checks f and turns it into a row. An empty status is a draft. A filed
// filing needs the date and the user who filed it, as the table's CHECK does. FiledOn is
// read as a business date in loc.
func normalizeFiling(f Filing, loc *time.Location) (filingRow, error) {
	r := filingRow{
		MatterID:   strings.TrimSpace(f.MatterID),
		DocumentID: strings.TrimSpace(f.DocumentID),
		Court:      strings.TrimSpace(f.Court),
		Reference:  strings.TrimSpace(f.Reference),
		Status:     f.Status,
		FiledBy:    strings.TrimSpace(f.FiledBy),
	}
	if r.MatterID == "" {
		return filingRow{}, fmt.Errorf("filing: matter: %w", ErrInvalid)
	}
	if r.Court == "" {
		return filingRow{}, fmt.Errorf("filing: court: %w", ErrInvalid)
	}
	if r.Status == "" {
		r.Status = FilingDraft
	}
	switch r.Status {
	case FilingDraft, FilingFiled, FilingRejected:
	default:
		return filingRow{}, fmt.Errorf("filing: status %q: %w", r.Status, ErrInvalid)
	}
	if !f.FiledOn.IsZero() {
		d := clock.DateOf(f.FiledOn, loc)
		r.FiledOn = &d
	}
	if r.Status == FilingFiled && (r.FiledOn == nil || r.FiledBy == "") {
		return filingRow{}, fmt.Errorf("filing: filed needs filed_on and filed_by: %w", ErrInvalid)
	}
	return r, nil
}

// nullable returns nil for an empty string so the column stays NULL.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RecordFiling appends f and returns it with ID and RecordedAt set. A filed filing emits
// EventFilingReceiptReceived in the same tx.
func (s *FilingStore) RecordFiling(ctx context.Context, f Filing) (Filing, error) {
	return s.record(ctx, "record filing", f, EventFilingReceiptReceived)
}

// RecordAffidavitOfService appends f, a filed affidavit of service (its document is the
// affidavit), and emits EventAffidavitOfServiceReceived in the same tx instead of the
// plain receipt event, so the deadline matcher runs once, on the more specific evidence.
func (s *FilingStore) RecordAffidavitOfService(ctx context.Context, f Filing) (Filing, error) {
	if f.Status != FilingFiled || strings.TrimSpace(f.DocumentID) == "" {
		return Filing{}, fmt.Errorf("record affidavit of service: needs a filed filing with its document: %w", ErrInvalid)
	}
	return s.record(ctx, "record affidavit of service", f, EventAffidavitOfServiceReceived)
}

// record appends f and, when it is filed, emits event typ in the same tx.
func (s *FilingStore) record(ctx context.Context, op string, f Filing, typ string) (Filing, error) {
	c, err := clock.From(ctx)
	if err != nil {
		return Filing{}, fmt.Errorf("%s: %w", op, err)
	}
	r, err := normalizeFiling(f, c.Location())
	if err != nil {
		return Filing{}, fmt.Errorf("%s: %w", op, err)
	}
	if r.Status == FilingFiled && s.Events == nil {
		return Filing{}, fmt.Errorf("%s: no event emitter: %w", op, ErrInvalid)
	}
	var filedOn any
	if r.FiledOn != nil {
		filedOn = r.FiledOn.String()
	}
	out := Filing{
		MatterID: r.MatterID, DocumentID: r.DocumentID, Court: r.Court, Reference: r.Reference,
		Status: r.Status, FiledBy: r.FiledBy,
	}
	if r.FiledOn != nil {
		out.FiledOn = r.FiledOn.Start(c.Location())
	}
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO legal.court_filing
			   (matter_id, document_id, court, reference, status, filed_by, filed_on)
			 VALUES ($1, $2, $3, $4, $5, $6, $7::date)
			 RETURNING id::text, created_at`,
			r.MatterID, nullable(r.DocumentID), r.Court, nullable(r.Reference), string(r.Status),
			nullable(r.FiledBy), filedOn).Scan(&out.ID, &out.RecordedAt); err != nil {
			return err
		}
		// The receipt event commits with the filing row.
		if ev, ok := filingEvent(out, r.FiledOn); ok {
			return s.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectCourtFiling, ID: out.ID}, typ, ev)
		}
		return nil
	})
	if err != nil {
		return Filing{}, fmt.Errorf("%s: %w", op, err)
	}
	return out, nil
}

// IssueClaim records that the court issued the claim filed as filing id: the filing gets
// the court file number, and EventClaimIssued is emitted in the same tx. Only a filed
// filing can be issued; a missing, unfiled or unseen filing is ErrNotFound.
func (s *FilingStore) IssueClaim(ctx context.Context, id, courtFileNo string, issuedOn time.Time) (Filing, error) {
	id, courtFileNo = strings.TrimSpace(id), strings.TrimSpace(courtFileNo)
	if id == "" || courtFileNo == "" || issuedOn.IsZero() {
		return Filing{}, fmt.Errorf("issue claim: needs filing, court file number and date: %w", ErrInvalid)
	}
	if s.Events == nil {
		return Filing{}, fmt.Errorf("issue claim: no event emitter: %w", ErrInvalid)
	}
	c, err := clock.From(ctx)
	if err != nil {
		return Filing{}, fmt.Errorf("issue claim: %w", err)
	}
	on := clock.DateOf(issuedOn, c.Location())
	var out Filing
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var filedOn string
		err := tx.QueryRow(ctx,
			`UPDATE legal.court_filing SET reference = $2
			  WHERE id = $1 AND status = 'filed' AND archived_at IS NULL
			  RETURNING id::text, matter_id::text, coalesce(document_id::text, ''), court,
			            status, coalesce(filed_by::text, ''), to_char(filed_on, 'YYYY-MM-DD'), created_at`,
			id, courtFileNo).Scan(&out.ID, &out.MatterID, &out.DocumentID, &out.Court, &out.Status,
			&out.FiledBy, &filedOn, &out.RecordedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out.Reference = courtFileNo
		d, err := time.ParseInLocation(time.DateOnly, filedOn, c.Location())
		if err != nil {
			return fmt.Errorf("filed_on: %w", err)
		}
		out.FiledOn = d
		// The issued event commits with the court file number.
		return s.Events.Emit(ctx, tx, outbox.Subject{Kind: SubjectCourtFiling, ID: out.ID}, EventClaimIssued, claimIssuedEvent(out, on))
	})
	if err != nil {
		return Filing{}, fmt.Errorf("issue claim %s: %w", id, err)
	}
	return out, nil
}

// claimIssuedEvent is the body of EventClaimIssued for issued filing f.
func claimIssuedEvent(f Filing, on clock.Date) ClaimIssued {
	return ClaimIssued{FilingID: f.ID, MatterID: f.MatterID, DocumentID: f.DocumentID,
		CourtFileNo: f.Reference, IssuedOn: on.String()}
}

// FilingsFor lists a matter's live filings, oldest first. A matter with none, or one the
// session cannot see (row-level security), returns an empty list.
func (s *FilingStore) FilingsFor(ctx context.Context, matterID string) ([]Filing, error) {
	matterID = strings.TrimSpace(matterID)
	if matterID == "" {
		return nil, fmt.Errorf("filings: matter: %w", ErrInvalid)
	}
	c, err := clock.From(ctx)
	if err != nil {
		return nil, fmt.Errorf("filings: %w", err)
	}
	var out []Filing
	err = s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id::text, matter_id::text, coalesce(document_id::text, ''), court,
			        coalesce(reference, ''), status, coalesce(filed_by::text, ''),
			        coalesce(to_char(filed_on, 'YYYY-MM-DD'), ''), created_at
			   FROM legal.court_filing
			  WHERE matter_id = $1 AND archived_at IS NULL
			  ORDER BY created_at, id`, matterID)
		if err != nil {
			return fmt.Errorf("query: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var f Filing
			var status, filedOn string
			if err := rows.Scan(&f.ID, &f.MatterID, &f.DocumentID, &f.Court, &f.Reference,
				&status, &f.FiledBy, &filedOn, &f.RecordedAt); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			f.Status = FilingStatus(status)
			if filedOn != "" {
				d, err := time.ParseInLocation(time.DateOnly, filedOn, c.Location())
				if err != nil {
					return fmt.Errorf("filed_on: %w", err)
				}
				f.FiledOn = d
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("filings: %w", err)
	}
	return out, nil
}
