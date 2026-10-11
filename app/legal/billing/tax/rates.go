package tax

import (
	"context"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/kernel/money"
)

// FlagVerifyRules gates rates whose taxability is unconfirmed (SK PST). It is
// legalflags.TaxVerifyRules.
const FlagVerifyRules = legalflags.TaxVerifyRules

// DefaultPerformedProvince is where the firm primarily performs its work. It is the place of
// supply for a Canadian-resident client with no Canadian address on file.
//
// rule: BIL-T-014
const DefaultPerformedProvince = "ON"

// Zero is the place-of-supply result for a zero-rated supply.
//
// rule: BIL-T-015
const Zero = "ZERO"

// dateOf keeps only the calendar date of t, in t's own zone, so business dates compare
// as dates.
func dateOf(t time.Time) time.Time { return day(t.Year(), t.Month(), t.Day()) }

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// ErrRateUnknown is returned for a tax point before the earliest seeded rate for the code,
// for example ON HST before 2010-07-01. It wraps ErrNoRate, so errors.Is(err, ErrNoRate)
// still holds for existing callers.
var ErrRateUnknown = fmt.Errorf("%w: tax point before earliest known rate", ErrNoRate)

// Source citations for the seeded rows.
const (
	citeETA165   = "Excise Tax Act s.165(1); CRA, GST/HST rates by province"
	citeHSTSched = "Excise Tax Act s.165(2), Sch. VIII; CRA, GST/HST rates by province"
)

