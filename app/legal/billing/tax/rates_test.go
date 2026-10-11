package tax

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
)

func lookupCase(t *testing.T, ctx context.Context, prov string, c Component, on time.Time, want money.PPM, wantErr error) {
	t.Helper()
	got, err := NewRateTable(Seed()).Lookup(ctx, prov, c, on)
	if !errors.Is(err, wantErr) && !(wantErr == nil && err == nil) {
		t.Fatalf("lookup %s %s %s: err %v, want %v", prov, c, on.Format(time.DateOnly), err, wantErr)
	}
	if got != want {
		t.Fatalf("lookup %s %s %s = %d, want %d", prov, c, on.Format(time.DateOnly), got, want)
	}
}

// rule: BIL-T-001
func TestBIL_T_001_SeededRates(t *testing.T) {
	ctx := context.Background()
	on := day(2026, time.October, 11)
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		prov string
		c    Component
		on   time.Time
		want money.PPM
	}{
		{"ON", HST, on, 130000},
		{"ON", GST, on, 50000},
		{"NB", HST, on, 150000},
		{"NL", HST, on, 150000},
		{"PE", HST, on, 150000},
		{"NS", HST, day(2025, time.March, 31), 150000},
		{"NS", HST, time.Date(2025, time.March, 31, 23, 30, 0, 0, toronto), 150000},
		{"NS", HST, day(2025, time.April, 1), 140000},
		{"NS", HST, on, 140000},
	}
	for _, c := range cases {
		t.Run(c.prov+string(c.c)+c.on.Format(time.DateOnly), func(t *testing.T) {
			lookupCase(t, ctx, c.prov, c.c, c.on, c.want, nil)
		})
	}
}

// rule: BIL-T-002
func TestBIL_T_002_RateAtTaxPoint(t *testing.T) {
	due := day(2025, time.March, 15)
	cases := []struct {
		name         string
		paid, due    time.Time
		wantPoint    time.Time
		wantNSRatePM money.PPM
	}{
		{"unpaid uses bill date", time.Time{}, due, due, 150000},
		{"paid before bill date", day(2025, time.March, 1), due, day(2025, time.March, 1), 150000},
		{"paid after rate change keeps bill date", day(2025, time.May, 1), due, due, 150000},
		{"billed after change", time.Time{}, day(2025, time.April, 2), day(2025, time.April, 2), 140000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := TaxPointDate(c.paid, c.due)
			if !p.Equal(c.wantPoint) {
				t.Fatalf("tax point %s, want %s", p, c.wantPoint)
			}
			lookupCase(t, context.Background(), "NS", HST, p, c.wantNSRatePM, nil)
		})
	}
}

// rule: BIL-T-010
func TestBIL_T_010_PlaceOfSupplyCodes(t *testing.T) {
	ctx := context.Background()
	on := day(2026, time.October, 11)
	cases := []struct {
		prov string
		want money.PPM
		err  error
	}{
		{Zero, 0, nil},
		{"ON", 130000, nil},
		{"XX", 0, ErrUnknownProvince},
		{"", 0, ErrUnknownProvince},
	}
	for _, c := range cases {
		t.Run(c.prov, func(t *testing.T) { lookupCase(t, ctx, c.prov, HST, on, c.want, c.err) })
	}
}

// rule: BIL-T-011
func TestBIL_T_011_RealPropertyProvinceIsAJurisdiction(t *testing.T) {
	// The real-property rule resolves to the property's province; each such province looks up.
	cases := []struct {
		prov string
		c    Component
		want money.PPM
	}{
		{"NS", HST, 140000},
		{"ON", HST, 130000},
		{"AB", GST, 50000},
	}
	for _, c := range cases {
		t.Run(c.prov, func(t *testing.T) {
			lookupCase(t, context.Background(), c.prov, c.c, day(2026, time.October, 11), c.want, nil)
		})
	}
}

// rule: BIL-T-013
func TestBIL_T_013_AddressProvinceLooksUp(t *testing.T) {
	cases := []struct {
		prov string
		want money.PPM
	}{
		{"NB", 150000},
		{"PE", 150000},
	}
	for _, c := range cases {
		t.Run(c.prov, func(t *testing.T) {
			lookupCase(t, context.Background(), c.prov, HST, day(2026, time.October, 11), c.want, nil)
		})
	}
}

