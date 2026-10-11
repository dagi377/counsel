package withdraw

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
)

// svcRunner runs fn on tx.
type svcRunner struct{ tx pgx.Tx }

func (r svcRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, r.tx)
}

// stubLocker returns a fixed balance and hold, and records the ledger it locked.
type stubLocker struct {
	bal    trust.Balance
	held   int64
	locked trust.LedgerID
}

func (s *stubLocker) LockLedger(_ context.Context, _ pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error) {
	s.locked = id
	return s.bal, s.held, nil
}

const (
	insertFragment = "INSERT INTO trust.withdrawal_request"
	ledgerFragment = "FROM trust.client_ledger WHERE account_id"
)

func serviceTx(ledger bool) *fakeTx {
	rows := map[string][]fakeRow{insertFragment: {{vals: []any{"w-new"}}}}
	if ledger {
		rows[ledgerFragment] = []fakeRow{{vals: []any{"led-1"}}}
	}
	return &fakeTx{rows: rows, order: []string{insertFragment, ledgerFragment}}
}

func svcReq(amount int64) Request {
	return Request{TrustAccountID: "acct-1", MatterID: "M1", AmountCents: amount, Payee: "Vendor Inc",
		Method: MethodCheque, Reason: "disbursement", PreparedBy: "prep"}
}

// rule: BIL-C-053 (verify)
// PGService.Request checks the available balance under the ledger lock: an overdraw or a
// shortfall covered only by uncleared receipts is refused and nothing is inserted.
func TestBIL_C_053_PGServiceRequest(t *testing.T) {
	bal := trust.Balance{PostedCents: 12000, ClearedCents: 10000, ProtectedCents: 1000, MinimumCents: 500}
	tests := []struct {
		name    string
		req     Request
		ledger  bool
		held    int64
		wantErr error
	}{
		{"within available", svcReq(7000), true, 1000, nil},
		{"exactly available", svcReq(7500), true, 1000, nil},
		{"over available, covered by uncleared", svcReq(9000), true, 1000, ErrUnclearedFunds},
		{"over available", svcReq(20000), true, 1000, ErrTrustOverdraw},
		{"bearer payee", func() Request { r := svcReq(100); r.Payee = "Cash"; return r }(), true, 0, ErrBearerPayee},
		{"eft without form 9a", func() Request { r := svcReq(100); r.Method = MethodEFT; return r }(), true, 0, ErrNoEFTAuthorization},
		{"fees purpose needs its bill", func() Request { r := svcReq(100); r.Reason = "fees"; return r }(), true, 0, ErrInvalid},
		{"no ledger for matter", svcReq(100), false, 0, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := serviceTx(tt.ledger)
			lk := &stubLocker{bal: bal, held: tt.held}
			got, err := PGService{DB: svcRunner{tx: tx}, Ledgers: lk}.Request(verifiedCtx(t), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Request err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.ID != "w-new" || got.Status != StatusRequested || got.AuthorizedBy != "" {
				t.Fatalf("Request = %+v", got)
			}
			if lk.locked != "led-1" {
				t.Fatalf("locked ledger %q, want led-1", lk.locked)
			}
		})
	}
}

// rule: BIL-C-053 (verify)
// While By-Law 9 is unverified the request is refused before any database work.
func TestBIL_C_053_PGServiceUnverified(t *testing.T) {
	lk := &stubLocker{}
	_, err := PGService{DB: svcRunner{tx: serviceTx(true)}, Ledgers: lk}.Request(context.Background(), svcReq(100))
	if !errors.Is(err, ErrUnverified) {
		t.Fatalf("err = %v, want ErrUnverified", err)
	}
	if lk.locked != "" {
		t.Fatal("ledger locked while unverified")
	}
}

