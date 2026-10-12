package books

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/eventroute"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// CapReconcile is the capability to prepare a bank reconciliation (and to sign one, through
// the gate, when the signer is not the preparer).
const CapReconcile = "trust.reconcile"

// SubjectOpRecon is the gate subject kind of a gl.operating_reconciliation row.
const SubjectOpRecon = "gl.operating_reconciliation"

// Problem codes of the operating reconciliation route.
const (
	CodeReconUnbalanced = "reconciliation_unbalanced"
	CodeReconExists     = "reconciliation_exists"
)

// ErrNotOperatingBank means the account is not the operating bank account.
var ErrNotOperatingBank = errors.New("books: not the operating bank account")

// ErrReconExists means the account already has a reconciliation for the period end.
var ErrReconExists = errors.New("books: operating reconciliation already prepared")

// maxCents bounds an amount the route accepts.
const maxCents = 100_000_000_000

// maxOutstanding bounds the outstanding items on one reconciliation.
const maxOutstanding = 500

var idPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// OutstandingItem is a reconciling item: a deposit in transit (positive) or an outstanding
// cheque (negative), in integer cents.
type OutstandingItem struct {
	Description string `json:"description"`
	AmountCents int64  `json:"amount_cents"`
}

// OpReconDraft is an operating reconciliation as its preparer records it.
type OpReconDraft struct {
	AccountID         string
	PeriodEnd         string
	BankBalanceCents  int64
	Outstanding       []OutstandingItem
	AdjustedBankCents int64
	BookBalanceCents  int64
	PreparedBy        string
}

// OpReconPreparer reads the book balance and records a prepared reconciliation. Every method
// runs in the caller's tx. PGOpRecon implements it.
type OpReconPreparer interface {
	// BookBalance is the GL balance of the operating bank account accountID at periodEnd, or
	// ErrNotOperatingBank.
	BookBalance(ctx context.Context, tx pgx.Tx, accountID, periodEnd string) (int64, error)
	// Prepare records d as ready and returns its id, or ErrReconExists.
	Prepare(ctx context.Context, tx pgx.Tx, d OpReconDraft) (string, error)
}

// OpReconHTTP serves POST /operating-reconciliations: it records a ready operating bank
// reconciliation with the book balance read from the GL and opens gl.operating_reconciliation
// with the acting user as preparer. Only the gate's effect signs it.
type OpReconHTTP struct {
	Route  eventroute.Route
	DB     eventroute.TxRunner
	Gates  eventroute.Gates
	Recons OpReconPreparer
	// MinApprovers is the gl.operating_reconciliation approver count for the Gate body.
	MinApprovers int
}

func init() { add(registerOpReconHTTP) }

// registerOpReconHTTP mounts POST /operating-reconciliations. Boot wiring supplies the dependencies; until it does, the
// route answers 503.
func registerOpReconHTTP(r registry.Registry) error {
	r.Routes((&OpReconHTTP{}).Routes)
	return nil
}

// Routes mounts the route.
func (h *OpReconHTTP) Routes(r chi.Router) {
	// rule: ARC-I-012
	// The route records the reconciliation; signing it is gl.operating_reconciliation's.
	r.Method(http.MethodPost, "/operating-reconciliations", server.Declare(h.create))
}

type opReconBody struct {
	AccountID        string            `json:"account_id"`
	PeriodEnd        string            `json:"period_end"`
	BankBalanceCents *int64            `json:"bank_balance_cents"`
	Outstanding      []OutstandingItem `json:"outstanding_items"`
}

// adjusted checks the body and returns the adjusted bank balance, or false.
func (b opReconBody) adjusted() (int64, bool) {
	if !idPattern.MatchString(b.AccountID) || b.BankBalanceCents == nil || *b.BankBalanceCents < 0 ||
		*b.BankBalanceCents > maxCents || len(b.Outstanding) > maxOutstanding {
		return 0, false
	}
	if _, err := time.Parse(time.DateOnly, b.PeriodEnd); err != nil {
		return 0, false
	}
	// rule: BIL-C-064
	// Integer cents only, each item bounded, so the sum cannot overflow.
	sum := *b.BankBalanceCents
	for _, it := range b.Outstanding {
		if it.AmountCents == 0 || it.AmountCents > maxCents || it.AmountCents < -maxCents ||
			strings.TrimSpace(it.Description) == "" || len(it.Description) > 200 {
			return 0, false
		}
		sum += it.AmountCents
	}
	return sum, true
}

func (h *OpReconHTTP) create(w http.ResponseWriter, r *http.Request) {
	ready := h.DB != nil && h.Gates != nil && h.Recons != nil
	h.Route.Serve(w, r, ready, CapReconcile, func(w http.ResponseWriter, r *http.Request, p registry.Principal) {
		var b opReconBody
		if eventroute.Decode(r, &b) != nil {
			h.Route.Invalid(w, r, "Body is not valid")
			return
		}
		adjusted, ok := b.adjusted()
		if !ok {
			h.Route.Invalid(w, r, "Body is not valid")
			return
		}
		h.Route.OpenGate(w, r, h.DB, h.Gates, p, KindOperatingReconciliation, h.MinApprovers,
			func(ctx context.Context, tx pgx.Tx) (eventroute.Prepared, error) {
				return h.prepare(ctx, tx, b, adjusted, eventroute.User(p))
			})
	})
}

