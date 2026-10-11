package export

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// TrustBalancesPath is the per-ledger trust balance file of an export (MIG-073).
const TrustBalancesPath = "trust_balances.csv"

var (
	// ErrTrustMismatch is returned when exported trust balances do not reconcile.
	ErrTrustMismatch = errors.New("export: trust balances do not reconcile")
	// ErrBadTrustInput is returned for a trust line or reconciliation without ids.
	ErrBadTrustInput = errors.New("export: bad trust input")
)

// TrustLine is one exported trust ledger line, in cents (receipt > 0, disbursement < 0).
type TrustLine struct {
	AccountID string
	LedgerID  string
	Date      time.Time
	Cents     int64
}

// SignedReconciliation is a signed monthly trust reconciliation of one account.
type SignedReconciliation struct {
	ID                 string
	AccountID          string
	MonthEnd           time.Time
	ClientListingCents int64
}

// TrustExport is the trust part of an export: manifest balances and trust_balances.csv.
type TrustExport struct {
	Balances []TrustBalance
	CSV      []byte
}

var toronto = func() *time.Location {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return time.FixedZone("EST", -5*3600)
	}
	return loc
}()

// day is the business date of t in America/Toronto.
func day(t time.Time) string { return Date(t.In(toronto)) }

// TrustBalances sums the exported ledger lines up to asOf per ledger and per account. When
// asOf is the month end of an account's last signed reconciliation, the account total must
// equal that reconciliation's client listing total, else ErrTrustMismatch.
func TrustBalances(asOf time.Time, lines []TrustLine, recs []SignedReconciliation) (TrustExport, error) {
	asOfDay := day(asOf)
	perLedger := map[[2]string]int64{}
	perAccount := map[string]int64{}
	for i, l := range lines {
		if l.AccountID == "" || l.LedgerID == "" {
			return TrustExport{}, fmt.Errorf("trust line %d: %w", i, ErrBadTrustInput)
		}
		if day(l.Date) > asOfDay {
			continue
		}
		perLedger[[2]string{l.AccountID, l.LedgerID}] += l.Cents
		perAccount[l.AccountID] += l.Cents
	}
	last := map[string]SignedReconciliation{}
	for i, r := range recs {
		if r.ID == "" || r.AccountID == "" {
			return TrustExport{}, fmt.Errorf("reconciliation %d: %w", i, ErrBadTrustInput)
		}
		if day(r.MonthEnd) > asOfDay {
			continue
		}
		if cur, ok := last[r.AccountID]; !ok || day(r.MonthEnd) > day(cur.MonthEnd) {
			last[r.AccountID] = r
		}
		if _, ok := perAccount[r.AccountID]; !ok {
			perAccount[r.AccountID] = 0
		}
	}

	accounts := make([]string, 0, len(perAccount))
	for a := range perAccount {
		accounts = append(accounts, a)
	}
	sort.Strings(accounts)
	out := TrustExport{Balances: make([]TrustBalance, 0, len(accounts))}
	for _, a := range accounts {
		tb := TrustBalance{AccountID: a, TotalCents: perAccount[a], Currency: Currency}
		if r, ok := last[a]; ok {
			id := r.ID
			tb.LastSignedReconciliation = &id
			// rule: MIG-073
			if day(r.MonthEnd) == asOfDay && r.ClientListingCents != tb.TotalCents {
				return TrustExport{}, fmt.Errorf("account %s: ledger lines %d != signed reconciliation %s client listing %d: %w",
					a, tb.TotalCents, r.ID, r.ClientListingCents, ErrTrustMismatch)
			}
		}
		out.Balances = append(out.Balances, tb)
	}

	keys := make([][2]string, 0, len(perLedger))
	for k := range perLedger {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		return keys[i][1] < keys[j][1]
	})
	rows := make([][]string, 0, len(keys))
	for _, k := range keys {
		rows = append(rows, []string{k[0], k[1], strconv.FormatInt(perLedger[k], 10), Currency, asOfDay})
	}
	// rule: MIG-073
	b, err := CSV([]string{"account_id", "ledger_id", "balance_cents", "currency", "as_of"}, rows)
	if err != nil {
		return TrustExport{}, fmt.Errorf("trust balances: %w", err)
	}
	out.CSV = b
	return out, nil
}
