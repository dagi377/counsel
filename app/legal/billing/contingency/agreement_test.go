package contingency

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

func agreements(t *testing.T, on bool) (Agreements, context.Context) {
	t.Helper()
	return Agreements{Money: moneyfake.New()}, legalflagstest.Context(t, agreementFlag, on)
}

// wantCode fails unless err is a problem with code (or nil when code is empty).
func wantCode(t *testing.T, err error, code string, status int) {
	t.Helper()
	if code == "" {
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		return
	}
	var p *api.Problem
	if !errors.As(err, &p) {
		t.Fatalf("err = %v, want problem %s", err, code)
	}
	if p.Code != code || p.Status != status || p.Rule == "" {
		t.Fatalf("problem = %+v, want code %s status %d with a rule", p, code, status)
	}
}

// valid is an active-ready percentage CFA for an individual personal-injury client.
func valid() Agreement {
	return Agreement{ID: "cfa-1", MatterID: "M-1", PracticeArea: "personal_injury", Kind: KindPercentage,
		SignedDocID: "doc-1", Form: StandardForm, Stages: []Stage{{Name: "trial", PctPPM: 300000}}}
}

func TestAgreementsFlagOff(t *testing.T) {
	s, ctx := agreements(t, false)
	if err := s.CheckArrangement(ctx, valid()); !errors.Is(err, ErrAgreementsOff) {
		t.Fatalf("err = %v, want ErrAgreementsOff", err)
	}
	if _, err := s.Fee(ctx, 100, 1); !errors.Is(err, ErrAgreementsOff) {
		t.Fatalf("err = %v, want ErrAgreementsOff", err)
	}
}

// rule: BIL-L-016
func TestBIL_L_016_NoContingencyInFamilyOrCriminal(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		area PracticeArea
		code string
	}{
		{AreaFamily, "CONTINGENCY_NOT_PERMITTED"},
		{AreaCriminal, "CONTINGENCY_NOT_PERMITTED"},
		{AreaQuasiCriminal, "CONTINGENCY_NOT_PERMITTED"},
		{"personal_injury", ""},
	} {
		t.Run(string(tc.area), func(t *testing.T) {
			a := valid()
			a.PracticeArea = tc.area
			wantCode(t, s.CheckArrangement(ctx, a), tc.code, 422)
		})
	}
}

// rule: BIL-L-015
func TestBIL_L_015_SignedDocumentRequired(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		name, doc, code string
	}{
		{"unsigned", "", "CFA_DOCUMENT_REQUIRED"},
		{"signed", "doc-1", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := valid()
			a.SignedDocID = tc.doc
			wantCode(t, s.CheckArrangement(ctx, a), tc.code, 422)
		})
	}
}

// rule: BIL-L-017
func TestBIL_L_017_StandardFormRequired(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		name string
		form string
		ex   *Exemption
		code string
	}{
		{"individual on custom form", "firm-custom", nil, "CFA_STANDARD_FORM_REQUIRED"},
		{"standard form", StandardForm, nil, ""},
		{"revenue over 10M with evidence", "firm-custom", &Exemption{OrgAssetsOrRevenueCents: 1_200_000_000, EvidenceDocID: "ev"}, ""},
		{"revenue exactly 10M", "firm-custom", &Exemption{OrgAssetsOrRevenueCents: 1_000_000_000, EvidenceDocID: "ev"}, "CFA_STANDARD_FORM_REQUIRED"},
		{"revenue over 10M without evidence", "firm-custom", &Exemption{OrgAssetsOrRevenueCents: 1_200_000_000}, "CFA_STANDARD_FORM_REQUIRED"},
		{"26 employees", "firm-custom", &Exemption{OrgEmployees: 26, EvidenceDocID: "ev"}, ""},
		{"25 employees", "firm-custom", &Exemption{OrgEmployees: 25, EvidenceDocID: "ev"}, "CFA_STANDARD_FORM_REQUIRED"},
		{"full-time lawyer", "firm-custom", &Exemption{OrgHasFullTimeLawyer: true, EvidenceDocID: "ev"}, ""},
		{"court approved", "firm-custom", &Exemption{CourtApprovalDocID: "order"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := valid()
			a.Form, a.Exemption = tc.form, tc.ex
			wantCode(t, s.Activate(ctx, a), tc.code, 422)
		})
	}
}

