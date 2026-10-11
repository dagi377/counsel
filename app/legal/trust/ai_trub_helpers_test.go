package trust_test

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
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	"github.com/dagi377/counsel/app/legal/conflict/conflictfake"
	"github.com/dagi377/counsel/app/legal/matter/matterfake"
	"github.com/dagi377/counsel/app/legal/party/partyfake"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/docs/docsfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/work/workfake"
)

// Shared test helpers of the trust AI features F-TRU-06, F-TRU-07 and F-TRU-09. Every name
// carries the trub prefix.
//
// The end-to-end tests drive a Def with agenttest.FromFeatureWith over the app's real handlers
// and no stand-ins. The Seed has no trust or bill ports, so trubHandlers adds a withdrawal
// fake (the agent.TrustAvailable get_trust_balance reads), a bill fake (get_bill) and the
// matter's trust accounts ahead of the Seed's ports, and wraps the draft tools with
// trust.SourcedDraft so every cents figure a draft carries must be one the run read.

// trubOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const trubOtherLawyer = "user-synthetic-trust-lawyer"

// trubAccount is the synthetic trust account that holds every seeded ledger.
const trubAccount = "acct-synthetic-trust-b"

// trubDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var trubDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// trubAccounts lists trubAccount for every matter.
type trubAccounts struct{}

// TrustAccountsOf implements agent.MatterTrustAccounts.
func (trubAccounts) TrustAccountsOf(context.Context, string) ([]string, error) {
	return []string{trubAccount}, nil
}

// trubBalances reads a bill's unpaid balance from the bill fake.
type trubBalances struct{ bills *billfake.Fake }

// BalanceCents implements agent.BillBalances.
func (b trubBalances) BalanceCents(ctx context.Context, id bill.ID) (int64, error) {
	got, err := b.bills.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	return got.BalanceCents, nil
}

