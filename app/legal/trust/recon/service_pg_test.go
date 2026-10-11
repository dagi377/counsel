package recon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// scriptRow scans fixed values (string, int64, bool) into dest, or returns err.
type scriptRow struct {
	vals []any
	err  error
}

func (r scriptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = r.vals[i].(string)
		case *int64:
			*p = r.vals[i].(int64)
		case *bool:
			*p = r.vals[i].(bool)
		default:
			return errors.New("scriptRow: unsupported dest")
		}
	}
	return nil
}

type scriptRows struct {
	pgx.Rows
	rows []scriptRow
	i    int
}

func (r *scriptRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *scriptRows) Scan(dest ...any) error { return r.rows[r.i-1].Scan(dest...) }
func (r *scriptRows) Err() error             { return nil }
func (r *scriptRows) Close()                 {}

// prepareTx scripts one Prepare transaction: the month lock, the statement closing balance,
// the book, the client ledgers and the upsert, whose arguments it records.
type prepareTx struct {
	pgx.Tx
	locked  bool
	closing *int64
	book    int64
	ledgers []int64
	upsert  scriptRow
	args    []any
}

func (t *prepareTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	switch {
	case strings.Contains(sql, "SELECT EXISTS"):
		return scriptRow{vals: []any{t.locked}}
	case strings.Contains(sql, "trust.bank_import"):
		if t.closing == nil {
			return scriptRow{err: pgx.ErrNoRows}
		}
		return scriptRow{vals: []any{*t.closing}}
	case strings.Contains(sql, "coalesce(sum(amount_cents), 0)"):
		return scriptRow{vals: []any{t.book}}
	case strings.Contains(sql, "INSERT INTO trust.reconciliation"):
		t.args = args
		return t.upsert
	}
	return scriptRow{err: errors.New("unexpected query")}
}

func (t *prepareTx) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	if !strings.Contains(sql, "GROUP BY ledger_id") {
		return nil, errors.New("unexpected query")
	}
	rows := &scriptRows{}
	for i, c := range t.ledgers {
		rows.rows = append(rows.rows, scriptRow{vals: []any{string(rune('a' + i)), c}})
	}
	return rows, nil
}

type prepRunner struct{ tx pgx.Tx }

func (r prepRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, r.tx)
}

const testAccount = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"

// rule: BIL-I-009
// PGService.Prepare takes the bank balance from the imported statement and the book and
// client ledgers from the trust journal. An unbalanced month is stored as draft, never
// corrected; a locked month, a missing statement or an unverified flag fail closed.
func TestBIL_I_009_PGServicePrepare(t *testing.T) {
	closing := func(c int64) *int64 { return &c }
	tests := []struct {
		name       string
		tx         prepareTx
		items      []ReconcilingItem
		wantErr    error
		wantStatus string
		wantResult ThreeWay
	}{
		{"balanced", prepareTx{closing: closing(5000), book: 5000, ledgers: []int64{3000, 2000},
			upsert: scriptRow{vals: []any{"rec-1"}}}, nil, nil, "ready",
			ThreeWay{BankCents: 5000, AdjustedBankCents: 5000, BookCents: 5000, ClientLedgerCents: 5000}},
		{"balanced by a reconciling item", prepareTx{closing: closing(6000), book: 5000, ledgers: []int64{5000},
			upsert: scriptRow{vals: []any{"rec-1"}}},
			[]ReconcilingItem{{Kind: OutstandingCheque, Reference: "chq 12", AmountCents: -1000}}, nil, "ready",
			ThreeWay{BankCents: 6000, AdjustedBankCents: 5000, BookCents: 5000, ClientLedgerCents: 5000}},
		{"unbalanced is stored as draft", prepareTx{closing: closing(4000), book: 5000, ledgers: []int64{-200, 5200},
			upsert: scriptRow{vals: []any{"rec-1"}}}, nil, nil, "draft",
			ThreeWay{BankCents: 4000, AdjustedBankCents: 4000, BookCents: 5000, ClientLedgerCents: 5000}},
		{"locked month", prepareTx{locked: true, closing: closing(1)}, nil, ErrPeriodLocked, "", ThreeWay{}},
		{"signed between check and write", prepareTx{closing: closing(1), book: 1, ledgers: []int64{1},
			upsert: scriptRow{err: pgx.ErrNoRows}}, nil, ErrPeriodLocked, "", ThreeWay{}},
		{"no statement", prepareTx{}, nil, ErrNotFound, "", ThreeWay{}},
		{"invalid item", prepareTx{closing: closing(1), book: 1, ledgers: []int64{1}},
			[]ReconcilingItem{{Kind: "guess", Reference: "x", AmountCents: 1}}, ErrInvalidItem, "", ThreeWay{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := tt.tx
			p := Period{Year: 2026, Month: 3}
			got, err := PGService{DB: prepRunner{tx: &tx}}.Prepare(verifiedCtx(t), testAccount, p, tt.items, "prep")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Prepare err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			tt.wantResult.Items = tt.items
			if got.ID != "rec-1" || got.Status != StatusPrepared || got.PreparedBy != "prep" ||
				got.Result.BankCents != tt.wantResult.BankCents || got.Result.AdjustedBankCents != tt.wantResult.AdjustedBankCents ||
				got.Result.BookCents != tt.wantResult.BookCents || got.Result.ClientLedgerCents != tt.wantResult.ClientLedgerCents {
				t.Fatalf("Prepare = %+v, want result %+v", got, tt.wantResult)
			}
			if st := tx.args[len(tx.args)-1]; st != tt.wantStatus {
				t.Fatalf("stored status %v, want %s", st, tt.wantStatus)
			}
		})
	}
}

// rule: BIL-I-009
// While By-Law 9 is unverified nothing is prepared.
func TestBIL_I_009_PGServicePrepareUnverified(t *testing.T) {
	tx := &prepareTx{}
	_, err := PGService{DB: prepRunner{tx: tx}}.Prepare(context.Background(), testAccount, Period{Year: 2026, Month: 3}, nil, "prep")
	if !errors.Is(err, ErrUnverified) {
		t.Fatalf("err = %v, want ErrUnverified", err)
	}
	if tx.args != nil {
		t.Fatal("reconciliation written while unverified")
	}
}

// rule: BIL-I-025
// The service never signs off: an agent is refused as an agent, anyone else is sent to the gate.
func TestBIL_I_025_PGServiceNeverApproves(t *testing.T) {
	tests := []struct {
		name string
		by   Approver
		want error
	}{
		{"agent", Approver{UserID: "a", IsAgent: true}, ErrAgentApproval},
		{"user", Approver{UserID: "lawyer"}, ErrEffectOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := (PGService{}).Approve(context.Background(), "rec-1", tt.by); !errors.Is(err, tt.want) {
				t.Fatalf("Approve = %v, want %v", err, tt.want)
			}
		})
	}
	if _, err := (PGService{}).ImportStatement(context.Background(), Statement{}); !errors.Is(err, ErrNoParser) {
		t.Fatalf("ImportStatement = %v, want ErrNoParser", err)
	}
}
