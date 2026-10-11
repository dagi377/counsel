package tax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the firm tax registration routes check (spec/legal/access.json).
const (
	// CapRegistrationsView reads the firm's provincial registrations.
	CapRegistrationsView = "billing.view"
	// CapRegistrationsEdit changes them: they decide which provincial tax a bill charges, so
	// only a lawyer who approves bills may set them (Q-TAX-API).
	CapRegistrationsEdit = "billing.approve"
)

// Problem codes the registration routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeUnavailable       = "unavailable"
)

// ErrNoFirm means core.firm has no settings row.
var ErrNoFirm = errors.New("tax: no firm settings row")

// Registrations are the firm's provincial sales tax registrations (BIL-T-020) at a version.
type Registrations struct {
	Provinces []string
	Version   int
}

// RegistrationStore reads and writes the firm's registrations. Set at a stale version
// returns server.ErrPreconditionFailed.
type RegistrationStore interface {
	Get(ctx context.Context) (Registrations, error)
	Set(ctx context.Context, version int, provinces []string, by registry.Principal) (Registrations, error)
}

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// RegistrationsHTTP serves GET and PATCH /firm/tax-registrations. Every dependency is
// required; a route answers 503 and calls nothing while one is missing.
type RegistrationsHTTP struct {
	Store    RegistrationStore
	Access   Access
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

func init() { add(registerRegistrationRoutes) }

// registerRegistrationRoutes mounts GET and PATCH /firm/tax-registrations through
// Registry.Routes. Boot wiring supplies the dependencies; until it does, the routes answer 503.
func registerRegistrationRoutes(r registry.Registry) error {
	r.Routes((&RegistrationsHTTP{}).Routes)
	return nil
}

// Routes mounts the registration routes with their write declaration.
func (h *RegistrationsHTTP) Routes(r chi.Router) {
	r.Get("/firm/tax-registrations", h.get)
	r.Method(http.MethodPatch, "/firm/tax-registrations", server.Declare(h.patch, "firm.tax_registration_provinces"))
}

type registrationsView struct {
	Provinces []string `json:"provinces"`
	Version   int      `json:"version"`
}

func viewOfRegistrations(r Registrations) registrationsView {
	p := r.Provinces
	if p == nil {
		p = []string{}
	}
	return registrationsView{Provinces: p, Version: r.Version}
}

func (h *RegistrationsHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := h.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

// authorize answers 503, 401 or 403 itself and returns false when the request cannot go on.
// It runs before any store call.
func (h *RegistrationsHTTP) authorize(w http.ResponseWriter, r *http.Request, capability string) bool {
	if h.Store == nil || h.Access == nil || h.Problems == nil || h.Principal == nil {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return false
	}
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := h.Access.Can(r.Context(), p, capability, registry.Subject{})
	switch {
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return false
	}
	return true
}

func (h *RegistrationsHTTP) write(w http.ResponseWriter, status int, reg Registrations) {
	server.SetETag(w, int64(reg.Version))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(viewOfRegistrations(reg))
}

func (h *RegistrationsHTTP) get(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, CapRegistrationsView) {
		return
	}
	reg, err := h.Store.Get(r.Context())
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	h.write(w, http.StatusOK, reg)
}

// ValidRegistrations reports whether each entry is a Canadian province or territory code,
// listed once. An empty list means the firm collects no provincial sales tax.
//
// rule: BIL-T-020
func ValidRegistrations(codes []string) bool {
	seen := map[string]bool{}
	for _, p := range codes {
		if !provinces[p] || seen[p] {
			return false
		}
		seen[p] = true
	}
	return true
}

type patchRegistrations struct {
	Provinces *[]string `json:"provinces"`
}

// patch serves PATCH /firm/tax-registrations: it replaces the list at the If-Match version
// (200 and the new ETag).
func (h *RegistrationsHTTP) patch(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, CapRegistrationsEdit) {
		return
	}
	p, _ := h.Principal(r)
	// rule: ARC-I-006
	if len(r.Header.Values("If-Match")) == 0 {
		h.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	var b patchRegistrations
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil || dec.More() || b.Provinces == nil || !ValidRegistrations(*b.Provinces) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	cur, err := h.Store.Get(r.Context())
	if err != nil {
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	if err := server.CheckIfMatch(r, int64(cur.Version)); err != nil {
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	list := slices.Clone(*b.Provinces)
	slices.Sort(list)
	out, err := h.Store.Set(r.Context(), cur.Version, list, p)
	switch {
	case errors.Is(err, server.ErrPreconditionFailed):
		h.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return
	}
	h.write(w, http.StatusOK, out)
}

// DB is the part of kernel/db's pool PGRegistrations uses.
type DB interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// PGRegistrations stores the registrations on the core.firm settings row.
type PGRegistrations struct {
	DB DB
}

var _ RegistrationStore = PGRegistrations{}

const (
	registrationsGetSQL = `SELECT coalesce(array_to_string(tax_registration_provinces, ','), ''), tax_registrations_version
  FROM core.firm WHERE singleton`
	registrationsSetSQL = `UPDATE core.firm
   SET tax_registration_provinces = $1::char(2)[], tax_registrations_version = tax_registrations_version + 1,
       updated_at = now()
 WHERE singleton AND tax_registrations_version = $2
RETURNING tax_registrations_version`
)

// Get reads the registrations; no firm row is ErrNoFirm.
func (s PGRegistrations) Get(ctx context.Context) (Registrations, error) {
	var out Registrations
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var list string
		if err := tx.QueryRow(ctx, registrationsGetSQL).Scan(&list, &out.Version); err != nil {
			return err
		}
		out.Provinces = []string{}
		if list != "" {
			out.Provinces = strings.Split(list, ",")
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Registrations{}, ErrNoFirm
	}
	if err != nil {
		return Registrations{}, fmt.Errorf("tax registrations: %w", err)
	}
	return out, nil
}

// Set replaces the registrations at version; a stale version is server.ErrPreconditionFailed.
func (s PGRegistrations) Set(ctx context.Context, version int, provinces []string, _ registry.Principal) (Registrations, error) {
	if !ValidRegistrations(provinces) {
		return Registrations{}, ErrUnknownProvince
	}
	out := Registrations{Provinces: slices.Clone(provinces)}
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, registrationsSetSQL, provinces, version).Scan(&out.Version)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Registrations{}, server.ErrPreconditionFailed
	}
	if err != nil {
		return Registrations{}, fmt.Errorf("set tax registrations: %w", err)
	}
	return out, nil
}
