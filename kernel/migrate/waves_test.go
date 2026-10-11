package migrate

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

type fakeRehearsals map[string]Rehearsal // keyed by cell id

func (f fakeRehearsals) Rehearsal(_ context.Context, migration, cell string) (Rehearsal, bool, error) {
	r, ok := f[cell]
	if !ok || r.Migration != migration {
		return Rehearsal{}, false, nil
	}
	return r, true, nil
}

// fakeCells records migrations and fails invariants on chosen cells.
type fakeCells struct {
	migrated  []string
	failInv   map[string]bool
	failApply map[string]bool
}

func (f *fakeCells) Migrate(_ context.Context, c Cell, _ Migration) error {
	if f.failApply[c.ID] {
		return errors.New("apply failed")
	}
	f.migrated = append(f.migrated, c.ID)
	return nil
}

func (f *fakeCells) Check(_ context.Context, c Cell) error {
	if f.failInv[c.ID] {
		return errors.New("invariant VC-INV-01 failed")
	}
	return nil
}

var testMigration = Migration{ID: "0042_work_subject", Budget: 120 * time.Second}

// fleet returns an internal cell, a canary and n-2 other cells (cell-01..), deliberately
// out of order.
func fleet(n int) []Cell {
	var cells []Cell
	for i := n - 2; i >= 1; i-- {
		cells = append(cells, Cell{ID: fmt.Sprintf("cell-%02d", i)})
		if i == (n-2)/2 {
			cells = append(cells, Cell{ID: "internal", Internal: true})
		}
	}
	return append(cells, Cell{ID: "canary", Canary: true})
}

func passing(cells []Cell) fakeRehearsals {
	out := fakeRehearsals{}
	for _, c := range cells {
		out[c.ID] = Rehearsal{Migration: testMigration.ID, Cell: c.ID, Anonymized: true, InvariantsPassed: true, Duration: 41 * time.Second}
	}
	return out
}

func waveIDs(ws []Wave) [][]string {
	var out [][]string
	for _, w := range ws {
		var ids []string
		for _, c := range w.Cells {
			ids = append(ids, c.ID)
		}
		out = append(out, ids)
	}
	return out
}

// rule: MIG-065
func TestMIG_065_Plan(t *testing.T) {
	tests := []struct {
		name    string
		cells   []Cell
		want    [][]string
		wantErr error
	}{
		{"internal, canary, rest sorted", fleet(5), [][]string{{"internal"}, {"canary"}, {"cell-01", "cell-02", "cell-03"}}, nil},
		{"two cells", []Cell{{ID: "c", Canary: true}, {ID: "i", Internal: true}}, [][]string{{"i"}, {"c"}}, nil},
		{"no internal", []Cell{{ID: "c", Canary: true}, {ID: "a"}}, nil, ErrBadPlan},
		{"no canary", []Cell{{ID: "i", Internal: true}, {ID: "a"}}, nil, ErrBadPlan},
		{"two canaries", []Cell{{ID: "i", Internal: true}, {ID: "a", Canary: true}, {ID: "b", Canary: true}}, nil, ErrBadPlan},
		{"both flags", []Cell{{ID: "i", Internal: true, Canary: true}}, nil, ErrBadPlan},
		{"duplicate id", []Cell{{ID: "i", Internal: true}, {ID: "c", Canary: true}, {ID: "c"}}, nil, ErrBadPlan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws, err := Plan(tt.cells)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if got := waveIDs(ws); fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("waves = %v, want %v", got, tt.want)
			}
		})
	}
}

