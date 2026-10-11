package trust

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

// memRepo is an in-memory ledgerRepo. ops records the call order so tests can check that the
// ledger is locked before it is read or posted to.
type memRepo struct {
	ledgers map[LedgerID]Ledger
	posted  []Txn
	holds   map[string]memHold // withdrawal id -> hold
	ops     []string
	n       int
	failTxn error
}

type memHold struct {
	ledger LedgerID
	cents  int64
}

func newMemRepo() *memRepo {
	return &memRepo{ledgers: map[LedgerID]Ledger{}, holds: map[string]memHold{}}
}

func (m *memRepo) next(p string) string { m.n++; return fmt.Sprintf("%s-%d", p, m.n) }

func (m *memRepo) insertLedger(_ context.Context, _ pgx.Tx, l Ledger) (LedgerID, error) {
	m.ops = append(m.ops, "insertLedger")
	for _, o := range m.ledgers {
		if o.AccountID == l.AccountID && o.MatterID == l.MatterID {
			return "", ErrDuplicateLedger
		}
	}
	l.ID = LedgerID(m.next("ledger"))
	m.ledgers[l.ID] = l
	return l.ID, nil
}

func (m *memRepo) getLedger(_ context.Context, _ pgx.Tx, id LedgerID) (Ledger, error) {
	m.ops = append(m.ops, "getLedger")
	l, ok := m.ledgers[id]
	if !ok {
		return Ledger{}, ErrNotFound
	}
	return l, nil
}

func (m *memRepo) lockLedger(ctx context.Context, tx pgx.Tx, id LedgerID) (Ledger, error) {
	l, err := m.getLedger(ctx, tx, id)
	m.ops[len(m.ops)-1] = "lockLedger"
	return l, err
}

func (m *memRepo) sums(_ context.Context, _ pgx.Tx, id LedgerID, today time.Time, except string) (ledgerSums, error) {
	m.ops = append(m.ops, "sums")
	var s ledgerSums
	for _, t := range m.posted {
		if t.LedgerID != id {
			continue
		}
		s.PostedCents += t.AmountCents
		if t.AmountCents < 0 || t.Kind != KindReceipt || (!t.ClearedOn.IsZero() && !t.ClearedOn.After(today)) {
			s.ClearedCents += t.AmountCents
		}
	}
	for w, h := range m.holds {
		if h.ledger == id && w != except {
			s.HeldCents += h.cents
		}
	}
	return s, nil
}

func (m *memRepo) insertTxn(_ context.Context, _ pgx.Tx, t Txn) (TxnID, error) {
	m.ops = append(m.ops, "insertTxn")
	if m.failTxn != nil {
		return "", m.failTxn
	}
	t.ID = TxnID(m.next("txn"))
	m.posted = append(m.posted, t)
	if t.Kind == KindDisbursement {
		delete(m.holds, t.WithdrawalID)
	}
	return t.ID, nil
}

