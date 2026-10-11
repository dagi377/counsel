package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolDraftBill assembles a pre-bill from a matter's unbilled WIP. The bill stays a draft:
// the tool never finalizes or issues it.
const ToolDraftBill = "draft_bill"

// GatePrebillReview is the Counsel gate kind a pre-bill opens (spec G-B1).
const GatePrebillReview = "billing.prebill_review"

// SubjectBill is the gate subject kind of a draft bill.
const SubjectBill = "billing.bill"

// Line actions of draft_bill's input.
const (
	lineBill      = "bill"
	lineHold      = "hold"
	lineWriteDown = "write_down"
	lineNoCharge  = "no_charge"
)

// UnbilledWIP lists the time entries a matter's draft bill would take: its approved, unbilled
// WIP up to periodTo (wired at boot).
type UnbilledWIP interface {
	UnbilledEntryIDs(ctx context.Context, matterID string, periodTo clock.Date) ([]string, error)
}

// DraftBillHandler is draft_bill's handler, for the boot HandlerSet.
func DraftBillHandler() Handler {
	return Handler{Name: ToolDraftBill, Build: buildDraftBill}
}

// buildDraftBill reads Deps: CapabilityCheck, roles.Walls, batch.Drafts, UnbilledWIP,
// draft.Store, draft.Calc, agenttools.GateOpener and *agenttools.Provenance.
func buildDraftBill(d Deps) (registry.Tool, error) {
	wrap := func(err error) (registry.Tool, error) {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftBill, err)
	}
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return wrap(err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return wrap(err)
	}
	drafts, err := DepOf[batch.Drafts](d)
	if err != nil {
		return wrap(err)
	}
	wip, err := DepOf[UnbilledWIP](d)
	if err != nil {
		return wrap(err)
	}
	store, err := DepOf[draft.Store](d)
	if err != nil {
		return wrap(err)
	}
	calc, err := DepOf[draft.Calc](d)
	if err != nil {
		return wrap(err)
	}
	open, err := DepOf[agenttools.GateOpener](d)
	if err != nil {
		return wrap(err)
	}
	pv, err := DepOf[*agenttools.Provenance](d)
	if err != nil {
		return wrap(err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolDraftBill, Kind: Write, GateKind: GatePrebillReview}})
	if err != nil {
		return wrap(err)
	}
	s := specs[0]
	t := draftBill{can: can, walls: walls, drafts: drafts, wip: wip, store: store, calc: calc,
		open: agenttools.ScopedOpener(open), capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return wrap(err)
	}
	// rule: ARC-U-027
	// A write-down amount must be a figure the run read; the model never invents money.
	tool, err = pv.SourcedWrite(tool, "/line_actions/*/write_down_cents")
	if err != nil {
		return wrap(err)
	}
	return tool, nil
}

type draftBill struct {
	can        CapabilityCheck
	walls      roles.Walls
	drafts     batch.Drafts
	wip        UnbilledWIP
	store      draft.Store
	calc       draft.Calc
	open       agenttools.GateOpener
	capability string
}

type lineActionIn struct {
	SourceID       string `json:"source_id"`
	Action         string `json:"action"`
	WriteDownCents int64  `json:"write_down_cents"`
	Narrative      string `json:"narrative"`
}

type draftBillIn struct {
	MatterID    string         `json:"matter_id"`
	PeriodFrom  string         `json:"period_from"`
	PeriodTo    string         `json:"period_to"`
	LineActions []lineActionIn `json:"line_actions"`
}

// prebillPayload is the G-B1 payload: ids and dates, no narrative.
type prebillPayload struct {
	MatterID   string `json:"matter_id"`
	PeriodFrom string `json:"period_from,omitempty"`
	PeriodTo   string `json:"period_to"`
	BillID     string `json:"bill_id"`
}

func (t draftBill) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	fail := func(err error) (registry.ToolResult, error) {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftBill, err)
	}
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return fail(fmt.Errorf("capability: %w", err))
	}
	if !ok {
		return fail(ErrForbidden)
	}
	var in draftBillIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return fail(ErrInvalidInput)
	}
	// rule: ARC-I-036
	if err := visibleTo(ctx, t.walls, user, in.MatterID); err != nil {
		return fail(err)
	}
	periodTo, err := parseCivilDate(in.PeriodTo)
	if err != nil {
		return fail(err)
	}
	if in.PeriodFrom != "" {
		from, err := parseCivilDate(in.PeriodFrom)
		if err != nil {
			return fail(err)
		}
		if periodTo.Before(from) {
			return fail(fmt.Errorf("period: %w", ErrInvalidInput))
		}
	}
	actions, err := lineActions(in.LineActions)
	if err != nil {
		return fail(err)
	}
	// rule: ARC-I-035
	// A run whose feature does not declare the pre-bill gate assembles nothing.
	if err := agenttools.CheckGateKind(ctx, p, GatePrebillReview); err != nil {
		return fail(err)
	}
	// rule: ARC-I-035
	// Every line action names one of the matter's unbilled entries; a bad source is refused
	// before the draft exists, so a refused call leaves nothing behind.
	if err := t.checkSources(ctx, in.MatterID, periodTo, actions); err != nil {
		return fail(err)
	}
	// rule: ARC-I-035
	// Create builds a draft bill only; a retried call of the same run and input returns it.
	billID, _, err := t.drafts.Create(ctx, draftBillKey(p, raw), batch.Target{MatterID: in.MatterID}, periodTo)
	if err != nil {
		return fail(fmt.Errorf("draft: %w", err))
	}
	if len(actions) > 0 {
		if err := t.applyActions(ctx, billID, actions); err != nil {
			return fail(err)
		}
	}
	body, err := json.Marshal(prebillPayload{MatterID: in.MatterID, PeriodFrom: in.PeriodFrom, PeriodTo: in.PeriodTo, BillID: billID})
	if err != nil {
		return fail(fmt.Errorf("payload: %w", err))
	}
	// rule: ARC-I-039
	// The calling principal is the preparer; an agent principal can never decide the gate.
	gateID, err := t.open(ctx, GatePrebillReview, registry.Subject{Kind: SubjectBill, ID: billID}, body, p)
	if err != nil {
		return fail(fmt.Errorf("open gate: %w", err))
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeGate, Ref: string(gateID)})
	if err != nil {
		return fail(err)
	}
	return registry.ToolResult{Output: out, GateID: string(gateID)}, nil
}

