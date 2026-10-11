package bill

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Capabilities the bill routes check (spec/legal/access.json).
const (
	// CapView reads bills and their lines.
	CapView = "billing.view"
	// CapDraft drafts, edits, submits and delivers bills.
	CapDraft = "billing.draft"
	// CapApprove approves bills; void needs it too.
	CapApprove = "billing.approve"
)

// Problem codes the bill routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
)

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// HTTP serves GET /bills, GET /bills/{bill_id} and GET /bills/{bill_id}/lines. Every
// dependency is required; a route answers 503 and calls nothing while one is missing.
type HTTP struct {
	Bills    Service
	Access   Access
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Routes mounts the bill read routes.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/bills", h.list)
	r.Get("/bills/{bill_id}", h.get)
	r.Get("/bills/{bill_id}/lines", h.lines)
}

// billPage is the openapi page shape of the bill list routes.
type billPage[T any] struct {
	Data       []T     `json:"data"`
	NextCursor *string `json:"next_cursor"`
}

type billView struct {
	ID                  string  `json:"id"`
	Version             int     `json:"version"`
	Number              *string `json:"number"`
	MatterID            string  `json:"matter_id"`
	ClientID            string  `json:"client_id"`
	Status              string  `json:"status"`
	PeriodFrom          string  `json:"period_from"`
	PeriodTo            string  `json:"period_to"`
	FeesCents           int64   `json:"fees_cents"`
	TaxableDisbCents    int64   `json:"taxable_disb_cents"`
	NontaxableDisbCents int64   `json:"nontaxable_disb_cents"`
	HSTRatePPM          int64   `json:"hst_rate_ppm"`
	HSTCents            int64   `json:"hst_cents"`
	DiscountCents       int64   `json:"discount_cents"`
	TotalCents          int64   `json:"total_cents"`
	TrustAppliedCents   int64   `json:"trust_applied_cents"`
	BalanceCents        int64   `json:"balance_cents"`
	GateID              string  `json:"gate_id,omitempty"`
	ApprovedBy          string  `json:"approved_by,omitempty"`
	ApprovedAt          string  `json:"approved_at,omitempty"`
	DeliveredAt         string  `json:"delivered_at,omitempty"`
	DeliveryChannel     string  `json:"delivery_channel,omitempty"`
	CreatedAt           string  `json:"created_at"`
}

// apiStatus maps a stored bill status to the openapi BillStatus.
func apiStatus(s Status) string {
	switch s {
	case StatusPreflightFailed:
		return "draft"
	case StatusPendingApproval:
		return "in_review"
	case StatusVoid:
		return "voided"
	}
	return string(s)
}

// storedStatuses maps an openapi BillStatus filter to the stored statuses; ok is false for
// a value outside the enum. An empty result matches nothing.
func storedStatuses(s string) ([]Status, bool) {
	switch s {
	case "draft":
		return []Status{StatusDraft, StatusPreflightFailed}, true
	case "in_review":
		return []Status{StatusPendingApproval}, true
	case "returned":
		return []Status{}, true
	case "voided":
		return []Status{StatusVoid}, true
	case "approved", "delivered", "partially_paid", "paid":
		return []Status{Status(s)}, true
	}
	return nil, false
}

