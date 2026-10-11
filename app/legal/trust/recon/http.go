package recon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapView reads reconciliations (spec/legal/access.json).
const CapView = "trust.view"

// AccountSubject is the access subject kind of a trust account.
const AccountSubject = "trust.account"

// Problem codes the reconciliation read route writes.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
)

// ListFilter selects one page of List: reconciliations whose id sorts after After, on
// AccountID when set, at most Limit.
type ListFilter struct {
	AccountID string
	After     string
	Limit     int
}

// Lists is the reconciliation list query, read under the caller's RLS session. Service has
// Get only; this port serves GET /trust/reconciliations (HTTP-11).
type Lists interface {
	// List returns one page of reconciliations, by id.
	List(ctx context.Context, f ListFilter) ([]Reconciliation, error)
}

// Access is the part of identity.Access the route uses.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// Reads serves GET /trust/reconciliations. Every dependency is required; the route answers
// 503 and calls nothing while one is missing.
type Reads struct {
	Lists    Lists
	Access   Access
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the read route. It writes nothing.
func (h *Reads) Routes(r chi.Router) {
	r.Get("/trust/reconciliations", h.list)
}

// reconView is the openapi Reconciliation.
type reconView struct {
	ID                      string `json:"id"`
	Version                 int    `json:"version"`
	AccountID               string `json:"account_id"`
	Month                   string `json:"month"`
	BankBalanceCents        int64  `json:"bank_balance_cents"`
	OutstandingItemsCents   int64  `json:"outstanding_items_cents"`
	AdjustedBankCents       int64  `json:"adjusted_bank_cents"`
	JournalBalanceCents     int64  `json:"journal_balance_cents"`
	ClientListingTotalCents int64  `json:"client_listing_total_cents"`
	Balanced                bool   `json:"balanced"`
	Status                  string `json:"status"`
	SignedBy                string `json:"signed_by,omitempty"`
	SignedAt                string `json:"signed_at,omitempty"`
}

func reconOf(c Reconciliation) reconView {
	var items int64
	for _, it := range c.Result.Items {
		items += it.AmountCents
	}
	v := reconView{ID: c.ID, Version: 1, AccountID: c.AccountID,
		Month:            fmt.Sprintf("%04d-%02d", c.Period.Year, int(c.Period.Month)),
		BankBalanceCents: c.Result.BankCents, OutstandingItemsCents: items,
		AdjustedBankCents: c.Result.AdjustedBankCents, JournalBalanceCents: c.Result.BookCents,
		ClientListingTotalCents: c.Result.ClientLedgerCents, Balanced: c.Result.Balanced(),
		Status: "prepared"}
	if c.Status == StatusApproved {
		v.Status, v.SignedBy = "signed", c.ApprovedBy
		if c.ApprovedAt != nil {
			v.SignedAt = c.ApprovedAt.UTC().Format(time.RFC3339)
		}
	}
	return v
}

func (h *Reads) ready() bool {
	return h.Lists != nil && h.Access != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
}

func (h *Reads) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *Reads) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps an unexpected error to a problem. The error text never reaches the body.
func (h *Reads) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// authorize answers 503, 401, 403 or 404 itself and returns false when the request cannot go
// on. It runs before any service call.
func (h *Reads) authorize(w http.ResponseWriter, r *http.Request, s registry.Subject) bool {
	if !h.ready() {
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
	allowed, err := h.Access.Can(r.Context(), p, CapView, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such account")
		return false
	case err != nil:
		h.writeErr(w, r, err)
		return false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return false
	}
	return true
}

// list serves GET /trust/reconciliations: reconciliations by id, one page at a time,
// optionally for one account.
func (h *Reads) list(w http.ResponseWriter, r *http.Request) {
	accountID := r.URL.Query().Get("account_id")
	subject := registry.Subject{}
	if accountID != "" && idPattern.MatchString(accountID) {
		subject = registry.Subject{Kind: AccountSubject, ID: accountID}
	}
	if !h.authorize(w, r, subject) {
		return
	}
	if accountID != "" && !idPattern.MatchString(accountID) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "account_id is not valid")
		return
	}
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	// One extra row tells whether another page follows.
	got, err := h.Lists.List(r.Context(), ListFilter{AccountID: accountID, After: cur.After, Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[reconView]{Items: []reconView{}}
	for i, c := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: got[i-1].ID, Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, reconOf(c))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(page)
}
