package tax

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/kernel/flags"
)

// nonResidentProvince applies ETA Sch. VI Part V s.23: a legal service to a non-resident is
// zero-rated unless an exclusion applies. Excluded supplies are taxed under the ordinary
// place-of-supply rules. Tangible personal property (s.23(c)) is not modelled.
//
// The client's presence in Canada is not a test: s.23 has none, and s.7(b) keeps advisory,
// consulting and professional services out of s.7(a.1). MetInCanada therefore changes nothing.
func nonResidentProvince(ctx context.Context, s Supply) (string, []string, error) {
	// rule: BIL-T-015
	// s.23(a): litigation in Canada after commencement, for an individual only. A non-resident
	// corporation, trust or partnership in the same litigation stays zero-rated.
	if s.CourtProvince != "" && !s.RecipientBusiness {
		if s.CourtProvince == FederalCourt {
			// rule: BIL-T-012 (verify)
			if !flags.Enabled(ctx, FlagVerifyRules) {
				return "", nil, fmt.Errorf("decide non-resident federal court: %w", ErrUnverified)
			}
			return DefaultPerformedProvince, []string{"BIL-T-015(a)", "BIL-T-012", "BIL-T-014"}, nil
		}
		if !provinces[s.CourtProvince] {
			return "", nil, fmt.Errorf("decide court %q: %w", s.CourtProvince, ErrUnknownProvince)
		}
		return s.CourtProvince, []string{"BIL-T-015(a)", "BIL-T-012"}, nil
	}
	// rule: BIL-T-015
	// s.23(b): real property situated in Canada is taxed where it is (BIL-T-011).
	if s.PropertyProvince != "" {
		if !provinces[s.PropertyProvince] {
			return "", nil, fmt.Errorf("decide property %q: %w", s.PropertyProvince, ErrUnknownProvince)
		}
		return s.PropertyProvince, []string{"BIL-T-015(b)", "BIL-T-011"}, nil
	}
	// rule: BIL-T-015 (verify)
	// s.23(d): acting as the non-resident's agent is excluded. The general rule then applies;
	// with no Canadian address that is where the work is performed (BIL-T-014, typically ON).
	if s.AgentRole {
		if !flags.Enabled(ctx, FlagVerifyRules) {
			return "", nil, fmt.Errorf("decide non-resident agent role: %w", ErrUnverified)
		}
		return DefaultPerformedProvince, []string{"BIL-T-015(d)", "BIL-T-014"}, nil
	}
	return Zero, []string{"BIL-T-015"}, nil
}
