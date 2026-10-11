package ledger

import (
	"errors"
	"math/rand/v2"
	"regexp"
	"strconv"
	"testing"
	"time"
)

func line(acct string, s Side, c Cents) Line { return Line{Account: acct, Side: s, Amount: c} }

// rule: BIL-I-001
func TestBIL_I_001_ValidateRejectsUnbalancedAndInvalid(t *testing.T) {
	d := Date{Year: 2026, Month: time.March, Day: 31}
	ok := []Line{line("1000", Debit, 500), line("4000", Credit, 500)}
	tests := []struct {
		name string
		e    Entry
		want error
	}{
		{"balanced", Entry{Book: "main", Date: d, SourceKind: "manual", SourceID: "s1", Lines: ok}, nil},
		{"three lines balanced", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: []Line{line("1000", Debit, 300), line("1010", Debit, 200), line("4000", Credit, 500)}}, nil},
		{"unbalanced", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: []Line{line("1000", Debit, 500), line("4000", Credit, 499)}}, ErrUnbalanced},
		{"one line", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: ok[:1]}, ErrInvalidEntry},
		{"no book", Entry{SourceKind: "k", SourceID: "s", Lines: ok}, ErrInvalidEntry},
		{"no source", Entry{Book: "main", Lines: ok}, ErrInvalidEntry},
		{"zero amount", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: []Line{line("1000", Debit, 0), line("4000", Credit, 0)}}, ErrInvalidEntry},
		{"negative amount", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: []Line{line("1000", Debit, -5), line("4000", Credit, -5)}}, ErrInvalidEntry},
		{"bad side", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: []Line{line("1000", "up", 5), line("4000", Credit, 5)}}, ErrInvalidEntry},
		{"no account", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: []Line{line("", Debit, 5), line("4000", Credit, 5)}}, ErrInvalidEntry},
		{"overflow", Entry{Book: "main", SourceKind: "k", SourceID: "s", Lines: []Line{line("1000", Debit, 1<<62), line("1000", Debit, 1<<62), line("4000", Credit, 1)}}, ErrInvalidEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := Validate(tt.e); !errors.Is(err, tt.want) {
				t.Fatalf("Validate = %v, want %v", err, tt.want)
			}
		})
	}
}

// randomEntry returns a balanced entry over accts with 2..6 lines.
func randomEntry(r *rand.Rand, accts []string, n int) Entry {
	e := Entry{Book: "main", Date: Date{Year: 2026, Month: time.Month(1 + r.IntN(12)), Day: 1 + r.IntN(28)}, SourceKind: "manual", SourceID: strconv.Itoa(n)}
	debits := 1 + r.IntN(3)
	var total Cents
	for range debits {
		c := Cents(1 + r.Int64N(1_000_000))
		total += c
		e.Lines = append(e.Lines, line(accts[r.IntN(len(accts))], Debit, c))
	}
	credits := 1 + r.IntN(3)
	left := total
	for i := range credits {
		c := left
		if i < credits-1 && left > 1 {
			c = Cents(1 + r.Int64N(int64(left-1)))
			if c >= left {
				c = left - 1
			}
		}
		if c <= 0 {
			break
		}
		left -= c
		e.Lines = append(e.Lines, line(accts[r.IntN(len(accts))], Credit, c))
	}
	return e
}

func TestRandomBalancedEntriesValidateAndNetToZero(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	accts := []string{"1000", "1100", "2000", "4000", "6000"}
	trial := map[string]Cents{}
	for n := range 500 {
		e := randomEntry(r, accts, n)
		if err := Validate(e); err != nil {
			t.Fatalf("entry %d: %v", n, err)
		}
		for _, l := range e.Lines {
			if l.Side == Debit {
				trial[l.Account] += l.Amount
			} else {
				trial[l.Account] -= l.Amount
			}
		}
	}
	var sum Cents
	for _, b := range trial {
		sum += b
	}
	if sum != 0 {
		t.Fatalf("trial balance = %d, want 0", sum)
	}
}

// rule: BIL-I-003
func TestBIL_I_003_PostOnlyInserts(t *testing.T) {
	mutating := regexp.MustCompile(`(?i)\b(UPDATE|DELETE|TRUNCATE)\b`)
	for i, s := range postStatements {
		if mutating.MatchString(s) {
			t.Errorf("statement %d mutates journal rows: %s", i, s)
		}
	}
}