// rule: BIL-L-018
func TestBIL_L_018_PrescribedStatements(t *testing.T) {
	s, ctx := agreements(t, true)
	ticked := func(items ...int) map[int]string {
		m := map[int]string{}
		for _, n := range items {
			m[n] = "cl. 4"
		}
		return m
	}
	for _, tc := range []struct {
		name       string
		statements map[int]string
		disability bool
		code       string
	}{
		{"adult, 1-5,7,8", ticked(1, 2, 3, 4, 5, 7, 8), false, ""},
		{"adult, missing 5", ticked(1, 2, 3, 4, 7, 8), false, "CFA_MISSING_STATEMENT_5"},
		{"disability, missing 6", ticked(1, 2, 3, 4, 5, 7, 8), true, "CFA_MISSING_STATEMENT_6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := valid()
			a.Kind, a.Stages, a.Statements = KindNonPercentage, nil, tc.statements
			a.ClientUnderDisability, a.ReviewUnderR708 = tc.disability, true
			wantCode(t, s.Activate(ctx, a), tc.code, 422)
		})
	}
}

// rule: BIL-L-019
func TestBIL_L_019_ImpermissibleTerms(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		tag, code string
	}{
		{TagConsentToSettle, "CFA_IMPERMISSIBLE_TERM"},
		{TagNoTermination, "CFA_IMPERMISSIBLE_TERM"},
		{TagFeeSplitNonLicensee, "CFA_IMPERMISSIBLE_TERM"},
		{"governing_law", ""},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			a := valid()
			a.ClauseTags = []string{"parties", tc.tag}
			wantCode(t, s.Activate(ctx, a), tc.code, 422)
		})
	}
}

// rule: BIL-L-025
func TestBIL_L_025_DisabilityApproval(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		name          string
		judge         string
		review        bool
		order         string
		activate, pay string
	}{
		{"minor, no approval", "", false, "", "CFA_DISABILITY_APPROVAL_REQUIRED", "R708_ORDER_REQUIRED"},
		{"minor, judge approved, no order", "j", false, "", "", "R708_ORDER_REQUIRED"},
		{"minor, r.7.08 review, order linked", "", true, "o", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := valid()
			a.ClientUnderDisability, a.JudgeApprovalDocID, a.ReviewUnderR708 = true, tc.judge, tc.review
			wantCode(t, s.Activate(ctx, a), tc.activate, 422)
			wantCode(t, s.CheckFeeFromSettlement(ctx, a, tc.order), tc.pay, 409)
		})
	}
}

// rule: BIL-L-026
func TestBIL_L_026_ParalegalMattersSameRules(t *testing.T) {
	s, ctx := agreements(t, true)
	a := valid()
	a.Paralegal, a.PracticeArea, a.Form = true, "small_claims", "firm-custom"
	a.Stages = []Stage{{Name: "trial", PctPPM: 250000}}
	wantCode(t, s.Activate(ctx, a), "CFA_STANDARD_FORM_REQUIRED", 422)
	a.PracticeArea = AreaFamily
	wantCode(t, s.CheckArrangement(ctx, a), "CONTINGENCY_NOT_PERMITTED", 422)
}

