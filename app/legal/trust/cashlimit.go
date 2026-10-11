package trust

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/legalflags"
)

// Cash limit on one client file (LSO By-Law 9 ss.4-6, verify). Cash received in trust and cash
// fee payments into operating both count.
const (
	// CashLimitCents blocks a cash receipt that brings the file to "$7,500 or more".
	CashLimitCents int64 = 750000
	// CashWarnCents warns once the file reaches $6,000.
	CashWarnCents int64 = 600000
)

// Cash exception codes (By-Law 9 s.6, verify). A receipt at or over the limit needs one of
// them; the trust.cash_over_limit gate (G6c) holds the evidence for it.
const (
	CashFromFinancialInstitution = "from_financial_institution"
	CashFromPublicBody           = "from_public_body"
	CashFromPeaceOfficer         = "from_peace_officer"
	CashForBail                  = "bail"
	CashFineOrPenalty            = "fine_or_penalty"
	CashFeesRefundedInCash       = "fees_refunded_in_cash"
)

// validCashException reports whether code is a known By-Law 9 s.6 exception.
func validCashException(code string) bool {
	switch code {
	case CashFromFinancialInstitution, CashFromPublicBody, CashFromPeaceOfficer, CashForBail,
		CashFineOrPenalty, CashFeesRefundedInCash:
		return true
	}
	return false
}

// EvaluateCash is the one cash-limit check, shared by the trust receipt and operating payment
// paths. priorCents is the cash already received on the file (trust and operating); the
// result is how the file stands once amountCents more is received.
func EvaluateCash(priorCents, amountCents int64) CashCheck {
	total := priorCents + amountCents
	// rule: BIL-L-039 (verify)
	return CashCheck{TotalCents: total, Warn: total >= CashWarnCents, Blocked: total >= CashLimitCents}
}

// cashRepo reads the cash already received on a client file. pgLedgerRepo implements it over
// billing.cash_received_cents; the repo behind a LedgerStore must implement it.
type cashRepo interface {
	// lockMatterCash takes the per-matter cash lock for the rest of tx.
	lockMatterCash(ctx context.Context, tx pgx.Tx, matterID string) error
	// cashReceivedCents is cash received on the matter: trust cash receipts plus operating
	// cash payments.
	cashReceivedCents(ctx context.Context, tx pgx.Tx, matterID string) (int64, error)
}

func (s *LedgerStore) cashRepo() (cashRepo, error) {
	c, ok := s.repo.(cashRepo)
	if !ok {
		return nil, fmt.Errorf("cash limit: store has no cash source: %w", ErrInvalid)
	}
	return c, nil
}

// checkCashTx locks the matter's cash and evaluates amountCents against the limit in tx.
func (s *LedgerStore) checkCashTx(ctx context.Context, tx pgx.Tx, matterID string, amountCents int64) (CashCheck, error) {
	c, err := s.cashRepo()
	if err != nil {
		return CashCheck{}, err
	}
	// rule: BIL-L-039 (verify)
	// Lock first, then read: two cash receipts on one file cannot both pass under the limit.
	if err := c.lockMatterCash(ctx, tx, matterID); err != nil {
		return CashCheck{}, fmt.Errorf("cash lock: %w", err)
	}
	prior, err := c.cashReceivedCents(ctx, tx, matterID)
	if err != nil {
		return CashCheck{}, fmt.Errorf("cash received: %w", err)
	}
	return EvaluateCash(prior, amountCents), nil
}

// CheckCash implements Ledgers. Behind FlagBylaw9Verified; off, it fails closed with
// ErrUnverified.
func (s *LedgerStore) CheckCash(ctx context.Context, matterID string, amountCents int64) (CashCheck, error) {
	if matterID == "" || amountCents <= 0 {
		return CashCheck{}, fmt.Errorf("check cash: %w", ErrInvalid)
	}
	// rule: BIL-L-039 (verify)
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return CashCheck{}, fmt.Errorf("check cash: %w", ErrUnverified)
	}
	var out CashCheck
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = s.checkCashTx(ctx, tx, matterID, amountCents)
		return err
	})
	if err != nil {
		return CashCheck{}, fmt.Errorf("check cash: %w", err)
	}
	return out, nil
}

// checkCashReceipt is the Receive check for cash: it refuses a cash receipt that brings the
// file to the limit unless it carries a By-Law 9 s.6 exception code. Other methods pass.
func checkCashReceipt(ctx context.Context, s *LedgerStore, tx pgx.Tx, l Ledger, r Receipt) error {
	if r.Method != MethodCash {
		return nil
	}
	// rule: BIL-L-039 (verify)
	// Fails closed: an unconfirmed cash rule never lets cash into trust unchecked.
	if !legalflags.Enabled(ctx, FlagBylaw9Verified) {
		return fmt.Errorf("cash limit: %w", ErrUnverified)
	}
	if r.CashExceptionCode != "" && !validCashException(r.CashExceptionCode) {
		return fmt.Errorf("cash exception code: %w", ErrInvalid)
	}
	c, err := s.checkCashTx(ctx, tx, l.MatterID, r.AmountCents)
	if err != nil {
		return fmt.Errorf("cash limit: %w", err)
	}
	// rule: BIL-L-039 (verify)
	if c.Blocked && r.CashExceptionCode == "" {
		return fmt.Errorf("cash on file %d: %w", c.TotalCents, ErrCashLimit)
	}
	return nil
}

func init() { addReceiveCheck(checkCashReceipt) }

// lockMatterCashSQL is the per-matter cash lock that billing.cash_received_cents callers take.
const lockMatterCashSQL = `SELECT pg_advisory_xact_lock(hashtext($1::uuid::text))`

func (pgLedgerRepo) lockMatterCash(ctx context.Context, tx pgx.Tx, matterID string) error {
	if _, err := tx.Exec(ctx, lockMatterCashSQL, matterID); err != nil {
		return fmt.Errorf("lock matter cash: %w", err)
	}
	return nil
}

const cashReceivedSQL = `SELECT billing.cash_received_cents($1::uuid)`

func (pgLedgerRepo) cashReceivedCents(ctx context.Context, tx pgx.Tx, matterID string) (int64, error) {
	var cents int64
	if err := tx.QueryRow(ctx, cashReceivedSQL, matterID).Scan(&cents); err != nil {
		return 0, fmt.Errorf("cash received: %w", err)
	}
	return cents, nil
}
