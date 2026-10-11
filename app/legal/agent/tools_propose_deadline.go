package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolProposeDeadline stores a new deadline as proposed and opens its cal.confirm gate, or,
// given the deadline_id of a stored deadline, opens a cal.change gate that holds the proposed
// new date without applying it. Either counts only once a person other than the acting user
// approves it.
const ToolProposeDeadline = "propose_deadline"

// GateCalConfirm is the gate kind a proposed new deadline opens.
const GateCalConfirm = "cal.confirm"

// GateCalChange is the gate kind a proposed change to a stored deadline opens.
const GateCalChange = "cal.change"

// SubjectDeadline is the gate subject kind of a stored deadline.
const SubjectDeadline = "deadline"

// DeadlineSaver stores a proposed deadline (kernel/deadline Engine.Save).
type DeadlineSaver interface {
	Save(ctx context.Context, tx pgx.Tx, p deadline.Proposed) (deadline.DeadlineID, error)
}

// DeadlineGetter reads one stored deadline (kernel/deadline Engine.Get), so a proposed change
// names a deadline that exists on the matter.
type DeadlineGetter interface {
	Get(ctx context.Context, id deadline.DeadlineID) (deadline.Deadline, error)
}

// ProposeDeadlineHandler is propose_deadline's handler, for the boot HandlerSet.
func ProposeDeadlineHandler() Handler {
	return Handler{Name: ToolProposeDeadline, Build: buildProposeDeadline}
}

// buildProposeDeadline reads Deps: CapabilityCheck, roles.Walls, DeadlineSaver,
// DeadlineGetter, agenttools.GateOpener and the run's *agenttools.Provenance.
func buildProposeDeadline(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	saver, err := DepOf[DeadlineSaver](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	get, err := DepOf[DeadlineGetter](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	open, err := DepOf[agenttools.GateOpener](d)
	if err != nil || open == nil {
		return registry.Tool{}, fmt.Errorf("%s: gate opener: %w", ToolProposeDeadline, ErrMissingDep)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolProposeDeadline, Kind: Write, GateKind: GateCalConfirm}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	s := specs[0]
	p := proposeDeadline{can: can, walls: walls, saver: saver, get: get, open: agenttools.ScopedOpener(open), capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: p.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	// The due date, and the trigger date when given, must be dates the run read: a
	// compute_deadline result or a date a read tool returned from a cited document.
	tool, err = pv.SourcedWrite(tool, "/due_on", "/trigger_date")
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	return tool, nil
}

type proposeDeadline struct {
	can        CapabilityCheck
	walls      roles.Walls
	saver      DeadlineSaver
	get        DeadlineGetter
	open       agenttools.GateOpener
	capability string
}

type deadlineSource struct {
	DocID     string `json:"doc_id"`
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	QuoteHash string `json:"quote_hash"`
}

type proposeDeadlineIn struct {
	DeadlineID  string         `json:"deadline_id"`
	MatterID    string         `json:"matter_id"`
	Title       string         `json:"title"`
	DueOn       string         `json:"due_on"`
	RuleID      string         `json:"rule_id"`
	TriggerDate string         `json:"trigger_date"`
	Source      deadlineSource `json:"source"`
}

// deadlineGatePayload is what the confirming person sees on the cal.confirm gate.
type deadlineGatePayload struct {
	DeadlineID  string         `json:"deadline_id"`
	MatterID    string         `json:"matter_id"`
	Title       string         `json:"title"`
	DueOn       string         `json:"due_on"`
	RuleID      string         `json:"rule_id,omitempty"`
	TriggerDate string         `json:"trigger_date,omitempty"`
	Source      deadlineSource `json:"source"`
}

// deadlineChangePayload is what the approving person sees on the cal.change gate: the stored
// deadline and its proposed new date. The deadline keeps its date until the gate is approved.
type deadlineChangePayload struct {
	DeadlineID  string         `json:"deadline_id"`
	DueOn       string         `json:"due_on"`
	Reason      string         `json:"reason"`
	MatterID    string         `json:"matter_id"`
	CurrentDue  string         `json:"current_due_on"`
	RuleID      string         `json:"rule_id,omitempty"`
	TriggerDate string         `json:"trigger_date,omitempty"`
	Source      deadlineSource `json:"source"`
}

// civilDay parses a YYYY-MM-DD calendar date.
func civilDay(s string) (deadline.Date, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return deadline.Date{}, fmt.Errorf("date: %w", ErrInvalidInput)
	}
	return deadline.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

func (p proposeDeadline) handle(ctx context.Context, pr registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(pr)
	// rule: ARC-I-036
	ok, err := p.can(ctx, user, p.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolProposeDeadline, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, ErrForbidden)
	}
	var in proposeDeadlineIn
	if err := json.Unmarshal(raw, &in); err != nil || in.MatterID == "" || in.Title == "" {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, ErrInvalidInput)
	}
	// rule: ARC-I-036
	if err := visibleTo(ctx, p.walls, user, in.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	due, err := civilDay(in.DueOn)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: due_on: %w", ToolProposeDeadline, err)
	}
	prop := deadline.Proposed{RuleID: in.RuleID, Due: due, Subject: registry.Subject{Kind: WorkSubjectMatter, ID: in.MatterID}}
	if in.TriggerDate != "" {
		if prop.Trigger, err = civilDay(in.TriggerDate); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: trigger_date: %w", ToolProposeDeadline, err)
		}
	}
	if in.DeadlineID != "" {
		return p.change(ctx, pr, in)
	}
	// rule: ARC-I-035
	// Check the gate kind before anything is stored, so a refused run leaves no deadline.
	if err := agenttools.CheckGateKind(ctx, pr, GateCalConfirm); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	id, err := p.saver.Save(ctx, nil, prop)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: save: %w", ToolProposeDeadline, err)
	}
	payload, err := json.Marshal(deadlineGatePayload{
		DeadlineID: string(id), MatterID: in.MatterID, Title: in.Title, DueOn: in.DueOn,
		RuleID: in.RuleID, TriggerDate: in.TriggerDate, Source: in.Source,
	})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	return p.gate(ctx, pr, GateCalConfirm, id, payload)
}