// trubDep appends d's value of type T to vals, when d has one.
func trubDep[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// trubHandlers is agent.Handlers() with each Build given the trust and bill fakes ahead of the
// Seed's ports, and draft_document and draft_email wrapped with trust.SourcedDraft.
func trubHandlers(t *testing.T, wd *withdrawfake.Fake, bills *billfake.Fake) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := []any{agent.TrustAvailable(wd), agent.MatterTrustAccounts(trubAccounts{}),
				agent.BillReader(bills), agent.BillBalances(trubBalances{bills: bills})}
			vals = trubDep[agent.CapabilityCheck](vals, d)
			vals = trubDep[roles.Walls](vals, d)
			vals = trubDep[agenttools.Plans](vals, d)
			vals = trubDep[agenttools.GateOpener](vals, d)
			vals = trubDep[agenttools.RunScopes](vals, d)
			vals = trubDep[*partyfake.Fake](vals, d)
			vals = trubDep[*matterfake.Fake](vals, d)
			vals = trubDep[*workfake.Fake](vals, d)
			vals = trubDep[*docsfake.Fake](vals, d)
			vals = trubDep[*conflictfake.Fake](vals, d)
			vals = trubDep[*deadlinefake.Fake](vals, d)
			vals = trubDep[*agenttools.Provenance](vals, d)
			vals = trubDep[*agenttest.SeedStore](vals, d)
			deps := agent.NewDeps(vals...)
			tool, err := build(deps)
			if err != nil || (name != agent.ToolDraftDocument && name != agent.ToolDraftEmail) {
				return tool, err
			}
			pv, err := agent.DepOf[*agenttools.Provenance](deps)
			if err != nil {
				return registry.Tool{}, err
			}
			// rule: ARC-U-027
			return trust.SourcedDraft(pv, tool)
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// trubLedger is one seeded matter: its trust balance and one bill.
type trubLedger struct {
	trustCents int64       // cleared trust balance; below zero is an overdrawn ledger
	billStatus bill.Status // the bill's status
	billCents  int64       // the bill's total and unpaid balance
	narrative  string      // the bill line's narrative, returned quoted
}

// trubWorld is one seeded run: the driver, the matter, its client contact and its bill.
type trubWorld struct {
	fd       *agenttest.FeatureDriver
	matterID string
	clientID string
	billID   string
}

// trubFeature builds def's FeatureDriver over the real handlers with a gate store in which the
// acting user and trubOtherLawyer hold every kind's approver capability, so the preparer rule,
// not a missing capability, is what stops the acting user. It seeds one matter with l's trust
// balance and bill.
func trubFeature(t *testing.T, def feature.Def, l trubLedger) trubWorld {
	t.Helper()
	wd := withdrawfake.New(48 * time.Hour)
	bills := billfake.New(func() time.Time { return trubDecideAt })
	fd := agenttest.FromFeatureWith(t, def, trubHandlers(t, wd, bills))
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, trubOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return trubDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	m := fd.Seed.Matter("2026-0601", "Synth Trust")
	wd.SetBalance(trubAccount, string(m.ID), l.trustCents, 0)
	b := bill.Bill{
		ID: "bill-synth-0601", Number: "INV-2026-0601", MatterID: string(m.ID), Status: l.billStatus,
		PreparedBy: agenttest.FeatureActingFor, FeesCents: l.billCents, TotalCents: l.billCents,
		BalanceCents: l.billCents, CreatedAt: trubDecideAt,
		Lines: []bill.Line{{Ordinal: 1, Source: "time", Narrative: l.narrative, QuantityMilli: 1000,
			RateCents: l.billCents, AmountCents: l.billCents}},
	}
	if err := bills.Put(b); err != nil {
		t.Fatalf("seed bill: %v", err)
	}
	return trubWorld{fd: fd, matterID: string(m.ID), clientID: string(m.ClientID), billID: string(b.ID)}
}

// trubDraftID is the id the Seed gives the run's first draft.
func trubDraftID(kind string) string { return "draft-" + kind + "-1" }

// trubCall is one scripted model tool call.
type trubCall struct {
	name  string
	input string // JSON
}

// trubJSON is v as JSON for a scripted call input.
func trubJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("script input: %v", err)
	}
	return string(b)
}

// trubPlan is a report_plan call of two steps.
func trubPlan() trubCall {
	return trubCall{agenttools.ToolReportPlan, `{"steps": [{"step_id": "read", "title": "Read the matter, its trust balance and its bill"}, {"step_id": "write", "title": "Prepare for approval"}]}`}
}

// trubReads are the read calls every trust script makes on w, in order.
func trubReads(w trubWorld) []trubCall {
	return []trubCall{
		{agent.ToolGetMatter, `{"matter_id": "` + w.matterID + `"}`},
		{agent.ToolGetTrustBalance, `{"matter_id": "` + w.matterID + `"}`},
		{agent.ToolGetBill, `{"bill_id": "` + w.billID + `"}`},
	}
}

// trubDocumentWrites plans, drafts body on w's matter and submits the draft under gateKind.
func trubDocumentWrites(t *testing.T, w trubWorld, title, body, gateKind string) []trubCall {
	t.Helper()
	return []trubCall{
		trubPlan(),
		{agent.ToolDraftDocument, trubJSON(t, map[string]any{"matter_id": w.matterID, "title": title, "body_markdown": body})},
		{agent.ToolSubmitForApproval, trubJSON(t, map[string]any{"subject_type": "document", "subject_id": trubDraftID("document"), "gate_kind": gateKind})},
	}
}

// trubEmailWrites plans and drafts body to the recipients on w's matter with submit set.
func trubEmailWrites(t *testing.T, w trubWorld, subject, body string, to ...string) []trubCall {
	t.Helper()
	return []trubCall{
		trubPlan(),
		{agent.ToolDraftEmail, trubJSON(t, map[string]any{"matter_id": w.matterID, "to_contact_ids": to,
			"subject": subject, "body_markdown": body, "submit": true})},
	}
}

