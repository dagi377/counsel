package trust

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

// nsfMem adds the NSF storage to memRepo: transactions come from the posted rows, exceptions
// are kept in a slice.
type nsfMem struct {
	*memRepo
	exceptions []trustException
	failExc    error
}

func (m *nsfMem) getTxn(_ context.Context, _ pgx.Tx, id TxnID) (Txn, error) {
	for _, t := range m.posted {
		if t.ID == id {
			return t, nil
		}
	}
	return Txn{}, ErrNotFound
}

func (m *nsfMem) reversalOf(_ context.Context, _ pgx.Tx, id TxnID) (TxnID, error) {
	for _, t := range m.posted {
		if t.ReversesID == id {
			return t.ID, nil
		}
	}
	return "", nil
}

func (m *nsfMem) openException(_ context.Context, _ pgx.Tx, e trustException) (string, error) {
	if m.failExc != nil {
		return "", m.failExc
	}
	m.exceptions = append(m.exceptions, e)
	return m.next("exception"), nil
}

type nsfRig struct {
	s    *LedgerStore
	repo *nsfMem
	gl   *ledgerfake.Fake
}

func newNSFRig(t *testing.T) nsfRig {
	t.Helper()
	repo := &nsfMem{memRepo: newMemRepo()}
	gl := ledgerfake.New(clock.DateOf(today, toronto), books.AcctTrustBank, books.AcctClientTrustLedger,
		books.AcctARFees, books.AcctOperatingBank)
	clk := clockfake.New(today.Add(10*time.Hour), toronto)
	s := &LedgerStore{db: dbfake.New(), repo: repo, signers: signers{"acct-a": true}, gl: gl, clk: clk}
	return nsfRig{s: s, repo: repo, gl: gl}
}

func (r nsfRig) balance(t *testing.T, account string) int64 {
	t.Helper()
	b, err := r.gl.Balance(context.Background(), account, clock.DateOf(today, toronto))
	if err != nil {
		t.Fatal(err)
	}
	return int64(b)
}

func on(t *testing.T) context.Context {
	return legalflagstest.Context(t, FlagBylaw9Verified, true)
}

// seed opens ledger A, posts the given receipts and pays out paidOut. It returns the ledger
// and the id of the first receipt, the one that comes back NSF.
func (r nsfRig) seed(t *testing.T, receipts []int64, paidOut int64) (LedgerID, TxnID) {
	t.Helper()
	ctx := context.Background()
	l, err := r.s.OpenLedger(ctx, nil, Ledger{AccountID: "acct-a", MatterID: "matter-A", ClientID: "client-A"})
	if err != nil {
		t.Fatal(err)
	}
	var first TxnID
	for i, c := range receipts {
		rc := receipt(l, c)
		rc.Method, rc.InstrumentNo = MethodCheque, "000123"
		id, err := r.s.Receive(ctx, nil, rc)
		if err != nil {
			t.Fatalf("receive: %v", err)
		}
		if i == 0 {
			first = id
		}
	}
	if paidOut > 0 {
		if _, err := r.s.Disburse(ctx, nil, disbursement(l, paidOut)); err != nil {
			t.Fatalf("disburse: %v", err)
		}
	}
	return l, first
}

