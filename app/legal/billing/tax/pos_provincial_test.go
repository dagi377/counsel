package tax

import (
	"testing"
)

// rule: BIL-T-020
func TestBIL_T_020_RegisteredOnly(t *testing.T) {
	all := []string{"QC", "BC", "MB", "SK"}
	withProv := func(prov string, c Component, rules ...string) Place {
		return Place{Province: prov, Components: []Component{GST, c}, Rules: append(rules, "BIL-T-020")}
	}
	cases := []posCase{
		{name: "BC client, firm not BC-registered: GST only", s: Supply{ClientProvince: "BC", RecipientResident: true}, want: gstAt("BC", "BIL-T-013")},
		{name: "BC client, registered elsewhere: GST only", ctx: verifyOn(t), s: Supply{ClientProvince: "BC", RecipientResident: true, RegisteredProvinces: []string{"MB"}}, want: gstAt("BC", "BIL-T-013")},
		{name: "ON client, registered everywhere: HST only", ctx: verifyOn(t), s: Supply{ClientProvince: "ON", RecipientResident: true, RegisteredProvinces: all}, want: hstAt("ON", "BIL-T-013")},
		{name: "AB client, registered everywhere: GST only", ctx: verifyOn(t), s: Supply{ClientProvince: "AB", RecipientResident: true, RegisteredProvinces: all}, want: gstAt("AB", "BIL-T-013")},
		{name: "BC registered", ctx: verifyOn(t), s: Supply{ClientProvince: "BC", RecipientResident: true, RegisteredProvinces: all}, want: withProv("BC", PST, "BIL-T-013")},
		{name: "QC registered", ctx: verifyOn(t), s: Supply{ClientProvince: "QC", RecipientResident: true, RegisteredProvinces: all}, want: withProv("QC", QST, "BIL-T-013")},
		{name: "MB registered: RST as PST", ctx: verifyOn(t), s: Supply{ClientProvince: "MB", RecipientResident: true, RegisteredProvinces: all}, want: withProv("MB", PST, "BIL-T-013")},
		{name: "SK registered", ctx: verifyOn(t), s: Supply{ClientProvince: "SK", RecipientResident: true, RegisteredProvinces: all}, want: withProv("SK", PST, "BIL-T-013")},
		{name: "SK court, registered", ctx: verifyOn(t), s: Supply{ClientProvince: "ON", RecipientResident: true, CourtProvince: "SK", RegisteredProvinces: []string{"SK"}}, want: withProv("SK", PST, "BIL-T-012")},
		{name: "zero-rated: nothing provincial", ctx: verifyOn(t), s: Supply{ClientProvince: "US", RegisteredProvinces: all}, want: zeroPlace()},
	}
	// rule: BIL-T-020 (verify)
	for _, p := range all {
		cases = append(cases, posCase{name: p + " registered, flag off: fail closed", s: Supply{ClientProvince: p, RecipientResident: true, RegisteredProvinces: []string{p}}, err: ErrUnverified})
	}
	runPos(t, cases)
}

// rule: BIL-T-010
func TestResolveGSTHST(t *testing.T) {
	cases := []struct {
		place string
		want  Component
	}{
		{"ON", HST}, {"NS", HST}, {"NB", HST}, {"NL", HST}, {"PE", HST},
		{"QC", GST}, {"BC", GST}, {"MB", GST}, {"SK", GST}, {"AB", GST}, {"YT", GST}, {"NT", GST}, {"NU", GST},
		{Zero, None},
	}
	for _, tc := range cases {
		t.Run(tc.place, func(t *testing.T) {
			if got := ResolveGSTHST(tc.place); got != tc.want {
				t.Fatalf("ResolveGSTHST(%s) = %s, want %s", tc.place, got, tc.want)
			}
		})
	}
}
