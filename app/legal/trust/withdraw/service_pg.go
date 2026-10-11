package withdraw

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/legalflags"
	"github.com/dagi377/counsel/app/legal/trust"
)

// LedgerLocker locks a client ledger in the caller's tx and returns its balance and the sum of
// its authorised-not-executed holds; trust.LedgerStore implements it.
type LedgerLocker interface {
	LockLedger(ctx context.Context, tx pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error)
}

// PGService is Service over trust.withdrawal_request. It records requests and reads them; it
// never authorizes or executes. Authorization is the trust.withdrawal gate's effect (Effect.Apply,
// a signatory other than the preparer) and execution is Execution.Record, so Authorize and
// RecordExecution here always refuse. It does not open the gate: the request route does.
type PGService struct {
	DB      TxRunner
	Ledgers LedgerLocker
}

var _ Service = PGService{}

// ErrEffectOnly is returned by PGService.Authorize and RecordExecution: those steps run only
// through the gate effect and the execution route.
var ErrEffectOnly = errors.New("withdraw: authorize and execute run only through the gate effect")

// dbPurpose maps the openapi purpose_category (Request.Reason) onto the stored one. A fee
// withdrawal needs its bill (fees_to_operating requires bill_id) and Request carries none, so
// fees are refused here rather than stored without one.
var dbPurpose = map[string]string{
	"disbursement":   "disbursement",
	"third_party":    "third_party",
	"client_payment": "client_refund",
	"refund":         "client_refund",
}

const ledgerOfSQL = `SELECT id::text FROM trust.client_ledger WHERE account_id = $1::uuid AND matter_id = $2::uuid`

const insertRequestSQL = `INSERT INTO trust.withdrawal_request
  (ledger_id, purpose_category, payee, amount_cents, method, form_9a_doc_id, initiated_by, status)
VALUES ($1::uuid, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7::uuid, 'pending_authorization')
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
	out := Withdrawal{Request: r, Status: StatusRequested}
	err := s.DB.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		ledger, f, err := s.funds(ctx, tx, r.TrustAccountID, r.MatterID)
		if err != nil {
			return err
		}
		if err := Validate(ctx, r, f); err != nil {
			return err
		}
		return tx.QueryRow(ctx, insertRequestSQL, string(ledger), purpose, r.Payee, r.AmountCents, string(r.Method),
			r.EFTAuthorizationID, r.PreparedBy).Scan(&out.ID)
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