// rule: BIL-C-065
func TestBIL_C_065_NSFReversal(t *testing.T) {
	tests := []struct {
		name          string
		receipts      []int64 // the first one comes back NSF
		paidOut       int64
		wantShortfall int64
		wantLedger    int64
	}{
		// BIL-X-026: shortfall 300000 replenished, ledger 0.
		{"receipt already paid out: replenish 300000", []int64{300000}, 300000, 300000, 0},
		{"partly paid out: replenish the 150000 shortfall", []int64{300000, 100000}, 250000, 150000, 0},
		// BIL-X-026: ledger with 500000 -> reversal only.
		{"ledger still covers it: reversal only", []int64{300000, 500000}, 0, 0, 500000},
		{"ledger exactly covers it: reversal only", []int64{300000, 300000}, 300000, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newNSFRig(t)
			l, nsf := r.seed(t, tt.receipts, tt.paidOut)
			got, err := r.s.ReverseNSF(on(t), nil, nsf, "approval-g6", "user-2")
			if err != nil {
				t.Fatalf("reverse nsf: %v", err)
			}
			if got.ShortfallCents != tt.wantShortfall || !got.ExceptionOpened || got.ReversalID == "" {
				t.Fatalf("result = %+v, want shortfall %d and an exception", got, tt.wantShortfall)
			}
			if (got.ReplenishmentID != "") != (tt.wantShortfall > 0) {
				t.Fatalf("replenishment id = %q with shortfall %d", got.ReplenishmentID, tt.wantShortfall)
			}
			b, err := r.s.Balance(context.Background(), l)
			if err != nil {
				t.Fatal(err)
			}
			if b.PostedCents != tt.wantLedger {
				t.Fatalf("ledger = %d, want %d", b.PostedCents, tt.wantLedger)
			}
			var rev, rep *Txn
			for i := range r.repo.posted {
				switch r.repo.posted[i].ID {
				case got.ReversalID:
					rev = &r.repo.posted[i]
				case got.ReplenishmentID:
					rep = &r.repo.posted[i]
				}
			}
			// rule: DM-055
			if rev == nil || rev.Kind != KindReversal || rev.ReversesID != nsf || rev.AmountCents != -tt.receipts[0] || rev.LedgerID != l {
				t.Fatalf("reversal = %+v, want -%d reversing %s", rev, tt.receipts[0], nsf)
			}
			if tt.wantShortfall > 0 {
				if rep == nil || rep.Kind != KindReceipt || rep.ReplenishesID != nsf || rep.AmountCents != tt.wantShortfall ||
					rep.ApprovalID != "approval-g6" || rep.Method != MethodInternal || rep.Counterparty != nsfFirmCounterparty {
					t.Fatalf("replenishment = %+v, want %d from firm funds replenishing %s", rep, tt.wantShortfall, nsf)
				}
			}
			// Operating leg: Dr OP-1100 receivable from client / Cr OP-1000, only for a shortfall.
			if ar, bank := r.balance(t, books.AcctARFees), r.balance(t, books.AcctOperatingBank); ar != tt.wantShortfall || bank != -tt.wantShortfall {
				t.Fatalf("OP-1100 = %d, OP-1000 = %d, want %d / -%d", ar, bank, tt.wantShortfall, tt.wantShortfall)
			}
			if tt.wantShortfall > 0 {
				// Post is idempotent per source, so replaying the source returns the first entry.
				id, err := r.gl.Post(context.Background(), nil, ledger.Entry{Book: books.BookOperating,
					SourceKind: GLSourceNSFReplenishment, SourceID: string(got.ReplenishmentID), Date: clock.DateOf(today, toronto),
					Lines: []ledger.Line{{Account: books.AcctARFees, Side: ledger.Debit, Amount: 1}, {Account: books.AcctOperatingBank, Side: ledger.Credit, Amount: 1}}})
				if err != nil {
					t.Fatal(err)
				}
				e, found := r.gl.Entry(id)
				if !found || e.Book != books.BookOperating || len(e.Lines) != 2 || e.Lines[0].Amount != ledger.Cents(tt.wantShortfall) {
					t.Fatalf("operating leg = %+v", e)
				}
				for _, ln := range e.Lines {
					if ln.Account == books.AcctARFees && (ln.Side != ledger.Debit || ln.Subject.Kind != matterkind.Subject || ln.Subject.ID != "matter-A") {
						t.Fatalf("OP-1100 line = %+v", ln)
					}
				}
			}
			// Every NSF reversal opens exactly one trust exception.
			want := trustException{Kind: ExceptionNSF, ReceiptID: nsf, LedgerID: l, MatterID: "matter-A",
				AmountCents: tt.receipts[0], ShortfallCents: tt.wantShortfall, OpenedBy: "user-2"}
			if len(r.repo.exceptions) != 1 || r.repo.exceptions[0] != want {
				t.Fatalf("exceptions = %+v, want [%+v]", r.repo.exceptions, want)
			}
		})
	}
}

