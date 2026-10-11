package conflict

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/db"
)

// Scripted tables for PGRuns. A check "chk-X" is on matter X; a check party "cp-X" is on
// check "chk-X". Ids without those prefixes do not exist.
type fakeRunRow struct {
	id, check, prep string
	version         int32
	index           int64
	ranAt           time.Time
	complete        bool
	terms           []string
}

type fakeHitRow struct {
	run, term, matched, matter, role, party, kind, typ string
	score, ordinal                                     int32
}

type runTables struct {
	runs []fakeRunRow
	hits []fakeHitRow
}

func (t runTables) clone() runTables {
	return runTables{runs: append([]fakeRunRow(nil), t.runs...), hits: append([]fakeHitRow(nil), t.hits...)}
}

func checkMatter(id string) (string, bool) {
	if !strings.HasPrefix(id, "chk-") {
		return "", false
	}
	return strings.TrimPrefix(id, "chk-"), true
}

// runsTx answers the PGRuns statements over runTables; unknown statements fail the call.
type runsTx struct {
	pgx.Tx
	t       *runTables
	failSQL string // a statement that fails with errDown
}

var errDown = errors.New("connection reset")

func (x *runsTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	if sql == x.failSQL {
		return scripted{err: errDown}
	}
	switch sql {
	case runLockCheckSQL:
		m, ok := checkMatter(args[0].(string))
		if !ok {
			return scripted{err: pgx.ErrNoRows}
		}
		return scripted{vals: []any{m}}
	case runNextSQL:
		var v int
		for _, r := range x.t.runs {
			if r.check == args[0] && int(r.version) > v {
				v = int(r.version)
			}
		}
		return scripted{vals: []any{v + 1}}
	case runInsertSQL:
		check, version := args[0].(string), int32(args[1].(int))
		for _, r := range x.t.runs {
			if r.check == check && r.version == version {
				return scripted{err: &pgconn.PgError{Code: "23505"}}
			}
		}
		id := fmt.Sprintf("r%d", len(x.t.runs)+1)
		x.t.runs = append(x.t.runs, fakeRunRow{id: id, check: check, version: version, index: args[2].(int64),
			ranAt: args[3].(time.Time), complete: args[4].(bool), prep: args[5].(string), terms: append([]string(nil), args[6].([]string)...)})
		return scripted{vals: []any{id}}
	case runByIDSQL:
		for _, r := range x.t.runs {
			if r.id == args[0] {
				return scripted{vals: runVals(r)}
			}
		}
		return scripted{err: pgx.ErrNoRows}
	}
	return scripted{err: fmt.Errorf("unexpected query %q", sql)}
}

func runVals(r fakeRunRow) []any {
	m, _ := checkMatter(r.check)
	return []any{r.id, m, r.check, r.version, r.index, r.ranAt, r.complete, r.prep, append([]string(nil), r.terms...)}
}

func (x *runsTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	if sql == x.failSQL {
		return nil, errDown
	}
	switch sql {
	case runsByMatterSQL:
		var out [][]any
		for _, r := range x.t.runs {
			if m, _ := checkMatter(r.check); m == args[0] {
				out = append(out, runVals(r))
			}
		}
		return &scriptedRows{rows: out}, nil
	case hitsSQL:
		var out [][]any
		for _, h := range x.t.hits {
			if h.run == args[0] {
				o := h.ordinal
				out = append(out, []any{h.term, h.matched, h.matter, h.role, h.party, h.kind, h.typ, h.score, &o})
			}
		}
		return &scriptedRows{rows: out}, nil
	}
	return nil, fmt.Errorf("unexpected query %q", sql)
}

func (x *runsTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if sql == x.failSQL {
		return pgconn.CommandTag{}, errDown
	}
	if sql != hitInsertSQL {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec %q", sql)
	}
	party, check := args[1].(string), args[10].(string)
	if !strings.HasPrefix(party, "cp-") || "chk-"+strings.TrimPrefix(party, "cp-") != check {
		return pgconn.NewCommandTag("INSERT 0 0"), nil
	}
	x.t.hits = append(x.t.hits, fakeHitRow{run: args[0].(string), party: party, kind: args[2].(string), matched: args[3].(string),
		typ: args[4].(string), score: int32(args[5].(int64)), ordinal: int32(args[6].(int)), term: args[7].(string),
		matter: args[8].(string), role: args[9].(string)})
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

// runsDB is a db.DB whose InTx commits the scripted tables only when fn succeeds.
type runsDB struct {
	db.DB
	t       *runTables
	failSQL string
}

func (d *runsDB) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	staged := d.t.clone()
	if err := fn(ctx, &runsTx{t: &staged, failSQL: d.failSQL}); err != nil {
		return err
	}
	*d.t = staged
	return nil
}

type scripted struct {
	vals []any
	err  error
}

func (r scripted) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return scanInto(r.vals, dest)
}

