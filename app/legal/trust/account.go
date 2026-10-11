package trust

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/kernel/db"
)

// FlagBylaw9Verified gates the By-Law 9 checks marked (verify). Off by default.
const FlagBylaw9Verified = "trust.bylaw9_verified"

// FlagReader is the slice of kernel/flags this file reads.
type FlagReader interface {
	Enabled(name string) bool
}

// Sealer encrypts a full bank account number before it is stored.
type Sealer interface {
	Seal(ctx context.Context, plaintext []byte) ([]byte, error)
}

const (
	pgUniqueViolation = "23505"
	pgInvalidText     = "22P02"
)

var last4RE = regexp.MustCompile(`^[0-9]{4}$`)

// AccountStore keeps trust.trust_account and trust.signatory. Writes run in the caller's tx so
// they commit with the gate effect that approved them; reads open their own tx.
type AccountStore struct {
	db    db.DB
	users signatory.Directory
	seal  Sealer
	flags FlagReader
}

// NewAccountStore returns an AccountStore.
func NewAccountStore(pool db.DB, users signatory.Directory, seal Sealer, flags FlagReader) *AccountStore {
	return &AccountStore{db: pool, users: users, seal: seal, flags: flags}
}

// ErrNeedsApproval means a contract method lacks data the table requires (Q-004): use
// OpenAccountNumber or AddApprovedSignatory.
var ErrNeedsApproval = fmt.Errorf("account number or approval id required (Q-004): %w", ErrInvalid)

// validateAccount checks an account before it is opened. Errors name fields, never values.
func validateAccount(a Account) error {
	switch a.Type {
	case AccountMixed, AccountSeparateInterestBearing:
	default:
		return fmt.Errorf("type: %w", ErrInvalid)
	}
	if a.Institution == "" || a.Title == "" || a.OpenedOn.IsZero() {
		return fmt.Errorf("missing field: %w", ErrInvalid)
	}
	if !last4RE.MatchString(a.AccountLast4) {
		return fmt.Errorf("account last4: %w", ErrInvalid)
	}
	if (a.Type == AccountSeparateInterestBearing) != (a.ForMatterID != "") {
		return fmt.Errorf("for matter only on separate accounts: %w", ErrInvalid)
	}
	return nil
}

// OpenAccount implements Accounts. The contract carries no full account number, which the
// table requires sealed, so it always returns ErrNeedsApproval (Q-004).
func (s *AccountStore) OpenAccount(_ context.Context, _ pgx.Tx, a Account) (AccountID, error) {
	if err := validateAccount(a); err != nil {
		return "", fmt.Errorf("open account: %w", err)
	}
	return "", fmt.Errorf("open account: %w", ErrNeedsApproval)
}

const insertAccountSQL = `INSERT INTO trust.trust_account
  (type, institution, account_no_ciphertext, account_no_last4, title, for_matter_id, opened_on)
VALUES ($1, $2, $3, $4, $5, NULLIF($6, '')::uuid, $7::date)
RETURNING id::text`

// OpenAccountNumber opens a trust account whose full number is sealed before it is stored.
// The account has no signatory yet, so it accepts no receipt until one is added (MIG-035).
func (s *AccountStore) OpenAccountNumber(ctx context.Context, tx pgx.Tx, a Account, accountNo string) (AccountID, error) {
	if err := validateAccount(a); err != nil {
		return "", fmt.Errorf("open account: %w", err)
	}
	if len(accountNo) < 4 || accountNo[len(accountNo)-4:] != a.AccountLast4 {
		return "", fmt.Errorf("open account: account number does not end in last4: %w", ErrInvalid)
	}
	sealed, err := s.seal.Seal(ctx, []byte(accountNo))
	if err != nil {
		return "", fmt.Errorf("open account: seal: %w", err)
	}
	var id string
	if err := tx.QueryRow(ctx, insertAccountSQL, string(a.Type), a.Institution, sealed, a.AccountLast4,
		a.Title, a.ForMatterID, a.OpenedOn.Format(time.DateOnly)).Scan(&id); err != nil {
		return "", fmt.Errorf("open account: insert: %w", err)
	}
	return AccountID(id), nil
}

