package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/jackc/pgx/v5"
)

// Toronto is the zone retention days are counted in.
var Toronto = mustZone("America/Toronto")

func mustZone(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(fmt.Sprintf("retention: load %s: %v", name, err))
	}
	return l
}

// RecordSource lists the retained records of one class.
type RecordSource interface {
	Records(ctx context.Context, class string) ([]Record, error)
}

// ListStore keeps purge lists. Propose only inserts; nothing here destroys records.
type ListStore interface {
	InsertList(ctx context.Context, l PurgeList) error
	List(ctx context.Context, tx pgx.Tx, id string) (PurgeList, error)
}

// GateLookup returns an approved gate by id. A gate that is not approved returns an error.
type GateLookup func(ctx context.Context, tx pgx.Tx, gateID string) (registry.Approved, error)

// DestroyFunc destroys records inside the destruction gate's effect tx.
type DestroyFunc func(ctx context.Context, tx pgx.Tx, class string, recordIDs []string) error

// PurgePayload is the destruction gate payload: the purge list it approves.
type PurgePayload struct {
	ListID string `json:"purge_list_id"`
}

// Purger implements Engine.
type Purger struct {
	Classes Classes
	Records RecordSource
	Lists   ListStore
	Gates   GateLookup
	Destroy DestroyFunc
	Now     func() time.Time
	NewID   func() string
}

var _ Engine = (*Purger)(nil)

// Propose inserts a purge list of the class's due records and destroys nothing. A
// never-purge class is refused and never proposed; held records are left out.
func (p *Purger) Propose(ctx context.Context, class string) (PurgeList, error) {
	c, err := (Guard{Classes: p.Classes}).classFor(class)
	if err != nil {
		return PurgeList{}, fmt.Errorf("propose: %w", err)
	}
	rs, err := p.Records.Records(ctx, class)
	if err != nil {
		return PurgeList{}, fmt.Errorf("propose %s: records: %w", class, err)
	}
	asOf := p.Now().In(Toronto)
	l := PurgeList{ID: p.NewID(), Class: class, AsOf: asOf}
	for _, r := range rs {
		if c.Due(r, asOf) {
			l.Records = append(l.Records, r.ID)
		}
	}
	if err := p.Lists.InsertList(ctx, l); err != nil {
		return PurgeList{}, fmt.Errorf("propose %s: insert list: %w", class, err)
	}
	return l, nil
}

// Apply destroys an approved purge list. gateID must name an approved gate of the class's
// destruction kind whose payload is this list; otherwise ErrNotApproved.
func (p *Purger) Apply(ctx context.Context, tx pgx.Tx, listID, gateID string) error {
	g, err := p.Gates(ctx, tx, gateID)
	if err != nil {
		return fmt.Errorf("apply %s: gate %s: %w: %w", listID, gateID, ErrNotApproved, err)
	}
	return p.apply(ctx, tx, listID, g)
}

// Effect is the destruction gate's effect handler; it is the only path that destroys.
func (p *Purger) Effect() registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		var pl PurgePayload
		if err := json.Unmarshal(g.Payload, &pl); err != nil || pl.ListID == "" {
			return fmt.Errorf("purge effect %s: payload: %w", g.GateID, ErrUnknownList)
		}
		return p.apply(ctx, tx, pl.ListID, g)
	}
}

func (p *Purger) apply(ctx context.Context, tx pgx.Tx, listID string, g registry.Approved) error {
	l, err := p.Lists.List(ctx, tx, listID)
	if err != nil {
		return fmt.Errorf("apply %s: %w", listID, err)
	}
	c, err := (Guard{Classes: p.Classes}).classFor(l.Class)
	if err != nil {
		return fmt.Errorf("apply %s: %w", listID, err)
	}
	var pl PurgePayload
	if g.Kind != c.DestructionGate || json.Unmarshal(g.Payload, &pl) != nil || pl.ListID != listID {
		return fmt.Errorf("apply %s: gate %s: %w", listID, g.GateID, ErrNotApproved)
	}
	if err := p.Destroy(ctx, tx, l.Class, l.Records); err != nil {
		return fmt.Errorf("apply %s: destroy: %w", listID, err)
	}
	return nil
}

// classFor returns a registered class that may be purged.
func (g Guard) classFor(id string) (Class, error) {
	if err := g.CheckClass(id); err != nil {
		return Class{}, err
	}
	c, err := g.Classes.Class(id)
	if err != nil {
		return Class{}, fmt.Errorf("class %s: %w", id, err)
	}
	if c.DestructionGate == "" {
		return Class{}, fmt.Errorf("class %s: no destruction gate: %w", id, ErrInvalidClass)
	}
	return c, nil
}
