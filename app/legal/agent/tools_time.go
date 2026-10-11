package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolProposeTimeEntries suggests time entries for the acting user. The entries stay
// suggested until that user accepts them through the timekeeping.accept gate.
const ToolProposeTimeEntries = "propose_time_entries"

// GateTimeAccept is the Counsel gate kind a batch of suggested time entries opens (spec G9).
const GateTimeAccept = "timekeeping.accept"

// SubjectTimeEntry is the gate subject kind of suggested time entries: the gate names the
// first entry of the batch and its payload lists them all.
const SubjectTimeEntry = "timekeeping.time_entry"

// businessZone is where a work date is a calendar day.
const businessZone = "America/Toronto"

// ProposeTimeEntriesHandler is propose_time_entries' handler, for the boot HandlerSet.
func ProposeTimeEntriesHandler() Handler {
	return Handler{Name: ToolProposeTimeEntries, Build: buildProposeTimeEntries}
}

// buildProposeTimeEntries reads Deps: CapabilityCheck, roles.Walls, timekeeping.Entries,
// agenttools.GateOpener and *agenttools.Provenance.
func buildProposeTimeEntries(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	entries, err := DepOf[tk.Entries](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	open, err := DepOf[agenttools.GateOpener](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	loc, err := time.LoadLocation(businessZone)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: zone: %w", ToolProposeTimeEntries, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolProposeTimeEntries, Kind: Write, GateKind: GateTimeAccept}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	s := specs[0]
	t := proposeTime{can: can, walls: walls, entries: entries, open: agenttools.ScopedOpener(open), capability: s.Capability, loc: loc}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	// rule: LSO-3.6
	// rule: ARC-U-027
	// A suggested entry becomes a client bill, so its work date and minutes must be values
	// the run read: a structured read result or a date or number a quoted transcript or
	// document page states. An invented figure is refused before anything is saved.
	tool, err = pv.SourcedWrite(tool, "/entries/*/work_date", "/entries/*/minutes")
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	return tool, nil
}

type proposeTime struct {
	can        CapabilityCheck
	walls      roles.Walls
	entries    tk.Entries
	open       agenttools.GateOpener
	capability string
	loc        *time.Location
}

type timeEntryIn struct {
	MatterID   string `json:"matter_id"`
	WorkDate   string `json:"work_date"`
	Minutes    int    `json:"minutes"`
	Narrative  string `json:"narrative"`
	ActivityID string `json:"activity_category_id"`
}

type proposeTimeIn struct {
	Entries []timeEntryIn `json:"entries"`
}

// timeAcceptEntry is one entry of the G9 payload: ids and minutes, no narrative.
type timeAcceptEntry struct {
	EntryID  string `json:"entry_id"`
	MatterID string `json:"matter_id"`
	WorkDate string `json:"work_date"`
	Minutes  int    `json:"minutes"`
}

func (t proposeTime) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolProposeTimeEntries, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, ErrForbidden)
	}
	var in proposeTimeIn
	if err := json.Unmarshal(raw, &in); err != nil || len(in.Entries) == 0 {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, ErrInvalidInput)
	}
	// Check every entry before saving any, so a refused call leaves nothing behind.
	dates := make([]time.Time, len(in.Entries))
	for i, e := range in.Entries {
		// rule: ARC-I-036
		if err := visibleTo(ctx, t.walls, user, e.MatterID); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: entry %d: %w", ToolProposeTimeEntries, i, err)
		}
		d, err := time.ParseInLocation(time.DateOnly, e.WorkDate, t.loc)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: entry %d: work date: %w", ToolProposeTimeEntries, i, ErrInvalidInput)
		}
		dates[i] = d
	}
	// rule: ARC-I-035
	// An agent run whose feature does not declare the accept gate suggests nothing.
	if err := agenttools.CheckGateKind(ctx, p, GateTimeAccept); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	by := tk.Actor{UserID: user, Agent: p.Kind == registry.PrincipalAgent}
	payload := struct {
		Entries []timeAcceptEntry `json:"entries"`
	}{Entries: make([]timeAcceptEntry, 0, len(in.Entries))}
	for i, e := range in.Entries {
		// rule: ARC-I-035
		// Suggest leaves the entry suggested; only its owner accepts it through the gate.
		// The entry belongs to the acting user only: the input has no field for anyone else.
		id, err := t.entries.Suggest(ctx, nil, tk.TimeEntry{
			MatterID: e.MatterID, UserID: user, WorkDate: dates[i], Minutes: e.Minutes,
			ActivityCode: e.ActivityID, Narrative: e.Narrative, Billable: true, Origin: tk.OriginSuggestion,
		}, by)
		if err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: entry %d: %w", ToolProposeTimeEntries, i, err)
		}
		payload.Entries = append(payload.Entries, timeAcceptEntry{EntryID: string(id), MatterID: e.MatterID, WorkDate: e.WorkDate, Minutes: e.Minutes})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: payload: %w", ToolProposeTimeEntries, err)
	}
	// One gate per batch.
	subject := registry.Subject{Kind: SubjectTimeEntry, ID: payload.Entries[0].EntryID}
	// rule: ARC-I-039
	// The calling principal is the preparer; an agent principal can never decide the gate.
	gateID, err := t.open(ctx, GateTimeAccept, subject, body, p)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: open gate: %w", ToolProposeTimeEntries, err)
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeGate, Ref: string(gateID)})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeTimeEntries, err)
	}
	return registry.ToolResult{Output: out, GateID: string(gateID)}, nil
}
