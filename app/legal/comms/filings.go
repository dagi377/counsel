package comms

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
)

// FilingStore keeps court filings in legal.court_filing (migrations/6000_court_filing.sql).
// comms.comm_filing is a different record: it files a message to a matter.
type FilingStore struct {
	db db.DB
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

// RecordFiling appends f and returns it with ID and RecordedAt set.
func (s *FilingStore) RecordFiling(ctx context.Context, f Filing) (Filing, error) {
	c, err := clock.From(ctx)
	if err != nil {
		return Filing{}, fmt.Errorf("record filing: %w", err)
	}
	r, err := normalizeFiling(f, c.Location())
	if err != nil {
		return Filing{}, fmt.Errorf("record filing: %w", err)
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
		return tx.QueryRow(ctx,
			`INSERT INTO legal.court_filing
			   (matter_id, document_id, court, reference, status, filed_by, filed_on)
			 VALUES ($1, $2, $3, $4, $5, $6, $7::date)
			 RETURNING id::text, created_at`,
			r.MatterID, nullable(r.DocumentID), r.Court, nullable(r.Reference), string(r.Status),
			nullable(r.FiledBy), filedOn).Scan(&out.ID, &out.RecordedAt)
	})
	if err != nil {
		return Filing{}, fmt.Errorf("record filing: %w", err)
	}
	return out, nil
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
