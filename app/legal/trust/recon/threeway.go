package recon

import (
	"context"
	"errors"
	"fmt"

	"github.com/dagi377/counsel/kernel/flags"
)

// FlagBylaw9Verified gates the three-way reconciliation, which is built on unconfirmed
// By-Law 9 text. It mirrors legal.FlagTrustBylaw9Verified; this package cannot import
// app/legal (import cycle).
const FlagBylaw9Verified = "trust.bylaw9_verified"

var (
	// ErrUnverified is returned while FlagBylaw9Verified is off.
	ErrUnverified = errors.New("recon: by-law 9 rules not verified")
	// ErrInvalidItem is returned for a reconciling item without a kind, a reference or an amount.
	ErrInvalidItem = errors.New("recon: invalid reconciling item")
)

// ComputeThreeWay compares the bank balance, adjusted by the reconciling items, with the
// trust book and the sum of the client ledgers. When they do not agree it returns the
// comparison together with an error wrapping ErrUnbalanced: the month stays unreconciled
// until a reconciling item explains every difference. Nothing here corrects the book.
func ComputeThreeWay(ctx context.Context, bankCents, bookCents int64, ledgerCents []int64, items []ReconcilingItem) (ThreeWay, error) {
	if !flags.Enabled(ctx, FlagBylaw9Verified) {
		return ThreeWay{}, ErrUnverified
	}
	t := ThreeWay{BankCents: bankCents, AdjustedBankCents: bankCents, BookCents: bookCents, Items: items}
	for _, it := range items {
		switch it.Kind {
		case OutstandingDeposit, OutstandingCheque, BankError, InTransit:
		default:
			return ThreeWay{}, fmt.Errorf("compute three-way: kind %q: %w", it.Kind, ErrInvalidItem)
		}
		if it.Reference == "" || it.AmountCents == 0 {
			return ThreeWay{}, fmt.Errorf("compute three-way: %w", ErrInvalidItem)
		}
		t.AdjustedBankCents += it.AmountCents
	}
	for _, c := range ledgerCents {
		t.ClientLedgerCents += c
	}
	if !t.Balanced() {
		return t, fmt.Errorf("compute three-way: adjusted bank %d, book %d, client ledgers %d; reconciling item required: %w",
			t.AdjustedBankCents, t.BookCents, t.ClientLedgerCents, ErrUnbalanced)
	}
	return t, nil
}
