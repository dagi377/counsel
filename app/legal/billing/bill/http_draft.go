package bill

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// Problem codes the draft routes write.
const (
	CodeBillNotEditable = "bill_not_editable"
	CodeNoUnbilledWIP   = "no_unbilled_wip"
)

// Draft route errors. A Drafts implementation returns them; callers match with errors.Is.
var (
	// ErrNotEditable means the bill is not in draft (409 bill_not_editable).
	ErrNotEditable = errors.New("bill: not editable")
	// ErrNoUnbilledWIP means the matter has no unbilled time or expenses in the period (422).
	ErrNoUnbilledWIP = errors.New("bill: no unbilled work in progress")
)

// LineAction is what to do with one unbilled WIP item on a draft (openapi LineAction).
type LineAction struct {
	SourceID       string
	Action         string // bill, hold, write_down, write_off, no_charge
	WriteDownCents int64
	ReasonCode     string
	Narrative      string
}

// DraftRequest drafts one bill from a matter's unbilled WIP.
type DraftRequest struct {
	MatterID    string
	PeriodFrom  time.Time // zero: from the oldest unbilled item
	PeriodTo    time.Time
	TemplateID  string
	LineActions []LineAction
	PreparedBy  string // the user, or the user an agent acts for
}

// DraftEdit is a change to a draft. Nil fields are not changed. Totals are never set by
// the caller; the implementation recomputes them.
type DraftEdit struct {
	PeriodFrom    *time.Time
	PeriodTo      *time.Time
	TemplateID    *string
	DiscountCents *int64
	LineActions   []LineAction
	EditedBy      string
}

// Drafts creates and edits draft bills. It is the narrow port the draft routes need;
// billing/draft supplies it at boot.
type Drafts interface {
	// Create drafts a bill from unbilled WIP, or returns ErrNoUnbilledWIP.
	Create(ctx context.Context, tx pgx.Tx, d DraftRequest) (ID, error)
	// Edit changes a draft and recomputes its totals; a bill past draft is ErrNotEditable.
	Edit(ctx context.Context, tx pgx.Tx, id ID, e DraftEdit) error
}

// TxRunner runs fn in one transaction; kernel/db's pool implements it.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
}

// DraftHTTP serves POST /bills and PATCH /bills/{bill_id}. Every dependency is required; a
// route answers 503 and calls nothing while one is missing.
type DraftHTTP struct {
	Bills    Service
	Drafts   Drafts
	DB       TxRunner
	Access   Access
	Idem     idem.Store
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// Routes mounts the draft routes with their write declarations.
func (h *DraftHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// Drafting and editing a draft are not gate-owned; approval, number, void and delivery are.
	r.Method(http.MethodPost, "/bills", server.Declare(h.create, "bill.status=draft"))
	r.Method(http.MethodPatch, "/bills/{bill_id}", server.Declare(h.update,
		"bill.period", "bill.template_id", "bill.discount_cents", "bill.lines"))
}

func (h *DraftHTTP) gw() gateway {
	return gateway{problems: h.Problems, access: h.Access, principal: h.Principal}
}

func (h *DraftHTTP) ready() bool {
	return h.Bills != nil && h.Drafts != nil && h.DB != nil && h.Access != nil && h.Idem != nil &&
		h.Problems != nil && h.Principal != nil
}

// writeErr maps a draft error to a problem. Error text never reaches the body.
func (h *DraftHTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	g := h.gw()
	switch {
	case errors.Is(err, ErrNotEditable), errors.Is(err, ErrBillGuard):
		g.fail(w, r, http.StatusConflict, CodeBillNotEditable, "Bill is not a draft")
	case errors.Is(err, ErrNoUnbilledWIP):
		g.fail(w, r, http.StatusUnprocessableEntity, CodeNoUnbilledWIP, "No unbilled work in the period")
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrReadOnlyField):
		g.fail(w, r, http.StatusUnprocessableEntity, server.CodeValidationFailed, "Bill is not valid")
	default:
		g.readErr(w, r, err)
	}
}

// preparer is the user who prepares the bill: an agent's acting-for user counts.
func preparer(p registry.Principal) string {
	if p.Kind == registry.PrincipalAgent {
		return p.ActingFor
	}
	return p.UserID
}

