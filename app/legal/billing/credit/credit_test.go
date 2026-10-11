package credit

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

// rule: BIL-C-203
// rule: BIL-T-070
func TestBIL_C_203_CreditTaxUsesSourceBill(t *testing.T) {
	c := Calc{M: moneyfake.New()}
	tests := []struct {
		name    string
		line    TaxLine
		credit  int64
		want    int64
		wantErr error
	}{
		{"accept: 20000 on base 100000 tax 13000", TaxLine{ID: "hst", RatePPM: 130000, TaxCents: 13000, BaseCents: 100000}, 20000, 2600, nil},
		// The source line keeps its 13% snapshot; today's rate never enters the formula.
		{"source rate after rate change", TaxLine{ID: "hst", RatePPM: 130000, TaxCents: 13000, BaseCents: 100000}, 20000, 2600, nil},
		{"BIL-X-011 tiny base rounds up", TaxLine{ID: "hst", TaxCents: 1, BaseCents: 5}, 3, 1, nil},
		{"exhausting credit takes the remainder", TaxLine{ID: "hst", TaxCents: 131, BaseCents: 1010, CreditedBase: 680, CreditedCents: 88}, 330, 43, nil},
		{"never more than remaining tax", TaxLine{ID: "hst", TaxCents: 1, BaseCents: 5, CreditedBase: 3, CreditedCents: 1}, 1, 0, nil},
		{"beyond remaining base", TaxLine{ID: "hst", TaxCents: 13000, BaseCents: 100000, CreditedBase: 90000}, 20000, 0, ErrExceedsBalance},
		{"zero credit", TaxLine{ID: "hst", TaxCents: 13000, BaseCents: 100000}, 0, 0, ErrInvalidAmount},
		{"overflow", TaxLine{ID: "hst", TaxCents: math.MaxInt64, BaseCents: math.MaxInt64}, 2, 0, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.LineTax(tt.line, tt.credit)
			if tt.name == "overflow" {
				if err == nil {
					t.Fatal("want overflow error")
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("LineTax = %d, want %d", got, tt.want)
			}
		})
	}
}

