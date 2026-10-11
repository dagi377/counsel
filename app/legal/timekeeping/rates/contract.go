// CONTRACT: app/legal/timekeeping/rates types and interfaces. Frozen once on main; changes are
// additive only and need a docs/QUESTIONS.md entry. Source: spec/eng/b3-billing-engine.html
// (BIL-C-003, BIL-C-004, BIL-C-005, BIL-C-010, BIL-C-011, BIL-C-012).

// Package rates resolves hourly rates for time entries, rounds time and prices time lines.
// Money is int64 cents; every division goes through money.Rhu.
package rates

import (
	"context"
	"errors"

	"github.com/dagi377/counsel/kernel/clock"
)

// Source is the tier of the resolution chain a rate came from (BIL-C-003). Stored on the
// entry as rate_source. Tiers are listed in resolution order; the first hit wins.
type Source string

const (
	SourceManual       Source = "manual"        // tier 1: manual rate on the entry
	SourceFlatActivity Source = "flat_activity" // tier 2: flat activity amount
	SourceMatterUser   Source = "matter_user"   // tier 3: matter rate for the user
	SourceMatterRole   Source = "matter_role"   // tier 4: matter rate for the role group or table
	SourceClientUser   Source = "client_user"   // tier 5: client rate for the user
	SourceClientRole   Source = "client_role"   // tier 6: client rate for the role group or table
	SourceActivity     Source = "activity"      // tier 7: activity-category hourly rate
	SourceUserDefault  Source = "user_default"  // tier 8: user default rate
)

// Chain is the fixed resolution order (not configurable).
var Chain = []Source{
	SourceManual, SourceFlatActivity, SourceMatterUser, SourceMatterRole,
	SourceClientUser, SourceClientRole, SourceActivity, SourceUserDefault,
}

// RateRow is one effective-dated rate at tiers 2..8. Only the keys that tier uses are set:
// matter tiers use MatterID, client tiers ClientID, user tiers UserID, role tiers RoleGroup,
// activity tiers ActivityID.
type RateRow struct {
	Source        Source
	MatterID      string
	ClientID      string
	UserID        string
	RoleGroup     string
	ActivityID    string
	RateCents     int64
	EffectiveFrom clock.Date // business date
}

// Input is what resolution needs about one time entry.
type Input struct {
	MatterID        string
	ClientID        string
	UserID          string
	RoleGroup       string
	ActivityID      string
	WorkDate        clock.Date // business date
	ManualRateCents *int64     // tier 1 when set
	CanSetRates     bool       // caller holds time.rates or the user override right
}

// Resolved is the result of resolution, stored on the entry as rate_cents and rate_source.
// For SourceFlatActivity, RateCents is the flat amount of the line, not an hourly rate.
type Resolved struct {
	RateCents int64
	Source    Source
}

// Mode is the time rounding mode (BIL-C-010).
type Mode string

const (
	ModeUp      Mode = "up"
	ModeNearest Mode = "nearest" // half-up
)

// Rounding is the time setting copied onto each entry at save.
type Rounding struct {
	IncrementMinutes int // 1, 6 or 15
	Mode             Mode
	MinimumEntry     int // minutes; 0 = off (BIL-C-012)
}

// Entry is the rate-relevant view of a time entry.
type Entry struct {
	ID          string
	Input       Input
	Resolved    Resolved
	Billed      bool
	RateRecheck bool
}

// Resolver resolves a rate through the fixed chain (BIL-C-003, BIL-C-004).
type Resolver interface {
	// Resolve returns the first hit in Chain for in, using rows effective on in.WorkDate.
	// A manual rate without in.CanSetRates returns ErrManualRateForbidden.
	Resolve(ctx context.Context, in Input) (Resolved, error)
}

// Table stores effective-dated rate rows.
type Table interface {
	// Put adds a rate row. A row with Source manual returns ErrBadRow.
	Put(ctx context.Context, row RateRow) error
}

// Pricer turns raw minutes into billed minutes and a line amount.
type Pricer interface {
	// BilledMinutes applies rounding and the minimum entry (BIL-C-010, BIL-C-012).
	BilledMinutes(rawMinutes int, r Rounding) (int, error)
	// Amount is rhu(billedMinutes * rateCents, 60) (BIL-C-011).
	Amount(billedMinutes int, rateCents int64) (int64, error)
}

// Repricer applies rate changes and matter moves to entries.
type Repricer interface {
	// Reprice re-resolves the unbilled entries dated on or after row.EffectiveFrom after
	// row is added (BIL-C-004) and returns the ones whose rate changed.
	Reprice(ctx context.Context, row RateRow, entries []Entry) ([]Entry, error)
	// Move re-resolves tiers 2..8 for e on toMatterID; a manual rate is kept and
	// RateRecheck is set (BIL-C-005).
	Move(ctx context.Context, e Entry, toMatterID, toClientID string) (Entry, error)
}

// Service is the whole rates surface.
type Service interface {
	Resolver
	Table
	Pricer
	Repricer
}

// ErrNoRate is returned when no tier yields a rate.
var ErrNoRate = errors.New("rates: no rate resolves")

// ErrManualRateForbidden is returned for a manual rate without time.rates (HTTP 403).
var ErrManualRateForbidden = errors.New("rates: manual rate needs time.rates")

// ErrBadRounding is returned for an increment outside {1, 6, 15} or an unknown mode.
var ErrBadRounding = errors.New("rates: bad rounding setting")

// ErrBadRow is returned by Put for a row with a missing key, a manual source or a negative rate.
var ErrBadRow = errors.New("rates: bad rate row")
