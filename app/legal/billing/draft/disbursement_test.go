package draft

import (
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/money"
)

// rule: BIL-C-041
func TestBIL_C_041_Markup(t *testing.T) {
	cases := []struct {
		name    string
		kind    LineKind
		cost    int64
		ppm     int64
		want    int64
		wantErr error
	}{
		{"own cost 10%", LineOwnCost, 5000, 100000, 500, nil},
		{"own cost 15% BIL-X-016", LineOwnCost, 2500, 150000, 375, nil},
		{"own cost default zero", LineOwnCost, 5000, 0, 0, nil},
		{"agent with markup", LineAgentDis, 16000, 100000, 0, ErrMarkupOnAgent},
		{"agent no markup", LineAgentDis, 16000, 0, 0, nil},
		{"fee line", LineFee, 5000, 100000, 0, nil},
		{"negative ppm", LineOwnCost, 5000, -1, 0, money.ErrOverflow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Markup(arith, tc.kind, tc.cost, tc.ppm)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("markup = %d, want %d", got, tc.want)
			}
		})
	}
}

// rule: BIL-T-033
// rule: BIL-L-048 (verify)
func TestBIL_T_033_MarkupTaxableAgentNot(t *testing.T) {
	cases := []struct {
		name     string
		d        Disbursement
		wantErr  error
		wantAmt  int64
		wantTax  int64
		taxable  bool
		wantKind LineKind
	}{
		{"own cost 5000 + 10% markup", Disbursement{ID: "a", PreTaxCents: 5000, MarkupPPM: 100000}, nil, 5500, 715, true, LineOwnCost},
		{"photocopies 1000 + 50 markup", Disbursement{ID: "b", PreTaxCents: 1000, MarkupPPM: 50000}, nil, 1050, 137, true, LineOwnCost},
		{"own cost photocopies 5000", Disbursement{ID: "c", PreTaxCents: 5000}, nil, 5000, 650, true, LineOwnCost},
		{"agent court filing 16000", Disbursement{ID: "d", Agent: true, PaidCents: 16000}, nil, 16000, 0, false, LineAgentDis},
		{"agent with markup", Disbursement{ID: "e", Agent: true, PaidCents: 16000, MarkupPPM: 100000}, ErrMarkupOnAgent, 0, 0, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := DisbursementLine(arith, tc.d)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if l.Kind != tc.wantKind || LineNet(l) != tc.wantAmt {
				t.Fatalf("line = %s %d, want %s %d", l.Kind, LineNet(l), tc.wantKind, tc.wantAmt)
			}
			if got := RebillTaxable(l.Kind); got != tc.taxable || l.Taxable != tc.taxable {
				t.Fatalf("taxable = %v/%v, want %v", got, l.Taxable, tc.taxable)
			}
			var tax int64
			if RebillTaxable(l.Kind) {
				tax, err = arith.Rhu(LineNet(l)*hstPPM, money.PPMScale)
				if err != nil {
					t.Fatal(err)
				}
			}
			if tax != tc.wantTax {
				t.Fatalf("tax = %d, want %d", tax, tc.wantTax)
			}
		})
	}
}