// change opens cal.change on the stored deadline in.DeadlineID with the proposed new date. It
// stores nothing: the date is applied only when a person approves the gate.
func (p proposeDeadline) change(ctx context.Context, pr registry.Principal, in proposeDeadlineIn) (registry.ToolResult, error) {
	// rule: ARC-I-035
	// Check the gate kind before reading the deadline, so a refused run learns nothing.
	if err := agenttools.CheckGateKind(ctx, pr, GateCalChange); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	cur, err := p.get.Get(ctx, deadline.DeadlineID(in.DeadlineID))
	if err != nil {
		if errors.Is(err, deadline.ErrNotFound) {
			return registry.ToolResult{}, fmt.Errorf("%s: deadline: %w", ToolProposeDeadline, agenttools.ErrNotFound)
		}
		return registry.ToolResult{}, fmt.Errorf("%s: deadline: %w", ToolProposeDeadline, err)
	}
	// rule: ARC-I-036
	// The deadline must belong to the matter the walls were checked on.
	if cur.Proposed.Subject != (deadline.Subject{Kind: WorkSubjectMatter, ID: in.MatterID}) {
		return registry.ToolResult{}, fmt.Errorf("%s: deadline %s on another matter: %w", ToolProposeDeadline, in.DeadlineID, agenttools.ErrNotFound)
	}
	d := cur.Proposed.Due
	payload, err := json.Marshal(deadlineChangePayload{
		DeadlineID: in.DeadlineID, DueOn: in.DueOn, Reason: in.Title, MatterID: in.MatterID,
		CurrentDue: fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day),
		RuleID:     in.RuleID, TriggerDate: in.TriggerDate, Source: in.Source,
	})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	return p.gate(ctx, pr, GateCalChange, cur.ID, payload)
}

// gate opens kind on deadline id with payload and returns the gate as the write result.
func (p proposeDeadline) gate(ctx context.Context, pr registry.Principal, kind string, id deadline.DeadlineID, payload json.RawMessage) (registry.ToolResult, error) {
	// rule: ARC-I-039
	// The caller is the gate's preparer (for an agent, its acting-for user), so neither the
	// agent nor that user can approve the deadline.
	gid, err := p.open(ctx, kind, registry.Subject{Kind: SubjectDeadline, ID: string(id)}, payload, pr)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: open gate: %w", ToolProposeDeadline, err)
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeGate, Ref: string(gid)})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeDeadline, err)
	}
	return registry.ToolResult{Output: out, GateID: string(gid)}, nil
}