// rule: BIL-T-014
func TestBIL_T_014_NoAddressFallsBackToOntario(t *testing.T) {
	cases := []struct {
		name string
		want money.PPM
	}{{"resident, no address", 130000}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lookupCase(t, context.Background(), DefaultPerformedProvince, HST, day(2026, time.October, 11), c.want, nil)
		})
	}
}

// rule: BIL-T-015
func TestBIL_T_015_ZeroRated(t *testing.T) {
	for _, c := range []Component{HST, GST, PST, QST} {
		t.Run(string(c), func(t *testing.T) {
			lookupCase(t, context.Background(), Zero, c, day(2026, time.October, 11), 0, nil)
		})
	}
}

// rule: BIL-T-016
func TestBIL_T_016_DisbursementInheritsMatterPlace(t *testing.T) {
	cases := []struct {
		name      string
		place     string
		asAgent   bool
		wantPlace string
		wantTaxed bool
	}{
		{"own cost, Alberta", "AB", false, "AB", true},
		{"own cost, zero-rated", Zero, false, Zero, true},
		{"agent disbursement", "ON", true, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, taxed := DisbursementProvince(c.place, c.asAgent)
			if p != c.wantPlace || taxed != c.wantTaxed {
				t.Fatalf("got %q %v, want %q %v", p, taxed, c.wantPlace, c.wantTaxed)
			}
		})
	}
}

// rule: BIL-T-020
func TestBIL_T_020_ProvincialLineNeedsRegistration(t *testing.T) {
	regs := map[Component]map[string]string{PST: {"BC": "PST-1234-5678"}}
	cases := []struct {
		name  string
		place string
		c     Component
		want  bool
	}{
		{"BC registered", "BC", PST, true},
		{"MB not registered", "MB", PST, false},
		{"QC not registered", "QC", QST, false},
		{"GST is not provincial", "BC", GST, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ProvincialApplies(c.place, c.c, regs); got != c.want {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}

// rule: BIL-T-021
func TestBIL_T_021_BCPST(t *testing.T) {
	lookupCase(t, context.Background(), "BC", PST, day(2026, time.October, 11), 70000, nil)
}

// rule: BIL-T-022
func TestBIL_T_022_QST(t *testing.T) {
	cases := []struct {
		c    Component
		want money.PPM
	}{{QST, 99750}, {GST, 50000}}
	for _, c := range cases {
		t.Run(string(c.c), func(t *testing.T) {
			lookupCase(t, context.Background(), "QC", c.c, day(2026, time.October, 11), c.want, nil)
		})
	}
}

// rule: BIL-T-023
func TestBIL_T_023_ManitobaRST(t *testing.T) {
	lookupCase(t, context.Background(), "MB", PST, day(2026, time.October, 11), 70000, nil)
}

// rule: BIL-T-024
func TestBIL_T_024_SaskatchewanPSTBehindFlag(t *testing.T) {
	on := day(2026, time.October, 11)
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagVerifyRules}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagVerifyRules, true); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ctx  context.Context
		want money.PPM
		err  error
	}{
		{"flag off", context.Background(), 0, ErrUnverified},
		{"flag on", flags.WithSet(context.Background(), s), 60000, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { lookupCase(t, c.ctx, "SK", PST, on, c.want, c.err) })
	}
}

// rule: BIL-T-025
func TestBIL_T_025_GSTOnlyTerritoriesAndAlberta(t *testing.T) {
	on := day(2026, time.October, 11)
	for _, p := range []string{"AB", "YT", "NT", "NU"} {
		t.Run(p, func(t *testing.T) {
			lookupCase(t, context.Background(), p, GST, on, 50000, nil)
			lookupCase(t, context.Background(), p, PST, on, 0, ErrNoRate)
			lookupCase(t, context.Background(), p, HST, on, 0, ErrNoRate)
		})
	}
}