// rule: BIL-I-009
func TestBIL_I_009_NSFKeepsTrustBookBalanced(t *testing.T) {
	tests := []struct {
		name     string
		receipts []int64
		paidOut  int64
	}{
		{"with replenishment", []int64{300000}, 300000},
		{"reversal only", []int64{300000, 500000}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newNSFRig(t)
			l, nsf := r.seed(t, tt.receipts, tt.paidOut)
			if _, err := r.s.ReverseNSF(on(t), nil, nsf, "approval-g6", "user-2"); err != nil {
				t.Fatal(err)
			}
			b, err := r.s.Balance(context.Background(), l)
			if err != nil {
				t.Fatal(err)
			}
			if bank, client := r.balance(t, books.AcctTrustBank), r.balance(t, books.AcctClientTrustLedger); bank != b.PostedCents || client != -b.PostedCents {
				t.Fatalf("TR-1500 = %d, TR-2500 = %d, ledger = %d", bank, client, b.PostedCents)
			}
			if err := r.s.CheckJournal(context.Background(), nil, "acct-a"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// rule: BIL-C-065
func TestBIL_C_065_NSFRefusals(t *testing.T) {
	tests := []struct {
		name     string
		flag     bool
		target   func(r nsfRig, l LedgerID, nsf TxnID) TxnID
		approval string
		by       string
		want     error
	}{
		{"flag off fails closed", false, func(_ nsfRig, _ LedgerID, nsf TxnID) TxnID { return nsf }, "approval-g6", "user-2", ErrUnverified},
		{"shortfall without approval", true, func(_ nsfRig, _ LedgerID, nsf TxnID) TxnID { return nsf }, "", "user-2", ErrInvalid},
		{"no poster", true, func(_ nsfRig, _ LedgerID, nsf TxnID) TxnID { return nsf }, "approval-g6", "", ErrInvalid},
		{"unknown transaction", true, func(nsfRig, LedgerID, TxnID) TxnID { return "txn-missing" }, "approval-g6", "user-2", ErrNotFound},
		{"not a receipt", true, func(r nsfRig, _ LedgerID, _ TxnID) TxnID {
			for _, t := range r.repo.posted {
				if t.Kind == KindDisbursement {
					return t.ID
				}
			}
			return ""
		}, "approval-g6", "user-2", ErrNotReceipt},
		{"cash cannot bounce", true, func(r nsfRig, l LedgerID, _ TxnID) TxnID {
			rc := receipt(l, 100)
			rc.Method = MethodCash
			id, _ := r.s.Receive(context.Background(), nil, rc)
			return id
		}, "approval-g6", "user-2", ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newNSFRig(t)
			l, nsf := r.seed(t, []int64{300000}, 300000)
			target := tt.target(r, l, nsf)
			before := len(r.repo.posted)
			ctx := legalflagstest.Context(t, FlagBylaw9Verified, tt.flag)
			_, err := r.s.ReverseNSF(ctx, nil, target, tt.approval, tt.by)
			if !errors.Is(err, tt.want) {
				t.Fatalf("reverse nsf = %v, want %v", err, tt.want)
			}
			if len(r.repo.posted) != before || len(r.repo.exceptions) != 0 || r.balance(t, books.AcctARFees) != 0 {
				t.Fatal("refused reversal posted or opened an exception")
			}
		})
	}
}

// rule: DM-055
func TestDM_055_NSFReversesOnce(t *testing.T) {
	tests := []struct {
		name string
		want error
	}{
		{"second reversal of the same receipt", ErrAlreadyReversed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newNSFRig(t)
			_, nsf := r.seed(t, []int64{300000, 500000}, 0)
			if _, err := r.s.ReverseNSF(on(t), nil, nsf, "", "user-2"); err != nil {
				t.Fatal(err)
			}
			before := len(r.repo.posted)
			if _, err := r.s.ReverseNSF(on(t), nil, nsf, "", "user-2"); !errors.Is(err, tt.want) {
				t.Fatalf("second reversal = %v, want %v", err, tt.want)
			}
			if len(r.repo.posted) != before || len(r.repo.exceptions) != 1 {
				t.Fatal("second reversal posted")
			}
		})
	}
}

func TestNSFFailuresSurface(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		set  func(r nsfRig)
	}{
		{"posting fails", func(r nsfRig) { r.repo.failTxn = boom }},
		{"exception insert fails", func(r nsfRig) { r.repo.failExc = boom }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newNSFRig(t)
			_, nsf := r.seed(t, []int64{300000}, 300000)
			tt.set(r)
			if _, err := r.s.ReverseNSF(on(t), nil, nsf, "approval-g6", "user-2"); !errors.Is(err, boom) {
				t.Fatalf("reverse nsf = %v, want boom", err)
			}
		})
	}
}

func TestNSFNeedsNSFStorage(t *testing.T) {
	r := newRig(t)
	if _, err := r.s.ReverseNSF(legalflagstest.Context(t, FlagBylaw9Verified, true), nil, "txn-1", "a", "u"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("reverse nsf without nsf storage = %v, want ErrInvalid", err)
	}
}

// The insert writes both links, so a reversal and a replenishment keep them in the database.
func TestInsertTxnWritesLinks(t *testing.T) {
	tests := []struct {
		column, param string
	}{
		{"reverses_id", "NULLIF($15, '')::uuid"},
		{"replenishes_id", "NULLIF($16, '')::uuid"},
	}
	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			if !strings.Contains(insertTxnSQL, tt.column) || !strings.Contains(insertTxnSQL, tt.param) {
				t.Fatalf("insertTxnSQL does not write %s as %s", tt.column, tt.param)
			}
		})
	}
}
