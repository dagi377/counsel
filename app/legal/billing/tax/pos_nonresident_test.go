package tax

import (
	"context"
	"errors"
	"testing"
	"time"
)

// attester is the lawyer who recorded a non-resident determination in these tests.
const attester = "u-lawyer"

func zeroPlace() Place { return Place{Province: Zero, Rules: []string{"BIL-T-015"}} }

// rule: BIL-T-015
func TestBIL_T_015_NonResident(t *testing.T) {
	runPos(t, []posCase{
		{name: "US corporation, contract advice", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, RecipientBusiness: true, Date: posDate}, want: zeroPlace()},
		{name: "US individual, general advice", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, Date: posDate}, want: zeroPlace()},
		{name: "UK corporation in Ontario action stays zero-rated", s: Supply{ClientProvince: "GB", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, RecipientBusiness: true, CourtProvince: "ON"}, want: zeroPlace()},
		{name: "UK individual in Ontario action: court province", s: Supply{ClientProvince: "GB", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, CourtProvince: "ON"}, want: hstAt("ON", "BIL-T-015(a)", "BIL-T-012")},
		{name: "US individual in Alberta action: GST", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, CourtProvince: "AB"}, want: gstAt("AB", "BIL-T-015(a)", "BIL-T-012")},
		{name: "US individual met in Toronto: still zero-rated", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, MetInCanada: true}, want: zeroPlace()},
		{name: "US individual selling Toronto house", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, PropertyProvince: "ON"}, want: hstAt("ON", "BIL-T-015(b)", "BIL-T-011")},
		{name: "US corporation, Halifax property beats residence", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, RecipientBusiness: true, PropertyProvince: "NS"}, want: hstAt("NS", "BIL-T-015(b)", "BIL-T-011")},
		{name: "UK corporation in ON action over NS land: property", s: Supply{ClientProvince: "GB", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, RecipientBusiness: true, CourtProvince: "ON", PropertyProvince: "NS"}, want: hstAt("NS", "BIL-T-015(b)", "BIL-T-011")},
		{name: "unknown court province", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, CourtProvince: "ZZ"}, err: ErrUnknownProvince},
		{name: "unknown property province", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, PropertyProvince: "ZZ"}, err: ErrUnknownProvince},
		// rule: BIL-T-012 (verify)
		{name: "individual in federal court, flag off", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, CourtProvince: FederalCourt}, err: ErrUnverified},
		{name: "individual in federal court, flag on", ctx: verifyOn(t), s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, CourtProvince: FederalCourt}, want: hstAt("ON", "BIL-T-015(a)", "BIL-T-012", "BIL-T-014")},
		// rule: BIL-T-015 (verify)
		{name: "agent role, flag off: fail closed", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, AgentRole: true}, err: ErrUnverified},
		{name: "agent role, flag on: general rule ON", ctx: verifyOn(t), s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate, AgentRole: true}, want: hstAt("ON", "BIL-T-015(d)", "BIL-T-014")},
		// A Canadian address without the resident flag is taxed by address (fail closed).
		{name: "not flagged resident, Canadian address", s: Supply{ClientProvince: "BC"}, want: gstAt("BC", "BIL-T-013")},
	})
}

// rule: BIL-T-015
// A foreign address alone never zero-rates a supply. Zero-rating needs a recorded
// non-resident determination (who and when); without it the decision fails closed.
func TestBIL_T_015_ForeignAddressNeedsAttestation(t *testing.T) {
	cases := []struct {
		name string
		s    Supply
		want Place
		err  error
	}{
		{name: "foreign address, no determination", s: Supply{ClientProvince: "US", Date: posDate}, err: ErrResidencyUnknown},
		{name: "foreign business address, no determination", s: Supply{ClientProvince: "US", RecipientBusiness: true}, err: ErrResidencyUnknown},
		{name: "attested by, no time", s: Supply{ClientProvince: "GB", NonResidentAttestedBy: attester}, err: ErrResidencyUnknown},
		{name: "time, no attester", s: Supply{ClientProvince: "GB", NonResidentAttestedAt: posDate}, err: ErrResidencyUnknown},
		{name: "attested non-resident: zero-rated", s: Supply{ClientProvince: "US", NonResidentAttestedBy: attester, NonResidentAttestedAt: posDate}, want: zeroPlace()},
		{name: "resident abroad: taxed where work is performed", s: Supply{ClientProvince: "US", RecipientResident: true}, want: hstAt("ON", "BIL-T-014")},
		{name: "resident flag beats a stale attestation", s: Supply{ClientProvince: "US", RecipientResident: true, NonResidentAttestedBy: attester, NonResidentAttestedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}, want: hstAt("ON", "BIL-T-014")},
		{name: "Canadian address, not flagged resident: taxed by address", s: Supply{ClientProvince: "ON"}, want: hstAt("ON", "BIL-T-013")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewPlaceOfSupply().Decide(context.Background(), tc.s)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && (got.Province != tc.want.Province || len(got.Rules) != len(tc.want.Rules)) {
				t.Fatalf("place = %+v, want %+v", got, tc.want)
			}
		})
	}
}
