package ledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

func acct(code string, t registry.AccountType) registry.Account {
	return registry.Account{Code: code, Name: "n" + code, Type: t}
}

func TestNewChartsValidates(t *testing.T) {
	desk := registry.Chart{Book: "desk", Accounts: []registry.Account{acct("1000", registry.AccountAsset), acct("4000", registry.AccountRevenue)}}
	tests := []struct {
		name   string
		charts []registry.Chart
		want   error
	}{
		{"one chart", []registry.Chart{desk}, nil},
		{"two books", []registry.Chart{desk, {Book: "side", Accounts: []registry.Account{acct("2000", registry.AccountLiability)}}}, nil},
		{"no charts", nil, nil},
		{"no book", []registry.Chart{{Accounts: desk.Accounts}}, ErrInvalidChart},
		{"duplicate book", []registry.Chart{desk, {Book: "desk"}}, ErrInvalidChart},
		{"code in two books", []registry.Chart{desk, {Book: "side", Accounts: []registry.Account{acct("1000", registry.AccountAsset)}}}, ErrInvalidChart},
		{"code twice", []registry.Chart{{Book: "b", Accounts: []registry.Account{acct("1", registry.AccountAsset), acct("1", registry.AccountAsset)}}}, ErrInvalidChart},
		{"no code", []registry.Chart{{Book: "b", Accounts: []registry.Account{acct("", registry.AccountAsset)}}}, ErrInvalidChart},
		{"no name", []registry.Chart{{Book: "b", Accounts: []registry.Account{{Code: "1", Type: registry.AccountAsset}}}}, ErrInvalidChart},
		{"bad type", []registry.Chart{{Book: "b", Accounts: []registry.Account{acct("1", "income")}}}, ErrInvalidChart},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewCharts(tt.charts...); !errors.Is(err, tt.want) {
				t.Fatalf("NewCharts = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestChartsCheckUsesRegisteredAccountsOnly(t *testing.T) {
	c, err := NewCharts(
		registry.Chart{Book: "desk", Accounts: []registry.Account{acct("1000", registry.AccountAsset), acct("4000", registry.AccountRevenue)}},
		registry.Chart{Book: "side", Accounts: []registry.Account{acct("2000", registry.AccountLiability)}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Codes(); strings.Join(got, ",") != "1000,2000,4000" {
		t.Fatalf("Codes = %v", got)
	}
	if a, book, ok := c.Account("2000"); !ok || book != "side" || a.Type != registry.AccountLiability {
		t.Fatalf("Account(2000) = %v %q %v", a, book, ok)
	}
	tests := []struct {
		name string
		e    Entry
		want error
	}{
		{"registered", Entry{Book: "desk", Lines: []Line{line("1000", Debit, 5), line("4000", Credit, 5)}}, nil},
		{"unregistered code", Entry{Book: "desk", Lines: []Line{line("1000", Debit, 5), line("9999", Credit, 5)}}, ErrUnknownAccount},
		{"account of other book", Entry{Book: "desk", Lines: []Line{line("1000", Debit, 5), line("2000", Credit, 5)}}, ErrUnknownAccount},
		{"unregistered book", Entry{Book: "none", Lines: []Line{line("1000", Debit, 5), line("4000", Credit, 5)}}, ErrUnknownAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := c.Check(tt.e); !errors.Is(err, tt.want) {
				t.Fatalf("Check = %v, want %v", err, tt.want)
			}
		})
	}
}

// The engine knows no account code: no string literal in non-test ledger source looks like
// an account code, and an empty chart accepts nothing.
func TestEngineKnowsNoAccountCode(t *testing.T) {
	codeLit := regexp.MustCompile(`"([A-Z]{1,4}-)?\d{3,5}"`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := codeLit.FindString(string(b)); m != "" {
			t.Errorf("%s hard-codes account code %s", f, m)
		}
	}
	empty, err := NewCharts()
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Check(Entry{Book: "desk", Lines: []Line{line("1000", Debit, 1), line("4000", Credit, 1)}}); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("empty chart Check = %v, want ErrUnknownAccount", err)
	}
}

func TestChartAndBalanceStatementsNeverMutateJournal(t *testing.T) {
	mutating := regexp.MustCompile(`(?i)\b(UPDATE|DELETE|TRUNCATE)\b`)
	for i, s := range append(append([]string{}, syncStatements...), balanceStatements...) {
		if mutating.MatchString(s) {
			t.Errorf("statement %d mutates rows: %s", i, s)
		}
	}
	for i, s := range balanceStatements {
		if !strings.HasPrefix(s, "SELECT") {
			t.Errorf("balance statement %d is not a read: %s", i, s)
		}
	}
}

type row struct {
	v   any
	err error
}

func (r row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	switch d := dest[0].(type) {
	case *string:
		*d = r.v.(string)
	case *int64:
		*d = r.v.(int64)
	}
	return nil
}

type querier struct {
	rows []row
	args [][]any
}

func (q *querier) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.args = append(q.args, args)
	r := q.rows[0]
	q.rows = q.rows[1:]
	return r
}

func TestBalance(t *testing.T) {
	boom := errors.New("boom")
	asOf := Date{Year: 2026, Month: time.March, Day: 31}
	tests := []struct {
		name string
		rows []row
		want Cents
		err  error
	}{
		{"debit balance", []row{{v: "a1"}, {v: int64(1250)}}, 1250, nil},
		{"credit balance", []row{{v: "a1"}, {v: int64(-700)}}, -700, nil},
		{"no entries", []row{{v: "a1"}, {v: int64(0)}}, 0, nil},
		{"unknown account", []row{{err: pgx.ErrNoRows}}, 0, ErrUnknownAccount},
		{"lookup fails", []row{{err: boom}}, 0, boom},
		{"sum fails", []row{{v: "a1"}, {err: boom}}, 0, boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &querier{rows: tt.rows}
			got, err := Balances{Q: q}.Balance(context.Background(), "x", asOf)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Fatalf("Balance = %d, %v; want %d, %v", got, err, tt.want, tt.err)
			}
			if tt.err == nil {
				d := q.args[1][1].(time.Time)
				if d.Year() != 2026 || d.Month() != time.March || d.Day() != 31 {
					t.Fatalf("asOf passed as %v", d)
				}
			}
		})
	}
}
