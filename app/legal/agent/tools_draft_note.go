package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolDraftNote drafts a file note on a matter and opens its approval gate. The note joins
// the file only when a person approves the gate.
const ToolDraftNote = "draft_note"

// GateAgentNote is the Counsel gate kind a drafted file note opens (spec G-NT).
const GateAgentNote = "matter.agent_note"

// SubjectAgentNote is the gate subject kind of a drafted file note.
const SubjectAgentNote = "agent_note"

// NewDraftNote is a draft file note to store on a matter.
type NewDraftNote struct {
	MatterID       string
	BodyMarkdown   string
	Citations      []agenttools.Citation
	PreparedBy     string // the acting user; the agent never prepares in its own name
	RunID          string // empty on the manual path
	IdempotencyKey string
}

// NoteDrafts stores draft file notes (wired at boot). A stored draft is not on the file until
// its matter.agent_note gate is approved.
type NoteDrafts interface {
	CreateNoteDraft(ctx context.Context, n NewDraftNote) (draftID string, err error)
}

// DraftNoteHandler is draft_note's handler, for the boot HandlerSet.
func DraftNoteHandler() Handler {
	return Handler{Name: ToolDraftNote, Build: buildDraftNote}
}

// buildDraftNote reads Deps: CapabilityCheck, roles.Walls, NoteDrafts and
// agenttools.GateOpener.
func buildDraftNote(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	notes, err := DepOf[NoteDrafts](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	open, err := DepOf[agenttools.GateOpener](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolDraftNote, Kind: Write, GateKind: GateAgentNote}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	s := specs[0]
	t := draftNote{can: can, walls: walls, notes: notes, open: open, capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	return tool, nil
}

type draftNote struct {
	can        CapabilityCheck
	walls      roles.Walls
	notes      NoteDrafts
	open       agenttools.GateOpener
	capability string
}

// noteCitation is the a2 citation shape.
type noteCitation struct {
	DocID     string `json:"doc_id"`
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	QuoteHash string `json:"quote_hash"`
}

type draftNoteIn struct {
	MatterID     string         `json:"matter_id"`
	BodyMarkdown string         `json:"body_markdown"`
	Citations    []noteCitation `json:"citations,omitempty"`
}

func (t draftNote) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolDraftNote, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftNote, ErrForbidden)
	}
	var in draftNoteIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftNote, ErrInvalidInput)
	}
	// rule: ARC-I-036
	if err := visibleTo(ctx, t.walls, user, in.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	cites := make([]agenttools.Citation, 0, len(in.Citations))
	for i, c := range in.Citations {
		src := agenttools.Citation{VersionID: c.VersionID, Page: c.Page, FromLine: c.LineStart, ToLine: c.LineEnd}
		// rule: ARC-I-038
		if err := agenttools.ValidCitation(src); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: citation %d: %w", ToolDraftNote, i, err)
		}
		cites = append(cites, src)
	}
	// rule: ARC-I-035
	// The tool stores a draft note and opens its gate; the note is not on the file until a
	// person approves.
	id, err := t.notes.CreateNoteDraft(ctx, NewDraftNote{
		MatterID: in.MatterID, BodyMarkdown: in.BodyMarkdown, Citations: cites, PreparedBy: user, RunID: p.RunID,
		IdempotencyKey: idempotencyKey(Call{RunID: p.RunID, ActingFor: user, Tool: ToolDraftNote, Input: raw}),
	})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	// The payload is the G-NT payload schema; citations are left out when there are none.
	payload, err := json.Marshal(in)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: payload: %w", ToolDraftNote, err)
	}
	// rule: ARC-I-039
	// The calling principal is the preparer: for an agent run its acting-for user, so neither
	// the agent nor that user can decide the gate.
	gateID, err := t.open(ctx, GateAgentNote, registry.Subject{Kind: SubjectAgentNote, ID: id}, payload, p)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: open gate: %w", ToolDraftNote, err)
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeGate, Ref: string(gateID)})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftNote, err)
	}
	return registry.ToolResult{Output: out, GateID: string(gateID), Citations: cites}, nil
}
