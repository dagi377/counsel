package payment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the payment routes check (spec/legal/access.json).
const (
	// CapView reads payments.
	CapView = "billing.view"
	// CapPayments records payments.
	CapPayments = "billing.payments"
)

// SubjectKind is the access subject kind of a payment.
const SubjectKind = "billing.payment"

// Problem codes the payment routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
	CodeUseTrustReceipts  = "use_trust_receipts"
)

// TrustReceiptsPath is where a trust-account payment is recorded instead.
const TrustReceiptsPath = server.BasePath + "/trust/receipts"

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// HTTP serves GET /payments, POST /payments and GET /payments/{payment_id}. Every dependency
// is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Payments Service
	Access   Access
	Cursors  api.CursorCodec
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// businessZone is the business time zone dates are read in (America/Toronto).
var businessZone = sync.OnceValues(func() (*time.Location, error) { return time.LoadLocation("America/Toronto") })

// Routes mounts the payment routes. POST /payments declares the one state key it writes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/payments", h.list)
	// rule: ARC-I-012
	// A recorded payment is captured; no gate owns capture.
	r.Method(http.MethodPost, "/payments", server.Declare(h.create, "payment.captured"))
	r.Get("/payments/{payment_id}", h.get)
}

func (h *HTTP) ready() bool {
	return h.Payments != nil && h.Access != nil && h.Cursors != nil && h.Idem != nil && h.Problems != nil && h.Principal != nil
}