func scanInto(vals, dest []any) error {
	if len(vals) != len(dest) {
		return fmt.Errorf("scan: %d dest for %d columns", len(dest), len(vals))
	}
	for i, v := range vals {
		switch d := dest[i].(type) {
		case *string:
			*d = v.(string)
		case *int:
			*d = v.(int)
		case *int32:
			*d = v.(int32)
		case *int64:
			*d = v.(int64)
		case *bool:
			*d = v.(bool)
		case *time.Time:
			*d = v.(time.Time)
		case *[]string:
			*d = v.([]string)
		case **int32:
			*d = v.(*int32)
		default:
			return fmt.Errorf("scan: unsupported dest %T", dest[i])
		}
	}
	return nil
}

type scriptedRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *scriptedRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *scriptedRows) Scan(dest ...any) error { return scanInto(r.rows[r.i-1], dest) }
func (r *scriptedRows) Close()                 {}
func (r *scriptedRows) Err() error             { return nil }

// enriched is a Searcher that fills the values the party index gives a real hit: the check
// party of the run's check, the match kind and type, the score and the index version.
type enriched struct{ s Searcher }

func (e enriched) Search(ctx context.Context, term string) ([]Hit, error) {
	hits, err := e.s.Search(ctx, term)
	out := make([]Hit, len(hits))
	for i, h := range hits {
		h.MatchedKind, h.MatchType, h.ScorePPM = "party_role", "exact", 1_000_000
		out[i] = h
	}
	return out, err
}

func (enriched) IndexVersion(context.Context) (int64, error) { return 7, nil }

// checkFor is a RunStore that puts each run on its matter's check ("chk-<matter>") and each
// hit on that check's party, as the intake caller and the party index do, before PGRuns.
type checkFor struct{ *PGRuns }

func (c checkFor) Append(ctx context.Context, r Run) (Run, error) {
	r.CheckID = "chk-" + r.MatterID
	for i := range r.Hits {
		r.Hits[i].CheckPartyID = "cp-" + r.MatterID
	}
	return c.PGRuns.Append(ctx, r)
}

// The check_test.go cases, run again with PGRuns as the run store.
func TestChecksCasesOnPGRuns(t *testing.T) {
	mem := checksOn
	t.Cleanup(func() { checksOn = mem })
	checksOn = func(s Searcher) *Checks {
		store := checkFor{NewPGRuns(&runsDB{t: &runTables{}})}
		return NewChecksWith(enriched{s}, store, func() time.Time { return time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC) })
	}
	for _, tt := range []struct {
		name string
		fn   func(*testing.T)
	}{
		{"TestCheck", TestCheck},
		{"TestRunsAppendOnly", TestRunsAppendOnly},
		{"TestDecideG2", TestDecideG2},
		{"TestG2ClearNeedsEveryHitDisposed", TestG2ClearNeedsEveryHitDisposed},
		{"TestDisposeChecks", TestDisposeChecks},
	} {
		t.Run(tt.name, tt.fn)
	}
}

func validTestRun() Run {
	return Run{
		CheckID: "chk-m1", MatterID: "m1", Terms: []string{"acme"}, PreparedBy: "u-prep", IndexVersion: 7, Complete: true,
		RanAt: time.Date(2026, 3, 2, 14, 0, 0, 0, time.UTC),
		Hits: []Hit{{Term: "acme", PartyID: "p1", MatterID: "m9", Role: "adverse", CheckPartyID: "cp-m1",
			MatchedKind: "party_role", MatchType: "fuzzy", ScorePPM: 870_000}},
	}
}

