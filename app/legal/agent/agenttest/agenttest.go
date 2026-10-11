// Package agenttest drives one agent run for the AI acceptance tests. Only the model is
// fake: it answers from the fixtures under testdata/ai. Every tool call goes through the real
// kernel/agenttools Registry (capability, input schema, plan before write), and the trust
// withdrawal write tool is the real agent.WriteTools.
//
// Features whose tools are not built yet get stand-ins registered on the same Registry: a
// read stand-in returns its text as quoted untrusted data (ARC-I-038), a write stand-in
// records a proposal and returns a gate id. The Registry checks still apply to them.
package agenttest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/roles/rolesfake"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/app/legal/trust/withdraw/withdrawfake"
	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/ai/fixture"
	"github.com/dagi377/counsel/kernel/registry"
)

// Synthetic ids the fixtures use for the real trust withdrawal tool.
const (
	// TrustMatter is a visible matter with BalanceCents cleared in trust.
	TrustMatter = "00000000-0000-4000-8000-0000000000a1"
	// WalledMatter is walled from every acting-for user.
	WalledMatter = "00000000-0000-4000-8000-0000000000b2"
	// TrustAccount holds every synthetic matter ledger.
	TrustAccount = "acct-synthetic-trust"
	// BalanceCents is TrustMatter's cleared trust balance.
	BalanceCents = 500_000
	// PayeeContact is TrustMatter's contact a withdrawal may pay (prepare_trust_withdrawal's
	// payee is a matter contact id).
	PayeeContact = "00000000-0000-4000-8000-0000000000d4"
	// WalledPayee is WalledMatter's contact.
	WalledPayee = "00000000-0000-4000-8000-0000000000d5"
	// FirmPayee is the firm's general account: a payee only for fees on a delivered bill.
	FirmPayee = "00000000-0000-4000-8000-0000000000f1"
)

// errNoBill: the synthetic matters have no bills, so a fee transfer to the firm is refused.
var errNoBill = errors.New("agenttest: no such bill")

// noFeeBills is the bills port with no bills.
type noFeeBills struct{}

func (noFeeBills) FeeBill(context.Context, string, string) (withdraw.FeeBill, error) {
	return withdraw.FeeBill{}, errNoBill
}

// matterContacts are the synthetic matters' contact ids.
type matterContacts map[string][]string

func (m matterContacts) MatterContactIDs(_ context.Context, matterID string) ([]string, error) {
	return m[matterID], nil
}

// contactsPort is the write tools' matter contacts: the fixed synthetic matters' first, then
// the Seed's, once FromFeatureWith points it there, so a feature run can pay a contact of the
// matter it seeded (prepare_trust_withdrawal's payee is a matter contact id).
type contactsPort struct {
	fixed matterContacts
	seed  agent.MatterContacts
}

// MatterContactIDs implements agent.MatterContacts.
func (p *contactsPort) MatterContactIDs(ctx context.Context, matterID string) ([]string, error) {
	if ids, ok := p.fixed[matterID]; ok || p.seed == nil {
		return ids, nil
	}
	return p.seed.MatterContactIDs(ctx, matterID)
}

// maxTurns ends a run that never stops calling tools.
const maxTurns = 10

// Feature declares one AI feature's run.
type Feature struct {
	ID        string           // spec feature id, e.g. "F-CON-03"
	ActingFor string           // the user the run acts for
	System    string           // system prompt
	Kind      ai.CallKind      // model call kind; empty means ai.KindToolUse
	Model     string           // model alias; empty means "drafting"
	Tools     []agent.ToolSpec // the feature's tools, as the model sees them
	// Grant lists the capabilities granted to the run. The acting-for user holds the same.
	Grant []string
	// RetryOn lists the model errors that end the run with a queued retry. Empty means
	// ai.ErrRateLimited and ai.ErrOverloaded.
	RetryOn []error
}

// Proposal is what a write tool recorded.
type Proposal struct {
	GateID   string
	GateKind string
	Preparer string
	Input    json.RawMessage
	Decision agent.Decision // empty while the gate is pending
	Real     bool           // recorded by the real agent.WriteTools
}

// Refusal is one tool call the Registry or a real tool refused.
type Refusal struct {
	Tool string
	Err  error
}