// rule: BIL-T-001
// rule: BIL-T-002
func TestBIL_T_001_RateHistoryBands(t *testing.T) {
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagVerifyRules}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagVerifyRules, true); err != nil {
		t.Fatal(err)
	}
	ctx := flags.WithSet(context.Background(), s)
	// A credit note looks up at its source bill's tax point; an imported historic bill at its
	// own. Both are exercised on each side of every band edge.
	creditNote := func(billDate time.Time) time.Time {
		return TaxPointDate(time.Time{}, billDate)
	}
	imported := func(billDate time.Time) time.Time {
		return TaxPointDate(billDate.AddDate(0, 0, 10), billDate)
	}
	kinds := []struct {
		name  string
		point func(time.Time) time.Time
	}{{"credit note", creditNote}, {"import", imported}}
	cases := []struct {
		prov string
		c    Component
		on   time.Time
		want money.PPM
		err  error
	}{
		{"AB", GST, day(1990, time.December, 31), 0, ErrRateUnknown},
		{"AB", GST, day(1991, time.January, 1), 70000, nil},
		{"AB", GST, day(2006, time.June, 30), 70000, nil},
		{"AB", GST, day(2006, time.July, 1), 60000, nil},
		{"AB", GST, day(2007, time.December, 31), 60000, nil},
		{"AB", GST, day(2008, time.January, 1), 50000, nil},
		{"ON", HST, day(2009, time.June, 1), 0, ErrRateUnknown},
		{"ON", HST, day(2010, time.June, 30), 0, ErrRateUnknown},
		{"ON", HST, day(2010, time.July, 1), 130000, nil},
		{"ON", GST, day(2010, time.June, 30), 50000, nil},
		{"NS", HST, day(1997, time.March, 31), 0, ErrRateUnknown},
		{"NS", HST, day(1997, time.April, 1), 150000, nil},
		{"NS", HST, day(2006, time.July, 1), 140000, nil},
		{"NS", HST, day(2008, time.January, 1), 130000, nil},
		{"NS", HST, day(2010, time.June, 30), 130000, nil},
		{"NS", HST, day(2010, time.July, 1), 150000, nil},
		{"NS", HST, day(2025, time.March, 31), 150000, nil},
		{"NS", HST, day(2025, time.April, 1), 140000, nil},
		{"NB", HST, day(1997, time.March, 31), 0, ErrRateUnknown},
		{"NB", HST, day(2006, time.June, 30), 150000, nil},
		{"NB", HST, day(2007, time.June, 1), 140000, nil},
		{"NB", HST, day(2016, time.June, 30), 130000, nil},
		{"NB", HST, day(2016, time.July, 1), 150000, nil},
		{"NL", HST, day(2008, time.January, 1), 130000, nil},
		{"NL", HST, day(2016, time.June, 30), 130000, nil},
		{"NL", HST, day(2016, time.July, 1), 150000, nil},
		{"PE", HST, day(2013, time.March, 31), 0, ErrRateUnknown},
		{"PE", HST, day(2013, time.April, 1), 140000, nil},
		{"PE", HST, day(2016, time.September, 30), 140000, nil},
		{"PE", HST, day(2016, time.October, 1), 150000, nil},
		{"BC", PST, day(2013, time.March, 31), 0, ErrRateUnknown},
		{"BC", PST, day(2013, time.April, 1), 70000, nil},
		{"QC", QST, day(2012, time.December, 31), 0, ErrRateUnknown},
		{"QC", QST, day(2013, time.January, 1), 99750, nil},
		{"MB", PST, day(2019, time.June, 30), 0, ErrRateUnknown},
		{"MB", PST, day(2019, time.July, 1), 70000, nil},
		{"SK", PST, day(2017, time.March, 22), 0, ErrRateUnknown},
		{"SK", PST, day(2017, time.March, 23), 60000, nil},
	}
	for _, k := range kinds {
		for _, c := range cases {
			t.Run(k.name+"/"+c.prov+string(c.c)+c.on.Format(time.DateOnly), func(t *testing.T) {
				lookupCase(t, ctx, c.prov, c.c, k.point(c.on), c.want, c.err)
				if c.err != nil {
					_, err := NewRateTable(Seed()).Lookup(ctx, c.prov, c.c, k.point(c.on))
					if !errors.Is(err, ErrNoRate) {
						t.Fatalf("ErrRateUnknown must also match ErrNoRate, got %v", err)
					}
				}
			})
		}
	}
}

// rule: BIL-T-001
func TestBIL_T_001_EverySeedRowCitesItsSource(t *testing.T) {
	for _, r := range Seed() {
		t.Run(r.TaxCode+r.EffectiveFrom.Format(time.DateOnly), func(t *testing.T) {
			if r.SourceCite == "" || r.EffectiveFrom.IsZero() {
				t.Fatalf("row %+v lacks source or start date", r)
			}
		})
	}
}
