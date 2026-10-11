package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/registry"
)

// ToolComputeDeadline computes the due date of one rule of a published rule pack. It is a
// pure computation: nothing is saved, and the result is not a deadline until a person saves
// and confirms one.
const ToolComputeDeadline = "compute_deadline"

// CapDeadlinesCompute is the capability compute_deadline needs (a2: deadlines.view).
const CapDeadlinesCompute = "deadlines.view"

// PublishedPacks lists the rule packs published for use (wired at boot). A pack still behind
// its flag (unverified law) is not published and is not listed.
type PublishedPacks interface {
	PublishedPacks(ctx context.Context) ([]deadline.Pack, error)
}

// DeadlineCompute is the compute half of kernel/deadline Engine.
type DeadlineCompute interface {
	Compute(ctx context.Context, pack, rule string, trigger deadline.Date, subject deadline.Subject) ([]deadline.Proposed, error)
}

// ComputeDeadlineHandler is compute_deadline's handler, for the boot HandlerSet.
func ComputeDeadlineHandler() Handler {
	return Handler{Name: ToolComputeDeadline, Build: buildComputeDeadline}
}

// buildComputeDeadline reads Deps: CapabilityCheck, PublishedPacks, DeadlineCompute and
// the run's *agenttools.Provenance.
func buildComputeDeadline(d Deps) (registry.Tool, error) {
	can, err := DepOf[CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolComputeDeadline, err)
	}
	packs, err := DepOf[PublishedPacks](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolComputeDeadline, err)
	}
	engine, err := DepOf[DeadlineCompute](d)
	if err != nil {
		return registry.Tool{}, fmt.Errorf("%s: %w", ToolComputeDeadline, err)
	}
	c := computeDeadline{packs: packs, engine: engine}
	t, err := handlerTool(ToolComputeDeadline, Read, can, c.handle)
	if err != nil {
		return registry.Tool{}, err
	}
	// rule: ARC-U-027
	// The dates this tool returns are values a later write in the run may carry.
	return sourcedRead(d, t)
}

type computeDeadline struct {
	packs  PublishedPacks
	engine DeadlineCompute
}

// computedOut is one computed due date. It carries rule references and dates only, no
// document or client text, so it needs no quoted envelope. Due is a YYYY-MM-DD date: an agent
// run's provenance records it, so a later write in the run may carry it.
type computedOut struct {
	PackID      string `json:"pack_id"`
	PackVersion string `json:"pack_version"`
	RuleID      string `json:"rule_id"`
	Trigger     string `json:"trigger_date"`
	Due         string `json:"due_date"`
}

type computeDeadlineOut struct {
	Computed []computedOut `json:"computed"`
	// Saved is always false: the tool computes only; a person saves and confirms.
	Saved bool `json:"saved"`
}

func (c computeDeadline) handle(ctx context.Context, _ string, raw json.RawMessage) (registry.ToolResult, error) {
	var in struct {
		RuleID      string `json:"rule_id"`
		TriggerDate string `json:"trigger_date"`
		PackVersion string `json:"rule_pack_version"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolComputeDeadline, ErrInvalidInput)
	}
	t, err := time.Parse(time.DateOnly, in.TriggerDate)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: trigger_date: %w", ToolComputeDeadline, ErrInvalidInput)
	}
	trigger := clock.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}
	pack, rule, err := c.publishedRule(ctx, in.RuleID, in.PackVersion)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolComputeDeadline, err)
	}
	// The subject is empty: the computation reads no matter, and nothing is saved.
	props, err := c.engine.Compute(ctx, pack.ID, rule, trigger, deadline.Subject{})
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: compute: %w", ToolComputeDeadline, err)
	}
	out := computeDeadlineOut{Computed: []computedOut{}}
	for _, p := range props {
		// The engine computes on the version it loaded; a result on any other version than
		// the published one is refused rather than shown as current law.
		if p.PackID != pack.ID || p.PackVersion != pack.Version {
			return registry.ToolResult{}, fmt.Errorf("%s: pack %s version: %w", ToolComputeDeadline, pack.ID, deadline.ErrUnknownPack)
		}
		out.Computed = append(out.Computed, computedOut{
			PackID: p.PackID, PackVersion: p.PackVersion, RuleID: p.RuleID,
			Trigger: p.Trigger.String(), Due: p.Due.String(),
		})
	}
	b, err := json.Marshal(out)
	if err != nil {
		return registry.ToolResult{}, fmt.Errorf("%s: %w", ToolComputeDeadline, err)
	}
	return registry.ToolResult{Output: b}, nil
}

// publishedRule finds the published pack that holds ruleID. ruleID is a bare rule id, found
// in exactly one published pack, or "<pack>/<rule>". version, when set, must be that pack's
// published version.
func (c computeDeadline) publishedRule(ctx context.Context, ruleID, version string) (deadline.Pack, string, error) {
	packID, rule, qualified := strings.Cut(ruleID, "/")
	if !qualified {
		packID, rule = "", ruleID
	}
	packs, err := c.packs.PublishedPacks(ctx)
	if err != nil {
		return deadline.Pack{}, "", fmt.Errorf("packs: %w", err)
	}
	var found []deadline.Pack
	for _, p := range packs {
		if packID != "" && p.ID != packID {
			continue
		}
		for _, r := range p.Rules {
			if r.ID == rule {
				found = append(found, p)
				break
			}
		}
	}
	switch {
	case len(found) == 0:
		return deadline.Pack{}, "", fmt.Errorf("rule: %w", deadline.ErrUnknownRule)
	case len(found) > 1:
		// rule_id names a rule of several packs: the caller must qualify it as pack/rule.
		return deadline.Pack{}, "", fmt.Errorf("rule in %d packs: %w", len(found), ErrInvalidInput)
	case version != "" && found[0].Version != version:
		return deadline.Pack{}, "", fmt.Errorf("pack %s version: %w", found[0].ID, deadline.ErrUnknownPack)
	}
	return found[0], rule, nil
}
