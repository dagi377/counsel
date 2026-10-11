package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolDraftEmail drafts an email in a matter. With submit it opens the outgoing-email gate;
// it never sends.
const ToolDraftEmail = "draft_email"

// GateOutgoingEmail is the Counsel gate kind a submitted email draft opens (spec G-OUT).
const GateOutgoingEmail = "platform.outgoing_email"

// SubjectEmailDraft is the gate subject kind of a submitted email draft.
const SubjectEmailDraft = "email_draft"

// ErrNotMatterContact means an email recipient is not a contact of the matter.
var ErrNotMatterContact = errors.New("agent: recipient is not a contact of the matter")

// NewDraftEmail is a draft email to store in a matter.
type NewDraftEmail struct {
	MatterID             string
	ToContactIDs         []string
	Subject              string
	BodyMarkdown         string
	AttachmentVersionIDs []string
	PreparedBy           string // the acting user; the agent never prepares in its own name
	RunID                string // empty on the manual path
	IdempotencyKey       string
}

// EmailDrafts stores draft emails (wired at boot). It has no send method: a draft leaves the
// firm only through the outgoing-email gate's effect, after a person approves it.
type EmailDrafts interface {
	CreateEmailDraft(ctx context.Context, d NewDraftEmail) (draftID string, err error)
}

// MatterContacts lists the contacts of a matter (client and parties), wired at boot.
type MatterContacts interface {
	MatterContactIDs(ctx context.Context, matterID string) ([]string, error)
}

// DraftEmailHandler is draft_email's handler, for the boot HandlerSet.
func DraftEmailHandler() Handler {
	return Handler{Name: ToolDraftEmail, Build: buildDraftEmail}
}

// buildDraftEmail reads Deps: CapabilityCheck, roles.Walls, EmailDrafts, MatterContacts and
// agenttools.GateOpener.
func buildDraftEmail(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	drafts, err := DepOf[EmailDrafts](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	contacts, err := DepOf[MatterContacts](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	open, err := DepOf[agenttools.GateOpener](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolDraftEmail, Kind: Write, GateKind: GateOutgoingEmail}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	s := specs[0]
	// rule: ARC-I-035
	// The run's scoped opener: a gate kind the run's feature does not declare is refused.
	t := draftEmail{can: can, walls: walls, drafts: drafts, contacts: contacts, open: agenttools.ScopedOpener(open), capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	return tool, nil
}

type draftEmail struct {
	can        CapabilityCheck
	walls      roles.Walls
	drafts     EmailDrafts
	contacts   MatterContacts
	open       agenttools.GateOpener
	capability string
}

type draftEmailIn struct {
	MatterID             string   `json:"matter_id"`
	ToContactIDs         []string `json:"to_contact_ids"`
	Subject              string   `json:"subject"`
	BodyMarkdown         string   `json:"body_markdown"`
	AttachmentVersionIDs []string `json:"attachment_version_ids"`
	Submit               bool     `json:"submit"`
}

func (t draftEmail) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolDraftEmail, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftEmail, ErrForbidden)
	}
	var in draftEmailIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftEmail, ErrInvalidInput)
	}
	// rule: ARC-I-036
	if err := visibleTo(ctx, t.walls, user, in.MatterID); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	known, err := t.contacts.MatterContactIDs(ctx, in.MatterID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: contacts: %w", ToolDraftEmail, err)
	}
	for i, id := range in.ToContactIDs {
		if !slices.Contains(known, id) {
			// The id stays out of the error: only its position is reported.
			return registry.ToolResult{}, fmt.Errorf("%s: recipient %d: %w", ToolDraftEmail, i, ErrNotMatterContact)
		}
	}
	// rule: ARC-I-035
	// A submitting call opens the outgoing-email gate: check its kind before storing
	// anything, so a refused run leaves no draft.
	if in.Submit {
		if err := agenttools.CheckGateKind(ctx, p, GateOutgoingEmail); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
		}
	}
	// rule: ARC-I-035
	// The tool stores a draft and at most opens the outgoing-email gate. No send path is
	// reachable from here: EmailDrafts has no send method and the gate's effect runs only
	// after a person other than the preparer approves.
	id, err := t.drafts.CreateEmailDraft(ctx, NewDraftEmail{
		MatterID: in.MatterID, ToContactIDs: in.ToContactIDs, Subject: in.Subject, BodyMarkdown: in.BodyMarkdown,
		AttachmentVersionIDs: in.AttachmentVersionIDs, PreparedBy: user, RunID: p.RunID,
		IdempotencyKey: idempotencyKey(Call{RunID: p.RunID, ActingFor: user, Tool: ToolDraftEmail, Input: raw}),
	})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	if !in.Submit {
		return emailResult(agenttools.OutcomeDraft, id, "")
	}
	// The payload is the G-OUT payload schema: the draft as the approver will see it.
	payload, err := json.Marshal(struct {
		MatterID             string   `json:"matter_id"`
		ToContactIDs         []string `json:"to_contact_ids"`
		Subject              string   `json:"subject"`
		BodyMarkdown         string   `json:"body_markdown"`
		AttachmentVersionIDs []string `json:"attachment_version_ids,omitempty"`
	}{in.MatterID, in.ToContactIDs, in.Subject, in.BodyMarkdown, in.AttachmentVersionIDs})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: payload: %w", ToolDraftEmail, err)
	}
	// rule: ARC-I-039
	// The calling principal is the preparer (an agent's acting-for user counts as such), so
	// neither the agent nor that user can decide the gate.
	gateID, err := t.open(ctx, GateOutgoingEmail, registry.Subject{Kind: SubjectEmailDraft, ID: id}, payload, p)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: open gate: %w", ToolDraftEmail, err)
	}
	return emailResult(agenttools.OutcomeGate, string(gateID), string(gateID))
}

func emailResult(outcome agenttools.WriteOutcome, ref, gateID string) (registry.ToolResult, error) {
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: outcome, Ref: ref})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftEmail, err)
	}
	return registry.ToolResult{Output: out, GateID: gateID}, nil
}
