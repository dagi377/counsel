package billing_test

// Shared helpers of the billing feature tests (prefix bil). Each test drives a feature Def
// with agenttest.FromFeatureWith over the app's real tool handlers. The Seed lacks the billing
// ports, so bilWorld supplies them from in-memory fakes holding synthetic records only: a
// matter's priced time, its bills, its trust balance and the draft bills draft_bill builds.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/draft/draftfake"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/payment/paymentfake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Synthetic records every billing feature test reads. The ids are fixed so the checked-in
// fixtures can name them.
const (
	bilMatter      = "00000000-0000-4000-8000-0000000b1001"
	bilEntryA      = "00000000-0000-4000-8000-0000000b1e01"
	bilEntryB      = "00000000-0000-4000-8000-0000000b1e02"
	bilBillA       = "00000000-0000-4000-8000-0000000b1b01"
	bilBillB       = "00000000-0000-4000-8000-0000000b1b02"
	bilPayment     = "00000000-0000-4000-8000-0000000b1a01"
	bilOtherLawyer = "user-synthetic-partner"
	// bilEntryACents and bilEntryBCents are the priced amounts of the two unbilled entries.
	bilEntryACents = 50_000
	bilEntryBCents = 20_000
	// bilTrustCents is the matter's trust balance.
	bilTrustCents = 120_000
)

// bilDecideAt is when the tests decide gates.
var bilDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)

// bilTime is the matter's priced time (agent.MatterTime) and its unbilled WIP
// (agent.UnbilledWIP), and builds draft bills from it (batch.Drafts) into lines.
type bilTime struct {
	lines *draftfake.Fake

	mu      sync.Mutex
	entries []agent.PricedEntry
	keys    map[string]string
}

func newBilTime(lines *draftfake.Fake) *bilTime {
	return &bilTime{lines: lines, keys: map[string]string{}}
}

// add records an approved, billable, unbilled entry priced at cents.
func (b *bilTime) add(id, matterID string, minutes int, cents int64, narrative string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, agent.PricedEntry{
		Entry: tk.TimeEntry{
			ID: tk.EntryID(id), MatterID: matterID, UserID: agenttest.FeatureActingFor,
			WorkDate: time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC), Minutes: minutes, BilledMinutes: minutes,
			ActivityCode: "A101", Narrative: narrative, Billable: true, State: tk.StateApproved,
		},
		RateCents: cents * 60 / int64(minutes), AmountCents: cents,
	})
}

func (b *bilTime) MatterTime(_ context.Context, matterID string) ([]agent.PricedEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []agent.PricedEntry
	for _, e := range b.entries {
		if e.Entry.MatterID == matterID {
			out = append(out, e)
		}
	}
	return out, nil
}

func (b *bilTime) UnbilledEntryIDs(ctx context.Context, matterID string, _ clock.Date) ([]string, error) {
	es, err := b.MatterTime(ctx, matterID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(es))
	for _, e := range es {
		ids = append(ids, string(e.Entry.ID))
	}
	return ids, nil
}

// Create assembles a draft from the matter's unbilled WIP, one fee line per entry. A retried
// key returns the first draft.
func (b *bilTime) Create(ctx context.Context, key string, t batch.Target, _ clock.Date) (string, bool, error) {
	es, err := b.MatterTime(ctx, t.MatterID)
	if err != nil {
		return "", false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if id, ok := b.keys[key]; ok {
		return id, false, nil
	}
	id := fmt.Sprintf("00000000-0000-4000-8000-0000000b1d%02d", len(b.keys)+1)
	lines := make([]draft.Line, 0, len(es))
	for i, e := range es {
		lines = append(lines, draft.Line{
			ID: fmt.Sprintf("line-%d", i+1), Kind: draft.LineFee, SortOrder: i + 1, Description: "Synthetic time",
			AmountCents: e.AmountCents, SourceTimeEntryID: string(e.Entry.ID),
		})
	}
	if err := b.lines.SaveLines(ctx, id, lines); err != nil {
		return "", false, err
	}
	b.keys[key] = id
	return id, true, nil
}

// drafts is how many draft bills Create built.
func (b *bilTime) drafts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.keys)
}

