package server

import (
	"bufio"
	"bytes"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// BasePath is the version prefix every module route is served under. It is the path of the
// OpenAPI server URL (https://{cell_host}/v1); modules mount paths relative to it.
const BasePath = "/v1"

// Mux is the kernel's Server: a chi router whose module routes live under BasePath.
type Mux struct {
	mu       sync.Mutex
	root     chi.Router
	api      chi.Router
	seen     map[Route]bool
	rootSeen map[Route]bool
	problems ProblemWriter
}

var _ Server = (*Mux)(nil)

// New returns an empty Mux. Unknown paths, wrong methods and handler panics are answered as
// problem+json through pw; nil pw means a Problems with default logging.
func New(pw ProblemWriter) *Mux {
	if pw == nil {
		pw = &Problems{}
	}
	m := &Mux{root: chi.NewRouter(), seen: map[Route]bool{}, rootSeen: map[Route]bool{}, problems: pw}
	m.root.Use(m.recoverer)
	m.root.NotFound(m.notFound)
	m.root.MethodNotAllowed(m.methodNotAllowed)
	m.root.Route(BasePath, func(r chi.Router) {
		r.NotFound(m.notFound)
		r.MethodNotAllowed(m.methodNotAllowed)
		m.api = r
	})
	return m
}

// Mount applies mt to a scratch router first, so a duplicate route leaves the Mux unchanged.
func (m *Mux) Mount(mt Mounter) error {
	if mt == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	scratch := chi.NewRouter()
	mt(scratch)
	added, err := walk(scratch)
	if err != nil {
		return err
	}
	for _, rt := range added {
		if m.seen[rt] {
			return fmt.Errorf("mount %s %s: %w", rt.Method, rt.Pattern, ErrDuplicateRoute)
		}
	}
	mt(m.api)
	for _, rt := range added {
		m.seen[rt] = true
	}
	return nil
}

// MountRoot adds routes outside BasePath: browser flows such as sign-in redirects, which
// are not operations of the versioned API and so are not in openapi.yaml or Routes. A route
// registered twice makes boot fail.
func (m *Mux) MountRoot(mt Mounter) error {
	if mt == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	scratch := chi.NewRouter()
	mt(scratch)
	added, err := walk(scratch)
	if err != nil {
		return err
	}
	for i := range added {
		added[i].Pattern = strings.TrimPrefix(added[i].Pattern, BasePath)
		if m.rootSeen[added[i]] || strings.HasPrefix(added[i].Pattern, BasePath+"/") {
			return fmt.Errorf("mount root %s %s: %w", added[i].Method, added[i].Pattern, ErrDuplicateRoute)
		}
	}
	mt(m.root)
	for _, rt := range added {
		m.rootSeen[rt] = true
	}
	return nil
}

// Routes lists every registered route with its full served pattern (BasePath included),
// sorted by pattern then method.
func (m *Mux) Routes() []Route {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Route, 0, len(m.seen))
	for rt := range m.seen {
		out = append(out, rt)
	}
	SortRoutes(out)
	return out
}

// Handler is the root handler with middleware applied.
func (m *Mux) Handler() http.Handler { return m.root }

func (m *Mux) notFound(w http.ResponseWriter, r *http.Request) {
	m.problems.WriteProblem(w, r, NewProblem(http.StatusNotFound, "not_found", "No such resource"))
}

func (m *Mux) methodNotAllowed(w http.ResponseWriter, r *http.Request) {
	m.problems.WriteProblem(w, r, NewProblem(http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed"))
}

// recoverer turns a handler panic into a 500 problem. The panic value is not logged or
// written: it may carry client data.
func (m *Mux) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				m.problems.WriteProblem(w, r, NewProblem(http.StatusInternalServerError, CodeInternal, "Internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// walk lists the routes on r with BasePath prepended.
func walk(r chi.Router) ([]Route, error) {
	var out []Route
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, Route{Method: method, Pattern: BasePath + strings.TrimSuffix(route, "/*")})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk routes: %w", err)
	}
	return out, nil
}

// SortRoutes sorts routes by pattern then method.
func SortRoutes(rs []Route) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Pattern != rs[j].Pattern {
			return rs[i].Pattern < rs[j].Pattern
		}
		return rs[i].Method < rs[j].Method
	})
}

var openAPIMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// OpenAPIRoutes lists every operation in an OpenAPI document as a Route with BasePath
// prepended, sorted by pattern then method.
func OpenAPIRoutes(doc []byte) ([]Route, error) {
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(doc, &spec); err != nil {
		return nil, fmt.Errorf("parse openapi: %w", err)
	}
	var out []Route
	for p, item := range spec.Paths {
		for _, method := range openAPIMethods {
			if _, ok := item[method]; ok {
				out = append(out, Route{Method: strings.ToUpper(method), Pattern: BasePath + p})
			}
		}
	}
	SortRoutes(out)
	return out, nil
}

// ParseRouteList reads one "METHOD /path" per line; blank lines and lines starting with #
// are skipped. Paths are OpenAPI paths without BasePath; the result has BasePath prepended.
func ParseRouteList(data []byte) ([]Route, error) {
	var out []Route
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 2 || !strings.HasPrefix(f[1], "/") {
			return nil, fmt.Errorf("route list line %d: want \"METHOD /path\"", n)
		}
		out = append(out, Route{Method: strings.ToUpper(f[0]), Pattern: BasePath + f[1]})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read route list: %w", err)
	}
	return out, nil
}

// Conformance lists every way the served routes and the OpenAPI document disagree. The check
// is two-way: a served route must be documented, and a documented operation must be served
// unless it is listed in unserved. The unserved list only shrinks: a listed operation that is
// now served, or that the document no longer has, is a mismatch too. An empty result means
// the routes conform.
func Conformance(served, documented, unserved []Route) []string {
	doc := set(documented)
	srv := set(served)
	pending := set(unserved)
	var out []string
	for _, rt := range sorted(served) {
		if !doc[rt] {
			out = append(out, fmt.Sprintf("served but not in openapi: %s %s", rt.Method, rt.Pattern))
		}
	}
	for _, rt := range sorted(documented) {
		if !srv[rt] && !pending[rt] {
			out = append(out, fmt.Sprintf("in openapi but not served or listed unserved: %s %s", rt.Method, rt.Pattern))
		}
	}
	for _, rt := range sorted(unserved) {
		switch {
		case srv[rt]:
			out = append(out, fmt.Sprintf("listed unserved but now served, remove it from the list: %s %s", rt.Method, rt.Pattern))
		case !doc[rt]:
			out = append(out, fmt.Sprintf("listed unserved but not in openapi: %s %s", rt.Method, rt.Pattern))
		}
	}
	return out
}

func set(rs []Route) map[Route]bool {
	m := make(map[Route]bool, len(rs))
	for _, rt := range rs {
		m[rt] = true
	}
	return m
}

func sorted(rs []Route) []Route {
	out := append([]Route(nil), rs...)
	SortRoutes(out)
	return out
}
