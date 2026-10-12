package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/agenttools/agenttoolsfake"
	agentrun "github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai/aifake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

const (
	runLawyer    = "00000000-0000-0000-0000-00000000a001" // the acting user of a screen action
	runDesignate = "00000000-0000-0000-0000-00000000a002" // the designated lawyer of F-DOC-09
	runMatter    = "00000000-0000-0000-0000-00000000b001"
	runDraft     = "00000000-0000-0000-0000-00000000c001"
)

// runFixture scripts one run of feature: a plan, a draft on runMatter, its submission for
// gateKind, then the end of the turn. The ids are synthetic.
func runFixture(feature, gateKind string) string {
	return fmt.Sprintf(`synthetic: true
feature: %s
match:
  last_user_regex: 'feature: %s'
turns:
  - reply:
      tool_calls:
        - name: report_plan
          input: {steps: [{step_id: draft, title: "Draft for review"}, {step_id: submit, title: "Submit for approval"}]}
      stop_reason: tool_use
      usage: {input_tokens: 100, output_tokens: 20}
  - reply:
      tool_calls:
        - name: draft_document
          input: {matter_id: "%s", title: "Synthetic draft", body_markdown: "Synthetic body text."}
      stop_reason: tool_use
      usage: {input_tokens: 120, output_tokens: 30}
  - reply:
      tool_calls:
        - name: submit_for_approval
          input: {subject_type: "draft", subject_id: "%s", gate_kind: "%s"}
      stop_reason: tool_use
      usage: {input_tokens: 140, output_tokens: 20}
  - reply:
      text: "Drafted one document for your review."
      stop_reason: end_turn
      usage: {input_tokens: 160, output_tokens: 10}
`, feature, feature, runMatter, runDraft, gateKind)
}

// capJobs records every job inserted and gives each the next job id.
type capJobs struct {
	mu   sync.Mutex
	args []trigger.RunArgs
}

func (c *capJobs) Insert(_ context.Context, a river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := a.(trigger.RunArgs); ok {
		c.args = append(c.args, r)
	}
	return &rivertype.JobInsertResult{Job: &rivertype.JobRow{ID: int64(len(c.args))}}, nil
}

// runAccess lets every user read every record and builds agent principals with the full grant.
type runAccess struct{}

func (runAccess) Can(context.Context, registry.Principal, string, registry.Subject) (bool, error) {
	return true, nil
}

func (runAccess) AgentPrincipal(_ context.Context, run identity.RunID, actingFor identity.UserID, grant []string) (registry.Principal, error) {
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: string(run), ActingFor: string(actingFor), Grant: slices.Clone(grant)}, nil
}

// memRecords keeps run records in memory, with run.PGRecords' state rules.
type memRecords struct {
	mu   sync.Mutex
	recs map[string]agentrun.Record
}

func (m *memRecords) Create(_ context.Context, r agentrun.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.recs[r.ID]; ok {
		return fmt.Errorf("create %s: duplicate", r.ID)
	}
	m.recs[r.ID] = r
	return nil
}

func (m *memRecords) Start(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[id]
	if !ok || r.State != agentrun.StateQueued {
		return agentrun.ErrRecordNotFound
	}
	r.State, r.StartedAt = agentrun.StateRunning, &at
	m.recs[id] = r
	return nil
}

func (m *memRecords) Finish(_ context.Context, id string, at time.Time, f agentrun.Finish) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[id]
	if !ok || (r.State != agentrun.StateQueued && r.State != agentrun.StateRunning) {
		return agentrun.ErrRecordNotFound
	}
	r.State, r.ErrorCode, r.EndedAt = f.State, f.ErrorCode, &at
	r.InTokens += f.InTokens
	r.OutTokens += f.OutTokens
	m.recs[id] = r
	return nil
}

func (m *memRecords) Get(_ context.Context, id string) (agentrun.Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.recs[id]
	if !ok {
		return agentrun.Record{}, agentrun.ErrRecordNotFound
	}
	return r, nil
}