// checkSources refuses a line action whose source is not unbilled WIP the draft would take.
func (t draftBill) checkSources(ctx context.Context, matterID string, periodTo clock.Date, actions map[string]lineActionIn) error {
	if len(actions) == 0 {
		return nil
	}
	ids, err := t.wip.UnbilledEntryIDs(ctx, matterID, periodTo)
	if err != nil {
		return fmt.Errorf("unbilled wip: %w", err)
	}
	unbilled := make(map[string]bool, len(ids))
	for _, id := range ids {
		unbilled[id] = true
	}
	for id := range actions {
		if id == "" || !unbilled[id] {
			return fmt.Errorf("line action: source not unbilled: %w", ErrInvalidInput)
		}
	}
	return nil
}

// applyActions changes the draft's lines by their source time entry. Every action is checked
// before the lines are saved, so a refused action leaves the draft as assembled.
func (t draftBill) applyActions(ctx context.Context, billID string, actions map[string]lineActionIn) error {
	lines, err := t.store.Lines(ctx, billID)
	if err != nil {
		return fmt.Errorf("lines: %w", err)
	}
	kept := make([]draft.Line, 0, len(lines))
	seen := map[string]bool{}
	for _, l := range lines {
		a, ok := actions[l.SourceTimeEntryID]
		if !ok || l.SourceTimeEntryID == "" {
			kept = append(kept, l)
			continue
		}
		seen[l.SourceTimeEntryID] = true
		switch a.Action {
		case lineHold:
			// Held time stays unbilled WIP for a later bill.
			continue
		case lineWriteDown:
			l, err = t.calc.ApplyReduction(l, draft.Reduction{Cents: a.WriteDownCents, Reason: draft.ReasonOther, ReasonText: a.Narrative, ShowOnBill: true})
		case lineNoCharge:
			// The reduction replaces any earlier one, so it is the whole unreduced net.
			base := l
			base.Reduction = nil
			l, err = t.calc.ApplyReduction(l, draft.Reduction{Cents: t.calc.Net(base), Reason: draft.ReasonCourtesy, ShowOnBill: true})
		case lineBill:
			if a.Narrative != "" {
				l.Description = a.Narrative
			}
		}
		if err != nil {
			return fmt.Errorf("line action %s: %w: %w", a.Action, ErrInvalidInput, err)
		}
		kept = append(kept, l)
	}
	for id := range actions {
		if !seen[id] {
			// The source is not unbilled WIP on this draft.
			return fmt.Errorf("line action: %w", ErrInvalidInput)
		}
	}
	if err := t.store.SaveLines(ctx, billID, kept); err != nil {
		return fmt.Errorf("save lines: %w", err)
	}
	return nil
}

// lineActions indexes actions by source id; a source named twice or a write-down without an
// amount is invalid.
func lineActions(in []lineActionIn) (map[string]lineActionIn, error) {
	out := make(map[string]lineActionIn, len(in))
	for _, a := range in {
		if _, dup := out[a.SourceID]; dup {
			return nil, fmt.Errorf("line action: duplicate source: %w", ErrInvalidInput)
		}
		if (a.Action == lineWriteDown) != (a.WriteDownCents > 0) {
			return nil, fmt.Errorf("line action: write-down amount: %w", ErrInvalidInput)
		}
		out[a.SourceID] = a
	}
	return out, nil
}

// parseCivilDate parses a YYYY-MM-DD input date.
func parseCivilDate(s string) (clock.Date, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return clock.Date{}, fmt.Errorf("date: %w", ErrInvalidInput)
	}
	return clock.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()}, nil
}

// draftBillKey makes a retried call of the same run (or user) and input build one draft.
func draftBillKey(p registry.Principal, raw json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(p.RunID))
	h.Write([]byte{0})
	h.Write([]byte(actor(p)))
	h.Write([]byte{0})
	h.Write([]byte(ToolDraftBill))
	h.Write([]byte{0})
	h.Write(raw)
	return "agent:" + hex.EncodeToString(h.Sum(nil))
}