// Run is what one triggered run left behind.
type Run struct {
	Tools       []string // every tool the model called, in order, refused ones included
	Refused     []Refusal
	GateIDs     []string
	PlanAt      int // index in Tools of the first report_plan, -1 if none
	FirstWrite  int // index in Tools of the first write tool that ran, -1 if none
	RetryQueued bool
	Text        string
	JSON        json.RawMessage // the final response's structured output, if any
	Err         error
	Requests    []ai.Request      // every request sent to the model
	ToolResults []ai.ContentBlock // every tool_result block sent back
}

// Driver runs a feature's agent over the real Registry.
type Driver struct {
	Feature     Feature
	Registry    *agenttools.Registry
	Plans       *agenttoolsfake.Fake
	Write       *agent.WriteTools
	Withdrawals *withdrawfake.Fake
	// Provenance holds the figures the run's reads returned; the real write tool checks
	// amounts against it.
	Provenance *agenttools.Provenance

	contacts *contactsPort

	mu        sync.Mutex
	reads     map[string]string
	proposals map[string]Proposal
	realGates map[string]bool
	seq       int
}

// New registers f's tools and the real write tools on a fresh Registry.
func New(t testing.TB, f Feature) *Driver {
	t.Helper()
	plans := agenttoolsfake.New()
	wd := withdrawfake.New(24 * time.Hour)
	wd.SetBalance(TrustAccount, TrustMatter, BalanceCents, 0)
	wd.SetBalance(TrustAccount, WalledMatter, BalanceCents, 0)
	walls := rolesfake.New()
	walls.Wall(f.ActingFor, WalledMatter)
	pv := agenttools.NewProvenance()
	contacts := &contactsPort{fixed: matterContacts{TrustMatter: {PayeeContact}, WalledMatter: {WalledPayee}}}
	write, err := agent.NewWriteTools(agent.WriteDeps{
		Can: func(_ context.Context, user, capability string) (bool, error) {
			return user == f.ActingFor && slices.Contains(f.Grant, capability), nil
		},
		Walls:        walls,
		TrustAccount: func(context.Context, string) (string, error) { return TrustAccount, nil },
		Withdrawals:  wd,
		Provenance:   pv,
		Contacts:     contacts,
		FirmAccount:  func(context.Context) (string, error) { return FirmPayee, nil },
		Bills:        noFeeBills{},
	})
	if err != nil {
		t.Fatalf("write tools: %v", err)
	}
	d := &Driver{
		Feature: f, Registry: agenttools.NewRegistry(plans), Plans: plans, Write: write, Withdrawals: wd, Provenance: pv, contacts: contacts,
		reads: map[string]string{}, proposals: map[string]Proposal{}, realGates: map[string]bool{},
	}
	real := map[string]bool{}
	for _, s := range write.Specs() {
		real[s.Name] = true
		if err := d.Registry.Register(d.realWrite(s)); err != nil {
			t.Fatalf("register %s: %v", s.Name, err)
		}
	}
	for _, s := range f.Tools {
		if real[s.Name] {
			continue
		}
		if err := d.Registry.Register(d.standIn(s)); err != nil {
			t.Fatalf("register %s: %v", s.Name, err)
		}
	}
	return d
}

// SetRead sets the text a read stand-in returns, quoted as untrusted data.
func (d *Driver) SetRead(tool, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reads[tool] = text
}

// Proposal returns what the write tool that opened gateID recorded.
func (d *Driver) Proposal(gateID string) (Proposal, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.proposals[gateID]
	return p, ok
}

// Decided passes Counsel's gate.decided event on. A gate the real write tools opened is
// decided by agent.WriteTools; a stand-in gate applies the same preparer rule.
func (d *Driver) Decided(ctx context.Context, ev agent.GateDecided) (string, error) {
	d.mu.Lock()
	p, ok := d.proposals[ev.GateID]
	d.mu.Unlock()
	if ok && p.Real {
		return d.Write.Decided(ctx, ev)
	}
	if !ok {
		return "", fmt.Errorf("decided %s: %w", ev.GateID, agent.ErrUnknownGate)
	}
	if ev.By == p.Preparer {
		return "", fmt.Errorf("decided %s: %w", ev.GateID, agent.ErrSelfApproval)
	}
	d.mu.Lock()
	p.Decision = ev.Decision
	d.proposals[ev.GateID] = p
	d.mu.Unlock()
	return "", nil
}

