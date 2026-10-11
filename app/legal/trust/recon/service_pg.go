package recon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// ErrEffectOnly is returned by PGService.Approve: a reconciliation is signed off only by the
// trust.reconciliation_signoff gate's effect (PGSignOff.Sign, with the sealed package and the
// approved gate), never through the service.
var ErrEffectOnly = errors.New("recon: sign-off runs only through the gate effect")

// TxStatements is the Statements port over the kernel DB: PGStatements read inside one RLS
// transaction.
type TxStatements struct {
	DB TxRunner
}

var _ Statements = TxStatements{}

var _ Statements = (*PGStatements)(nil)

// Closing implements Statements.
func (s TxStatements) Closing(ctx context.Context, accountID string, p Period) (int64, error) {
	var closing int64
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		closing, err = NewPGStatements(tx).Closing(ctx, accountID, p)
		return err
	})
	return closing, err
}

// PGService is Service over trust.reconciliation. It prepares a month's three-way
// reconciliation from the imported statement's closing balance (the bank), the trust
// journal and the client ledgers in trust.trust_transaction, and reads it back. It never signs
// off (Approve refuses) and imports no statement (no parser is configured).
type PGService struct {
	DB TxRunner
}

var _ Service = PGService{}

// bookSQL is the trust journal balance of the account at the end of the month.
const bookSQL = `SELECT coalesce(sum(amount_cents), 0)::bigint FROM trust.trust_transaction
WHERE account_id = $1::uuid AND txn_date <= $2::date`

// ledgerTotalsSQL is each client ledger's balance on the account at the end of the month.
const ledgerTotalsSQL = `SELECT ledger_id::text, sum(amount_cents)::bigint FROM trust.trust_transaction
WHERE account_id = $1::uuid AND txn_date <= $2::date
GROUP BY ledger_id ORDER BY ledger_id`

// upsertReconSQL records the month's open reconciliation, replacing an earlier draft or
// ready one. A signed or imported month matches no row: it is locked.
const upsertReconSQL = `INSERT INTO trust.reconciliation (account_id, month, bank_balance_cents, outstanding_items,
  adjusted_bank_cents, journal_balance_cents, client_listing_total_cents, client_listing_snapshot, prepared_by, status)
VALUES ($1::uuid, $2::date, $3, $4::jsonb, $5, $6, $7, $8::jsonb, $9::uuid, $10)
ON CONFLICT (account_id, month) WHERE status <> 'superseded' DO UPDATE SET
  bank_balance_cents = EXCLUDED.bank_balance_cents, outstanding_items = EXCLUDED.outstanding_items,
  adjusted_bank_cents = EXCLUDED.adjusted_bank_cents, journal_balance_cents = EXCLUDED.journal_balance_cents,
  client_listing_total_cents = EXCLUDED.client_listing_total_cents,
  client_listing_snapshot = EXCLUDED.client_listing_snapshot, prepared_by = EXCLUDED.prepared_by,
  status = EXCLUDED.status
WHERE trust.reconciliation.status IN ('draft', 'ready')
RETURNING id::text`

const getReconSQL = `SELECT id::text, account_id::text, month, bank_balance_cents, outstanding_items,
  adjusted_bank_cents, journal_balance_cents, client_listing_total_cents, status, prepared_by::text,
  coalesce(signed_by::text, ''), signed_at
FROM trust.reconciliation WHERE id = $1::uuid`

type snapshotLine struct {
	LedgerID     string `json:"ledger_id"`
	BalanceCents int64  `json:"balance_cents"`
}

// monthEnd is the last day of p.
func monthEnd(p Period) time.Time { return time.Date(p.Year, p.Month+1, 0, 0, 0, 0, 0, time.UTC) }