func (h *HTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	pw := h.Problems
	if pw == nil {
		pw = &server.Problems{}
	}
	pw.WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps an unexpected error to a problem; the error text never reaches the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.Problems.WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// authorize answers 503, 401, 403 or 404 itself and returns false when the request cannot go
// on. It runs before any service call.
func (h *HTTP) authorize(w http.ResponseWriter, r *http.Request, capability string, s registry.Subject) (registry.Principal, bool) {
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
	allowed, err := h.Access.Can(r.Context(), p, capability, s)
	switch {
	// rule: ARC-I-042
	case errors.Is(err, identity.ErrNotVisible):
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such payment")
		return registry.Principal{}, false
	case err != nil:
		h.writeErr(w, r, err)
		return registry.Principal{}, false
	case !allowed:
		h.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
		return registry.Principal{}, false
	}
	return p, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// allocationView is one allocation as the API returns it.
type allocationView struct {
	ID          string `json:"id"`
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id,omitempty"`
	AmountCents int64  `json:"amount_cents"`
	State       string `json:"state"`
	FeeCents    int64  `json:"fee_cents"`
	TaxCents    int64  `json:"tax_cents"`
}

// view is a payment as the API returns it (openapi Payment).
type view struct {
	ID             string           `json:"id"`
	Version        int              `json:"version"`
	ReceivedAt     string           `json:"received_at"`
	PayerContactID string           `json:"payer_contact_id,omitempty"`
	AmountCents    int64            `json:"amount_cents"`
	Method         string           `json:"method"`
	Account        string           `json:"account"`
	Provider       string           `json:"provider,omitempty"`
	ProviderRef    string           `json:"provider_ref,omitempty"`
	Status         string           `json:"status"`
	Allocations    []allocationView `json:"allocations"`
}

// allocationID names allocation i of a payment stably in uuid form; allocations have no id
// of their own in the service.
func allocationID(paymentID string, i int) string {
	sum := sha256.Sum256([]byte(paymentID + "/" + strconv.Itoa(i)))
	b := sum[:16]
	b[6] = (b[6] & 0x0f) | 0x80 // version 8
	b[8] = (b[8] & 0x3f) | 0x80
	x := hex.EncodeToString(b)
	return x[:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:]
}

func viewOf(p Payment, as []Allocation) view {
	c := p.Capture
	v := view{ID: p.ID, Version: 1, ReceivedAt: c.ReceivedOn.UTC().Format(time.RFC3339), AmountCents: c.AmountCents,
		Method: c.Method, Account: "operating", Provider: c.Provider, ProviderRef: c.ProviderEventID,
		Status: "received", Allocations: []allocationView{}}
	if idPattern.MatchString(c.ClientID) {
		v.PayerContactID = c.ClientID
	}
	state := "applied"
	if p.Status == StatusReversed {
		v.Status, state = "reversed", "reversed"
	}
	for i, a := range as {
		av := allocationView{ID: allocationID(p.ID, i), TargetType: "unapplied", AmountCents: a.Total(), State: state,
			FeeCents: a.FeeCents, TaxCents: a.TaxCents}
		if a.BillID != "" {
			av.TargetType, av.TargetID = "bill", a.BillID
			if v.Status == "received" {
				v.Status = "allocated"
			}
		} else {
			// Unapplied money carries no bucket split.
			av.FeeCents, av.TaxCents = 0, 0
		}
		v.Allocations = append(v.Allocations, av)
	}
	return v
}

// paymentPage is the openapi listPayments body.
type paymentPage struct {
	Data       []view  `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

// parseDate reads a YYYY-MM-DD business date; empty is the zero time.
func parseDate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, true
	}
	loc, err := businessZone()
	if err != nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation(time.DateOnly, s, loc)
	return t, err == nil
}

// list serves GET /payments: payments by id, one page at a time.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.authorize(w, r, CapView, registry.Subject{}); !ok {
		return
	}
	q := r.URL.Query()
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	from, okFrom := parseDate(q.Get("received_from"))
	to, okTo := parseDate(q.Get("received_to"))
	client := q.Get("client_id")
	if !okFrom || !okTo || (client != "" && !idPattern.MatchString(client)) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Query is not valid")
		return
	}
	// One extra row tells whether another page follows.
	got, err := h.Payments.List(r.Context(), ListFilter{After: cur.After, Limit: cur.Limit + 1, ClientID: client,
		ReceivedFrom: from, ReceivedTo: to})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := paymentPage{Data: []view{}}
	for i, p := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: got[i-1].ID, Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.NextCursor = &next
			break
		}
		as, err := h.Payments.Allocations(r.Context(), p.ID)
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		page.Data = append(page.Data, viewOf(p, as))
	}
	writeJSON(w, http.StatusOK, page)
}

// allocationBody is the openapi AllocationInput.
type allocationBody struct {
	TargetType  string `json:"target_type"`
	TargetID    string `json:"target_id,omitempty"`
	AmountCents int64  `json:"amount_cents"`
}

// createBody is the openapi PaymentCreate.
type createBody struct {
	ReceivedAt        time.Time        `json:"received_at"`
	PayerContactID    string           `json:"payer_contact_id"`
	AmountCents       *int64           `json:"amount_cents"`
	Method            string           `json:"method"`
	Provider          string           `json:"provider,omitempty"`
	ProviderRef       string           `json:"provider_ref,omitempty"`
	ProcessorFeeCents int64            `json:"processor_fee_cents,omitempty"`
	Allocations       []allocationBody `json:"allocations,omitempty"`
}

var methods = map[string]bool{"eft": true, "cheque": true, "card": true, "cash": true, "wire": true, "interac": true}

var targetTypes = map[string]bool{"bill": true, "trust_ledger": true, "unapplied": true}

const maxCents = 100_000_000_000

func (b createBody) valid() bool {
	ok := !b.ReceivedAt.IsZero() && idPattern.MatchString(b.PayerContactID) && b.AmountCents != nil &&
		*b.AmountCents >= 0 && *b.AmountCents <= maxCents && methods[b.Method] && len(b.ProviderRef) <= 120 &&
		b.ProcessorFeeCents >= 0 && b.ProcessorFeeCents <= maxCents
	for _, a := range b.Allocations {
		ok = ok && targetTypes[a.TargetType] && a.AmountCents >= 0 && a.AmountCents <= maxCents &&
			(a.TargetID == "" || idPattern.MatchString(a.TargetID))
	}
	return ok
}

func decodeStrict(body []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data")
	}
	return nil
}

// create serves POST /payments: a payment received into the operating account.
func (h *HTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.authorize(w, r, CapPayments, registry.Subject{})
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
	idem.New(h.Idem, func(*http.Request) string { return p.UserID + p.RunID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.record(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *HTTP) record(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	raw, err := io.ReadAll(r.Body)
	var b createBody
	if err != nil || decodeStrict(raw, &b) != nil || !b.valid() {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	var bills []allocationBody
	for _, a := range b.Allocations {
		switch a.TargetType {
		case "trust_ledger":
			// Trust money never enters through the operating payment route.
			h.useTrustReceipts(w, r)
			return
		case "bill":
			bills = append(bills, a)
		}
	}
	if len(bills) > 1 || (len(bills) == 1 && bills[0].TargetID == "") {
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Allocate across bills with POST /payments/{payment_id}/allocations")
		return
	}
	loc, err := businessZone()
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	y, m, d := b.ReceivedAt.In(loc).Date()
	by := p.UserID
	if p.Kind == registry.PrincipalAgent {
		by = p.ActingFor
	}
	c := Capture{ClientID: b.PayerContactID, Kind: KindBillPayment, AmountCents: *b.AmountCents,
		ReceivedOn: time.Date(y, m, d, 0, 0, 0, 0, loc), Provider: b.Provider, ProviderEventID: b.ProviderRef,
		IdempotencyKey: r.Header.Get(idem.HeaderName), RecordedBy: by, Method: b.Method}
	if len(bills) == 1 {
		c.BillID = bills[0].TargetID
	}
	got, as, err := h.Payments.Capture(r.Context(), c)
	switch {
	case errors.Is(err, ErrRetainerToOperating), errors.Is(err, ErrTrustReceipt):
		h.useTrustReceipts(w, r)
		return
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrOtherClient), errors.Is(err, ErrNotFound):
		h.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Payment is not valid")
		return
	case err != nil:
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, 1)
	writeJSON(w, http.StatusCreated, viewOf(got, as))
}

// useTrustReceipts answers 422 pointing at the trust receipts route.
func (h *HTTP) useTrustReceipts(w http.ResponseWriter, r *http.Request) {
	pr := server.NewProblem(http.StatusUnprocessableEntity, CodeUseTrustReceipts, "Trust money is recorded as a trust receipt")
	pr.Detail = "Use POST " + TrustReceiptsPath
	pr.Instance = TrustReceiptsPath
	h.Problems.WriteProblem(w, r, pr)
}

// get serves GET /payments/{payment_id}. A payment hidden by a wall or RLS answers 404.
func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "payment_id")
	if !idPattern.MatchString(id) {
		if _, ok := h.authorize(w, r, CapView, registry.Subject{}); !ok {
			return
		}
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "payment_id is not valid")
		return
	}
	if _, ok := h.authorize(w, r, CapView, registry.Subject{Kind: SubjectKind, ID: id}); !ok {
		return
	}
	got, err := h.Payments.Get(r.Context(), id)
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such payment")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	as, err := h.Payments.Allocations(r.Context(), id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, 1)
	writeJSON(w, http.StatusOK, viewOf(got, as))
}