// Seed returns the tax rate rows seeded into billing.tax_rate. Rates are data, not code.
// Each row is the full history for its code, so a credit note (source bill's tax point)
// or an imported historic bill gets the rate of its own date. A tax point before a code's
// first row is refused with ErrRateUnknown rather than given today's rate.
//
// (verify) Every row is legal data an owner confirms against its cited source before
// go-live. ON before 2010-07-01 was GST plus 8% RST on taxable legal services; it is not
// modelled, so ON HST before that date is ErrRateUnknown. PE before 2013-04-01 and NS, NB
// and NL before 1997-04-01 were GST plus provincial sales tax; likewise not modelled.
//
// rule: BIL-T-001
func Seed() []Rate {
	open := time.Time{}
	hst := func(prov string, ppm money.PPM, from, to time.Time, cite string) Rate {
		return Rate{TaxCode: "HST-" + prov, Component: HST, Province: prov, RatePPM: ppm, EffectiveFrom: from, EffectiveTo: to, SourceCite: cite}
	}
	prov := func(c Component, prov string, ppm money.PPM, from time.Time, cite string) Rate {
		return Rate{TaxCode: string(c) + "-" + prov, Component: c, Province: prov, RatePPM: ppm, EffectiveFrom: from, SourceCite: cite}
	}
	gst := func(ppm money.PPM, from, to time.Time, cite string) Rate {
		return Rate{TaxCode: "GST", Component: GST, RatePPM: ppm, EffectiveFrom: from, EffectiveTo: to, SourceCite: cite}
	}
	var (
		gstStart = day(1991, time.January, 1) // GST introduced
		cut2006  = day(2006, time.July, 1)    // GST 7% -> 6%
		cut2008  = day(2008, time.January, 1) // GST 6% -> 5%
		hst1997  = day(1997, time.April, 1)   // HST in NS, NB, NL
		hst2010  = day(2010, time.July, 1)    // HST in ON; NS 13% -> 15%
		pe2013   = day(2013, time.April, 1)   // HST in PE
		nbnl2016 = day(2016, time.July, 1)    // NB, NL 13% -> 15%
		pe2016   = day(2016, time.October, 1) // PE 14% -> 15%
		ns2025   = day(2025, time.April, 1)   // NS 15% -> 14%
	)
	return []Rate{
		// (verify) GST 7% at introduction, Goods and Services Tax Act, S.C. 1990, c.45.
		gst(70000, gstStart, cut2006, citeETA165+" (7% from 1991-01-01)"),
		// (verify) GST 6%, Budget Implementation Act 2006, S.C. 2006, c.4.
		gst(60000, cut2006, cut2008, citeETA165+" (6% from 2006-07-01)"),
		// (verify) GST 5%, Budget and Economic Statement Implementation Act 2007, S.C. 2007, c.35.
		gst(50000, cut2008, open, citeETA165+" (5% from 2008-01-01)"),
		// (verify) ON HST 13% from 2010-07-01; earlier ON dates are ErrRateUnknown (GST + RST era).
		hst("ON", 130000, hst2010, open, citeHSTSched+" (ON 13% from 2010-07-01)"),
		// (verify) NS HST 15% at harmonization; 14% and 13% follow the federal cuts.
		hst("NS", 150000, hst1997, cut2006, citeHSTSched+" (NS 15% from 1997-04-01)"),
		hst("NS", 140000, cut2006, cut2008, citeHSTSched+" (NS 14% from 2006-07-01)"),
		hst("NS", 130000, cut2008, hst2010, citeHSTSched+" (NS 13% from 2008-01-01)"),
		// (verify) NS raised its component to 10% (15% HST), Financial Measures (2010) Act.
		hst("NS", 150000, hst2010, ns2025, citeHSTSched+" (NS 15% from 2010-07-01)"),
		// (verify) NS cut its component to 9% (14% HST), Financial Measures (2024) Act.
		hst("NS", 140000, ns2025, open, citeHSTSched+" (NS 14% from 2025-04-01)"),
		// (verify) NB HST history.
		hst("NB", 150000, hst1997, cut2006, citeHSTSched+" (NB 15% from 1997-04-01)"),
		hst("NB", 140000, cut2006, cut2008, citeHSTSched+" (NB 14% from 2006-07-01)"),
		hst("NB", 130000, cut2008, nbnl2016, citeHSTSched+" (NB 13% from 2008-01-01)"),
		hst("NB", 150000, nbnl2016, open, citeHSTSched+" (NB 15% from 2016-07-01)"),
		// (verify) NL HST history.
		hst("NL", 150000, hst1997, cut2006, citeHSTSched+" (NL 15% from 1997-04-01)"),
		hst("NL", 140000, cut2006, cut2008, citeHSTSched+" (NL 14% from 2006-07-01)"),
		hst("NL", 130000, cut2008, nbnl2016, citeHSTSched+" (NL 13% from 2008-01-01)"),
		hst("NL", 150000, nbnl2016, open, citeHSTSched+" (NL 15% from 2016-07-01)"),
		// (verify) PE joined HST at 14% on 2013-04-01 and moved to 15% on 2016-10-01.
		hst("PE", 140000, pe2013, pe2016, citeHSTSched+" (PE 14% from 2013-04-01)"),
		hst("PE", 150000, pe2016, open, citeHSTSched+" (PE 15% from 2016-10-01)"),
		// (verify) BC PST 7% since BC left HST on 2013-04-01; earlier dates are ErrRateUnknown.
		// rule: BIL-T-021
		prov(PST, "BC", 70000, day(2013, time.April, 1), "BC PST Bulletin PST 106 (7% from 2013-04-01)"),
		// (verify) QST 9.975% since 2013-01-01; earlier dates are ErrRateUnknown.
		// rule: BIL-T-022
		prov(QST, "QC", 99750, day(2013, time.January, 1), "Revenu Quebec, QST rate (9.975% from 2013-01-01)"),
		// (verify) MB RST 7% since 2019-07-01 (8% before); earlier dates are ErrRateUnknown.
		// rule: BIL-T-023
		prov(PST, "MB", 70000, day(2019, time.July, 1), "Manitoba RST Bulletin 056 (7% from 2019-07-01)"),
		// (verify) SK PST 6% since 2017-03-23; earlier dates are ErrRateUnknown.
		// rule: BIL-T-024 (verify)
		prov(PST, "SK", 60000, day(2017, time.March, 23), "SK Provincial Sales Tax Act (6% from 2017-03-23)"),
		// rule: BIL-T-025
		// AB, YT, NT and NU have no provincial rows: GST only.
	}
}