// rule: ARC-I-025
// Available is the one available-balance formula, unclamped: holds above the cleared balance
// give a negative figure.
func TestARC_I_025_PGServiceAvailable(t *testing.T) {
	tests := []struct {
		name string
		bal  trust.Balance
		held int64
		want int64
	}{
		{"positive", trust.Balance{ClearedCents: 10000, ProtectedCents: 1000, MinimumCents: 500}, 1500, 7000},
		{"negative is not clamped", trust.Balance{ClearedCents: 1000, ProtectedCents: 2000}, 500, -1500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := PGService{DB: svcRunner{tx: serviceTx(true)}, Ledgers: &stubLocker{bal: tt.bal, held: tt.held}}
			got, err := s.Available(context.Background(), "acct-1", "M1")
			if err != nil || got != tt.want {
				t.Fatalf("Available = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

// rule: ARC-I-027
// The service never authorizes or executes: only the gate effect and the execution route do.
func TestARC_I_027_PGServiceNeverAuthorizes(t *testing.T) {
	tx := serviceTx(true)
	s := PGService{DB: svcRunner{tx: tx}, Ledgers: &stubLocker{}}
	tests := []struct {
		name string
		call func() error
	}{
		{"authorize", func() error { _, err := s.Authorize(verifiedCtx(t), "w1", "lawyer", time.Now()); return err }},
		{"execute", func() error { _, err := s.RecordExecution(verifiedCtx(t), "w1", "clerk", time.Now()); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrEffectOnly) || !errors.Is(err, ErrNotAuthorized) {
				t.Fatalf("err = %v, want ErrEffectOnly and ErrNotAuthorized", err)
			}
		})
	}
	if len(tx.execs) != 0 {
		t.Fatal("unexpected writes")
	}
}

// insertArgs returns the arguments of the withdrawal INSERT tx ran, or nil.
func insertArgs(tx *fakeTx) []any {
	for _, q := range tx.queries {
		if strings.Contains(q.sql, insertFragment) {
			return q.args
		}
	}
	return nil
}

// rule: BIL-L-035 (verify)
// rule: ARC-I-027
// PGService stores a fee withdrawal with its bill and the person who prepared it; the trust
// effect re-checks the stored bill at authorization. A fee withdrawal with no bill, or a
// request with no preparer, is refused and nothing is inserted.
func TestPGServiceFeeBillStoredAndRechecked(t *testing.T) {
	delivered := execDay.Add(-24 * time.Hour)
	bills := feeBills{
		"bill-delivered": {MatterID: "M1", DeliveredAt: delivered, BalanceDueCents: 4000},
		"bill-draft":     {MatterID: "M1", BalanceDueCents: 4000},
	}
	fee := func(bill, preparer string) Request {
		r := svcReq(4000)
		r.Reason, r.BillID, r.Payee, r.PreparedBy = "fees", bill, "Firm General", preparer
		return r
	}
	tests := []struct {
		name       string
		req        Request
		requestErr error
		applyErr   error
	}{
		{name: "fees with a delivered bill passes", req: fee("bill-delivered", "lawyer2")},
		{name: "fees with an undelivered bill refused at authorization", req: fee("bill-draft", "lawyer2"), applyErr: ErrBillNotDelivered},
		{name: "fees without a bill refused", req: fee("", "lawyer2"), requestErr: ErrBillMatter},
		{name: "no preparer refused", req: fee("bill-delivered", ""), requestErr: ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := serviceTx(true)
			lk := &stubLocker{bal: trust.Balance{PostedCents: 10000, ClearedCents: 10000}}
			_, err := PGService{DB: svcRunner{tx: tx}, Ledgers: lk}.Request(verifiedCtx(t), tt.req)
			if !errors.Is(err, tt.requestErr) {
				t.Fatalf("Request err = %v, want %v", err, tt.requestErr)
			}
			args := insertArgs(tx)
			if tt.requestErr != nil {
				if args != nil {
					t.Fatalf("refused request inserted %v", args)
				}
				return
			}
			if len(args) != 8 || args[1] != "fees_to_operating" || args[6] != tt.req.PreparedBy || args[7] != tt.req.BillID {
				t.Fatalf("insert args = %v, want fees_to_operating by %s for %s", args, tt.req.PreparedBy, tt.req.BillID)
			}
			// The effect authorizes the stored row: its purpose, bill and preparer.
			r := newEffectRig(t, 10000, 4000, false)
			h := r.store.w["w1"]
			h.Request.Reason, h.Request.BillID, h.Request.PreparedBy = args[1].(string), args[7].(string), args[6].(string)
			r.store.w["w1"] = h
			r.e.Bills = bills
			if err := r.e.Apply(context.Background(), nil, r.approved(user("lawyer1"))); !errors.Is(err, tt.applyErr) {
				t.Fatalf("Apply err = %v, want %v", err, tt.applyErr)
			}
		})
	}
}
