package reports

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

func day(s string) time.Time {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return d
}

type docs []TaxDoc

func (d docs) TaxDocs(context.Context, Period) ([]TaxDoc, error) { return d, nil }

func verifiedCtx(t *testing.T) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagBillingRecordsVerified}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagBillingRecordsVerified, true); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

var q3 = Period{From: day("2026-07-01"), To: day("2026-09-30")}

// rule: BIL-T-090
func TestBIL_T_090_HSTBillsMinusCreditNotes(t *testing.T) {
	tests := []struct {
		name   string
		period Period
		docs   docs
		want   HST
		err    error
	}{
		{name: "bills minus credit note", period: q3, docs: docs{
			{Kind: DocBill, Number: "0141", Day: day("2026-07-15"), TaxCents: 30000},
			{Kind: DocBill, Number: "0143", Day: day("2026-09-30"), TaxCents: 25886},
			{Kind: DocCredit, Number: "CN-1", Day: day("2026-08-02"), TaxCents: 1300},
		}, want: HST{Period: q3, BillTaxCents: 55886, CreditTaxCents: 1300, NetTaxCents: 54586, BillCount: 2, CreditNoteCount: 1}},
		{name: "outside period skipped", period: q3, docs: docs{
			{Kind: DocBill, Number: "0140", Day: day("2026-06-30"), TaxCents: 9999},
			{Kind: DocCredit, Number: "CN-2", Day: day("2026-10-01"), TaxCents: 500},
			{Kind: DocBill, Number: "0141", Day: day("2026-07-01"), TaxCents: 13000},
		}, want: HST{Period: q3, BillTaxCents: 13000, NetTaxCents: 13000, BillCount: 1}},
		{name: "void in same period nets to zero", period: q3, docs: docs{
			{Kind: DocBill, Number: "0142", Day: day("2026-08-01"), TaxCents: 13000, Void: true},
		}, want: HST{Period: q3}},
		{name: "empty", period: q3, want: HST{Period: q3}},
		{name: "end before start", period: Period{From: day("2026-09-30"), To: day("2026-07-01")}, err: ErrInvalidPeriod},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HSTFrom(context.Background(), tc.docs, tc.period)
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("HSTFrom() error = %v, want %v", err, tc.err)
			}
			if got != tc.want {
				t.Fatalf("HSTFrom() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// rule: BIL-L-040
func TestBIL_L_040_FeesBookListsVoids(t *testing.T) {
	in := []TaxDoc{
		{Kind: DocBill, Number: "0143", Day: day("2026-08-03"), TaxCents: 300},
		{Kind: DocBill, Number: "0141", Day: day("2026-08-01"), TaxCents: 100},
		{Kind: DocCredit, Number: "CN-1", Day: day("2026-08-02"), TaxCents: 50},
		{Kind: DocBill, Number: "0142", Day: day("2026-08-02"), TaxCents: 200, Void: true, Reason: "wrong client"},
	}
	tests := []struct {
		name string
		ctx  context.Context
		want []string
		err  error
	}{
		{name: "flag off", ctx: context.Background(), err: ErrUnverified},
		{name: "all bills in number order", ctx: verifiedCtx(t), want: []string{"0141", "0142", "0143"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := FeesBook(tc.ctx, q3, in)
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("FeesBook() error = %v, want %v", err, tc.err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("FeesBook() = %+v, want %v", got, tc.want)
			}
			for i, n := range tc.want {
				if got[i].Number != n {
					t.Fatalf("line %d = %s, want %s", i, got[i].Number, n)
				}
			}
			if len(got) == 3 && (!got[1].Void || got[1].Reason != "wrong client") {
				t.Fatalf("void line = %+v", got[1])
			}
		})
	}
}

// rule: BIL-L-041
func TestBIL_L_041_PurgeRefusedInsideRetention(t *testing.T) {
	tests := []struct {
		name  string
		ctx   context.Context
		day   string
		today string
		err   error
	}{
		{name: "flag off", ctx: context.Background(), day: "2020-03-01", today: "2030-01-01", err: ErrUnverified},
		{name: "inside six years after fye", ctx: verifiedCtx(t), day: "2020-03-01", today: "2026-10-10", err: ErrRetained},
		{name: "on retention end", ctx: verifiedCtx(t), day: "2020-03-01", today: "2026-12-31", err: ErrRetained},
		{name: "after retention end", ctx: verifiedCtx(t), day: "2020-03-01", today: "2027-01-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPurge(tc.ctx, day(tc.day), day(tc.today), time.December, 31)
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("CheckPurge() = %v, want %v", err, tc.err)
			}
		})
	}
	if got := RetainUntil(day("2020-03-01"), time.March, 31); got.Format(time.DateOnly) != "2026-03-31" {
		t.Fatalf("RetainUntil() = %s", got.Format(time.DateOnly))
	}
	if got := RetainUntil(day("2020-04-01"), time.March, 31); got.Format(time.DateOnly) != "2027-03-31" {
		t.Fatalf("RetainUntil() = %s", got.Format(time.DateOnly))
	}
}
