package trustfake

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
)

var _ trust.Requests = (*Requests)(nil)

// Requests is an in-memory trust.Requests. Calls counts every method call; Fail, when set, is
// returned by Create.
type Requests struct {
	mu    sync.Mutex
	rows  map[trust.RequestID]trust.Request
	n     int
	Calls int
	Fail  error
}

// NewRequests returns an empty Requests.
func NewRequests() *Requests {
	return &Requests{rows: map[trust.RequestID]trust.Request{}}
}

// Create implements trust.Requests: a draft with no approver, as the store writes it.
func (f *Requests) Create(_ context.Context, _ pgx.Tx, r trust.Request, by trust.RequestActor) (trust.RequestID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	if f.Fail != nil {
		return "", f.Fail
	}
	r.PreparedBy, r.PreparedByAgent = by.UserID, by.Agent
	if err := trust.ValidateRequest(r); err != nil {
		return "", fmt.Errorf("create trust request: %w", err)
	}
	f.n++
	r.ID = trust.RequestID(fmt.Sprintf("tr-%04d", f.n))
	r.Status, r.ApprovedBy, r.RowVersion = trust.RequestDraft, "", 1
	f.rows[r.ID] = r
	return r.ID, nil
}

// Get implements trust.Requests.
func (f *Requests) Get(_ context.Context, id trust.RequestID) (trust.Request, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	r, ok := f.rows[id]
	if !ok {
		return trust.Request{}, trust.ErrNotFound
	}
	return r, nil
}

// List implements trust.Requests.
func (f *Requests) List(_ context.Context, flt trust.RequestFilter) ([]trust.Request, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls++
	out := []trust.Request{}
	for _, r := range f.rows {
		if flt.After == "" || r.ID > flt.After {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b trust.Request) int { return strings.Compare(string(a.ID), string(b.ID)) })
	if flt.Limit > 0 && len(out) > flt.Limit {
		out = out[:flt.Limit]
	}
	return out, nil
}
