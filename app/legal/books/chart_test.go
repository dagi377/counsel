package books

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
)

func dr(acct string, c int64) Line { return Line{Account: acct, DebitCents: c} }
func cr(acct string, c int64) Line { return Line{Account: acct, CreditCents: c} }

// rule: BIL-I-002
func TestBIL_I_002_EntryStaysInOneBook(t *testing.T) {
	tests := []struct {
		name string
		e    Entry
		want error
	}{
		{"operating entry", Entry{Book: BookOperating, Lines: []Line{dr(AcctOperatingBank, 1000), cr(AcctARFees, 1000)}}, nil},
		{"trust entry", Entry{Book: BookTrust, Lines: []Line{dr(AcctTrustBank, 1000), cr(AcctClientTrustLedger, 1000)}}, nil},
		{"OP entry with TR-1500 line", Entry{Book: BookOperating, Lines: []Line{dr(AcctTrustBank, 1000), cr(AcctARFees, 1000)}}, ErrCrossBook},
		{"TR entry with OP line", Entry{Book: BookTrust, Lines: []Line{dr(AcctTrustBank, 1000), cr(AcctFeeRevenue, 1000)}}, ErrCrossBook},
		{"unknown account", Entry{Book: BookOperating, Lines: []Line{dr("OP-9999", 1), cr(AcctARFees, 1)}}, ErrUnknownAccount},
		{"unknown book", Entry{Book: "other", Lines: []Line{dr(AcctOperatingBank, 1), cr(AcctARFees, 1)}}, ErrUnknownAccount},
		{"unbalanced", Entry{Book: BookOperating, Lines: []Line{dr(AcctOperatingBank, 1000), cr(AcctARFees, 999)}}, ErrUnbalanced},
		{"one line", Entry{Book: BookOperating, Lines: []Line{dr(AcctOperatingBank, 0)}}, ErrUnbalanced},
		{"both sides on a line", Entry{Book: BookOperating, Lines: []Line{{Account: AcctOperatingBank, DebitCents: 5, CreditCents: 5}, cr(AcctARFees, 0)}}, ErrUnbalanced},
		{"negative", Entry{Book: BookOperating, Lines: []Line{dr(AcctOperatingBank, -5), cr(AcctARFees, -5)}}, ErrUnbalanced},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := (Charts{}).CheckEntry(tt.e); !errors.Is(err, tt.want) {
				t.Fatalf("CheckEntry = %v, want %v", err, tt.want)
			}
		})
	}
}

// rule: BIL-I-002
func TestBIL_I_002_ChartsRegisterAsSeparateBooks(t *testing.T) {
	var rec registry.Recorder
	if err := RegisterCharts(&rec); err != nil {
		t.Fatal(err)
	}
	calls := rec.Method(registry.MethodChartOfAccounts)
	if len(calls) != 2 {
		t.Fatalf("ChartOfAccounts calls = %d, want 2", len(calls))
	}
	prefix := map[string]string{BookOperating: "OP-", BookTrust: "TR-"}
	total := 0
	for _, c := range calls {
		ch := c.Args[0].(registry.Chart)
		p, ok := prefix[ch.Book]
		if !ok {
			t.Fatalf("unexpected book %q", ch.Book)
		}
		for _, a := range ch.Accounts {
			if !strings.HasPrefix(a.Code, p) {
				t.Errorf("book %s has account %s", ch.Book, a.Code)
			}
			if a.Type == "" {
				t.Errorf("account %s has no ledger type", a.Code)
			}
		}
		total += len(ch.Accounts)
	}
	if total != len(chart) {
		t.Fatalf("registered %d accounts, want %d", total, len(chart))
	}
}

// TestChartMatchesSpec checks codes and books against spec/eng/b3-billing-engine.json.
func TestChartMatchesSpec(t *testing.T) {
	raw, err := os.ReadFile(filepath.FromSlash("../../../spec/eng/b3-billing-engine.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Chart []struct{ Book, Code string } `json:"chart_of_accounts"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, a := range spec.Chart {
		want[a.Code] = a.Book
	}
	got := (Charts{}).Accounts()
	if len(got) != len(want) {
		t.Fatalf("chart has %d accounts, spec has %d", len(got), len(want))
	}
	for i, a := range got {
		if want[a.Code] != a.Book {
			t.Errorf("%s: book %q, spec %q", a.Code, a.Book, want[a.Code])
		}
		if i > 0 && (got[i-1].Book > a.Book || (got[i-1].Book == a.Book && got[i-1].Code >= a.Code)) {
			t.Errorf("Accounts not ordered at %d: %s/%s after %s/%s", i, a.Book, a.Code, got[i-1].Book, got[i-1].Code)
		}
	}
}
