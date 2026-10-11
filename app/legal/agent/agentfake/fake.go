// Package agentfake is an in-memory app/legal/agent for tests: no network, no Postgres,
// no model. Read tools return canned results; write tools open a fake gate and never
// apply an effect.
package agentfake

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/dagi377/counsel/app/legal/agent"
)

var _ agent.Service = (*Fake)(nil)

// Proposal is a gate a write tool opened. It stays proposed until decided.
type Proposal struct {
	GateID    string
	GateKind  string
	RunID     string
	Preparer  string
	Input     json.RawMessage
	Decision  agent.Decision // empty while proposed
	DecidedBy string
}

// Fake holds tool specs, canned read results, grants and walls in memory.
type Fake struct {
	mu        sync.Mutex
	specs     map[string]agent.ToolSpec
	canned    map[string]agent.Result
	grants    map[[2]string]bool // (user, capability)
	hidden    map[string]bool    // subject ids hidden by walls
	proposals map[string]*Proposal
	next      int
}

// New returns a fake with the given tools.
func New(specs ...agent.ToolSpec) *Fake {
	f := &Fake{
		specs:     map[string]agent.ToolSpec{},
		canned:    map[string]agent.Result{},
		grants:    map[[2]string]bool{},
		hidden:    map[string]bool{},
		proposals: map[string]*Proposal{},
	}
	for _, s := range specs {
		f.specs[s.Name] = s
	}
	return f
}

// Grant gives user a capability.
func (f *Fake) Grant(user, capability string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants[[2]string{user, capability}] = true
}

// Hide walls off a subject: any call whose input names it as "id" fails ErrNotVisible.
func (f *Fake) Hide(subjectID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hidden[subjectID] = true
}

// SetResult sets the canned result of a read or report tool.
func (f *Fake) SetResult(tool string, r agent.Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.canned[tool] = r
}

// Proposal returns a copy of the proposal behind a gate.
func (f *Fake) Proposal(gateID string) (Proposal, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.proposals[gateID]
	if !ok {
		return Proposal{}, false
	}
	return *p, true
}

// Specs implements agent.Tools, ordered by name.
func (f *Fake) Specs() []agent.ToolSpec {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]agent.ToolSpec, 0, len(f.specs))
	for _, s := range f.specs {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Call implements agent.Tools. Input must be a JSON object; an "id" field names the
// subject for wall checks.
func (f *Fake) Call(ctx context.Context, c agent.Call) (agent.Result, error) {
	if err := ctx.Err(); err != nil {
		return agent.Result{}, fmt.Errorf("call: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	spec, ok := f.specs[c.Tool]
	if !ok {
		return agent.Result{}, fmt.Errorf("call %s: %w", c.Tool, agent.ErrUnknownTool)
	}
	var in struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(c.Input, &in); err != nil {
		return agent.Result{}, fmt.Errorf("call %s: %w", c.Tool, agent.ErrInvalidInput)
	}
	if !f.grants[[2]string{c.ActingFor, spec.Capability}] {
		return agent.Result{}, fmt.Errorf("call %s: %w", c.Tool, agent.ErrForbidden)
	}
	if in.ID != "" && f.hidden[in.ID] {
		return agent.Result{}, fmt.Errorf("call %s: %w", c.Tool, agent.ErrNotVisible)
	}
	if spec.Kind != agent.Write {
		return f.canned[c.Tool], nil
	}
	f.next++
	gateID := "gate-" + strconv.Itoa(f.next)
	f.proposals[gateID] = &Proposal{
		GateID:   gateID,
		GateKind: spec.GateKind,
		RunID:    c.RunID,
		Preparer: c.ActingFor,
		Input:    append(json.RawMessage(nil), c.Input...),
	}
	out, _ := json.Marshal(map[string]string{"status": "proposed", "gate_id": gateID})
	return agent.Result{Output: out, GateID: gateID}, nil
}

// Decided implements agent.Runs. The acting-for user never decides the run's own gate.
func (f *Fake) Decided(ctx context.Context, ev agent.GateDecided) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("decided: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.proposals[ev.GateID]
	if !ok || p.Decision != "" {
		return "", fmt.Errorf("decided %s: %w", ev.GateID, agent.ErrUnknownGate)
	}
	if ev.By == p.Preparer {
		return "", fmt.Errorf("decided %s: %w", ev.GateID, agent.ErrSelfApproval)
	}
	p.Decision, p.DecidedBy = ev.Decision, ev.By
	return p.RunID, nil
}
