package trust

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
)

type users map[string]signatory.User

func (u users) User(_ context.Context, id string) (signatory.User, error) {
	x, ok := u[id]
	if !ok {
		return signatory.User{}, signatory.ErrNotFound
	}
	return x, nil
}

type sealer struct{}

func (sealer) Seal(_ context.Context, p []byte) ([]byte, error) {
	return append([]byte("sealed:"), p...), nil
}

// stubRows yields vals as single-column string rows.
type stubRows struct {
	pgx.Rows
	vals []string
	i    int
}

func (r *stubRows) Next() bool                    { r.i++; return r.i <= len(r.vals) }
func (r *stubRows) Scan(dest ...any) error        { *dest[0].(*string) = r.vals[r.i-1]; return nil }
func (r *stubRows) Err() error                    { return nil }
func (r *stubRows) Close()                        {}
func (r *stubRows) CommandTag() pgconn.CommandTag { return pgconn.CommandTag{} }
func (r *stubRows) FieldDescriptions() []pgconn.FieldDescription {
	return []pgconn.FieldDescription{{Name: "user_id"}}
}

type stubRow struct{ err error }

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*dest[0].(*string) = "acct"
	return nil
}

// stubTx records every statement. The account lock returns lockErr; the signatory query
// returns signers.
type stubTx struct {
	pgx.Tx
	lockErr error
	signers []string
	sql     []string
}

func (t *stubTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.sql = append(t.sql, sql)
	return stubRow{err: t.lockErr}
}

func (t *stubTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	t.sql = append(t.sql, sql)
	return &stubRows{vals: t.signers}, nil
}

func (t *stubTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	t.sql = append(t.sql, sql)
	return pgconn.CommandTag{}, nil
}

func newStore() *AccountStore {
	return NewAccountStore(nil, users{
		"lic":  {ID: "lic", Licensee: true, Active: true},
		"left": {ID: "left", Licensee: true},
		"para": {ID: "para", Active: true},
	}, sealer{})
}

func TestMIG_035_NoReceiptWithoutActiveSignatory(t *testing.T) {
	tests := []struct {
		name    string
		signers []string
		lockErr error
		err     error
	}{
		{name: "no signatory", err: ErrNoSignatory},
		{name: "one signatory", signers: []string{"lic"}},
		{name: "unknown account", lockErr: pgx.ErrNoRows, err: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &stubTx{signers: tt.signers, lockErr: tt.lockErr}
			if err := newStore().RequireSignatory(context.Background(), tx, "acct"); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestMIG_035_LastSignatoryStays(t *testing.T) {
	tests := []struct {
		name    string
		signers []string
		user    string
		err     error
		updates int
	}{
		{name: "last one refused", signers: []string{"lic"}, user: "lic", err: signatory.ErrLastSignatory},
		{name: "one of two removed", signers: []string{"a", "lic"}, user: "lic", updates: 1},
		{name: "not a signatory", signers: []string{"a"}, user: "lic", err: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &stubTx{signers: tt.signers}
			err := newStore().RemoveSignatory(context.Background(), tx, "acct", tt.user)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			n := 0
			for _, s := range tx.sql {
				if strings.HasPrefix(s, "UPDATE trust.signatory SET valid_to") {
					n++
				}
			}
			if n != tt.updates {
				t.Fatalf("end-dates = %d, want %d", n, tt.updates)
			}
		})
	}
}

func TestARC_U_026_LockBeforeSignatoryCheck(t *testing.T) {
	tests := []struct {
		name string
		run  func(*AccountStore, pgx.Tx) error
	}{
		{name: "require", run: func(s *AccountStore, tx pgx.Tx) error { return s.RequireSignatory(context.Background(), tx, "acct") }},
		{name: "remove", run: func(s *AccountStore, tx pgx.Tx) error {
			return s.RemoveSignatory(context.Background(), tx, "acct", "lic")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &stubTx{signers: []string{"a", "lic"}}
			if err := tt.run(newStore(), tx); err != nil {
				t.Fatal(err)
			}
			if len(tx.sql) == 0 || !strings.HasSuffix(tx.sql[0], "FOR UPDATE") {
				t.Fatalf("first statement = %q, want account lock", tx.sql)
			}
		})
	}
}

func TestARC_I_026_NoTrustRowRewritten(t *testing.T) {
	src, err := os.ReadFile("account.go")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, forbidden string
	}{
		{name: "no delete", forbidden: "DELETE"},
		{name: "no truncate", forbidden: "TRUNCATE"},
		{name: "no transaction update", forbidden: "UPDATE trust.trust_transaction"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(string(src), tt.forbidden) {
				t.Fatalf("account.go contains %q", tt.forbidden)
			}
		})
	}
}

