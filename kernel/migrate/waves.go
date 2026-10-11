package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// Sentinel errors for cell rollout.
var (
	ErrBadPlan        = errors.New("migrate: rollout needs exactly one internal cell and one canary cell")
	ErrBadMigration   = errors.New("migrate: migration has no id or no duration budget")
	ErrNotRehearsed   = errors.New("migrate: migration has no passing snapshot rehearsal")
	ErrMigrateFailed  = errors.New("migrate: cell migration failed")
	ErrInvariantsFail = errors.New("migrate: post-migration invariants failed")
)

// Cell is one deployment cell. Exactly one cell is Internal (run by the operator) and exactly
// one is the Canary; every other cell rolls out in the last wave.
type Cell struct {
	ID       string
	Internal bool
	Canary   bool
}

// Wave is a set of cells migrated together; wave N+1 starts only after wave N passes.
type Wave struct {
	Index int
	Cells []Cell
}

// Migration is one schema change. Budget is the duration budget recorded in its header.
type Migration struct {
	ID     string
	Budget time.Duration
}

// Rehearsal is the recorded result of running a migration against one cell's anonymized
// snapshot (VC-INV is the invariant suite).
type Rehearsal struct {
	Migration        string
	Cell             string
	Anonymized       bool          // the snapshot passed the anonymizer check
	InvariantsPassed bool          // VC-INV passed on the snapshot after the migration
	Duration         time.Duration // how long the migration ran on the snapshot
}

// Rehearsals looks up the rehearsal of a migration on a cell; ok=false when there is none.
type Rehearsals interface {
	Rehearsal(ctx context.Context, migration, cell string) (r Rehearsal, ok bool, err error)
}

// Migrator applies a migration to one cell.
type Migrator interface {
	Migrate(ctx context.Context, cell Cell, m Migration) error
}

// Invariants runs the post-migration invariant suite (VC-INV) on one cell.
type Invariants interface {
	Check(ctx context.Context, cell Cell) error
}

// Rollout migrates cells wave by wave.
type Rollout struct {
	Rehearsals Rehearsals
	Migrator   Migrator
	Invariants Invariants
}

// Report says how far a rollout got.
type Report struct {
	Waves    []Wave
	Migrated []string // cell ids migrated and verified, in order
	HaltedAt string   // cell that failed; empty when the rollout completed
}

// Plan orders cells into waves: the internal cell, then one canary, then the rest (sorted by
// id so the order is stable).
//
// rule: MIG-065
func Plan(cells []Cell) ([]Wave, error) {
	var internal, canary, rest []Cell
	seen := map[string]bool{}
	for _, c := range cells {
		if c.ID == "" || seen[c.ID] {
			return nil, fmt.Errorf("%w: empty or duplicate cell id %q", ErrBadPlan, c.ID)
		}
		seen[c.ID] = true
		switch {
		case c.Internal && c.Canary:
			return nil, fmt.Errorf("%w: cell %s is both internal and canary", ErrBadPlan, c.ID)
		case c.Internal:
			internal = append(internal, c)
		case c.Canary:
			canary = append(canary, c)
		default:
			rest = append(rest, c)
		}
	}
	if len(internal) != 1 || len(canary) != 1 {
		return nil, fmt.Errorf("%w: got %d internal, %d canary", ErrBadPlan, len(internal), len(canary))
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].ID < rest[j].ID })
	waves := []Wave{{Index: 0, Cells: internal}, {Index: 1, Cells: canary}}
	if len(rest) > 0 {
		waves = append(waves, Wave{Index: 2, Cells: rest})
	}
	return waves, nil
}

// Run checks every cell's rehearsal, then migrates wave by wave. A cell whose migration or
// invariants fail halts the rollout: no later cell, in this wave or any later one, is touched.
func (r Rollout) Run(ctx context.Context, m Migration, cells []Cell) (Report, error) {
	if r.Rehearsals == nil || r.Migrator == nil || r.Invariants == nil {
		return Report{}, fmt.Errorf("rollout %q: %w: Rehearsals, Migrator and Invariants are required", m.ID, ErrBadPlan)
	}
	if m.ID == "" || m.Budget <= 0 {
		return Report{}, fmt.Errorf("rollout %q: %w", m.ID, ErrBadMigration)
	}
	waves, err := Plan(cells)
	if err != nil {
		return Report{}, fmt.Errorf("rollout %s: %w", m.ID, err)
	}
	rep := Report{Waves: waves}
	// rule: MIG-064
	// Every cell needs a passing rehearsal before wave 0 starts.
	for _, w := range waves {
		for _, c := range w.Cells {
			if err := r.rehearsed(ctx, m, c); err != nil {
				return rep, fmt.Errorf("rollout %s: %w", m.ID, err)
			}
		}
	}
	// rule: MIG-065
	for _, w := range waves {
		for _, c := range w.Cells {
			if err := ctx.Err(); err != nil {
				return rep, fmt.Errorf("rollout %s: %w", m.ID, err)
			}
			if err := r.Migrator.Migrate(ctx, c, m); err != nil {
				rep.HaltedAt = c.ID
				return rep, fmt.Errorf("rollout %s: wave %d cell %s: %w: %w", m.ID, w.Index, c.ID, ErrMigrateFailed, err)
			}
			if err := r.Invariants.Check(ctx, c); err != nil {
				rep.HaltedAt = c.ID
				slog.Error("rollout halted", "migration", m.ID, "wave", w.Index, "cell", c.ID)
				return rep, fmt.Errorf("rollout %s: wave %d cell %s: %w: %w", m.ID, w.Index, c.ID, ErrInvariantsFail, err)
			}
			rep.Migrated = append(rep.Migrated, c.ID)
		}
		slog.Info("rollout wave passed", "migration", m.ID, "wave", w.Index, "cells", len(w.Cells))
	}
	return rep, nil
}

// rehearsed reports whether m has a passing rehearsal on cell c: anonymized snapshot,
// invariants passed, and a duration under the migration's budget.
//
// rule: MIG-064
func (r Rollout) rehearsed(ctx context.Context, m Migration, c Cell) error {
	reh, ok, err := r.Rehearsals.Rehearsal(ctx, m.ID, c.ID)
	if err != nil {
		return fmt.Errorf("cell %s: load rehearsal: %w", c.ID, err)
	}
	switch {
	case !ok:
		return fmt.Errorf("cell %s: %w: none recorded", c.ID, ErrNotRehearsed)
	case !reh.Anonymized:
		return fmt.Errorf("cell %s: %w: snapshot not anonymized", c.ID, ErrNotRehearsed)
	case !reh.InvariantsPassed:
		return fmt.Errorf("cell %s: %w: invariants failed", c.ID, ErrNotRehearsed)
	case reh.Duration <= 0 || reh.Duration >= m.Budget:
		return fmt.Errorf("cell %s: %w: ran %s, budget %s", c.ID, ErrNotRehearsed, reh.Duration, m.Budget)
	}
	return nil
}
