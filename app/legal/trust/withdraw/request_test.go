package withdraw

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

func verifiedCtx(t *testing.T) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagBylaw9Verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagBylaw9Verified, true); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

func req(mut func(*Request)) Request {
	r := Request{TrustAccountID: "acc1", MatterID: "m1", AmountCents: 100000, Payee: "Acme Realty",
		Method: MethodCheque, Reason: "closing", PreparedBy: "u-clerk"}
	if mut != nil {
		mut(&r)
	}
	return r
}

func TestValidateFlagOff(t *testing.T) {
	err := Validate(context.Background(), req(nil), Funds{ClearedCents: 500000})
	if !errors.Is(err, ErrUnverified) {
		t.Fatalf("got %v, want ErrUnverified", err)
	}
}

// rule: BIL-L-037
func TestBIL_L_037_NoCashOrBearerPayee(t *testing.T) {
	ctx := verifiedCtx(t)
	funds := Funds{ClearedCents: 500000}
	cases := []struct {
		name  string
		payee string
		want  error
	}{
		{"cash", "Cash", ErrBearerPayee},
		{"bearer", " bearer ", ErrBearerPayee},
		{"empty", "", ErrInvalid},
		{"named payee", "Acme Realty", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(ctx, req(func(r *Request) { r.Payee = c.payee; r.AmountCents = 10000 }), funds)
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
	if err := Validate(ctx, req(func(r *Request) { r.Method = "cash" }), funds); !errors.Is(err, ErrBearerPayee) {
		t.Fatalf("cash method: got %v", err)
	}
}

// rule: BIL-C-053
func TestBIL_C_053_AvailableBalance(t *testing.T) {
	ctx := verifiedCtx(t)
	cases := []struct {
		name   string
		amount int64
		funds  Funds
		want   error
	}{
		{"uncleared cheque receipt", 100000, Funds{UnclearedCents: 500000}, ErrUnclearedFunds},
		{"protected over limit", 300001, Funds{ClearedCents: 500000, ProtectedCents: 200000}, ErrTrustOverdraw},
		{"protected at limit", 300000, Funds{ClearedCents: 500000, ProtectedCents: 200000}, nil},
		{"minimum and authorized", 100001, Funds{ClearedCents: 300000, MinimumCents: 100000, AuthorizedCents: 100000}, ErrTrustOverdraw},
		{"beyond uncleared too", 700000, Funds{ClearedCents: 100000, UnclearedCents: 100000}, ErrTrustOverdraw},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(ctx, req(func(r *Request) { r.AmountCents = c.amount }), c.funds)
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// rule: BIL-L-035
func TestBIL_L_035_FeesOnlyForDeliveredBill(t *testing.T) {
	ctx := verifiedCtx(t)
	on := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	delivered := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		amount int64
		bill   FeeBill
		want   error
	}{
		{"not delivered", 300000, FeeBill{BalanceDueCents: 300000}, ErrBillNotDelivered},
		{"delivered after date", 300000, FeeBill{DeliveredAt: on.AddDate(0, 0, 1), BalanceDueCents: 300000}, ErrBillNotDelivered},
		{"delivered at balance", 300000, FeeBill{DeliveredAt: delivered, BalanceDueCents: 300000}, nil},
		{"over balance", 300001, FeeBill{DeliveredAt: delivered, BalanceDueCents: 300000}, ErrExceedsBillBalance},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateFee(ctx, req(func(r *Request) { r.AmountCents = c.amount }), c.bill, on)
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// rule: BIL-L-038
func TestBIL_L_038_EFTAuthorization(t *testing.T) {
	ctx := verifiedCtx(t)
	funds := Funds{ClearedCents: 500000}
	cases := []struct {
		name   string
		r      Request
		signer Signer
		want   error
	}{
		{"eft without form 9A", req(func(r *Request) { r.Method = MethodEFT }), Signer{UserID: "u-lawyer", TrustSigning: true}, ErrNoEFTAuthorization},
		{"signer without authority", req(func(r *Request) { r.Method = MethodEFT; r.EFTAuthorizationID = "f9a" }), Signer{UserID: "u-clerk2"}, ErrSignerNotAuthorized},
		{"signing lawyer", req(func(r *Request) { r.Method = MethodEFT; r.EFTAuthorizationID = "f9a" }), Signer{UserID: "u-lawyer", TrustSigning: true}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Validate(ctx, c.r, funds)
			if err == nil {
				err = ValidateAuthorizer(ctx, Withdrawal{Request: c.r}, c.signer, funds)
			}
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}

// rule: ARC-I-019
func TestARC_I_019_AuthorizerDistinctFromPreparer(t *testing.T) {
	ctx := verifiedCtx(t)
	w := Withdrawal{Request: req(nil)}
	cases := []struct {
		name   string
		signer Signer
		funds  Funds
		want   error
	}{
		{"preparer", Signer{UserID: "u-clerk", TrustSigning: true}, Funds{ClearedCents: 500000}, ErrPreparerApproves},
		{"no user", Signer{TrustSigning: true}, Funds{ClearedCents: 500000}, ErrPreparerApproves},
		{"other signer", Signer{UserID: "u-lawyer", TrustSigning: true}, Funds{ClearedCents: 500000}, nil},
		{"balance re-checked", Signer{UserID: "u-lawyer", TrustSigning: true}, Funds{ClearedCents: 50000}, ErrTrustOverdraw},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateAuthorizer(ctx, w, c.signer, c.funds)
			if !errors.Is(err, c.want) && !(c.want == nil && err == nil) {
				t.Fatalf("got %v, want %v", err, c.want)
			}
		})
	}
}