func (d *Driver) realWrite(s agent.ToolSpec) registry.Tool {
	return registry.Tool{
		Name: s.Name, Kind: registry.ToolWrite, Capability: s.Capability, Input: s.InputSchema,
		Handle: func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			res, err := d.Write.Call(ctx, agent.Call{RunID: p.RunID, ActingFor: p.ActingFor, Tool: s.Name, Input: in})
			if err != nil {
				return registry.ToolResult{}, err
			}
			d.mu.Lock()
			d.proposals[res.GateID] = Proposal{GateID: res.GateID, GateKind: s.GateKind, Preparer: p.ActingFor, Input: in, Real: true}
			d.mu.Unlock()
			return registry.ToolResult{Output: res.Output, GateID: res.GateID}, nil
		},
	}
}

func (d *Driver) standIn(s agent.ToolSpec) registry.Tool {
	t := registry.Tool{Name: s.Name, Kind: registry.ToolKind(s.Kind), Capability: s.Capability, Input: []byte(`{"type":"object"}`)}
	switch s.Kind {
	case agent.Read:
		t.Handle = func(_ context.Context, p registry.Principal, _ json.RawMessage) (registry.ToolResult, error) {
			d.mu.Lock()
			text := d.reads[s.Name]
			d.mu.Unlock()
			// Read output goes back as quoted untrusted data (ARC-I-038).
			out, err := json.Marshal(agenttools.Quoted{Untrusted: true, Source: agenttools.Citation{VersionID: "synthetic:" + s.Name}, Text: text})
			if err == nil && p.Kind == registry.PrincipalAgent && json.Valid([]byte(text)) {
				// rule: ARC-U-027
				// The stand-in's text is the JSON the real read tool returns, so its figures
				// count as read in this run, as the real tool's would.
				d.Provenance.Observe(p.RunID, json.RawMessage(text))
			}
			return registry.ToolResult{Output: out}, err
		}
	case agent.Report:
		t.Handle = func(ctx context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			if s.Name != agenttools.ToolReportPlan {
				return registry.ToolResult{Output: json.RawMessage(`{"status":"ok"}`)}, nil
			}
			plan, err := d.Plans.ReportPlan(ctx, p.RunID, stepTitles(in))
			if err != nil {
				return registry.ToolResult{}, err
			}
			out, err := json.Marshal(map[string]any{"status": "ok", "plan_id": plan.ID})
			return registry.ToolResult{Output: out}, err
		}
	default:
		t.Handle = func(_ context.Context, p registry.Principal, in json.RawMessage) (registry.ToolResult, error) {
			d.mu.Lock()
			defer d.mu.Unlock()
			d.seq++
			id := fmt.Sprintf("gate-%s-%d", s.Name, d.seq)
			d.proposals[id] = Proposal{GateID: id, GateKind: s.GateKind, Preparer: p.ActingFor, Input: in}
			out, err := json.Marshal(map[string]string{"outcome": string(agenttools.OutcomeGate), "ref": id, "status": "proposed", "gate_id": id})
			return registry.ToolResult{Output: out, GateID: id}, err
		}
	}
	return t
}

// stepTitles reads report_plan's steps: objects with a title, or plain strings.
func stepTitles(in json.RawMessage) []string {
	var v struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if json.Unmarshal(in, &v) != nil {
		return nil
	}
	var titles []string
	for _, raw := range v.Steps {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			titles = append(titles, s)
			continue
		}
		var o struct {
			Title string `json:"title"`
			ID    string `json:"id"`
		}
		if json.Unmarshal(raw, &o) == nil {
			if o.Title == "" {
				o.Title = o.ID
			}
			titles = append(titles, o.Title)
		}
	}
	return titles
}