// rule: MIG-065
func TestMIG_065_CanaryHalts(t *testing.T) {
	const n = 10
	tests := []struct {
		name      string
		failInv   string
		failApply string
		wantErr   error
		wantDone  []string
		wantHalt  string
	}{
		{"all pass", "", "", nil, []string{"internal", "canary", "cell-01", "cell-02", "cell-03", "cell-04", "cell-05", "cell-06", "cell-07", "cell-08"}, ""},
		{"canary invariant fails", "canary", "", ErrInvariantsFail, []string{"internal"}, "canary"},
		{"internal invariant fails", "internal", "", ErrInvariantsFail, nil, "internal"},
		{"canary migration fails", "", "canary", ErrMigrateFailed, []string{"internal"}, "canary"},
		{"last wave halts mid-wave", "cell-05", "", ErrInvariantsFail, []string{"internal", "canary", "cell-01", "cell-02", "cell-03", "cell-04"}, "cell-05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := fleet(n)
			fc := &fakeCells{failInv: map[string]bool{tt.failInv: true}, failApply: map[string]bool{tt.failApply: true}}
			r := Rollout{Rehearsals: passing(cells), Migrator: fc, Invariants: fc}
			rep, err := r.Run(context.Background(), testMigration, cells)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(rep.Migrated, tt.wantDone) {
				t.Errorf("verified = %v, want %v", rep.Migrated, tt.wantDone)
			}
			if rep.HaltedAt != tt.wantHalt {
				t.Errorf("halted at %q, want %q", rep.HaltedAt, tt.wantHalt)
			}
			if tt.name == "canary invariant fails" {
				// 0 of the N-2 remaining cells were touched.
				for _, id := range fc.migrated {
					if id != "internal" && id != "canary" {
						t.Errorf("cell %s migrated after the canary failed", id)
					}
				}
				if rest := n - 2 - countRest(fc.migrated); rest != n-2 {
					t.Errorf("remaining cells untouched = %d, want %d", rest, n-2)
				}
			}
		})
	}
}

func countRest(ids []string) int {
	k := 0
	for _, id := range ids {
		if id != "internal" && id != "canary" {
			k++
		}
	}
	return k
}

// rule: MIG-064
func TestMIG_064_RehearsalRequired(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(fakeRehearsals)
		m       Migration
		wantErr error
	}{
		{"all rehearsed", func(fakeRehearsals) {}, testMigration, nil},
		{"one cell never rehearsed", func(f fakeRehearsals) { delete(f, "cell-02") }, testMigration, ErrNotRehearsed},
		{"rehearsal of another migration", func(f fakeRehearsals) { r := f["canary"]; r.Migration = "0041_other"; f["canary"] = r }, testMigration, ErrNotRehearsed},
		{"snapshot not anonymized", func(f fakeRehearsals) { r := f["canary"]; r.Anonymized = false; f["canary"] = r }, testMigration, ErrNotRehearsed},
		{"invariants failed on snapshot", func(f fakeRehearsals) { r := f["internal"]; r.InvariantsPassed = false; f["internal"] = r }, testMigration, ErrNotRehearsed},
		{"over budget", func(f fakeRehearsals) { r := f["cell-03"]; r.Duration = 121 * time.Second; f["cell-03"] = r }, testMigration, ErrNotRehearsed},
		{"exactly at budget", func(f fakeRehearsals) { r := f["cell-03"]; r.Duration = 120 * time.Second; f["cell-03"] = r }, testMigration, ErrNotRehearsed},
		{"no budget in header", func(fakeRehearsals) {}, Migration{ID: testMigration.ID}, ErrBadMigration},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cells := fleet(5)
			reh := passing(cells)
			tt.edit(reh)
			fc := &fakeCells{}
			rep, err := Rollout{Rehearsals: reh, Migrator: fc, Invariants: fc}.Run(context.Background(), tt.m, cells)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && (len(fc.migrated) != 0 || len(rep.Migrated) != 0) {
				t.Errorf("refused rollout still migrated %v", fc.migrated)
			}
			if tt.wantErr == nil && len(fc.migrated) != len(cells) {
				t.Errorf("migrated %d of %d cells", len(fc.migrated), len(cells))
			}
		})
	}
}
