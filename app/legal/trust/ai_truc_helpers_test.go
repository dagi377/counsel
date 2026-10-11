package trust_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
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
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/gates/gatesfake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Shared test helpers of the trust features F-TRU-03 (payee-fraud-check), F-TRU-02
// (withdrawal-drafter) and F-TRU-01's write path. Every name carries the truc prefix.
//
// A run drives the Def with agenttest.FromFeatureWith over agent.Handlers(): every tool runs
// its production handler. The Seed has no trust, bill or uuid-keyed matter ports, so
// trucHandlers passes synthetic ones ahead of the Seed's: the trust balance is the driver's
// own withdrawal fake (the one the real prepare_trust_withdrawal checks), so a read and the
// write see one ledger.

// trucOtherLawyer is a second synthetic lawyer who may decide the run's gates.
const trucOtherLawyer = "user-synthetic-trust-approver"

// trucBill is the synthetic bill every run reads.
const trucBill = "00000000-0000-4000-8000-0000000003b1"

// trucNow is the fake stores' clock: a weekday morning in America/Toronto.
var trucNow = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// trucWorld is one seeded run: the driver, the bill store and the matter store.
type trucWorld struct {
	fd      *agenttest.FeatureDriver
	bills   *billfake.Fake
	matters trucMatters
}

// trucAvail reads the driver's withdrawal fake at call time (agent.TrustAvailable).
type trucAvail struct{ w *trucWorld }

func (a trucAvail) Available(ctx context.Context, account, matterID string) (int64, error) {
	return a.w.fd.Withdrawals.Available(ctx, account, matterID)
}

// trucAccounts holds every matter's ledger in agenttest.TrustAccount (agent.MatterTrustAccounts).
type trucAccounts struct{}

func (trucAccounts) TrustAccountsOf(context.Context, string) ([]string, error) {
	return []string{agenttest.TrustAccount}, nil
}

// trucBalances reads a bill's unpaid balance from the bill fake (agent.BillBalances).
type trucBalances struct{ bills *billfake.Fake }

func (b trucBalances) BalanceCents(ctx context.Context, id bill.ID) (int64, error) {
	got, err := b.bills.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	return got.BalanceCents, nil
}

// trucMatters is a matter.Query over uuid-keyed synthetic matters.
type trucMatters map[matter.ID]matter.Matter

func (m trucMatters) Get(_ context.Context, id matter.ID) (matter.Matter, error) {
	got, ok := m[id]
	if !ok {
		return matter.Matter{}, matter.ErrNotFound
	}
	return got, nil
}

