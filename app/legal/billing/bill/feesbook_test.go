package bill

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

func recordsCtx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagBillingRecordsVerified, Owner: "lawyer/LSO",
		Expiry: time.Date(2027, 12, 31, 0, 0, 0, 0, time.UTC), Doc: "fees book export"}); err != nil {
		t.Fatal(err)
	}
	if on {
		if err := s.Set(FlagBillingRecordsVerified, true); err != nil {
			t.Fatal(err)
		}
	}
	return flags.WithSet(context.Background(), s)
}

func numbered(number string, status Status, day int) Bill {
	b := pendingBill()
	b.Number, b.Status, b.ApprovalID, b.ApprovedBy = number, status, "g-"+number, "u2"
	b.IssueDate = time.Date(2026, 9, day, 0, 0, 0, 0, business)
	b.TaxableDisbCents, b.NontaxableDisbCents = 15000, 2000
	b.TaxCents = 14950
	b.TotalCents = b.FeesCents + 17000 + b.TaxCents
	return b
}

func entry(t *testing.T, b Bill) FeesBookEntry {
	t.Helper()
	e, err := NewFeesBookEntry(b)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// rule: BIL-L-040 (verify)
func TestNewFeesBookEntry(t *testing.T) {
	tests := []struct {
		name   string
		bill   Bill
		status FeesBookStatus
		err    error
	}{
		{"approved", numbered("INV-2026-0141", StatusApproved, 3), FeesBookIssued, nil},
		{"paid", numbered("INV-2026-0141", StatusPaid, 3), FeesBookIssued, nil},
		{"void keeps row", numbered("INV-2026-0142", StatusVoid, 3), FeesBookVoid, nil},
		{"pending has no row", pendingBill(), "", ErrNotApproved},
		{"void draft without number", func() Bill { b := pendingBill(); b.Status = StatusVoid; return b }(), "", ErrNotApproved},
		{"bad number", numbered("INV-26-1", StatusApproved, 3), "", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e, err := NewFeesBookEntry(tc.bill)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err != nil {
				return
			}
			b := tc.bill
			want := FeesBookEntry{Number: b.Number, BillDate: b.IssueDate, ClientID: "p1", MatterID: "m1",
				FeesCents: 100000, DisbursementsCents: 17000, TaxCents: 14950, TotalCents: 131950, Status: tc.status}
			if e != want {
				t.Fatalf("entry = %+v, want %+v", e, want)
			}
		})
	}
}

// rule: BIL-L-040 (verify)
func TestBIL_L_040_FeesBookNoGaps(t *testing.T) {
	b141 := entry(t, numbered("INV-2026-0141", StatusApproved, 3))
	b142 := entry(t, numbered("INV-2026-0142", StatusVoid, 4))
	b143 := entry(t, numbered("INV-2026-0143", StatusDelivered, 5))
	b9999 := entry(t, numbered("INV-2026-9999", StatusApproved, 28))
	b10000 := entry(t, numbered("INV-2026-10000", StatusApproved, 29))
	outside := entry(t, numbered("INV-2026-0140", StatusApproved, 1))
	sep3 := time.Date(2026, 9, 3, 0, 0, 0, 0, business)
	tests := []struct {
		name     string
		on       bool
		entries  []FeesBookEntry
		from, to time.Time
		want     []string
		void     string
		err      error
	}{
		{"void kept, number order", true, []FeesBookEntry{b143, outside, b142, b141}, sep3, sep3.AddDate(0, 0, 2),
			[]string{"INV-2026-0141", "INV-2026-0142", "INV-2026-0143"}, "INV-2026-0142", nil},
		{"wide numbers sort after narrow", true, []FeesBookEntry{b10000, b9999}, sep3, sep3.AddDate(0, 0, 27),
			[]string{"INV-2026-9999", "INV-2026-10000"}, "", nil},
		// 2026-09-05 23:00 UTC is still 2026-09-05 in Toronto, so the range ends there inclusive.
		{"to is a Toronto business date", true, []FeesBookEntry{b143}, sep3, time.Date(2026, 9, 5, 23, 0, 0, 0, time.UTC),
			[]string{"INV-2026-0143"}, "", nil},
		{"flag off fails closed", false, []FeesBookEntry{b141}, sep3, sep3, nil, "", ErrUnverified},
		{"to before from", true, []FeesBookEntry{b141}, sep3, sep3.AddDate(0, 0, -1), nil, "", ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ExportFeesBook(recordsCtx(t, tc.on), tc.entries, tc.from, tc.to)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			var nums []string
			for _, e := range got {
				nums = append(nums, e.Number)
				if (e.Number == tc.void) != (e.Status == FeesBookVoid) {
					t.Fatalf("row %s status = %s", e.Number, e.Status)
				}
			}
			if !slices.Equal(nums, tc.want) {
				t.Fatalf("rows = %v, want %v", nums, tc.want)
			}
		})
	}
}
