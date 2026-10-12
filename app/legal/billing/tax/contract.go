// CONTRACT: app/legal/billing/tax. Frozen once on main; changes are additive only and need
// a docs/QUESTIONS.md entry. Bodies live in the T-014 subtasks (rates.go, pos.go,
// compute.go, bn.go, verify_rules.go).

// Package tax computes Canadian sales tax on legal fees and disbursements: rate lookup,
// place of supply, per-component tax lines (no tax on tax) and business number checks.
package tax

import (
	"context"
	"errors"
	"time"

	"github.com/dagi377/counsel/kernel/money"
)

// Sentinel errors.
var (
	// ErrNoRate is returned when no rate is in effect for the code, component and date.
	ErrNoRate = errors.New("tax: no rate in effect")
	// ErrUnknownProvince is returned for a province or territory code that is not Canadian.
	ErrUnknownProvince = errors.New("tax: unknown province")
	// ErrBadBN is returned when a business number fails its format or check digit.
	ErrBadBN = errors.New("tax: invalid business number")
	// ErrUnverified is returned when a rule that needs V-TAX confirmation is reached while its flag is off.
	ErrUnverified = errors.New("tax: rule not verified")
	// ErrResidencyUnknown is returned for a client with a foreign address whose
	// non-residence has not been determined and recorded by a lawyer (BIL-T-015).
	ErrResidencyUnknown = errors.New("tax: non-residence not attested")
)

// Component is one tax component; it mirrors billing.tax_rate.component.
type Component string

// Components.
const (
	HST  Component = "HST"
	GST  Component = "GST"
	PST  Component = "PST"
	QST  Component = "QST"
	None Component = "NONE"
)

// Rate is one row of billing.tax_rate. Dates are business dates (America/Toronto).
type Rate struct {
	TaxCode       string    // "HST-ON", "GST", "HST-NS", "ZERO", "EXEMPT"
	Component     Component //
	Province      string    // two-letter code; empty for federal GST
	RatePPM       money.PPM // 13% = 130000
	EffectiveFrom time.Time // first day in effect
	EffectiveTo   time.Time // first day no longer in effect; zero when open-ended
	SourceCite    string    // where the rate comes from
}

// Supply describes what is being taxed, for the place-of-supply decision.
type Supply struct {
	ClientProvince string    // client's province code from its address
	ServiceType    string    // decision-table service type, e.g. "general"
	Date           time.Time // business date of the supply

	// Additive (T-014.2a). Zero values keep the earlier behaviour: a recipient with a
	// Canadian address is taxed by that address.

	// PropertyProvince is where the real property the service relates to is situated;
	// empty when the service is not in relation to real property (BIL-T-011).
	PropertyProvince string
	// CourtProvince is the province whose court or tribunal has the litigation, set only for
	// services rendered after the litigation commenced; empty otherwise (BIL-T-012).
	CourtProvince string
	// RecipientResident is true when the client is a Canadian resident (ETA s.132).
	RecipientResident bool
	// NonResidentAttestedBy and NonResidentAttestedAt record who determined that the client
	// is not a Canadian resident, and when (additive, LAW-P5). A foreign address alone never
	// zero-rates a supply: without this record the decision fails with ErrResidencyUnknown.
	NonResidentAttestedBy string
	NonResidentAttestedAt time.Time
	// RecipientBusiness is true when the client is a corporation, trust or partnership,
	// false for an individual (ETA Sch. VI Part V s.23(a) covers individuals only).
	RecipientBusiness bool
	// MetInCanada is true when a non-resident individual received the service in Canada.
	MetInCanada bool
	// AgentRole is true when the firm acts as the client's agent or procures supplies for it
	// (ETA Sch. VI Part V s.23(d)).
	AgentRole bool
	// RegisteredProvinces are the provinces whose provincial sales tax the firm is
	// registered to collect (BIL-T-020).
	RegisteredProvinces []string
}

// Place is the outcome of the place-of-supply decision: where the supply is made and
// which components apply there.
type Place struct {
	Province   string
	Components []Component
	Rules      []string // rule ids that decided it
}

// Line is one computed tax line. Each component is computed on the pre-tax base: there is
// never tax on tax.
type Line struct {
	Component    Component
	Jurisdiction string // province code, or "CA" for GST
	RatePPM      money.PPM
	Base         money.Cents
	Amount       money.Cents // money.Rhu(Base*RatePPM, 1e6)
	Label        string      // name printed on the bill when it differs from Component, e.g. "RST" for MB PST
}

// Rates looks up the rate in effect for a province and component on a date.
type Rates interface {
	Lookup(ctx context.Context, province string, c Component, on time.Time) (money.PPM, error)
}

// PlaceOfSupply decides where a supply is made and which components apply.
type PlaceOfSupply interface {
	Decide(ctx context.Context, s Supply) (Place, error)
}

// Calculator computes tax lines for a pre-tax base at a place of supply on a date.
type Calculator interface {
	Compute(ctx context.Context, base money.Cents, p Place, on time.Time) ([]Line, error)
}

// BNChecker validates a business number such as "123456789RT0001".
type BNChecker interface {
	CheckBN(bn string) error
}

// Service is the whole tax surface other billing tasks call.
type Service interface {
	Rates
	PlaceOfSupply
	Calculator
	BNChecker
}
