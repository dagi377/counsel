package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolProposeConflictParties adds proposed parties to a conflict check and runs it. The run
// stays open: clearance is gate conflict.clearance (G2), decided by a lawyer.
const ToolProposeConflictParties = "propose_conflict_parties"

// ProposedParty is one party an agent proposes for a conflict check.
type ProposedParty struct {
	Name   string
	Role   string
	Kind   string               // "person", "org" or empty
	Source *agenttools.Citation // where the agent read the party, if from a document
}

// ConflictCheckParties keeps the parties of conflict checks (wired at boot).
type ConflictCheckParties interface {
	// CheckMatter returns the matter a check belongs to, or "" for a check on a lead only.
	CheckMatter(ctx context.Context, checkID string) (matterID string, err error)
	// ProposeParties adds parties to the check as proposed, prepared by preparedBy, and
	// returns the search terms of every party now on the check.
	ProposeParties(ctx context.Context, checkID string, parties []ProposedParty, preparedBy string) (terms []string, err error)
}

// ProposeConflictPartiesHandler is propose_conflict_parties' handler, for the boot
// HandlerSet.
func ProposeConflictPartiesHandler() Handler {
	return Handler{Name: ToolProposeConflictParties, Build: buildProposeConflictParties}
}

// buildProposeConflictParties reads Deps: CapabilityCheck, roles.Walls, ConflictCheckParties
// conflict.Checker and RunScope.
func buildProposeConflictParties(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	parties, err := DepOf[ConflictCheckParties](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	checker, err := DepOf[conflict.Checker](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	scope, err := DepOf[RunScope](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	specs, _, err := a2Specs([]ToolSpec{{Name: ToolProposeConflictParties, Kind: Write, GateKind: conflict.GateClearance}})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	s := specs[0]
	t := proposeConflictParties{can: can, walls: walls, parties: parties, checker: checker, scope: scope, capability: s.Capability}
	// rule: ARC-I-035
	// WriteTool holds every result to a draft, a proposal or an opened gate.
	tool, err := agenttools.WriteTool(registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema, Handle: t.handle,
	})
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	return tool, nil
}

type proposeConflictParties struct {
	can        CapabilityCheck
	walls      roles.Walls
	parties    ConflictCheckParties
	checker    conflict.Checker
	scope      RunScope
	capability string
}

// partySource is the a2 citation shape of where a party was read.
type partySource struct {
	VersionID string `json:"version_id"`
	Page      int    `json:"page"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
}

type conflictPartyIn struct {
	Name   string       `json:"name"`
	Role   string       `json:"role"`
	Kind   string       `json:"kind"`
	Source *partySource `json:"source"`
}

type proposeConflictPartiesIn struct {
	CheckID string            `json:"check_id"`
	Parties []conflictPartyIn `json:"parties"`
}

func (t proposeConflictParties) handle(ctx context.Context, p registry.Principal, raw json.RawMessage) (registry.ToolResult, error) {
	user := actor(p)
	// rule: ARC-I-036
	ok, err := t.can(ctx, user, t.capability)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: capability: %w", ToolProposeConflictParties, err)
	}
	if !ok {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, ErrForbidden)
	}
	var in proposeConflictPartiesIn
	if err := json.Unmarshal(raw, &in); err != nil || len(in.Parties) == 0 {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, ErrInvalidInput)
	}
	matterID, err := t.parties.CheckMatter(ctx, in.CheckID)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: check: %w", ToolProposeConflictParties, err)
	}
	if matterID != "" {
		// rule: ARC-I-036
		if err := visibleTo(ctx, t.walls, user, matterID); err != nil {
			return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
		}
	}
	proposed := make([]ProposedParty, 0, len(in.Parties))
	var cites []agenttools.Citation
	for i, pin := range in.Parties {
		pp := ProposedParty{Name: pin.Name, Role: pin.Role, Kind: pin.Kind}
		if pin.Source != nil {
			src := agenttools.Citation{VersionID: pin.Source.VersionID, Page: pin.Source.Page, FromLine: pin.Source.LineStart, ToLine: pin.Source.LineEnd}
			// rule: ARC-I-038
			if err := agenttools.ValidCitation(src); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: party %d: %w", ToolProposeConflictParties, i, err)
			}
			// rule: ARC-U-027
			// An agent cites only a passage a tool result of its run returned; an invented
			// source never reaches the check. The manual path cites what the user saw.
			if p.Kind == registry.PrincipalAgent {
				read, err := t.scope.Returned(ctx, p.RunID, src)
				if err != nil {
					return registry.ToolResult{}, fmt.Errorf("%s: party %d: %w", ToolProposeConflictParties, i, err)
				}
				if !read {
					return registry.ToolResult{}, fmt.Errorf("%s: party %d source: %w", ToolProposeConflictParties, i, agenttools.ErrUnsourcedValue)
				}
			}
			pp.Source = &src
			cites = append(cites, src)
		}
		proposed = append(proposed, pp)
	}
	// rule: ARC-I-035
	// The check opens conflict.clearance: refuse a run whose feature does not declare it
	// before any party is recorded.
	if err := agenttools.CheckGateKind(ctx, p, conflict.GateClearance); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	// rule: ARC-I-035
	// The parties are recorded as proposed and the check runs; the run stays open until a
	// lawyer decides conflict.clearance (G2). Nothing here clears it.
	terms, err := t.parties.ProposeParties(ctx, in.CheckID, proposed, user)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	// rule: ARC-I-039
	// The preparer is the acting user, through the agent, so neither can decide the run.
	run, err := t.checker.Check(ctx, conflict.Request{
		MatterID: matterID, Terms: terms,
		Preparer: conflict.Actor{UserID: user, IsAgent: p.Kind == registry.PrincipalAgent, ActingFor: p.ActingFor},
	})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: run check: %w", ToolProposeConflictParties, err)
	}
	if run.Status != conflict.StatusOpen {
		return registry.ToolResult{}, fmt.Errorf("%s: run %s is %s, want open: %w", ToolProposeConflictParties, run.ID, run.Status, agenttools.ErrBadTool)
	}
	out, err := json.Marshal(agenttools.WriteOutput{Outcome: agenttools.OutcomeProposal, Ref: run.ID})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolProposeConflictParties, err)
	}
	return registry.ToolResult{Output: out, Citations: cites}, nil
}