// AddSignatory implements Accounts. trust.signatory needs the GA1 approval id, which the
// contract does not carry, so it always returns ErrNeedsApproval (Q-004).
func (s *AccountStore) AddSignatory(_ context.Context, _ pgx.Tx, id AccountID, userID string) error {
	if id == "" || userID == "" {
		return fmt.Errorf("add signatory: missing field: %w", ErrInvalid)
	}
	return fmt.Errorf("add signatory: %w", ErrNeedsApproval)
}

const insertSignatorySQL = `INSERT INTO trust.signatory (account_id, user_id, is_licensee, valid_from, approval_id)
VALUES ($1::uuid, $2::uuid, true, $3::date, $4::uuid)`

// AddApprovedSignatory adds an active licensee as a signatory under the GA1 approval that
// authorised it, from validFrom (a business date). Bonded non-licensees go through the
// signatory module, which records bond evidence.
func (s *AccountStore) AddApprovedSignatory(ctx context.Context, tx pgx.Tx, id AccountID, userID, approvalID string, validFrom time.Time) error {
	if id == "" || userID == "" || approvalID == "" || validFrom.IsZero() {
		return fmt.Errorf("add signatory: missing field: %w", ErrInvalid)
	}
	u, err := s.users.User(ctx, userID)
	if err != nil {
		return fmt.Errorf("add signatory: user: %w", err)
	}
	if !u.Active || !u.Licensee {
		return fmt.Errorf("add signatory: %w", signatory.ErrNotEligible)
	}
	if _, err := tx.Exec(ctx, insertSignatorySQL, string(id), userID, validFrom.Format(time.DateOnly), approvalID); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == pgUniqueViolation {
			return fmt.Errorf("add signatory: already active: %w", ErrInvalid)
		}
		return fmt.Errorf("add signatory: insert: %w", err)
	}
	return nil
}

// lockAccountSQL locks the account row so signatory changes and receipts on one account
// serialise.
const lockAccountSQL = `SELECT id FROM trust.trust_account WHERE id = $1::uuid FOR UPDATE`

const activeSignatoriesSQL = `SELECT user_id::text FROM trust.signatory
WHERE account_id = $1::uuid AND valid_to IS NULL ORDER BY user_id`

// RemoveSignatory implements Accounts. The row is end-dated, never deleted, and the last
// active signatory cannot be removed: the account would stop accepting receipts (MIG-035).
func (s *AccountStore) RemoveSignatory(ctx context.Context, tx pgx.Tx, id AccountID, userID string) error {
	if id == "" || userID == "" {
		return fmt.Errorf("remove signatory: missing field: %w", ErrInvalid)
	}
	if err := lockAccount(ctx, tx, id); err != nil {
		return fmt.Errorf("remove signatory: %w", err)
	}
	active, err := queryStrings(ctx, tx, activeSignatoriesSQL, string(id))
	if err != nil {
		return fmt.Errorf("remove signatory: %w", err)
	}
	found := false
	for _, u := range active {
		found = found || u == userID
	}
	if !found {
		return fmt.Errorf("remove signatory: %w", ErrNotFound)
	}
	// rule: MIG-035
	if len(active) == 1 {
		return fmt.Errorf("remove signatory: %w", signatory.ErrLastSignatory)
	}
	// rule: ARC-I-026
	// End-date, never delete: trust history is append-only.
	if _, err := tx.Exec(ctx, `UPDATE trust.signatory SET valid_to = CURRENT_DATE
WHERE account_id = $1::uuid AND user_id = $2::uuid AND valid_to IS NULL`, string(id), userID); err != nil {
		return fmt.Errorf("remove signatory: end-date: %w", err)
	}
	return nil
}

