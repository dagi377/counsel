package timekeeping_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Shared test helpers of the timekeeping AI features. Every name carries the timc prefix.
//
// The end-to-end tests drive a Def with agenttest.FromFeatureWith over the app's real
// handlers and no stand-ins. The Seed has no time ports, so timcHandlers adds a timekeeping
// fake (the tk.Entries propose_time_entries writes to) and timcMatterTime (the agent.MatterTime
// list_unbilled prices from) ahead of the Seed's ports.

// timcOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const timcOtherLawyer = "user-synthetic-time-lawyer"

// timcApprover approves the seeded entries, so they are unbilled WIP.
const timcApprover = "user-synthetic-time-approver"

// timcRateCents is the synthetic hourly rate every seeded entry is priced at.
const timcRateCents = 45000

// timcDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var timcDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// timcWorkDate is the work date of every seeded entry.
var timcWorkDate = time.Date(2026, 10, 2, 0, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// timcMatterTime prices the fake's entries of a matter at timcRateCents.
type timcMatterTime struct{ time *timekeepingfake.Fake }

// MatterTime implements agent.MatterTime.
func (m timcMatterTime) MatterTime(ctx context.Context, matterID string) ([]agent.PricedEntry, error) {
	all, err := m.time.List(ctx, tk.EntryFilter{})
	if err != nil {
		return nil, err
	}
	var out []agent.PricedEntry
	for _, e := range all {
		if e.MatterID != matterID {
			continue
		}
		// Round half up to the cent.
		amount := (int64(e.BilledMinutes)*timcRateCents + 30) / 60
		out = append(out, agent.PricedEntry{Entry: e, RateCents: timcRateCents, AmountCents: amount})
	}
	return out, nil
}

// timcDep appends d's value of type T to vals, when d has one.
func timcDep[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// timcHandlers is agent.Handlers() with each Build given the timekeeping fake and its
// pricing ahead of the Seed's ports.
func timcHandlers(t *testing.T, tf *timekeepingfake.Fake) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := []any{tk.Entries(tf), agent.MatterTime(timcMatterTime{time: tf})}
			vals = timcDep[agent.CapabilityCheck](vals, d)
			vals = timcDep[roles.Walls](vals, d)
			vals = timcDep[agenttools.Plans](vals, d)
			vals = timcDep[agenttools.GateOpener](vals, d)
			vals = timcDep[agenttools.RunScopes](vals, d)
			vals = timcDep[agent.ClientMatters](vals, d)
			vals = timcDep[matter.Query](vals, d)
			vals = timcDep[agent.WorkList](vals, d)
			vals = timcDep[agent.DocumentText](vals, d)
			vals = timcDep[*agenttools.Provenance](vals, d)
			vals = timcDep[*agenttest.SeedStore](vals, d)
			return build(agent.NewDeps(vals...))
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// timcWorld is one seeded run: the driver, the timekeeping fake, the matter and its
// unbilled entries.
type timcWorld struct {
	fd       *agenttest.FeatureDriver
	time     *timekeepingfake.Fake
	matterID string
	entries  []tk.EntryID
}

// timcFeature builds def's FeatureDriver over the real handlers with a gate store in which
// the acting user and timcOtherLawyer hold every kind's approver capability, so the preparer
// rule, not a missing capability, is what stops the acting user. It seeds one matter with one
// open task and one approved, unbilled entry of 90 minutes per narrative.
func timcFeature(t *testing.T, def feature.Def, narratives ...string) timcWorld {
	t.Helper()
	tf := timekeepingfake.New(nil, nil)
	fd := agenttest.FromFeatureWith(t, def, timcHandlers(t, tf))
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, timcOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return timcDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter("2026-0501", "Synth Retainer")
	fd.Seed.WorkItem(string(m.ID), "Review the signed engagement letter")
	w := timcWorld{fd: fd, time: tf, matterID: string(m.ID)}
	ctx := context.Background()
	for _, n := range narratives {
		id, err := tf.Record(ctx, nil, tk.TimeEntry{
			MatterID: w.matterID, UserID: agenttest.FeatureActingFor, WorkDate: timcWorkDate, Minutes: 90,
			ActivityCode: "A101", Narrative: n, Billable: true,
		}, tk.Actor{UserID: agenttest.FeatureActingFor})
		if err != nil {
			t.Fatalf("seed entry: %v", err)
		}
		if err := tf.Transition(ctx, nil, id, tk.StateSubmitted, tk.Actor{UserID: agenttest.FeatureActingFor}); err != nil {
			t.Fatalf("submit entry: %v", err)
		}
		if err := tf.Transition(ctx, nil, id, tk.StateApproved, tk.Actor{UserID: timcApprover}); err != nil {
			t.Fatalf("approve entry: %v", err)
		}
		w.entries = append(w.entries, id)
	}
	return w
}

// timcDraftID is the id the Seed gives the run's first draft (no document is seeded).
const timcDraftID = "draft-document-1"

// timcCall is one scripted model tool call.
type timcCall struct {
	name  string
	input string // JSON
}

// timcReads are the read calls every timekeeping script makes on w, in order.
func timcReads(w timcWorld) []timcCall {
	return []timcCall{
		{agent.ToolGetMatter, `{"matter_id": "` + w.matterID + `"}`},
		{agent.ToolListUnbilled, `{"matter_id": "` + w.matterID + `"}`},
		{agent.ToolListWorkItems, `{"matter_id": "` + w.matterID + `"}`},
	}
}

// timcPlan is a report_plan call of two steps.
func timcPlan() timcCall {
	return timcCall{agenttools.ToolReportPlan, `{"steps": [{"step_id": "read", "title": "Read the matter, its time and tasks"}, {"step_id": "write", "title": "Prepare for approval"}]}`}
}

// timcDraftWrites plans, drafts on w's matter and submits the draft under gateKind.
func timcDraftWrites(w timcWorld, title, body, gateKind string) []timcCall {
	return []timcCall{
		timcPlan(),
		{agent.ToolDraftDocument, `{"matter_id": "` + w.matterID + `", "title": "` + title + `", "body_markdown": "` + body + `"}`},
		{agent.ToolSubmitForApproval, `{"subject_type": "document", "subject_id": "` + timcDraftID + `", "gate_kind": "` + gateKind + `"}`},
	}
}

// timcScript is an inline aifake fixture for feature: one tool call per turn, then text.
func timcScript(feature, dir string, calls []timcCall, text string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", feature)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	return aifake.New(fstest.MapFS{strings.ToLower(feature) + "/" + dir + "/ok.yaml": {Data: []byte(b.String())}})
}

// timcToolNames lists the tool names of calls.
func timcToolNames(calls []timcCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// timcCheckRun fails t unless run used no stand-in, ran every call, planned before its first
// write (firstWrite), read the seeded unbilled time for real and opened one gate of kind that
// only another lawyer can decide.
func timcCheckRun(t *testing.T, w timcWorld, run agenttest.FeatureRun, calls []timcCall, firstWrite, kind string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want none", w.fd.StandIns, run.StandIns)
	}
	if want := timcToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != firstWrite {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before %s", run.PlanAt, run.FirstWrite, run.Tools, firstWrite)
	}
	// list_unbilled ran its real handler over the seeded entries.
	read := slices.Index(run.Tools, agent.ToolListUnbilled)
	for _, id := range w.entries {
		if read < 0 || !strings.Contains(run.ToolResults[read].Text, string(id)) {
			t.Fatalf("list_unbilled result lacks seeded entry %s: %v", id, run.ToolResults)
		}
	}
	timcCheckGate(t, w.fd, run, kind)
}

// timcCheckDraft fails t unless the run stored exactly one draft, on w's matter, prepared for
// the acting user and never in the agent's name.
func timcCheckDraft(t *testing.T, w timcWorld) {
	t.Helper()
	drafts := w.fd.Seed.Store.Drafts
	if len(drafts) != 1 || drafts[0].ID != timcDraftID || drafts[0].PreparedBy != agenttest.FeatureActingFor || drafts[0].MatterID != w.matterID {
		t.Fatalf("drafts = %+v, want one %s on %s prepared by %s", drafts, timcDraftID, w.matterID, agenttest.FeatureActingFor)
	}
}

// timcDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func timcDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: timcDecideAt.Add(-time.Minute),
	}, by)
}

