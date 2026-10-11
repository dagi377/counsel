package recon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	kaudit "github.com/dagi377/counsel/kernel/audit"
)

// ActionSoleSignOff is the audit action of a sole practitioner signing their own
// reconciliation (docs/QUESTIONS.md Q-PORT-24).
const ActionSoleSignOff = "sign_off.sole_practitioner"

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
	// audit records the sole-practitioner exception. While nil the preparer never signs.
	audit kaudit.Log
}

var _ Locker = (*PGSignOff)(nil)

// NewPGSignOff returns a PGSignOff that answers Locked on conn.
func NewPGSignOff(conn Querier) *PGSignOff { return &PGSignOff{conn: conn} }

// WithAudit returns s recording the sole-practitioner exception through log, in the sign-off
// tx. Without it the preparer is always refused.
func (s *PGSignOff) WithAudit(log kaudit.Log) *PGSignOff {
	return &PGSignOff{conn: s.conn, audit: log}
}

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

const signLoadSQL = `SELECT prepared_by::text, status, adjusted_bank_cents, journal_balance_cents, client_listing_total_cents,
  bank_balance_cents, outstanding_items
FROM trust.reconciliation WHERE id = $1::uuid FOR UPDATE`

const signSQL = `UPDATE trust.reconciliation
SET status = 'signed', signed_by = $2::uuid, signed_at = $3, package_sha256 = $4, approval_id = $5::uuid
WHERE id = $1::uuid AND status IN ('draft', 'ready')`

// soleLicenseeSQL counts the firm's active licensees (a human lawyer or paralegal in good
// standing whose account is active, not offboarded, archived or expired, as the signatory
// directory defines it) and reports whether $1 is one of them.
const soleLicenseeSQL = `SELECT count(*), COALESCE(bool_or(u.id = $1::uuid), false)
FROM core.app_user u
WHERE u.principal_kind = 'human' AND u.licence_type IN ('lawyer', 'paralegal')
  AND u.licence_status = 'good_standing'
  AND u.status = 'active' AND u.offboarded_at IS NULL AND u.archived_at IS NULL
  AND (u.expires_on IS NULL OR u.expires_on >= (now() AT TIME ZONE 'America/Toronto')::date)`

// soleLicensee reports whether userID is the firm's one and only active licensee.
func soleLicensee(ctx context.Context, tx pgx.Tx, userID string) (bool, error) {
	var (
		n      int64
		signer bool
	)
	if err := tx.QueryRow(ctx, soleLicenseeSQL, userID).Scan(&n, &signer); err != nil {
		return false, err
	}
	return n == 1 && signer, nil
}

// Sign records the sign-off in tx. It checks what the table CHECKs and triggers would refuse
// first, so the caller gets a sentinel: an unknown id is ErrNotFound, an unbalanced
// reconciliation ErrUnbalanced, the preparer ErrSelfApproval, an agent ErrAgentApproval, and a
// row that is not draft or ready, or a record missing its signer, time, package hash or
// approval, ErrInvalidState. The preparer signs only as a sole practitioner: the firm has
// exactly one active licensee, the preparer is that licensee, and the exception is audited as
// ActionSoleSignOff in tx (core.check_sod also requires core.firm.sole_practitioner).
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
		bank                       int64
		itemsJSON                  []byte
	)
	err := tx.QueryRow(ctx, signLoadSQL, id).Scan(&preparedBy, &status, &adjusted, &journal, &clients, &bank, &itemsJSON)
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
	items, err := decodeItems(itemsJSON)
	if err != nil {
		return fmt.Errorf("recon sign %s: items: %w", id, err)
	}
	tw := ThreeWay{BankCents: bank, AdjustedBankCents: adjusted, BookCents: journal, ClientLedgerCents: clients, Items: items}
	// rule: BIL-I-009
	// By-law 9: the stored adjusted bank balance must be the bank balance plus its items.
	if !tw.ItemsAgree() {
		return fmt.Errorf("recon sign %s: %w", id, ErrUnbalanced)
	}
	// rule: BIL-I-025
	if adjusted != journal || journal != clients {
		return fmt.Errorf("recon sign %s: %w", id, ErrUnbalanced)
	}
	sole := false
	if r.By.UserID == preparedBy {
		// rule: ARC-I-021
		// By-law 9 needs a licensee to sign and a sole practitioner has no second person, so
		// the preparer may sign only when they are the firm's one active licensee and the
		// exception is audited. Every other preparer is refused; an agent never reaches here.
		if s == nil || s.audit == nil {
			return fmt.Errorf("recon sign %s: no audit for the sole-practitioner exception: %w", id, ErrSelfApproval)
		}
		ok, err := soleLicensee(ctx, tx, r.By.UserID)
		if err != nil {
			return fmt.Errorf("recon sign %s: licensees: %w", id, err)
		}
		if !ok {
			return fmt.Errorf("recon sign %s: %w", id, ErrSelfApproval)
		}
		sole = true
	}
	tag, err := tx.Exec(ctx, signSQL, id, r.By.UserID, r.At, r.PackageSHA256, r.ApprovalID)
	if err != nil {
		return fmt.Errorf("recon sign %s: %w", id, err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("recon sign %s: %w", id, ErrInvalidState)
	}
	if sole {
		e := kaudit.Event{
			ActorKind: "user",
			ActorID:   r.By.UserID,
			Action:    ActionSoleSignOff,
			Subject:   kaudit.Subject{Kind: "trust.reconciliation", ID: id},
			Detail:    map[string]string{"approval_id": r.ApprovalID},
		}
		if err := s.audit.Append(ctx, tx, e); err != nil {
			return fmt.Errorf("recon sign %s: audit: %w", id, err)
		}
	}
	return nil
}