// bilBills is the bill store get_bill reads (agent.BillReader and agent.BillBalances).
type bilBills struct{ *billfake.Fake }

func (b bilBills) BalanceCents(ctx context.Context, id bill.ID) (int64, error) {
	got, err := b.Get(ctx, id)
	if err != nil {
		return 0, err
	}
	return got.BalanceCents, nil
}

// bilTrust is the matter's cleared trust balance in one synthetic trust account
// (agent.TrustAvailable and agent.MatterTrustAccounts).
type bilTrust struct {
	mu      sync.Mutex
	balance map[string]int64
}

func (b *bilTrust) set(matterID string, cents int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.balance[matterID] = cents
}

func (b *bilTrust) Available(_ context.Context, account, matterID string) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if account != agenttest.TrustAccount {
		return 0, nil
	}
	return b.balance[matterID], nil
}

func (b *bilTrust) TrustAccountsOf(context.Context, string) ([]string, error) {
	return []string{agenttest.TrustAccount}, nil
}

// bilPayments is a payment service holding one captured, non-cash payment of
// bilEntryACents+bilEntryBCents cents on the matter (bilPayment), as a provider webhook or a
// bank feed line records it before F-BIL-04 runs.
func bilPayments(t *testing.T) *paymentfake.Fake {
	t.Helper()
	p := paymentfake.New()
	p.NewID = func() string { return bilPayment }
	p.AddBill(bilBillA, "client-synthetic", bilEntryACents+bilEntryBCents, 0)
	if _, _, err := p.Capture(context.Background(), payment.Capture{
		ClientID: "client-synthetic", BillID: bilBillA, Kind: payment.KindBillPayment, AmountCents: bilEntryACents + bilEntryBCents,
		ReceivedOn: bilDecideAt, Provider: "synthetic-feed", ProviderEventID: "evt-synthetic-1", MatterID: bilMatter, Method: "eft",
	}); err != nil {
		t.Fatalf("capture: %v", err)
	}
	return p
}

// bilWorld is one feature run's driver and the billing fakes its real handlers read.
type bilWorld struct {
	fd    *agenttest.FeatureDriver
	time  *bilTime
	lines *draftfake.Fake
	bills bilBills
	trust *bilTrust
}

// bilFeature builds def's FeatureDriver over the real handlers with the billing ports, the
// synthetic matter's two unbilled entries (narrative of entry A: narrativeA), a draft bill and
// its trust balance, and a gate store in which the acting user and bilOtherLawyer could
// approve, so the preparer rule is what stops the acting user. It fails on any stand-in.
func bilFeature(t *testing.T, def feature.Def, narrativeA string, extra ...any) bilWorld {
	t.Helper()
	lines := draftfake.New()
	w := bilWorld{
		time: newBilTime(lines), lines: lines,
		bills: bilBills{billfake.New(func() time.Time { return bilDecideAt })},
		trust: &bilTrust{balance: map[string]int64{}},
	}
	w.time.add(bilEntryA, bilMatter, 60, bilEntryACents, narrativeA)
	w.time.add(bilEntryB, bilMatter, 24, bilEntryBCents, "Synthetic call with the client")
	if err := w.bills.Put(bill.Bill{
		ID: bilBillA, MatterID: bilMatter, Status: bill.StatusDraft, PreparedBy: agenttest.FeatureActingFor,
		FeesCents: bilEntryACents + bilEntryBCents, TotalCents: bilEntryACents + bilEntryBCents,
		BalanceCents: bilEntryACents + bilEntryBCents, CreatedAt: bilDecideAt,
		Lines: []bill.Line{
			{Ordinal: 1, Source: bill.SourceTime, SourceID: bilEntryA, Narrative: narrativeA, QuantityMilli: 1000, RateCents: bilEntryACents, AmountCents: bilEntryACents},
			{Ordinal: 2, Source: bill.SourceTime, SourceID: bilEntryB, Narrative: "Synthetic call with the client", QuantityMilli: 400, RateCents: bilEntryACents, AmountCents: bilEntryBCents},
		},
	}); err != nil {
		t.Fatalf("bill: %v", err)
	}
	w.trust.set(bilMatter, bilTrustCents)
	ports := append([]any{w.time, w.bills, w.trust, lines}, extra...)
	w.fd = agenttest.FromFeatureWith(t, def, bilHandlers(t, ports...))
	if len(w.fd.StandIns) != 0 {
		t.Fatalf("stand-ins = %v, want none (every tool runs its real handler)", w.fd.StandIns)
	}
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, bilOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	w.fd.Seed.Gates = gatefake.New(func() time.Time { return bilDecideAt }, caps)
	for _, k := range def.GateKinds {
		w.fd.Seed.GateKind(k)
	}
	return w
}

