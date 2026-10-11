package payment

import (
	"errors"
	"testing"
)

// rule: BIL-C-065
func TestBIL_C_065_NSFRestoresBucketSplit(t *testing.T) {
	billA := Buckets{Fee: 80000, Dtx: 5000, Dnt: 2000, Tax: 11050, Interest: 300}
	billB := Buckets{Fee: 40000, Tax: 5200}
	a := Allocator{PreTax: PreTaxProportional, BillOrder: BillOldestDueFirst}
	split, err := a.Split(50000, billA)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	tests := []struct {
		name          string
		amount        int64
		reason        ReversalReason
		rows          []Row
		open          map[string]Buckets
		wantOpen      map[string]Buckets
		wantUnapplied int64
		wantErr       error
	}{
		{
			name:   "partial payment on one bill",
			amount: 50000,
			reason: ReasonNSF,
			rows:   []Row{{BillID: "a", AmountCents: 50000, Buckets: split}},
			open: map[string]Buckets{"a": {
				Fee: billA.Fee - split.Fee, Dtx: billA.Dtx - split.Dtx, Dnt: billA.Dnt - split.Dnt,
				Tax: billA.Tax - split.Tax, Interest: billA.Interest - split.Interest,
			}},
			wantOpen: map[string]Buckets{"a": billA},
		},
		{
			name:   "two bills and unapplied",
			amount: 200000,
			reason: ReasonNSF,
			rows: []Row{
				{BillID: "a", AmountCents: billA.Sum(), Buckets: billA},
				{BillID: "b", AmountCents: 1000, Buckets: Buckets{Fee: 885, Tax: 115}},
				{AmountCents: 200000 - billA.Sum() - 1000},
			},
			open:          map[string]Buckets{"a": {}, "b": {Fee: 39115, Tax: 5085}},
			wantOpen:      map[string]Buckets{"a": billA, "b": billB},
			wantUnapplied: 200000 - billA.Sum() - 1000,
		},
		{
			name:     "same bill in two rows",
			amount:   300,
			reason:   ReasonChargeback,
			rows:     []Row{{BillID: "b", AmountCents: 100, Buckets: Buckets{Fee: 100}}, {BillID: "b", AmountCents: 200, Buckets: Buckets{Tax: 200}}},
			open:     map[string]Buckets{"b": {Fee: 39900, Tax: 5000}},
			wantOpen: map[string]Buckets{"b": billB},
		},
		{
			name:    "unknown reason",
			amount:  100,
			reason:  "lost",
			rows:    []Row{{BillID: "b", AmountCents: 100, Buckets: Buckets{Fee: 100}}},
			open:    map[string]Buckets{"b": {}},
			wantErr: ErrInvalid,
		},
		{
			name:    "rows do not sum to payment",
			amount:  500,
			reason:  ReasonNSF,
			rows:    []Row{{BillID: "b", AmountCents: 100, Buckets: Buckets{Fee: 100}}},
			open:    map[string]Buckets{"b": {}},
			wantErr: ErrUnbalanced,
		},
		{
			name:    "bucket split does not sum to row",
			amount:  100,
			reason:  ReasonNSF,
			rows:    []Row{{BillID: "b", AmountCents: 100, Buckets: Buckets{Fee: 90}}},
			open:    map[string]Buckets{"b": {}},
			wantErr: ErrUnbalanced,
		},
		{
			name:    "bill unknown",
			amount:  100,
			reason:  ReasonNSF,
			rows:    []Row{{BillID: "z", AmountCents: 100, Buckets: Buckets{Fee: 100}}},
			open:    map[string]Buckets{},
			wantErr: ErrNotFound,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := map[string]Buckets{}
			for k, v := range tt.open {
				before[k] = v
			}
			got, err := ReverseRows(tt.amount, tt.reason, tt.rows, tt.open)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			for id, want := range tt.wantOpen {
				if got.Open[id] != want {
					t.Errorf("bill %s open = %+v, want %+v", id, got.Open[id], want)
				}
			}
			if got.UnappliedCents != tt.wantUnapplied {
				t.Errorf("unapplied = %d, want %d", got.UnappliedCents, tt.wantUnapplied)
			}
			if len(got.Rows) != len(tt.rows) {
				t.Fatalf("rows = %d, want %d", len(got.Rows), len(tt.rows))
			}
			var sum int64
			for i, r := range got.Rows {
				o := tt.rows[i]
				sum += r.AmountCents
				if r.BillID != o.BillID || r.AmountCents != -o.AmountCents || r.Buckets.Sum() != -o.Buckets.Sum() ||
					r.Buckets.Fee != -o.Buckets.Fee || r.Buckets.Tax != -o.Buckets.Tax || r.Buckets.Interest != -o.Buckets.Interest {
					t.Errorf("row %d = %+v, want mirror of %+v", i, r, o)
				}
			}
			if sum != -tt.amount {
				t.Errorf("reversal rows sum %d, want %d", sum, -tt.amount)
			}
			for k, v := range before {
				if tt.open[k] != v {
					t.Errorf("input open for %s modified", k)
				}
			}
		})
	}
}
