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

// ToolDraftDocument drafts a document in a matter. The draft is only stored; filing, sending
// or releasing it is a person's later step.
const ToolDraftDocument = "draft_document"

// ErrUncitedSource means a draft cites a source no tool result of its run returned.
var ErrUncitedSource = errors.New("agent: draft cites a source not returned by this run")

// SubjectFirm is the subject_ref of a firm-level draft: wording that belongs to the firm, not
// to a matter, such as an intake form's public questions (F-INT-12). A firm draft names no
// matter; it never borrows one.
const SubjectFirm = "firm"

// CapIntakeForms is the capability to build the firm's intake forms. draft_document stores a
// firm-level draft only when the acting user holds it and, for an agent run, the run's grant
// names it (Q-FORM-SUBJECT).
const CapIntakeForms = "intake.forms"

// DraftCitation is one citation in a draft body: the anchor in the body and the source it
// points at.
type DraftCitation struct {
	Anchor    string
	DocID     string
	QuoteHash string
	Source    agenttools.Citation
}

// NewDraftDocument is a draft document to store in a matter, or on the firm when MatterID is
// empty and SubjectRef is SubjectFirm.
type NewDraftDocument struct {
	MatterID   string
	Title      string
	TemplateID string
	// SubjectRef links the draft to the record it is about (F-CON-03: the conflict run ref);
	// optional (Q-EVENTS-WIRE-2).
	SubjectRef     string
	BodyMarkdown   string
	Citations      []DraftCitation
	PreparedBy     string // the acting user; the agent never prepares in its own name
	RunID          string // empty on the manual path
	IdempotencyKey string
}

// DocumentDrafts stores draft documents (app/legal/docs, wired at boot). A draft is never
// filed, sent or released by storing it.
type DocumentDrafts interface {
	CreateDraft(ctx context.Context, d NewDraftDocument) (draftID string, err error)
}

// RunScope is the run's scope as the draft tools see it: the kernel agenttools.Scope the
// Runner puts in ctx, seen by run id. agenttools.RunScopes is the implementation boot wires;
// tests use a fake. It reports the citations the run's tool results returned and records the
// drafts it made.
type RunScope interface {
	// Returned reports whether a tool result of the run returned a citation covering c.
	Returned(ctx context.Context, runID string, c agenttools.Citation) (bool, error)
	// RecordDraft records a draft made in the run, so the run may later submit it.
	RecordDraft(ctx context.Context, runID, ref string) error
}

var _ RunScope = agenttools.RunScopes{}

// DraftDocumentHandler is draft_document's handler, for the boot HandlerSet.
func DraftDocumentHandler() Handler {
	return Handler{Name: ToolDraftDocument, Build: buildDraftDocument}
}

// buildDraftDocument reads Deps: CapabilityCheck, roles.Walls, DocumentDrafts, RunScope.
func buildDraftDocument(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	drafts, err := DepOf[DocumentDrafts](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	scope, err := DepOf[RunScope](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolDraftDocument, Kind: Write}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	s := specs[0]
	t := draftDocument{can: can, walls: walls, drafts: drafts, scope: scope, capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	return tool, nil
}

type draftDocument struct {
	can        CapabilityCheck
	walls      roles.Walls
	drafts     DocumentDrafts
	scope      RunScope
	capability string
}

type draftDocumentIn struct {
	MatterID     string `json:"matter_id"`
	Title        string `json:"title"`
	TemplateID   string `json:"template_id"`
	SubjectRef   string `json:"subject_ref"`
	BodyMarkdown string `json:"body_markdown"`
	Citations    []struct {
		Anchor   string `json:"anchor"`
		Citation struct {
			DocID     string `json:"doc_id"`
			VersionID string `json:"version_id"`
			Page      int    `json:"page"`
			LineStart int    `json:"line_start"`
			LineEnd   int    `json:"line_end"`
			QuoteHash string `json:"quote_hash"`
		} `json:"citation"`
	} `json:"citations"`
}

func (t draftDocument) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolDraftDocument, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftDocument, ErrForbidden)
	}
	var in draftDocumentIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftDocument, ErrInvalidInput)
	}
	if err := t.subject(ctx, p, user, in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	agentRun := p.Kind == registry.PrincipalAgent
	cites := make([]DraftCitation, 0, len(in.Citations))
	sources := make([]agenttools.Citation, 0, len(in.Citations))
	for i, c := range in.Citations {
		src := agenttools.Citation{VersionID: c.Citation.VersionID, Page: c.Citation.Page, FromLine: c.Citation.LineStart, ToLine: c.Citation.LineEnd}
		// rule: ARC-I-038
		if err := agenttools.ValidCitation(src); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: citation %d: %w", ToolDraftDocument, i, err)
		}
		// rule: ARC-I-038
		// An agent may cite only what a tool result of this run returned.
		if agentRun {
			ok, err := t.scope.Returned(ctx, p.RunID, src)
			if err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: scope: %w", ToolDraftDocument, err)
			}
			if !ok {
				return registry.ToolResult{}, fmt.Errorf("%s: citation %d: %w", ToolDraftDocument, i, ErrUncitedSource)
			}
		}
		cites = append(cites, DraftCitation{Anchor: c.Anchor, DocID: c.Citation.DocID, QuoteHash: c.Citation.QuoteHash, Source: src})
		sources = append(sources, src)
	}
	// rule: ARC-I-035
	// The tool only stores a draft: nothing is filed, sent or released.
	id, err := t.drafts.CreateDraft(ctx, NewDraftDocument{
		MatterID: in.MatterID, Title: in.Title, TemplateID: in.TemplateID, SubjectRef: in.SubjectRef, BodyMarkdown: in.BodyMarkdown,
		Citations: cites, PreparedBy: user, RunID: p.RunID,
		IdempotencyKey: idempotencyKey(Call{RunID: p.RunID, ActingFor: user, Tool: ToolDraftDocument, Input: raw}),
	})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	if agentRun {
		if err := t.scope.RecordDraft(ctx, p.RunID, id); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: scope: %w", ToolDraftDocument, err)
		}
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeDraft, Ref: id})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolDraftDocument, err)
	}
	return registry.ToolResult{Output: out, Citations: sources}, nil
}

// subject checks what the draft is on. A matter draft needs the matter visible to user. A
// firm draft (no matter, subject_ref SubjectFirm) needs CapIntakeForms for user and, for an
// agent run, in the run's grant; any other draft without a matter is invalid input.
func (t draftDocument) subject(ctx context.Context, p registry.Principal, user string, in draftDocumentIn) error {
	if in.MatterID != "" {
		if in.SubjectRef == SubjectFirm {
			return ErrInvalidInput
		}
		// rule: ARC-I-036
		return visibleTo(ctx, t.walls, user, in.MatterID)
	}
	if in.SubjectRef != SubjectFirm {
		return ErrInvalidInput
	}
	// rule: ARC-I-036
	// A firm draft borrows no matter: the run's grant and the acting user must both allow it.
	if p.Kind == registry.PrincipalAgent && !slices.Contains(p.Grant, CapIntakeForms) {
		return ErrForbidden
	}
	ok, err := t.can(ctx, user, CapIntakeForms)
	if err != nil {
		return fmt.Errorf("capability: %w", err)
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}
