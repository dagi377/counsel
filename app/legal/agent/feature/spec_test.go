package feature

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// strictSync turns unregistered spec features from a reported count into a failure.
// FEAT-CORE-16 flips it once all 194 features are registered.
const strictSync = false

const (
	featuresPath = "../../../../spec/ai/features.json"
	gatesPath    = "../../../../spec/gates.json"
	toolSubmit   = "submit_for_approval"
	toolDraft    = "draft_document"
)

type specFeature struct {
	ID        string   `json:"id"`
	Source    string   `json:"source"`
	Tools     []string `json:"tools"`
	ToolGap   []string `json:"tool_gap"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

type specFile struct {
	ToolGaps struct {
		Kinds []string `json:"kinds"`
	} `json:"tool_gaps"`
	Features []specFeature `json:"features"`
}

type aiSpec struct {
	features map[string]specFeature
	order    []string
	gates    map[string]bool // spec/gates.json slugs
	gaps     map[string]bool // tool_gaps.kinds
}

func loadSpec(t *testing.T) aiSpec {
	t.Helper()
	var f specFile
	readJSON(t, featuresPath, &f)
	var gates []struct {
		Slug string `json:"slug"`
	}
	readJSON(t, gatesPath, &gates)
	s := aiSpec{features: map[string]specFeature{}, gates: map[string]bool{}, gaps: map[string]bool{}}
	for _, sf := range f.Features {
		s.features[sf.ID] = sf
		s.order = append(s.order, sf.ID)
	}
	for _, g := range gates {
		s.gates[g.Slug] = true
	}
	for _, k := range f.ToolGaps.Kinds {
		s.gaps[k] = true
	}
	if len(s.order) == 0 || len(s.gates) == 0 || len(s.gaps) == 0 {
		t.Fatalf("spec files are empty: %d features, %d gates, %d tool_gap kinds", len(s.order), len(s.gates), len(s.gaps))
	}
	return s
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

// specKind is the run kind a features.json model_call drives. "embedding+extraction" ranks
// with embeddings through the run rank helper and ends in one extraction call, so its Def
// kind is extraction.
func specKind(modelCall string) (ai.CallKind, bool) {
	switch modelCall {
	case "chat":
		return ai.KindChat, true
	case "tool-use":
		return ai.KindToolUse, true
	case "extraction", "embedding+extraction":
		return ai.KindExtraction, true
	case "embedding":
		return ai.KindEmbedding, true
	}
	return "", false
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// writeTools returns the a2 write tools among names; unknown names are skipped (Add already
// refuses them).
func writeTools(names []string) []string {
	var out []string
	for _, n := range names {
		if d, err := schemas.ByName(n); err == nil && d.Kind == registry.ToolWrite {
			out = append(out, n)
		}
	}
	return out
}

// gapProblems checks how a feature reaches tool_gap gate kinds. A gap kind has no
// dedicated a2 write tool, so the only way to open it is to draft the subject (draft_document,
// or the feature's own drafting write tool) and then call submit_for_approval.
func gapProblems(id string, tools, kinds []string, gaps map[string]bool) []string {
	var gapKinds []string
	for _, k := range kinds {
		if gaps[k] {
			gapKinds = append(gapKinds, k)
		}
	}
	if len(gapKinds) == 0 {
		return nil
	}
	var out []string
	// rule: AI-R-003
	if !slices.Contains(tools, toolSubmit) {
		out = append(out, fmt.Sprintf("%s: tool_gap kinds %v without %s", id, gapKinds, toolSubmit))
	}
	drafts := slices.DeleteFunc(writeTools(tools), func(n string) bool { return n == toolSubmit })
	if len(drafts) == 0 {
		out = append(out, fmt.Sprintf("%s: tool_gap kinds %v without %s or another drafting write tool", id, gapKinds, toolDraft))
	}
	return out
}

// syncProblems compares every Def in s with its features.json entry and returns one line per
// mismatch, plus the number of spec features with no Def.
func syncProblems(s *Set, spec aiSpec) (problems []string, unregistered int) {
	for _, d := range s.All() {
		sf, ok := spec.features[d.ID]
		if !ok {
			problems = append(problems, d.ID+": not in spec/ai/features.json")
			continue
		}
		// rule: AI-R-002
		if !sameSet(d.Tools, sf.Tools) {
			problems = append(problems, fmt.Sprintf("%s: tools %v, spec %v", d.ID, d.Tools, sf.Tools))
		}
		if !sameSet(d.GateKinds, sf.Gate.Kind) {
			problems = append(problems, fmt.Sprintf("%s: gate kinds %v, spec %v", d.ID, d.GateKinds, sf.Gate.Kind))
		}
		kind := d.Kind
		if kind == "" {
			kind = ai.KindToolUse
		}
		if want, ok := specKind(sf.ModelCall); !ok || kind != want {
			problems = append(problems, fmt.Sprintf("%s: kind %q, spec model_call %q", d.ID, kind, sf.ModelCall))
		}
		if string(d.Source) != sf.Source {
			problems = append(problems, fmt.Sprintf("%s: source %q, spec %q", d.ID, d.Source, sf.Source))
		}
		// rule: AI-R-002
		if d.Fixture != sf.Fixture {
			problems = append(problems, fmt.Sprintf("%s: fixture %q, spec %q", d.ID, d.Fixture, sf.Fixture))
		}
		for _, k := range d.GateKinds {
			// rule: AI-R-003
			if !spec.gates[k] {
				problems = append(problems, fmt.Sprintf("%s: gate kind %q not in spec/gates.json", d.ID, k))
			}
		}
		problems = append(problems, gapProblems(d.ID, d.Tools, d.GateKinds, spec.gaps)...)
	}
	for _, id := range spec.order {
		if _, ok := s.ByID(id); !ok {
			unregistered++
		}
	}
	return problems, unregistered
}

// registeredSet builds the feature Set the way app/legal/boot does. Modules add their Defs
// explicitly (no init()); none is wired yet, so the set starts empty and the count below
// reports every spec feature as unregistered.
func registeredSet(t *testing.T) *Set {
	t.Helper()
	return NewSet()
}

// rule: AI-R-002
func TestAI_R_002_RegisteredDefsMatchSpec(t *testing.T) {
	spec := loadSpec(t)
	problems, unregistered := syncProblems(registeredSet(t), spec)
	for _, p := range problems {
		t.Error(p)
	}
	if unregistered > 0 {
		if strictSync {
			t.Errorf("%d of %d spec features have no registered Def", unregistered, len(spec.order))
		} else {
			t.Logf("%d of %d spec features have no registered Def (strict from FEAT-CORE-16)", unregistered, len(spec.order))
		}
	}
}

// rule: AI-R-003
func TestAI_R_003_SpecGateKindsAndToolGaps(t *testing.T) {
	spec := loadSpec(t)
	for k := range spec.gaps {
		if !spec.gates[k] {
			t.Errorf("tool_gaps kind %q not in spec/gates.json", k)
		}
	}
	for _, id := range spec.order {
		sf := spec.features[id]
		for _, k := range sf.Gate.Kind {
			if !spec.gates[k] {
				t.Errorf("%s: gate kind %q not in spec/gates.json", id, k)
			}
		}
		for _, k := range sf.ToolGap {
			if !spec.gaps[k] || !slices.Contains(sf.Gate.Kind, k) {
				t.Errorf("%s: tool_gap %q is not a tool_gaps kind opened by the feature", id, k)
			}
		}
		for _, n := range sf.Tools {
			if _, err := schemas.ByName(n); err != nil {
				t.Errorf("%s: tool %q: %v", id, n, err)
			}
		}
		if _, ok := specKind(sf.ModelCall); !ok {
			t.Errorf("%s: model_call %q has no run kind", id, sf.ModelCall)
		}
		for _, p := range gapProblems(id, sf.Tools, sf.Gate.Kind, spec.gaps) {
			t.Error(p)
		}
	}
}

// defFromSpec builds the Def a module would add for the spec entry id.
func defFromSpec(t *testing.T, spec aiSpec, id string) Def {
	t.Helper()
	sf, ok := spec.features[id]
	if !ok {
		t.Fatalf("%s not in spec", id)
	}
	kind, _ := specKind(sf.ModelCall)
	return Def{
		ID: sf.ID, Area: "test", Name: sf.ID, Source: Source(sf.Source), Kind: kind,
		Tools: slices.Clone(sf.Tools), GateKinds: slices.Clone(sf.Gate.Kind),
		Prompt: prompts, PromptPath: "prompts/f-tim-01.md", Fixture: sf.Fixture,
	}
}

// rule: AI-R-002
func TestAI_R_002_SpecSyncDetectsDrift(t *testing.T) {
	spec := loadSpec(t)
	tests := []struct {
		name string
		id   string
		edit func(*Def)
		want string // substring of the one expected problem; "" means in sync
	}{
		{"read-only feature in sync", "F-ACC-01", func(*Def) {}, ""},
		{"tool_gap feature in sync", "F-ACC-02", func(*Def) {}, ""},
		{"tool order is not drift", "F-ACC-02", func(d *Def) { slices.Reverse(d.Tools) }, ""},
		{"tool-use as empty kind", "F-ACC-02", func(d *Def) { d.Kind = "" }, ""},
		{"extra tool", "F-ACC-01", func(d *Def) { d.Tools = append(d.Tools, "get_matter") }, "tools"},
		{"missing tool", "F-ACC-02", func(d *Def) { d.Tools = slices.DeleteFunc(d.Tools, func(n string) bool { return n == "report_step" }) }, "tools"},
		{"other gate kind", "F-ACC-02", func(d *Def) { d.GateKinds = []string{"core.review_close"} }, "gate kinds"},
		{"wrong model call", "F-ACC-01", func(d *Def) { d.Kind = ai.KindExtraction }, "model_call"},
		{"wrong source", "F-ACC-01", func(d *Def) { d.Source = SourceScreenAction }, "source"},
		{"wrong fixture dir", "F-ACC-01", func(d *Def) { d.Fixture = "testdata/ai/f-acc-01/other" }, "fixture"},
		{"not in spec", "F-ACC-01", func(d *Def) { d.ID = "F-ACC-99" }, "not in spec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := defFromSpec(t, spec, tt.id)
			tt.edit(&d)
			s := NewSet()
			if err := s.Add(d); err != nil {
				t.Fatalf("Add: %v", err)
			}
			problems, unregistered := syncProblems(s, spec)
			if want := len(spec.order) - 1; tt.want != "not in spec" && unregistered != want {
				t.Fatalf("unregistered = %d, want %d", unregistered, want)
			}
			if tt.want == "" {
				if len(problems) != 0 {
					t.Fatalf("problems = %v, want none", problems)
				}
				return
			}
			if len(problems) != 1 || !strings.Contains(problems[0], tt.want) {
				t.Fatalf("problems = %v, want one naming %q", problems, tt.want)
			}
		})
	}
}

// rule: AI-R-003
func TestAI_R_003_GateKindsAndToolGapPath(t *testing.T) {
	spec := loadSpec(t)
	tests := []struct {
		name  string
		tools []string
		kinds []string
		want  []string // substrings, one per expected problem
	}{
		{"gap kind via draft_document and submit", []string{"draft_document", "report_plan", "submit_for_approval"}, []string{"core.user_activation"}, nil},
		{"gap kind via own drafting tool and submit", []string{"propose_deadline", "report_plan", "submit_for_approval"}, []string{"cal.second_check"}, nil},
		{"gap kind without submit", []string{"draft_document", "report_plan"}, []string{"core.user_activation"}, []string{toolSubmit}},
		{"gap kind with submit only", []string{"report_plan", "submit_for_approval"}, []string{"core.user_activation"}, []string{"drafting write tool"}},
		{"non-gap kind needs no submit", []string{"propose_deadline", "report_plan"}, []string{"cal.confirm"}, nil},
		{"unknown gate kind", []string{"draft_document", "report_plan", "submit_for_approval"}, []string{"core.no_such_gate"}, []string{"not in spec/gates.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Def{ID: "F-ACC-02", Area: "access", Name: "test", Source: SourceJob, Tools: tt.tools,
				GateKinds: tt.kinds, Prompt: prompts, PromptPath: "prompts/f-tim-01.md"}
			s := NewSet()
			if err := s.Add(d); err != nil {
				t.Fatalf("Add: %v", err)
			}
			problems, _ := syncProblems(s, spec)
			var got []string
			for _, p := range problems {
				// Only the gate checks are under test; tools, kinds and fixture differ from spec.
				if strings.Contains(p, "gate kinds [") || strings.Contains(p, ": tools [") ||
					strings.Contains(p, "fixture") || strings.Contains(p, "model_call") {
					continue
				}
				got = append(got, p)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("problems = %v, want %d naming %v", got, len(tt.want), tt.want)
			}
			for i, w := range tt.want {
				if !strings.Contains(got[i], w) {
					t.Fatalf("problem %q, want one naming %q", got[i], w)
				}
			}
		})
	}
}