func (m *memRepo) txns(_ context.Context, _ pgx.Tx, id LedgerID) ([]Txn, error) {
	var out []Txn
	for _, t := range m.posted {
		if t.LedgerID == id {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *memRepo) accountTotals(_ context.Context, _ pgx.Tx, id AccountID) (int64, int64, error) {
	var journal, ledgers int64
	for _, t := range m.posted {
		if t.AccountID == id {
			journal += t.AmountCents
		}
	}
	for _, t := range m.posted {
		if l, ok := m.ledgers[t.LedgerID]; ok && l.AccountID == id {
			ledgers += t.AmountCents
		}
	}
	return journal, ledgers, nil
}

// signers is a SignatoryGuard: accounts in the set have an active signatory.
type signers map[AccountID]bool

func (s signers) RequireSignatory(_ context.Context, _ pgx.Tx, id AccountID) error {
	if !s[id] {
		return fmt.Errorf("account %s: %w", id, ErrNoSignatory)
	}
	return nil
}

var (
	toronto = mustLoc("America/Toronto")
	today   = time.Date(2026, 3, 16, 0, 0, 0, 0, toronto)
)

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

type rig struct {
	s    *LedgerStore
	repo *memRepo
	gl   *ledgerfake.Fake
}

func newRig(t *testing.T) rig {
	t.Helper()
	repo := newMemRepo()
	gl := ledgerfake.New(clock.DateOf(today, toronto), books.AcctTrustBank, books.AcctClientTrustLedger)
	clk := clockfake.New(today.Add(10*time.Hour), toronto)
	s := &LedgerStore{db: dbfake.New(), repo: repo, signers: signers{"acct-a": true, "acct-b": true}, gl: gl, clk: clk}
	return rig{s: s, repo: repo, gl: gl}
}

func (r rig) open(t *testing.T, acct AccountID, matter string) LedgerID {
	t.Helper()
	id, err := r.s.OpenLedger(context.Background(), nil, Ledger{AccountID: acct, MatterID: matter, ClientID: "client-" + matter})
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	return id
}

func receipt(l LedgerID, cents int64) Receipt {
	return Receipt{LedgerID: l, TxnDate: today, Method: MethodEFT, Counterparty: "Client", AmountCents: cents,
		Purpose: "retainer", ClearedOn: today, PostedBy: "user-1"}
}

// glBalance is debits minus credits on account in the trust book.
func (r rig) glBalance(t *testing.T, account string) int64 {
	t.Helper()
	b, err := r.gl.Balance(context.Background(), account, clock.DateOf(today, toronto))
	if err != nil {
		t.Fatal(err)
	}
	return int64(b)
}

// rule: BIL-I-009
func TestBIL_I_009_ReceiptPostsLedgerAndTrustBook(t *testing.T) {
	tests := []struct {
		name     string
		receipts []struct {
			acct   AccountID
			matter string
			cents  int64
		}
		wantA int64 // client A ledger balance
	}{
		{"one receipt of 150000 to client A", []struct {
			acct   AccountID
			matter string
			cents  int64
		}{{"acct-a", "A", 150000}}, 150000},
		{"two accounts, three ledgers", []struct {
			acct   AccountID
			matter string
			cents  int64
		}{{"acct-a", "A", 150000}, {"acct-a", "B", 2500}, {"acct-b", "C", 99}, {"acct-a", "A", 1}}, 150001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			ctx := context.Background()
			ids := map[string]LedgerID{}
			for _, rc := range tt.receipts {
				if _, ok := ids[rc.matter]; !ok {
					ids[rc.matter] = r.open(t, rc.acct, rc.matter)
				}
				if _, err := r.s.Receive(ctx, nil, receipt(ids[rc.matter], rc.cents)); err != nil {
					t.Fatalf("receive: %v", err)
				}
			}
			b, err := r.s.Balance(ctx, ids["A"])
			if err != nil {
				t.Fatal(err)
			}
			if b.PostedCents != tt.wantA || b.ClearedCents != tt.wantA || b.AvailableCents != tt.wantA {
				t.Fatalf("balance A = %+v, want %d", b, tt.wantA)
			}
			// TR-1500 (debit-normal) equals the sum of TR-2500 (credit-normal) ledgers.
			var sum int64
			for _, id := range ids {
				bb, err := r.s.Balance(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				sum += bb.PostedCents
			}
			if bank, client := r.glBalance(t, books.AcctTrustBank), r.glBalance(t, books.AcctClientTrustLedger); bank != sum || client != -sum {
				t.Fatalf("TR-1500 = %d, TR-2500 = %d, ledgers = %d", bank, client, sum)
			}
			for _, acct := range []AccountID{"acct-a", "acct-b"} {
				if err := r.s.CheckJournal(ctx, nil, acct); err != nil {
					t.Fatalf("check journal %s: %v", acct, err)
				}
			}
			for _, txn := range r.repo.posted {
				e, ok := r.gl.Entry(mustEntry(t, r, txn.ID))
				if !ok || e.Book != books.BookTrust || len(e.Lines) != 2 {
					t.Fatalf("trust book entry for %s = %+v", txn.ID, e)
				}
				for _, l := range e.Lines {
					want := SubjectClientLedger
					if l.Account == books.AcctTrustBank {
						want = SubjectTrustAccount
					}
					if l.Subject.Kind != want {
						t.Fatalf("line %s subject = %+v, want kind %s", l.Account, l.Subject, want)
					}
				}
			}
		})
	}
}

// mustEntry finds the trust book entry posted for txn by replaying its source: Post is
// idempotent per source, so it returns the first id.
func mustEntry(t *testing.T, r rig, txn TxnID) ledger.EntryID {
	t.Helper()
	id, err := r.gl.Post(context.Background(), nil, ledger.Entry{Book: books.BookTrust, SourceKind: GLSourceKind,
		SourceID: string(txn), Date: clock.DateOf(today, toronto),
		Lines: []ledger.Line{{Account: books.AcctTrustBank, Side: ledger.Debit, Amount: 1}, {Account: books.AcctClientTrustLedger, Side: ledger.Credit, Amount: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestBIL_I_009_CheckJournalMismatch(t *testing.T) {
	r := newRig(t)
	a := r.open(t, "acct-a", "A")
	if _, err := r.s.Receive(context.Background(), nil, receipt(a, 500)); err != nil {
		t.Fatal(err)
	}
	// A row whose account disagrees with its ledger's account breaks the per-account split.
	r.repo.posted = append(r.repo.posted, Txn{ID: "bad", AccountID: "acct-b", LedgerID: a, Kind: KindReceipt, AmountCents: 7})
	tests := []struct {
		acct AccountID
		want error
	}{
		{"acct-a", ErrJournalMismatch},
		{"acct-b", ErrJournalMismatch},
	}
	for _, tt := range tests {
		if err := r.s.CheckJournal(context.Background(), nil, tt.acct); !errors.Is(err, tt.want) {
			t.Fatalf("check %s = %v, want %v", tt.acct, err, tt.want)
		}
	}
}

// rule: MIG-035
func TestMIG_035_ReceiveNeedsActiveSignatory(t *testing.T) {
	tests := []struct {
		name string
		acct AccountID
		want error
	}{
		{"active signatory", "acct-a", nil},
		{"no signatory", "acct-none", ErrNoSignatory},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			l := r.open(t, tt.acct, "A")
			_, err := r.s.Receive(context.Background(), nil, receipt(l, 1000))
			if !errors.Is(err, tt.want) {
				t.Fatalf("receive = %v, want %v", err, tt.want)
			}
			if tt.want != nil && (len(r.repo.posted) != 0 || r.glBalance(t, books.AcctTrustBank) != 0) {
				t.Fatal("refused receipt posted")
			}
		})
	}
}

func TestReceiveValidation(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*Receipt)
		want error
	}{
		{"ok", func(*Receipt) {}, nil},
		{"zero amount", func(r *Receipt) { r.AmountCents = 0 }, ErrInvalid},
		{"negative amount", func(r *Receipt) { r.AmountCents = -1 }, ErrInvalid},
		{"no purpose", func(r *Receipt) { r.Purpose = "" }, ErrInvalid},
		{"no counterparty", func(r *Receipt) { r.Counterparty = "" }, ErrInvalid},
		{"no poster", func(r *Receipt) { r.PostedBy = "" }, ErrInvalid},
		{"no date", func(r *Receipt) { r.TxnDate = time.Time{} }, ErrInvalid},
		{"bad method", func(r *Receipt) { r.Method = "barter" }, ErrInvalid},
		{"uncleared eft", func(r *Receipt) { r.ClearedOn = time.Time{} }, ErrInvalid},
		{"uncleared cheque", func(r *Receipt) { r.Method, r.ClearedOn = MethodCheque, time.Time{} }, nil},
		{"uncleared bank draft", func(r *Receipt) { r.Method, r.ClearedOn = MethodBankDraft, time.Time{} }, nil},
		{"unknown ledger", func(r *Receipt) { r.LedgerID = "nope" }, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			rc := receipt(r.open(t, "acct-a", "A"), 100)
			tt.mod(&rc)
			if _, err := r.s.Receive(context.Background(), nil, rc); !errors.Is(err, tt.want) {
				t.Fatalf("receive = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOpenLedger(t *testing.T) {
	tests := []struct {
		name string
		l    Ledger
		want error
	}{
		{"ok", Ledger{AccountID: "acct-a", MatterID: "M", ClientID: "C"}, nil},
		{"protected with purpose", Ledger{AccountID: "acct-a", MatterID: "M", ClientID: "C", ProtectedCents: 5, ProtectedPurpose: "undertaking"}, nil},
		{"protected without purpose", Ledger{AccountID: "acct-a", MatterID: "M", ClientID: "C", ProtectedCents: 5}, ErrInvalid},
		{"negative minimum", Ledger{AccountID: "acct-a", MatterID: "M", ClientID: "C", MinimumBalanceCents: -1}, ErrInvalid},
		{"no matter", Ledger{AccountID: "acct-a", ClientID: "C"}, ErrInvalid},
		{"no account", Ledger{MatterID: "M", ClientID: "C"}, ErrInvalid},
		{"duplicate", Ledger{AccountID: "acct-a", MatterID: "DUP", ClientID: "C"}, ErrDuplicateLedger},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.open(t, "acct-a", "DUP")
			if _, err := r.s.OpenLedger(context.Background(), nil, tt.l); !errors.Is(err, tt.want) {
				t.Fatalf("open = %v, want %v", err, tt.want)
			}
		})
	}
}

func disbursement(l LedgerID, cents int64) Disbursement {
	return Disbursement{LedgerID: l, WithdrawalID: "w-1", ApprovalID: "gate-1", TxnDate: today, Method: MethodCheque,
		Counterparty: "Vendor", AmountCents: cents, Purpose: "disbursement", PostedBy: "user-2"}
}

// rule: ARC-I-025
func TestARC_I_025_DisburseRechecksAvailable(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(r rig, l LedgerID)
		amount    int64
		want      error
		wantAfter int64 // ledger posted balance after
	}{
		{"within balance", func(rig, LedgerID) {}, 4000, nil, 6000},
		{"whole balance", func(rig, LedgerID) {}, 10000, nil, 0},
		{"ledger 10000, withdrawal 12000", func(rig, LedgerID) {}, 12000, ErrInsufficientFunds, 10000},
		{"protected funds", func(r rig, l LedgerID) {
			x := r.repo.ledgers[l]
			x.ProtectedCents = 3000
			r.repo.ledgers[l] = x
		}, 8000, ErrInsufficientFunds, 10000},
		{"minimum balance", func(r rig, l LedgerID) {
			x := r.repo.ledgers[l]
			x.MinimumBalanceCents = 1
			r.repo.ledgers[l] = x
		}, 10000, ErrInsufficientFunds, 10000},
		{"another withdrawal's hold", func(r rig, l LedgerID) {
			r.repo.holds["w-other"] = memHold{l, 2000}
		}, 9000, ErrInsufficientFunds, 10000},
		{"own hold is released, not counted", func(r rig, l LedgerID) {
			r.repo.holds["w-1"] = memHold{l, 10000}
		}, 10000, nil, 0},
		{"uncleared cheque", func(r rig, l LedgerID) {
			rc := receipt(l, 5000)
			rc.Method, rc.ClearedOn = MethodCheque, time.Time{}
			if _, err := r.s.Receive(context.Background(), nil, rc); err != nil {
				panic(err)
			}
		}, 12000, ErrInsufficientFunds, 15000},
		{"cheque clearing tomorrow", func(r rig, l LedgerID) {
			rc := receipt(l, 5000)
			rc.Method, rc.ClearedOn = MethodCheque, today.AddDate(0, 0, 1)
			if _, err := r.s.Receive(context.Background(), nil, rc); err != nil {
				panic(err)
			}
		}, 12000, ErrInsufficientFunds, 15000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			ctx := context.Background()
			l := r.open(t, "acct-a", "A")
			if _, err := r.s.Receive(ctx, nil, receipt(l, 10000)); err != nil {
				t.Fatal(err)
			}
			tt.setup(r, l)
			before := len(r.repo.posted)
			bankBefore := r.glBalance(t, books.AcctTrustBank)
			_, err := r.s.Disburse(ctx, nil, disbursement(l, tt.amount))
			if !errors.Is(err, tt.want) {
				t.Fatalf("disburse = %v, want %v", err, tt.want)
			}
			b, err2 := r.s.Balance(ctx, l)
			if err2 != nil {
				t.Fatal(err2)
			}
			if b.PostedCents != tt.wantAfter {
				t.Fatalf("posted = %d, want %d", b.PostedCents, tt.wantAfter)
			}
			if b.PostedCents < 0 {
				t.Fatal("ledger negative")
			}
			if tt.want != nil {
				if len(r.repo.posted) != before || r.glBalance(t, books.AcctTrustBank) != bankBefore {
					t.Fatal("refused disbursement posted")
				}
				return
			}
			last := r.repo.posted[len(r.repo.posted)-1]
			if last.Kind != KindDisbursement || last.AmountCents != -tt.amount || last.WithdrawalID != "w-1" || last.ApprovalID != "gate-1" {
				t.Fatalf("posted txn = %+v", last)
			}
			if bank, client := r.glBalance(t, books.AcctTrustBank), r.glBalance(t, books.AcctClientTrustLedger); bank != tt.wantAfter || client != -tt.wantAfter {
				t.Fatalf("TR-1500 = %d, TR-2500 = %d, want %d", bank, client, tt.wantAfter)
			}
			if err := r.s.CheckJournal(ctx, nil, "acct-a"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// rule: ARC-I-025
func TestARC_I_025_DisburseLocksBeforeReading(t *testing.T) {
	r := newRig(t)
	l := r.open(t, "acct-a", "A")
	if _, err := r.s.Receive(context.Background(), nil, receipt(l, 100)); err != nil {
		t.Fatal(err)
	}
	r.repo.ops = nil
	if _, err := r.s.Disburse(context.Background(), nil, disbursement(l, 100)); err != nil {
		t.Fatal(err)
	}
	want := []string{"lockLedger", "sums", "insertTxn"}
	if fmt.Sprint(r.repo.ops) != fmt.Sprint(want) {
		t.Fatalf("ops = %v, want %v", r.repo.ops, want)
	}
}

func TestDisburseValidation(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*Disbursement)
		want error
	}{
		{"ok", func(*Disbursement) {}, nil},
		{"cash payout", func(d *Disbursement) { d.Method = MethodCash }, ErrCashPayout},
		{"no withdrawal", func(d *Disbursement) { d.WithdrawalID = "" }, ErrInvalid},
		{"no approval (not via a gate)", func(d *Disbursement) { d.ApprovalID = "" }, ErrInvalid},
		{"zero amount", func(d *Disbursement) { d.AmountCents = 0 }, ErrInvalid},
		{"negative amount", func(d *Disbursement) { d.AmountCents = -5 }, ErrInvalid},
		{"no poster", func(d *Disbursement) { d.PostedBy = "" }, ErrInvalid},
		{"bad method", func(d *Disbursement) { d.Method = "barter" }, ErrInvalid},
		{"unknown ledger", func(d *Disbursement) { d.LedgerID = "nope" }, ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			l := r.open(t, "acct-a", "A")
			if _, err := r.s.Receive(context.Background(), nil, receipt(l, 100)); err != nil {
				t.Fatal(err)
			}
			d := disbursement(l, 50)
			tt.mod(&d)
			if _, err := r.s.Disburse(context.Background(), nil, d); !errors.Is(err, tt.want) {
				t.Fatalf("disburse = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPostFailuresSurface(t *testing.T) {
	tests := []struct {
		name string
		fail func(r rig)
	}{
		{"insert fails", func(r rig) { r.repo.failTxn = errors.New("boom") }},
		{"trust book fails", func(r rig) {
			r.s.gl = ledgerfake.New(clock.DateOf(today, toronto), "OP-1000")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			l := r.open(t, "acct-a", "A")
			tt.fail(r)
			if _, err := r.s.Receive(context.Background(), nil, receipt(l, 100)); err == nil {
				t.Fatal("receive succeeded, want error so the caller's tx rolls back")
			}
		})
	}
}

func TestTxnsListsPostingOrder(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	l := r.open(t, "acct-a", "A")
	other := r.open(t, "acct-a", "B")
	for _, c := range []int64{100, 200} {
		if _, err := r.s.Receive(ctx, nil, receipt(l, c)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.s.Receive(ctx, nil, receipt(other, 1)); err != nil {
		t.Fatal(err)
	}
	got, err := r.s.Txns(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].AmountCents != 100 || got[1].AmountCents != 200 {
		t.Fatalf("txns = %+v", got)
	}
	if _, err := r.s.Txns(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("txns unknown = %v", err)
	}
}