// trubScript is an inline aifake fixture for def: one tool call per turn, then text.
func trubScript(def feature.Def, calls []trubCall, text string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", def.ID)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	dir := strings.TrimPrefix(def.Fixture, "testdata/ai/")
	return aifake.New(fstest.MapFS{dir + "/ok.yaml": {Data: []byte(b.String())}})
}

// trubFire runs def over w once with calls scripted.
func trubFire(t *testing.T, w trubWorld, calls []trubCall, prompt string) agenttest.FeatureRun {
	t.Helper()
	model := trubScript(w.fd.Def, calls, "Prepared for lawyer approval.")
	return w.fd.Fire(context.Background(), model, "run-"+strings.ToLower(w.fd.Def.ID), prompt)
}

// trubToolNames lists the tool names of calls.
func trubToolNames(calls []trubCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// trubCheckRun fails t unless run used no stand-in, ran every call, planned before its first
// write (firstWrite), stored one draft of kind prepared for the acting user, executed nothing
// and opened one gate of gateKind that only another lawyer can decide.
func trubCheckRun(t *testing.T, w trubWorld, run agenttest.FeatureRun, calls []trubCall, firstWrite, draftKind, gateKind string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	// rule: ARC-I-035
	if len(w.fd.StandIns) != 0 || len(run.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, run used %v; want none", w.fd.StandIns, run.StandIns)
	}
	if want := trubToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v", run.Tools, want)
	}
	// rule: ARC-I-039
	if run.PlanAt < 0 || run.FirstWrite <= run.PlanAt || run.Tools[run.FirstWrite] != firstWrite {
		t.Fatalf("plan at %d, first write %d in %v; want report_plan before %s", run.PlanAt, run.FirstWrite, run.Tools, firstWrite)
	}
	drafts := w.fd.Seed.Store.Drafts
	if len(drafts) != 1 || drafts[0].ID != trubDraftID(draftKind) || drafts[0].PreparedBy != agenttest.FeatureActingFor || drafts[0].MatterID != w.matterID {
		t.Fatalf("drafts = %+v, want one %s on %s prepared by %s", drafts, trubDraftID(draftKind), w.matterID, agenttest.FeatureActingFor)
	}
	trubCheckNoExecution(t, w, run)
	trubCheckGate(t, w.fd, run, gateKind)
}

// trubDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func trubDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: trubDecideAt.Add(-time.Minute),
	}, by)
}

