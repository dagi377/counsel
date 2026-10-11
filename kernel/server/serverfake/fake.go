// Package serverfake is an in-memory server.Server backed by a chi router; it never listens.
package serverfake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/server"
)

var (
	_ server.Server        = (*Fake)(nil)
	_ server.ProblemWriter = (*Fake)(nil)
)

// Fake records mounted routes and serves them through ServeHTTP in tests.
type Fake struct {
	mu     sync.Mutex
	router chi.Router
	seen   map[server.Route]bool
}

// New returns an empty Fake.
func New() *Fake { return &Fake{router: chi.NewRouter(), seen: map[server.Route]bool{}} }

// Mount applies m to a scratch router first, so a duplicate route leaves the Fake unchanged.
func (f *Fake) Mount(m server.Mounter) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	scratch := chi.NewRouter()
	m(scratch)
	added, err := walk(scratch)
	if err != nil {
		return err
	}
	for _, rt := range added {
		if f.seen[rt] {
			return fmt.Errorf("mount %s %s: %w", rt.Method, rt.Pattern, server.ErrDuplicateRoute)
		}
	}
	m(f.router)
	for _, rt := range added {
		f.seen[rt] = true
	}
	return nil
}

// Routes lists registered routes sorted by pattern then method.
func (f *Fake) Routes() []server.Route {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]server.Route, 0, len(f.seen))
	for rt := range f.seen {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pattern != out[j].Pattern {
			return out[i].Pattern < out[j].Pattern
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// Handler returns the router with no middleware.
func (f *Fake) Handler() http.Handler { return f.router }

// WriteProblem writes p as problem+json.
func (f *Fake) WriteProblem(w http.ResponseWriter, _ *http.Request, p api.Problem) {
	w.Header().Set("Content-Type", api.ProblemContentType)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func walk(r chi.Router) ([]server.Route, error) {
	var out []server.Route
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, server.Route{Method: method, Pattern: route})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk routes: %w", err)
	}
	return out, nil
}