// timcStatus is the HTTP status the API returns for a gate decision error.
func timcStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// timcCheckGate fails t unless the run opened exactly one gate, pending, of kind, prepared
// for the acting user, that neither an agent principal nor the acting user can decide and
// another lawyer can.
func timcCheckGate(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, kind string) {
	t.Helper()
	ctx := context.Background()
	if len(run.GateIDs) != 1 {
		t.Fatalf("gates = %v, want one", run.GateIDs)
	}
	id := run.GateIDs[0]
	g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatal(err)
	}
	// rule: AI-R-003
	if g.Kind != kind || g.Status != gate.StatusPending || g.Preparer.ActingFor != agenttest.FeatureActingFor {
		t.Fatalf("gate = %s %s preparer %+v; want pending %s prepared for %s", g.Kind, g.Status, g.Preparer, kind, agenttest.FeatureActingFor)
	}
	tests := []struct {
		name       string
		by         registry.Principal
		wantErr    error
		wantStatus int
	}{
		// rule: ARC-I-038
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: timcOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := timcDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || timcStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, timcStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// timcCheckGateKindRefused fails t unless submit_for_approval was refused with
// ErrGateKindNotAllowed, no gate opened and the one draft stays stored.
func timcCheckGateKindRefused(t *testing.T, w timcWorld, run agenttest.FeatureRun) {
	t.Helper()
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolSubmitForApproval || !errors.Is(run.Refused[0].Err, agenttools.ErrGateKindNotAllowed) {
		t.Fatalf("refused = %v, want submit_for_approval refused with ErrGateKindNotAllowed", run.Refused)
	}
	if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != 1 {
		t.Fatalf("gates opened: %v, drafts %d; want none and the one draft", run.GateIDs, len(w.fd.Seed.Store.Drafts))
	}
}

// timcCheckInjection fails t unless run made exactly calls, the injected text (an entry
// narrative) reached the model only as quoted data and is absent from every write input and
// gate payload.
func timcCheckInjection(t *testing.T, w timcWorld, run agenttest.FeatureRun, calls []timcCall, injected string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := timcToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	read := slices.Index(run.Tools, agent.ToolListUnbilled)
	if !strings.Contains(run.ToolResults[read].Text, injected) {
		t.Fatalf("list_unbilled result lacks the seeded narrative: %s", run.ToolResults[read].Text)
	}
	// rule: ARC-I-038
	if err := agenttest.QuotedOnly(run.Run, injected); err != nil {
		t.Fatal(err)
	}
	if err := timcAbsent(context.Background(), w.fd, run, injected); err != nil {
		t.Fatal(err)
	}
}

// timcAbsent reports an error when text is in any write input of run or in the payload of
// any gate the run opened.
func timcAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
	kinds := map[string]bool{}
	for _, s := range fd.Feature.Tools {
		kinds[s.Name] = s.Kind == agent.Write
	}
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && kinds[b.ToolName] && strings.Contains(string(b.Input), text) {
					return fmt.Errorf("write input carries the injected text: %s", b.Input)
				}
			}
		}
	}
	for _, id := range run.GateIDs {
		g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
		if err != nil {
			return err
		}
		if strings.Contains(string(g.Payload), text) {
			return fmt.Errorf("gate %s payload carries the injected text", id)
		}
	}
	return nil
}

