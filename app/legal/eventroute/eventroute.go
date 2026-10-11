// Package eventroute is the shared request flow of the minimal write routes over the
// publishing write points (Q-EVENTS-WIRE-5): principal, capability, Idempotency-Key, and the
// matter-level check every matter-scoped write makes (walled matter 404, missing
// capability 403).
package eventroute

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Problem codes the routes answer.
const (
	CodeUnavailable       = "unavailable"
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeHumanOnly         = "human_only"
)

// Access is identity.Access as the routes use it.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// Route holds the dependencies every write route shares. A route with one missing answers 503.
type Route struct {
	Access   Access
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Ready reports whether every shared dependency is set.
func (rt Route) Ready() bool {
	return rt.Access != nil && rt.Idem != nil && rt.Problems != nil && rt.Principal != nil
}

// Fail writes a problem.
func (rt Route) Fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := rt.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

// User is the human a principal acts as: the session user, or the user an agent run acts for.
func User(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

// Serve runs the shared flow: 503 when ready is false, 401 without a principal, the firm-wide
// capability check, 400 without an Idempotency-Key, then apply under the idempotency
// middleware keyed by the acting user.
func (rt Route) Serve(w http.ResponseWriter, r *http.Request, ready bool, capability string,
	apply func(w http.ResponseWriter, r *http.Request, p registry.Principal)) {
	if !ready || !rt.Ready() {
		rt.Fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return
	}
	p, ok := rt.Principal(r)
	if !ok {
		rt.Fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return
	}
	if !rt.Can(w, r, p, capability, registry.Subject{}) {
		return
	}
	// rule: ARC-I-007
	// The key is required before any store call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		rt.Fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	user := User(p)
	// rule: ARC-I-008
	idem.New(rt.Idem, func(*http.Request) string { return user }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { apply(w, r, p) }),
	).ServeHTTP(w, r)
}

// Can checks capability on s and writes the problem when it fails: a subject the principal
// may not see (a walled matter) answers 404, a missing capability 403, an error 500.
func (rt Route) Can(w http.ResponseWriter, r *http.Request, p registry.Principal, capability string, s registry.Subject) bool {
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := rt.Access.Can(r.Context(), p, capability, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		rt.Fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
	case err != nil:
		rt.Fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	case !allowed:
		rt.Fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
	default:
		return true
	}
	return false
}

// CanOnMatter is Can on matter matterID.
func (rt Route) CanOnMatter(w http.ResponseWriter, r *http.Request, p registry.Principal, capability, matterID string) bool {
	return rt.Can(w, r, p, capability, registry.Subject{Kind: matterkind.Subject, ID: matterID})
}

// Internal writes a 500.
func (rt Route) Internal(w http.ResponseWriter, r *http.Request) {
	rt.Fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// Invalid writes a 400 validation problem.
func (rt Route) Invalid(w http.ResponseWriter, r *http.Request, title string) {
	rt.Fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, title)
}

// Decode reads r's JSON body into v, refusing unknown fields and trailing data.
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// JSON writes v as the JSON body with status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
