package boot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/deadlines"
	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/ai"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/report/reportfake"
	"github.com/dagi377/counsel/kernel/server/serverfake"
)

// recordJobs is the kernel's insert-only job client: it records every insert.
type recordJobs struct {
	mu   sync.Mutex
	args []river.JobArgs
}

func (r *recordJobs) Insert(_ context.Context, a river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.args = append(r.args, a)
	return &rivertype.JobInsertResult{Job: &rivertype.JobRow{ID: int64(len(r.args)), Kind: a.Kind()}}, nil
}

func (r *recordJobs) runs() []trigger.RunArgs {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []trigger.RunArgs
	for _, a := range r.args {
		if ra, ok := a.(trigger.RunArgs); ok {
			out = append(out, ra)
		}
	}
	return out
}

func (r *recordJobs) reset() {
	r.mu.Lock()
	r.args = nil
	r.mu.Unlock()
}

// actionAccess grants every capability and builds an agent principal for any run.
type actionAccess struct{}

func (actionAccess) Can(context.Context, registry.Principal, string, registry.Subject) (bool, error) {
	return true, nil
}

func (actionAccess) AgentPrincipal(_ context.Context, run identity.RunID, actingFor identity.UserID, _ []string) (registry.Principal, error) {
	return registry.Principal{Kind: registry.PrincipalAgent, RunID: string(run), ActingFor: string(actingFor)}, nil
}

// oneLawyer names one designated lawyer for every scheduled feature.
type oneLawyer struct{}

func (oneLawyer) DesignatedLawyer(context.Context, string) (string, error) { return "u-lawyer", nil }

// oneTimekeeper names one active timekeeper for every per-user scheduled feature.
type oneTimekeeper struct{}

func (oneTimekeeper) ActiveUsers(context.Context, string) ([]string, error) {
	return []string{"u-timekeeper"}, nil
}

const actionUser = "u-requester"

// enqueueKernel builds the legal app the way counseld and counsel-worker do: Production with
// no Jobs, over a kernel whose Enqueuer is q. The clock is a Monday that opens a month, so
// every daily, weekly and monthly binding has a slot.
func enqueueKernel(t *testing.T, q kboot.JobInserter) (*kboot.Kernel, *AgentWiring) {
	t.Helper()
	schemas, err := registry.LoadSchemas(os.DirFS("../../../spec/kernel"))
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("America/Toronto")
	clk := clockfake.New(time.Date(2026, 6, 1, 10, 0, 0, 0, loc), loc)
	ob := outboxfake.New()
	deps := kboot.Deps{
		Registry: registry.NewBoot(schemas), DB: noConnPool{dbfake.New()}, Clock: clk,
		Gates: gatefake.New(clk.Now, gatefake.Caps{}), Reports: reportfake.New(clk.Now, reportfake.Caps{}),
		Server: serverfake.New(), Emitter: ob, Enqueuer: q,
		Outbox: &outbox.Deliverer{Relay: ob, Inbox: ob, DB: noConnPool{dbfake.New()}},
	}
	p := mockProduction(nil)
	p.Jobs = nil
	p.Principal = func(*http.Request) (registry.Principal, bool) {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: actionUser}, true
	}
	w := &AgentWiring{Designated: oneLawyer{}, Timekeepers: oneTimekeeper{}, Actions: &trigger.Actions{Access: actionAccess{}}}
	k, err := kboot.BuildWith(context.Background(), deps, legalFlagNames(t), App(Options{Production: p, Agent: w}))
	if err != nil {
		t.Fatalf("BuildWith: %v", err)
	}
	return k, w
}

// queuedAction is a queued (not chat) feature with a screen action whose screen the route
// knows.
func queuedAction(t *testing.T) (featureID, screen string) {
	t.Helper()
	features, err := Features()
	if err != nil {
		t.Fatal(err)
	}
	screens := ActionScreens()
	for _, d := range features.All() {
		if d.Kind == ai.KindChat {
			continue
		}
		for _, a := range d.Actions {
			if _, ok := screens[screenOf(a)]; ok {
				return d.ID, screenOf(a)
			}
		}
	}
	t.Fatal("no queued feature with a screen action")
	return "", ""
}

