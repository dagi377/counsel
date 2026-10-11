package retention

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Guard refuses purge work on any class marked NeverPurge. Which classes are never purged
// is app data registered in Classes, not a kernel constant. Purge jobs call the guard before they propose or apply anything.
type Guard struct {
	Classes Classes
}

// CheckClass returns ErrNeverPurge for a never-purge class and ErrUnknownClass for a
// class that is not registered; an unknown class is refused, never assumed purgeable.
func (g Guard) CheckClass(id string) error {
	c, err := g.Classes.Class(id)
	if err != nil {
		return fmt.Errorf("purge guard: %w", err)
	}
	// rule: ARC-U-013
	if c.NeverPurge {
		return fmt.Errorf("purge guard: class %s: %w", c.ID, ErrNeverPurge)
	}
	return nil
}

// CheckList refuses a purge list whose class is never purged.
func (g Guard) CheckList(l PurgeList) error {
	if err := g.CheckClass(l.Class); err != nil {
		return fmt.Errorf("purge list %s: %w", l.ID, err)
	}
	return nil
}

// CheckRecords refuses the whole batch if any record belongs to a never-purge or unknown
// class, so a mixed batch cannot slip a protected record through.
func (g Guard) CheckRecords(rs []Record) error {
	for _, r := range rs {
		if err := g.CheckClass(r.Class); err != nil {
			return fmt.Errorf("record %s: %w", r.ID, err)
		}
	}
	return nil
}

// GuardedEngine wraps an Engine so Propose refuses never-purge classes before the inner
// engine runs. Apply only receives approved lists that Propose produced, and passes through.
type GuardedEngine struct {
	Guard Guard
	Inner Engine
}

var _ Engine = GuardedEngine{}

// Propose implements Engine.
func (e GuardedEngine) Propose(ctx context.Context, class string) (PurgeList, error) {
	// rule: ARC-U-013
	if err := e.Guard.CheckClass(class); err != nil {
		return PurgeList{}, fmt.Errorf("propose: %w", err)
	}
	return e.Inner.Propose(ctx, class)
}

// Apply implements Engine.
func (e GuardedEngine) Apply(ctx context.Context, tx pgx.Tx, listID, gateID string) error {
	return e.Inner.Apply(ctx, tx, listID, gateID)
}