func TestBIL_L_034_FundsInTrustGoToTrust(t *testing.T) {
	tests := []struct {
		name               string
		on, inTrust, toTrA bool
		err                error
	}{
		{name: "flag off refuses deposit to operating", inTrust: true, err: ErrUnverified},
		{name: "flag off refuses deposit to trust", inTrust: true, toTrA: true, err: ErrUnverified},
		{name: "flag off refuses earned fee", err: ErrUnverified},
		{name: "to operating refused", on: true, inTrust: true, err: ErrInvalid},
		{name: "to trust allowed", on: true, inTrust: true, toTrA: true},
		{name: "earned fee to operating", on: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := newStore().CheckDepositTarget(legalflagstest.Context(t, FlagBylaw9Verified, tt.on), tt.inTrust, tt.toTrA); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestBIL_L_034_FlagOffDoesNotAllowOperatingDeposit(t *testing.T) {
	tests := []struct {
		name           string
		inTrust, toTrA bool
	}{
		{name: "funds in trust to operating", inTrust: true},
		{name: "funds in trust to trust", inTrust: true, toTrA: true},
		{name: "earned fee to operating"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := newStore().CheckDepositTarget(legalflagstest.Context(t, FlagBylaw9Verified, false), tt.inTrust, tt.toTrA)
			if err == nil || !errors.Is(err, ErrUnverified) {
				t.Fatalf("err = %v, want ErrUnverified", err)
			}
		})
	}
}

func TestBIL_L_036_LedgerNeverNegative(t *testing.T) {
	tests := []struct {
		name   string
		on     bool
		b      Balance
		amount int64
		err    error
	}{
		{name: "overdraw refused", on: true, b: Balance{PostedCents: 120000, ClearedCents: 120000}, amount: 150000, err: ErrInsufficientFunds},
		{name: "full balance allowed", on: true, b: Balance{PostedCents: 120000, ClearedCents: 120000}, amount: 120000},
		{name: "flag off still refuses", b: Balance{PostedCents: 120000, ClearedCents: 120000}, amount: 150000, err: ErrInsufficientFunds},
		{name: "zero amount", on: true, b: Balance{PostedCents: 1}, err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := newStore().CheckOverdraw(legalflagstest.Context(t, FlagBylaw9Verified, tt.on), tt.b, 0, tt.amount); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestARC_I_025_AvailableCountsHolds(t *testing.T) {
	tests := []struct {
		name   string
		b      Balance
		held   int64
		amount int64
		err    error
	}{
		{name: "first of two holds", b: Balance{PostedCents: 500000, ClearedCents: 500000}, amount: 300000},
		{name: "second hold refused", b: Balance{PostedCents: 500000, ClearedCents: 500000}, held: 300000, amount: 300000, err: ErrInsufficientFunds},
		{name: "protected and minimum held back", b: Balance{PostedCents: 500000, ClearedCents: 500000, ProtectedCents: 100000, MinimumCents: 50000}, amount: 350001, err: ErrInsufficientFunds},
		{name: "uncleared not available", b: Balance{PostedCents: 500000, ClearedCents: 100000}, amount: 100001, err: ErrInsufficientFunds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := newStore().CheckOverdraw(legalflagstest.Context(t, FlagBylaw9Verified, true), tt.b, tt.held, tt.amount); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
		})
	}
	if got := AvailableCents(500000, 100000, 50000, 25000); got != 325000 {
		t.Fatalf("Available = %d, want 325000", got)
	}
}

func TestOpenAccount(t *testing.T) {
	opened := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	valid := Account{Type: AccountMixed, Institution: "Bank", AccountLast4: "1234", Title: "Mixed trust", OpenedOn: opened}
	tests := []struct {
		name  string
		in    func(Account) Account
		no    string
		err   error
		calls int
	}{
		{name: "opens mixed", in: func(a Account) Account { return a }, no: "0001234", calls: 1},
		{name: "separate needs matter", in: func(a Account) Account { a.Type = AccountSeparateInterestBearing; return a }, no: "0001234", err: ErrInvalid},
		{name: "mixed refuses matter", in: func(a Account) Account { a.ForMatterID = "m1"; return a }, no: "0001234", err: ErrInvalid},
		{name: "bad last4", in: func(a Account) Account { a.AccountLast4 = "12a4"; return a }, no: "12a4", err: ErrInvalid},
		{name: "number mismatch", in: func(a Account) Account { return a }, no: "0009999", err: ErrInvalid},
		{name: "no title", in: func(a Account) Account { a.Title = ""; return a }, no: "0001234", err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &stubTx{}
			id, err := newStore().OpenAccountNumber(context.Background(), tx, tt.in(valid), tt.no)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if len(tx.sql) != tt.calls || (tt.err == nil && id != "acct") {
				t.Fatalf("id %q statements %d", id, len(tx.sql))
			}
		})
	}
	if _, err := newStore().OpenAccount(context.Background(), &stubTx{}, valid); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("OpenAccount err = %v, want ErrNeedsApproval", err)
	}
}

func TestAddApprovedSignatory(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name, user, approval string
		err                  error
		calls                int
	}{
		{name: "licensee added", user: "lic", approval: "ga1", calls: 1},
		{name: "left firm", user: "left", approval: "ga1", err: signatory.ErrNotEligible},
		{name: "non-licensee", user: "para", approval: "ga1", err: signatory.ErrNotEligible},
		{name: "no approval", user: "lic", err: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &stubTx{}
			err := newStore().AddApprovedSignatory(context.Background(), tx, "acct", tt.user, tt.approval, day)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if len(tx.sql) != tt.calls {
				t.Fatalf("statements = %d, want %d", len(tx.sql), tt.calls)
			}
		})
	}
	if err := newStore().AddSignatory(context.Background(), &stubTx{}, "acct", "lic"); !errors.Is(err, ErrNeedsApproval) {
		t.Fatalf("AddSignatory err = %v, want ErrNeedsApproval", err)
	}
}