func (m trucMatters) List(context.Context, matter.Filter) ([]matter.Matter, error) {
	out := make([]matter.Matter, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out, nil
}

func (m trucMatters) Members(context.Context, matter.ID) ([]matter.Member, error) { return nil, nil }

// trucDep appends d's value of type T to vals, when d has one.
func trucDep[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// trucHandlers is agent.Handlers() with each production Build given extra ahead of the Seed's
// ports. No handler is replaced or wrapped.
func trucHandlers(t *testing.T, extra ...any) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := slices.Clone(extra)
			vals = trucDep[agent.CapabilityCheck](vals, d)
			vals = trucDep[roles.Walls](vals, d)
			vals = trucDep[agenttools.Plans](vals, d)
			vals = trucDep[agenttools.GateOpener](vals, d)
			vals = trucDep[agenttools.RunScopes](vals, d)
			vals = trucDep[agent.SubjectMatters](vals, d)
			vals = trucDep[*agenttools.Provenance](vals, d)
			vals = trucDep[*agenttest.SeedStore](vals, d)
			return build(agent.NewDeps(vals...))
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// trucFeature builds def's driver over the production handlers. The run's matter is
// agenttest.TrustMatter holding trustCents in agenttest.TrustAccount; its one bill trucBill
// has status, a total and unpaid balance of billCents and one line with narrative, quoted
// by get_bill. Gates are stored where the acting user and trucOtherLawyer hold each kind's
// approver capability, so only the preparer rule stops the acting user.
func trucFeature(t *testing.T, def feature.Def, trustCents int64, status bill.Status, billCents int64, narrative string) *trucWorld {
	t.Helper()
	w := &trucWorld{
		bills: billfake.New(func() time.Time { return trucNow }),
		matters: trucMatters{agenttest.TrustMatter: {
			ID: agenttest.TrustMatter, Number: "2026-0303", ShortName: "Synth Trust Matter",
			ClientID: agenttest.PayeeContact, ResponsibleLawyerID: agenttest.FeatureActingFor, Status: matter.Open,
		}},
	}
	w.fd = agenttest.FromFeatureWith(t, def, trucHandlers(t,
		agent.TrustAvailable(trucAvail{w}), agent.MatterTrustAccounts(trucAccounts{}),
		agent.BillReader(w.bills), agent.BillBalances(trucBalances{w.bills}), matter.Query(w.matters),
	))
	w.fd.Withdrawals.SetBalance(agenttest.TrustAccount, agenttest.TrustMatter, trustCents, 0)
	b := bill.Bill{
		ID: trucBill, Number: "INV-2026-0303", MatterID: agenttest.TrustMatter, Status: status,
		PreparedBy: agenttest.FeatureActingFor, FeesCents: billCents, TotalCents: billCents, BalanceCents: billCents,
		CreatedAt: trucNow, Lines: []bill.Line{{Ordinal: 1, Source: "time", Narrative: narrative,
			QuantityMilli: 1000, RateCents: billCents, AmountCents: billCents}},
	}
	if err := w.bills.Put(b); err != nil {
		t.Fatalf("seed bill: %v", err)
	}
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, trucOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	w.fd.Seed.Gates = gatefake.New(func() time.Time { return trucNow }, caps)
	for _, k := range def.GateKinds {
		w.fd.Seed.GateKind(k)
	}
	return w
}

// trucCall is one scripted model tool call; input is JSON.
type trucCall struct {
	name  string
	input string
}

// trucJSON is v as JSON for a scripted input or answer.
func trucJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	return string(b)
}

// trucModel is an inline aifake script for featureID: one tool call per turn, then final as
// the answer text.
func trucModel(featureID string, calls []trucCall, final string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", featureID)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: '%s'\n      stop_reason: end_turn\n", strings.ReplaceAll(final, "'", "''"))
	return aifake.New(fstest.MapFS{strings.ToLower(featureID) + "/inline/ok.yaml": {Data: []byte(b.String())}})
}

// trucNames lists the tool names of calls.
func trucNames(calls []trucCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// trucOutcome is the run as the job runner sees it: the final answer and every citation the
// run's tool results quoted (a quoted envelope, or get_bill's quoted narratives).
func trucOutcome(r agenttest.FeatureRun) run.Outcome {
	out := run.Outcome{Text: r.Text, JSON: r.JSON, GateIDs: r.GateIDs}
	for _, b := range r.ToolResults {
		if b.IsError {
			continue
		}
		var res struct {
			agenttools.Quoted
			Quotes []agenttools.Quoted `json:"quotes"`
		}
		if json.Unmarshal([]byte(b.Text), &res) != nil {
			continue
		}
		for _, q := range append([]agenttools.Quoted{res.Quoted}, res.Quotes...) {
			if q.Untrusted && q.Source.Page > 0 {
				out.Citations = append(out.Citations, q.Source)
			}
		}
	}
	return out
}

// trucBillLine is the citation get_bill returns for line ordinal of trucBill.
func trucBillLine(ordinal int) map[string]any {
	c := agent.BillLineCitation(trucBill, ordinal)
	return map[string]any{"version_id": c.VersionID, "page": c.Page, "from_line": c.FromLine, "to_line": c.ToLine}
}

// trucSpecFeature is one spec/ai/features.json feature.
type trucSpecFeature struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Source    string   `json:"source"`
	Trigger   string   `json:"trigger"`
	OnRequest []string `json:"on_request"`
	Tools     []string `json:"tools"`
	ModelCall string   `json:"model_call"`
	Fixture   string   `json:"fixture"`
	Gate      struct {
		Kind []string `json:"kind"`
	} `json:"gate"`
}