func decodeStrict(r *http.Request, v any) error {
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

type lineActionBody struct {
	SourceID       string `json:"source_id"`
	Action         string `json:"action"`
	WriteDownCents int64  `json:"write_down_cents,omitempty"`
	ReasonCode     string `json:"reason_code,omitempty"`
	Narrative      string `json:"narrative,omitempty"`
}

var lineActions = map[string]bool{"bill": true, "hold": true, "write_down": true, "write_off": true, "no_charge": true}

// lineActionsOf validates and converts the body's line actions.
func lineActionsOf(in []lineActionBody) ([]LineAction, bool) {
	out := make([]LineAction, 0, len(in))
	for _, a := range in {
		if !uuidPattern.MatchString(a.SourceID) || !lineActions[a.Action] || a.WriteDownCents < 0 ||
			len(a.ReasonCode) > 64 || len(a.Narrative) > 1000 {
			return nil, false
		}
		out = append(out, LineAction(a))
	}
	return out, true
}

// parseDate parses an optional YYYY-MM-DD; ok is false for a malformed date.
func parseDate(s *string) (*time.Time, bool) {
	if s == nil {
		return nil, true
	}
	t, err := time.Parse(time.DateOnly, *s)
	if err != nil {
		return nil, false
	}
	return &t, true
}

type createBody struct {
	MatterID    string           `json:"matter_id"`
	PeriodFrom  *string          `json:"period_from,omitempty"`
	PeriodTo    string           `json:"period_to"`
	TemplateID  string           `json:"template_id,omitempty"`
	LineActions []lineActionBody `json:"line_actions,omitempty"`
}

// create serves POST /bills: a draft from the matter's unbilled WIP.
func (h *DraftHTTP) create(w http.ResponseWriter, r *http.Request) {
	p, ok := h.gw().authorize(w, r, h.ready(), CapDraft)
	if !ok {
		return
	}
	// rule: ARC-I-007
	// The key is required before any service call.
	if strings.TrimSpace(r.Header.Get(idem.HeaderName)) == "" {
		h.gw().fail(w, r, http.StatusBadRequest, idem.CodeKeyRequired, "Idempotency-Key header is required")
		return
	}
	// rule: ARC-I-008
	idem.New(h.Idem, func(*http.Request) string { return p.UserID }, idem.DefaultTTL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.draft(w, r, p) }),
	).ServeHTTP(w, r)
}

func (h *DraftHTTP) draft(w http.ResponseWriter, r *http.Request, p registry.Principal) {
	g := h.gw()
	var b createBody
	if err := decodeStrict(r, &b); err != nil || !uuidPattern.MatchString(b.MatterID) ||
		(b.TemplateID != "" && !uuidPattern.MatchString(b.TemplateID)) {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	from, okFrom := parseDate(b.PeriodFrom)
	to, okTo := parseDate(&b.PeriodTo)
	actions, okActions := lineActionsOf(b.LineActions)
	if !okFrom || !okTo || !okActions || (from != nil && from.After(*to)) {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	ctx := r.Context()
	// rule: ARC-I-042
	// A matter hidden by a wall answers 404.
	if err := g.visible(ctx, p, CapDraft, Bill{MatterID: b.MatterID}); err != nil {
		h.writeErr(w, r, err)
		return
	}
	d := DraftRequest{MatterID: b.MatterID, PeriodTo: *to, TemplateID: b.TemplateID, LineActions: actions, PreparedBy: preparer(p)}
	if from != nil {
		d.PeriodFrom = *from
	}
	var id ID
	err := h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = h.Drafts.Create(ctx, tx, d)
		return err
	})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Bills.Get(ctx, id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.Version))
	w.Header().Set("Location", "/v1/bills/"+string(out.ID))
	writeJSON(w, http.StatusCreated, viewOf(out))
}

type patchBody struct {
	PeriodFrom    *string          `json:"period_from"`
	PeriodTo      *string          `json:"period_to"`
	TemplateID    *string          `json:"template_id"`
	DiscountCents *int64           `json:"discount_cents"`
	LineActions   []lineActionBody `json:"line_actions"`
}

// draftEditable reports whether the PATCH route may edit a bill in status s (openapi: draft
// or returned). A bill in review is not editable here.
func draftEditable(s Status) bool { return s == StatusDraft || s == StatusPreflightFailed }

// update serves PATCH /bills/{bill_id} at the If-Match version. Status, number and totals
// are not in the body schema, so a body carrying them is 400.
func (h *DraftHTTP) update(w http.ResponseWriter, r *http.Request) {
	g := h.gw()
	p, ok := g.authorize(w, r, h.ready(), CapDraft)
	if !ok {
		return
	}
	id, ok := g.billID(w, r)
	if !ok {
		return
	}
	// rule: ARC-I-006
	if len(r.Header.Values("If-Match")) == 0 {
		g.fail(w, r, http.StatusPreconditionRequired, server.CodePreconditionRequired, "If-Match is required")
		return
	}
	var b patchBody
	if err := decodeStrict(r, &b); err != nil || (b.TemplateID != nil && !uuidPattern.MatchString(*b.TemplateID)) ||
		(b.DiscountCents != nil && *b.DiscountCents < 0) {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	from, okFrom := parseDate(b.PeriodFrom)
	to, okTo := parseDate(b.PeriodTo)
	actions, okActions := lineActionsOf(b.LineActions)
	if !okFrom || !okTo || !okActions {
		g.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "Body is not valid")
		return
	}
	ctx := r.Context()
	cur, err := h.Bills.Get(ctx, id)
	if err == nil {
		err = g.visible(ctx, p, CapDraft, cur)
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	if err := server.CheckIfMatch(r, int64(cur.Version)); err != nil {
		g.fail(w, r, http.StatusPreconditionFailed, server.CodeVersionConflict, "If-Match does not match the current version")
		return
	}
	if !draftEditable(cur.Status) {
		h.writeErr(w, r, ErrNotEditable)
		return
	}
	e := DraftEdit{PeriodFrom: from, PeriodTo: to, TemplateID: b.TemplateID, DiscountCents: b.DiscountCents, EditedBy: preparer(p)}
	if b.LineActions != nil {
		e.LineActions = actions
	}
	if err := h.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { return h.Drafts.Edit(ctx, tx, id, e) }); err != nil {
		h.writeErr(w, r, err)
		return
	}
	out, err := h.Bills.Get(ctx, id)
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	server.SetETag(w, int64(out.Version))
	writeJSON(w, http.StatusOK, viewOf(out))
}
