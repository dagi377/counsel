package reports

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/legalflags"
)

// FlagBylaw9Verified gates the monthly client trust listing until the By-Law 9 provisions
// are verified. It is off by default.
const FlagBylaw9Verified = legalflags.TrustBylaw9Verified

// ErrDisabled is returned while FlagBylaw9Verified is off.
var ErrDisabled = errors.New("reports: trust listing is disabled")

// TrustSource reads the balances a trust listing is built from.
type TrustSource interface {
	// ClientBalances returns each client matter's balance in the trust account at the end of
	// day asOf; ErrNotFound for an unknown account.
	ClientBalances(ctx context.Context, trustAccountID string, asOf time.Time) ([]TrustLine, error)
	// AccountBalance returns the balance of ledger account code for the trust account at the
	// end of day asOf.
	AccountBalance(ctx context.Context, trustAccountID, code string, asOf time.Time) (int64, error)
}

// TrustLister builds monthly client trust listings.
type TrustLister struct {
	Source TrustSource
}

// MonthEnd returns the last calendar day of the month containing day, in day's location.
func MonthEnd(day time.Time) time.Time {
	return time.Date(day.Year(), day.Month()+1, 0, 0, 0, 0, 0, day.Location())
}

// TrustListing returns the client trust listing for the month containing monthEnd. Lines
// are ordered by client then matter. The total is checked against the account's TR-2500
// balance; when they differ the listing is returned with ErrOutOfBalance.
func (l TrustLister) TrustListing(ctx context.Context, trustAccountID string, monthEnd time.Time) (TrustListing, error) {
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return TrustListing{}, ErrDisabled
	}
	if trustAccountID == "" {
		return TrustListing{}, fmt.Errorf("trust listing: account missing: %w", ErrNotFound)
	}
	end := MonthEnd(monthEnd)
	lines, err := l.Source.ClientBalances(ctx, trustAccountID, end)
	if err != nil {
		return TrustListing{}, fmt.Errorf("trust listing %s: client balances: %w", trustAccountID, err)
	}
	ledger, err := l.Source.AccountBalance(ctx, trustAccountID, books.AcctClientTrustLedger, end)
	if err != nil {
		return TrustListing{}, fmt.Errorf("trust listing %s: ledger balance: %w", trustAccountID, err)
	}
	out := TrustListing{
		TrustAccountID: trustAccountID,
		MonthEnd:       end,
		Lines:          append([]TrustLine(nil), lines...),
		LedgerCents:    ledger,
	}
	sort.Slice(out.Lines, func(i, j int) bool {
		if out.Lines[i].ClientID != out.Lines[j].ClientID {
			return out.Lines[i].ClientID < out.Lines[j].ClientID
		}
		return out.Lines[i].MatterID < out.Lines[j].MatterID
	})
	for _, x := range out.Lines {
		out.TotalCents += x.BalanceCents
	}
	// The listing reconciles to the client trust ledger control account per trust account.
	if out.TotalCents != out.LedgerCents {
		return out, fmt.Errorf("trust listing %s: lines %d, %s %d: %w",
			trustAccountID, out.TotalCents, books.AcctClientTrustLedger, out.LedgerCents, ErrOutOfBalance)
	}
	return out, nil
}