func (h *OpReconHTTP) prepare(ctx context.Context, tx pgx.Tx, b opReconBody, adjusted int64, by string) (eventroute.Prepared, error) {
	// rule: BIL-L-034
	// Only the operating bank is reconciled here; the trust bank has its own sign-off.
	book, err := h.Recons.BookBalance(ctx, tx, b.AccountID, b.PeriodEnd)
	if errors.Is(err, ErrNotOperatingBank) {
		return eventroute.Prepared{}, &eventroute.Refusal{Status: http.StatusUnprocessableEntity,
			Code: server.CodeValidationFailed, Title: "Account is not the operating bank account"}
	}
	if err != nil {
		return eventroute.Prepared{}, err
	}
	// rule: BIL-I-009
	// Only a reconciled month goes to sign-off: the adjusted bank balance equals the books.
	if book < 0 || adjusted != book {
		return eventroute.Prepared{}, &eventroute.Refusal{Status: http.StatusUnprocessableEntity,
			Code: CodeReconUnbalanced, Title: "Adjusted bank balance does not equal the book balance"}
	}
	id, err := h.Recons.Prepare(ctx, tx, OpReconDraft{AccountID: b.AccountID, PeriodEnd: b.PeriodEnd,
		BankBalanceCents: *b.BankBalanceCents, Outstanding: b.Outstanding, AdjustedBankCents: adjusted,
		BookBalanceCents: book, PreparedBy: by})
	if errors.Is(err, ErrReconExists) {
		return eventroute.Prepared{}, &eventroute.Refusal{Status: http.StatusConflict, Code: CodeReconExists,
			Title: "The account already has a reconciliation for this period end"}
	}
	if err != nil {
		return eventroute.Prepared{}, err
	}
	raw, err := json.Marshal(opReconPayload{AccountID: b.AccountID, PeriodEnd: b.PeriodEnd,
		BankBalanceCents: b.BankBalanceCents, BookBalanceCents: &book})
	if err != nil {
		return eventroute.Prepared{}, err
	}
	return eventroute.Prepared{Subject: gate.Subject{Kind: SubjectOpRecon, ID: id}, Payload: raw}, nil
}

var _ OpReconPreparer = PGOpRecon{}

// operatingBankSQL finds the operating bank account by id.
const operatingBankSQL = `SELECT EXISTS (SELECT 1 FROM gl.account WHERE id = $1::uuid AND code = $2)`

const bookBalanceSQL = `SELECT coalesce(sum(l.debit_cents - l.credit_cents), 0)::bigint
FROM gl.journal_line l JOIN gl.journal_entry e ON e.id = l.entry_id
WHERE l.account_id = $1::uuid AND e.entry_date <= $2::date`

const prepareOpReconSQL = `INSERT INTO gl.operating_reconciliation (account_id, period_end, bank_balance_cents,
  outstanding_items, adjusted_bank_cents, book_balance_cents, prepared_by, status)
VALUES ($1::uuid, $2::date, $3, $4::jsonb, $5, $6, $7::uuid, 'ready') RETURNING id::text`

// BookBalance implements OpReconPreparer.
func (PGOpRecon) BookBalance(ctx context.Context, tx pgx.Tx, accountID, periodEnd string) (int64, error) {
	var ok bool
	if err := tx.QueryRow(ctx, operatingBankSQL, accountID, AcctOperatingBank).Scan(&ok); err != nil {
		return 0, fmt.Errorf("operating bank %s: %w", accountID, err)
	}
	if !ok {
		return 0, fmt.Errorf("account %s: %w", accountID, ErrNotOperatingBank)
	}
	var sum int64
	if err := tx.QueryRow(ctx, bookBalanceSQL, accountID, periodEnd).Scan(&sum); err != nil {
		return 0, fmt.Errorf("book balance %s: %w", accountID, err)
	}
	return sum, nil
}

// Prepare implements OpReconPreparer.
func (PGOpRecon) Prepare(ctx context.Context, tx pgx.Tx, d OpReconDraft) (string, error) {
	items := d.Outstanding
	if items == nil {
		items = []OutstandingItem{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("prepare reconciliation: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, prepareOpReconSQL, d.AccountID, d.PeriodEnd, d.BankBalanceCents, raw,
		d.AdjustedBankCents, d.BookBalanceCents, d.PreparedBy).Scan(&id)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return "", fmt.Errorf("prepare reconciliation %s %s: %w", d.AccountID, d.PeriodEnd, ErrReconExists)
	}
	if err != nil {
		return "", fmt.Errorf("prepare reconciliation: %w", err)
	}
	return id, nil
}
