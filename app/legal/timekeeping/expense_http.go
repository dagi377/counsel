package timekeeping

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// ExpenseHTTP serves GET /expenses and POST /expenses. Every dependency is required; a route
// answers 503 and calls nothing while one is missing.
type ExpenseHTTP struct {
	Expenses Expenses
	DB       TxRunner
	Access   Access
	Cursors  api.CursorCodec
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Routes mounts the expense routes with their write declarations.
func (h *ExpenseHTTP) Routes(r chi.Router) {
	r.Get("/expenses", h.list)
	// rule: ARC-I-012
	// Recording an expense is not gate-owned; approval and billing are.
	r.Method(http.MethodPost, "/expenses", server.Declare(h.create, "expense.recorded"))
}

type expenseView struct {
	ID                string `json:"id"`
	MatterID          string `json:"matter_id"`
	UserID            string `json:"user_id"`
	ExpenseDate       string `json:"expense_date"`
	Vendor            string `json:"vendor,omitempty"`
	AmountPreTaxCents int64  `json:"amount_pre_tax_cents"`
	VendorTaxCents    int64  `json:"vendor_tax_cents"`
	IsAgency          bool   `json:"is_agency"`
	PaidFrom          string `json:"paid_from"`
	Billable          bool   `json:"billable"`
	Status            string `json:"status"`
	BillID            string `json:"bill_id,omitempty"`
	Version           int    `json:"version"`
}

func expenseViewOf(e Expense) expenseView {
	return expenseView{ID: string(e.ID), MatterID: e.MatterID, UserID: e.UserID,
		ExpenseDate: e.ExpenseDate.Format(time.DateOnly), Vendor: e.Vendor, AmountPreTaxCents: e.AmountPreTaxCents,
		VendorTaxCents: e.VendorTaxCents, IsAgency: e.IsAgency, PaidFrom: e.PaidFrom, Billable: e.Billable,
		Status: string(e.Status), BillID: e.BillID, Version: e.RowVersion}
}

func (h *ExpenseHTTP) ready() bool {
	return h.Expenses != nil && h.DB != nil && h.Access != nil && h.Cursors != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil
}

func (h *ExpenseHTTP) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *ExpenseHTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps a service error to a problem. Error text never reaches the body.
func (h *ExpenseHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, api.ErrBadCursor):
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
	case errors.Is(err, ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such expense")
	case errors.Is(err, ErrBilledLocked):
		h.fail(w, r, http.StatusConflict, CodeLockedToBill, "Expense is on a bill")
	case errors.Is(err, ErrInvalid):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Expense is not valid")
	case errors.Is(err, ErrNotOwner), errors.Is(err, ErrAgentForbidden):
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Not allowed for this expense")
	default:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

// authorize answers 503, 401, 403 or 404 itself; it runs before any service call.
func (h *ExpenseHTTP) authorize(w http.ResponseWriter, r *http.Request) (registry.Principal, bool) {
	if !h.ready() {
		h.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, false
	}
	p, ok := h.Principal(r)
	if !ok {
		h.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return registry.Principal{}, false
	}
	// rule: ARC-I-051
	// Fail closed: an error or a missing capability stops the request.
	allowed, err := h.Access.Can(r.Context(), p, CapRecord, registry.Subject{})
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
		return registry.Principal{}, false
	case err != nil:
		h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
		return registry.Principal{}, false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return registry.Principal{}, false
	}
	return p, true
}

// viewAll reports whether p sees every timekeeper's expenses. An error counts as no.
func (h *ExpenseHTTP) viewAll(ctx context.Context, p registry.Principal) bool {
	ok, err := h.Access.Can(ctx, p, CapViewAll, registry.Subject{})
	return err == nil && ok
}

// list pages the caller's expenses, or everyone's with time.view_all.
func (h *ExpenseHTTP) list(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
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
	f := ExpenseFilter{After: ExpenseID(cur.After), Limit: cur.Limit + 1}
	if !h.viewAll(r.Context(), p) {
		f.UserID = actorOf(p).UserID
	}
	got, err := h.Expenses.List(r.Context(), f)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[expenseView]{Items: []expenseView{}}
	for i, e := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, expenseViewOf(e))
	}
	writeJSON(w, http.StatusOK, page)
}

type expenseBody struct {
	MatterID          string `json:"matter_id"`
	ExpenseDate       string `json:"expense_date"`
	Vendor            string `json:"vendor,omitempty"`
	AmountPreTaxCents int64  `json:"amount_pre_tax_cents"`
	VendorTaxCents    int64  `json:"vendor_tax_cents,omitempty"`
	IsAgency          bool   `json:"is_agency,omitempty"`
	PaidFrom          string `json:"paid_from"`
	Billable          *bool  `json:"billable,omitempty"`
}

// create serves POST /expenses: the caller records their own expense as a draft.
func (h *ExpenseHTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r)
	if !ok {
		return
	}
	// rule: ARC-I-007
	// The key is required before any service call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return actorOf(p).UserID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.record(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *ExpenseHTTP) record(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	var b expenseBody
	if err := decodeStrict(r, &b); err != nil || !uuidPattern.MatchString(b.MatterID) ||
		b.AmountPreTaxCents <= 0 || b.VendorTaxCents < 0 || !paidFromValues[b.PaidFrom] {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, defaultProblemTitleBody)
		return
	}
	day, err := time.Parse(time.DateOnly, b.ExpenseDate)
	if err != nil {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "expense_date is not a date")
		return
	}
	// rule: ARC-I-042
	// A matter hidden by a wall answers 404.
	if _, err := h.Access.Can(r.Context(), p, CapRecord, registry.Subject{Kind: matterkind.Subject, ID: b.MatterID}); err != nil {
		h.writeErr(w, r, err)
		return
	}
	by := actorOf(p)
	e := Expense{MatterID: b.MatterID, UserID: by.UserID, ExpenseDate: day, Vendor: b.Vendor,
		AmountPreTaxCents: b.AmountPreTaxCents, VendorTaxCents: b.VendorTaxCents, IsAgency: b.IsAgency,
		PaidFrom: b.PaidFrom, Billable: b.Billable == nil || *b.Billable}
	var id ExpenseID
	err = h.DB.InTx(r.Context(), func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Expenses.Record(ctx, tx, e, by)
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Expenses.Get(r.Context(), id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.RowVersion))
	writeJSON(w, http.StatusCreated, expenseViewOf(out))
}
