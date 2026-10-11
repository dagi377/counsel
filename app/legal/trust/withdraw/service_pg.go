package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
)

// LedgerLocker locks a client ledger in the caller's tx and returns its balance and the sum of
// its authorised-not-executed holds; trust.LedgerStore implements it.
type LedgerLocker interface {
	LockLedger(ctx context.Context, tx pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error)
}

// PGService is Service over trust.withdrawal_request. It records requests and reads them; it
// never authorizes or executes. Authorization is the trust.withdrawal gate's effect (Effect.Apply,
// a signatory other than the preparer) and execution is Execution.Record, so Authorize and
// RecordExecution here always refuse. With Gates set it opens the withdrawal's gate
// (GateKind, or GateFeeKind for a withdrawal that pays a bill) in the request's tx and
// stores its id, so every caller (the route and the agent tool) gets a gated withdrawal.
type PGService struct {
	DB      TxRunner
	Ledgers LedgerLocker
	// Gates, when set, opens the gate in the request tx. Nil leaves opening to the caller.
	Gates Gates
}

var _ Service = PGService{}

// ErrEffectOnly is returned by PGService.Authorize and RecordExecution: those steps run only
// through the gate effect and the execution route.
var ErrEffectOnly = errors.New("withdraw: authorize and execute run only through the gate effect")

// dbPurpose maps the openapi purpose_category (Request.Reason) onto the stored one. A fee
// withdrawal is stored with its bill (fees_to_operating requires bill_id); one that names no
// bill is refused here, and the effect re-checks the bill at authorization.
var dbPurpose = map[string]string{
	"fees":              "fees_to_operating",
	"fees_to_operating": "fees_to_operating",
	"disbursement":      "disbursement",
	"third_party":       "third_party",
	"client_payment":    "client_refund",
	"refund":            "client_refund",
}

const ledgerOfSQL = `SELECT id::text FROM trust.client_ledger WHERE account_id = $1::uuid AND matter_id = $2::uuid`

const insertRequestSQL = `INSERT INTO trust.withdrawal_request
  (ledger_id, purpose_category, payee, amount_cents, method, form_9a_doc_id, initiated_by, bill_id, status)
VALUES ($1::uuid, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7::uuid, NULLIF($8, '')::uuid, 'pending_authorization')
RETURNING id::text`

// ledgerOf returns the client ledger of the matter on the account, or ErrNotFound.
func ledgerOf(ctx context.Context, tx pgx.Tx, account, matter string) (trust.LedgerID, error) {
	var id string
	err := tx.QueryRow(ctx, ledgerOfSQL, account, matter).Scan(&id)
	var pg *pgconn.PgError
	if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == "22P02") {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("ledger: %w", err)
	}
	return trust.LedgerID(id), nil
}

// funds locks the matter's ledger and reads its trust position. Uncleared is posted minus
// cleared, as stored: nothing is clamped.
func (s PGService) funds(ctx context.Context, tx pgx.Tx, account, matter string) (trust.LedgerID, Funds, error) {
	id, err := ledgerOf(ctx, tx, account, matter)
	if err != nil {
		return "", Funds{}, err
	}
	// rule: ARC-I-025
	b, held, err := s.Ledgers.LockLedger(ctx, tx, id)
	if err != nil {
		return "", Funds{}, fmt.Errorf("lock ledger: %w", err)
	}
	return id, Funds{ClearedCents: b.ClearedCents, UnclearedCents: b.PostedCents - b.ClearedCents,
		ProtectedCents: b.ProtectedCents, MinimumCents: b.MinimumCents, AuthorizedCents: held}, nil
}

func (s PGService) ready() error {
	if s.DB == nil || s.Ledgers == nil {
		return fmt.Errorf("withdraw service: DB and Ledgers: %w", ErrInvalid)
	}
	return nil
}