// bilPull appends d's T, when it has one, to vals.
func bilPull[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// bilHandlers is agent.Handlers() with each Build given ports ahead of the Seed's. A port
// that is an agent.Handler replaces the app's handler of its tool (a feature's own wrapper).
// list_unbilled also records its figures in the run's provenance, as the Def's prompt and
// accept text require (see docs/QUESTIONS.md FEAT-BIL-01: the boot handler does not yet).
func bilHandlers(t *testing.T, ports ...any) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	own := map[string]agent.Handler{}
	ports = slices.DeleteFunc(slices.Clone(ports), func(p any) bool {
		h, ok := p.(agent.Handler)
		if ok {
			own[h.Name] = h
		}
		return ok
	})
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		if o, ok := own[name]; ok {
			h = o
		}
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := slices.Clone(ports)
			vals = bilPull[agent.CapabilityCheck](vals, d)
			vals = bilPull[roles.Walls](vals, d)
			vals = bilPull[agenttools.Plans](vals, d)
			vals = bilPull[agenttools.GateOpener](vals, d)
			vals = bilPull[agenttools.RunScopes](vals, d)
			vals = bilPull[agent.DocumentText](vals, d)
			vals = bilPull[agent.DocumentLookup](vals, d)
			vals = bilPull[agent.SubjectMatters](vals, d)
			vals = bilPull[agent.ClientMatters](vals, d)
			vals = bilPull[agent.ContactSearch](vals, d)
			vals = bilPull[matter.Query](vals, d)
			vals = bilPull[*agenttools.Provenance](vals, d)
			vals = bilPull[*agenttest.SeedStore](vals, d)
			tool, err := build(agent.NewDeps(vals...))
			if err != nil || name != agent.ToolListUnbilled {
				return tool, err
			}
			pv, err := agent.DepOf[*agenttools.Provenance](d)
			if err != nil {
				return registry.Tool{}, err
			}
			return pv.ReadTool(tool)
		}})
	}
	set, err := agent.NewHandlerSet(hs...)
	if err != nil {
		t.Fatalf("handler set: %v", err)
	}
	return set
}

// bilCall is one scripted model tool call.
type bilCall struct {
	name  string
	input map[string]any
}

// bilPlan is a report_plan call over steps.
func bilPlan(steps ...string) bilCall {
	var s []map[string]any
	for _, st := range steps {
		s = append(s, map[string]any{"step_id": st, "title": "Step " + st})
	}
	return bilCall{agenttools.ToolReportPlan, map[string]any{"steps": s}}
}

// bilNames lists the tool names of calls.
func bilNames(calls []bilCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// bilScript is an inline aifake model for featureID: one tool call per turn, then text.
func bilScript(t *testing.T, featureID string, calls []bilCall, text string) ai.ModelClient {
	t.Helper()
	var turns []map[string]any
	for _, c := range calls {
		turns = append(turns, map[string]any{"reply": map[string]any{
			"tool_calls": []map[string]any{{"name": c.name, "input": c.input}}, "stop_reason": "tool_use",
		}})
	}
	turns = append(turns, map[string]any{"reply": map[string]any{"text": text, "stop_reason": "end_turn"}})
	b, err := json.Marshal(map[string]any{
		"synthetic": true, "feature": featureID, "match": map[string]any{"last_user_regex": "."}, "turns": turns,
	})
	if err != nil {
		t.Fatal(err)
	}
	return aifake.New(fstest.MapFS{strings.ToLower(featureID) + "/inline/ok.yaml": {Data: b}})
}

// bilDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func bilDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: bilDecideAt.Add(-time.Minute),
	}, by)
}