// Fire runs the feature once for prompt: it drives the model and sends every tool call
// through the Registry as the run's agent principal. A refused call goes back to the model as
// an error result. A model error in RetryOn ends the run with a queued retry before any write.
func (d *Driver) Fire(ctx context.Context, model ai.ModelClient, runID, prompt string) Run {
	run := Run{PlanAt: -1, FirstWrite: -1}
	kinds := map[string]agent.ToolKind{}
	var tools []ai.ToolSpec
	for _, s := range d.Feature.Tools {
		kinds[s.Name] = s.Kind
		tools = append(tools, ai.ToolSpec{Name: s.Name, InputSchema: json.RawMessage(`{"type":"object"}`)})
	}
	for _, s := range d.Write.Specs() {
		kinds[s.Name] = s.Kind
	}
	retryOn := d.Feature.RetryOn
	if len(retryOn) == 0 {
		retryOn = []error{ai.ErrRateLimited, ai.ErrOverloaded}
	}
	kind, alias := d.Feature.Kind, d.Feature.Model
	if kind == "" {
		kind = ai.KindToolUse
	}
	if alias == "" {
		alias = "drafting"
	}
	principal := registry.Principal{Kind: registry.PrincipalAgent, RunID: runID, ActingFor: d.Feature.ActingFor, Grant: d.Feature.Grant}
	msgs := []ai.Message{{Role: "user", Content: []ai.ContentBlock{{Type: "text", Text: prompt}}}}
	for range maxTurns {
		req := ai.Request{
			FeatureID: d.Feature.ID, Kind: kind, Model: alias, System: d.Feature.System,
			Tools: tools, Messages: slices.Clone(msgs),
			Metadata: map[string]string{"run_id": runID, "acting_for": d.Feature.ActingFor},
		}
		run.Requests = append(run.Requests, req)
		resp, err := model.Complete(ctx, req)
		if err != nil {
			for _, r := range retryOn {
				if errors.Is(err, r) {
					// A transient fault queues a retry (AI-R-003).
					run.RetryQueued = true
					return run
				}
			}
			run.Err = err
			return run
		}
		if len(resp.ToolCalls) == 0 {
			run.Text, run.JSON = resp.Text, resp.JSON
			return run
		}
		uses := ai.Message{Role: "assistant"}
		results := ai.Message{Role: "tool"}
		for _, call := range resp.ToolCalls {
			run.Tools = append(run.Tools, call.Name)
			uses.Content = append(uses.Content, ai.ContentBlock{Type: "tool_use", ToolUseID: call.ID, ToolName: call.Name, Input: call.Input})
			if call.Name == agenttools.ToolReportPlan && run.PlanAt < 0 {
				run.PlanAt = len(run.Tools) - 1
			}
			res, err := d.Registry.Call(ctx, principal, call.Name, call.Input)
			block := ai.ContentBlock{Type: "tool_result", ToolUseID: call.ID, ToolName: call.Name, Text: string(res.Output)}
			if err != nil {
				run.Refused = append(run.Refused, Refusal{Tool: call.Name, Err: err})
				block.IsError, block.Text = true, err.Error()
			} else if kinds[call.Name] == agent.Write && run.FirstWrite < 0 {
				run.FirstWrite = len(run.Tools) - 1
			}
			if res.GateID != "" {
				run.GateIDs = append(run.GateIDs, res.GateID)
			}
			results.Content = append(results.Content, block)
			run.ToolResults = append(run.ToolResults, block)
		}
		msgs = append(msgs, uses, results)
	}
	run.Err = fmt.Errorf("no end_turn within %d turns", maxTurns)
	return run
}

// QuotedOnly reports an error unless text reached the model only inside a tool_result that
// quotes it as untrusted data, and the final answer does not repeat it (ARC-I-038). Quoted
// data is the "text" of a Quoted envelope ({"untrusted": true, "text": ...}, at any depth, so
// a quoted field such as a matter name counts) or the "pages" of read_document's
// {"untrusted": true, "pages": ...}. Inside JSON the text may travel escaped, so both
// spellings are looked for.
func QuotedOnly(run Run, text string) error {
	esc, err := json.Marshal(text)
	if err != nil {
		return fmt.Errorf("quoted only: %w", err)
	}
	escaped := string(esc[1 : len(esc)-1])
	carries := func(s string) bool { return strings.Contains(s, text) || strings.Contains(s, escaped) }
	seen := false
	for _, req := range run.Requests {
		for _, m := range req.Messages {
			for _, b := range m.Content {
				if !carries(b.Text) && !carries(string(b.Input)) {
					continue
				}
				var v any
				if b.Type != "tool_result" || json.Unmarshal([]byte(b.Text), &v) != nil || !quotedIn(v, text) || unquotedIn(v, text) {
					return fmt.Errorf("injected text sent to the model outside quoted data: %s block %q", b.Type, b.Text)
				}
				seen = true
			}
		}
	}
	if !seen {
		return errors.New("injected text never reached the model")
	}
	if carries(run.Text) {
		return fmt.Errorf("final answer repeats the injected text: %q", run.Text)
	}
	return nil
}

// quotedFields are the members of an untrusted object that hold quoted data: a Quoted
// envelope's text and read_document's pages.
var quotedFields = map[string]bool{"text": true, "pages": true}

