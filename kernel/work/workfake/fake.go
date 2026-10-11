// Package workfake is an in-memory kernel/work Service and Automations for tests. It ignores
// the transaction argument. No network, Postgres or Docker.
package workfake

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/work"
)

var (
	_ work.Service     = (*Fake)(nil)
	_ work.Automations = (*Fake)(nil)
)

type move struct{ wf, from, to string }

// Fake is a concurrency-safe in-memory work store.
type Fake struct {
	mu          sync.Mutex
	seq         int
	items       map[work.ItemID]*work.Item
	order       []work.ItemID
	transitions map[move]bool
	autos       map[string]work.Automation
	autoOrder   []string
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{
		items:       map[work.ItemID]*work.Item{},
		transitions: map[move]bool{},
		autos:       map[string]work.Automation{},
	}
}

// AddTransition allows a move in a workflow.
func (f *Fake) AddTransition(t work.Transition) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.transitions[move{t.WorkflowID, t.From, t.To}] = true
}

func validate(it work.Item) error {
	if it.Title == "" || it.Type == "" || it.WorkflowID == "" || it.StatusID == "" || it.ReporterID == "" ||
		it.Priority < 0 || it.Priority > 5 {
		return work.ErrInvalidItem
	}
	return nil
}

// Create implements work.Service.
func (f *Fake) Create(ctx context.Context, _ pgx.Tx, it work.Item) (work.ItemID, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validate(it); err != nil {
		return "", fmt.Errorf("create: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	it.ID = work.ItemID("item-" + strconv.Itoa(f.seq))
	if it.Key == "" {
		it.Key = "W-" + strconv.Itoa(f.seq)
	}
	if it.Priority == 0 {
		it.Priority = 3
	}
	it.Labels = append([]string(nil), it.Labels...)
	it.Version = 1
	f.items[it.ID] = &it
	f.order = append(f.order, it.ID)
	return it.ID, nil
}

// Get implements work.Service.
func (f *Fake) Get(ctx context.Context, id work.ItemID) (work.Item, error) {
	if err := ctx.Err(); err != nil {
		return work.Item{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		return work.Item{}, fmt.Errorf("get %s: %w", id, work.ErrNotFound)
	}
	return *it, nil
}

// List implements work.Service.
func (f *Fake) List(ctx context.Context, flt work.Filter) ([]work.Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []work.Item
	for _, id := range f.order {
		it := f.items[id]
		if (flt.Subject.Kind != "" && it.Subject != flt.Subject) ||
			(flt.AssigneeUserID != "" && it.AssigneeUserID != flt.AssigneeUserID) ||
			(flt.StatusID != "" && it.StatusID != flt.StatusID) {
			continue
		}
		if flt.Limit > 0 && len(out) >= flt.Limit {
			break
		}
		out = append(out, *it)
	}
	return out, nil
}

// Move implements work.Service.
func (f *Fake) Move(ctx context.Context, _ pgx.Tx, id work.ItemID, version int, toStatusID string, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		return fmt.Errorf("move %s: %w", id, work.ErrNotFound)
	}
	if it.Version != version {
		return fmt.Errorf("move %s: %w", id, work.ErrStale)
	}
	if !f.transitions[move{it.WorkflowID, it.StatusID, toStatusID}] {
		return fmt.Errorf("move %s: %w", id, work.ErrTransitionNotAllowed)
	}
	it.StatusID = toStatusID
	it.Version++
	return nil
}

// SetApprover implements work.Service.
func (f *Fake) SetApprover(ctx context.Context, _ pgx.Tx, id work.ItemID, approverID string, approverIsAgent bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.items[id]
	if !ok {
		return fmt.Errorf("set approver %s: %w", id, work.ErrNotFound)
	}
	// The preparer (assignee) never approves, and the agent never approves.
	if approverIsAgent || approverID == "" || approverID == it.AssigneeUserID {
		return fmt.Errorf("set approver %s: %w", id, work.ErrSelfApproval)
	}
	it.ApproverID = approverID
	it.Version++
	return nil
}

// Save implements work.Automations.
func (f *Fake) Save(ctx context.Context, _ pgx.Tx, a work.Automation) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if a.Action.HasEffect && a.Action.GateKind == "" {
		return "", fmt.Errorf("save automation: %w", work.ErrEffectNeedsGate)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if a.ID == "" {
		f.seq++
		a.ID = "auto-" + strconv.Itoa(f.seq)
	}
	if _, ok := f.autos[a.ID]; !ok {
		f.autoOrder = append(f.autoOrder, a.ID)
	}
	f.autos[a.ID] = a
	return a.ID, nil
}

// ForTrigger implements work.Automations. Only enabled rules are returned.
func (f *Fake) ForTrigger(ctx context.Context, trigger string) ([]work.Automation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []work.Automation
	for _, id := range f.autoOrder {
		if a := f.autos[id]; a.Enabled && a.Trigger == trigger {
			out = append(out, a)
		}
	}
	return out, nil
}
