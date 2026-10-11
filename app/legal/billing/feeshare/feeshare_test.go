package feeshare

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

func checker(t *testing.T, on bool) Checker {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: feeshareFlag, Owner: "lawyer/LSO", Expiry: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(feeshareFlag, on); err != nil {
		t.Fatal(err)
	}
	return Checker{Flags: s, Money: moneyfake.New()}
}

func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		return
	}
	var p *api.Problem
	if !errors.As(err, &p) || p.Code != code || p.Status != 422 || p.Rule == "" {
		t.Fatalf("err = %v, want 422 %s", err, code)
	}
}

var (
	outsideLawyer = Payee{ID: "p1", LicenceNumber: "12345A", OutsideFirm: true}
	agency        = Payee{ID: "p2"}
	mdpPartner    = Payee{ID: "p3", AffiliationID: "mdp-1"}
)

func TestFlagOff(t *testing.T) {
	c := checker(t, false)
	if err := c.CheckPayable(Payable{Kind: KindOrdinary}, 0); !errors.Is(err, ErrOff) {
		t.Fatalf("err = %v, want ErrOff", err)
	}
}

// rule: BIL-L-030
func TestBIL_L_030_FeeDivisionNeedsConsent(t *testing.T) {
	c := checker(t, true)
	for _, tc := range []struct {
		name string
		p    Payable
		code string
	}{
		{"outside lawyer, no consent", Payable{Kind: KindFeeDivision, Payee: outsideLawyer, AmountCents: 50_000}, "FEE_DIVISION_CONSENT_REQUIRED"},
		{"outside lawyer, consent", Payable{Kind: KindFeeDivision, Payee: outsideLawyer, AmountCents: 50_000, ClientConsentDocID: "c"}, ""},
		{"outside MDP member without licence, consent", Payable{Kind: KindFeeDivision, Payee: Payee{AffiliationID: "mdp-1", OutsideFirm: true}, AmountCents: 50_000, ClientConsentDocID: "c"}, "FEE_DIVISION_CONSENT_REQUIRED"},
		{"in-firm licensee", Payable{Kind: KindFeeDivision, Payee: Payee{LicenceNumber: "9"}, AmountCents: 50_000}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) { wantCode(t, c.CheckPayable(tc.p, 0), tc.code) })
	}
}

// rule: BIL-L-031
func TestBIL_L_031_ReferralFee(t *testing.T) {
	c := checker(t, true)
	ok := Payable{Kind: KindReferral, Payee: outsideLawyer, ReferralAgreementDocID: "ra", ClientConsentDocID: "c"}
	with := func(f func(*Payable)) Payable { p := ok; f(&p); return p }
	for _, tc := range []struct {
		name string
		p    Payable
		fees money.Cents
		code string
	}{
		{"at cap", with(func(p *Payable) { p.AmountCents = 1_100_000 }), 12_000_000, ""},
		{"over cap", with(func(p *Payable) { p.AmountCents = 1_200_000 }), 12_000_000, "REFERRAL_CAP"},
		{"no agreement", with(func(p *Payable) { p.AmountCents = 1; p.ReferralAgreementDocID = "" }), 12_000_000, "REFERRAL_AGREEMENT_REQUIRED"},
		{"no consent", with(func(p *Payable) { p.AmountCents = 1; p.ClientConsentDocID = "" }), 12_000_000, "REFERRAL_CONSENT_REQUIRED"},
		{"conflict referral", with(func(p *Payable) { p.AmountCents = 1; p.ConflictReferral = true }), 12_000_000, "REFERRAL_CONFLICT"},
		{"MDP payee without licence", with(func(p *Payable) { p.AmountCents = 1; p.Payee = mdpPartner }), 12_000_000, "FEE_SPLIT_NON_LICENSEE"},
	} {
		t.Run(tc.name, func(t *testing.T) { wantCode(t, c.CheckPayable(tc.p, tc.fees), tc.code) })
	}
	for _, tc := range []struct {
		fees, want money.Cents
	}{
		{12_000_000, 1_100_000},
		{60_000_000, 2_500_000},
		{5_000_000, 750_000},
		{0, 0},
		{1 << 62, 2_500_000},
	} {
		got, err := c.ReferralCap(tc.fees)
		if err != nil || got != tc.want {
			t.Fatalf("ReferralCap(%d) = %d, %v; want %d", tc.fees, got, err, tc.want)
		}
	}
}

// rule: BIL-L-032
func TestBIL_L_032_NoSplitWithNonLicensee(t *testing.T) {
	c := checker(t, true)
	for _, tc := range []struct {
		name string
		p    Payable
		code string
	}{
		{"referral bonus to agency", Payable{Kind: KindReferral, Payee: agency, AmountCents: 20_000, ReferralAgreementDocID: "ra", ClientConsentDocID: "c"}, "FEE_SPLIT_NON_LICENSEE"},
		{"fee split to agency", Payable{Kind: KindFeeDivision, Payee: agency, AmountCents: 20_000, ClientConsentDocID: "c"}, "FEE_SPLIT_NON_LICENSEE"},
		{"fixed-price ad invoice", Payable{Kind: KindOrdinary, Payee: agency, AmountCents: 20_000}, ""},
		{"fee split within approved MDP", Payable{Kind: KindFeeDivision, Payee: mdpPartner, AmountCents: 20_000}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) { wantCode(t, c.CheckPayable(tc.p, 10_000_000), tc.code) })
	}
}
