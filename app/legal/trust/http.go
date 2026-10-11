package trust

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapView reads trust accounts, ledgers and journal lines (spec/legal/access.json).
const CapView = "trust.view"

// Subject kinds the read routes check access on.
const (
	AccountSubject = "trust.account"
	LedgerSubject  = "trust.ledger"
)

// Problem codes the trust read routes write.
const (
	CodeUnauthenticated   = "unauthenticated"
	CodeMissingCapability = "missing_capability"
	CodeNotFound          = "not_found"
	CodeUnavailable       = "unavailable"
)

// LedgerFilter selects one page of ListLedgers: ledgers whose id sorts after After, on
// MatterID and AccountID when set, at most Limit.
type LedgerFilter struct {
	MatterID  string
	AccountID string
	After     LedgerID
	Limit     int
}

// Lists lists trust accounts and client ledgers under the caller's RLS session. Accounts and
// Ledgers read one record; this port serves GET /trust/accounts and GET /trust/ledgers
// (HTTP-10).
type Lists interface {
	// ListAccounts returns every trust account, by id.
	ListAccounts(ctx context.Context) ([]Account, error)
	// ListLedgers returns one page of ledgers, by id.
	ListLedgers(ctx context.Context, f LedgerFilter) ([]Ledger, error)
}

// LedgerReads is the part of Ledgers the read routes use. Balance is the only source of a
// ledger's balance: the handler never sums transactions itself.
type LedgerReads interface {
	Balance(ctx context.Context, id LedgerID) (Balance, error)
	Txns(ctx context.Context, id LedgerID) ([]Txn, error)
}

// Access is the part of identity.Access the routes use.
type Access interface {
	Can(ctx context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error)
}

// HTTP serves the trust read views. Every dependency is required; a route answers 503 and
// calls nothing while one is missing.
type HTTP struct {
	Lists    Lists
	Ledgers  LedgerReads
	Access   Access
	Cursors  api.CursorCodec
	Problems server.ProblemWriter
	// Principal returns who the request acts as; false means unauthenticated.
	Principal func(r *http.Request) (registry.Principal, bool)
}

// idPattern is the openapi Id format (uuid).
var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// trustZone is the business time zone of trust dates.
var trustZone = func() *time.Location {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// Routes mounts the read routes. They are read-only: no write method is served on these
// paths, so they carry no write declaration.
func (h *HTTP) Routes(r chi.Router) {
	r.Get("/trust/accounts", h.listAccounts)
	r.Get("/trust/ledgers", h.listLedgers)
	r.Get("/trust/ledgers/{ledger_id}/lines", h.listLines)
}

// accountView is the openapi TrustAccount. The full account number never leaves the module.
type accountView struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Institution     string `json:"institution"`
	AccountNoMasked string `json:"account_no_masked"`
	ForMatterID     string `json:"for_matter_id,omitempty"`
	OpenedOn        string `json:"opened_on"`
}

func accountOf(a Account) accountView {
	typ := "mixed"
	if a.Type == AccountSeparateInterestBearing {
		typ = "separate"
	}
	return accountView{ID: string(a.ID), Type: typ, Institution: a.Institution,
		AccountNoMasked: "****" + a.AccountLast4, ForMatterID: a.ForMatterID,
		OpenedOn: a.OpenedOn.In(trustZone).Format(time.DateOnly)}
}

// ledgerView is the openapi TrustLedger.
type ledgerView struct {
	ID             string `json:"id"`
	AccountID      string `json:"account_id"`
	ClientID       string `json:"client_id"`
	MatterID       string `json:"matter_id"`
	BalanceCents   int64  `json:"balance_cents"`
	AvailableCents int64  `json:"available_cents"`
	ProtectedCents int64  `json:"protected_cents"`
}

func ledgerOf(l Ledger, b Balance) ledgerView {
	// Cents is non-negative: a ledger whose holds exceed its cleared money has nothing
	// available, not a negative amount.
	avail := b.AvailableCents
	if avail < 0 {
		avail = 0
	}
	return ledgerView{ID: string(l.ID), AccountID: string(l.AccountID), ClientID: l.ClientID, MatterID: l.MatterID,
		BalanceCents: b.PostedCents, AvailableCents: avail, ProtectedCents: b.ProtectedCents}
}

// lineView is the openapi TrustJournalLine.
type lineView struct {
	ID           string `json:"id"`
	AccountID    string `json:"account_id"`
	LedgerID     string `json:"ledger_id"`
	Kind         string `json:"kind"`
	PostedOn     string `json:"posted_on"`
	Method       string `json:"method,omitempty"`
	InstrumentNo string `json:"instrument_no,omitempty"`
	Counterparty string `json:"counterparty,omitempty"`
	AmountCents  int64  `json:"amount_cents"`
	Purpose      string `json:"purpose,omitempty"`
	ReversesID   string `json:"reverses_id,omitempty"`
	GateID       string `json:"gate_id,omitempty"`
	PostedBy     string `json:"posted_by"`
	PostedAt     string `json:"posted_at"`
}

// lineKinds maps transaction kinds to the openapi journal line kinds.
var lineKinds = map[TxnKind]string{
	KindOpeningBalance: "receipt",
	KindReceipt:        "receipt",
	KindInterest:       "receipt",
	KindDisbursement:   "disbursement",
	KindTransferIn:     "transfer",
	KindTransferOut:    "transfer",
	KindReversal:       "correction",
}

