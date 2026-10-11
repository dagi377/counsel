package contingency

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/flags"
)

func templates(t *testing.T, on bool) Templates {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: templateFlag, Owner: "lawyer/LSO", Expiry: time.Date(2027, 6, 30, 0, 0, 0, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(templateFlag, on); err != nil {
		t.Fatal(err)
	}
	return Templates{Flags: s}
}

func TestTemplatesFlagOff(t *testing.T) {
	s := templates(t, false)
	if err := s.CheckPublish(Template{}); !errors.Is(err, ErrTemplatesOff) {
		t.Fatalf("CheckPublish err = %v, want ErrTemplatesOff", err)
	}
	if _, err := s.CourtApprovalWarnings(FeeAgreement{}); !errors.Is(err, ErrTemplatesOff) {
		t.Fatalf("CourtApprovalWarnings err = %v, want ErrTemplatesOff", err)
	}
}

// rule: BIL-L-014
func TestBIL_L_014_NoNegligenceRelease(t *testing.T) {
	s := templates(t, true)
	for _, tc := range []struct {
		name    string
		clauses []Clause
		code    string
	}{
		{"liability waiver", []Clause{{ID: "c1", Tags: []string{"scope"}}, {ID: "c2", Tags: []string{"limits", TagLiabilityWaiver}}}, "TEMPLATE_PROHIBITED_CLAUSE"},
		{"clean", []Clause{{ID: "c1", Tags: []string{"scope"}}, {ID: "c2", Tags: []string{"fees"}}}, ""},
		{"no clauses", nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := s.CheckPublish(Template{ID: "t1", Type: TypeFixed, Clauses: tc.clauses})
			if tc.code == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			var p *api.Problem
			if !errors.As(err, &p) || p.Code != tc.code || p.Status != 422 || p.Rule != "BIL-L-014" {
				t.Fatalf("err = %v, want 422 %s", err, tc.code)
			}
		})
	}
}

// rule: BIL-L-013
func TestBIL_L_013_CourtAgreementNeedsAssessmentOfficer(t *testing.T) {
	s := templates(t, true)
	for _, tc := range []struct {
		name string
		a    FeeAgreement
		warn bool
	}{
		{"fixed fee, Superior Court, not approved", FeeAgreement{Type: TypeFixed, Court: "superior"}, true},
		{"percentage, Superior Court, not approved", FeeAgreement{Type: TypePercentage, Court: "superior"}, true},
		{"fixed fee, Superior Court, approved", FeeAgreement{Type: TypeFixed, Court: "superior", AssessmentOfficerApproved: true}, false},
		{"fixed fee, Small Claims", FeeAgreement{Type: TypeFixed, Court: CourtSmallClaims}, false},
		{"fixed fee, no court", FeeAgreement{Type: TypeFixed}, false},
		{"hourly retainer, Superior Court", FeeAgreement{Type: TypeHourly, Court: "superior"}, false},
		{"contingency, Superior Court", FeeAgreement{Type: TypeContingency, Court: "superior"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.CourtApprovalWarnings(tc.a)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(got, WarnS17Approval) != tc.warn {
				t.Fatalf("warnings = %v, want warn %v", got, tc.warn)
			}
		})
	}
}