func apiChannel(c Channel) string {
	switch c {
	case ChannelEmail, ChannelPortal:
		return string(c)
	case ChannelMail, ChannelHand:
		return "print"
	}
	return ""
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func viewOf(b Bill) billView {
	v := billView{ID: string(b.ID), Version: b.Version, MatterID: b.MatterID, ClientID: b.PayerID,
		Status: apiStatus(b.Status), PeriodFrom: b.PeriodFrom.Format(time.DateOnly), PeriodTo: b.PeriodTo.Format(time.DateOnly),
		FeesCents: b.FeesCents, TaxableDisbCents: b.TaxableDisbCents, NontaxableDisbCents: b.NontaxableDisbCents,
		HSTRatePPM: b.TaxRatePPM, HSTCents: b.TaxCents, DiscountCents: b.DiscountCents, TotalCents: b.TotalCents,
		TrustAppliedCents: b.TrustAppliedCents, BalanceCents: b.BalanceCents, GateID: b.ApprovalID,
		ApprovedBy: b.ApprovedBy, ApprovedAt: stamp(b.ApprovedAt), DeliveredAt: stamp(b.DeliveredAt),
		DeliveryChannel: apiChannel(b.DeliveryChannel), CreatedAt: stamp(b.CreatedAt)}
	if b.Number != "" {
		n := b.Number
		v.Number = &n
	}
	return v
}

type lineView struct {
	ID            string `json:"id"`
	BillID        string `json:"bill_id"`
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id,omitempty"`
	Narrative     string `json:"narrative,omitempty"`
	QuantityMilli int64  `json:"quantity_milli"`
	RateCents     int64  `json:"rate_cents"`
	AmountCents   int64  `json:"amount_cents"`
	Taxable       bool   `json:"taxable"`
	Action        string `json:"action"`
}

func lineViewOf(id ID, l Line) lineView {
	return lineView{ID: l.ID, BillID: string(id), SourceType: string(l.Source), SourceID: l.SourceID,
		Narrative: l.Narrative, QuantityMilli: l.QuantityMilli, RateCents: l.RateCents, AmountCents: l.AmountCents,
		Taxable: l.Taxable, Action: "bill"}
}

// gateway holds what every bill route needs to answer problems and authorize.
type gateway struct {
	problems  server.ProblemWriter
	access    Access
	principal func(r *http.Request) (registry.Principal, bool)
}

func (g gateway) pw() server.ProblemWriter {
	if g.problems == nil {
		return &server.Problems{}
	}
	return g.problems
}

func (g gateway) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	g.pw().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// authorize answers 503 (ready false), 401 or 403 itself; it runs before any service call.
// Every capability in caps is required.
func (g gateway) authorize(w http.ResponseWriter, r *http.Request, ready bool, caps ...string) (registry.Principal, bool) {
	if !ready {
		g.fail(w, r, http.StatusServiceUnavailable, CodeUnavailable, "Service not configured")
		return registry.Principal{}, false
	}
	p, ok := g.principal(r)
	if !ok {
		g.fail(w, r, http.StatusUnauthorized, CodeUnauthenticated, "Authentication required")
		return registry.Principal{}, false
	}
	for _, c := range caps {
		// rule: ARC-I-051
		// Fail closed: an error or a missing capability stops the request.
		allowed, err := g.access.Can(r.Context(), p, c, registry.Subject{})
		switch {
		case errors.Is(err, identity.ErrNotVisible):
			g.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
			return registry.Principal{}, false
		case err != nil:
			g.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
			return registry.Principal{}, false
		case !allowed:
			g.fail(w, r, http.StatusForbidden, CodeMissingCapability, "Missing capability")
			return registry.Principal{}, false
		}
	}
	return p, true
}

// visible checks the bill's matter for p. A matter hidden by a wall or RLS is not found.
func (g gateway) visible(ctx context.Context, p registry.Principal, capability string, b Bill) error {
	// rule: ARC-I-042
	allowed, err := g.access.Can(ctx, p, capability, registry.Subject{Kind: matterkind.Subject, ID: b.MatterID})
	switch {
	case err != nil:
		return err
	case !allowed:
		return identity.ErrNotVisible
	}
	return nil
}

// billID reads the bill_id path parameter; a malformed id answers 400.
func (g gateway) billID(w http.ResponseWriter, r *http.Request) (ID, bool) {
	id := chi.URLParam(r, "bill_id")
	if !uuidPattern.MatchString(id) {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "bill_id is not valid")
		return "", false
	}
	return ID(id), true
}

// readErr maps a read error to a problem. Error text never reaches the body.
func (g gateway) readErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, api.ErrBadCursor):
		g.pw().WriteProblem(w, r, api.BadCursorProblem())
	case errors.Is(err, ErrNotFound), errors.Is(err, identity.ErrNotVisible):
		g.fail(w, r, http.StatusNotFound, CodeNotFound, "No such bill")
	default:
		g.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *HTTP) gw() gateway {
	return gateway{problems: h.Problems, access: h.Access, principal: h.Principal}
}

