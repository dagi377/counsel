package trust

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// pgCheckViolation is SQLSTATE check_violation, the code the trust balance guards raise.
const pgCheckViolation = "23514"

// Rule ids the database's own trust balance guards put first in their message.
const (
	// dmLedgerNonNegative: a client ledger never goes below zero. The constraint trigger is
	// DEFERRABLE INITIALLY DEFERRED, so it fires at COMMIT, not at the insert.
	dmLedgerNonNegative = "DM-052"
	// dmAvailableFunds: money out uses only available funds; raised at the insert, or when a
	// withdrawal is authorised.
	dmAvailableFunds = "DM-064"
)

// ledgerCheck maps a trust balance guard violation (SQLSTATE 23514 raised by DM-052 or
// DM-064) to ErrInsufficientFunds, so a caller sees 409 insufficient_trust_funds and never a
// 500. Any other error, including other CHECK violations, is returned unchanged. The database
// message names the ledger and its balance, so it is not carried into the returned error.
func ledgerCheck(err error) error {
	var pg *pgconn.PgError
	if err == nil || !errors.As(err, &pg) || pg.Code != pgCheckViolation {
		return err
	}
	for _, dm := range []string{dmLedgerNonNegative, dmAvailableFunds} {
		// rule: BIL-I-010
		if strings.HasPrefix(pg.Message, dm+":") {
			return fmt.Errorf("trust balance guard %s: %w", dm, ErrInsufficientFunds)
		}
	}
	return err
}

// InLedgerTx runs fn in one transaction and commits it. Postings in fn serialise on each
// ledger's row lock (SELECT FOR UPDATE, taken by Disburse and Receive before they read), so
// concurrent withdrawals from one ledger queue rather than deadlock. A balance guard the
// database raises, at an insert or at COMMIT through the deferred DM-052 trigger, comes back
// as ErrInsufficientFunds.
func (s *LedgerStore) InLedgerTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	// rule: BIL-I-010
	if err := ledgerCheck(s.db.InTx(ctx, fn)); err != nil {
		return fmt.Errorf("ledger tx: %w", err)
	}
	return nil
}
