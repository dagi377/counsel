package total

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

const (
	hstON = "HST-ON"
	gst   = "GST"
	qst   = "QST"
)

func line(id string, cents int64, codes ...string) Line {
	return Line{ID: id, NetCents: cents, TaxCodes: codes}
}

type taxCase struct {
	name    string
	lines   []Line
	rates   []Rate
	want    []TaxLine
	wantErr error
}

func runTax(t *testing.T, tests []taxCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := TaxLines(moneyfake.New(), tt.lines, tt.rates)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

var on = []Rate{{Code: hstON, RatePPM: 130000}}

// rule: BIL-C-200
func TestBIL_C_200(t *testing.T) {
	runTax(t, []taxCase{
		{"once per tax line not per line: 3333+3333+3334 -> 1300 not 1299",
			[]Line{line("a", 3333, hstON), line("b", 3333, hstON), line("c", 3334, hstON)}, on,
			[]TaxLine{{Code: hstON, RatePPM: 130000, BaseCents: 10000, TaxCents: 1300}}, nil},
		{"BIL-X-003: $4,298.90 x 13% -> $558.86",
			[]Line{line("a", 429890, hstON)}, on,
			[]TaxLine{{Code: hstON, RatePPM: 130000, BaseCents: 429890, TaxCents: 55886}}, nil},
		{"unknown tax code", []Line{line("a", 100, "PST")}, on, nil, ErrUnknownTaxCode},
		{"duplicate rate", nil, append(on, on...), nil, ErrDuplicateRate},
		{"negative rate", nil, []Rate{{Code: hstON, RatePPM: -1}}, nil, ErrNegativeRate},
		{"overflow", []Line{line("a", math.MaxInt64/2, hstON)}, on, nil, money.ErrOverflow},
	})
}

// rule: BIL-T-040
func TestBIL_T_040(t *testing.T) {
	runTax(t, []taxCase{
		{"three ON lines 333, 333, 334 -> 130",
			[]Line{line("a", 333, hstON), line("b", 333, hstON), line("c", 334, hstON)}, on,
			[]TaxLine{{Code: hstON, RatePPM: 130000, BaseCents: 1000, TaxCents: 130}}, nil},
		{"ON base 429890 -> 55886",
			[]Line{line("a", 429890, hstON)}, on,
			[]TaxLine{{Code: hstON, RatePPM: 130000, BaseCents: 429890, TaxCents: 55886}}, nil},
		{"credit base -1050 -> -137",
			[]Line{line("a", -1050, hstON)}, on,
			[]TaxLine{{Code: hstON, RatePPM: 130000, BaseCents: -1050, TaxCents: -137}}, nil},
	})
}

// rule: BIL-T-041
func TestBIL_T_041(t *testing.T) {
	qc := []Rate{{Code: gst, RatePPM: 50000}, {Code: qst, RatePPM: 99750}}
	runTax(t, []taxCase{
		{"QC fees 100000 -> GST 5000, QST 9975 (no tax on tax)",
			[]Line{line("a", 100000, gst, qst)}, qc,
			[]TaxLine{
				{Code: gst, RatePPM: 50000, BaseCents: 100000, TaxCents: 5000},
				{Code: qst, RatePPM: 99750, BaseCents: 100000, TaxCents: 9975},
			}, nil},
	})
}

// rule: BIL-T-043
func TestBIL_T_043(t *testing.T) {
	runTax(t, []taxCase{
		{"interest 1250 on overdue bill -> HST 0",
			[]Line{line("int", 1250, TaxCodeNone)}, on, []TaxLine{}, nil},
		{"interest line with no code is not taxed",
			[]Line{line("fees", 100000, hstON), line("int", 1250)}, on,
			[]TaxLine{{Code: hstON, RatePPM: 130000, BaseCents: 100000, TaxCents: 13000}}, nil},
		{"early-payment discount is not a line: HST stays 13000",
			[]Line{line("fees", 100000, hstON)}, on,
			[]TaxLine{{Code: hstON, RatePPM: 130000, BaseCents: 100000, TaxCents: 13000}}, nil},
	})
}
