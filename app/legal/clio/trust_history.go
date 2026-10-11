package clio

import (
	"context"
	"errors"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/clock"
)

// ErrHistoryUnverified is returned while the By-Law 9 flag is off.
var ErrHistoryUnverified = errors.New("clio: trust history: by-law 9 rules not verified")

// HistoricalLine is one imported Clio trust line (trust.historical_line). It is evidence for
// reports only. Kind and Method are kept as Clio gave them. AmountCents is signed (deposits > 0).
type HistoricalLine struct {
	ID          string
	LedgerID    string
	TxnDate     clock.Date
	Kind        string
	Method      string
	AmountCents int64
}

// LiveTxn is one row of the live book (trust.trust_transaction), including the one
// opening_balance row per ledger. AmountCents is signed (receipts > 0).
type LiveTxn struct {
	ID          string
	LedgerID    string
	Kind        string
	AmountCents int64
}

// TrustBook is one trust account's live rows and its imported history, kept apart.
type TrustBook struct {
	Live    []LiveTxn
	History []HistoricalLine
}

// LedgerBalance is the live balance of one client ledger: the sum of live rows only.
func (b TrustBook) LedgerBalance(ctx context.Context, ledgerID string) (int64, error) {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return 0, ErrHistoryUnverified
	}
	// rule: MIG-037 (verify)
	var sum int64
	for _, t := range b.Live {
		if t.LedgerID == ledgerID {
			sum += t.AmountCents
		}
	}
	return sum, nil
}

// AccountBalance is the live balance of the whole account: the sum of live rows only.
func (b TrustBook) AccountBalance(ctx context.Context) (int64, error) {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return 0, ErrHistoryUnverified
	}
	// rule: MIG-037 (verify)
	var sum int64
	for _, t := range b.Live {
		sum += t.AmountCents
	}
	return sum, nil
}

// HistoryTotal is the sum of a ledger's imported history, for reports and the cut-over
// comparison. It is never a live balance.
func (b TrustBook) HistoryTotal(ledgerID string) int64 {
	var sum int64
	for _, h := range b.History {
		if h.LedgerID == ledgerID {
			sum += h.AmountCents
		}
	}
	return sum
}
