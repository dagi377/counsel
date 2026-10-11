package recon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/trust"
)

// ErrNoParser is returned by NoParser: no bank statement parser is configured, so an upload
// is refused (422) rather than guessed at.
var ErrNoParser = errors.New("recon: no bank statement parser is configured")

// NoParser is the Parser boot wires until a real CSV/OFX parser exists. It refuses every
// file with ErrNoParser and imports nothing.
type NoParser struct{}

var _ Parser = NoParser{}

// Parse implements Parser.
func (NoParser) Parse(context.Context, string, []byte) (Statement, error) {
	return Statement{}, ErrNoParser
}

// statusVersion is a reconciliation's row version. trust.reconciliation has no row_version
// column and its status only moves forward (draft -> ready -> signed -> superseded; an
// imported month is born imported), so the rank of the status changes on every update the
// submit route cares about. If-Match compares it.
var statusVersion = map[string]int64{
	"draft":      1,
	"imported":   1,
	"ready":      2,
	"signed":     3,
	"superseded": 4,
}

// PGLists is Lists over trust.reconciliation.
type PGLists struct {
	DB TxRunner
}

var _ Lists = PGLists{}

// listReconsSQL pages reconciliations by id. Empty filters are NULL, so the uuid casts never
// see an empty string.
const listReconsSQL = `SELECT id::text, account_id::text, month, bank_balance_cents, outstanding_items,
  adjusted_bank_cents, journal_balance_cents, client_listing_total_cents, status, prepared_by::text,
  coalesce(signed_by::text, ''), signed_at
FROM trust.reconciliation
WHERE (NULLIF($1, '')::uuid IS NULL OR account_id = NULLIF($1, '')::uuid)
  AND (NULLIF($2, '')::uuid IS NULL OR id > NULLIF($2, '')::uuid)
ORDER BY id LIMIT $3`

// storedItem is one entry of trust.reconciliation.outstanding_items.
type storedItem struct {
	Kind        string    `json:"kind"`
	Reference   string    `json:"reference"`
	AmountCents int64     `json:"amount_cents"`
	Since       time.Time `json:"since"`
}

// List implements Lists. Signed and imported months are reported as approved.
func (p PGLists) List(ctx context.Context, f ListFilter) ([]Reconciliation, error) {
	var out []Reconciliation
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, listReconsSQL, f.AccountID, f.After, trust.ListLimit(f.Limit))
		if err != nil {
			return fmt.Errorf("select: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var c Reconciliation
			var month time.Time
			var items []byte
			var status string
			if err := rows.Scan(&c.ID, &c.AccountID, &month, &c.Result.BankCents, &items, &c.Result.AdjustedBankCents,
				&c.Result.BookCents, &c.Result.ClientLedgerCents, &status, &c.PreparedBy, &c.ApprovedBy,
				&c.ApprovedAt); err != nil {
				return fmt.Errorf("scan: %w", err)
			}
			c.Period = Period{Year: month.Year(), Month: month.Month()}
			c.Status = StatusPrepared
			if status == "signed" || status == "imported" {
				c.Status = StatusApproved
			}
			if c.Result.Items, err = decodeItems(items); err != nil {
				return fmt.Errorf("reconciliation %s: %w", c.ID, err)
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list reconciliations: %w", err)
	}
	return out, nil
}

func decodeItems(raw []byte) ([]ReconcilingItem, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var stored []storedItem
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("outstanding items: %w", err)
	}
	out := make([]ReconcilingItem, 0, len(stored))
	for _, s := range stored {
		out = append(out, ReconcilingItem{Kind: ItemKind(s.Kind), Reference: s.Reference,
			AmountCents: s.AmountCents, Since: s.Since})
	}
	return out, nil
}

// PGVersions is Versions over trust.reconciliation.
type PGVersions struct {
	DB TxRunner
}

var _ Versions = PGVersions{}

// Version implements Versions. An unknown or malformed id is ErrNotFound.
func (p PGVersions) Version(ctx context.Context, id string) (int64, error) {
	var status string
	err := p.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status FROM trust.reconciliation WHERE id = $1::uuid`, id).Scan(&status)
	})
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
		return 0, fmt.Errorf("reconciliation %s: %w", id, ErrNotFound)
	}
	if err != nil {
		return 0, fmt.Errorf("reconciliation %s: %w", id, err)
	}
	return statusVersion[status], nil
}
