package deadlines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	legaldocs "github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// dlPack is the synthetic published rule pack the deadline features compute against.
var dlPack = deadline.Pack{ID: "syn-civil", Version: "2026.1", Rules: []deadline.Rule{
	{ID: "defence", Mode: deadline.Calendar, Days: 20},
	{ID: "limitation", Mode: deadline.Calendar, MonthsAfter: 24},
}}

// dlQuoteHash is a well-formed synthetic quote hash.
var dlQuoteHash = "sha256:" + strings.Repeat("ab", 32)

// dlWorld holds the module ports the deadline tools read through beyond the agenttest Seed:
// the deadline engine, the published packs, the run's provenance, document lookup and the
// matter of a submitted subject. Every record is synthetic.
type dlWorld struct {
	mu        sync.Mutex
	Deadlines *deadlinefake.Fake
	Prov      *agenttools.Provenance
	docs      map[string]legaldocs.Document
	subjects  map[string]string
}

func newDLWorld(t *testing.T) *dlWorld {
	t.Helper()
	w := &dlWorld{Deadlines: deadlinefake.New(), Prov: agenttools.NewProvenance(), docs: map[string]legaldocs.Document{}, subjects: map[string]string{}}
	if err := w.Deadlines.LoadPack(context.Background(), dlPack); err != nil {
		t.Fatalf("load pack: %v", err)
	}
	return w
}

// PublishedPacks implements agent.PublishedPacks.
func (w *dlWorld) PublishedPacks(context.Context) ([]deadline.Pack, error) {
	return []deadline.Pack{dlPack}, nil
}

// Document implements agent.DocumentLookup over the documents the test seeded.
func (w *dlWorld) Document(_ context.Context, id string) (legaldocs.Document, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	d, ok := w.docs[id]
	if !ok {
		return legaldocs.Document{}, legaldocs.ErrNotFound
	}
	return d, nil
}

// SubjectMatter implements agent.SubjectMatters for subjects the test registered.
func (w *dlWorld) SubjectMatter(_ context.Context, _, id string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.subjects[id], nil
}

func (w *dlWorld) addDoc(id, matterID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.docs[id] = legaldocs.Document{ID: id, MatterID: matterID, Kind: agenttest.SeedDocKind}
}

func (w *dlWorld) addSubject(id, matterID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.subjects[id] = matterID
}

// pull appends the value of type T in d, if d has one.
func pull[T any](d agent.Deps, out []any) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(out, v)
	}
	return out
}

// handlers is the app's handler set for def's tools with the world's ports added, every read
// tool recording into the run's provenance, and get_matter/read_document served by
// agent.ReadTools (they have no Handler of their own).
func (w *dlWorld) handlers(t *testing.T, def feature.Def) agent.HandlerSet {
	t.Helper()
	deps := func(d agent.Deps) agent.Deps {
		vals := []any{w.Deadlines, w.Prov, w, agent.DeadlineSaver(w.Deadlines), agent.DeadlineCompute(w.Deadlines), deadline.Lister(w.Deadlines)}
		vals = pull[agent.CapabilityCheck](d, vals)
		vals = pull[roles.Walls](d, vals)
		vals = pull[agenttools.Plans](d, vals)
		vals = pull[agenttools.GateOpener](d, vals)
		vals = pull[agent.DocumentText](d, vals)
		vals = pull[*agenttest.SeedStore](d, vals)
		vals = pull[matter.Query](d, vals)
		vals = append(vals, agenttools.RunScopes{})
		return agent.NewDeps(vals...)
	}
	var hs []agent.Handler
	for _, name := range def.Tools {
		name := name
		switch name {
		case agent.ToolGetMatter, agent.ToolReadDocument, agent.ToolSearchMatters:
			hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
				return w.readTool(name, deps(d))
			}})
			continue
		}
		h, ok := agent.HandlerFor(name)
		if !ok {
			continue
		}
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			tool, err := h.Build(deps(d))
			if err != nil || tool.Kind != registry.ToolRead {
				return tool, err
			}
			return w.Prov.ReadTool(tool)
		}})
	}
	s, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return s
}

