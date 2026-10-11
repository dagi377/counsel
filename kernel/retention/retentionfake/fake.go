// Package retentionfake is an in-memory kernel/retention for tests: no network, no
// Postgres. Tests stand in for the gate engine by calling Approve.
package retentionfake

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/dagi377/counsel/kernel/retention"
	"github.com/jackc/pgx/v5"
)

var _ retention.Service = (*Fake)(nil)

// Fake holds classes, records and purge lists in memory.
type Fake struct {
	mu       sync.Mutex
	today    func() time.Time
	classes  map[string]retention.Class
	records  map[string]retention.Record
	lists    map[string]retention.PurgeList
	approved map[string]string // list id -> gate id
	next     int
}

// New returns an empty fake; today gives the business date (America/Toronto).
func New(today func() time.Time) *Fake {
	return &Fake{
		today:    today,
		classes:  map[string]retention.Class{},
		records:  map[string]retention.Record{},
		lists:    map[string]retention.PurgeList{},
		approved: map[string]string{},
	}
}

// RegisterClass implements retention.Classes.
func (f *Fake) RegisterClass(c retention.Class) error {
	if c.ID == "" || c.MinYears < 0 || (!c.NeverPurge && c.DestructionGate == "") {
		return fmt.Errorf("register class %s: %w", c.ID, retention.ErrInvalidClass)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.classes[c.ID]; ok {
		return fmt.Errorf("register class %s: %w", c.ID, retention.ErrDuplicateClass)
	}
	f.classes[c.ID] = c
	return nil
}

// Class implements retention.Classes.
func (f *Fake) Class(id string) (retention.Class, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.classes[id]
	if !ok {
		return retention.Class{}, fmt.Errorf("class %s: %w", id, retention.ErrUnknownClass)
	}
	return c, nil
}

// AddRecord stores a retained record.
func (f *Fake) AddRecord(r retention.Record) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[r.ID] = r
}

// Exists reports whether a record is still stored.
func (f *Fake) Exists(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.records[id]
	return ok
}

// Approve records that gateID, the class's destruction gate, approved a purge list.
func (f *Fake) Approve(listID, gateID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.approved[listID] = gateID
}

// Propose implements retention.Engine.
func (f *Fake) Propose(ctx context.Context, class string) (retention.PurgeList, error) {
	if err := ctx.Err(); err != nil {
		return retention.PurgeList{}, fmt.Errorf("propose: %w", err)
	}
	c, err := f.Class(class)
	if err != nil {
		return retention.PurgeList{}, fmt.Errorf("propose: %w", err)
	}
	if c.NeverPurge {
		return retention.PurgeList{}, fmt.Errorf("propose %s: %w", class, retention.ErrNeverPurge)
	}
	asOf := f.today()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	l := retention.PurgeList{ID: "purge-" + strconv.Itoa(f.next), Class: class, AsOf: asOf}
	for _, r := range f.records {
		if r.Class != class || r.Hold || r.ClosedOn.IsZero() {
			continue
		}
		if r.ClosedOn.AddDate(c.MinYears, 0, 0).After(asOf) {
			continue
		}
		l.Records = append(l.Records, r.ID)
	}
	sort.Strings(l.Records)
	f.lists[l.ID] = l
	return l, nil
}

// Apply implements retention.Engine. tx is ignored.
func (f *Fake) Apply(ctx context.Context, _ pgx.Tx, listID, gateID string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.lists[listID]
	if !ok {
		return fmt.Errorf("apply %s: %w", listID, retention.ErrUnknownList)
	}
	c := f.classes[l.Class]
	if got, ok := f.approved[listID]; !ok || got != gateID || gateID != c.DestructionGate {
		return fmt.Errorf("apply %s: %w", listID, retention.ErrNotApproved)
	}
	for _, id := range l.Records {
		delete(f.records, id)
	}
	delete(f.lists, listID)
	return nil
}
