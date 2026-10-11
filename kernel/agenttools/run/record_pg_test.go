package run_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/agenttools/run"
	"github.com/dagi377/counsel/kernel/db"
)

// recordTable answers PGRecords' statements from in-memory agent.run_record rows.
type recordTable struct {
	pgx.Tx
	rows map[string]*run.Record
	sqls []string
}

func (t *recordTable) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.sqls = append(t.sqls, sql)
	id := args[0].(string)
	r, ok := t.rows[id]
	switch {
	case strings.HasPrefix(sql, "INSERT"):
		t.rows[id] = &run.Record{ID: id, FeatureID: args[1].(string), SkillID: args[2].(string),
			ActingFor: args[3].(string), Grant: args[4].([]string), SubjectID: args[5].(string),
			State: run.StateQueued, CreatedAt: args[6].(time.Time)}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(sql, "state = 'running'"):
		if !ok || r.State != run.StateQueued {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		at := args[1].(time.Time)
		r.State, r.StartedAt = run.StateRunning, &at
	case strings.Contains(sql, "state = $2"):
		if !ok || (r.State != run.StateQueued && r.State != run.StateRunning) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		at := args[3].(time.Time)
		r.State, r.ErrorCode, r.EndedAt = args[1].(string), args[2].(string), &at
		r.InTokens += args[4].(int64)
		r.OutTokens += args[5].(int64)
		r.CostMicroC += args[6].(int64)
	default:
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec: %s", sql)
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type recordRow struct{ r *run.Record }

func (x recordRow) Scan(dest ...any) error {
	if x.r == nil {
		return pgx.ErrNoRows
	}
	r := x.r
	vals := []any{r.ID, r.FeatureID, r.SkillID, r.ActingFor, r.Grant, r.SubjectID, r.State, r.ErrorCode, r.CreatedAt,
		r.StartedAt, r.EndedAt, r.InTokens, r.OutTokens, r.CostMicroC}
	if len(dest) != len(vals) {
		return fmt.Errorf("scan wants %d columns, got %d", len(vals), len(dest))
	}
	for i, v := range vals {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *[]string:
			*d = v.([]string)
		case *time.Time:
			*d = v.(time.Time)
		case **time.Time:
			*d = v.(*time.Time)
		case *int64:
			*d = v.(int64)
		default:
			return fmt.Errorf("column %d: %T", i, d)
		}
	}
	return nil
}

func (t *recordTable) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.sqls = append(t.sqls, sql)
	return recordRow{r: t.rows[args[0].(string)]}
}

type recordDB struct{ tx pgx.Tx }

func (d recordDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (recordDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (recordDB) Ping(context.Context) error                     { return nil }
func (recordDB) Close()                                         {}

// rule: AI-R-005
// TestPGRecordsLifecycle: a run is recorded queued, runs, ends once with its usage, and no
// statement names a prompt, input or answer column.
func TestPGRecordsLifecycle(t *testing.T) {
	ctx := context.Background()
	const id = "0190e000-0000-7000-8000-000000000001"
	t0 := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		step  func(s run.PGRecords) error
		state string
		err   error
	}{
		{"start", func(s run.PGRecords) error { return s.Start(ctx, id, t0.Add(time.Second)) }, run.StateRunning, nil},
		{"start twice", func(s run.PGRecords) error { return s.Start(ctx, id, t0) }, run.StateRunning, run.ErrRecordNotFound},
		{"finish", func(s run.PGRecords) error {
			return s.Finish(ctx, id, t0.Add(time.Minute), run.Finish{State: run.StateSucceeded, InTokens: 10, OutTokens: 4, CostMicroC: 700})
		}, run.StateSucceeded, nil},
		{"finish twice", func(s run.PGRecords) error {
			return s.Finish(ctx, id, t0, run.Finish{State: run.StateFailed, ErrorCode: "internal"})
		}, run.StateSucceeded, run.ErrRecordNotFound},
		{"finish with a bad state", func(s run.PGRecords) error {
			return s.Finish(ctx, id, t0, run.Finish{State: "waiting"})
		}, run.StateSucceeded, nil},
	}
	tbl := &recordTable{rows: map[string]*run.Record{}}
	s := run.PGRecords{DB: recordDB{tbl}}
	if err := s.Create(ctx, run.Record{ID: id, FeatureID: "F-DL-01", SkillID: "f-dl-01",
		ActingFor: "0190d000-0000-7000-8000-000000000001", Grant: []string{"agent.ask"}, CreatedAt: t0}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.step(s)
			if tt.err != nil && !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.err == nil && tt.name != "finish with a bad state" && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tt.name == "finish with a bad state" && err == nil {
				t.Fatal("bad state accepted")
			}
			got, err := s.Get(ctx, id)
			if err != nil || got.State != tt.state {
				t.Fatalf("Get = %+v, %v; want state %s", got, err, tt.state)
			}
		})
	}
	got, _ := s.Get(ctx, id)
	if got.InTokens != 10 || got.OutTokens != 4 || got.CostMicroC != 700 || got.EndedAt == nil {
		t.Fatalf("usage not recorded: %+v", got)
	}
	if _, err := s.Get(ctx, "0190e000-0000-7000-8000-0000000000ff"); !errors.Is(err, run.ErrRecordNotFound) {
		t.Fatalf("Get unknown: %v, want ErrRecordNotFound", err)
	}
	for _, sql := range tbl.sqls {
		for _, col := range []string{"prompt", "input ", "inputs", "answer", "goal", "message", "completion"} {
			if strings.Contains(sql, col) {
				t.Fatalf("statement names %q: %s", col, sql)
			}
		}
	}
}
