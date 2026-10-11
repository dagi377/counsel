package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dagi377/counsel/kernel/agenttools"
)

// memStore persists plans as JSON bytes, the way a database row would, so nothing is shared
// by pointer between a Planner and the store.
type memStore struct {
	mu    sync.Mutex
	rows  map[string][]byte // plan id -> JSON
	byRun map[string]string // run id -> plan id
	fail  error
}

func newMemStore() *memStore {
	return &memStore{rows: map[string][]byte{}, byRun: map[string]string{}}
}

func (m *memStore) ByRun(ctx context.Context, runID string) (agenttools.Plan, error) {
	m.mu.Lock()
	id, ok := m.byRun[runID]
	m.mu.Unlock()
	if !ok {
		return agenttools.Plan{}, agenttools.ErrNotFound
	}
	return m.ByID(ctx, id)
}

func (m *memStore) ByID(_ context.Context, planID string) (agenttools.Plan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.rows[planID]
	if !ok {
		return agenttools.Plan{}, agenttools.ErrNotFound
	}
	var p agenttools.Plan
	err := json.Unmarshal(b, &p)
	return p, err
}

func (m *memStore) Save(_ context.Context, p agenttools.Plan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	m.rows[p.ID] = b
	m.byRun[p.RunID] = p.ID
	return nil
}

func TestPlanPersistedWithSteps(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		titles []string
		err    error
	}{
		{"three steps", []string{"Read the file", "Draft the reply", "Open approval"}, nil},
		{"one step", []string{"Summarise"}, nil},
		{"no steps", nil, agenttools.ErrStepTitle},
		{"blank title", []string{"ok", "  "}, agenttools.ErrStepTitle},
		{"title too long", []string{strings.Repeat("x", agenttools.MaxStepTitle+1)}, agenttools.ErrStepTitle},
		{"title at limit", []string{strings.Repeat("é", agenttools.MaxStepTitle)}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemStore()
			p, err := agenttools.NewPlanner(store).ReportPlan(ctx, "run-1", tc.titles)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err = %v, want %v", err, tc.err)
				}
				if _, err := store.ByRun(ctx, "run-1"); !errors.Is(err, agenttools.ErrNotFound) {
					t.Fatal("rejected plan was stored")
				}
				return
			}
			if err != nil {
				t.Fatalf("ReportPlan: %v", err)
			}
			stored, err := store.ByRun(ctx, "run-1")
			if err != nil {
				t.Fatalf("stored: %v", err)
			}
			if !reflect.DeepEqual(stored, p) {
				t.Fatalf("stored %+v, returned %+v", stored, p)
			}
			for i, s := range stored.Steps {
				if s.Index != i || s.Title != tc.titles[i] || s.Status != agenttools.StepPending {
					t.Fatalf("step %d = %+v", i, s)
				}
			}
		})
	}
}

func TestStepsLinkToolCalls(t *testing.T) {
	ctx := context.Background()
	type op struct {
		call string // record this call id
		done int    // or mark this step done (when call is "")
	}
	cases := []struct {
		name string
		ops  []op
		want [][]string
	}{
		{"calls go to first step", []op{{call: "c1"}, {call: "c2"}}, [][]string{{"c1", "c2"}, nil}},
		{"after done, next step", []op{{call: "c1"}, {done: 0}, {call: "c2"}}, [][]string{{"c1"}, {"c2"}}},
		{"duplicate call kept once", []op{{call: "c1"}, {call: "c1"}}, [][]string{{"c1"}, nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pl := agenttools.NewPlanner(newMemStore())
			p, err := pl.ReportPlan(ctx, "run-1", []string{"one", "two"})
			if err != nil {
				t.Fatal(err)
			}
			for _, o := range tc.ops {
				if o.call != "" {
					if _, err := pl.RecordCall(ctx, "run-1", o.call); err != nil {
						t.Fatalf("RecordCall: %v", err)
					}
					continue
				}
				if err := pl.ReportStep(ctx, p.ID, o.done, agenttools.StepDone); err != nil {
					t.Fatalf("ReportStep: %v", err)
				}
			}
			got, _, err := pl.Current(ctx, "run-1")
			if err != nil {
				t.Fatal(err)
			}
			for i, s := range got.Steps {
				if !reflect.DeepEqual(s.CallIDs, tc.want[i]) {
					t.Fatalf("step %d calls = %v, want %v", i, s.CallIDs, tc.want[i])
				}
			}
			if got.Steps[0].Status == agenttools.StepPending {
				t.Fatal("step with a call still pending")
			}
		})
	}
}

func TestRunResumesAtFirstUnfinishedStep(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		status []agenttools.StepStatus // set before the crash
		want   int
	}{
		{"fresh", nil, 0},
		{"first done", []agenttools.StepStatus{agenttools.StepDone}, 1},
		{"second running", []agenttools.StepStatus{agenttools.StepDone, agenttools.StepRunning}, 1},
		{"second failed is retried", []agenttools.StepStatus{agenttools.StepDone, agenttools.StepFailed, agenttools.StepDone}, 1},
		{"all done", []agenttools.StepStatus{agenttools.StepDone, agenttools.StepDone, agenttools.StepDone}, -1},
	}
	titles := []string{"a", "b", "c"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemStore()
			before := agenttools.NewPlanner(store)
			p, err := before.ReportPlan(ctx, "run-1", titles)
			if err != nil {
				t.Fatal(err)
			}
			for i, s := range tc.status {
				if err := before.ReportStep(ctx, p.ID, i, s); err != nil {
					t.Fatal(err)
				}
			}
			// crash: the first Planner is gone; a new one reads the same store.
			after := agenttools.NewPlanner(store)
			again, err := after.ReportPlan(ctx, "run-1", titles)
			if err != nil {
				t.Fatal(err)
			}
			if again.ID != p.ID {
				t.Fatalf("re-reported plan id %s, want %s (progress kept)", again.ID, p.ID)
			}
			_, at, err := after.Current(ctx, "run-1")
			if err != nil {
				t.Fatal(err)
			}
			if at != tc.want {
				t.Fatalf("resume at %d, want %d", at, tc.want)
			}
		})
	}
}

func TestPlannerErrors(t *testing.T) {
	ctx := context.Background()
	store := newMemStore()
	pl := agenttools.NewPlanner(store)
	p, err := pl.ReportPlan(ctx, "run-1", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	newPlan, err := pl.ReportPlan(ctx, "run-1", []string{"x", "y"})
	if err != nil || newPlan.ID == p.ID || len(newPlan.Steps) != 2 {
		t.Fatalf("different titles: plan %+v err %v, want a new plan", newPlan, err)
	}
	boom := fmt.Errorf("disk full")
	cases := []struct {
		name string
		run  func() error
		want error
	}{
		{"no plan", func() error { _, _, err := pl.Current(ctx, "run-2"); return err }, agenttools.ErrNotFound},
		{"step of unknown plan", func() error { return pl.ReportStep(ctx, "nope", 0, agenttools.StepDone) }, agenttools.ErrNotFound},
		{"step out of range", func() error { return pl.ReportStep(ctx, newPlan.ID, 2, agenttools.StepDone) }, agenttools.ErrNotFound},
		{"bad status", func() error { return pl.ReportStep(ctx, newPlan.ID, 0, "skipped") }, agenttools.ErrStepTitle},
		{"call without plan", func() error { _, err := pl.RecordCall(ctx, "run-2", "c1"); return err }, agenttools.ErrNotFound},
		{"save fails", func() error {
			store.fail = boom
			defer func() { store.fail = nil }()
			_, err := pl.RecordCall(ctx, "run-1", "c1")
			return err
		}, boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
