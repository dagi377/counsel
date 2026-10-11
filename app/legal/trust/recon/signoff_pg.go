package recon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is the part of a pgx pool or connection PGSignOff reads with (db.Pool satisfies it).
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// SignOffRecord is the sign-off persisted on a trust.reconciliation row.
type SignOffRecord struct {
	ReconciliationID string
	By               Approver
	At               time.Time
	PackageSHA256    []byte // SHA-256 of the sealed reconciliation package
	ApprovalID       string // the approved trust.reconciliation_signoff gate
}

// PGSignOff is the Postgres sign-off store and Locker over trust.reconciliation. A month is
// locked for an account when its reconciliation is signed or imported.
type PGSignOff struct {
	conn Querier
}

var _ Locker = (*PGSignOff)(nil)

// NewPGSignOff returns a PGSignOff that answers Locked on conn.
func NewPGSignOff(conn Querier) *PGSignOff { return &PGSignOff{conn: conn} }

// monthOf is the first day of p, the trust.reconciliation.month value.
func monthOf(p Period) time.Time { return time.Date(p.Year, p.Month, 1, 0, 0, 0, 0, time.UTC) }

const lockedSQL = `SELECT EXISTS (SELECT 1 FROM trust.reconciliation
WHERE account_id = $1::uuid AND month = $2::date AND status IN ('signed', 'imported'))`

// Locked implements Locker.
func (s *PGSignOff) Locked(ctx context.Context, accountID string, p Period) (bool, error) {
	var locked bool
	// rule: BIL-I-025
	if err := s.conn.QueryRow(ctx, lockedSQL, accountID, monthOf(p)).Scan(&locked); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "22P02" {
			return false, nil // not a uuid: no such account, nothing locked
		}
		return false, fmt.Errorf("recon locked %s %d-%02d: %w", accountID, p.Year, int(p.Month), err)
	}
	return locked, nil
}

const signLoadSQL = `SELECT prepared_by::text, status, adjusted_bank_cents, journal_balance_cents, client_listing_total_cents
FROM trust.reconciliation WHERE id = $1::uuid FOR UPDATE`

const signSQL = `UPDATE trust.reconciliation
SET status = 'signed', signed_by = $2::uuid, signed_at = $3, package_sha256 = $4, approval_id = $5::uuid
WHERE id = $1::uuid AND status IN ('draft', 'ready')`

// Sign records the sign-off in tx. It checks what the table CHECKs and triggers would refuse
// first, so the caller gets a sentinel: an unknown id is ErrNotFound, an unbalanced
// reconciliation ErrUnbalanced, the preparer ErrSelfApproval, an agent ErrAgentApproval, and a
// row that is not draft or ready, or a record missing its signer, time, package hash or
// approval, ErrInvalidState.
func (s *PGSignOff) Sign(ctx context.Context, tx pgx.Tx, r SignOffRecord) error {
	id := r.ReconciliationID
	if r.By.IsAgent {
		return fmt.Errorf("recon sign %s: %w", id, ErrAgentApproval)
	}
	if r.By.UserID == "" || r.At.IsZero() || len(r.PackageSHA256) == 0 || r.ApprovalID == "" {
		return fmt.Errorf("recon sign %s: signer, time, package hash and approval are required: %w", id, ErrInvalidState)
	}
	var (
		preparedBy, status         string
		adjusted, journal, clients int64
	)
	err := tx.QueryRow(ctx, signLoadSQL, id).Scan(&preparedBy, &status, &adjusted, &journal, &clients)
	if err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return fmt.Errorf("recon sign %s: %w", id, ErrNotFound)
		}
		return fmt.Errorf("recon sign %s: %w", id, err)
	}
	if status != "draft" && status != "ready" {
		return fmt.Errorf("recon sign %s: status %s: %w", id, status, ErrInvalidState)
	}
	// rule: BIL-I-025
	if adjusted != journal || journal != clients {
		return fmt.Errorf("recon sign %s: %w", id, ErrUnbalanced)
	}
	if r.By.UserID == preparedBy {
		return fmt.Errorf("recon sign %s: %w", id, ErrSelfApproval)
	}
	tag, err := tx.Exec(ctx, signSQL, id, r.By.UserID, r.At, r.PackageSHA256, r.ApprovalID)
	if err != nil {
		return fmt.Errorf("recon sign %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("recon sign %s: %w", id, ErrInvalidState)
	}
	return nil
}
