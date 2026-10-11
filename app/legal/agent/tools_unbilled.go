package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolListUnbilled lists a matter's approved time that is on no bill, with its amounts.
const ToolListUnbilled = "list_unbilled"

// CapBillingView is the capability list_unbilled needs.
const CapBillingView = "billing.view"

// unbilledZone is the business time zone a work date is read in.
const unbilledZone = "America/Toronto"

// PricedEntry is a time entry with its resolved rate and line amount, in integer cents.
type PricedEntry struct {
	Entry       timekeeping.TimeEntry
	RateCents   int64
	AmountCents int64 // rhu(billed minutes * rate, 60), or the flat activity amount
}

// MatterTime lists a matter's time entries priced at their resolved rates
// (app/legal/timekeeping entries with app/legal/timekeeping/rates, wired at boot).
type MatterTime interface {
	MatterTime(ctx context.Context, matterID string) ([]PricedEntry, error)
}

// ListUnbilledHandler is list_unbilled's handler, for the boot HandlerSet.
func ListUnbilledHandler() Handler {
	return Handler{Name: ToolListUnbilled, Build: buildListUnbilled}
}

// buildListUnbilled reads Deps: CapabilityCheck, roles.Walls and MatterTime.
func buildListUnbilled(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListUnbilled, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListUnbilled, err)
	}
	mt, err := DepOf[MatterTime](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListUnbilled, err)
	}
	loc, err := time.LoadLocation(unbilledZone)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolListUnbilled, err)
	}
	l := listUnbilled{walls: walls, entries: mt, loc: loc}
	t, err := handlerTool(ToolListUnbilled, Read, can, l.handle)
	if err != nil {
		return registry.Tool{}, err
	}
	// rule: ARC-U-027
	// The cents this tool returns (rate, amount, total) are the figures a later write in the
	// run may carry. Boot's AgentTools records every read tool's results in the run's
	// Provenance, so a draft_bill write-down taken from them is sourced.
	return t, nil
}

type listUnbilled struct {
	walls   roles.Walls
	entries MatterTime
	loc     *time.Location
}

// unbilledOut is one unbilled entry: ids, minutes and integer-cent amounts. Its narrative is
// client text and travels as quotes[i], the quoted envelope at the same index. Amounts are
// numbers an agent run's provenance records, so a later write in the run may carry them.
type unbilledOut struct {
	EntryID       string `json:"entry_id"`
	UserID        string `json:"user_id"`
	WorkDate      string `json:"work_date"`
	Minutes       int    `json:"minutes"`
	BilledMinutes int    `json:"billed_minutes"`
	ActivityCode  string `json:"activity_code,omitempty"`
	RateCents     int64  `json:"rate_cents"`
	AmountCents   int64  `json:"amount_cents"`
}

type listUnbilledOut struct {
	MatterID   string              `json:"matter_id"`
	Entries    []unbilledOut       `json:"entries"`
	TotalCents int64               `json:"total_cents"`
	Quotes     []agenttools.Quoted `json:"quotes"`
}

// TimeEntryCitation cites a time entry record. An entry has no document version, so the
// record id stands in for the version, at page 1, line 1.
func TimeEntryCitation(id timekeeping.EntryID) agenttools.Citation {
	return agenttools.Citation{VersionID: "time_entry:" + string(id), Page: 1, FromLine: 1, ToLine: 1}
}

func (l listUnbilled) handle(ctx context.Context, user string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		MatterID string `json:"matter_id"`
		PeriodTo string `json:"period_to"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListUnbilled, ErrInvalidInput)
	}
	var to *clock.Date
	if in.PeriodTo != "" {
		t, err := time.Parse(time.DateOnly, in.PeriodTo)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: period_to: %w", ToolListUnbilled, ErrInvalidInput)
		}
		d := clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
		to = &d
	}
	// rule: ARC-I-036
	if err := visibleTo(ctx, l.walls, user, in.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListUnbilled, err)
	}
	priced, err := l.entries.MatterTime(ctx, in.MatterID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: time: %w", ToolListUnbilled, err)
	}
	var keep []PricedEntry
	for _, p := range priced {
		e := p.Entry
		// Unbilled time is approved, billable and on no bill (b3 WIP).
		if e.MatterID != in.MatterID || e.State != timekeeping.StateApproved || e.BillID != "" || !e.Billable {
			continue
		}
		if to != nil && to.Before(clock.DateOf(e.WorkDate, l.loc)) {
			continue
		}
		if p.AmountCents < 0 || p.RateCents < 0 {
			return registry.ToolResult{}, fmt.Errorf("%s: entry %s: negative amount: %w", ToolListUnbilled, e.ID, ErrInvalidInput)
		}
		keep = append(keep, p)
	}
	sort.SliceStable(keep, func(i, j int) bool {
		a, b := keep[i].Entry, keep[j].Entry
		if !a.WorkDate.Equal(b.WorkDate) {
			return a.WorkDate.Before(b.WorkDate)
		}
		return a.ID < b.ID
	})
	out := listUnbilledOut{MatterID: in.MatterID, Entries: []unbilledOut{}, Quotes: []agenttools.Quoted{}}
	var cites []agenttools.Citation
	for _, p := range keep {
		e := p.Entry
		if out.TotalCents > math.MaxInt64-p.AmountCents {
			return registry.ToolResult{}, fmt.Errorf("%s: total overflows: %w", ToolListUnbilled, ErrInvalidInput)
		}
		out.TotalCents += p.AmountCents
		out.Entries = append(out.Entries, unbilledOut{
			EntryID: string(e.ID), UserID: e.UserID, WorkDate: clock.DateOf(e.WorkDate, l.loc).String(),
			Minutes: e.Minutes, BilledMinutes: e.BilledMinutes, ActivityCode: e.ActivityCode,
			RateCents: p.RateCents, AmountCents: p.AmountCents,
		})
		// rule: ARC-I-038
		// The narrative is client text: it goes back quoted as untrusted data with its citation.
		src := TimeEntryCitation(e.ID)
		out.Quotes = append(out.Quotes, agenttools.Quote(src, e.Narrative))
		cites = append(cites, src)
	}
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolListUnbilled, err)
	}
	return registry.ToolResult{Output: b, Citations: cites}, nil
}