// ledgerTotals reads every client ledger's month-end balance, signed and unclamped.
func ledgerTotals(ctx context.Context, tx pgx.Tx, accountID string, end time.Time) ([]snapshotLine, error) {
	rows, err := tx.Query(ctx, ledgerTotalsSQL, accountID, end)
	if err != nil {
		return nil, fmt.Errorf("client ledgers: %w", err)
	}
	defer rows.Close()
	var out []snapshotLine
	for rows.Next() {
		var l snapshotLine
		if err := rows.Scan(&l.LedgerID, &l.BalanceCents); err != nil {
			return nil, fmt.Errorf("client ledgers: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Prepare implements Service. It fails closed while FlagBylaw9Verified is off, on a locked
// month and when no statement is imported for the month. An unbalanced month is recorded as
// draft and returned without error: submit refuses it until reconciling items explain every
// difference. Nothing here corrects the book.
func (s PGService) Prepare(ctx context.Context, accountID string, p Period, items []ReconcilingItem, preparedBy string) (Reconciliation, error) {
	// rule: BIL-I-009
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return Reconciliation{}, ErrUnverified
	}
	if s.DB == nil || preparedBy == "" || accountID == "" {
		return Reconciliation{}, fmt.Errorf("prepare: database, account and preparer: %w", ErrInvalidState)
	}
	out := Reconciliation{AccountID: accountID, Period: p, Status: StatusPrepared, PreparedBy: preparedBy}
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// rule: BIL-I-025
		locked, err := NewPGSignOff(tx).Locked(ctx, accountID, p)
		if err != nil {
			return err
		}
		if locked {
			return ErrPeriodLocked
		}
		bank, err := NewPGStatements(tx).Closing(ctx, accountID, p)
		if err != nil {
			return err
		}
		end := monthEnd(p)
		var book int64
		if err := tx.QueryRow(ctx, bookSQL, accountID, end).Scan(&book); err != nil {
			return fmt.Errorf("book: %w", err)
		}
		lines, err := ledgerTotals(ctx, tx, accountID, end)
		if err != nil {
			return err
		}
		cents := make([]int64, len(lines))
		for i, l := range lines {
			cents[i] = l.BalanceCents
		}
		tw, err := ComputeThreeWay(ctx, bank, book, cents, items)
		if err != nil && !errors.Is(err, ErrUnbalanced) {
			return err
		}
		out.Result = tw
		status := "draft"
		if tw.Balanced() {
			status = "ready"
		}
		stored := make([]storedItem, 0, len(items))
		for _, it := range items {
			stored = append(stored, storedItem{Kind: string(it.Kind), Reference: it.Reference, AmountCents: it.AmountCents, Since: it.Since})
		}
		if lines == nil {
			lines = []snapshotLine{}
		}
		itemsJSON, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		snapJSON, err := json.Marshal(lines)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, upsertReconSQL, accountID, monthOf(p), tw.BankCents, string(itemsJSON),
			tw.AdjustedBankCents, tw.BookCents, tw.ClientLedgerCents, string(snapJSON), preparedBy, status).Scan(&out.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPeriodLocked
		}
		return err
	})
	if err != nil {
		return Reconciliation{}, fmt.Errorf("prepare %d-%02d: %w", p.Year, int(p.Month), err)
	}
	return out, nil
}

// Get implements Service. Signed and imported months read as approved.
func (s PGService) Get(ctx context.Context, id string) (Reconciliation, error) {
	if s.DB == nil {
		return Reconciliation{}, fmt.Errorf("reconciliation %s: no database: %w", id, ErrInvalidState)
	}
	var c Reconciliation
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var month time.Time
		var items []byte
		var status string
		err := tx.QueryRow(ctx, getReconSQL, id).Scan(&c.ID, &c.AccountID, &month, &c.Result.BankCents, &items,
			&c.Result.AdjustedBankCents, &c.Result.BookCents, &c.Result.ClientLedgerCents, &status, &c.PreparedBy,
			&c.ApprovedBy, &c.ApprovedAt)
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		c.Period = Period{Year: month.Year(), Month: month.Month()}
		c.Status = StatusPrepared
		if status == "signed" || status == "imported" {
			c.Status = StatusApproved
		}
		c.Result.Items, err = decodeItems(items)
		return err
	})
	if err != nil {
		return Reconciliation{}, fmt.Errorf("reconciliation %s: %w", id, err)
	}
	return c, nil
}

// Locked implements Service.
func (s PGService) Locked(ctx context.Context, accountID string, p Period) (bool, error) {
	if s.DB == nil {
		return false, fmt.Errorf("locked: no database: %w", ErrInvalidState)
	}
	var locked bool
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		locked, err = NewPGSignOff(tx).Locked(ctx, accountID, p)
		return err
	})
	return locked, err
}

// ImportStatement implements Service by refusing: no bank statement parser or import store is
// configured, so nothing is imported.
func (PGService) ImportStatement(_ context.Context, s Statement) (string, error) {
	return "", fmt.Errorf("import statement for %s: %w", s.AccountID, ErrNoParser)
}

// Approve implements Service by refusing. An agent is refused as such; everyone else is told
// sign-off runs only through the gate effect.
func (PGService) Approve(_ context.Context, id string, by Approver) error {
	// rule: BIL-I-025
	if by.IsAgent {
		return fmt.Errorf("approve %s: %w", id, ErrAgentApproval)
	}
	return fmt.Errorf("approve %s: %w: %w", id, ErrEffectOnly, ErrInvalidState)
}