// Request implements Service: under the ledger lock it validates r against the available
// balance (Validate, which fails closed while FlagBylaw9Verified is off) and records it as
// pending authorization. Request.IdempotencyKey is not stored: the route's idem layer replays
// a repeated key.
func (s PGService) Request(ctx context.Context, r Request) (Withdrawal, error) {
	// rule: BIL-C-053 (verify)
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return Withdrawal{}, ErrUnverified
	}
	if err := s.ready(); err != nil {
		return Withdrawal{}, err
	}
	purpose, ok := dbPurpose[r.Reason]
	if !ok {
		return Withdrawal{}, fmt.Errorf("request withdrawal: purpose %q: %w", r.Reason, ErrInvalid)
	}
	// rule: ARC-I-027
	// The preparer is a person: for an agent request, the user the run acts for. A request
	// with no preparer could later be authorized by anyone, so it is refused.
	if r.PreparedBy == "" {
		return Withdrawal{}, fmt.Errorf("request withdrawal: no preparer: %w", ErrInvalid)
	}
	// rule: BIL-L-035 (verify)
	// A fee withdrawal names its bill; the effect re-checks it at authorization.
	if purpose == "fees_to_operating" && r.BillID == "" {
		return Withdrawal{}, fmt.Errorf("request withdrawal: %w: %w", ErrBillMatter, ErrInvalid)
	}
	out := Withdrawal{Request: r, Status: StatusRequested}
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ledger, f, err := s.funds(ctx, tx, r.TrustAccountID, r.MatterID)
		if err != nil {
			return err
		}
		if err := Validate(ctx, r, f); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, insertRequestSQL, string(ledger), purpose, r.Payee, r.AmountCents, string(r.Method),
			r.EFTAuthorizationID, r.PreparedBy, r.BillID).Scan(&out.ID); err != nil {
			return err
		}
		if s.Gates == nil {
			return nil
		}
		gid, err := openRequestGate(ctx, tx, s.Gates, out.ID, string(ledger), r)
		if err != nil {
			return err
		}
		out.GateID = gid
		return nil
	})
	if err != nil {
		return Withdrawal{}, fmt.Errorf("request withdrawal: %w", err)
	}
	return out, nil
}

// Available implements Service: cleared minus protected, minimum and authorised holds, read
// under the ledger lock. A negative figure is returned as it is.
func (s PGService) Available(ctx context.Context, trustAccountID, matterID string) (int64, error) {
	if err := s.ready(); err != nil {
		return 0, err
	}
	var avail int64
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, f, err := s.funds(ctx, tx, trustAccountID, matterID)
		avail = f.Available()
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("available: %w", err)
	}
	return avail, nil
}

// Get implements Service.
func (s PGService) Get(ctx context.Context, id string) (Withdrawal, error) {
	if s.DB == nil {
		return Withdrawal{}, s.ready()
	}
	l, err := PGLists{DB: s.DB}.Listed(ctx, id)
	if err != nil {
		return Withdrawal{}, err
	}
	return l.Withdrawal, nil
}

// Authorize implements Service by refusing: a withdrawal is authorized only by the gate
// effect, by a signatory other than the preparer, never by an agent.
func (PGService) Authorize(_ context.Context, id, _ string, _ time.Time) (Withdrawal, error) {
	// rule: ARC-I-027
	return Withdrawal{}, fmt.Errorf("authorize withdrawal %s: %w: %w", id, ErrEffectOnly, ErrNotAuthorized)
}

// RecordExecution implements Service by refusing: execution is recorded only through
// Execution.Record, which re-checks the balance under lock.
func (PGService) RecordExecution(_ context.Context, id, _ string, _ time.Time) (Withdrawal, error) {
	return Withdrawal{}, fmt.Errorf("execute withdrawal %s: %w: %w", id, ErrEffectOnly, ErrNotAuthorized)
}

// RequestGateKind is the gate a withdrawal request opens: GateFeeKind when it pays a bill,
// GateKind otherwise.
func RequestGateKind(r Request) string {
	// rule: BIL-L-035 (verify)
	// A withdrawal that pays a bill is a fee transfer and opens the fee gate.
	if r.BillID != "" {
		return GateFeeKind
	}
	return GateKind
}

// RequestGatePayload is the gate payload of withdrawal id on ledger for r.
func RequestGatePayload(id, ledger string, r Request) (json.RawMessage, error) {
	p := map[string]any{"withdrawal_id": id, "ledger_id": ledger, "matter_id": r.MatterID,
		"amount_cents": r.AmountCents, "payee": r.Payee, "method": string(r.Method), "purpose_category": r.Reason}
	if r.BillID != "" {
		p["bill_id"] = r.BillID
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("withdrawal gate payload: %w", err)
	}
	return raw, nil
}

// openRequestGate opens the gate of withdrawal id in tx, prepared by r.PreparedBy (for an
// agent, the user it acts for), and stores the gate id on the row.
func openRequestGate(ctx context.Context, tx pgx.Tx, g Gates, id, ledger string, r Request) (string, error) {
	payload, err := RequestGatePayload(id, ledger, r)
	if err != nil {
		return "", err
	}
	// rule: ARC-I-019
	// The preparer is a person and is recorded as the gate's requester, so they never decide it.
	gid, err := g.Open(ctx, tx, RequestGateKind(r), gate.Subject{Kind: SubjectWithdrawal, ID: id}, payload,
		gate.Principal{Kind: gate.PrincipalUser, UserID: r.PreparedBy})
	if err != nil {
		return "", fmt.Errorf("open withdrawal gate: %w", err)
	}
	if _, err := tx.Exec(ctx, recordGateSQL, id, string(gid)); err != nil {
		return "", fmt.Errorf("record withdrawal gate: %w", err)
	}
	return string(gid), nil
}