// trucCheckSpec fails t unless d matches its spec/ai/features.json entry and a feature Set
// takes it once.
func trucCheckSpec(t *testing.T, d feature.Def) {
	t.Helper()
	var spec struct {
		Features []trucSpecFeature `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f trucSpecFeature) bool { return f.ID == d.ID })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", d.ID)
	}
	sf := spec.Features[i]
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, "|") }
	tests := []struct {
		field     string
		got, want string
	}{
		{"name", d.Name, sf.Name},
		{"source", string(d.Source), sf.Source},
		{"trigger", d.Trigger, sf.Trigger},
		{"on request", sorted(d.Actions), sorted(sf.OnRequest)},
		{"tools", sorted(d.Tools), sorted(sf.Tools)},
		{"gate kinds", sorted(d.GateKinds), sorted(sf.Gate.Kind)},
		{"model call", string(d.Kind), sf.ModelCall},
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
}

// trucCheckPrompt lints d's prompt and checks the system prompt starts with the preamble,
// carries the shared rules and states each of want.
func trucCheckPrompt(t *testing.T, d feature.Def, want ...string) {
	t.Helper()
	names, err := feature.FixtureNames(os.DirFS("../../.."), "testdata/ai")
	if err != nil {
		t.Fatalf("FixtureNames: %v", err)
	}
	if err := feature.LintPrompt(d, names); err != nil {
		t.Fatalf("LintPrompt: %v", err)
	}
	spec, err := d.Spec()
	if err != nil {
		t.Fatalf("Spec: %v", err)
	}
	if !strings.HasPrefix(spec.System, feature.Preamble(d.ID)) {
		t.Fatal("system prompt does not start with the feature preamble")
	}
	for _, w := range append([]string{feature.Rules()}, want...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// trucCheckAgentNeverDecides fails t unless deciding a gate of kind as the agent principal,
// or as the user who prepared it, is refused with 403.
func trucCheckAgentNeverDecides(t *testing.T, kind string) {
	t.Helper()
	ctx := context.Background()
	gf, err := gatesfake.New(gates.Kind{Code: kind, Approver: kind + ".approve"})
	if err != nil {
		t.Fatal(err)
	}
	actor := agenttest.FeatureActingFor
	caps := []string{kind + ".approve"}
	agentBy := gates.Principal{UserID: "agent", Agent: true, ActingFor: actor, Capabilities: caps}
	person := gates.Principal{UserID: actor, Capabilities: caps}
	tests := []struct {
		name     string
		preparer gates.Principal
		by       gates.Principal
		want     error
	}{
		{"agent decides", gates.Principal{UserID: trucOtherLawyer}, agentBy, gates.ErrAgentApproves},
		{"preparer decides", person, person, gates.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := gates.Gate{ID: "gate-truc", Code: kind, Preparer: tt.preparer}
			err := gf.CheckDecision(ctx, g, tt.by, time.Unix(0, 0))
			if !errors.Is(err, tt.want) || agenttest.DecisionStatus(err) != http.StatusForbidden {
				t.Fatalf("decision = %v (%d), want %v (403)", err, agenttest.DecisionStatus(err), tt.want)
			}
		})
	}
}

// trucPlan is a report_plan call of two steps.
func trucPlan() trucCall {
	return trucCall{agenttools.ToolReportPlan, `{"steps": [{"step_id": "read", "title": "Read the matter, trust and bill"}, {"step_id": "prepare", "title": "Prepare the withdrawal"}]}`}
}

// trucNoWithdrawal fails t if the trust module holds any withdrawal row.
func trucNoWithdrawal(t *testing.T, w *trucWorld) {
	t.Helper()
	if got, err := w.fd.Withdrawals.Get(context.Background(), "wd_1"); err == nil {
		t.Fatalf("withdrawal row %+v recorded, want none", got)
	}
}

// trucEffectMethods are the trust and billing methods only a gate effect or a person reaches.
var trucEffectMethods = []string{"Authorize", "Approve", "Deliver", "RecordExecution"}

// trucCheckNoEffectCalls fails t if file (in this package) calls any of trucEffectMethods.
func trucCheckNoEffectCalls(t *testing.T, file string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok && slices.Contains(trucEffectMethods, sel.Sel.Name) {
			t.Errorf("%s reaches %s", file, sel.Sel.Name)
		}
		return true
	})
}