// RateTable looks rates up in a set of rows.
type RateTable struct {
	rows []Rate
}

var _ Rates = (*RateTable)(nil)

// NewRateTable returns a table over rows; Seed() is the usual input.
func NewRateTable(rows []Rate) *RateTable { return &RateTable{rows: rows} }

// Lookup returns the rate in effect for the province and component on the tax point date.
// Federal GST ignores the province. A ZERO place yields 0. SK PST is refused with
// ErrUnverified while FlagVerifyRules is off.
func (t *RateTable) Lookup(ctx context.Context, province string, c Component, on time.Time) (money.PPM, error) {
	// rule: BIL-T-010
	if province == Zero {
		return 0, nil
	}
	if !provinces[province] {
		return 0, fmt.Errorf("lookup %q: %w", province, ErrUnknownProvince)
	}
	// rule: BIL-T-024 (verify)
	if province == "SK" && c == PST && !legalflags.Enabled(ctx, FlagVerifyRules) {
		return 0, fmt.Errorf("lookup SK PST: %w", ErrUnverified)
	}
	// rule: BIL-T-002
	on = dateOf(on)
	matched, early := false, true
	for _, r := range t.rows {
		if r.Component != c || (c != GST && r.Province != province) {
			continue
		}
		matched = true
		if !on.Before(r.EffectiveFrom) {
			early = false
		}
		if on.Before(r.EffectiveFrom) || (!r.EffectiveTo.IsZero() && !on.Before(r.EffectiveTo)) {
			continue
		}
		return r.RatePPM, nil
	}
	// rule: BIL-T-001
	if matched && early {
		return 0, fmt.Errorf("lookup %s %s %s: %w", province, c, on.Format(time.DateOnly), ErrRateUnknown)
	}
	return 0, fmt.Errorf("lookup %s %s %s: %w", province, c, on.Format(time.DateOnly), ErrNoRate)
}

// TaxPointDate is the date whose rate applies to a bill: the earlier of the day it is paid
// and the day it is due (the bill date). A zero paidOn means not yet paid; a trust retainer
// is not payment until applied to the bill. A credit note uses its source bill's tax point,
// never its own date.
func TaxPointDate(paidOn, dueOn time.Time) time.Time {
	// rule: BIL-T-002
	if !paidOn.IsZero() && paidOn.Before(dueOn) {
		return paidOn
	}
	return dueOn
}

// ProvincialApplies reports whether a provincial component (PST or QST) may be charged at a
// place of supply: only when the place is in that province and the firm holds a
// registration number for it. Otherwise the bill carries a self-assessment review flag.
func ProvincialApplies(place string, c Component, registrations map[Component]map[string]string) bool {
	// rule: BIL-T-020
	if c != PST && c != QST {
		return false
	}
	return registrations[c][place] != ""
}

// DisbursementProvince is the place of supply for a firm's own-cost disbursement: it inherits
// the matter's place (including ZERO). Agent disbursements are outside the tax base and
// have none.
func DisbursementProvince(matterPlace string, asAgent bool) (string, bool) {
	// rule: BIL-T-016
	if asAgent {
		return "", false
	}
	return matterPlace, true
}

// provinces are the place-of-supply jurisdiction codes. A real-property or address rule
// resolves to one of these.
//
// rule: BIL-T-011
// rule: BIL-T-013
var provinces = map[string]bool{
	"ON": true, "NS": true, "NB": true, "NL": true, "PE": true, "BC": true, "AB": true,
	"SK": true, "MB": true, "QC": true, "YT": true, "NT": true, "NU": true,
}
