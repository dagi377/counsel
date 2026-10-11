package reports

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/timekeeping/rates"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/money"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

type agingSource struct {
	bills   []OpenBill
	ledger  int64
	gotCode string
}

func (s *agingSource) OpenBills(context.Context, time.Time) ([]OpenBill, error) {
	return s.bills, nil
}

func (s *agingSource) AccountBalance(_ context.Context, code string, _ time.Time) (int64, error) {
	s.gotCode = code
	return s.ledger, nil
}

func TestAgingBucketsSumToOP1100(t *testing.T) {
	loc := toronto(t)
	asOf := time.Date(2026, time.June, 30, 0, 0, 0, 0, loc)
	daysAgo := func(n int) time.Time { return asOf.AddDate(0, 0, -n) }
	bills := []OpenBill{
		{BillID: "b0", BilledOn: daysAgo(0), BalanceCents: 100},
		{BillID: "b30", BilledOn: daysAgo(30), BalanceCents: 200},
		{BillID: "b31", BilledOn: daysAgo(31), BalanceCents: 400},
		{BillID: "b60", BilledOn: daysAgo(60), BalanceCents: 800},
		{BillID: "b61", BilledOn: daysAgo(61), BalanceCents: 1_600},
		{BillID: "b90", BilledOn: daysAgo(90), BalanceCents: 3_200},
		{BillID: "b91", BilledOn: daysAgo(91), BalanceCents: 6_400},
		{BillID: "future", BilledOn: asOf.AddDate(0, 0, 1), BalanceCents: 99_999},
	}
	tests := []struct {
		name    string
		ledger  int64
		buckets [4]int64
		wantErr error
	}{
		{name: "reconciles", ledger: 12_700, buckets: [4]int64{300, 1_200, 4_800, 6_400}},
		{name: "out of balance", ledger: 12_699, buckets: [4]int64{300, 1_200, 4_800, 6_400}, wantErr: ErrOutOfBalance},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := &agingSource{bills: bills, ledger: tc.ledger}
			got, err := AgingReporter{Source: src}.Aging(context.Background(), asOf)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got.Buckets != tc.buckets {
				t.Errorf("buckets = %v, want %v", got.Buckets, tc.buckets)
			}
			var sum int64
			for _, b := range got.Buckets {
				sum += b
			}
			if sum != got.TotalCents {
				t.Errorf("buckets sum %d, total %d", sum, got.TotalCents)
			}
			if src.gotCode != books.AcctARFees {
				t.Errorf("ledger code = %q, want %q", src.gotCode, books.AcctARFees)
			}
		})
	}
}

func TestBucketFor(t *testing.T) {
	tests := []struct {
		days int
		want AgingBucket
	}{
		{0, Days0To30}, {30, Days0To30}, {31, Days31To60}, {60, Days31To60},
		{61, Days61To90}, {90, Days61To90}, {91, DaysOver90}, {400, DaysOver90},
	}
	for _, tc := range tests {
		if got := BucketFor(tc.days); got != tc.want {
			t.Errorf("BucketFor(%d) = %d, want %d", tc.days, got, tc.want)
		}
	}
}

type wipSource struct{ entries []WIPEntry }

func (s wipSource) ApprovedUnbilled(context.Context, time.Time) ([]WIPEntry, error) {
	return s.entries, nil
}

func TestWIPEqualsApprovedUnbilledAtResolvedRates(t *testing.T) {
	loc := toronto(t)
	asOf := time.Date(2026, time.March, 31, 17, 0, 0, 0, loc)
	d := func(day int) clock.Date { return clock.Date{Year: 2026, Month: time.March, Day: day} }
	store := rates.NewStore(money.Default)
	rows := []rates.RateRow{
		{Source: rates.SourceUserDefault, UserID: "u1", RateCents: 30_000, EffectiveFrom: d(1)},
		{Source: rates.SourceUserDefault, UserID: "u1", RateCents: 36_000, EffectiveFrom: d(15)},
		{Source: rates.SourceMatterUser, MatterID: "m2", UserID: "u1", RateCents: 20_000, EffectiveFrom: d(1)},
		{Source: rates.SourceFlatActivity, ActivityID: "filing", RateCents: 7_500, EffectiveFrom: d(1)},
	}
	for _, r := range rows {
		if err := store.Put(context.Background(), r); err != nil {
			t.Fatal(err)
		}
	}
	manual := int64(10_000)
	in := func(matter string, day int) rates.Input {
		return rates.Input{MatterID: matter, ClientID: "c1", UserID: "u1", WorkDate: d(day)}
	}
	tests := []struct {
		name    string
		entries []WIPEntry
		lines   []WIPLine
		total   int64
	}{
		{
			name: "rates by work date and tier",
			entries: []WIPEntry{
				{MatterID: "m1", BilledMinutes: 60, Input: in("m1", 10)},  // 300.00/h
				{MatterID: "m1", BilledMinutes: 30, Input: in("m1", 20)},  // 360.00/h -> 180.00
				{MatterID: "m2", BilledMinutes: 6, Input: in("m2", 20)},   // matter rate 200.00/h -> 20.00
				{MatterID: "m0", BilledMinutes: 999, Input: in("m0", 31)}, // 360.00/h -> 5,994.00
			},
			lines: []WIPLine{{MatterID: "m0", Cents: 599_400}, {MatterID: "m1", Cents: 48_000}, {MatterID: "m2", Cents: 2_000}},
			total: 649_400,
		},
		{
			name: "flat activity and manual rate",
			entries: []WIPEntry{
				{MatterID: "m3", BilledMinutes: 45, Input: rates.Input{MatterID: "m3", UserID: "u1", ActivityID: "filing", WorkDate: d(5)}},
				{MatterID: "m3", BilledMinutes: 7, Input: rates.Input{MatterID: "m3", UserID: "u1", WorkDate: d(5), ManualRateCents: &manual, CanSetRates: true}},
			},
			// 7_500 flat + rhu(7*10_000, 60) = 7_500 + 1_167
			lines: []WIPLine{{MatterID: "m3", Cents: 8_667}},
			total: 8_667,
		},
		{
			name:    "work after as-of left out",
			entries: []WIPEntry{{MatterID: "m1", BilledMinutes: 60, Input: rates.Input{MatterID: "m1", UserID: "u1", WorkDate: clock.Date{Year: 2026, Month: time.April, Day: 1}}}},
			total:   0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, err := WIPReporter{Source: wipSource{tc.entries}, Rates: store}.WIP(context.Background(), asOf)
			if err != nil {
				t.Fatal(err)
			}
			if w.TotalCents != tc.total {
				t.Errorf("total = %d, want %d", w.TotalCents, tc.total)
			}
			if len(w.Lines) != len(tc.lines) {
				t.Fatalf("lines = %v, want %v", w.Lines, tc.lines)
			}
			for i := range w.Lines {
				if w.Lines[i] != tc.lines[i] {
					t.Errorf("line %d = %v, want %v", i, w.Lines[i], tc.lines[i])
				}
			}
		})
	}
}
