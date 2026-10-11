package intake

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
)

// PGForms reads intake.intake_form. A form is published when its status is published.
type PGForms struct {
	DB db.DB
}

var _ FormStore = PGForms{}

const formSQL = `SELECT id::text, name, status = 'published' FROM intake.intake_form WHERE id = $1::uuid`

// Form implements FormStore.
func (s PGForms) Form(ctx context.Context, id string) (Form, error) {
	var f Form
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, formSQL, id).Scan(&f.ID, &f.Name, &f.Published)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Form{}, ErrNotFound
	}
	if err != nil {
		return Form{}, fmt.Errorf("intake form %s: %w", id, err)
	}
	return f, nil
}

// PGLeads keeps leads in intake.lead under (form_id, idempotency_key)
// (migrations 5300 and 5301).
type PGLeads struct {
	DB db.DB
}

var _ LeadStore = PGLeads{}

// leadCols reads a lead; a converted matter makes the lead converted, else it is open.
const leadCols = `id::text, COALESCE(form_id::text, ''), contact_id::text,
  CASE WHEN converted_matter_id IS NOT NULL THEN 'converted' ELSE 'open' END,
  created_at, COALESCE(converted_matter_id::text, '')`

const leadByKeySQL = `SELECT ` + leadCols + ` FROM intake.lead
 WHERE form_id = $1::uuid AND idempotency_key = $2 AND archived_at IS NULL`

// insertLeadSQL stores the lead once per (form, key); a key already used returns no row.
const insertLeadSQL = `INSERT INTO intake.lead (form_id, contact_id, received_at, created_at, idempotency_key)
VALUES ($1::uuid, $2::uuid, $3, $3, NULLIF($4, ''))
ON CONFLICT (form_id, idempotency_key) WHERE form_id IS NOT NULL AND idempotency_key IS NOT NULL DO NOTHING
RETURNING ` + leadCols

func scanLead(row pgx.Row) (Lead, error) {
	var (
		l      Lead
		status string
		at     time.Time
	)
	if err := row.Scan(&l.ID, &l.FormID, &l.ContactID, &status, &at, &l.MatterID); err != nil {
		return Lead{}, err
	}
	l.Status, l.CreatedAt = LeadStatus(status), at
	return l, nil
}

// ByIdempotencyKey implements LeadStore.
func (s PGLeads) ByIdempotencyKey(ctx context.Context, formID, key string) (Lead, error) {
	var l Lead
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		l, err = scanLead(tx.QueryRow(ctx, leadByKeySQL, formID, key))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Lead{}, ErrNotFound
	}
	if err != nil {
		return Lead{}, fmt.Errorf("lead by key: %w", err)
	}
	return l, nil
}

// Insert implements LeadStore. A key another submission already used returns that lead.
func (s PGLeads) Insert(ctx context.Context, l Lead, key string) (Lead, error) {
	if l.FormID == "" || l.ContactID == "" || l.CreatedAt.IsZero() {
		return Lead{}, fmt.Errorf("insert lead: %w", ErrInvalid)
	}
	var out Lead
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanLead(tx.QueryRow(ctx, insertLeadSQL, l.FormID, l.ContactID, l.CreatedAt, key))
		if errors.Is(err, pgx.ErrNoRows) && key != "" {
			out, err = scanLead(tx.QueryRow(ctx, leadByKeySQL, l.FormID, key))
		}
		return err
	})
	if err != nil {
		return Lead{}, fmt.Errorf("insert lead: %w", err)
	}
	return out, nil
}