func (h *HTTP) ready() bool {
	return h.Bills != nil && h.Access != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
}

// list serves GET /bills, filtered by matter_id and status. Bills on matters hidden from the
// caller are left out.
func (h *HTTP) list(w http.ResponseWriter, r *http.Request) {
	g := h.gw()
	p, ok := g.authorize(w, r, h.ready(), CapView)
	if !ok {
		return
	}
	ctx := r.Context()
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return
	}
	if err != nil {
		g.readErr(w, r, err)
		return
	}
	q := r.URL.Query()
	f := Filter{After: ID(cur.After), Limit: cur.Limit + 1, MatterID: q.Get("matter_id")}
	if f.MatterID != "" && !uuidPattern.MatchString(f.MatterID) {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "matter_id is not valid")
		return
	}
	page := billPage[billView]{Data: []billView{}}
	if s := q.Get("status"); s != "" {
		st, ok := storedStatuses(s)
		if !ok {
			g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "status is not valid")
			return
		}
		if len(st) == 0 {
			writeJSON(w, http.StatusOK, page)
			return
		}
		f.Statuses = st
	}
	if f.MatterID != "" {
		if err := g.visible(ctx, p, CapView, Bill{MatterID: f.MatterID}); err != nil {
			g.readErr(w, r, err)
			return
		}
	}
	got, err := h.Bills.List(ctx, f)
	if err != nil {
		g.readErr(w, r, err)
		return
	}
	for i, b := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				g.readErr(w, r, err)
				return
			}
			page.NextCursor = &next
			break
		}
		err := g.visible(ctx, p, CapView, b)
		if errors.Is(err, identity.ErrNotVisible) {
			continue
		}
		if err != nil {
			g.readErr(w, r, err)
			return
		}
		page.Data = append(page.Data, viewOf(b))
	}
	writeJSON(w, http.StatusOK, page)
}

// load fetches the bill named by the path and checks its matter for p with capability.
func (h *HTTP) load(w http.ResponseWriter, r *http.Request, p registry.Principal) (Bill, bool) {
	g := h.gw()
	id, ok := g.billID(w, r)
	if !ok {
		return Bill{}, false
	}
	b, err := h.Bills.Get(r.Context(), id)
	if err == nil {
		err = g.visible(r.Context(), p, CapView, b)
	}
	if err != nil {
		g.readErr(w, r, err)
		return Bill{}, false
	}
	return b, true
}

// get serves GET /bills/{bill_id} with its version as the ETag.
func (h *HTTP) get(w http.ResponseWriter, r *http.Request) {
	p, ok := h.gw().authorize(w, r, h.ready(), CapView)
	if !ok {
		return
	}
	b, ok := h.load(w, r, p)
	if !ok {
		return
	}
	server.SetETag(w, int64(b.Version))
	writeJSON(w, http.StatusOK, viewOf(b))
}

// lines serves GET /bills/{bill_id}/lines, paged by line ordinal.
func (h *HTTP) lines(w http.ResponseWriter, r *http.Request) {
	g := h.gw()
	p, ok := g.authorize(w, r, h.ready(), CapView)
	if !ok {
		return
	}
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return
	}
	if err != nil {
		g.readErr(w, r, err)
		return
	}
	after := -1
	if cur.After != "" {
		if after, err = strconv.Atoi(cur.After); err != nil {
			g.pw().WriteProblem(w, r, api.BadCursorProblem())
			return
		}
	}
	b, ok := h.load(w, r, p)
	if !ok {
		return
	}
	page := billPage[lineView]{Data: []lineView{}}
	for _, l := range b.Lines {
		if l.Ordinal <= after {
			continue
		}
		if len(page.Data) == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: strconv.Itoa(after), Limit: cur.Limit})
			if err != nil {
				g.readErr(w, r, err)
				return
			}
			page.NextCursor = &next
			break
		}
		page.Data = append(page.Data, lineViewOf(b.ID, l))
		after = l.Ordinal
	}
	writeJSON(w, http.StatusOK, page)
}