// readTool serves one agent.ReadTools tool as a registry tool.
func (w *dlWorld) readTool(name string, d agent.Deps) (registry.Tool, error) {
	can, err := agent.DepOf[agent.CapabilityCheck](d)
	if err != nil {
		return registry.Tool{}, err
	}
	walls, err := agent.DepOf[roles.Walls](d)
	if err != nil {
		return registry.Tool{}, err
	}
	text, err := agent.DepOf[agent.DocumentText](d)
	if err != nil {
		return registry.Tool{}, err
	}
	mq, err := agent.DepOf[matter.Query](d)
	if err != nil {
		return registry.Tool{}, err
	}
	rt, err := agent.NewReadTools(agent.ReadDeps{Can: can, Matters: mq, Walls: walls, Documents: w, Text: text})
	if err != nil {
		return registry.Tool{}, err
	}
	i := slices.IndexFunc(rt.Specs(), func(s agent.ToolSpec) bool { return s.Name == name })
	s := rt.Specs()[i]
	tool := registry.Tool{Name: name, Kind: registry.ToolRead, Capability: s.Capability, Input: s.InputSchema,
		Handle: func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			res, err := rt.Call(ctx, agent.Call{RunID: p.RunID, ActingFor: p.ActingFor, Tool: name, Input: in})
			if err != nil {
				return registry.ToolResult{}, err
			}
			out := registry.ToolResult{Output: res.Output}
			for _, c := range res.Citations {
				out.Citations = append(out.Citations, registry.Citation{VersionID: c.VersionID, Page: c.Page, FromLine: c.FromLine, ToLine: c.ToLine})
			}
			return out, nil
		}}
	return w.Prov.ReadTool(tool)
}

// dlFeature builds def's FeatureDriver over the real handlers, with a gate engine where the
// acting user holds every kind's approver capability, so the only thing that stops their
// decision is the preparer rule.
func dlFeature(t *testing.T, def feature.Def) (*agenttest.FeatureDriver, *dlWorld) {
	t.Helper()
	w := newDLWorld(t)
	fd := agenttest.FromFeatureWith(t, def, w.handlers(t, def))
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		caps[agenttest.FeatureActingFor] = append(caps[agenttest.FeatureActingFor], k+".approve")
		caps["user-synthetic-partner"] = append(caps["user-synthetic-partner"], k+".approve")
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC) }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	return fd, w
}

// turn is one scripted model reply: tool calls, or the final text.
type turn struct {
	Calls []call
	Text  string
}

type call struct {
	Name  string
	Input map[string]any
}

// script renders an inline aifake fixture for featureID from turns. JSON is valid YAML.
func script(t *testing.T, featureID string, turns ...turn) *aifake.Fake {
	t.Helper()
	var out []map[string]any
	for _, tn := range turns {
		if len(tn.Calls) == 0 {
			out = append(out, map[string]any{"reply": map[string]any{"text": tn.Text, "stop_reason": "end_turn"}})
			continue
		}
		var calls []map[string]any
		for _, c := range tn.Calls {
			calls = append(calls, map[string]any{"name": c.Name, "input": c.Input})
		}
		out = append(out, map[string]any{"reply": map[string]any{"tool_calls": calls, "stop_reason": "tool_use"}})
	}
	b, err := json.Marshal(map[string]any{
		"synthetic": true, "feature": featureID, "match": map[string]any{"last_user_regex": "."}, "turns": out,
	})
	if err != nil {
		t.Fatal(err)
	}
	return aifake.New(fstest.MapFS{strings.ToLower(featureID) + "/inline/ok.yaml": {Data: b}})
}

func plan(steps ...string) call {
	var s []map[string]any
	for _, st := range steps {
		s = append(s, map[string]any{"step_id": st, "title": "Step " + st})
	}
	return call{Name: agent.ToolReportPlan, Input: map[string]any{"steps": s}}
}

// source is the propose_deadline citation of the seeded document.
func source(docID, versionID string) map[string]any {
	return map[string]any{"doc_id": docID, "version_id": versionID, "page": 1, "line_start": 1, "line_end": 2, "quote_hash": dlQuoteHash}
}

// gateDecisions checks the run's gates: each pending with the acting user as preparer (via
// the agent principal), an agent principal deciding is 403, and the acting user deciding
// their own gate is ErrSelfApproval.
func gateDecisions(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, wantKinds []string) {
	t.Helper()
	ctx := context.Background()
	var kinds []string
	for _, id := range run.GateIDs {
		g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
		if err != nil {
			t.Fatalf("gate %s: %v", id, err)
		}
		kinds = append(kinds, g.Kind)
		// rule: AI-R-003
		if g.Status != gate.StatusPending || g.Preparer.Kind != registry.PrincipalAgent || g.Preparer.ActingFor != agenttest.FeatureActingFor {
			t.Fatalf("gate %s: status %s preparer %+v, want pending, acting user as preparer", id, g.Status, g.Preparer)
		}
		dec := gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: g.CreatedAt}
		cases := []struct {
			name string
			by   registry.Principal
			want func(error) bool
		}{
			// rule: ARC-I-039
			{"agent principal -> 403", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-x", ActingFor: "user-synthetic-partner"},
				func(err error) bool { p := gate.Problem(err); return p != nil && p.Status == http.StatusForbidden }},
			{"acting user -> ErrSelfApproval", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor},
				func(err error) bool { return errors.Is(err, gate.ErrSelfApproval) }},
		}
		for _, c := range cases {
			err := fd.Seed.Gates.Decide(ctx, nil, g.ID, g.Version, dec, c.by)
			if !c.want(err) {
				t.Fatalf("gate %s %s: err = %v", id, c.name, err)
			}
		}
		if g, _ := fd.Seed.Gates.Get(ctx, g.ID); g.Status != gate.StatusPending {
			t.Fatalf("gate %s decided: %s", id, g.Status)
		}
	}
	slices.Sort(kinds)
	want := slices.Clone(wantKinds)
	slices.Sort(want)
	if !slices.Equal(kinds, want) {
		t.Fatalf("gate kinds = %v, want %v", kinds, want)
	}
}

