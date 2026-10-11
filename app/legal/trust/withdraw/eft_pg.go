package withdraw

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PGEFTRecords reads Form 9A records. The record is the document bound to a withdrawal by
// trust.withdrawal_request.form_9a_doc_id; it counts as signed once the document is final,
// by the user who filed it, at the time it was filed. A draft, archived or unbound document
// is not a record, so CheckEFT fails closed.
type PGEFTRecords struct{}

var _ EFTRecords = PGEFTRecords{}

const eftBindingSQL = `SELECT w.id::text, w.amount_cents FROM trust.withdrawal_request w
WHERE w.form_9a_doc_id = $1::uuid`

const eftDocumentSQL = `SELECT coalesce(d.created_by::text, ''), d.created_at FROM docs.document d
WHERE d.id = $1::uuid AND d.status IN ('final', 'sent') AND d.archived_at IS NULL`

// EFTAuthorization implements EFTRecords.
func (PGEFTRecords) EFTAuthorization(ctx context.Context, tx pgx.Tx, id string) (EFTAuthorization, error) {
	notFound := func(err error) bool {
		var pg *pgconn.PgError
		return errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02")
	}
	a := EFTAuthorization{ID: id}
	rows, err := tx.Query(ctx, eftBindingSQL, id)
	if err != nil {
		if notFound(err) {
			return EFTAuthorization{}, ErrNotFound
		}
		return EFTAuthorization{}, fmt.Errorf("eft record: %w", err)
	}
	n := 0
	for rows.Next() {
		n++
		if err := rows.Scan(&a.WithdrawalID, &a.AmountCents); err != nil {
			rows.Close()
			return EFTAuthorization{}, fmt.Errorf("eft record: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		if notFound(err) {
			return EFTAuthorization{}, ErrNotFound
		}
		return EFTAuthorization{}, fmt.Errorf("eft record: %w", err)
	}
	// One record authorises one withdrawal: a record bound to none, or to several, is not one.
	if n != 1 {
		return EFTAuthorization{}, ErrNotFound
	}
	if err := tx.QueryRow(ctx, eftDocumentSQL, id).Scan(&a.SignedBy, &a.SignedAt); err != nil {
		if notFound(err) {
			return EFTAuthorization{}, ErrNotFound
		}
		return EFTAuthorization{}, fmt.Errorf("eft record: %w", err)
	}
	return a, nil
}