// memDrafts stores document drafts in memory, one per run and idempotency key.
type memDrafts struct {
	mu     sync.Mutex
	drafts []agent.NewDraftDocument
}

func (m *memDrafts) CreateDraft(_ context.Context, d agent.NewDraftDocument) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.drafts {
		if x.RunID == d.RunID && x.IdempotencyKey == d.IdempotencyKey {
			return runDraft, nil
		}
	}
	m.drafts = append(m.drafts, d)
	return runDraft, nil
}

type oneMatterSubjects struct{}

func (oneMatterSubjects) SubjectMatter(context.Context, string, string) (string, error) {
	return runMatter, nil
}

// draftRecord is the drafted subject's record the payload builders read (in production
// PGSubjectRecords reads it from Postgres): it holds every field the two gate kinds of this
// test require, and each builder takes only its own kind's fields.
type draftRecord struct{}

func (draftRecord) SubjectRecord(_ context.Context, _, subjectID string) (map[string]any, error) {
	if subjectID != runDraft {
		return nil, nil
	}
	return map[string]any{
		"matter_id": runMatter, "to_stage": "closing",
		"template_id": "00000000-0000-4000-8000-00000000d001", "version": 1,
	}, nil
}

type zeroTrust struct{}

func (zeroTrust) MatterTrustBalance(context.Context, string) (int64, error) { return 0, nil }

type noWalls struct{}

func (noWalls) Visible(context.Context, string, string) (bool, error) { return true, nil }

type oneDesignate struct{}

func (oneDesignate) DesignatedLawyer(context.Context, string) (string, error) {
	return runDesignate, nil
}

// runWiring is the agent wiring over in-memory stores: the Postgres stores the run touches
// (drafts, plans, run records) and the identity checks are replaced, every other part is the
// production graph (model, tool registry, gate opener over the kernel gate engine, worker).
func runWiring(t *testing.T, records *memRecords, drafts *memDrafts) *AgentWiring {
	t.Helper()
	builders, err := agent.SpecPayloadBuilders(draftRecord{}, zeroTrust{})
	if err != nil {
		t.Fatal(err)
	}
	return &AgentWiring{
		Ports: AgentPorts{
			Can:             func(context.Context, string, string) (bool, error) { return true, nil },
			Walls:           noWalls{},
			Plans:           agenttoolsfake.New(),
			DocumentDrafts:  drafts,
			SubjectMatters:  oneMatterSubjects{},
			PayloadBuilders: builders,
			TrustBalances:   zeroTrust{},
		},
		Designated: oneDesignate{},
		Actions: &trigger.Actions{Access: runAccess{}, Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		}},
		RunRecords: records,
	}
}