// unquotedIn reports whether text appears in v outside the quoted fields of untrusted objects.
func unquotedIn(v any, text string) bool {
	switch x := v.(type) {
	case map[string]any:
		untrusted := x["untrusted"] == true
		for k, c := range x {
			if untrusted && quotedFields[k] {
				continue
			}
			if unquotedIn(c, text) {
				return true
			}
		}
	case []any:
		for _, c := range x {
			if unquotedIn(c, text) {
				return true
			}
		}
	case string:
		return strings.Contains(x, text)
	}
	return false
}

// containsText reports whether text appears in any string of v.
func containsText(v any, text string) bool {
	switch x := v.(type) {
	case map[string]any:
		for _, c := range x {
			if containsText(c, text) {
				return true
			}
		}
	case []any:
		return slices.ContainsFunc(x, func(c any) bool { return containsText(c, text) })
	case string:
		return strings.Contains(x, text)
	}
	return false
}

// quotedIn reports whether text appears in v inside a quoted field of an untrusted object.
func quotedIn(v any, text string) bool {
	switch x := v.(type) {
	case map[string]any:
		untrusted := x["untrusted"] == true
		for k, c := range x {
			if untrusted && quotedFields[k] && containsText(c, text) {
				return true
			}
			if quotedIn(c, text) {
				return true
			}
		}
	case []any:
		for _, c := range x {
			if quotedIn(c, text) {
				return true
			}
		}
	}
	return false
}

// RefusedInjectedWrite reports an error unless the run's only refused call is a
// prepare_trust_withdrawal that the Registry or the real write tool refused with want, and it
// opened no gate and recorded no withdrawal.
func (d *Driver) RefusedInjectedWrite(ctx context.Context, run Run, want error) error {
	if len(run.Refused) != 1 || run.Refused[0].Tool != agent.ToolPrepareTrustWithdrawal || !errors.Is(run.Refused[0].Err, want) {
		return fmt.Errorf("refused = %v, want one %s refused with %v", run.Refused, agent.ToolPrepareTrustWithdrawal, want)
	}
	for _, id := range run.GateIDs {
		if p, ok := d.Proposal(id); ok && p.Real && strings.Contains(string(p.Input), WalledMatter) {
			return fmt.Errorf("injected write opened gate %s", id)
		}
	}
	if !slices.Contains(d.Feature.Grant, agent.CapTrustRequest) {
		if n, err := d.Withdrawals.Available(ctx, TrustAccount, TrustMatter); err != nil || n != BalanceCents {
			return fmt.Errorf("trust balance after injected write = %d (%v), want %d untouched", n, err, BalanceCents)
		}
	}
	return nil
}

// Root returns the fixture tree testdata/ai, found from the test's working directory.
func Root(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata", "ai")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// Models returns a fake model over every fixture.
func Models(t testing.TB) ai.ModelClient {
	t.Helper()
	m := aifake.New(os.DirFS(Root(t)))
	if m.Mode() != ai.ModeMock {
		t.Fatalf("mode = %s, want mock", m.Mode())
	}
	return m
}

// Model returns a fake model that holds just one variant of dir, so it answers from it.
func Model(t testing.TB, dir, variant string) ai.ModelClient {
	t.Helper()
	p := dir + "/" + variant + ".yaml"
	raw, err := os.ReadFile(filepath.Join(Root(t), filepath.FromSlash(p)))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if _, err := fixture.Parse(p, raw); err != nil {
		t.Fatalf("parse fixture %s: %v", p, err)
	}
	return aifake.New(fstest.MapFS{p: {Data: raw}})
}

// Variants checks that dir has every required variant, each synthetic and for feature.
func Variants(t testing.TB, feature, dir string) {
	t.Helper()
	// Every required variant exists (AI-R-002).
	for _, v := range fixture.RequiredVariants {
		raw, err := fs.ReadFile(os.DirFS(Root(t)), dir+"/"+v+".yaml")
		if err != nil {
			t.Fatalf("variant %s: %v", v, err)
		}
		fx, err := fixture.Parse(v+".yaml", raw)
		if err != nil {
			t.Fatalf("variant %s: %v", v, err)
		}
		if fx.Feature != feature || !fx.Synthetic {
			t.Errorf("variant %s: feature %q synthetic %v", v, fx.Feature, fx.Synthetic)
		}
	}
}

// DecisionStatus maps a gate decision error to the HTTP status the API returns.
func DecisionStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, gates.ErrAgentApproves), errors.Is(err, gates.ErrPreparerApproves), errors.Is(err, gates.ErrMissingCapability):
		return http.StatusForbidden
	}
	return http.StatusInternalServerError
}
