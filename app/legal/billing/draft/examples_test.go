package draft

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

const hstPPM = 130000

var arith = moneyfake.New()

// hst is the bill tax on the summed taxable base, rounded once (BIL-C-200).
func hst(t *testing.T, lines []Line) int64 {
	t.Helper()
	var base int64
	for _, l := range lines {
		if l.Kind != LineAgentDis {
			base += LineNet(l)
		}
	}
	v, err := arith.Rhu(base*hstPPM, money.PPMScale)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func total(lines []Line) int64 {
	var n int64
	for _, l := range lines {
		n += LineNet(l)
	}
	return n
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func fee(id string, cents int64, order int) Line {
	return Line{ID: id, Kind: LineFee, AmountCents: cents, SortOrder: order, Taxable: true}
}

// TestGoldenExamples runs the b3 examples that a draft bill computes, inputs -> exact cents.
func TestGoldenExamples(t *testing.T) {
	line := must[Line](t)
	lines := must[[]Line](t)
	tests := []struct {
		name               string
		build              func() []Line
		wantShares         []int64
		wantHST, wantTotal int64
	}{
		{"BIL-X-003 Ontario bill, HST once per bill", func() []Line {
			return []Line{
				fee("fees", 421250, 1),
				line(DisbursementLine(arith, Disbursement{ID: "copies", PreTaxCents: 8640, SortOrder: 2})),
				line(DisbursementLine(arith, Disbursement{ID: "filing", Agent: true, PaidCents: 22900, SortOrder: 3})),
			}
		}, []int64{0, 0, 0}, 55886, 508676},
		{"BIL-X-004 10% discount on fees spread by largest remainder", func() []Line {
			ls := []Line{fee("a", 144500, 1), fee("b", 36000, 2), fee("c", 87550, 3)}
			d := Discount{Kind: DiscountPercent, PPM: 100000, Base: BaseFeesOnly}
			amt := must[int64](t)(PercentOf(arith, 268050, d.PPM))
			return lines(SpreadDiscount(arith, ls, d, amt))
		}, []int64{14450, 3600, 8755}, 31362, 272607},
		{"BIL-X-005 write-down first, then fixed $100 discount", func() []Line {
			l1 := fee("a", 144500, 1)
			l1.Reduction = &Reduction{Cents: 24500, Reason: ReasonInefficiency}
			ls := []Line{l1, fee("b", 36000, 2)}
			return lines(SpreadDiscount(arith, ls, Discount{Kind: DiscountFixed, AmountCents: 10000, Base: BaseFeesAndOwn}, 10000))
		}, []int64{7692, 2308}, 18980, 164980},
		{"BIL-X-016 flat milestone, out-of-scope hourly, marked-up soft cost", func() []Line {
			if err := OutOfScope(true, false); err != nil {
				t.Fatal(err)
			}
			return []Line{
				line(FlatMilestone(arith, "m1", 300000, 500000, 1)),
				fee("oos", 48000, 2), // 96 min x $300/h
				line(DisbursementLine(arith, Disbursement{ID: "courier", PreTaxCents: 2500, MarkupPPM: 150000, SortOrder: 3})),
			}
		}, []int64{0, 0, 0}, 26114, 226989},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ls := tt.build()
			var shares []int64
			for _, l := range ls {
				shares = append(shares, l.DiscountShareCents)
			}
			if !reflect.DeepEqual(shares, tt.wantShares) {
				t.Fatalf("shares = %v, want %v", shares, tt.wantShares)
			}
			h := hst(t, ls)
			if h != tt.wantHST || total(ls)+h != tt.wantTotal {
				t.Fatalf("hst %d total %d, want %d %d", h, total(ls)+h, tt.wantHST, tt.wantTotal)
			}
		})
	}
}