// TestFeatureRunEndToEnd builds the production graph on the mock model and runs one write
// feature from a screen action and one from a schedule slot through the agent_feature_run
// worker: the draft is stored, its gate opens pending with the acting user as preparer, the
// run is recorded, the agent cannot decide the gate, and a redelivered job runs nothing.
func TestFeatureRunEndToEnd(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		feature   string
		gateKind  string
		start     func(t *testing.T, c composed, jobs *capJobs) (trigger.RunArgs, string) // job args, run id the caller saw
		actingFor string
	}{
		{name: "screen action", feature: "F-MAT-03", gateKind: "matter.stage_move", actingFor: runLawyer,
			start: func(t *testing.T, c composed, jobs *capJobs) (trigger.RunArgs, string) {
				body := `{"screen":"matter-stage-board","record_ids":["` + runMatter + `"]}`
				req := httptest.NewRequest(http.MethodPost, "/agent/actions/F-MAT-03", strings.NewReader(body))
				req.Header.Set("X-Test-User", runLawyer)
				rec := httptest.NewRecorder()
				c.server.Handler().ServeHTTP(rec, req)
				if rec.Code != http.StatusAccepted {
					t.Fatalf("POST action = %d %s", rec.Code, rec.Body)
				}
				var out struct {
					RunID string `json:"run_id"`
				}
				if err := json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&out); err != nil {
					t.Fatal(err)
				}
				if len(jobs.args) != 1 {
					t.Fatalf("%d jobs enqueued, want 1", len(jobs.args))
				}
				return jobs.args[0], out.RunID
			}},
		{name: "scheduled job acts for the designated lawyer", feature: "F-DOC-09", gateKind: "docs.template_publish", actingFor: runDesignate,
			start: func(t *testing.T, _ composed, _ *capJobs) (trigger.RunArgs, string) {
				// A tick found no user in the job: the worker reads the designated lawyer.
				return trigger.RunArgs{FeatureID: "F-DOC-09", Slot: "2026-10-12"}, JobRunID(1)
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jobs := &capJobs{}
			records := &memRecords{recs: map[string]agentrun.Record{}}
			drafts := &memDrafts{}
			key := trust.SealKey{ID: 1, Key: make([]byte, 32)}
			model := aifake.New(fstest.MapFS{
				strings.ToLower(tc.feature) + "/run/ok.yaml": {Data: []byte(runFixture(tc.feature, tc.gateKind))},
			})
			wiring := runWiring(t, records, drafts)
			prod := &Production{SealKey: &key, ModelClient: model, FirmAccount: "firm-general", Jobs: jobs}
			c := compose(t, legalFlagNames(t), Options{Production: prod, Agent: wiring})
			if !registeredKind(c, tc.gateKind) {
				// docs.template_publish has no module registration in this graph yet
				// (docs/QUESTIONS.md AGENT-PROD-2b); the test stands in for it.
				if err := c.gates.Register(gate.GateKind{Slug: tc.gateKind, Approver: "documents.edit", MinApprovers: 1,
					TTL: 24 * time.Hour, Effect: func(context.Context, pgx.Tx, registry.Approved) error { return nil }}); err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Contains(c.k.Jobs.Kinds(), trigger.JobKindRun) {
				t.Fatalf("no %s worker in the job set: %v", trigger.JobKindRun, c.k.Jobs.Kinds())
			}
			if wiring.RunWorker == nil {
				t.Fatal("boot added no feature run worker")
			}
			args, runID := tc.start(t, c, jobs)
			if runID != JobRunID(1) {
				t.Fatalf("run id %q, want the job's %q", runID, JobRunID(1))
			}
			job := &river.Job[trigger.RunArgs]{JobRow: &rivertype.JobRow{ID: 1, Attempt: 1, MaxAttempts: 25}, Args: args}
			if err := wiring.RunWorker.Work(ctx, job); err != nil {
				t.Fatalf("Work: %v", err)
			}
			// rule: AI-R-005
			rec, err := records.Get(ctx, runID)
			if err != nil || rec.State != agentrun.StateSucceeded || rec.ActingFor != tc.actingFor || rec.FeatureID != tc.feature {
				t.Fatalf("run record = %+v, %v", rec, err)
			}
			if len(drafts.drafts) != 1 || drafts.drafts[0].PreparedBy != tc.actingFor || drafts.drafts[0].RunID != runID {
				t.Fatalf("drafts = %+v", drafts.drafts)
			}
			// rule: ARC-I-039
			// The gate is pending and its preparer is the acting user, through the agent run.
			g, err := c.gates.Get(ctx, gate.GateID("gate-1"))
			if err != nil {
				t.Fatalf("gate: %v", err)
			}
			if g.Status != gate.StatusPending || g.Kind != tc.gateKind || g.Preparer.Kind != registry.PrincipalAgent ||
				g.Preparer.ActingFor != tc.actingFor || g.Preparer.RunID != runID {
				t.Fatalf("gate = %+v", g)
			}
			// rule: ARC-I-017
			// The agent never decides a gate, whoever it acts for: 403.
			agentP := gate.Principal{Kind: gate.PrincipalAgent, RunID: runID, ActingFor: tc.actingFor}
			err = c.gates.Decide(ctx, nil, g.ID, g.Version, gate.Decision{Verdict: gate.VerdictApprove, PayloadHash: g.PayloadHash}, agentP)
			if !errors.Is(err, gate.ErrAgentApprover) {
				t.Fatalf("agent decide = %v, want ErrAgentApprover", err)
			}
			if p := gate.Problem(err); p == nil || p.Status != http.StatusForbidden {
				t.Fatalf("agent decide problem = %+v, want 403", p)
			}
			// A redelivered job runs nothing: no second draft, no second gate.
			if err := wiring.RunWorker.Work(ctx, job); err != nil {
				t.Fatalf("redelivered Work: %v", err)
			}
			if len(drafts.drafts) != 1 {
				t.Fatalf("redelivery stored %d drafts", len(drafts.drafts))
			}
			if _, err := c.gates.Get(ctx, gate.GateID("gate-2")); !errors.Is(err, gate.ErrNotFound) {
				t.Fatalf("redelivery opened a second gate: %v", err)
			}
		})
	}
}

