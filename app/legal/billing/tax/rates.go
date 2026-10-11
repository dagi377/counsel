package tax

import (
	"context"
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/money"
)

// FlagVerifyRules gates rates whose taxability is unconfirmed (SK PST). It mirrors
// legal.FlagTaxVerifyRules; this package cannot import app/legal (import cycle).
const FlagVerifyRules = "tax.verify_rules"

// DefaultPerformedProvince is where the firm primarily performs its work. It is the place of
// supply for a Canadian-resident client with no Canadian address on file.
//
// rule: BIL-T-014
const DefaultPerformedProvince = "ON"

// Zero is the place-of-supply result for a zero-rated supply.
//
// rule: BIL-T-015
const Zero = "ZERO"

const craRates = "CRA, GST/HST rates by province"

// dateOf keeps only the calendar date of t, in t's own zone, so business dates compare
// as dates.
func dateOf(t time.Time) time.Time { return day(t.Year(), t.Month(), t.Day()) }

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// Seed returns the tax rate rows seeded into billing.tax_rate. Rates are data, not code.
func Seed() []Rate {
	since := day(1991, time.January, 1)
	hst := func(prov string, ppm money.PPM, from, to time.Time) Rate {
		return Rate{TaxCode: "HST-" + prov, Component: HST, Province: prov, RatePPM: ppm, EffectiveFrom: from, EffectiveTo: to, SourceCite: craRates}
	}
	prov := func(c Component, prov string, ppm money.PPM, cite string) Rate {
		return Rate{TaxCode: string(c) + "-" + prov, Component: c, Province: prov, RatePPM: ppm, EffectiveFrom: since, SourceCite: cite}
	}
	nsCut := day(2025, time.April, 1)
	return []Rate{
		{TaxCode: "GST", Component: GST, RatePPM: 50000, EffectiveFrom: since, SourceCite: craRates},
		hst("ON", 130000, since, time.Time{}),
		hst("NB", 150000, since, time.Time{}),
		hst("NL", 150000, since, time.Time{}),
		hst("PE", 150000, since, time.Time{}),
		hst("NS", 150000, since, nsCut),
		hst("NS", 140000, nsCut, time.Time{}),
		// rule: BIL-T-021
		prov(PST, "BC", 70000, "BC PST Bulletin PST 106"),
		// rule: BIL-T-022
		prov(QST, "QC", 99750, "Revenu Quebec, QST rate"),
		// rule: BIL-T-023
		prov(PST, "MB", 70000, "Manitoba RST Bulletin 056"),
		// rule: BIL-T-024 (verify)
		prov(PST, "SK", 60000, "SK Provincial Sales Tax Act"),
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
	if province == "SK" && c == PST && !flags.Enabled(ctx, FlagVerifyRules) {
		return 0, fmt.Errorf("lookup SK PST: %w", ErrUnverified)
	}
	// rule: BIL-T-002
	on = dateOf(on)
	for _, r := range t.rows {
		if r.Component != c || (c != GST && r.Province != province) {
			continue
		}
		if on.Before(r.EffectiveFrom) || (!r.EffectiveTo.IsZero() && !on.Before(r.EffectiveTo)) {
			continue
		}
		return r.RatePPM, nil
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
