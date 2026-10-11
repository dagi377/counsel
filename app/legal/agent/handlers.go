package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/registry"
)

// SearchMattersHandler is search_matters' handler, for the boot HandlerSet.
func SearchMattersHandler() Handler {
	return Handler{Name: ToolSearchMatters, Build: buildMatterRead(ToolSearchMatters)}
}

// GetMatterHandler is get_matter's handler, for the boot HandlerSet.
func GetMatterHandler() Handler {
	return Handler{Name: ToolGetMatter, Build: buildMatterRead(ToolGetMatter)}
}

// ReadDocumentHandler is read_document's handler, for the boot HandlerSet.
func ReadDocumentHandler() Handler {
	return Handler{Name: ToolReadDocument, Build: buildReadDocument}
}

// PrepareTrustWithdrawalHandler is prepare_trust_withdrawal's handler, for the boot
// HandlerSet. It runs through the *WriteTools boot passes in Deps, so the run it parks is
// the one that WriteTools resumes on gate.decided.
func PrepareTrustWithdrawalHandler() Handler {
	return Handler{Name: ToolPrepareTrustWithdrawal, Build: buildPrepareTrustWithdrawal}
}

// buildMatterRead reads Deps: CapabilityCheck, roles.Walls, matter.Query.
func buildMatterRead(name string) func(d Deps) (registry.Tool, error) {
	return func(d Deps) (registry.Tool, error) {
		can, err := DepOf[CapabilityCheck](d)
		if err != nil {
			return registry.Tool{}, fmt.Errorf("%s: %w", name, err)
		}
		walls, err := DepOf[roles.Walls](d)
		if err != nil {
			return registry.Tool{}, fmt.Errorf("%s: %w", name, err)
		}
		matters, err := DepOf[matter.Query](d)
		if err != nil {
			return registry.Tool{}, fmt.Errorf("%s: %w", name, err)
		}
		return readTool(name, ReadDeps{Can: can, Walls: walls, Matters: matters})
	}
}

// buildReadDocument reads Deps: CapabilityCheck, roles.Walls, DocumentLookup, DocumentText.
func buildReadDocument(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolReadDocument, err)
	}
	walls, err := DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolReadDocument, err)
	}
	docs, err := DepOf[DocumentLookup](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolReadDocument, err)
	}
	text, err := DepOf[DocumentText](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolReadDocument, err)
	}
	return readTool(ToolReadDocument, ReadDeps{Can: can, Walls: walls, Documents: docs, Text: text})
}

// readTool serves one ReadTools tool as a registry tool. handlerTool checks the actor's
// capability; ReadTools runs the body with the acting user's walls.
func readTool(name string, deps ReadDeps) (registry.Tool, error) {
	rt, err := NewReadTools(deps)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", name, err)
	}
	return handlerTool(name, Read, deps.Can, func(ctx context.Context, user string, in json.RawMessage) (registry.ToolResult, error) {
		c := Call{ActingFor: user, Tool: name, Input: in}
		var res Result
		switch name {
		case ToolSearchMatters:
			res, err = rt.searchMatters(ctx, c)
		case ToolGetMatter:
			res, err = rt.getMatter(ctx, c)
		default:
			res, err = rt.readDocument(ctx, c)
		}
		if err != nil {
			return registry.ToolResult{}, err
		}
		return toToolResult(res), nil
	})
}

// buildPrepareTrustWithdrawal reads Deps: *WriteTools.
func buildPrepareTrustWithdrawal(d Deps) (registry.Tool, error) {
	w, err := DepOf[*WriteTools](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolPrepareTrustWithdrawal, err)
	}
	if w == nil {
		return registry.Tool{}, fmt.Errorf("%s: write tools: %w", ToolPrepareTrustWithdrawal, ErrMissingDep)
	}
	var s ToolSpec
	for _, sp := range w.Specs() {
		if sp.Name == ToolPrepareTrustWithdrawal {
			s = sp
		}
	}
	return registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema,
		Handle: func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			// rule: ARC-I-035
			// The trust module opens trust.withdrawal: refuse a run whose feature does not
			// declare it before the trust module is reached, so nothing is recorded.
			if err := agenttools.CheckGateKind(ctx, p, GateTrustWithdrawal); err != nil {
				return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolPrepareTrustWithdrawal, err)
			}
			// The acting user is the preparer; WriteTools checks that user's capability.
			res, err := w.Call(ctx, Call{RunID: p.RunID, ActingFor: actor(p), Tool: s.Name, Input: in})
			if err != nil {
				return registry.ToolResult{}, err
			}
			return toToolResult(res), nil
		},
	}, nil
}

func toToolResult(r Result) registry.ToolResult {
	out := registry.ToolResult{Output: r.Output, GateID: r.GateID}
	for _, c := range r.Citations {
		out.Citations = append(out.Citations, registry.Citation{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine})
	}
	return out
}

// RegisterHandlers builds every handler of hs from d and registers the a2 tools with tools
// through schemas.Register. Boot fails with schemas.ErrMissingHandler when an a2 tool has no
// handler in hs, and with ErrMissingDep when d lacks a port a handler reads through, so no
// tool runs unregistered or as a stand-in in the app.
func RegisterHandlers(tools agenttools.Tools, hs HandlerSet, d Deps) error {
	defs, err := schemas.Defs()
	if err != nil {
		return fmt.Errorf("register handlers: %w", err)
	}
	handlers := make(map[string]schemas.Handler, len(defs))
	for _, def := range defs {
		h, ok := hs.For(def.Name)
		if !ok {
			continue // schemas.Register reports it as ErrMissingHandler
		}
		// rule: ARC-I-035
		t, err := h.Build(d)
		if err != nil {
			return fmt.Errorf("register handlers: build %s: %w", def.Name, err)
		}
		handlers[def.Name] = t.Handle
	}
	if err := schemas.Register(tools, handlers); err != nil {
		return fmt.Errorf("register handlers: %w", err)
	}
	return nil
}

// AgentGrant is the capability grant of an agent run acting for user: the capabilities of
// the agent role that user also holds. An agent never holds a capability its acting user
// lacks, and never more than its role.
func AgentGrant(ctx context.Context, can CapabilityCheck, user string, role []string) ([]string, error) {
	grant := []string{}
	for _, c := range role {
		// rule: ARC-I-036
		ok, err := can(ctx, user, c)
		if err != nil {
			return nil, fmt.Errorf("agent grant: %w", err)
		}
		if ok && !slices.Contains(grant, c) {
			grant = append(grant, c)
		}
	}
	return grant, nil
}