// registeredKind reports whether the graph registered gate kind slug.
func registeredKind(c composed, slug string) bool {
	for _, call := range c.boot.Method(registry.MethodGateKind) {
		if call.Args[0].(registry.GateKind).Slug == slug {
			return true
		}
	}
	return false
}

// noLawyerFor names no designated lawyer.
type noLawyerFor struct{}

func (noLawyerFor) DesignatedLawyer(context.Context, string) (string, error) {
	return "", trigger.ErrNoDesignatedLawyer
}

// TestFeatureRunWorkerRefuses checks the jobs the worker cancels or skips without a model call.
func TestFeatureRunWorkerRefuses(t *testing.T) {
	ctx := context.Background()
	features, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	ended := &memRecords{recs: map[string]agentrun.Record{JobRunID(7): {ID: JobRunID(7), State: agentrun.StateSucceeded}}}
	model := aifake.New(fstest.MapFS{})
	cases := []struct {
		name   string
		w      FeatureRunWorker
		args   trigger.RunArgs
		want   error // nil: the job ends without error
		cancel bool
	}{
		{name: "missing dependency", w: FeatureRunWorker{}, args: trigger.RunArgs{FeatureID: "F-DOC-09"}, want: agent.ErrMissingDep},
		{name: "unknown feature is cancelled", args: trigger.RunArgs{FeatureID: "F-XXX-99", ActingFor: runLawyer},
			want: ErrUnknownFeature, cancel: true},
		// rule: ARC-I-040
		{name: "scheduled job with no designated lawyer is cancelled", args: trigger.RunArgs{FeatureID: "F-DOC-09", Slot: "2026-10-12"},
			want: ErrNoActingUser, cancel: true},
		{name: "an ended run is not run again", args: trigger.RunArgs{FeatureID: "F-DOC-09", ActingFor: runLawyer}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.w
			if tc.name != "missing dependency" {
				w = FeatureRunWorker{Features: features, Access: runAccess{}, Model: model, Tools: agenttoolsfake.New(),
					Records: ended, Designated: noLawyerFor{}}
			}
			id := int64(8)
			if tc.want == nil {
				id = 7 // the ended run
			}
			job := &river.Job[trigger.RunArgs]{JobRow: &rivertype.JobRow{ID: id, Attempt: 1, MaxAttempts: 25}, Args: tc.args}
			err := w.Work(ctx, job)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Work = %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Work = %v, want %v", err, tc.want)
			}
			var cancel *rivertype.JobCancelError
			if errors.As(err, &cancel) != tc.cancel {
				t.Fatalf("cancelled = %v, want %v", !tc.cancel, tc.cancel)
			}
		})
	}
	if a, b := JobRunID(1), JobRunID(1); a != b || a == JobRunID(2) || !isUUID(a) {
		t.Fatalf("JobRunID not a stable UUID per job: %q %q", a, JobRunID(2))
	}
}