// rule: BIL-C-082
// rule: BIL-I-014
func TestBIL_C_082_CreditLimitPerBucket(t *testing.T) {
	tests := []struct {
		name    string
		b       Bucket
		credit  int64
		wantErr error
	}{
		{"within", Bucket{Key: "fees", BilledCents: 100000, CreditedCents: 50000}, 50000, nil},
		{"prior CN 900 then 200 on 1000", Bucket{Key: "fees", BilledCents: 100000, CreditedCents: 90000}, 20000, ErrExceedsBalance},
		{"write-off counts", Bucket{Key: "fees", BilledCents: 100000, WrittenOff: 100000}, 1, ErrExceedsBalance},
		{"zero", Bucket{Key: "fees", BilledCents: 100000}, 0, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckBucket(tt.b, tt.credit); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// rule: BIL-C-082
func TestBIL_C_082_CreditExceedsBillIs409(t *testing.T) {
	tests := []struct {
		name   string
		b      Bucket
		credit int64
	}{
		{"prior CN 900 then 200 on 1000", Bucket{Key: "fees", BilledCents: 100000, CreditedCents: 90000}, 20000},
		{"write-off counts", Bucket{Key: "fees", BilledCents: 100000, WrittenOff: 100000}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var p *api.Problem
			if err := CheckBucket(tt.b, tt.credit); !errors.As(err, &p) {
				t.Fatalf("err = %v, want *api.Problem in chain", err)
			}
			if p.Status != http.StatusConflict || p.Code != CodeCreditExceedsBill || p.Rule != "BIL-C-082" {
				t.Fatalf("problem = %+v, want 409 %s BIL-C-082", p, CodeCreditExceedsBill)
			}
		})
	}
}

// rule: BIL-C-080
func TestBIL_C_080_CreditNoteKinds(t *testing.T) {
	tests := []struct {
		k    Kind
		want bool
	}{
		{KindFeeReduction, true}, {KindBillingError, true}, {KindBadDebtWriteOff, true}, {KindRefund, true},
		{"discount", false}, {"", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.k), func(t *testing.T) {
			if got := tt.k.Valid(); got != tt.want {
				t.Fatalf("Valid(%q) = %v, want %v", tt.k, got, tt.want)
			}
		})
	}
}

// rule: BIL-C-081 (verify)
func TestBIL_C_081_CreditRouting(t *testing.T) {
	tests := []struct {
		name               string
		off                bool
		total, open, trust int64
		want               Route
		wantErr            error
	}{
		{"BIL-X-010 paid bill", false, 395500, 0, 300000, Route{TrustCents: 300000, ClientCents: 95500}, nil},
		{"AR first", false, 50000, 30000, 100000, Route{ARCents: 30000, TrustCents: 20000}, nil},
		{"all AR", false, 10000, 30000, 0, Route{ARCents: 10000}, nil},
		{"zero", false, 0, 0, 0, Route{}, ErrInvalidAmount},
		{"flag off paid bill refused", true, 395500, 0, 300000, Route{}, ErrUnverified},
		{"flag off excess over AR refused", true, 50000, 30000, 100000, Route{}, ErrUnverified},
		{"flag off excess to client refused", true, 50000, 30000, 0, Route{}, ErrUnverified},
		{"flag off within AR routed", true, 10000, 30000, 0, Route{ARCents: 10000}, nil},
		{"flag off exactly AR routed", true, 30000, 30000, 5000, Route{ARCents: 30000}, nil},
		{"flag off zero still invalid", true, 0, 0, 0, Route{}, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RouteCredit(flagCtx(t, FlagCreditRouteVerified, !tt.off), tt.total, tt.open, tt.trust)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("RouteCredit = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// rule: BIL-C-085
// rule: BIL-T-080
func TestBIL_C_085_WriteOffBadDebtTax(t *testing.T) {
	c := Calc{M: moneyfake.New()}
	tests := []struct {
		name                  string
		off, tax, total, want int64
		wantErr               error
	}{
		{"whole bill", 113000, 13000, 113000, 13000, nil},
		{"part of bill", 56500, 13000, 113000, 6500, nil},
		{"rounds half away from zero", 1, 1, 2, 1, nil},
		{"more than bill", 113001, 13000, 113000, 0, ErrExceedsBalance},
		{"zero", 0, 13000, 113000, 0, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.BadDebtTax(tt.off, tt.tax, tt.total)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("BadDebtTax = %d, want %d", got, tt.want)
			}
		})
	}
}

// rule: BIL-C-086
// rule: BIL-T-081
func TestBIL_C_086_RecoveryAfterWriteOff(t *testing.T) {
	c := Calc{M: moneyfake.New()}
	tests := []struct {
		name                  string
		rec, left, tax, total int64
		want                  Recovery
		wantErr               error
	}{
		{"BIL-X-030 $118 -> tax $13", 11800, 11800, 1300, 11800, Recovery{TaxCents: 1300, PreTaxCents: 10500}, nil},
		{"partial recovery", 22600, 113000, 13000, 113000, Recovery{TaxCents: 2600, PreTaxCents: 20000}, nil},
		{"beyond written-off", 11801, 11800, 1300, 11800, Recovery{}, ErrExceedsBalance},
		{"zero", 0, 11800, 1300, 11800, Recovery{}, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.RecoverAfterWriteOff(tt.rec, tt.left, tt.tax, tt.total)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("Recover = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// rule: BIL-T-071
func TestBIL_T_071_OnlyDeliveredBillsTakeCredits(t *testing.T) {
	tests := []struct {
		name    string
		b       Bill
		wantErr error
	}{
		{"delivered", Bill{ID: "b1", Delivered: true}, nil},
		{"draft is edited, not credited", Bill{ID: "b1"}, ErrBillNotDelivered},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckDelivered(tt.b); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// flagCtx returns a context whose flag set has name defined, and turned on when on.
func flagCtx(t *testing.T, name string, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: name, Owner: "test", Expiry: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(name, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}
