package draft

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
)

func discountLines() []Line {
	return []Line{
		{ID: "fee", Kind: LineFee, SortOrder: 1, AmountCents: 100000, Taxable: true},
		{ID: "own", Kind: LineOwnCost, SortOrder: 2, AmountCents: 20000, Taxable: true},
		{ID: "agent", Kind: LineAgentDis, SortOrder: 3, AmountCents: 7000},
	}
}

// rule: BIL-C-020
// rule: BIL-C-021
// rule: BIL-C-022
func TestBIL_C_020_BillDiscountSpread(t *testing.T) {
	tests := []struct {
		name      string
		lines     []Line
		d         Discount
		wantTotal int64
		want      map[string]int64 // line id -> DiscountShareCents
	}{
		{"10% on fees+own; agent untouched", discountLines(),
			Discount{Kind: DiscountPercent, PPM: 100000, Base: BaseFeesAndOwn}, 12000,
			map[string]int64{"fee": 10000, "own": 2000, "agent": 0}},
		{"10% on fees only", discountLines(),
			Discount{Kind: DiscountPercent, PPM: 100000, Base: BaseFeesOnly}, 10000,
			map[string]int64{"fee": 10000, "own": 0, "agent": 0}},
		{"one line", discountLines(),
			Discount{Kind: DiscountPercent, PPM: 100000, Base: BaseOneLine, LineID: "own"}, 2000,
			map[string]int64{"fee": 0, "own": 2000, "agent": 0}},
		{"BIL-X-005: $100 over $1,200 / $360 by largest remainder",
			[]Line{
				{ID: "a", Kind: LineFee, SortOrder: 1, AmountCents: 144500, Reduction: &Reduction{Cents: 24500, Reason: ReasonCourtesy}},
				{ID: "b", Kind: LineFee, SortOrder: 2, AmountCents: 36000},
			},
			Discount{Kind: DiscountFixed, AmountCents: 10000, Base: BaseFeesAndOwn}, 10000,
			map[string]int64{"a": 7692, "b": 2308}},
		{"rhu: 10% of $2,680.50 = $268.05",
			[]Line{{ID: "a", Kind: LineFee, SortOrder: 1, AmountCents: 268050}},
			Discount{Kind: DiscountPercent, PPM: 100000, Base: BaseFeesOnly}, 26805,
			map[string]int64{"a": 26805}},
		{"tie goes to earliest line",
			[]Line{
				{ID: "late", Kind: LineFee, SortOrder: 2, AmountCents: 1000},
				{ID: "early", Kind: LineFee, SortOrder: 1, AmountCents: 1000},
			},
			Discount{Kind: DiscountFixed, AmountCents: 1, Base: BaseFeesOnly}, 1,
			map[string]int64{"early": 1, "late": 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, err := DiscountAmount(money.Default, tt.lines, tt.d)
			if err != nil || total != tt.wantTotal {
				t.Fatalf("DiscountAmount = %d, %v; want %d", total, err, tt.wantTotal)
			}
			got, err := ApplyDiscount(money.Default, tt.lines, tt.d)
			if err != nil {
				t.Fatalf("ApplyDiscount: %v", err)
			}
			var sum int64
			for _, l := range got {
				if l.DiscountShareCents != tt.want[l.ID] {
					t.Errorf("line %s share = %d, want %d", l.ID, l.DiscountShareCents, tt.want[l.ID])
				}
				sum += l.DiscountShareCents
			}
			if sum != tt.wantTotal {
				t.Fatalf("sum of shares = %d, want %d", sum, tt.wantTotal)
			}
			for _, l := range tt.lines {
				if l.DiscountShareCents != 0 {
					t.Fatalf("input line %s changed", l.ID)
				}
			}
		})
	}
}

// rule: BIL-C-021
func TestBIL_C_021_DiscountNeverNegative(t *testing.T) {
	tests := []struct {
		name      string
		lines     []Line
		d         Discount
		wantTotal int64
		wantErr   error
	}{
		{"fixed $5,000 on a $1,000 base -> $1,000",
			[]Line{{ID: "a", Kind: LineFee, SortOrder: 1, AmountCents: 100000}, {ID: "g", Kind: LineAgentDis, SortOrder: 2, AmountCents: 50000}},
			Discount{Kind: DiscountFixed, AmountCents: 500000, Base: BaseFeesAndOwn}, 100000, nil},
		{"100% leaves lines at zero",
			discountLines(), Discount{Kind: DiscountPercent, PPM: 1000000, Base: BaseFeesAndOwn}, 120000, nil},
		{"over 100% rejected",
			discountLines(), Discount{Kind: DiscountPercent, PPM: 1000001, Base: BaseFeesAndOwn}, 0, ErrInvalidDiscount},
		{"zero percent rejected",
			discountLines(), Discount{Kind: DiscountPercent, PPM: 0, Base: BaseFeesAndOwn}, 0, ErrInvalidDiscount},
		{"negative fixed rejected",
			discountLines(), Discount{Kind: DiscountFixed, AmountCents: -1, Base: BaseFeesAndOwn}, 0, ErrInvalidDiscount},
		{"unknown kind",
			discountLines(), Discount{Kind: "bogus", PPM: 1, Base: BaseFeesAndOwn}, 0, ErrInvalidDiscount},
		{"unknown base",
			discountLines(), Discount{Kind: DiscountPercent, PPM: 1, Base: "all"}, 0, ErrInvalidDiscount},
		{"one line on agent disbursement",
			discountLines(), Discount{Kind: DiscountPercent, PPM: 100000, Base: BaseOneLine, LineID: "agent"}, 0, ErrInvalidDiscount},
		{"one line without id",
			discountLines(), Discount{Kind: DiscountPercent, PPM: 100000, Base: BaseOneLine}, 0, ErrInvalidDiscount},
		{"already negative eligible line",
			[]Line{{ID: "a", Kind: LineFee, SortOrder: 1, AmountCents: 100, LineDiscountCents: 200}},
			Discount{Kind: DiscountFixed, AmountCents: 1, Base: BaseFeesOnly}, 0, ErrNegativeLine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ApplyDiscount(money.Default, tt.lines, tt.d)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			var sum int64
			for _, l := range got {
				if LineNet(l) < 0 {
					t.Fatalf("line %s net %d < 0", l.ID, LineNet(l))
				}
				if l.Kind == LineAgentDis && l.DiscountShareCents != 0 {
					t.Fatalf("agent line %s discounted", l.ID)
				}
				sum += l.DiscountShareCents
			}
			if sum != tt.wantTotal {
				t.Fatalf("total discount = %d, want %d", sum, tt.wantTotal)
			}
		})
	}
}
