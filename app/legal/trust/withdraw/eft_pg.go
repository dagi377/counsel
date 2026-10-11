package withdraw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PGEFTRecords reads Form 9A records. The record is the document bound to a withdrawal by
// trust.withdrawal_request.form_9a_doc_id. Its signer and signing time come from its
// trust.form_9a_signature row, never from who uploaded the document or when. A record with no
// signature row, or whose document is a draft or archived, is not a record, so CheckEFT fails
// closed.
type PGEFTRecords struct{}

var _ EFTRecords = PGEFTRecords{}

const eftBindingSQL = `SELECT w.id::text, w.amount_cents FROM trust.withdrawal_request w
WHERE w.form_9a_doc_id = $1::uuid`

const eftSignatureSQL = `SELECT s.signed_by::text, s.signed_at FROM trust.form_9a_signature s
  JOIN docs.document d ON d.id = s.form_9a_doc_id
WHERE s.form_9a_doc_id = $1::uuid AND d.status IN ('final', 'sent') AND d.archived_at IS NULL`

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
	// rule: BIL-L-038 (verify)
	// The signer is the signature record's signer: the uploader is not a signer.
	if err := tx.QueryRow(ctx, eftSignatureSQL, id).Scan(&a.SignedBy, &a.SignedAt); err != nil {
		if notFound(err) {
			return EFTAuthorization{}, ErrNotFound
		}
		return EFTAuthorization{}, fmt.Errorf("eft record: %w", err)
	}
	return a, nil
}

const recordSignatureSQL = `INSERT INTO trust.form_9a_signature (form_9a_doc_id, signed_by, signed_at)
VALUES ($1::uuid, $2::uuid, $3)`

// RecordSignature records that signedBy signed the Form 9A document docID at signedAt. A record
// is signed once; a second signature on it fails.
func (PGEFTRecords) RecordSignature(ctx context.Context, tx pgx.Tx, docID, signedBy string, signedAt time.Time) error {
	if docID == "" || signedBy == "" || signedAt.IsZero() {
		return fmt.Errorf("record form 9a signature: %w", ErrInvalid)
	}
	if _, err := tx.Exec(ctx, recordSignatureSQL, docID, signedBy, signedAt); err != nil {
		return fmt.Errorf("record form 9a signature: %w", err)
	}
	return nil
}
