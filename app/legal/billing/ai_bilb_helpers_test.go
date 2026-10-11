package billing

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
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/agenttest"
	"github.com/dagi377/counsel/app/legal/agent/feature"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/roles"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/ai/fixture"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/registry"
)

// Shared helpers of the billing tool-use features' end-to-end tests (F-BIL-05, F-BIL-06).
// They drive a Def with agenttest.FromFeatureWith over the app's production handlers, each
// read tool recording its results in the run's Provenance as boot's AgentTools does, and no
// stand-ins: bilbPorts supplies the
// billing read ports (priced time, bills and their balances, trust balances) and an email
// draft store with a2 uuid ids, ahead of the Seed's ports. Every name carries the bilb prefix.

// Synthetic ids the scripts and fixtures use. bilbMatter is visible to the acting user.
const (
	bilbMatter  = agenttest.TrustMatter
	bilbClient  = "00000000-0000-4000-8000-0000000000e1"
	bilbBill    = "00000000-0000-4000-8000-0000000000b7"
	bilbAccount = agenttest.TrustAccount
)

// Synthetic figures, integer cents. bilbBalance is the matter's available trust balance and
// bilbEntryCents the one unbilled entry's amount (90 minutes at bilbRate).
const (
	bilbBalance     int64 = 120_000
	bilbRate        int64 = 40_000
	bilbEntryCents  int64 = 60_000
	bilbBillBalance int64 = 84_750
)

// bilbOtherLawyer is a second synthetic user who holds every gate kind's approver capability.
const bilbOtherLawyer = "user-synthetic-billing-lawyer"

// bilbDecideAt is the fake gate store's clock: a weekday morning in America/Toronto.
var bilbDecideAt = time.Date(2026, 10, 5, 9, 0, 0, 0, time.FixedZone("EDT", -4*60*60))

// bilbDraftID is the id of the n-th email draft a run stores.
func bilbDraftID(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-00000000e%03d", n) }

// bilbPorts is the synthetic billing side of a run: one matter with one client contact, one
// unbilled time entry whose narrative is client text, one overdue bill and a trust balance.
// It also stores the email drafts the run makes.
type bilbPorts struct {
	mu        sync.Mutex
	narrative string
	balance   int64
	emails    []agent.NewDraftEmail
	matters   map[string]string // draft id -> matter id
}

func newBilbPorts(narrative string) *bilbPorts {
	return &bilbPorts{narrative: narrative, balance: bilbBalance, matters: map[string]string{}}
}

// MatterTime implements agent.MatterTime.
func (p *bilbPorts) MatterTime(_ context.Context, matterID string) ([]agent.PricedEntry, error) {
	if matterID != bilbMatter {
		return nil, nil
	}
	return []agent.PricedEntry{{
		Entry: tk.TimeEntry{
			ID: "00000000-0000-4000-8000-0000000000t1", MatterID: bilbMatter, UserID: agenttest.FeatureActingFor,
			WorkDate: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), Minutes: 90, BilledMinutes: 90,
			ActivityCode: "A101", Narrative: p.narrative, Billable: true, State: tk.StateApproved,
		},
		RateCents: bilbRate, AmountCents: bilbEntryCents,
	}}, nil
}

// Get implements agent.BillReader.
func (p *bilbPorts) Get(_ context.Context, id bill.ID) (bill.Bill, error) {
	if string(id) != bilbBill {
		return bill.Bill{}, bill.ErrNotFound
	}
	return bill.Bill{
		ID: id, Number: "B-2026-0031", MatterID: bilbMatter, Status: bill.StatusDelivered, PreparedBy: bilbOtherLawyer,
		IssueDate: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC), DueDate: time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		FeesCents: 75_000, TaxCents: 9_750, TotalCents: bilbBillBalance, BalanceCents: bilbBillBalance,
	}, nil
}

// BalanceCents implements agent.BillBalances.
func (p *bilbPorts) BalanceCents(_ context.Context, id bill.ID) (int64, error) {
	if string(id) != bilbBill {
		return 0, bill.ErrNotFound
	}
	return bilbBillBalance, nil
}

