package feature

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dagi377/counsel/app/legal/agent/schemas"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/registry"
)

// strictSync is the spec-sync gate of FEAT-CORE-16 (AI-R-002). While it is false, every spec
// feature must have a registered Def except the ids in pendingDefs, and every Def's tools
// must run their real handlers under agenttest.FromFeature except the tools in
// pendingStandIns: a new gap fails, and a pending entry that is closed is reported so the
// list can shrink. Integration pass 19 sets it to true once the feature branches below are
// merged and agenttest's Seed builds every tool; then any unregistered feature or any
// stand-in fails. Never add an entry to either list to make a test pass.
const strictSync = false

// pendingDefs are the spec/ai/features.json ids with no registered Def on main when
// FEAT-CORE-16 landed. Each is registered on a pushed task branch not yet merged:
// FEAT-ACC-07 to 11, FEAT-BIL-03/07/08, FEAT-PLT-01 to 27, FEAT-RDY-01 to 09 and
// FEAT-TRU-03/08/10/11.
var pendingDefs = []string{
	"F-ACC-01", "F-ACC-05", "F-ACC-08", "F-ACC-09", "F-ACC-11", "F-ACC-12", "F-ACC-13",
	"F-BIL-03", "F-BIL-07", "F-BIL-08",
	"F-PLT-01", "F-PLT-02", "F-PLT-03", "F-PLT-04", "F-PLT-05", "F-PLT-06", "F-PLT-07",
	"F-PLT-08", "F-PLT-09", "F-PLT-10", "F-PLT-11", "F-PLT-12", "F-PLT-13", "F-PLT-14",
	"F-PLT-15", "F-PLT-16", "F-PLT-17", "F-PLT-18", "F-PLT-19", "F-PLT-20", "F-PLT-21",
	"F-PLT-22", "F-PLT-23", "F-PLT-24", "F-PLT-25", "F-PLT-26", "F-PLT-27", "F-PLT-28",
	"F-RDY-01", "F-RDY-02", "F-RDY-03", "F-RDY-04", "F-RDY-05", "F-RDY-06", "F-RDY-07",
	"F-RDY-08", "F-RDY-09",
	"F-TRU-02", "F-TRU-03", "F-TRU-08", "F-TRU-10",
}

// pendingStandIns are the a2 tools agenttest.FromFeature still runs as stand-ins because its
// Seed does not provide their ports (MatterTime, TaskCreator and ProposedTasks, BillReader and
// BillBalances, TrustAvailable and MatterTrustAccounts, tk.Entries, payment.Service, the draft
// bill stores). The feature tests of those tools pass the ports with FromFeatureWith; pass 19
// moves them into the Seed.
var pendingStandIns = []string{
	"create_task", "draft_bill", "get_bill", "get_trust_balance", "list_unbilled",
	"propose_payment_allocation", "propose_time_entries",
}

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
// mismatch, plus the spec features with no Def, in spec order.
func syncProblems(s *Set, spec aiSpec) (problems []string, unregistered []string) {
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
			unregistered = append(unregistered, id)
		}
	}
	return problems, unregistered
}

// gateProblems applies the strictSync gate to gaps, the spec features or tools still open,
// against pending. While the gate is off a gap outside pending is a problem and a pending
// entry that is no longer a gap is returned as closed; once it is on every gap is a problem.
func gateProblems(what string, gaps, pending []string) (problems, closed []string) {
	for _, g := range gaps {
		// rule: AI-R-002
		if strictSync || !slices.Contains(pending, g) {
			problems = append(problems, fmt.Sprintf("%s %s", what, g))
		}
	}
	if !strictSync {
		for _, p := range pending {
			if !slices.Contains(gaps, p) {
				closed = append(closed, p)
			}
		}
	}
	return problems, closed
}

// CheckRegistered is the spec-sync check of the app's feature Set s (AI-R-002): every Def in
// s matches its spec/ai/features.json entry, and every spec feature has a registered Def,
// under the strictSync gate. featureset imports this package, so only the external test
// package can build s (spec_sync_test.go).
func CheckRegistered(t *testing.T, s *Set) {
	t.Helper()
	spec := loadSpec(t)
	problems, unregistered := syncProblems(s, spec)
	for _, p := range problems {
		t.Error(p)
	}
	gaps, closed := gateProblems("spec feature with no registered Def:", unregistered, pendingDefs)
	for _, p := range gaps {
		t.Error(p)
	}
	if len(closed) > 0 {
		t.Logf("registered now, drop from pendingDefs: %v", closed)
	}
	if !strictSync {
		t.Logf("%d of %d spec features have no registered Def (strictSync is off)", len(unregistered), len(spec.order))
	}
}

// CheckStandIns is the stand-in check of FEAT-CORE-16: standIns maps each spec feature to the
// tools agenttest.FromFeature ran as stand-ins for it, and every one must run its real
// handler, under the strictSync gate.
func CheckStandIns(t *testing.T, standIns map[string][]string) {
	t.Helper()
	var tools []string
	for _, names := range standIns {
		for _, n := range names {
			if !slices.Contains(tools, n) {
				tools = append(tools, n)
			}
		}
	}
	slices.Sort(tools)
	gaps, closed := gateProblems("tool runs as a stand-in under agenttest.FromFeature:", tools, pendingStandIns)
	for _, p := range gaps {
		t.Error(p)
	}
	if len(closed) > 0 {
		t.Logf("real now, drop from pendingStandIns: %v", closed)
	}
}

// SpecDef builds a Def from the spec/ai/features.json entry id, with a one-line prompt, so a
// feature not registered yet can still be driven through agenttest.FromFeature.
func SpecDef(t *testing.T, id string) Def {
	t.Helper()
	d := defFromSpec(t, loadSpec(t), id)
	d.Prompt = fstest.MapFS{"spec.md": {Data: []byte("# " + id + "\n")}}
	d.PromptPath = "spec.md"
	return d
}

// SpecIDs lists the spec/ai/features.json ids in spec order.
func SpecIDs(t *testing.T) []string {
	t.Helper()
	return slices.Clone(loadSpec(t).order)
}

// rule: AI-R-002
func TestAI_R_002_GateRefusesNewGaps(t *testing.T) {
	tests := []struct {
		name        string
		gaps        []string
		pending     []string
		wantProblem int
		wantClosed  []string
	}{
		{"pending gap", []string{"F-X-01"}, []string{"F-X-01"}, 0, nil},
		{"new gap", []string{"F-X-01", "F-X-02"}, []string{"F-X-01"}, 1, nil},
		{"closed gap", nil, []string{"F-X-01"}, 0, []string{"F-X-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems, closed := gateProblems("gap", tt.gaps, tt.pending)
			if strictSync {
				// Once strict, every gap is a problem and nothing is pending.
				if len(problems) != len(tt.gaps) || len(closed) != 0 {
					t.Fatalf("strict: problems %v, closed %v", problems, closed)
				}
				return
			}
			if len(problems) != tt.wantProblem || !slices.Equal(closed, tt.wantClosed) {
				t.Fatalf("problems %v, closed %v; want %d, %v", problems, closed, tt.wantProblem, tt.wantClosed)
			}
		})
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
			if want := len(spec.order) - 1; tt.want != "not in spec" && len(unregistered) != want {
				t.Fatalf("unregistered = %d, want %d", len(unregistered), want)
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
