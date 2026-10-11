// Package middlewarefake holds in-memory implementations of the kernel/server/middleware
// interfaces for tests. No network, Postgres or Docker.
package middlewarefake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/dagi377/counsel/kernel/server/middleware"
)

var (
	_ middleware.Authenticator = (*Authenticator)(nil)
	_ middleware.ProblemWriter = (*ProblemWriter)(nil)
	_ middleware.RequestIDs    = (*RequestIDs)(nil)
)

// Authenticator maps bearer tokens to principals.
type Authenticator struct {
	mu     sync.Mutex
	tokens map[string]middleware.Principal
}

// NewAuthenticator returns an Authenticator that knows tokens.
func NewAuthenticator(tokens map[string]middleware.Principal) *Authenticator {
	a := &Authenticator{tokens: map[string]middleware.Principal{}}
	for k, v := range tokens {
		a.tokens[k] = v
	}
	return a
}

// Authenticate implements middleware.Authenticator.
func (a *Authenticator) Authenticate(r *http.Request) (middleware.Principal, error) {
	tok, ok := strings.CutPrefix(r.Header.Get(middleware.HeaderAuthorization), "Bearer ")
	if !ok || tok == "" {
		return middleware.Principal{}, fmt.Errorf("authenticate: %w", middleware.ErrUnauthenticated)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.tokens[tok]
	if !ok {
		return middleware.Principal{}, fmt.Errorf("authenticate: %w", middleware.ErrUnauthenticated)
	}
	return p, nil
}

// ProblemWriter writes problem+json and remembers each problem it wrote.
type ProblemWriter struct {
	mu      sync.Mutex
	Written []middleware.Problem
}

// WriteProblem implements middleware.ProblemWriter.
func (pw *ProblemWriter) WriteProblem(w http.ResponseWriter, r *http.Request, p middleware.Problem) {
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	if p.RequestID == "" {
		p.RequestID = r.Header.Get(middleware.HeaderRequestID)
	}
	pw.mu.Lock()
	pw.Written = append(pw.Written, p)
	pw.mu.Unlock()
	w.Header().Set("Content-Type", middleware.ProblemContentType)
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// RequestIDs mints sequential ids "req-1", "req-2", ...
type RequestIDs struct {
	mu sync.Mutex
	n  int
}

// NewRequestID implements middleware.RequestIDs.
func (g *RequestIDs) NewRequestID() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return "req-" + strconv.Itoa(g.n)
}