// docQuotedOnly reports an error unless text reached the model only inside a tool_result
// that marks its content untrusted (read_document's {"untrusted": true, pages}) or quotes it
// (agenttools.Quoted), and the final answer does not repeat it (ARC-I-038).
func docQuotedOnly(run agenttest.FeatureRun, text string) error {
	if err := agenttest.QuotedOnly(run.Run, text); err == nil {
		return nil
	}
	seen := false
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if !strings.Contains(b.Text, text) && !strings.Contains(string(b.Input), text) {
					continue
				}
				var env struct {
					Untrusted bool `json:"untrusted"`
				}
				if b.Type != "tool_result" || b.ToolName != agent.ToolReadDocument || json.Unmarshal([]byte(b.Text), &env) != nil || !env.Untrusted {
					return fmt.Errorf("injected text sent outside quoted data: %s block", b.Type)
				}
				seen = true
			}
		}
	}
	if !seen {
		return errors.New("injected text never reached the model")
	}
	if strings.Contains(run.Text, text) {
		return errors.New("final answer repeats the injected text")
	}
	return nil
}

// dlSeed seeds one matter with a filed order (page 1 dated 2026-10-05) and returns the
// matter, document and version ids.
func dlSeed(t *testing.T, fd *agenttest.FeatureDriver, w *dlWorld, lines ...string) (string, string, string) {
	t.Helper()
	m := fd.Seed.Matter("2026-0101", "Synth v Example")
	mid := string(m.ID)
	if len(lines) == 0 {
		lines = []string{"ORDER made 2026-10-05.", "The defendant shall deliver a statement of defence within 20 days."}
	}
	docID, v := fd.Seed.Document(mid, lines)
	w.addDoc(docID, mid)
	return mid, docID, string(v)
}

