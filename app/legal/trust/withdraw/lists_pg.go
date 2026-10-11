package withdraw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/registry"
)

// statusVersion is a withdrawal's row version. trust.withdrawal_request has no row_version
// column, and its status only moves forward (proposed -> pending_authorization -> authorized
// -> executed -> applied -> confirmed, or to rejected or cancelled, which are final), so the
// rank of the status changes on every update the routes care about. If-Match compares it.
var statusVersion = map[string]int{
	"proposed":              1,
	"pending_authorization": 2,
	"authorized":            3,
	"executed":              4,
	"applied":               5,
	"confirmed":             6,
	"rejected":              7,
	"cancelled":             8,
}

// contractStatus maps a database status onto the contract's: proposed and
// pending_authorization are both requested, the three executed states are executed.
func contractStatus(s string) Status {
	switch s {
	case "proposed", "pending_authorization":
		return StatusRequested
	case "authorized":
		return StatusAuthorized
	case "executed", "applied", "confirmed":
		return StatusExecuted
	case "cancelled":
		return StatusCancelled
	}
	return StatusRejected
}

// dbStatuses is contractStatus reversed, for the list filter.
func dbStatuses(s Status) []string {
	var out []string
	for name := range statusVersion {
		if contractStatus(name) == s {
			out = append(out, name)
		}
	}
	return out
}

// PGLists is Lists over trust.withdrawal_request. The ledger's account and matter come from
// trust.client_ledger, which the trust module owns too.
type PGLists struct {
	DB TxRunner
}

var _ Lists = PGLists{}

const listedColumns = `SELECT w.id::text, w.ledger_id::text, l.account_id::text, l.matter_id::text, w.amount_cents,
  w.payee, w.method, coalesce(w.form_9a_doc_id::text, ''), w.purpose_category, w.initiated_by::text, w.status,
  coalesce(w.approval_id::text, ''), coalesce(w.authorized_by::text, ''), w.authorized_at, w.expires_at,
  coalesce(w.executed_by::text, ''), w.executed_on
FROM trust.withdrawal_request w JOIN trust.client_ledger l ON l.id = w.ledger_id`

const listWithdrawalsSQL = listedColumns + `
WHERE (cardinality($1::text[]) = 0 OR w.status = ANY($1::text[]))
  AND (NULLIF($2, '')::uuid IS NULL OR w.id > NULLIF($2, '')::uuid)
ORDER BY w.id LIMIT $3`

const listedSQL = listedColumns + ` WHERE w.id = $1::uuid`

func scanListed(row pgx.Row) (Listed, error) {
	var l Listed
	var method, status string
	var authorizedAt, expiresAt, executedOn *time.Time
	if err := row.Scan(&l.ID, &l.LedgerID, &l.Request.TrustAccountID, &l.Request.MatterID, &l.Request.AmountCents,
		&l.Request.Payee, &method, &l.Request.EFTAuthorizationID, &l.Request.Reason, &l.Request.PreparedBy, &status,
		&l.GateID, &l.AuthorizedBy, &authorizedAt, &expiresAt, &l.ExecutedBy, &executedOn); err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
			return Listed{}, ErrNotFound
		}
		return Listed{}, fmt.Errorf("scan withdrawal: %w", err)
	}
	l.Request.Method, l.Status, l.Version = Method(method), contractStatus(status), statusVersion[status]
	if authorizedAt != nil {
		l.AuthorizedAt = *authorizedAt
	}
	if expiresAt != nil {
		l.ExpiresAt = *expiresAt
	}
	if executedOn != nil {
		l.ExecutedOn = *executedOn
	}
	return l, nil
}

// List implements Lists.
func (p PGLists) List(ctx context.Context, f ListFilter) ([]Listed, error) {
	statuses := []string{}
	if f.Status != "" {
		statuses = dbStatuses(f.Status)
		if len(statuses) == 0 {
			return nil, nil
		}
	}
	var out []Listed
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listWithdrawalsSQL, statuses, f.After, trust.ListLimit(f.Limit))
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			l, err := scanListed(rows)
			if err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list withdrawals: %w", err)
	}
	return out, nil
}

// Listed implements Lists.
func (p PGLists) Listed(ctx context.Context, id string) (Listed, error) {
	var out Listed
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanListed(tx.QueryRow(ctx, listedSQL, id))
		return err
	})
	if err != nil {
		return Listed{}, fmt.Errorf("withdrawal %s: %w", id, err)
	}
	return out, nil
}

// PGLedgers is LedgerReader over the trust module's Postgres ledger read.
type PGLedgers struct {
	Lists trust.PGLists
}

var _ LedgerReader = PGLedgers{}

// Ledger implements LedgerReader.
func (p PGLedgers) Ledger(ctx context.Context, id trust.LedgerID) (trust.Ledger, error) {
	return p.Lists.Ledger(ctx, id)
}

// PGCancels is Cancels over trust.withdrawal_request.
type PGCancels struct {
	DB TxRunner
}

var _ Cancels = PGCancels{}

const cancelRowSQL = `SELECT status, ledger_id::text, coalesce(bill_id::text, '')
FROM trust.withdrawal_request WHERE id = $1::uuid`

// Row implements Cancels.
func (p PGCancels) Row(ctx context.Context, id string) (Row, error) {
	var out Row
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var status string
		if err := tx.QueryRow(ctx, cancelRowSQL, id).Scan(&status, &out.LedgerID, &out.BillID); err != nil {
			var pg *pgconn.PgError
			if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
				return ErrNotFound
			}
			return fmt.Errorf("select: %w", err)
		}
		out.Version = int64(statusVersion[status])
		return nil
	})
	if err != nil {
		return Row{}, fmt.Errorf("withdrawal row %s: %w", id, err)
	}
	return out, nil
}

// cancelSQL cancels a withdrawal that is not yet authorized. An authorized one has an
// approved gate, which cannot be withdrawn, and money moves only through the gate effect.
const cancelSQL = `UPDATE trust.withdrawal_request SET status = 'cancelled'
WHERE id = $1::uuid AND status IN ('proposed', 'pending_authorization') RETURNING status`

// Cancel implements Cancels: it marks the withdrawal cancelled in tx and returns its new
// version. A withdrawal past authorization is ErrNotAuthorized; an unknown one ErrNotFound.
func (PGCancels) Cancel(ctx context.Context, tx pgx.Tx, id string, _ registry.Principal) (int64, error) {
	var status string
	err := tx.QueryRow(ctx, cancelSQL, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		var cur string
		if err := tx.QueryRow(ctx, `SELECT status FROM trust.withdrawal_request WHERE id = $1::uuid`, id).Scan(&cur); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return 0, fmt.Errorf("cancel withdrawal %s: %w", id, ErrNotFound)
			}
			return 0, fmt.Errorf("cancel withdrawal %s: select: %w", id, err)
		}
		return 0, fmt.Errorf("cancel withdrawal %s: status %s: %w", id, cur, ErrNotAuthorized)
	}
	if err != nil {
		return 0, fmt.Errorf("cancel withdrawal %s: %w", id, err)
	}
	return int64(statusVersion[status]), nil
}