// timcFeatureSpec is one spec/ai/features.json feature with its trigger and screen actions.
type timcFeatureSpec struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Trigger   string   `json:"trigger"`
	OnRequest []string `json:"on_request"`
	Outputs   string   `json:"outputs"`
	Tools     []string `json:"tools"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

// timcSpec returns id's spec/ai/features.json entry.
func timcSpec(t *testing.T, id string) timcFeatureSpec {
	t.Helper()
	var spec struct {
		Features []timcFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f timcFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// timcCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id), a feature Set takes it once and its run scope is its gate kinds.
// A screen-action feature's one action is the one its trigger names.
func timcCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := timcSpec(t, d.ID)
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	actions := sf.OnRequest
	if d.Source == feature.SourceScreenAction {
		actions = []string{strings.TrimPrefix(sf.Trigger, "on request: ")}
	}
	kind := d.Kind
	if kind == "" {
		kind = ai.KindToolUse
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"name", d.Name, sf.Name},
		{"area", d.Area, "time"},
		{"source", string(d.Source), sf.Source},
		{"trigger", d.Trigger, sf.Trigger},
		{"on request", sorted(d.Actions), sorted(actions)},
		{"tools", sorted(d.Tools), sorted(sf.Tools)},
		{"gate kinds", sorted(d.GateKinds), sorted(sf.Gate.Kind)},
		{"model call", string(kind), sf.ModelCall},
		{"fixture", d.Fixture, sf.Fixture},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %q, spec %q", tt.field, tt.got, tt.want)
			}
		})
	}
	s := feature.NewSet()
	if err := s.Add(d); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(d); !errors.Is(err, feature.ErrBadDef) {
		t.Fatalf("second Add = %v, want ErrBadDef", err)
	}
	rs, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	// rule: AI-R-003
	// The run scope lets the run open only the Def's gate kinds.
	if !slices.Equal(rs.GateKinds, d.GateKinds) {
		t.Fatalf("run gate kinds = %v, want %v", rs.GateKinds, d.GateKinds)
	}
}

// timcCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, and names the task and the expected output from features.json plus every
// phrase in want.
func timcCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	sf := timcSpec(t, d.ID)
	names, err := feature.FixtureNames(os.DirFS("../../.."), "testdata/ai")
	if err != nil {
		t.Fatalf("FixtureNames: %v", err)
	}
	// rule: AI-R-005
	if err := feature.LintPrompt(d, names); err != nil {
		t.Fatalf("LintPrompt: %v", err)
	}
	spec, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if !strings.HasPrefix(spec.System, feature.Preamble(d.ID)) {
		t.Fatalf("system prompt does not start with the feature preamble")
	}
	for _, w := range append([]string{feature.Rules(), sf.Name, sf.Outputs}, want...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// timcCheckBinding fails t unless b binds d to events with no cadence, run for the event's
// user.
func timcCheckBinding(t *testing.T, b trigger.Binding, err error, d feature.Def, events ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"feature", b.FeatureID, d.ID},
		{"events", strings.Join(b.Events, ","), strings.Join(events, ",")},
		{"cadence", b.Cadence, trigger.CadenceNone},
		{"acting for", b.ActingFor, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}