func lineOf(t Txn) lineView {
	day := t.TxnDate.In(trustZone)
	return lineView{ID: string(t.ID), AccountID: string(t.AccountID), LedgerID: string(t.LedgerID),
		Kind: lineKinds[t.Kind], PostedOn: day.Format(time.DateOnly), Method: string(t.Method),
		InstrumentNo: t.InstrumentNo, Counterparty: t.Counterparty, AmountCents: t.AmountCents,
		Purpose: t.Purpose, ReversesID: string(t.ReversesID), GateID: t.ApprovalID, PostedBy: t.PostedBy,
		// Txn carries the business date only; posted_at is that date's start in Toronto.
		PostedAt: day.Format(time.RFC3339)}
}

func (h *HTTP) ready() bool {
	return h.Lists != nil && h.Ledgers != nil && h.Access != nil && h.Cursors != nil && h.Problems != nil && h.Principal != nil
}

func (h *HTTP) problems() server.ProblemWriter {
	if h.Problems == nil {
		return &server.Problems{}
	}
	return h.Problems
}

func (h *HTTP) fail(w http.ResponseWriter, r *http.Request, status int, code, title string) {
	h.problems().WriteProblem(w, r, server.NewProblem(status, code, title))
}

// writeErr maps an unexpected error to a problem. The error text never reaches the body.
func (h *HTTP) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, api.ErrBadCursor) {
		h.problems().WriteProblem(w, r, api.BadCursorProblem())
		return
	}
	h.fail(w, r, http.StatusInternalServerError, server.CodeInternal, "Internal error")
}

// authorize answers 503, 401, 403 or 404 itself and returns false when the request cannot go
// on. It runs before any service call.
func (h *HTTP) authorize(w http.ResponseWriter, r *http.Request, s registry.Subject) bool {
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
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "Not found")
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

// cursor reads the page position; it answers 400 itself and returns false on a bad one.
func (h *HTTP) cursor(w http.ResponseWriter, r *http.Request) (api.Cursor, bool) {
	cur, err := api.FromRequest(r, h.Cursors)
	if errors.Is(err, api.ErrBadLimit) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "limit must be an integer")
		return api.Cursor{}, false
	}
	if err != nil {
		h.writeErr(w, r, err)
		return api.Cursor{}, false
	}
	return cur, true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// listAccounts serves GET /trust/accounts: every account the caller can see. The list is
// short, so it is one page.
func (h *HTTP) listAccounts(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r, registry.Subject{}) {
		return
	}
	got, err := h.Lists.ListAccounts(r.Context())
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[accountView]{Items: []accountView{}}
	for _, a := range got {
		page.Items = append(page.Items, accountOf(a))
	}
	writeJSON(w, http.StatusOK, page)
}

// listLedgers serves GET /trust/ledgers: ledgers by id, one page at a time, each with the
// balance trust.Ledgers.Balance reports.
func (h *HTTP) listLedgers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	matterID, accountID := q.Get("matter_id"), q.Get("account_id")
	subject := registry.Subject{}
	switch {
	case matterID != "" && idPattern.MatchString(matterID):
		subject = registry.Subject{Kind: matterkind.Subject, ID: matterID}
	case accountID != "" && idPattern.MatchString(accountID):
		subject = registry.Subject{Kind: AccountSubject, ID: accountID}
	}
	if !h.authorize(w, r, subject) {
		return
	}
	if (matterID != "" && !idPattern.MatchString(matterID)) || (accountID != "" && !idPattern.MatchString(accountID)) {
		h.fail(w, r, http.StatusBadRequest, server.CodeValidationFailed, "matter_id or account_id is not valid")
		return
	}
	cur, ok := h.cursor(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	// One extra row tells whether another page follows.
	got, err := h.Lists.ListLedgers(ctx, LedgerFilter{MatterID: matterID, AccountID: accountID, After: LedgerID(cur.After), Limit: cur.Limit + 1})
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	page := api.Page[ledgerView]{Items: []ledgerView{}}
	for i, l := range got {
		if i == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(got[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		b, err := h.Ledgers.Balance(ctx, l.ID)
		if err != nil {
			h.writeErr(w, r, err)
			return
		}
		page.Items = append(page.Items, ledgerOf(l, b))
	}
	writeJSON(w, http.StatusOK, page)
}

// listLines serves GET /trust/ledgers/{ledger_id}/lines: the ledger's journal in posting
// order. A malformed, unknown or hidden ledger answers 404.
func (h *HTTP) listLines(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "ledger_id")
	if !idPattern.MatchString(id) {
		if h.authorize(w, r, registry.Subject{}) {
			h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such ledger")
		}
		return
	}
	if !h.authorize(w, r, registry.Subject{Kind: LedgerSubject, ID: id}) {
		return
	}
	cur, ok := h.cursor(w, r)
	if !ok {
		return
	}
	txns, err := h.Ledgers.Txns(r.Context(), LedgerID(id))
	if errors.Is(err, ErrNotFound) {
		h.fail(w, r, http.StatusNotFound, CodeNotFound, "No such ledger")
		return
	}
	if err != nil {
		h.writeErr(w, r, err)
		return
	}
	start := 0
	if cur.After != "" {
		start = -1
		for i, t := range txns {
			if string(t.ID) == cur.After {
				start = i + 1
				break
			}
		}
		if start < 0 {
			h.problems().WriteProblem(w, r, api.BadCursorProblem())
			return
		}
	}
	page := api.Page[lineView]{Items: []lineView{}}
	for i := start; i < len(txns); i++ {
		if len(page.Items) == cur.Limit {
			next, err := h.Cursors.Encode(api.Cursor{After: string(txns[i-1].ID), Limit: cur.Limit})
			if err != nil {
				h.writeErr(w, r, err)
				return
			}
			page.Next = next
			break
		}
		page.Items = append(page.Items, lineOf(txns[i]))
	}
	writeJSON(w, http.StatusOK, page)
}