// rule: ARC-I-035
// rule: ARC-I-039
// rule: AI-R-003
func TestFDL01_FromFeature(t *testing.T) {
	def := ExtractDeadlinesDef()
	type tc struct {
		name       string
		dueOn      string
		inject     bool
		wantTools  []string
		wantGates  []string
		wantRefuse error
	}
	full := []string{"report_plan", "get_matter", "read_document", "list_deadlines", "compute_deadline", "propose_deadline"}
	cases := []tc{
		{name: "ok", dueOn: "2026-10-25", wantTools: full, wantGates: []string{agent.GateCalConfirm}},
		// rule: ARC-I-035
		// A date the model worked out itself (not from compute_deadline or a cited document).
		{name: "model-computed date", dueOn: "2026-10-26", wantTools: full, wantRefuse: agenttools.ErrUnsourcedValue},
		// rule: ARC-I-038
		{name: "injection", dueOn: "2026-10-25", inject: true, wantTools: full, wantGates: []string{agent.GateCalConfirm}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fd, w := dlFeature(t, def)
			if len(fd.StandIns) != 0 {
				t.Fatalf("stand-ins = %v, want none", fd.StandIns)
			}
			var lines []string
			if c.inject {
				lines = []string{"ORDER made 2026-10-05.", "The defendant shall deliver a statement of defence within 20 days.", injected}
			}
			mid, docID, ver := dlSeed(t, fd, w, lines...)
			fd.Seed.Deadline(mid, "syn-civil/reply", deadline.Date{Year: 2026, Month: 12, Day: 1})
			model := script(t, def.ID,
				turn{Calls: []call{plan("read", "compute", "propose")}},
				turn{Calls: []call{{Name: "get_matter", Input: map[string]any{"matter_id": mid}}}},
				turn{Calls: []call{{Name: "read_document", Input: map[string]any{"doc_id": docID, "version_id": ver}}}},
				turn{Calls: []call{{Name: "list_deadlines", Input: map[string]any{"matter_id": mid}}}},
				turn{Calls: []call{{Name: "compute_deadline", Input: map[string]any{"rule_id": "syn-civil/defence", "trigger_date": "2026-10-05"}}}},
				turn{Calls: []call{{Name: "propose_deadline", Input: map[string]any{
					"matter_id": mid, "title": "Deliver statement of defence", "due_on": c.dueOn,
					"rule_id": "syn-civil/defence", "trigger_date": "2026-10-05", "source": source(docID, ver),
				}}}},
				turn{Text: "Proposed the defence deadline for confirmation."},
			)
			run := fd.Fire(context.Background(), model, "run-fdl01-"+c.name, "extract deadlines from the filed order")
			if run.Err != nil {
				t.Fatalf("run: %v", run.Err)
			}
			if !slices.Equal(run.Tools, c.wantTools) {
				t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, c.wantTools)
			}
			// rule: AI-R-003
			// report_plan precedes the first write.
			if run.PlanAt != 0 || (c.wantRefuse == nil && run.FirstWrite <= run.PlanAt) {
				t.Fatalf("plan at %d, first write %d", run.PlanAt, run.FirstWrite)
			}
			if c.wantRefuse != nil {
				if len(run.Refused) != 1 || run.Refused[0].Tool != "propose_deadline" || !errors.Is(run.Refused[0].Err, c.wantRefuse) {
					t.Fatalf("refused = %v, want propose_deadline %v", run.Refused, c.wantRefuse)
				}
				if len(run.GateIDs) != 0 {
					t.Fatalf("unsourced date opened gates %v", run.GateIDs)
				}
				if got, _ := w.Deadlines.List(context.Background(), deadline.Filter{}); len(got) != 0 {
					t.Fatalf("unsourced date saved %d deadlines", len(got))
				}
				return
			}
			if len(run.Refused) != 0 {
				t.Fatalf("refused = %v", run.Refused)
			}
			gateDecisions(t, fd, run, c.wantGates)
			if c.inject {
				if err := docQuotedOnly(run, injected); err != nil {
					t.Fatal(err)
				}
				for _, id := range run.GateIDs {
					g, _ := fd.Seed.Gates.Get(context.Background(), gate.GateID(id))
					if strings.Contains(string(g.Payload), "ignore previous") {
						t.Fatalf("injected text in gate %s payload", id)
					}
				}
				for _, req := range run.Requests {
					for _, m := range req.Messages {
						for _, b := range m.Content {
							if b.Type == "tool_use" && strings.Contains(string(b.Input), "ignore previous") {
								t.Fatalf("injected text in %s input", b.ToolName)
							}
						}
					}
				}
			}
		})
	}
}

// F-DL-01 starts from a filed document, and on request from the matter-deadlines screen.
func TestFDL01_TriggerBinding(t *testing.T) {
	b, err := ExtractDeadlinesBinding()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	def := ExtractDeadlinesDef()
	tests := []struct {
		field     string
		got, want string
	}{
		{"feature", b.FeatureID, "F-DL-01"},
		{"events", strings.Join(b.Events, ","), EventDocumentUploaded},
		{"cadence", string(b.Cadence), string(trigger.CadenceNone)},
		{"acting for", b.ActingFor, ""},
		{"trigger", def.Trigger, "event: document filed to matter (order, endorsement, pleading, affidavit of service, notice, correspondence, email)"},
		{"on request", strings.Join(def.Actions, ";"), "matter-deadlines > Extract from document"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, want %q", tt.field, tt.got, tt.want)
			}
		})
	}
}

// rule: ARC-I-035
// The write path never invents a due date: an item with only a trigger and a period is
// refused until compute_deadline gives its date.
func TestFDL01_ProposeInput(t *testing.T) {
	cite := agenttools.Citation{VersionID: "ver-1", Page: 1, FromLine: 2, ToLine: 3}
	tests := []struct {
		name    string
		item    ProposedDeadline
		wantErr error
	}{
		{"stated due date", ProposedDeadline{Title: "Serve affidavit", DueDate: "2026-11-20", Citation: cite}, nil},
		{"trigger and period only", ProposedDeadline{Title: "Costs", TriggerDate: "2026-10-09", Period: "30 days", Citation: cite}, ErrBadProposal},
		{"no title", ProposedDeadline{DueDate: "2026-11-20", Citation: cite}, ErrBadProposal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := ProposeInput("matter-1", tt.item, "doc-1", dlQuoteHash)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			var in struct {
				DueOn  string `json:"due_on"`
				Source struct {
					VersionID string `json:"version_id"`
					LineStart int    `json:"line_start"`
				} `json:"source"`
			}
			if err := json.Unmarshal(b, &in); err != nil || in.DueOn != tt.item.DueDate || in.Source.VersionID != "ver-1" || in.Source.LineStart != 2 {
				t.Fatalf("input = %s (%v)", b, err)
			}
		})
	}
}