// Available implements agent.TrustAvailable.
func (p *bilbPorts) Available(_ context.Context, account, matterID string) (int64, error) {
	if account != bilbAccount || matterID != bilbMatter {
		return 0, nil
	}
	return p.balance, nil
}

// TrustAccountsOf implements agent.MatterTrustAccounts.
func (p *bilbPorts) TrustAccountsOf(_ context.Context, matterID string) ([]string, error) {
	if matterID != bilbMatter {
		return nil, nil
	}
	return []string{bilbAccount}, nil
}

// MatterContactIDs implements agent.MatterContacts.
func (p *bilbPorts) MatterContactIDs(_ context.Context, matterID string) ([]string, error) {
	if matterID != bilbMatter {
		return nil, nil
	}
	return []string{bilbClient}, nil
}

// CreateEmailDraft implements agent.EmailDrafts.
func (p *bilbPorts) CreateEmailDraft(_ context.Context, d agent.NewDraftEmail) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.emails = append(p.emails, d)
	id := bilbDraftID(len(p.emails))
	p.matters[id] = d.MatterID
	return id, nil
}

// SubjectMatter implements agent.SubjectMatters for the drafts the run made.
func (p *bilbPorts) SubjectMatter(_ context.Context, subjectType, id string) (string, error) {
	if subjectType == roles.SubjectMatter {
		return id, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m, ok := p.matters[id]
	if !ok {
		return "", fmt.Errorf("subject %s: %w", id, agenttools.ErrNotFound)
	}
	return m, nil
}

// drafts returns the email drafts stored so far.
func (p *bilbPorts) drafts() []agent.NewDraftEmail {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.emails)
}

// bilbDepOf appends d's value of type T to vals, when d has one.
func bilbDepOf[T any](vals []any, d agent.Deps) []any {
	if v, err := agent.DepOf[T](d); err == nil {
		return append(vals, v)
	}
	return vals
}

