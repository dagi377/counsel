package ledger

import (
	"context"
	"errors"
	"os"
	"reflect"
	"regexp"
	"testing"
)

func strp(s string) *string { return &s }

// rule: BIL-I-019
func TestBIL_I_019_MirrorSwapsSidesOnly(t *testing.T) {
	subj := Subject{Kind: "k", ID: "1"}
	tests := []struct {
		name string
		in   []Line
		want []Line
	}{
		{"empty", nil, []Line{}},
		{"two lines", []Line{line("1000", Debit, 500), line("4000", Credit, 500)}, []Line{line("1000", Credit, 500), line("4000", Debit, 500)}},
		{"three lines with subject", []Line{{Account: "1000", Side: Debit, Amount: 300, Subject: subj}, line("1010", Debit, 200), line("4000", Credit, 500)},
			[]Line{{Account: "1000", Side: Credit, Amount: 300, Subject: subj}, line("1010", Credit, 200), line("4000", Debit, 500)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Mirror(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Mirror = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(Mirror(got), append([]Line{}, tt.in...)) && len(tt.in) > 0 {
				t.Fatalf("Mirror twice != original")
			}
			var net Cents
			for i := range tt.in {
				for _, l := range []Line{tt.in[i], got[i]} {
					if l.Side == Debit {
						net += l.Amount
					} else {
						net -= l.Amount
					}
				}
			}
			if net != 0 {
				t.Fatalf("original plus reversal nets %d, want 0", net)
			}
		})
	}
}

// rule: BIL-I-019
func TestBIL_I_019_MirrorRowsKeepsEveryColumn(t *testing.T) {
	tests := []struct {
		name string
		in   []lineRow
		want []lineRow
	}{
		{"debit and credit", []lineRow{{AccountID: "a", Debit: 500}, {AccountID: "b", Credit: 500}},
			[]lineRow{{AccountID: "a", Credit: 500}, {AccountID: "b", Debit: 500}}},
		{"optional columns copied", []lineRow{{AccountID: "a", Debit: 7, Party: strp("p"), TaxCode: strp("t"), BankLineID: strp("bl")}, {AccountID: "b", Credit: 7}},
			[]lineRow{{AccountID: "a", Credit: 7, Party: strp("p"), TaxCode: strp("t"), BankLineID: strp("bl")}, {AccountID: "b", Debit: 7}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mirrorRows(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("mirrorRows = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// rule: BIL-I-019
func TestBIL_I_019_ReverseRefusesBadInputBeforeIO(t *testing.T) {
	g := &PG{}
	tests := []struct {
		name   string
		id     EntryID
		reason string
	}{
		{"no id", "", "wrong account"},
		{"no reason", "e1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A nil tx proves no statement runs.
			if _, err := g.Reverse(context.Background(), nil, tt.id, tt.reason); !errors.Is(err, ErrInvalidEntry) {
				t.Fatalf("Reverse = %v, want ErrInvalidEntry", err)
			}
		})
	}
}

// Reverse never rewrites the original rows, and the schema refuses UPDATE and DELETE on
// journal rows.
// rule: BIL-I-019
func TestBIL_I_019_ReverseOnlyInsertsAndJournalIsImmutable(t *testing.T) {
	mutating := regexp.MustCompile(`(?i)\b(UPDATE|DELETE|TRUNCATE)\b`)
	for i, s := range reverseStatements {
		if mutating.MatchString(s) {
			t.Errorf("statement %d mutates journal rows: %s", i, s)
		}
	}
	ddl, err := os.ReadFile("../../migrations/0001_init.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for _, want := range []string{
		`CREATE TRIGGER journal_line_immutable  BEFORE UPDATE OR DELETE ON gl.journal_line  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();`,
		`CREATE TRIGGER journal_entry_immutable BEFORE UPDATE OR DELETE ON gl.journal_entry FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();`,
	} {
		if !regexp.MustCompile(regexp.QuoteMeta(want)).Match(ddl) {
			t.Errorf("migration lacks %q", want)
		}
	}
}
