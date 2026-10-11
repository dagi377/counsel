package run_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/ai"
)

// fakeQueue is an in-memory enqueuer that keeps one job per unique args, like River with
// UniqueOpts.ByArgs.
type fakeQueue struct {
	jobs []queued
	keys map[string]bool
}

type queued struct {
	args run.RetryArgs
	at   time.Time
	raw  []byte
}

func (q *fakeQueue) Insert(_ context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	a, ok := args.(run.RetryArgs)
	if !ok {
		return nil, fmt.Errorf("unexpected job %T", args)
	}
	if opts == nil || !opts.UniqueOpts.ByArgs {
		return nil, errors.New("retry job must be unique by args")
	}
	key := fmt.Sprintf("%s|%s|%d", a.FeatureID, a.RunID, a.Attempt)
	if q.keys == nil {
		q.keys = map[string]bool{}
	}
	if q.keys[key] {
		return &rivertype.JobInsertResult{UniqueSkippedAsDuplicate: true}, nil
	}
	q.keys[key] = true
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	q.jobs = append(q.jobs, queued{args: a, at: opts.ScheduledAt, raw: raw})
	return &rivertype.JobInsertResult{}, nil
}

type failures struct{ runs []string }

func (f *failures) RunFailed(_ context.Context, featureID, runID string, attempts int) error {
	f.runs = append(f.runs, fmt.Sprintf("%s/%s/%d", featureID, runID, attempts))
	return nil
}

var t0 = time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)

// rule: AI-R-003
func TestAI_R_003_RetryBackoff(t *testing.T) {
	tests := []struct {
		name       string
		out        run.Outcome
		done       int
		wantQueued bool
		wantWait   time.Duration
		wantFailed int
	}{
		{name: "no fault", out: run.Outcome{}, done: 0},
		{name: "first retry 1m", out: run.Outcome{RetryQueued: true}, done: 0, wantQueued: true, wantWait: time.Minute},
		{name: "second retry 5m", out: run.Outcome{RetryQueued: true}, done: 1, wantQueued: true, wantWait: 5 * time.Minute},
		{name: "third retry 25m", out: run.Outcome{RetryQueued: true}, done: 2, wantQueued: true, wantWait: 25 * time.Minute},
		{name: "gives up", out: run.Outcome{RetryQueued: true}, done: 3, wantFailed: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, f := &fakeQueue{}, &failures{}
			r := run.NewRetrier(q, f).WithClock(func() time.Time { return t0 })
			ok, err := r.Queue(context.Background(), spec(), agent("run-b"), tt.out, tt.done)
			if err != nil {
				t.Fatalf("queue: %v", err)
			}
			if ok != tt.wantQueued || len(f.runs) != tt.wantFailed {
				t.Fatalf("queued=%v failed=%v", ok, f.runs)
			}
			if !tt.wantQueued {
				if len(q.jobs) != 0 {
					t.Fatalf("jobs = %v, want none", q.jobs)
				}
				return
			}
			j := q.jobs[0]
			if j.at.Sub(t0) != tt.wantWait || j.args.Attempt != tt.done+1 || j.args.RunID != "run-b" {
				t.Fatalf("job = %+v at +%v", j.args, j.at.Sub(t0))
			}
		})
	}
}

// rule: AI-R-005
func TestAI_R_005_RetryArgsCarryIDsOnly(t *testing.T) {
	const prompt, trigger = "Draft the reply to Mrs Smith", "Please settle for $40,000"
	q := &fakeQueue{}
	r := run.NewRetrier(q, &failures{})
	tr := run.Trigger{Source: agenttools.Citation{VersionID: "ver-7", Page: 1, FromLine: 1, ToLine: 2}, Text: trigger}
	tests := []struct{ name string }{{name: "first"}, {name: "duplicate"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := r.Queue(context.Background(), spec(), agent("run-ids"), run.Outcome{RetryQueued: true}, 0, tr, tr); err != nil {
				t.Fatalf("queue: %v", err)
			}
		})
	}
	// Idempotent by run id: two enqueues of the same retry -> one job.
	if len(q.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(q.jobs))
	}
	raw := string(q.jobs[0].raw)
	for _, text := range []string{prompt, trigger, "Smith", "40,000"} {
		if strings.Contains(raw, text) {
			t.Fatalf("job args carry text %q: %s", text, raw)
		}
	}
	want := `{"feature_id":"F-RUN-01","run_id":"run-ids","trigger_refs":["ver-7"],"attempt":1}`
	if raw != want {
		t.Fatalf("args = %s, want %s", raw, want)
	}
	if k := (run.RetryArgs{}).Kind(); k != "agent_run_retry" {
		t.Fatalf("kind = %q", k)
	}
}