// bilbHandlers is agent.Handlers() with each Build given ports ahead of the Seed's ports and
// each read tool wrapped by the run's Provenance, as boot's AgentTools wraps it.
func bilbHandlers(t *testing.T, ports *bilbPorts) agent.HandlerSet {
	t.Helper()
	app := agent.Handlers()
	var hs []agent.Handler
	for _, name := range app.Names() {
		h, _ := app.For(name)
		build := h.Build
		hs = append(hs, agent.Handler{Name: name, Build: func(d agent.Deps) (registry.Tool, error) {
			vals := []any{ports}
			vals = bilbDepOf[agent.CapabilityCheck](vals, d)
			vals = bilbDepOf[roles.Walls](vals, d)
			vals = bilbDepOf[agenttools.Plans](vals, d)
			vals = bilbDepOf[agenttools.GateOpener](vals, d)
			vals = bilbDepOf[agenttools.RunScopes](vals, d)
			vals = bilbDepOf[agent.ClientMatters](vals, d)
			vals = bilbDepOf[*agenttools.Provenance](vals, d)
			vals = bilbDepOf[*agenttest.SeedStore](vals, d)
			tool, err := build(agent.NewDeps(vals...))
			if err != nil || tool.Kind != registry.ToolRead {
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

// bilbFeature builds def's FeatureDriver over the real handlers and ports, with a gate store
// in which the acting user and bilbOtherLawyer hold every gate kind's approver capability, so
// the preparer rule, not a missing capability, is what stops the acting user.
func bilbFeature(t *testing.T, def feature.Def, ports *bilbPorts) *agenttest.FeatureDriver {
	t.Helper()
	fd := agenttest.FromFeatureWith(t, def, bilbHandlers(t, ports))
	caps := gatefake.Caps{}
	for _, k := range def.GateKinds {
		for _, u := range []string{agenttest.FeatureActingFor, bilbOtherLawyer} {
			caps[u] = append(caps[u], k+".approve")
		}
	}
	fd.Seed.Gates = gatefake.New(func() time.Time { return bilbDecideAt }, caps)
	for _, k := range def.GateKinds {
		fd.Seed.GateKind(k)
	}
	if len(fd.StandIns) != 0 {
		t.Fatalf("stand-ins = %v; want every tool on its real handler", fd.StandIns)
	}
	return fd
}

// bilbCall is one scripted model tool call; input is JSON.
type bilbCall struct {
	name  string
	input string
}

// bilbScript is an inline aifake model for featureID: one tool call per turn, then text.
func bilbScript(featureID string, calls []bilbCall, text string) *aifake.Fake {
	var b strings.Builder
	fmt.Fprintf(&b, "synthetic: true\nfeature: %s\nmatch:\n  last_user_regex: '.'\nturns:\n", featureID)
	for _, c := range calls {
		fmt.Fprintf(&b, "  - reply:\n      tool_calls:\n        - name: %s\n          input: %s\n      stop_reason: tool_use\n", c.name, c.input)
	}
	fmt.Fprintf(&b, "  - reply:\n      text: %q\n      stop_reason: end_turn\n", text)
	return aifake.New(fstest.MapFS{strings.ToLower(featureID) + "/inline/ok.yaml": {Data: []byte(b.String())}})
}

// bilbToolNames lists the tool names of calls.
func bilbToolNames(calls []bilbCall) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		out = append(out, c.name)
	}
	return out
}

// bilbPlan is a report_plan call.
func bilbPlan(step string) bilbCall {
	return bilbCall{agenttools.ToolReportPlan, `{"steps": [{"step_id": "` + step + `", "title": "Draft for lawyer approval"}]}`}
}

// bilbEmail is a draft_email call to the matter's client with body, submitted or not.
func bilbEmail(subject, body string, submit bool) bilbCall {
	in, _ := json.Marshal(map[string]any{
		"matter_id": bilbMatter, "to_contact_ids": []string{bilbClient}, "subject": subject,
		"body_markdown": body, "submit": submit,
	})
	return bilbCall{agent.ToolDraftEmail, string(in)}
}

// bilbFeatureSpec is one spec/ai/features.json feature.
type bilbFeatureSpec struct {
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

// bilbSpec returns id's spec/ai/features.json entry.
func bilbSpec(t *testing.T, id string) bilbFeatureSpec {
	t.Helper()
	var spec struct {
		Features []bilbFeatureSpec `json:"features"`
	}
	b, err := os.ReadFile("../../../spec/ai/features.json")
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	i := slices.IndexFunc(spec.Features, func(f bilbFeatureSpec) bool { return f.ID == id })
	if i < 0 {
		t.Fatalf("%s not in spec/ai/features.json", id)
	}
	return spec.Features[i]
}

// bilbCheckDefMatchesSpec fails t unless d matches its spec/ai/features.json entry (the spec
// sync check for this id) and a feature Set takes it once.
func bilbCheckDefMatchesSpec(t *testing.T, d feature.Def) {
	t.Helper()
	sf := bilbSpec(t, d.ID)
	sorted := func(s []string) string { s = slices.Clone(s); slices.Sort(s); return strings.Join(s, ",") }
	kind := d.Kind
	if kind == "" {
		kind = ai.KindToolUse
	}
	tests := []struct {
		field     string
		got, want string
	}{
		{"name", d.Name, sf.Name},
		{"area", d.Area, "billing"},
		{"source", string(d.Source), sf.Source},
		{"trigger", d.Trigger, sf.Trigger},
		{"on request", sorted(d.Actions), sorted(sf.OnRequest)},
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
}

// bilbCheckPrompt fails t unless d's prompt passes lint, starts with the feature preamble and
// the shared rules, and names the task, the expected output and every gate kind.
func bilbCheckPrompt(t *testing.T, d feature.Def) {
	t.Helper()
	sf := bilbSpec(t, d.ID)
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
	if !strings.HasPrefix(spec.System, feature.Preamble(d.ID)+feature.Rules()) {
		t.Fatalf("system prompt does not start with the preamble and the shared rules")
	}
	for _, w := range append([]string{"shared rules", sf.Name, sf.Outputs, "report_plan", "integer cents"}, d.GateKinds...) {
		if !strings.Contains(spec.System, w) {
			t.Errorf("system prompt lacks %q", w)
		}
	}
}

// bilbDecide approves gate id as by, with a fresh step-up and the payload hash shown.
func bilbDecide(ctx context.Context, gates *gatefake.Fake, id string, by registry.Principal) error {
	g, err := gates.Get(ctx, gate.GateID(id))
	if err != nil {
		return err
	}
	return gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{
		Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash, MFAVerifiedAt: bilbDecideAt.Add(-time.Minute),
	}, by)
}

// bilbStatus is the HTTP status the API returns for a gate decision error.
func bilbStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if p := gate.Problem(err); p != nil {
		return p.Status
	}
	return http.StatusInternalServerError
}

// bilbCheckGate fails t unless gate id is pending, of kind, prepared for the acting user, and
// neither an agent principal (403) nor the acting user (ErrSelfApproval, 403) can decide it,
// while another lawyer can.
func bilbCheckGate(t *testing.T, fd *agenttest.FeatureDriver, id, kind string) {
	t.Helper()
	ctx := context.Background()
	g, err := fd.Seed.Gates.Get(ctx, gate.GateID(id))
	if err != nil {
		t.Fatal(err)
	}
	// rule: ARC-I-039
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
		{"agent principal", registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-" + id, ActingFor: agenttest.FeatureActingFor}, gate.ErrAgentApprover, http.StatusForbidden},
		// rule: ARC-I-039
		{"acting user decides own gate", registry.Principal{Kind: registry.PrincipalUser, UserID: agenttest.FeatureActingFor}, gate.ErrSelfApproval, http.StatusForbidden},
		{"another lawyer", registry.Principal{Kind: registry.PrincipalUser, UserID: bilbOtherLawyer}, nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bilbDecide(ctx, fd.Seed.Gates, id, tt.by)
			if !errors.Is(err, tt.wantErr) || bilbStatus(err) != tt.wantStatus {
				t.Fatalf("decide = %v (%d), want %v (%d)", err, bilbStatus(err), tt.wantErr, tt.wantStatus)
			}
		})
	}
}

// bilbAbsent reports an error when text reached any stored email draft.
func bilbAbsent(ports *bilbPorts, text string) error {
	for i, d := range ports.drafts() {
		if strings.Contains(d.Subject+d.BodyMarkdown, text) {
			return fmt.Errorf("draft %d carries the injected text", i)
		}
	}
	return nil
}

// bilbForbidden are the calls a prepare-only feature file must never make: it drafts and
// opens a gate, and a person authorizes, approves, delivers or records the execution.
var bilbForbidden = []string{"Authorize", "Approve", "Deliver", "RecordExecution"}

// bilbAllowedImports are the only non-standard packages a billing feature file may import.
var bilbAllowedImports = []string{
	"github.com/dagi377/counsel/app/legal/agent",
	"github.com/dagi377/counsel/app/legal/agent/feature",
	"github.com/dagi377/counsel/app/legal/agent/trigger",
	"github.com/dagi377/counsel/kernel/agenttools",
	"github.com/dagi377/counsel/kernel/ai",
	"github.com/dagi377/counsel/kernel/registry",
}

// bilbCheckPrepareOnly parses file and fails t if it imports a package outside
// bilbAllowedImports (so no import path reaches the bill, trust or payment modules that
// authorize, approve, deliver or record execution) or calls any of bilbForbidden.
func bilbCheckPrepareOnly(t *testing.T, file string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, im := range f.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		if strings.Contains(p, ".") && !slices.Contains(bilbAllowedImports, p) {
			t.Errorf("%s imports %s", file, p)
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			name := ""
			switch fn := c.Fun.(type) {
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			case *ast.Ident:
				name = fn.Name
			}
			if slices.Contains(bilbForbidden, name) {
				t.Errorf("%s calls %s", file, name)
			}
		}
		return true
	})
}

// bilbFixtureCase is one feature's checked-in fixture run for bilbCheckFixtures.
type bilbFixtureCase struct {
	def      feature.Def
	dir      string   // fixture dir under testdata/ai
	trigger  string   // the trigger prompt the fixtures match
	inject   string   // the instruction the injection variant's time narrative carries
	okTools  []string // the ok variant's tool calls, in order
	gateKind string
}

// bilbCheckFixtures runs c.def over the real handlers against its checked-in variants: ok
// opens one pending gate after report_plan; error_429 writes nothing and queues a retry that
// then succeeds; timeout fails with ai.ErrTimeout; injection keeps the injected narrative
// quoted, keeps it out of the draft, and its extra trust withdrawal is refused.
func bilbCheckFixtures(t *testing.T, c bilbFixtureCase) {
	t.Helper()
	// The ok fixture pins the Def's prompt hash, so a prompt change is noticed.
	if err := feature.LintFixtureHash(c.def, os.DirFS("../../..")); err != nil {
		t.Fatal(err)
	}
	ok, err := fixture.Load(os.DirFS(agenttest.Root(t)), c.dir+"/ok.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if ok.Match.PromptHash == "" {
		t.Fatal("ok fixture has no match.prompt_hash")
	}
	injTools := append(slices.Clone(c.okTools), agent.ToolPrepareTrustWithdrawal)
	tests := []struct {
		name      string
		variant   string
		inject    bool
		wantTools []string
		wantGates int
		wantRetry bool
		wantErr   error
	}{
		{name: "ok", variant: fixture.VariantOK, wantTools: c.okTools, wantGates: 1},
		// rule: AI-R-003
		{name: "error_429", variant: fixture.VariantError429, wantRetry: true},
		{name: "timeout", variant: fixture.VariantTimeout, wantErr: ai.ErrTimeout},
		// rule: ARC-I-038
		{name: "injection", variant: fixture.VariantInjection, inject: true, wantTools: injTools, wantGates: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			narrative := "Drafted reply to opposing counsel"
			if tc.inject {
				narrative += ". " + c.inject
			}
			ports := newBilbPorts(narrative)
			fd := bilbFeature(t, c.def, ports)
			model := agenttest.Model(t, c.dir, tc.variant)
			runID := "run-" + strings.ToLower(c.def.ID) + "-" + tc.name
			run := fd.Fire(ctx, model, runID, c.trigger)
			if !errors.Is(run.Err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", run.Err, tc.wantErr)
			}
			if run.RetryQueued != tc.wantRetry {
				t.Fatalf("retry queued = %v, want %v", run.RetryQueued, tc.wantRetry)
			}
			if !slices.Equal(run.Tools, tc.wantTools) {
				t.Fatalf("tools = %v, want %v", run.Tools, tc.wantTools)
			}
			// rule: AI-R-003
			// A failed or rate-limited call leaves no partial write.
			if len(run.GateIDs) != tc.wantGates || (tc.wantGates == 0 && len(ports.drafts()) != 0) {
				t.Fatalf("gates = %v, drafts %d; want %d gates", run.GateIDs, len(ports.drafts()), tc.wantGates)
			}
			if tc.wantRetry {
				// The queued retry reruns the same run id and now reaches the script.
				run = fd.Fire(ctx, model, runID, c.trigger)
				if run.Err != nil || run.RetryQueued || len(run.GateIDs) != 1 {
					t.Fatalf("retry: err %v queued %v gates %v", run.Err, run.RetryQueued, run.GateIDs)
				}
			}
			if tc.wantGates == 0 && !tc.wantRetry {
				return
			}
			if tc.inject {
				if err := agenttest.QuotedOnly(run.Run, c.inject); err != nil {
					t.Fatal(err)
				}
				// rule: ARC-I-036
				if err := fd.RefusedInjectedWrite(ctx, run.Run, agenttools.ErrMissingCapability); err != nil {
					t.Fatal(err)
				}
				if err := bilbAbsent(ports, c.inject); err != nil {
					t.Fatal(err)
				}
			} else if len(run.Refused) != 0 {
				t.Fatalf("refused calls: %v", run.Refused)
			}
			if len(run.StandIns) != 0 {
				t.Fatalf("stand-ins called: %v", run.StandIns)
			}
			// rule: ARC-I-039
			if run.PlanAt < 0 || run.PlanAt > run.FirstWrite {
				t.Fatalf("report_plan at %d, first write at %d: plan must come first", run.PlanAt, run.FirstWrite)
			}
			bilbCheckGate(t, fd, run.GateIDs[len(run.GateIDs)-1], c.gateKind)
		})
	}
}
