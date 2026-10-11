package tax

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
)

var posDate = day(2026, time.October, 11)

func verifyOn(t *testing.T) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagVerifyRules}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagVerifyRules, true); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

type posCase struct {
	name string
	ctx  context.Context
	s    Supply
	want Place
	err  error
}

func runPos(t *testing.T, cases []posCase) {
	t.Helper()
	d := NewPlaceOfSupply()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			got, err := d.Decide(ctx, tc.s)
			if !errors.Is(err, tc.err) || (tc.err == nil && err != nil) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if err != nil {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func hstAt(prov string, rules ...string) Place {
	return Place{Province: prov, Components: []Component{HST}, Rules: rules}
}

func gstAt(prov string, rules ...string) Place {
	return Place{Province: prov, Components: []Component{GST}, Rules: rules}
}

// rule: BIL-T-011
func TestBIL_T_011_RealProperty(t *testing.T) {
	runPos(t, []posCase{
		{name: "ON client, condo in Halifax", s: Supply{ClientProvince: "ON", RecipientResident: true, PropertyProvince: "NS", Date: posDate}, want: hstAt("NS", "BIL-T-011")},
		{name: "ON client, land in Alberta: GST only", s: Supply{ClientProvince: "ON", RecipientResident: true, PropertyProvince: "AB", Date: posDate}, want: gstAt("AB", "BIL-T-011")},
		{name: "resident, no address, Toronto house", s: Supply{RecipientResident: true, PropertyProvince: "ON", Date: posDate}, want: hstAt("ON", "BIL-T-011")},
		{name: "unknown property province", s: Supply{ClientProvince: "ON", RecipientResident: true, PropertyProvince: "ZZ"}, err: ErrUnknownProvince},
	})
}

// rule: BIL-T-012
func TestBIL_T_012_Litigation(t *testing.T) {
	runPos(t, []posCase{
		{name: "AB individual, Ontario Superior Court action started", s: Supply{ClientProvince: "AB", RecipientResident: true, CourtProvince: "ON"}, want: hstAt("ON", "BIL-T-012")},
		{name: "MB client, post-claim ON lines", s: Supply{ClientProvince: "MB", RecipientResident: true, CourtProvince: "ON"}, want: hstAt("ON", "BIL-T-012")},
		{name: "MB client, pre-claim lines follow address", s: Supply{ClientProvince: "MB", RecipientResident: true}, want: gstAt("MB", "BIL-T-013")},
		// rule: BIL-T-010 (verify) litigation beats real property (SOR/2010-117 s.27).
		{name: "court beats property", s: Supply{ClientProvince: "ON", RecipientResident: true, CourtProvince: "AB", PropertyProvince: "ON"}, want: gstAt("AB", "BIL-T-012")},
		{name: "unknown court province", s: Supply{ClientProvince: "ON", RecipientResident: true, CourtProvince: "ZZ"}, err: ErrUnknownProvince},
		// rule: BIL-T-012 (verify)
		{name: "federal court, flag off", s: Supply{ClientProvince: "NS", RecipientResident: true, CourtProvince: FederalCourt}, err: ErrUnverified},
		{name: "federal court, flag on: address rule", ctx: verifyOn(t), s: Supply{ClientProvince: "NS", RecipientResident: true, CourtProvince: FederalCourt}, want: hstAt("NS", "BIL-T-012", "BIL-T-013")},
	})
}

// rule: BIL-T-013
func TestBIL_T_013_ClientAddress(t *testing.T) {
	cases := []posCase{
		{name: "Calgary head office", s: Supply{ClientProvince: "AB", RecipientResident: true}, want: gstAt("AB", "BIL-T-013")},
		// Not flagged resident but a Canadian address on file: taxed by address (fail closed).
		{name: "earlier caller, address only", s: Supply{ClientProvince: "ON", ServiceType: "general", Date: posDate}, want: hstAt("ON", "BIL-T-013")},
	}
	for _, p := range []string{"ON", "NS", "NB", "NL", "PE"} {
		cases = append(cases, posCase{name: p + " participating: HST", s: Supply{ClientProvince: p, RecipientResident: true}, want: hstAt(p, "BIL-T-013")})
	}
	for _, p := range []string{"AB", "YT", "NT", "NU"} {
		cases = append(cases, posCase{name: p + " GST only", s: Supply{ClientProvince: p, RecipientResident: true}, want: gstAt(p, "BIL-T-013")})
	}
	runPos(t, cases)
}

// rule: BIL-T-014
func TestBIL_T_014_ResidentNoAddress(t *testing.T) {
	runPos(t, []posCase{
		{name: "resident, no address on file", s: Supply{RecipientResident: true}, want: hstAt("ON", "BIL-T-014")},
		{name: "resident, only a foreign address", s: Supply{ClientProvince: "US", RecipientResident: true}, want: hstAt("ON", "BIL-T-014")},
		{name: "no residence flag and no address: refused", s: Supply{}, err: ErrUnknownProvince},
	})
}