// flaky fails the call numbers in fail with a rate limit; other calls go to the model.
type flaky struct {
	ai.ModelClient
	n    int
	fail map[int]bool
}

func (f *flaky) Complete(ctx context.Context, req ai.Request) (ai.Response, error) {
	f.n++
	if f.fail[f.n] {
		return ai.Response{}, fmt.Errorf("provider: %w", ai.ErrRateLimited)
	}
	return f.ModelClient.Complete(ctx, req)
}

type resumer struct{ prompt string }

func (r resumer) Resume(_ context.Context, a run.RetryArgs) (run.Spec, run.Principal, string, []run.Trigger, error) {
	return spec(), agent(a.RunID), r.prompt, nil, nil
}

const resumable = `synthetic: true
feature: F-RUN-01
match:
  last_user_regex: "."
turns:
  - reply: {tool_calls: [{name: report_plan, input: {steps: [read, propose]}}]}
  - reply: {tool_calls: [{name: propose_note, input: {}}]}
  - reply: {text: "Proposed for review.", stop_reason: end_turn}
`

// rule: AI-R-003
func TestAI_R_003_RetryResumesPlanUnderSameRunID(t *testing.T) {
	tests := []struct {
		name       string
		fail       map[int]bool
		wantGates  []string
		wantNext   int // attempt of the next queued job, 0 for none
		wantFailed int
	}{
		{name: "retry resumes plan and proposes", fail: map[int]bool{2: true}, wantGates: []string{"gate-1"}},
		{name: "retry faults again queues next", fail: map[int]bool{2: true, 3: true}, wantNext: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(prev) })

			ctx := context.Background()
			reg := tools(t)
			gates := &gateLog{Tools: reg}
			loop := run.New(&flaky{ModelClient: model(resumable), fail: tt.fail}, gates)
			q, f := &fakeQueue{}, &failures{}
			r := run.NewRetrier(q, f)
			p := agent("run-resume")
			out, err := loop.Run(ctx, spec(), p, "Prepare a note.")
			if err != nil || !out.RetryQueued {
				t.Fatalf("first run: out=%+v err=%v", out, err)
			}
			if _, err := r.Queue(ctx, spec(), p, out, 0); err != nil {
				t.Fatalf("queue: %v", err)
			}
			if len(q.jobs) != 1 {
				t.Fatalf("jobs = %d, want 1", len(q.jobs))
			}
			w := run.NewRetryWorker(loop, resumer{prompt: "Prepare a note."}, r)
			if err := w.Retry(ctx, q.jobs[0].args); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if fmt.Sprint(gates.ids) != fmt.Sprint(tt.wantGates) {
				t.Fatalf("gates = %v, want %v", gates.ids, tt.wantGates)
			}
			if tt.wantNext == 0 && len(q.jobs) != 1 {
				t.Fatalf("jobs = %d, want no further retry", len(q.jobs))
			}
			if tt.wantNext > 0 && (len(q.jobs) != 2 || q.jobs[1].args.Attempt != tt.wantNext || q.jobs[1].args.RunID != p.RunID) {
				t.Fatalf("next job = %+v", q.jobs)
			}
			if strings.Contains(logs.String(), "Prepare a note") || strings.Contains(logs.String(), "Proposed for review") {
				t.Fatalf("slog carries prompt or completion text: %s", logs.String())
			}
		})
	}
}

// gateLog records gates opened by write calls the runner makes.
type gateLog struct {
	agenttools.Tools
	ids []string
}

func (g *gateLog) Call(ctx context.Context, p agenttools.Principal, name string, in json.RawMessage) (agenttools.ToolResult, error) {
	res, err := g.Tools.Call(ctx, p, name, in)
	if err == nil && res.GateID != "" {
		g.ids = append(g.ids, res.GateID)
	}
	return res, err
}

func TestRetryRejectsBadJobs(t *testing.T) {
	w := run.NewRetryWorker(nil, resumer{}, run.NewRetrier(&fakeQueue{}, &failures{}))
	tests := []struct {
		name string
		args run.RetryArgs
	}{
		{name: "no run id", args: run.RetryArgs{FeatureID: "F-RUN-01", Attempt: 1}},
		{name: "no feature", args: run.RetryArgs{RunID: "r", Attempt: 1}},
		{name: "no attempt", args: run.RetryArgs{FeatureID: "F-RUN-01", RunID: "r"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := w.Retry(context.Background(), tt.args); !errors.Is(err, run.ErrRetryArgs) {
				t.Fatalf("err = %v, want ErrRetryArgs", err)
			}
		})
	}
}
