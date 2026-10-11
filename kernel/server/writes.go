package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// DecisionRoute is the one route allowed to set gate-owned state: deciding a gate runs its
// effect, and only the effect writes what the gate guards.
var DecisionRoute = Route{Method: http.MethodPost, Pattern: BasePath + "/gates/{gate_id}/decisions"}

// RouteWrite declares which state keys a route sets directly. A route that only opens a gate
// declares an empty Sets: the gate's effect, not the route, writes the guarded state.
type RouteWrite struct {
	Route
	Sets []string
}

// Declared is a handler that carries its route's write declaration. Mount write routes as
// r.Method(method, path, server.Declare(h, sets...)) so the declaration lives next to the route.
type Declared struct {
	http.Handler
	Sets []string
}

// Declare wraps h with the state keys its route sets. No keys means the route sets nothing
// directly (it opens a gate or only reads).
func Declare(h http.HandlerFunc, sets ...string) *Declared {
	return &Declared{Handler: h, Sets: slices.Clone(sets)}
}

// DeclaredWrites lists the write declaration of every route on h that was mounted through
// Declare, with full served patterns. h must be a chi router, such as Mux.Handler().
func DeclaredWrites(h http.Handler) ([]RouteWrite, error) {
	routes, ok := h.(chi.Routes)
	if !ok {
		return nil, fmt.Errorf("declared writes: handler %T is not a chi router", h)
	}
	var out []RouteWrite
	err := chi.Walk(routes, func(method, route string, hh http.Handler, _ ...func(http.Handler) http.Handler) error {
		if ch, ok := hh.(*chi.ChainHandler); ok {
			hh = ch.Endpoint
		}
		if d, ok := hh.(*Declared); ok {
			out = append(out, RouteWrite{
				Route: Route{Method: method, Pattern: strings.TrimSuffix(route, "/*")},
				Sets:  slices.Clone(d.Sets),
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("declared writes: %w", err)
	}
	return out, nil
}

// writeMethods are the methods that change state; every served route with one of them needs
// a write declaration.
var writeMethods = map[string]bool{
	http.MethodPost: true, http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// CheckWrites lists every way the served routes break the gate-owned-state rule. owned maps a
// gate-owned state key to the gate kind whose effect sets it. A served POST, PUT, PATCH or
// DELETE route with no declaration is a mismatch, and a declaration that names a gate-owned
// key is a mismatch unless the route is decision. An empty result means the routes conform.
func CheckWrites(served []Route, writes []RouteWrite, owned map[string]string, decision Route) []string {
	declared := make(map[Route][]string, len(writes))
	for _, w := range writes {
		declared[w.Route] = append(declared[w.Route], w.Sets...)
	}
	var out []string
	for _, rt := range sorted(served) {
		if !writeMethods[rt.Method] {
			continue
		}
		sets, ok := declared[rt]
		if !ok {
			out = append(out, fmt.Sprintf("write route has no write declaration: %s %s", rt.Method, rt.Pattern))
			continue
		}
		if rt == decision {
			continue
		}
		// rule: ARC-I-012
		for _, key := range sets {
			if kind, ok := owned[key]; ok {
				out = append(out, fmt.Sprintf("route sets gate-owned state %s (only the %s effect may): %s %s",
					key, kind, rt.Method, rt.Pattern))
			}
		}
	}
	return out
}
