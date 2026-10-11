package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/rulepacks/ontario"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolShiftDate adds or subtracts calendar days or business days from a date the run read
// (an agent run's start date must be in its provenance).
// Business days skip weekends and the Ontario court holidays, in America/Toronto, and are
// refused while deadline.ontario_pack is off. It is a pure computation: nothing is saved.
const ToolShiftDate = "shift_date"

// ErrHolidayPackUnverified: a business-day shift was asked for while deadline.ontario_pack is
// off. The Ontario holiday calendar is not verified, so no business day is counted with it.
var ErrHolidayPackUnverified = errors.New("agent: shift_date: holiday pack not verified (deadline.ontario_pack is off)")

// Shift units.
const (
	ShiftCalendar = "calendar"
	ShiftBusiness = "business"
)

// ShiftDateHandler is shift_date's handler, for the boot HandlerSet.
func ShiftDateHandler() Handler {
	return Handler{Name: ToolShiftDate, Build: buildShiftDate}
}

// buildShiftDate reads Deps: CapabilityCheck and the run's *agenttools.Provenance.
func buildShiftDate(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolShiftDate, err)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil || pv == nil {
		return registry.Tool{}, fmt.Errorf("%s: provenance: %w", ToolShiftDate, ErrMissingDep)
	}
	holidays, err := ontario.Holidays()
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolShiftDate, err)
	}
	s := shiftDate{holidays: holidays}
	t, err := handlerTool(ToolShiftDate, Read, can, s.handle)
	if err != nil {
		return registry.Tool{}, err
	}
	// rule: ARC-U-027
	// The shifted date is a date the run read, so a later write in the run may carry it.
	t, err = pv.ReadTool(t)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolShiftDate, err)
	}
	h := t.Handle
	t.Handle = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
		if p.Kind == registry.PrincipalAgent {
			// rule: ARC-U-027
			// The start date must itself be a date the run read, so shift_date never turns an
			// invented date into a sourced one.
			if err := pv.Check(p.RunID, []string{"/date"}, in); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolShiftDate, err)
			}
		}
		return h(ctx, p, in)
	}
	return t, nil
}

type shiftDate struct {
	holidays []clock.Date
}

// shiftDateOut carries dates only, so it needs no quoted envelope. Date is the shifted
// YYYY-MM-DD date. The day count is not echoed: provenance records every number in a read
// result, and a count the model chose must never become a sourced amount.
type shiftDateOut struct {
	From string `json:"from"`
	Unit string `json:"unit"`
	Date string `json:"date"`
}

func (s shiftDate) handle(ctx context.Context, _ string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		Date string `json:"date"`
		Days int    `json:"days"`
		Unit string `json:"unit"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolShiftDate, ErrInvalidInput)
	}
	t, err := time.Parse(time.DateOnly, in.Date)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: date: %w", ToolShiftDate, ErrInvalidInput)
	}
	from := clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
	var to clock.Date
	switch in.Unit {
	case ShiftCalendar:
		to = from.AddDays(in.Days)
	case ShiftBusiness:
		// V-DEADLINES: fail closed: the holiday calendar is unverified until deadline.ontario_pack is on, so
		// a business-day count is refused rather than computed on weekends alone.
		if !legalflags.Enabled(ctx, legalflags.DeadlineOntarioPack) {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolShiftDate, ErrHolidayPackUnverified)
		}
		to = s.businessDays(from, in.Days)
	default:
		return registry.ToolResult{}, fmt.Errorf("%s: unit: %w", ToolShiftDate, ErrInvalidInput)
	}
	b, err := json.Marshal(shiftDateOut{From: from.String(), Unit: in.Unit, Date: to.String()})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolShiftDate, err)
	}
	return registry.ToolResult{Output: b}, nil
}

// businessDays moves n business days from d, forward when n is positive and back when it is
// negative. Weekends and holidays are skipped and do not count; zero returns d unchanged.
func (s shiftDate) businessDays(d clock.Date, n int) clock.Date {
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for i := 0; i < n; {
		d = d.AddDays(step)
		if deadline.IsBusinessDay(d, s.holidays) {
			i++
		}
	}
	return d
}
