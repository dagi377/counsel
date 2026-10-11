package withdraw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CodeNoEFTAuthorization is the effect failure code for an EFT executed without a signed
// authorization record (Form 9A).
const CodeNoEFTAuthorization = "eft_authorization_missing"

// EFTAuthorization is the signed requisition (Form 9A) for one electronic trust transfer.
type EFTAuthorization struct {
	ID           string
	WithdrawalID string
	AmountCents  int64
	SignedBy     string
	SignedAt     time.Time
}

// EFTRecords reads Form 9A records.
type EFTRecords interface {
	// EFTAuthorization returns one record, or ErrNotFound.
	EFTAuthorization(ctx context.Context, tx pgx.Tx, id string) (EFTAuthorization, error)
}

// CheckEFT refuses to execute an EFT withdrawal unless a Form 9A record exists for it, for its
// amount, signed by the licensee who authorised it before execution time at. A cheque needs no
// record. Every failure, including a missing record store, returns ErrNoEFTAuthorization.
func CheckEFT(ctx context.Context, tx pgx.Tx, recs EFTRecords, h Held, at time.Time) error {
	if h.Request.Method != MethodEFT {
		return nil
	}
	missing := func(why string) error { return fmt.Errorf("eft %s: %s: %w", h.ID, why, ErrNoEFTAuthorization) }
	if h.Request.EFTAuthorizationID == "" {
		return missing("no record")
	}
	if recs == nil {
		return missing("no record store")
	}
	a, err := recs.EFTAuthorization(ctx, tx, h.Request.EFTAuthorizationID)
	if errors.Is(err, ErrNotFound) {
		return missing("record not found")
	}
	if err != nil {
		return fmt.Errorf("eft %s: %w", h.ID, err)
	}
	switch {
	case a.WithdrawalID != h.ID:
		return missing("record is for another withdrawal")
	case a.AmountCents != h.Request.AmountCents:
		return missing("record amount differs")
	case a.SignedBy == "" || a.SignedBy != h.AuthorizedBy:
		return missing("record not signed by the authorizer")
	case a.SignedAt.IsZero() || a.SignedAt.After(at):
		return missing("record not signed before execution")
	}
	return nil
}