// rule: BIL-C-032
func TestBIL_C_032_ContingencyFee(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		name string
		base money.Cents
		pct  money.PPM
		want money.Cents
	}{
		{"30% of 1000000", 1_000_000, 300_000, 300_000},
		{"BIL-X-017 25% of 85000.00", 8_500_000, 250_000, 2_125_000},
		{"rounds half up", 5, 100_000, 1},
		{"zero", 0, 300_000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Fee(ctx, tc.base, tc.pct)
			if err != nil || got != tc.want {
				t.Fatalf("Fee = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
	if _, err := s.Fee(ctx, 1, 1_000_001); err == nil {
		t.Fatal("pct over 100% accepted")
	}
	a := valid()
	a.Stages = []Stage{{Name: "pleadings", PctPPM: 200_000}, {Name: "trial", PctPPM: 330_000}}
	if p, err := s.StagePct(ctx, a, "trial"); err != nil || p != 330_000 {
		t.Fatalf("StagePct = %d, %v", p, err)
	}
	wantCode(t, func() error { _, err := s.StagePct(ctx, a, "appeal"); return err }(), "CFA_UNKNOWN_STAGE", 422)
	a.InterimBillsAllowed = false
	wantCode(t, s.CheckInterimBill(ctx, a), "CFA_INTERIM_BILL_NOT_ALLOWED", 409)
	a.InterimBillsAllowed = true
	wantCode(t, s.CheckInterimBill(ctx, a), "", 0)
}

// rule: BIL-L-021
func TestBIL_L_021_FeeExcludesRecoverableDisbursements(t *testing.T) {
	s, ctx := agreements(t, true)
	base, err := s.RecoveryBase(ctx, Recovery{DamagesCents: 1_000_000, DisbursementsCents: 80_000})
	if err != nil || base != 920_000 {
		t.Fatalf("base = %d, %v; want 920000", base, err)
	}
	fee, err := s.Fee(ctx, base, 300_000)
	if err != nil || fee != 276_000 {
		t.Fatalf("fee = %d, %v; want 276000", fee, err)
	}
}

// rule: BIL-L-020
func TestBIL_L_020_RecoveryBase(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		name string
		r    Recovery
		want money.Cents
		code string
	}{
		{"settlement 3000000 incl. costs 500000 and disb 200000", Recovery{DamagesCents: 2_500_000, CostsCents: 500_000, DisbursementsCents: 200_000}, 2_800_000, ""},
		{"taxes excluded", Recovery{DamagesCents: 1_000_000, TaxesCents: 13_000}, 987_000, ""},
		{"disbursements exceed recovery", Recovery{DamagesCents: 100, DisbursementsCents: 200}, 0, "CFA_BAD_RECOVERY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.RecoveryBase(ctx, tc.r)
			wantCode(t, err, tc.code, 422)
			if got != tc.want {
				t.Fatalf("base = %d, want %d", got, tc.want)
			}
		})
	}
}

// rule: BIL-C-033
func TestBIL_C_033_ContingencyCap(t *testing.T) {
	s, ctx := agreements(t, true)
	for _, tc := range []struct {
		name      string
		fee, base money.Cents
		want      money.Cents
		code      string
	}{
		{"fee 300000 on 1000000", 300_000, 1_000_000, 300_000, ""},
		{"fee 2900000 on 2800000 -> capped", 2_900_000, 2_800_000, 2_800_000, "CFA_FEE_EXCEEDS_RECOVERY"},
		{"fee equal to base", 2_800_000, 2_800_000, 2_800_000, ""},
		{"fee over recovery base", 1_000_001, 1_000_000, 1_000_000, "CFA_FEE_EXCEEDS_RECOVERY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.CheckCap(ctx, tc.fee, tc.base)
			wantCode(t, err, tc.code, 409)
			if got != tc.want {
				t.Fatalf("proposal = %d, want %d", got, tc.want)
			}
		})
	}
}

// rule: BIL-L-024
func TestBIL_L_024_AssessmentDeadlines(t *testing.T) {
	s, ctx := agreements(t, true)
	d := func(y int, m time.Month, day int) clock.Date { return clock.Date{Year: y, Month: m, Day: day} }
	paid := d(2026, 5, 20)
	for _, tc := range []struct {
		name      string
		delivered clock.Date
		paid      *clock.Date
		over      bool
		after     clock.Date
		afterPaid *clock.Date
	}{
		{"delivered and paid", d(2026, 5, 1), &paid, false, d(2026, 5, 31), &clock.Date{Year: 2027, Month: 5, Day: 20}},
		{"30 days, not one month", d(2026, 1, 31), nil, false, d(2026, 3, 2), nil},
		{"over-maximum: six months", d(2026, 5, 1), nil, true, d(2026, 11, 1), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.AssessmentDeadlines(ctx, tc.delivered, tc.paid, tc.over)
			if err != nil {
				t.Fatal(err)
			}
			if got.AfterDelivery != tc.after {
				t.Fatalf("after delivery = %v, want %v", got.AfterDelivery, tc.after)
			}
			if (got.AfterPayment == nil) != (tc.afterPaid == nil) || (got.AfterPayment != nil && *got.AfterPayment != *tc.afterPaid) {
				t.Fatalf("after payment = %v, want %v", got.AfterPayment, tc.afterPaid)
			}
		})
	}
}
