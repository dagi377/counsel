package timekeeping

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/flags"
)

func scopeCtx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagParalegalScopeVerified, Owner: "lawyer/LSO", Doc: "test"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagParalegalScopeVerified, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

// rule: BIL-L-046
func TestBIL_L_046_ParalegalOutOfScopeFlaggedAndBlocked(t *testing.T) {
	scope := Scope{
		Paralegals: map[string]bool{"u-para": true},
		Activities: map[string]bool{"small_claims": true, "traffic": true},
	}
	tests := []struct {
		name        string
		flagOn      bool
		e           TimeEntry
		wantFlagged bool
	}{
		{"paralegal on family law: flagged W-SCOPE", true, TimeEntry{ID: "te-1", UserID: "u-para", Minutes: 120, ActivityCode: "family_law"}, true},
		{"paralegal inside scope", true, TimeEntry{ID: "te-2", UserID: "u-para", Minutes: 60, ActivityCode: "small_claims"}, false},
		{"lawyer outside paralegal list", true, TimeEntry{ID: "te-3", UserID: "u-law", Minutes: 60, ActivityCode: "family_law"}, false},
		{"already flagged stays flagged once", true, TimeEntry{ID: "te-4", UserID: "u-para", Minutes: 60, ActivityCode: "family_law", Flags: []string{FlagOutOfScope}}, true},
		{"verify flag off: no check", false, TimeEntry{ID: "te-5", UserID: "u-para", Minutes: 120, ActivityCode: "family_law"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := scopeCtx(t, tt.flagOn)
			in := append([]string(nil), tt.e.Flags...)
			got, err := FlagScope(ctx, scope, tt.e)
			if err != nil {
				t.Fatalf("FlagScope: %v", err)
			}
			n := 0
			for _, f := range got.Flags {
				if f == FlagOutOfScope {
					n++
				}
			}
			if (n == 1) != tt.wantFlagged || n > 1 {
				t.Fatalf("flags = %v, want flagged %v", got.Flags, tt.wantFlagged)
			}
			if len(tt.e.Flags) != len(in) {
				t.Fatalf("input flags changed: %v", tt.e.Flags)
			}
			err = CheckBillable(got)
			if tt.wantFlagged != errors.Is(err, ErrOutOfScope) {
				t.Fatalf("CheckBillable = %v, want blocked %v", err, tt.wantFlagged)
			}
		})
	}
}

type failingScope struct{}

func (failingScope) CheckScope(context.Context, string, string) error { return errors.New("down") }

// rule: BIL-L-046
func TestBIL_L_046_ScopeCheckErrorPropagates(t *testing.T) {
	_, err := FlagScope(context.Background(), failingScope{}, TimeEntry{ID: "te-1"})
	if err == nil || errors.Is(err, ErrOutOfScope) {
		t.Fatalf("err = %v, want a non-scope error", err)
	}
}