func TestPGRunsAppend(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*Run)
		failSQL     string
		wantErr     error
		wantVersion int
	}{
		{name: "first run is version 1", wantVersion: 1},
		{name: "explicit version is kept", mutate: func(r *Run) { r.Version = 4 }, wantVersion: 4},
		{name: "no check record", mutate: func(r *Run) { r.CheckID = "" }, wantErr: ErrInvalidRun},
		{name: "unknown check record", mutate: func(r *Run) { r.CheckID = "x" }, wantErr: ErrNotFound},
		{name: "check on another matter", mutate: func(r *Run) { r.MatterID = "m2" }, wantErr: ErrInvalidRun},
		{name: "no index version", mutate: func(r *Run) { r.IndexVersion = 0 }, wantErr: ErrInvalidRun},
		{name: "incomplete run", mutate: func(r *Run) { r.Complete = false }, wantErr: ErrInvalidRun},
		{name: "no preparer", mutate: func(r *Run) { r.PreparedBy = "" }, wantErr: ErrInvalidRun},
		{name: "no terms", mutate: func(r *Run) { r.Terms = nil }, wantErr: ErrInvalidRun},
		{name: "hit without check party", mutate: func(r *Run) { r.Hits[0].CheckPartyID = "" }, wantErr: ErrInvalidRun},
		{name: "hit party of another check", mutate: func(r *Run) { r.Hits[0].CheckPartyID = "cp-m2" }, wantErr: ErrInvalidRun},
		{name: "hit without matched party", mutate: func(r *Run) { r.Hits[0].PartyID = "" }, wantErr: ErrInvalidRun},
		{name: "hit with unknown match kind", mutate: func(r *Run) { r.Hits[0].MatchedKind = "guess" }, wantErr: ErrInvalidRun},
		{name: "hit without match type", mutate: func(r *Run) { r.Hits[0].MatchType = "" }, wantErr: ErrInvalidRun},
		{name: "hit without score", mutate: func(r *Run) { r.Hits[0].ScorePPM = 0 }, wantErr: ErrInvalidRun},
		{name: "hit score above 1000000 ppm", mutate: func(r *Run) { r.Hits[0].ScorePPM = 1_000_001 }, wantErr: ErrInvalidRun},
		{name: "insert error rolls back", failSQL: hitInsertSQL, wantErr: errDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := &runTables{}
			s := NewPGRuns(&runsDB{t: tables, failSQL: tt.failSQL})
			r := validTestRun()
			if tt.mutate != nil {
				tt.mutate(&r)
			}
			got, err := s.Append(context.Background(), r)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if len(tables.runs) != 0 || len(tables.hits) != 0 {
					t.Fatalf("tables = %+v, want nothing written", tables)
				}
				return
			}
			if got.Version != tt.wantVersion || got.ID == "" {
				t.Fatalf("run = %+v, want version %d", got, tt.wantVersion)
			}
			back, err := s.Get(context.Background(), got.ID)
			if err != nil {
				t.Fatal(err)
			}
			if back.Version != tt.wantVersion || back.IndexVersion != 7 || len(back.Hits) != 1 || back.Hits[0] != r.Hits[0] ||
				back.PreparedBy != "u-prep" || back.MatterID != "m1" || back.Terms[0] != "acme" {
				t.Fatalf("Get = %+v, want the appended run", back)
			}
		})
	}
}

// A second Append of the same (check, version) is ErrRunExists and nothing is overwritten.
func TestPGRunsAppendSameVersion(t *testing.T) {
	ctx := context.Background()
	tables := &runTables{}
	s := NewPGRuns(&runsDB{t: tables})
	first := validTestRun()
	first.Version = 1
	if _, err := s.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		mutate  func(*Run)
		wantErr error
	}{
		{"same version again", func(r *Run) { r.IndexVersion = 8; r.Hits[0].ScorePPM = 1 }, ErrRunExists},
		{"same version, no hits", func(r *Run) { r.Hits = nil }, ErrRunExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := validTestRun()
			r.Version = 1
			tt.mutate(&r)
			if _, err := s.Append(ctx, r); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			runs, err := s.Runs(ctx, "m1")
			if err != nil || len(runs) != 1 || runs[0].IndexVersion != 7 || len(runs[0].Hits) != 1 || runs[0].Hits[0].ScorePPM != 870_000 {
				t.Fatalf("runs = %+v, %v; want the first run unchanged", runs, err)
			}
		})
	}
	// The in-memory store refuses the same (check, version) too.
	m := &memRuns{}
	if _, err := m.Append(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Append(ctx, first); !errors.Is(err, ErrRunExists) {
		t.Fatalf("memRuns second append: %v, want ErrRunExists", err)
	}
}

func TestPGRunsRead(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		tables  runTables
		failSQL string
		id      string
		wantErr error
	}{
		{name: "unknown run", id: "r9", wantErr: ErrNotFound},
		{name: "run without preparer fails closed", id: "r1",
			tables: runTables{runs: []fakeRunRow{{id: "r1", check: "chk-m1", version: 1, index: 1, complete: true}}}, wantErr: ErrInvalidRun},
		{name: "gap in hit order fails closed", id: "r1",
			tables: runTables{runs: []fakeRunRow{{id: "r1", check: "chk-m1", version: 1, index: 1, complete: true, prep: "u"}},
				hits: []fakeHitRow{{run: "r1", ordinal: 1}}}, wantErr: ErrInvalidRun},
		{name: "read error", id: "r1", failSQL: runByIDSQL, wantErr: errDown},
		{name: "hits read error", id: "r1", failSQL: hitsSQL,
			tables: runTables{runs: []fakeRunRow{{id: "r1", check: "chk-m1", version: 1, index: 1, complete: true, prep: "u"}}}, wantErr: errDown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tables := tt.tables
			s := NewPGRuns(&runsDB{t: &tables, failSQL: tt.failSQL})
			if _, err := s.Get(ctx, tt.id); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Get err = %v, want %v", err, tt.wantErr)
			}
			if tt.failSQL == "" && tt.wantErr != ErrNotFound {
				if _, err := s.Runs(ctx, "m1"); !errors.Is(err, tt.wantErr) {
					t.Fatalf("Runs err = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}
