package agenttools_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/agenttools"
	"github.com/dagi377/counsel/kernel/db"
)

// planRow is one scripted agent.plan row.
type planRow struct {
	id, runID string
	body      []byte
}

// planTable answers PGPlans' statements from scripted rows: select by run_id or id, and the
// upsert on run_id. Methods PGPlans does not call panic (embedded nil pgx.Tx).
type planTable struct {
	pgx.Tx
	mu   sync.Mutex
	rows []planRow
	fail error
	sqls []string
}

func (f *planTable) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sqls = append(f.sqls, sql)
	if f.fail != nil {
		return errRow{f.fail}
	}
	key := args[0].(string)
	byRun := strings.Contains(sql, "WHERE run_id = $1")
	for _, r := range f.rows {
		if (byRun && r.runID == key) || (!byRun && r.id == key) {
			return planRowScan{r}
		}
	}
	return errRow{pgx.ErrNoRows}
}

func (f *planTable) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sqls = append(f.sqls, sql)
	if f.fail != nil {
		return pgconn.CommandTag{}, f.fail
	}
	if !strings.Contains(sql, "ON CONFLICT (run_id)") {
		return pgconn.CommandTag{}, errors.New("planTable: unexpected statement")
	}
	row := planRow{id: args[0].(string), runID: args[1].(string), body: slices.Clone(args[2].([]byte))}
	for i, r := range f.rows {
		if r.runID == row.runID {
			f.rows[i] = row
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		}
	}
	f.rows = append(f.rows, row)
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

type planRowScan struct{ r planRow }

func (s planRowScan) Scan(dest ...any) error {
	if len(dest) != 3 {
		return errors.New("planRowScan: want id, run_id, body")
	}
	*dest[0].(*string) = s.r.id
	*dest[1].(*string) = s.r.runID
	*dest[2].(*[]byte) = slices.Clone(s.r.body)
	return nil
}

// planDB is a db.DB whose InTx hands fn the scripted table.
type planDB struct{ tx *planTable }

func (d planDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (planDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (planDB) Ping(context.Context) error                     { return nil }
func (planDB) Close()                                         {}

func newPGPlans() (agenttools.PGPlans, *planTable) {
	tx := &planTable{}
	return agenttools.PGPlans{DB: planDB{tx}}, tx
}

// TestPGPlansPersistsPlan mirrors TestPlanPersistedWithSteps on the Postgres store.
func TestPGPlansPersistsPlan(t *testing.T) {
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
			store, _ := newPGPlans()
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
			byID, err := store.ByID(ctx, p.ID)
			if err != nil || !reflect.DeepEqual(byID, p) {
				t.Fatalf("ByID = %+v, %v; want %+v", byID, err, p)
			}
		})
	}
}