// rule: BIL-C-022
func TestBIL_C_022(t *testing.T) {
	tests := []struct {
		name    string
		lines   []Line
		amount  int64
		want    []int64
		wantErr error
	}{
		{"BIL-X-005 $100 over $1,200 / $360", []Line{fee("a", 120000, 1), fee("b", 36000, 2)}, 10000, []int64{7692, 2308}, nil},
		{"tie goes to earliest sort order", []Line{fee("b", 100, 2), fee("a", 100, 1)}, 1, []int64{0, 1}, nil},
		{"discount above base", []Line{fee("a", 100, 1)}, 101, nil, ErrNegativeLine},
		{"no eligible line", []Line{{ID: "x", Kind: LineAgentDis, AmountCents: 100}}, 1, nil, ErrInvalidDiscount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SpreadDiscount(arith, tt.lines, Discount{Base: BaseFeesOnly}, tt.amount)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			for i, w := range tt.want {
				if got[i].DiscountShareCents != w {
					t.Fatalf("line %d share = %d, want %d", i, got[i].DiscountShareCents, w)
				}
			}
			if err == nil && tt.lines[0].DiscountShareCents != 0 {
				t.Fatal("input mutated")
			}
		})
	}
}

// rule: BIL-C-023
func TestBIL_C_023(t *testing.T) {
	tests := []struct {
		r       Reduction
		wantErr error
	}{
		{Reduction{Reason: ""}, ErrReasonRequired},
		{Reduction{Reason: ReasonOther}, ErrReasonRequired},
		{Reduction{Reason: ReasonOther, ReasonText: "block billing"}, nil},
		{Reduction{Reason: ReasonCourtesy, ShowOnBill: true}, nil},
		{Reduction{Reason: "bogus"}, ErrReasonRequired},
	}
	for _, tt := range tests {
		t.Run(string(tt.r.Reason), func(t *testing.T) {
			if err := CheckReason(tt.r); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: BIL-C-030
func TestBIL_C_030(t *testing.T) {
	tests := []struct {
		flat, ppm, want int64
	}{
		{300000, 500000, 150000}, // BIL-X-016: 50% of $3,000
		{100001, 500000, 50001},  // half a cent rounds away from zero
	}
	for _, tt := range tests {
		l, err := FlatMilestone(arith, "m", tt.flat, tt.ppm, 1)
		if err != nil || l.AmountCents != tt.want || l.Kind != LineFlat {
			t.Fatalf("FlatMilestone(%d, %d) = %+v, %v; want %d", tt.flat, tt.ppm, l, err, tt.want)
		}
	}
}

// rule: BIL-C-031
func TestBIL_C_031(t *testing.T) {
	tests := []struct {
		name            string
		hybrid, changed bool
		wantErr         error
	}{
		{"hybrid billed hourly", true, false, nil},
		{"pure flat blocked", false, false, ErrOutOfScope},
		{"pure flat after change-of-scope letter", false, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := OutOfScope(tt.hybrid, tt.changed); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

type disbCase struct {
	name      string
	d         Disbursement
	wantLine  int64
	wantHST   int64
	wantErr   error
	wantTaxed bool
}

func runDisb(t *testing.T, tests []disbCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, err := DisbursementLine(arith, tt.d)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if l.AmountCents != tt.wantLine || l.Taxable != tt.wantTaxed || hst(t, []Line{l}) != tt.wantHST {
				t.Fatalf("line %+v hst %d, want %d hst %d", l, hst(t, []Line{l}), tt.wantLine, tt.wantHST)
			}
		})
	}
}

// rule: BIL-C-040
func TestBIL_C_040(t *testing.T) {
	runDisb(t, []disbCase{
		{"court filing fee agent: no HST", Disbursement{ID: "f", Agent: true, PaidCents: 22900}, 22900, 0, nil, false},
		{"photocopies own: HST 11.23", Disbursement{ID: "p", PreTaxCents: 8640}, 8640, 1123, nil, true},
	})
}

// rule: BIL-T-030
func TestBIL_T_030(t *testing.T) {
	runDisb(t, []disbCase{
		{"title search own: HST 455", Disbursement{ID: "t", PreTaxCents: 3500}, 3500, 455, nil, true},
	})
}

// rule: BIL-T-031
func TestBIL_T_031(t *testing.T) {
	runDisb(t, []disbCase{
		{"courier 2500 + vendor HST: bill pre-tax, HST 325", Disbursement{ID: "c", PreTaxCents: 2500, PaidCents: 2825}, 2500, 325, nil, true},
	})
}

// rule: BIL-T-032
func TestBIL_T_032(t *testing.T) {
	runDisb(t, []disbCase{
		{"land transfer tax billed exact", Disbursement{ID: "l", Agent: true, PaidCents: 1247500}, 1247500, 0, nil, false},
		{"markup on court filing fee rejected", Disbursement{ID: "f", Agent: true, PaidCents: 22900, MarkupPPM: 100000}, 0, 0, ErrMarkupOnAgent, false},
	})
}

// rule: BIL-C-043
func TestBIL_C_043(t *testing.T) {
	tests := []struct {
		name          string
		minor, fx     int64
		want          int64
		wantErr       error
		viaDisburseEx bool
	}{
		{"USD 100.00 at 1.372500", 10000, 1372500, 13725, nil, false},
		{"own cost in USD", 10000, 1372500, 13725, nil, true},
		{"zero rate", 10000, 0, 0, ErrInvalidDiscount, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got int64
			var err error
			if tt.viaDisburseEx {
				var l Line
				l, err = DisbursementLine(arith, Disbursement{ID: "x", OriginalMinor: tt.minor, OriginalCurrency: "USD", FXPPM: tt.fx})
				got = l.AmountCents
			} else {
				got, err = FXCents(arith, tt.minor, tt.fx)
			}
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("got %d, %v; want %d, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

// rule: BIL-T-042
func TestBIL_T_042(t *testing.T) {
	t.Run("line discount reduces base", func(t *testing.T) {
		f := fee("f", 100000, 1)
		f.LineDiscountCents = 5000
		ls := []Line{f, {ID: "o", Kind: LineOwnCost, AmountCents: 10000, SortOrder: 2}}
		if got := hst(t, ls); got != 13650 {
			t.Fatalf("hst = %d, want 13650", got)
		}
	})
	t.Run("bill discount skips agent disbursements", func(t *testing.T) {
		ls := []Line{fee("a", 60000, 1), {ID: "b", Kind: LineOwnCost, AmountCents: 40000, SortOrder: 2}, {ID: "ag", Kind: LineAgentDis, AmountCents: 50000, SortOrder: 3}}
		got, err := SpreadDiscount(arith, ls, Discount{Base: BaseFeesAndOwn}, 10000)
		if err != nil {
			t.Fatal(err)
		}
		want := []int64{6000, 4000, 0}
		for i, w := range want {
			if got[i].DiscountShareCents != w {
				t.Fatalf("line %s share %d, want %d", got[i].ID, got[i].DiscountShareCents, w)
			}
		}
	})
}

// rule: BIL-L-028
func TestBIL_L_028(t *testing.T) {
	tests := []struct {
		name    string
		lines   []Line
		want    Sections
		wantErr error
	}{
		{"mixed line blocked", []Line{{ID: "m", Kind: "mixed", AmountCents: 150000}}, Sections{}, ErrDeliveryBlocked},
		{"fees and disbursements shown separately", []Line{
			fee("f", 100000, 1),
			{ID: "o", Kind: LineOwnCost, AmountCents: 15000},
			{ID: "a", Kind: LineAgentDis, AmountCents: 5000},
		}, Sections{FeesCents: 100000, TaxableDisbCents: 15000, NonTaxableDisCents: 5000}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitSections(tt.lines)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("got %+v, %v; want %+v, %v", got, err, tt.want, tt.wantErr)
			}
			if err == nil {
				if h := hst(t, tt.lines); h != 14950 || total(tt.lines)+h != 134950 {
					t.Fatalf("hst %d total %d, want 14950 134950", h, total(tt.lines)+h)
				}
			}
		})
	}
}
