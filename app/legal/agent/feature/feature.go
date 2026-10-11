// Package feature holds the definitions of Counsel's AI features (spec/ai/features.json) and
// the set boot builds them into. A definition names the feature's a2 tools, the gate kinds
// it may open and its prompt; Spec turns it into the kernel run.Spec the runner drives.
//
// There is no package-level registry: app/legal/boot builds one Set and each module adds its
// definitions to it explicitly, so tests build their own Set and nothing registers from init().
package feature

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// ErrBadDef: a definition is incomplete, breaks ARC-I-035, names a tool that is not an a2
// tool, or repeats an id already in the set.
var ErrBadDef = errors.New("feature: invalid definition")

// Source is what starts a feature (features.json "source").
type Source string

// Sources.
const (
	SourceJob          Source = "job"
	SourceScreenAction Source = "screen-action"
)

// Def is one AI feature.
type Def struct {
	ID           string          // spec feature id, "F-TIM-01"
	Area         string          // spec area, "time"
	Name         string          // display name
	Source       Source          // job or screen-action
	Trigger      string          // what fires it, as the spec words it
	Actions      []string        // screen actions that run it on request ("screen > Button")
	Kind         ai.CallKind     // model call kind; empty means ai.KindToolUse
	Tools        []string        // a2 tool names the feature may call
	GateKinds    []string        // gate kinds its write tools may open
	Grant        []string        // capability ids granted to the run's agent principal
	Prompt       fs.FS           // file system holding the system prompt
	PromptPath   string          // path of the system prompt in Prompt
	Fixture      string          // mock fixture dir, "testdata/ai/f-tim-01/..."
	OutputSchema json.RawMessage // extraction: JSON Schema the final output must satisfy
	// AnswerMayBeUncited lets a chat answer carry zero citations. Only a read-only feature
	// whose tools return no document source (search_matters alone) sets it; any citation the
	// answer does carry is still checked against the run's tool results.
	AnswerMayBeUncited bool
}

// Preamble is the first line of every feature's system prompt. The mock LLM routes a call by
// it on the Paploo path, so it carries the feature id and never client content.
func Preamble(id string) string {
	return "feature: " + id + "\n"
}

// Validate checks d is complete and keeps to ARC-I-035.
func (d Def) Validate() error {
	switch {
	case d.ID == "":
		return fmt.Errorf("feature: id required: %w", ErrBadDef)
	case d.Source != SourceJob && d.Source != SourceScreenAction:
		return fmt.Errorf("feature %s: source %q: %w", d.ID, d.Source, ErrBadDef)
	case d.Prompt == nil || d.PromptPath == "":
		return fmt.Errorf("feature %s: prompt required: %w", d.ID, ErrBadDef)
	}
	if _, err := d.tools(); err != nil {
		return err
	}
	return nil
}

// tools resolves d.Tools against the a2 definitions. An unknown or repeated tool name, or a
// write tool in a feature that declares no gate kind, is ErrBadDef.
func (d Def) tools() ([]schemas.Def, error) {
	out := make([]schemas.Def, 0, len(d.Tools))
	for i, name := range d.Tools {
		if slices.Contains(d.Tools[:i], name) {
			return nil, fmt.Errorf("feature %s: tool %q repeated: %w", d.ID, name, ErrBadDef)
		}
		t, err := schemas.ByName(name)
		if err != nil {
			if errors.Is(err, schemas.ErrUnknownTool) {
				return nil, fmt.Errorf("feature %s: tool %q not an a2 tool: %w", d.ID, name, ErrBadDef)
			}
			return nil, fmt.Errorf("feature %s: %w", d.ID, err)
		}
		// rule: ARC-I-035
		// A write tool only opens a gate, so a feature that can write names the gates it opens.
		if t.Kind == registry.ToolWrite && len(d.GateKinds) == 0 {
			return nil, fmt.Errorf("feature %s: write tool %q without gate kinds: %w", d.ID, name, ErrBadDef)
		}
		out = append(out, t)
	}
	return out, nil
}

// Spec builds the run.Spec of d: the system prompt is the preamble, the shared prompt rules
// (Rules) and then the prompt file, and the tools carry their a2 input schemas.
func (d Def) Spec() (run.Spec, error) {
	if err := d.Validate(); err != nil {
		return run.Spec{}, err
	}
	defs, err := d.tools()
	if err != nil {
		return run.Spec{}, err
	}
	prompt, err := fs.ReadFile(d.Prompt, d.PromptPath)
	if err != nil {
		return run.Spec{}, fmt.Errorf("feature %s: prompt: %w", d.ID, err)
	}
	tools := make([]ai.ToolSpec, 0, len(defs))
	for _, t := range defs {
		tools = append(tools, ai.ToolSpec{Name: t.Name, InputSchema: slices.Clone(t.Input)})
	}
	kind := d.Kind
	if kind == "" {
		kind = ai.KindToolUse
	}
	return run.Spec{
		FeatureID:    d.ID,
		Kind:         kind,
		System:       Preamble(d.ID) + Rules() + "\n" + strings.TrimLeft(string(prompt), "\n"),
		Tools:        tools,
		Grant:        slices.Clone(d.Grant),
		GateKinds:    slices.Clone(d.GateKinds),
		OutputSchema: slices.Clone(d.OutputSchema),
		// rule: AI-R-005
		AnswerMayBeUncited: d.AnswerMayBeUncited,
	}, nil
}

// Set holds the feature definitions boot built. The zero value is empty and ready to use.
// It is not safe for concurrent Add; boot adds every definition before serving.
type Set struct {
	defs map[string]Def
}

// NewSet returns an empty Set.
func NewSet() *Set {
	return &Set{defs: map[string]Def{}}
}

// Add puts d in the set. An invalid definition or an id already in the set is ErrBadDef.
func (s *Set) Add(d Def) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if s.defs == nil {
		s.defs = map[string]Def{}
	}
	if _, dup := s.defs[d.ID]; dup {
		return fmt.Errorf("feature %s: duplicate id: %w", d.ID, ErrBadDef)
	}
	s.defs[d.ID] = d
	return nil
}

// ByID returns the definition with id.
func (s *Set) ByID(id string) (Def, bool) {
	d, ok := s.defs[id]
	return d, ok
}

// All returns every definition, sorted by id.
func (s *Set) All() []Def {
	out := make([]Def, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