// trubStatus is the HTTP status the API returns for a gate decision error.
func trubStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// trubCheckGate fails t unless the run opened exactly one gate, pending, of kind, prepared for
// the acting user, that neither an agent principal nor the acting user can decide and another
// lawyer can.
func trubCheckGate(t *testing.T, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, kind string) {
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
		// rule: AI-R-003
		// The agent never approves: an agent principal deciding is a 403.
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: trubOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := trubDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || trubStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, trubStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// trubCheckRefused fails t unless the run's only refusal is tool with want, no draft was
// stored and no gate opened: a refused write leaves no proposal row.
func trubCheckRefused(t *testing.T, w trubWorld, run agenttest.FeatureRun, tool string, want error, drafts int) {
	t.Helper()
	if len(run.Refused) != 1 || run.Refused[0].Tool != tool || !errors.Is(run.Refused[0].Err, want) {
		t.Fatalf("refused = %v, want %s refused with %v", run.Refused, tool, want)
	}
	if len(run.GateIDs) != 0 || len(w.fd.Seed.Store.Drafts) != drafts {
		t.Fatalf("gates opened: %v, drafts %d; want none and %d drafts", run.GateIDs, len(w.fd.Seed.Store.Drafts), drafts)
	}
}

// trubCheckInjection fails t unless run made exactly calls, the injected text (a bill line
// narrative) reached the model only as quoted data and is absent from every write input and
// gate payload.
func trubCheckInjection(t *testing.T, w trubWorld, run agenttest.FeatureRun, calls []trubCall, injected string) {
	t.Helper()
	if run.Err != nil || len(run.Refused) != 0 {
		t.Fatalf("run err = %v, refused = %v", run.Err, run.Refused)
	}
	if want := trubToolNames(calls); !slices.Equal(run.Tools, want) {
		t.Fatalf("tools = %v, want %v (no extra call)", run.Tools, want)
	}
	read := slices.Index(run.Tools, agent.ToolGetBill)
	if read < 0 || !strings.Contains(run.ToolResults[read].Text, injected) {
		t.Fatalf("get_bill result lacks the seeded narrative: %v", run.ToolResults)
	}
	// rule: ARC-I-038
	if err := agenttest.QuotedOnly(run.Run, injected); err != nil {
		t.Fatal(err)
	}
	if err := trubAbsent(context.Background(), w.fd, run, injected); err != nil {
		t.Fatal(err)
	}
}

// trubAbsent reports an error when text is in any write input of run or in the payload of any
// gate the run opened.
func trubAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
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

// trubCheckNoExecution fails t unless the run moved no money and decided nothing: every gate
// it opened is still pending and no tool it called authorizes, approves, delivers or
// executes.
func trubCheckNoExecution(t *testing.T, w trubWorld, run agenttest.FeatureRun) {
	t.Helper()
	// rule: AI-R-003
	for _, name := range run.Tools {
		for _, verb := range []string{"authorize", "approve", "deliver", "execute", "record_execution"} {
			if strings.Contains(name, verb) {
				t.Fatalf("run called %s", name)
			}
		}
	}
	for _, id := range run.GateIDs {
		g, err := w.fd.Seed.Gates.Get(context.Background(), gate.GateID(id))
		if err != nil {
			t.Fatal(err)
		}
		if g.Status != gate.StatusPending {
			t.Fatalf("gate %s is %s, want pending", id, g.Status)
		}
	}
}

// trubFeatureSpec is one spec/ai/features.json feature with its trigger and screen actions.
type trubFeatureSpec struct {
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

// trubSpec returns id's spec/ai/features.json entry.
func trubSpec(t *testing.T, id string) trubFeatureSpec {
	t.Helper()
	var spec struct {
		Features []trubFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f trubFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// trubCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id), a feature Set takes it once and its run scope is its gate kinds. A
// screen-action feature's one action is the one its trigger names.
func trubCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := trubSpec(t, d.ID)
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
		{"area", d.Area, "trust"},
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
	// rule: ARC-I-035
	// The run scope lets the run open only the Def's gate kinds.
	if !slices.Equal(rs.GateKinds, d.GateKinds) {
		t.Fatalf("run gate kinds = %v, want %v", rs.GateKinds, d.GateKinds)
	}
}

// trubCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, and names the task and the expected output from features.json plus every
// phrase in want.
func trubCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	sf := trubSpec(t, d.ID)
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

// trubCheckScheduled fails t unless b binds d to its schedule, with no event, acting for the
// designated lawyer, and d offers exactly actions on request.
func trubCheckScheduled(t *testing.T, b trigger.Binding, err error, d feature.Def, cadence trigger.Cadence, actions ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"feature", b.FeatureID, d.ID},
		{"no event", len(b.Events), 0},
		{"cadence", b.Cadence, cadence},
		{"designated lawyer", b.Designated, true},
		{"no fixed acting user", b.ActingFor, ""},
		{"on request", strings.Join(d.Actions, ","), strings.Join(actions, ",")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("%s = %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestTRUB_SourcedCents checks the trust draft amount check on its own: every cents figure
// must be one the run read, and it fails closed.
//
// rule: ARC-U-027
func TestTRUB_SourcedCents(t *testing.T) {
	pv := agenttools.NewProvenance()
	pv.Observe("run-1", json.RawMessage(`{"available_cents": 480000, "overdrawn_cents": -4200, "balance_cents": 125000}`))
	tests := []struct {
		name  string
		pv    *agenttools.Provenance
		runID string
		text  string
		want  error
	}{
		{"no amount", pv, "run-1", "The ledger is in balance.", nil},
		{"read figures", pv, "run-1", "Balance 480000 cents; bill 125,000 cents.", nil},
		{"overdraft figure", pv, "run-1", "Overdrawn ledgers: -4200 cents.", nil},
		{"invented figure", pv, "run-1", "Balance 480001 cents.", agenttools.ErrUnsourcedValue},
		{"worked-out figure", pv, "run-1", "Shortfall 355000 cents.", agenttools.ErrUnsourcedValue},
		{"other run", pv, "run-2", "Balance 480000 cents.", agenttools.ErrUnsourcedValue},
		{"fraction of a cent", pv, "run-1", "Balance 480000.5 cents.", agenttools.ErrUnsourcedValue},
		{"dollar sign", pv, "run-1", "Balance $4,800.00.", agenttools.ErrUnsourcedValue},
		{"dollars word", pv, "run-1", "Balance 4800 dollars.", agenttools.ErrUnsourcedValue},
		{"no provenance", nil, "run-1", "The ledger is in balance.", agenttools.ErrUnsourcedValue},
		{"no run", pv, "", "The ledger is in balance.", agenttools.ErrUnsourcedValue},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := trust.CheckSourcedCents(tt.pv, tt.runID, tt.text)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("CheckSourcedCents = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "480001") {
				t.Fatalf("error names the figure: %v", err)
			}
		})
	}
}

// TestTRUB_SourcedDraft checks the draft wrapper: an agent's invented amount never reaches the
// handler, a user's manual draft is not checked, and only a write tool can be wrapped.
//
// rule: ARC-U-027
func TestTRUB_SourcedDraft(t *testing.T) {
	pv := agenttools.NewProvenance()
	pv.Observe("run-1", json.RawMessage(`{"available_cents": 480000}`))
	calls := 0
	write := registry.Tool{Name: "draft_document", Kind: registry.ToolWrite, Handle: func(context.Context, registry.Principal, json.RawMessage) (registry.ToolResult, error) {
		calls++
		return registry.ToolResult{Output: json.RawMessage(`{}`)}, nil
	}}
	wrapped, err := trust.SourcedDraft(pv, write)
	if err != nil {
		t.Fatal(err)
	}
	agentP := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "u-1"}
	userP := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-1"}
	tests := []struct {
		name      string
		p         registry.Principal
		in        string
		want      error
		wantCalls int
	}{
		{"agent sourced", agentP, `{"title": "Worksheet", "body_markdown": "Balance 480000 cents."}`, nil, 1},
		{"agent invented", agentP, `{"title": "Worksheet", "body_markdown": "Balance 999 cents."}`, agenttools.ErrUnsourcedValue, 0},
		{"agent invented in subject", agentP, `{"subject": "Top up 999 cents", "body_markdown": "Balance 480000 cents."}`, agenttools.ErrUnsourcedValue, 0},
		{"agent bad input", agentP, `[`, agenttools.ErrInvalidInput, 0},
		{"user manual draft", userP, `{"title": "Worksheet", "body_markdown": "Balance 999 cents."}`, nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls = 0
			_, err := wrapped.Handle(context.Background(), tt.p, json.RawMessage(tt.in))
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) || calls != tt.wantCalls {
				t.Fatalf("Handle = %v, %d calls; want %v, %d calls", err, calls, tt.want, tt.wantCalls)
			}
		})
	}
	read := registry.Tool{Name: "get_bill", Kind: registry.ToolRead, Handle: write.Handle}
	if _, err := trust.SourcedDraft(pv, read); !errors.Is(err, agenttools.ErrBadTool) {
		t.Fatalf("wrap read tool = %v, want ErrBadTool", err)
	}
	if _, err := trust.SourcedDraft(nil, write); !errors.Is(err, agenttools.ErrUnsourcedValue) {
		t.Fatalf("wrap without provenance = %v, want ErrUnsourcedValue", err)
	}
}
