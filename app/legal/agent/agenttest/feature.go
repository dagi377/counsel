package agenttest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// FeatureActingFor is the synthetic user a feature run acts for.
const FeatureActingFor = "user-synthetic-lawyer"

// FeatureDriver runs one feature Def over the real Registry: each of the Def's tools runs its
// real handler when the handler set has one and the Seed provides its dependencies, else a
// stand-in.
type FeatureDriver struct {
	*Driver
	Def  feature.Def
	Seed *Seed
	// Real and StandIns list the Def's tools by how they run, sorted. The trust withdrawal
	// tool counts as real: it is agent.WriteTools.
	Real     []string
	StandIns []string
}

// FeatureRun is one run of a FeatureDriver and the tools that ran as stand-ins in it.
type FeatureRun struct {
	Run
	StandIns []string // tools the model called that ran as stand-ins, in call order, once each
}

// FromFeature builds the Driver of def with the app's handler set, agent.Handlers().
func FromFeature(t testing.TB, def feature.Def) *FeatureDriver {
	t.Helper()
	return FromFeatureWith(t, def, agent.Handlers())
}

// FromFeatureWith builds the Driver of def: def's system prompt (with its feature preamble),
// tools, grant and gate kinds. A tool in hs whose dependencies the Seed provides runs its real
// handler; a tool hs lacks, or whose dependency is missing, runs a stand-in.
func FromFeatureWith(t testing.TB, def feature.Def, hs agent.HandlerSet) *FeatureDriver {
	t.Helper()
	spec, err := def.Spec()
	if err != nil {
		t.Fatalf("feature %s: %v", def.ID, err)
	}
	specs := make([]agent.ToolSpec, 0, len(def.Tools))
	for _, name := range def.Tools {
		d, err := schemas.ByName(name)
		if err != nil {
			t.Fatalf("feature %s: tool %s: %v", def.ID, name, err)
		}
		s := agent.ToolSpec{Name: d.Name, Kind: agent.ToolKind(d.Kind), Capability: d.Capability, InputSchema: d.Input}
		if s.Kind == agent.Write && len(def.GateKinds) > 0 {
			s.GateKind = def.GateKinds[0]
		}
		specs = append(specs, s)
	}
	// New registers only the real write tools; the Def's tools are added below.
	d := New(t, Feature{
		ID: def.ID, ActingFor: FeatureActingFor, System: spec.System, Kind: spec.Kind, Grant: slices.Clone(def.Grant),
	})
	fd := &FeatureDriver{Driver: d, Def: def, Seed: NewSeed(t, FeatureActingFor, d.Withdrawals)}
	// rule: ARC-U-027
	// One run, one Provenance, as in production (boot builds the read and write tools over
	// one): what the real read tools record is what the real write tools check.
	fd.Seed.Provenance = d.Provenance
	// A fee transfer to the firm reads the Seed's bills (By-Law 9 s.9(1)).
	d.FeeBills(fd.Seed.FeeBills())
	// The real prepare_trust_withdrawal pays a contact of a matter the Seed holds.
	d.contacts.seed = fd.Seed.Store
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	can := agent.CapabilityCheck(func(_ context.Context, user, capability string) (bool, error) {
		return user == FeatureActingFor && slices.Contains(def.Grant, capability), nil
	})
	deps := fd.Seed.Deps(can, d.Plans)
	writeReal := map[string]bool{}
	for _, s := range d.Write.Specs() {
		writeReal[s.Name] = true
	}
	for _, s := range specs {
		if writeReal[s.Name] {
			fd.Real = append(fd.Real, s.Name)
			continue
		}
		// rule: ARC-I-035
		// A real handler goes on the same Registry as a stand-in would, so the capability,
		// schema, plan-before-write and write-result checks apply to both.
		if h, ok := hs.For(s.Name); ok {
			tool, err := h.Build(deps)
			if err == nil && tool.Kind == registry.ToolRead {
				// rule: ARC-U-027
				// As boot does, every real read records its dates and numbers, quoted text
				// included, so a sourced write of the run can carry them.
				tool, err = d.Provenance.ReadTool(tool)
			}
			switch {
			case err == nil:
				if err := d.Registry.Register(tool); err != nil {
					t.Fatalf("register %s: %v", s.Name, err)
				}
				fd.Real = append(fd.Real, s.Name)
				continue
			case !errors.Is(err, agent.ErrMissingDep):
				t.Fatalf("build %s: %v", s.Name, err)
			}
		}
		if err := d.Registry.Register(d.standIn(s)); err != nil {
			t.Fatalf("register %s: %v", s.Name, err)
		}
		fd.StandIns = append(fd.StandIns, s.Name)
	}
	slices.Sort(fd.Real)
	slices.Sort(fd.StandIns)
	d.Feature.Tools = specs
	return fd
}

// Fire runs the feature once for prompt inside a run scope that allows the Def's gate kinds.
func (fd *FeatureDriver) Fire(ctx context.Context, model ai.ModelClient, runID, prompt string) FeatureRun {
	// rule: ARC-I-039
	// The run scope is the Def's gate kinds; every gate the run opens is checked against it.
	ctx = agenttools.WithScope(ctx, agenttools.NewScope(runID, fd.Def.GateKinds))
	// The run serves fd.Def, as boot's runner scopes it, so a feature wrap applies.
	ctx = agent.WithFeature(ctx, fd.Def.ID)
	run := fd.Driver.Fire(ctx, model, runID, prompt)
	out := FeatureRun{Run: run}
	for _, name := range run.Tools {
		if slices.Contains(fd.StandIns, name) && !slices.Contains(out.StandIns, name) {
			out.StandIns = append(out.StandIns, name)
		}
	}
	return out
}