// RequireSignatory locks the account row and returns ErrNoSignatory unless it has an active
// signatory. Receive calls it in the posting tx, so the check and the receipt commit together.
func (s *AccountStore) RequireSignatory(ctx context.Context, tx pgx.Tx, id AccountID) error {
	// rule: ARC-U-026
	if err := lockAccount(ctx, tx, id); err != nil {
		return fmt.Errorf("require signatory: %w", err)
	}
	active, err := queryStrings(ctx, tx, activeSignatoriesSQL, string(id))
	if err != nil {
		return fmt.Errorf("require signatory: %w", err)
	}
	// rule: MIG-035
	if len(active) == 0 {
		return fmt.Errorf("account %s: %w", id, ErrNoSignatory)
	}
	return nil
}

const getAccountSQL = `SELECT id::text, type, institution, account_no_last4, title, coalesce(for_matter_id::text, ''), opened_on
FROM trust.trust_account WHERE id = $1::uuid`

// Account implements Accounts. SignatoryIDs lists active signatories only.
func (s *AccountStore) Account(ctx context.Context, id AccountID) (Account, error) {
	if id == "" {
		return Account{}, fmt.Errorf("account: %w", ErrNotFound)
	}
	var out Account
	err := s.db.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var aid, typ string
		if err := tx.QueryRow(ctx, getAccountSQL, string(id)).Scan(&aid, &typ, &out.Institution, &out.AccountLast4,
			&out.Title, &out.ForMatterID, &out.OpenedOn); err != nil {
			var pg *pgconn.PgError
			if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
				return ErrNotFound
			}
			return fmt.Errorf("select: %w", err)
		}
		out.ID = AccountID(aid)
		out.Type = AccountType(typ)
		sigs, err := queryStrings(ctx, tx, activeSignatoriesSQL, aid)
		if err != nil {
			return err
		}
		out.SignatoryIDs = sigs
		return nil
	})
	if err != nil {
		return Account{}, fmt.Errorf("account %s: %w", id, err)
	}
	return out, nil
}

// CheckDepositTarget refuses money received in trust for a client unless it goes to a trust
// account. Behind FlagBylaw9Verified; off, it allows everything.
func (s *AccountStore) CheckDepositTarget(fundsInTrust, toTrustAccount bool) error {
	if !s.flags.Enabled(FlagBylaw9Verified) {
		return nil
	}
	// rule: BIL-L-034 (verify)
	if fundsInTrust && !toTrustAccount {
		return fmt.Errorf("funds in trust to operating: %w", ErrInvalid)
	}
	return nil
}

// AvailableCents is a ledger's available balance: cleared minus protected, minimum and
// authorised-not-executed withdrawal holds.
func AvailableCents(clearedCents, protectedCents, minimumCents, heldCents int64) int64 {
	// rule: ARC-I-025
	return clearedCents - protectedCents - minimumCents - heldCents
}

// CheckOverdraw refuses a withdrawal larger than the ledger's available balance, so no client
// ledger goes negative. The By-Law 9 reading is behind FlagBylaw9Verified; ARC-I-025 always
// applies, so the check itself is never off.
func (s *AccountStore) CheckOverdraw(b Balance, heldCents, amountCents int64) error {
	if amountCents <= 0 {
		return fmt.Errorf("amount: %w", ErrInvalid)
	}
	// rule: ARC-I-025
	avail := AvailableCents(b.ClearedCents, b.ProtectedCents, b.MinimumCents, heldCents)
	if s.flags.Enabled(FlagBylaw9Verified) {
		// rule: BIL-L-036 (verify)
		if b.PostedCents-amountCents < 0 {
			return fmt.Errorf("ledger would go negative: %w", ErrInsufficientFunds)
		}
	}
	if amountCents > avail {
		return fmt.Errorf("available: %w", ErrInsufficientFunds)
	}
	return nil
}

func lockAccount(ctx context.Context, tx pgx.Tx, id AccountID) error {
	var got string
	if err := tx.QueryRow(ctx, lockAccountSQL, string(id)).Scan(&got); err != nil {
		var pg *pgconn.PgError
		if errors.Is(err, pgx.ErrNoRows) || (errors.As(err, &pg) && pg.Code == pgInvalidText) {
			return ErrNotFound
		}
		return fmt.Errorf("lock account: %w", err)
	}
	return nil
}

func queryStrings(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]string, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	return out, nil
}