// bilStatus is the HTTP status the API returns for a gate decision error.
func bilStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// bilCheckGate fails t unless gate id is pending, of kind, prepared for the acting user, and
// neither an agent principal (403) nor the acting user (ErrSelfApproval, 403) can decide it
// while another lawyer can.
func bilCheckGate(t *testing.T, fd *agenttest.FeatureDriver, id, kind string) {
	t.Helper()
	ctx := context.Background()
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
		// rule: ARC-I-039
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: bilOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bilDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || bilStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, bilStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// bilAbsent reports an error when text is in any write input of run or in the payload of any
// gate the run opened.
func bilAbsent(ctx context.Context, fd *agenttest.FeatureDriver, run agenttest.FeatureRun, text string) error {
	writes := map[string]bool{}
	for _, s := range fd.Feature.Tools {
		writes[s.Name] = s.Kind == agent.Write
	}
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if b.Type == "tool_use" && writes[b.ToolName] && strings.Contains(string(b.Input), text) {
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

// bilRefused returns the error the run's refused call of tool carried, or nil.
func bilRefused(run agenttest.FeatureRun, tool string) error {
	for _, r := range run.Refused {
		if r.Tool == tool {
			return r.Err
		}
	}
	return nil
}

// bilFeatureSpec is one spec/ai/features.json feature.
type bilFeatureSpec struct {
	ID        string   `json:"id"`
	Area      string   `json:"area"`
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

// bilSpec returns id's spec/ai/features.json entry.
func bilSpec(t *testing.T, id string) bilFeatureSpec {
	t.Helper()
	var spec struct {
		Features []bilFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f bilFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// bilCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id), a feature Set takes it once and its run scope is its gate kinds.
func bilCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := bilSpec(t, d.ID)
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	kind := string(d.Kind)
	if kind == "" {
		kind = string(ai.KindToolUse)
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"name", d.Name, sf.Name},
		{"area", d.Area, sf.Area},
		{"source", string(d.Source), sf.Source},
		{"trigger", d.Trigger, sf.Trigger},
		{"on request", sorted(d.Actions), sorted(sf.OnRequest)},
		{"tools", sorted(d.Tools), sorted(sf.Tools)},
		{"gate kinds", sorted(d.GateKinds), sorted(sf.Gate.Kind)},
		{"model call", kind, sf.ModelCall},
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

// bilCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, names the task and the expected output from features.json, and the ok
// fixture pins the prompt hash.
func bilCheckPrompt(t *testing.T, d feature.Def) {
	t.Helper()
	sf := bilSpec(t, d.ID)
	root := os.DirFS("../../..")
	names, err := feature.FixtureNames(root, "testdata/ai")
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
	if !strings.HasPrefix(spec.System, feature.Preamble(d.ID)+feature.Rules()) {
		t.Fatalf("system prompt does not start with the preamble and the shared rules")
	}
	for _, w := range []string{"shared rules", sf.Name, sf.Outputs} {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
	if err := feature.LintFixtureHash(d, root); err != nil {
		t.Fatal(err)
	}
	ok, err := fs.ReadFile(root, d.Fixture+"/ok.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ok), "prompt_hash:") {
		t.Fatal("ok fixture has no match.prompt_hash")
	}
}

// bilEffects are the module calls that approve, authorize, deliver or execute: the effect
// side a prepare-only feature must never reach.
var bilEffects = map[string]bool{"Authorize": true, "Approve": true, "Deliver": true, "RecordExecution": true}

// bilCheckPrepareOnly fails t when a non-test source of this package selects an effect call.
func bilCheckPrepareOnly(t *testing.T) {
	t.Helper()
	fset := token.NewFileSet()
	var found []string
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && bilEffects[sel.Sel.Name] {
				found = append(found, fset.Position(sel.Pos()).String()+" "+sel.Sel.Name)
			}
			return true
		})
	}
	// rule: ARC-U-027
	if len(found) > 0 {
		t.Fatalf("billing features reach an effect: %v", found)
	}
}

// bilLanded is a landed acceptance test moved onto agenttest.FromFeature: the Def runs over
// the real handlers and answers from its checked-in fixture variants.
type bilLanded struct {
	def      feature.Def
	dir      string // fixture dir under testdata/ai
	trigger  string // the run's prompt; the variant is appended in brackets
	injected string // entry A's narrative in the injection variant
	calls    []string
	gateKind string
	extra    func() []any // more ports per run, e.g. a payment service
	// observed is the trigger payload recorded in the run's provenance before it starts, as
	// the trigger records its event's values (dates the run may then write); nil records none.
	observed json.RawMessage
	// check checks the opened gate's payload.
	check func(t *testing.T, payload json.RawMessage)
}

// run checks every required fixture variant.
func (l bilLanded) run(t *testing.T) {
	t.Helper()
	agenttest.Variants(t, l.def.ID, l.dir)
	tests := []struct {
		variant   string
		wantRetry bool
		wantErr   error
	}{
		{variant: "ok"},
		// rule: AI-R-003
		{variant: "error_429", wantRetry: true},
		{variant: "timeout", wantErr: ai.ErrTimeout},
		// rule: ARC-I-038
		{variant: "injection"},
	}
	for _, tc := range tests {
		t.Run(tc.variant, func(t *testing.T) {
			ctx := context.Background()
			inject := tc.variant == "injection"
			narrative := "Synthetic review of the claim"
			if inject {
				narrative = l.injected
			}
			var extra []any
			if l.extra != nil {
				extra = l.extra()
			}
			w := bilFeature(t, l.def, narrative, extra...)
			model := agenttest.Model(t, l.dir, tc.variant)
			runID := "run-" + strings.ToLower(l.def.ID) + "-" + tc.variant
			prompt := l.trigger + " [" + tc.variant + "]"
			if l.observed != nil {
				w.fd.Seed.Provenance.Observe(runID, l.observed)
			}
			run := w.fd.Fire(ctx, model, runID, prompt)
			if !errors.Is(run.Err, tc.wantErr) || run.RetryQueued != tc.wantRetry {
				t.Fatalf("err = %v queued %v, want %v queued %v", run.Err, run.RetryQueued, tc.wantErr, tc.wantRetry)
			}
			// rule: AI-R-003
			// A failed or rate-limited call leaves no partial write.
			if len(run.Tools) != 0 && (tc.wantErr != nil || tc.wantRetry) {
				t.Fatalf("partial write on fault: calls %v gates %v", run.Tools, run.GateIDs)
			}
			if tc.wantErr != nil {
				return
			}
			if tc.wantRetry {
				// The queued retry reruns the same run id and now reaches the script.
				run = w.fd.Fire(ctx, model, runID, prompt)
			}
			if run.Err != nil || run.RetryQueued || len(run.StandIns) != 0 {
				t.Fatalf("run: err %v queued %v stand-ins %v", run.Err, run.RetryQueued, run.StandIns)
			}
			want := l.calls
			if inject {
				want = append(slices.Clone(l.calls), agent.ToolPrepareTrustWithdrawal)
			}
			// rule: ARC-I-038
			if !slices.Equal(run.Tools, want) {
				t.Fatalf("calls = %v, want %v", run.Tools, want)
			}
			if inject {
				if err := agenttest.QuotedOnly(run.Run, l.injected); err != nil {
					t.Fatal(err)
				}
				// rule: ARC-I-036
				// The injected trust withdrawal is refused: the run holds no trust capability.
				if err := w.fd.RefusedInjectedWrite(ctx, run.Run, agenttools.ErrMissingCapability); err != nil {
					t.Fatal(err)
				}
				if err := bilAbsent(ctx, w.fd, run, l.injected); err != nil {
					t.Fatal(err)
				}
			} else if len(run.Refused) != 0 {
				t.Fatalf("refused calls: %v", run.Refused)
			}
			// rule: ARC-I-039
			if run.PlanAt < 0 || run.PlanAt > run.FirstWrite {
				t.Fatalf("report_plan at %d, first write at %d: plan must come first", run.PlanAt, run.FirstWrite)
			}
			if len(run.GateIDs) != 1 || run.Text == "" {
				t.Fatalf("gates = %v, answer %q; want one gate and an answer", run.GateIDs, run.Text)
			}
			g, err := w.fd.Seed.Gates.Get(ctx, gate.GateID(run.GateIDs[0]))
			if err != nil {
				t.Fatal(err)
			}
			l.check(t, g.Payload)
			bilCheckGate(t, w.fd, run.GateIDs[0], l.gateKind)
		})
	}
}
