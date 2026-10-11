package tax

import (
	"testing"
)

func zeroPlace() Place { return Place{Province: Zero, Rules: []string{"BIL-T-015"}} }

// rule: BIL-T-015
func TestBIL_T_015_NonResident(t *testing.T) {
	runPos(t, []posCase{
		{name: "US corporation, contract advice", s: Supply{ClientProvince: "US", RecipientBusiness: true, Date: posDate}, want: zeroPlace()},
		{name: "US individual, general advice", s: Supply{ClientProvince: "US", Date: posDate}, want: zeroPlace()},
		{name: "UK corporation in Ontario action stays zero-rated", s: Supply{ClientProvince: "GB", RecipientBusiness: true, CourtProvince: "ON"}, want: zeroPlace()},
		{name: "UK individual in Ontario action: court province", s: Supply{ClientProvince: "GB", CourtProvince: "ON"}, want: hstAt("ON", "BIL-T-015(a)", "BIL-T-012")},
		{name: "US individual in Alberta action: GST", s: Supply{ClientProvince: "US", CourtProvince: "AB"}, want: gstAt("AB", "BIL-T-015(a)", "BIL-T-012")},
		{name: "US individual met in Toronto: still zero-rated", s: Supply{ClientProvince: "US", MetInCanada: true}, want: zeroPlace()},
		{name: "US individual selling Toronto house", s: Supply{ClientProvince: "US", PropertyProvince: "ON"}, want: hstAt("ON", "BIL-T-015(b)", "BIL-T-011")},
		{name: "US corporation, Halifax property beats residence", s: Supply{ClientProvince: "US", RecipientBusiness: true, PropertyProvince: "NS"}, want: hstAt("NS", "BIL-T-015(b)", "BIL-T-011")},
		{name: "UK corporation in ON action over NS land: property", s: Supply{ClientProvince: "GB", RecipientBusiness: true, CourtProvince: "ON", PropertyProvince: "NS"}, want: hstAt("NS", "BIL-T-015(b)", "BIL-T-011")},
		{name: "unknown court province", s: Supply{ClientProvince: "US", CourtProvince: "ZZ"}, err: ErrUnknownProvince},
		{name: "unknown property province", s: Supply{ClientProvince: "US", PropertyProvince: "ZZ"}, err: ErrUnknownProvince},
		// rule: BIL-T-012 (verify)
		{name: "individual in federal court, flag off", s: Supply{ClientProvince: "US", CourtProvince: FederalCourt}, err: ErrUnverified},
		{name: "individual in federal court, flag on", ctx: verifyOn(t), s: Supply{ClientProvince: "US", CourtProvince: FederalCourt}, want: hstAt("ON", "BIL-T-015(a)", "BIL-T-012", "BIL-T-014")},
		// rule: BIL-T-015 (verify)
		{name: "agent role, flag off: fail closed", s: Supply{ClientProvince: "US", AgentRole: true}, err: ErrUnverified},
		{name: "agent role, flag on: general rule ON", ctx: verifyOn(t), s: Supply{ClientProvince: "US", AgentRole: true}, want: hstAt("ON", "BIL-T-015(d)", "BIL-T-014")},
		// A Canadian address without the resident flag is taxed by address (fail closed).
		{name: "not flagged resident, Canadian address", s: Supply{ClientProvince: "BC"}, want: gstAt("BC", "BIL-T-013")},
	})
}
