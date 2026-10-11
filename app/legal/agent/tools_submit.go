package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolSubmitForApproval opens the gate for a draft the agent made in this run. The gate is
// decided by a person; the agent and its acting user, as preparer, cannot decide it.
const ToolSubmitForApproval = agenttools.ToolSubmitForApproval

// SubjectMatters resolves the matter a submitted subject belongs to (wired at boot), so the
// acting user's walls apply to the submission.
type SubjectMatters interface {
	// SubjectMatter returns the matter of subjectID of kind subjectType.
	SubjectMatter(ctx context.Context, subjectType, subjectID string) (matterID string, err error)
}

// SubmitForApprovalHandler is submit_for_approval's handler, for the boot HandlerSet.
func SubmitForApprovalHandler() Handler {
	return Handler{Name: ToolSubmitForApproval, Build: buildSubmitForApproval}
}

// buildSubmitForApproval reads Deps: CapabilityCheck, roles.Walls, SubjectMatters and
// agenttools.GateOpener.
func buildSubmitForApproval(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	subjects, err := DepOf[SubjectMatters](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	open, err := DepOf[agenttools.GateOpener](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	// The kernel tool owns the run-scope checks, the gate payload and the gate outcome; this
	// handler adds the a2 schema, the acting user's capability and walls.
	// rule: ARC-I-035
	kernel, err := agenttools.SubmitForApprovalTool(agenttools.ScopedOpener(open))
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolSubmitForApproval, Kind: Write}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	s := specs[0]
	t := submitForApproval{can: can, walls: walls, subjects: subjects, next: kernel.Handle, capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	return tool, nil
}

type submitForApproval struct {
	can        CapabilityCheck
	walls      roles.Walls
	subjects   SubjectMatters
	next       func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error)
	capability string
}

type submitForApprovalIn struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	GateKind    string `json:"gate_kind"`
}

func (t submitForApproval) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolSubmitForApproval, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, ErrForbidden)
	}
	var in submitForApprovalIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, ErrInvalidInput)
	}
	// rule: ARC-I-035
	// The run scope is checked before any module lookup: a kind outside the feature's gate
	// kinds is 403 and a subject the run did not draft is 404, whatever it names.
	if err := agenttools.CheckGateKind(ctx, p, in.GateKind); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	if p.Kind == registry.PrincipalAgent {
		if s, ok := agenttools.ScopeOf(ctx); !ok || !s.HasDraft(in.SubjectID) {
			return registry.ToolResult{}, fmt.Errorf("%s: draft %s: %w", ToolSubmitForApproval, in.SubjectID, agenttools.ErrNotFound)
		}
	}
	matterID, err := t.subjects.SubjectMatter(ctx, in.SubjectType, in.SubjectID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: subject: %w", ToolSubmitForApproval, err)
	}
	if matterID != "" {
		// rule: ARC-I-036
		if err := visibleTo(ctx, t.walls, user, matterID); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
		}
	}
	// rule: ARC-I-039
	// The gate's preparer is p: for an agent run the gate engine counts the acting user as
	// preparer, so neither the agent nor that user can decide the gate.
	res, err := t.next(ctx, p, raw)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolSubmitForApproval, err)
	}
	return res, nil
}