// TestPGPlansLinksCalls mirrors TestStepsLinkToolCalls on the Postgres store.
func TestPGPlansLinksCalls(t *testing.T) {
	ctx := context.Background()
	type op struct {
		call string
		done int
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
			store, _ := newPGPlans()
			pl := agenttools.NewPlanner(store)
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

// TestPGPlansResume mirrors TestRunResumesAtFirstUnfinishedStep: a new Planner over the same
// rows resumes at the first unfinished step.
func TestPGPlansResume(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		status []agenttools.StepStatus
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
			store, _ := newPGPlans()
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

// TestPGPlansErrors mirrors TestPlannerErrors, plus decode and query failures.
func TestPGPlansErrors(t *testing.T) {
	ctx := context.Background()
	store, tbl := newPGPlans()
	pl := agenttools.NewPlanner(store)
	p, err := pl.ReportPlan(ctx, "run-1", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	newPlan, err := pl.ReportPlan(ctx, "run-1", []string{"x", "y"})
	if err != nil || newPlan.ID == p.ID || len(newPlan.Steps) != 2 {
		t.Fatalf("different titles: plan %+v err %v, want a new plan", newPlan, err)
	}
	if len(tbl.rows) != 1 {
		t.Fatalf("rows = %d, want one plan per run", len(tbl.rows))
	}
	boom := errors.New("connection reset")
	withFail := func(err error, run func() error) func() error {
		return func() error {
			tbl.fail = err
			defer func() { tbl.fail = nil }()
			return run()
		}
	}
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
		{"query fails", withFail(boom, func() error { _, err := store.ByRun(ctx, "run-1"); return err }), boom},
		{"save fails", withFail(boom, func() error { return store.Save(ctx, newPlan) }), boom},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
	tbl.rows[0].body = []byte(`{"not":"an array"}`)
	if _, err := store.ByRun(ctx, "run-1"); err == nil || errors.Is(err, agenttools.ErrNotFound) {
		t.Fatalf("corrupt body: err = %v, want a decode error", err)
	}
}

// TestPGPlansBodyHoldsToolStepsOnly: the stored body is the step list and nothing else, so no
// prompt or completion text can reach agent.plan.
func TestPGPlansBodyHoldsToolStepsOnly(t *testing.T) {
	ctx := context.Background()
	store, tbl := newPGPlans()
	pl := agenttools.NewPlanner(store)
	if _, err := pl.ReportPlan(ctx, "run-1", []string{"Read the file", "Draft the reply"}); err != nil {
		t.Fatal(err)
	}
	if _, err := pl.RecordCall(ctx, "run-1", "call-1"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		check func(t *testing.T)
	}{
		{"body is an array of step objects with known keys", func(t *testing.T) {
			var steps []map[string]json.RawMessage
			if err := json.Unmarshal(tbl.rows[0].body, &steps); err != nil {
				t.Fatalf("body %s: %v", tbl.rows[0].body, err)
			}
			allowed := map[string]bool{"index": true, "title": true, "status": true, "call_ids": true, "id": true}
			for _, s := range steps {
				for k := range s {
					if !allowed[k] {
						t.Fatalf("body key %q stored; only tool steps allowed", k)
					}
				}
			}
		}},
		{"statements name no prompt or completion column", func(t *testing.T) {
			for _, s := range tbl.sqls {
				low := strings.ToLower(s)
				if strings.Contains(low, "prompt") || strings.Contains(low, "completion") {
					t.Fatalf("statement touches model text: %s", s)
				}
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.check)
	}
}

// TestPGPlansStepIDs: the step ids an agent names in its plan are stored with the plan, so a
// new Planner over the same table (a restarted process) finds a step by id (Q-BOOT-PORTS-B).
func TestPGPlansStepIDs(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		ids    []string
		lookup string
		want   int
		err    error
	}{
		{"named steps", []string{"read", "draft"}, "draft", 1, nil},
		{"unknown id", []string{"read", "draft"}, "send", -1, nil},
		{"empty lookup", []string{"read", "draft"}, "", -1, nil},
		{"too few ids", []string{"read"}, "read", -1, agenttools.ErrStepTitle},
		{"repeated id", []string{"read", "read"}, "read", -1, agenttools.ErrStepTitle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, _ := newPGPlans()
			pl := agenttools.NewPlanner(store)
			p, err := pl.ReportPlan(ctx, "run-1", []string{"Read the file", "Draft the reply"})
			if err != nil {
				t.Fatal(err)
			}
			err = pl.SetStepIDs(ctx, p.ID, tc.ids)
			if !errors.Is(err, tc.err) {
				t.Fatalf("SetStepIDs err = %v, want %v", err, tc.err)
			}
			got, _, err := agenttools.NewPlanner(store).Current(ctx, "run-1")
			if err != nil {
				t.Fatal(err)
			}
			if i := agenttools.StepIndex(got, tc.lookup); i != tc.want {
				t.Fatalf("StepIndex(%q) = %d, want %d", tc.lookup, i, tc.want)
			}
		})
	}
}
