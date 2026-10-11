package tax

import (
	"context"
	"fmt"

	"github.com/dagi377/counsel/kernel/flags"
)

// FederalCourt is the Supply.CourtProvince value for litigation in a federal court or
// tribunal (Federal Court, Federal Court of Appeal, Tax Court, Supreme Court). Those are not
// established under the laws of a province, so SOR/2010-117 s.27 does not apply.
const FederalCourt = "FED"

// participating are the HST provinces (ETA Sch. VIII). A supply made there carries HST;
// anywhere else in Canada it carries GST.
var participating = map[string]bool{"ON": true, "NS": true, "NB": true, "NL": true, "PE": true}

// Decider decides the place of supply of a legal service under the CRA place-of-supply
// rules (SOR/2010-117 Part 1; ETA Sch. VI Part V s.23; CRA B-103).
type Decider struct{}

var _ PlaceOfSupply = (*Decider)(nil)

// NewPlaceOfSupply returns the place-of-supply decider.
func NewPlaceOfSupply() *Decider { return &Decider{} }

// Decide applies, in order (BIL-T-010): the non-resident zero-rating test, the litigation
// rule, the real-property rule and the address rule. The first that applies wins.
//
// (verify) The order follows BIL-T-010 and SOR/2010-117 s.27, which applies despite the
// real-property rule: a court province beats a property province. Confirm with B-103.
func (d *Decider) Decide(ctx context.Context, s Supply) (Place, error) {
	// rule: BIL-T-010
	var (
		prov  string
		rules []string
		err   error
	)
	if nonResident(s) {
		// rule: BIL-T-015
		prov, rules, err = nonResidentProvince(ctx, s)
	} else {
		prov, rules, err = residentProvince(ctx, s)
	}
	if err != nil {
		return Place{}, err
	}
	if prov == Zero {
		return Place{Province: Zero, Rules: rules}, nil
	}
	return placeIn(ctx, s, prov, rules)
}

// nonResident reports whether the supply is to a non-resident: the client is not flagged
// resident and its address on file is outside Canada (a non-Canadian code). A client that is
// not flagged resident but has a Canadian address is taxed by that address (fail closed).
func nonResident(s Supply) bool {
	return !s.RecipientResident && s.ClientProvince != "" && !provinces[s.ClientProvince]
}

// residentProvince is the province of a supply that is not zero-rated: litigation first,
// then real property, then the address rules.
func residentProvince(ctx context.Context, s Supply) (string, []string, error) {
	if s.CourtProvince != "" {
		return litigationProvince(ctx, s)
	}
	// rule: BIL-T-011
	if s.PropertyProvince != "" {
		if !provinces[s.PropertyProvince] {
			return "", nil, fmt.Errorf("decide property %q: %w", s.PropertyProvince, ErrUnknownProvince)
		}
		return s.PropertyProvince, []string{"BIL-T-011"}, nil
	}
	return addressProvince(s)
}

// litigationProvince applies SOR/2010-117 s.27: a service after the litigation commenced in
// a provincial court or tribunal is supplied in that province. CourtProvince is set only for
// services dated on or after commencement; earlier services leave it empty.
func litigationProvince(ctx context.Context, s Supply) (string, []string, error) {
	// rule: BIL-T-012 (verify)
	if s.CourtProvince == FederalCourt {
		if !flags.Enabled(ctx, FlagVerifyRules) {
			return "", nil, fmt.Errorf("decide federal court: %w", ErrUnverified)
		}
		prov, rules, err := addressProvince(s)
		return prov, append([]string{"BIL-T-012"}, rules...), err
	}
	// rule: BIL-T-012
	if !provinces[s.CourtProvince] {
		return "", nil, fmt.Errorf("decide court %q: %w", s.CourtProvince, ErrUnknownProvince)
	}
	return s.CourtProvince, []string{"BIL-T-012"}, nil
}

// addressProvince applies SOR/2010-117 s.13: the province of the client's Canadian address,
// or, for a resident with no Canadian address, where the work is primarily performed.
func addressProvince(s Supply) (string, []string, error) {
	if provinces[s.ClientProvince] {
		// rule: BIL-T-013
		return s.ClientProvince, []string{"BIL-T-013"}, nil
	}
	// rule: BIL-T-014
	if s.RecipientResident {
		return DefaultPerformedProvince, []string{"BIL-T-014"}, nil
	}
	return "", nil, fmt.Errorf("decide address %q: %w", s.ClientProvince, ErrUnknownProvince)
}

// placeIn is the place of supply in prov with its components (pos_provincial.go).
func placeIn(ctx context.Context, s Supply, prov string, rules []string) (Place, error) {
	comps, err := componentsIn(ctx, prov, s.RegisteredProvinces)
	if err != nil {
		return Place{}, err
	}
	if len(comps) > 1 {
		// rule: BIL-T-020
		rules = append(rules, "BIL-T-020")
	}
	return Place{Province: prov, Components: comps, Rules: rules}, nil
}