// TestProductionScreenActionEnqueues posts a screen action to the route Production built with
// no Jobs set: the kernel's enqueuer gets exactly one agent_feature_run job, ids only.
func TestProductionScreenActionEnqueues(t *testing.T) {
	q := &recordJobs{}
	_, w := enqueueKernel(t, q)
	if w.Actions == nil || w.Actions.Queue == nil {
		t.Fatal("action route has no queue")
	}
	r := chi.NewRouter()
	w.Actions.Routes(r)
	id, screen := queuedAction(t)
	tests := []struct {
		name    string
		records string
	}{
		{"one record", `["m-1"]`},
		{"two records", `["m-1","m-2"]`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q.reset()
			body := `{"screen":"` + screen + `","record_ids":` + tc.records + `}`
			req := httptest.NewRequest(http.MethodPost, "/agent/actions/"+id, strings.NewReader(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("POST %s = %d %s, want 202", id, rec.Code, rec.Body)
			}
			// rule: AI-R-005
			if len(q.args) != 1 {
				t.Fatalf("enqueued %d jobs, want exactly 1", len(q.args))
			}
			got := q.runs()
			if len(got) != 1 || got[0].Kind() != trigger.JobKindRun || got[0].FeatureID != id || got[0].ActingFor != actionUser || got[0].EventID == "" {
				t.Fatalf("job = %+v", q.args[0])
			}
		})
	}
}

// TestProductionTriggersEnqueue checks the job-feature bindings enqueue on the kernel's
// enqueuer: a tick (the TickWorker the worker role runs) enqueues the scheduled runs, and the
// outbox relay consumer enqueues the event-bound runs. Both kinds have a worker.
func TestProductionTriggersEnqueue(t *testing.T) {
	q := &recordJobs{}
	k, w := enqueueKernel(t, q)
	kinds := k.Jobs.Kinds()
	for _, kind := range []string{trigger.JobKindTick, trigger.JobKindRun} {
		if !slices.Contains(kinds, kind) {
			t.Errorf("no worker for %s: %v", kind, kinds)
		}
	}
	if w.Bindings == nil {
		t.Fatal("no job-feature bindings built from the kernel enqueuer")
	}
	var consumer *outbox.Consumer
	for i, c := range k.Outbox.Consumers {
		if c.Name == ConsumerTrigger {
			consumer = &k.Outbox.Consumers[i]
		}
	}
	if consumer == nil {
		t.Fatal("trigger consumer not on the outbox relay")
	}
	ctx := context.Background()

	t.Run("tick enqueues the scheduled runs", func(t *testing.T) {
		q.reset()
		if err := (&trigger.TickWorker{Bindings: w.Bindings}).Work(ctx, nil); err != nil {
			t.Fatal(err)
		}
		got := q.runs()
		if len(got) == 0 {
			t.Fatal("tick enqueued no scheduled run")
		}
		perUser := false
		for _, a := range got {
			// A per-user feature (F-TIM-01) runs for each timekeeper as themselves; every other
			// scheduled run acts for the designated lawyer.
			want := "u-lawyer"
			if a.FeatureID == tk.FeatureRebuildDay {
				want, perUser = "u-timekeeper", true
			}
			if a.Slot == "" || a.ActingFor != want || a.EventID != "" {
				t.Errorf("scheduled run %+v", a)
			}
		}
		if !perUser {
			t.Errorf("tick enqueued no %s run for the timekeeper", tk.FeatureRebuildDay)
		}
	})

	events := []struct {
		name     string
		msg      outbox.Message
		wantRuns int
	}{
		{"bound event enqueues its feature", outbox.Message{ID: "e-1", Type: deadlines.EventDocumentUploaded,
			Subject: outbox.Subject{Kind: "document", ID: "d-1"}, Payload: []byte(`{"user_id":"u-1"}`)}, 1},
		{"unbound event enqueues nothing", outbox.Message{ID: "e-2", Type: "nobody.listens"}, 0},
		{"bound event with no acting user starts nothing", outbox.Message{ID: "e-3", Type: deadlines.EventDocumentUploaded,
			Payload: []byte(`{}`)}, 0},
	}
	for _, tc := range events {
		t.Run(tc.name, func(t *testing.T) {
			q.reset()
			if err := consumer.Handle(ctx, nil, tc.msg); err != nil {
				t.Fatal(err)
			}
			got := q.runs()
			n := 0
			for _, a := range got {
				if a.FeatureID == deadlines.FeatureExtractDeadlines {
					n++
					if a.EventID != tc.msg.ID || a.ActingFor != "u-1" {
						t.Errorf("event run %+v", a)
					}
				}
			}
			if n != tc.wantRuns {
				t.Fatalf("%s runs = %d, want %d (all: %+v)", deadlines.FeatureExtractDeadlines, n, tc.wantRuns, got)
			}
		})
	}
}
