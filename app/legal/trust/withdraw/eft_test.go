package withdraw

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/ledger"
)

// eftRecords is an in-memory Form 9A store.
type eftRecords map[string]EFTAuthorization

func (r eftRecords) EFTAuthorization(_ context.Context, _ pgx.Tx, id string) (EFTAuthorization, error) {
	a, ok := r[id]
	if !ok {
		return EFTAuthorization{}, ErrNotFound
	}
	return a, nil
}

func TestCheckEFT(t *testing.T) {
	at := execDay.Add(10 * time.Hour)
	signed := EFTAuthorization{ID: "9a-1", WithdrawalID: "w1", AmountCents: 4000, SignedBy: "lawyer1", SignedAt: at.Add(-time.Hour)}
	with := func(f func(*EFTAuthorization)) eftRecords {
		a := signed
		f(&a)
		return eftRecords{a.ID: a}
	}
	held := func(m Method, rec string) Held {
		return Held{Withdrawal: Withdrawal{ID: "w1", AuthorizedBy: "lawyer1",
			Request: Request{AmountCents: 4000, Method: m, EFTAuthorizationID: rec}}}
	}
	tests := []struct {
		name string
		recs EFTRecords
		h    Held
		want error
	}{
		{"cheque needs no record", nil, held(MethodCheque, ""), nil},
		{"signed record", eftRecords{"9a-1": signed}, held(MethodEFT, "9a-1"), nil},
		{"no record id", eftRecords{"9a-1": signed}, held(MethodEFT, ""), ErrNoEFTAuthorization},
		{"no record store", nil, held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
		{"record not found", eftRecords{}, held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
		{"record for another withdrawal", with(func(a *EFTAuthorization) { a.WithdrawalID = "w2" }), held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
		{"record amount differs", with(func(a *EFTAuthorization) { a.AmountCents = 4001 }), held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
		{"unsigned record", with(func(a *EFTAuthorization) { a.SignedBy = "" }), held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
		{"signed by someone else", with(func(a *EFTAuthorization) { a.SignedBy = "prep" }), held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
		{"signed after execution", with(func(a *EFTAuthorization) { a.SignedAt = at.Add(time.Minute) }), held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
		{"no signing time", with(func(a *EFTAuthorization) { a.SignedAt = time.Time{} }), held(MethodEFT, "9a-1"), ErrNoEFTAuthorization},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckEFT(context.Background(), nil, tt.recs, tt.h, at); !errors.Is(err, tt.want) {
				t.Fatalf("CheckEFT = %v, want %v", err, tt.want)
			}
		})
	}
}

// The effect refuses to post an EFT without its Form 9A record and posts it once one exists.
func TestEFTExecutionNeedsAuthorizationRecord(t *testing.T) {
	tests := []struct {
		name  string
		recs  EFTRecords
		rec   string
		want  error
		after int64
	}{
		{"no record store", nil, "9a-1", ErrNoEFTAuthorization, 10000},
		{"record missing", eftRecords{}, "9a-1", ErrNoEFTAuthorization, 10000},
		{"signed Form 9A", eftRecords{"9a-1": {ID: "9a-1", WithdrawalID: "w1", AmountCents: 4000, SignedBy: "lawyer1", SignedAt: execDay}}, "9a-1", nil, 6000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, false)
			h := r.store.w["w1"]
			h.Request.Method, h.Request.EFTAuthorizationID = MethodEFT, tt.rec
			r.store.w["w1"] = h
			r.e.EFT = tt.recs
			ctx := context.Background()
			if err := r.e.Apply(ctx, nil, approved(user("lawyer1"))); err != nil {
				t.Fatalf("apply: %v", err)
			}
			_, err := r.e.Execute(ctx, nil, "w1", user("bookkeeper"), execDay)
			if !errors.Is(err, tt.want) {
				t.Fatalf("execute = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				if !errors.Is(err, gate.ErrEffectFailed) {
					t.Fatalf("execute = %v, want effect failed", err)
				}
				assertCode(t, err, CodeNoEFTAuthorization)
				if r.store.w["w1"].Status != StatusAuthorized {
					t.Fatal("refused EFT marked executed")
				}
			}
			if got := posted(t, r); got != tt.after {
				t.Fatalf("ledger = %d, want %d", got, tt.after)
			}
		})
	}
}

// memTransfers is an in-memory TransferStore. ops records the call order.
type memTransfers struct {
	ledgers map[trust.LedgerID]trust.Ledger
	avail   map[trust.LedgerID]int64
	status  map[string]string
	legs    []trust.Txn
	ops     []string
}

func (m *memTransfers) LockLedgers(_ context.Context, _ pgx.Tx, a, b trust.LedgerID) ([]trust.Ledger, error) {
	if b < a {
		a, b = b, a
	}
	m.ops = append(m.ops, "lock "+string(a), "lock "+string(b))
	var out []trust.Ledger
	for _, id := range []trust.LedgerID{a, b} {
		if l, ok := m.ledgers[id]; ok {
			out = append(out, l)
		}
	}
	return out, nil
}

func (m *memTransfers) Available(_ context.Context, _ pgx.Tx, id trust.LedgerID, _ string) (int64, error) {
	m.ops = append(m.ops, "available")
	return m.avail[id], nil
}

func (m *memTransfers) MarkPosted(_ context.Context, _ pgx.Tx, id string) error {
	m.ops = append(m.ops, "posted")
	if m.status[id] != "authorized" {
		return ErrNotAuthorized
	}
	m.status[id] = "posted"
	return nil
}

func (m *memTransfers) InsertLeg(_ context.Context, _ pgx.Tx, _ string, t trust.Txn) (trust.TxnID, error) {
	m.ops = append(m.ops, "insert")
	m.legs = append(m.legs, t)
	return trust.TxnID(string(t.Kind) + "-" + string(t.LedgerID)), nil
}

// glLog records posted trust book entries.
type glLog struct {
	ledger.Ledger
	entries []ledger.Entry
}

func (g *glLog) Post(_ context.Context, _ pgx.Tx, e ledger.Entry) (ledger.EntryID, error) {
	g.entries = append(g.entries, e)
	return "je-1", nil
}

// A client-to-client transfer posts two balanced trust lines and one balanced trust book entry.
func TestTransferPostsTwoBalancedLines(t *testing.T) {
	ledgers := map[trust.LedgerID]trust.Ledger{
		"led-a": {ID: "led-a", AccountID: "acct-1", MatterID: "M1"},
		"led-b": {ID: "led-b", AccountID: "acct-1", MatterID: "M2"},
		"led-c": {ID: "led-c", AccountID: "acct-2", MatterID: "M3"},
	}
	base := Transfer{ID: "x1", FromLedger: "led-b", ToLedger: "led-a", AmountCents: 2500, Reason: "same client, new matter",
		ApprovalID: "gate-t", TxnDate: execDay, PostedBy: "lawyer1"}
	tests := []struct {
		name     string
		flagOff  bool
		mod      func(*Transfer)
		avail    int64
		status   string
		want     error
		wantLegs int
	}{
		{"posts two legs", false, func(*Transfer) {}, 2500, "authorized", nil, 2},
		{"flag off fails closed", true, func(*Transfer) {}, 2500, "authorized", ErrUnverified, 0},
		{"over available", false, func(*Transfer) {}, 2499, "authorized", ErrTrustOverdraw, 0},
		{"not authorized", false, func(*Transfer) {}, 2500, "pending_authorization", ErrNotAuthorized, 0},
		{"same ledger", false, func(x *Transfer) { x.ToLedger = x.FromLedger }, 2500, "authorized", ErrInvalid, 0},
		{"zero amount", false, func(x *Transfer) { x.AmountCents = 0 }, 2500, "authorized", ErrInvalid, 0},
		{"no approval", false, func(x *Transfer) { x.ApprovalID = "" }, 2500, "authorized", ErrInvalid, 0},
		{"across trust accounts", false, func(x *Transfer) { x.ToLedger = "led-c" }, 2500, "authorized", ErrCrossAccount, 0},
		{"unknown ledger", false, func(x *Transfer) { x.ToLedger = "led-z" }, 2500, "authorized", trust.ErrNotFound, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := base
			tt.mod(&x)
			st := &memTransfers{ledgers: ledgers, avail: map[trust.LedgerID]int64{x.FromLedger: tt.avail}, status: map[string]string{"x1": tt.status}}
			gl := &glLog{}
			ctx := context.Background()
			if !tt.flagOff {
				ctx = verifiedCtx(t)
			}
			ids, err := (&Transfers{Store: st, GL: gl, Loc: time.UTC}).Post(ctx, nil, x)
			if !errors.Is(err, tt.want) {
				t.Fatalf("post = %v, want %v", err, tt.want)
			}
			if len(st.legs) != tt.wantLegs {
				t.Fatalf("legs = %d, want %d", len(st.legs), tt.wantLegs)
			}
			if tt.want != nil {
				if len(gl.entries) != 0 || st.status["x1"] == "posted" {
					t.Fatal("refused transfer posted")
				}
				return
			}
			if ids[0] == "" || ids[1] == "" {
				t.Fatalf("ids = %v", ids)
			}
			out, in := st.legs[0], st.legs[1]
			if out.Kind != trust.KindTransferOut || out.LedgerID != "led-b" || out.AmountCents != -2500 ||
				in.Kind != trust.KindTransferIn || in.LedgerID != "led-a" || in.AmountCents != 2500 {
				t.Fatalf("legs = %+v, %+v", out, in)
			}
			if out.AmountCents+in.AmountCents != 0 || out.AccountID != in.AccountID || out.ApprovalID != "gate-t" || in.ApprovalID != "gate-t" {
				t.Fatalf("legs not balanced on one account under the gate: %+v, %+v", out, in)
			}
			// Both ledgers are locked, in id order, before the balance is read.
			if want := []string{"lock led-a", "lock led-b", "available", "posted", "insert", "insert"}; !slices.Equal(st.ops, want) {
				t.Fatalf("ops = %v, want %v", st.ops, want)
			}
			if len(gl.entries) != 1 {
				t.Fatalf("trust book entries = %d, want 1", len(gl.entries))
			}
			e := gl.entries[0]
			var dr, cr ledger.Cents
			for _, l := range e.Lines {
				if l.Account != books.AcctClientTrustLedger {
					t.Fatalf("line on %s, want only %s (the bank does not move)", l.Account, books.AcctClientTrustLedger)
				}
				if l.Side == ledger.Debit {
					dr += l.Amount
				} else {
					cr += l.Amount
				}
			}
			if e.Book != books.BookTrust || dr != 2500 || cr != 2500 {
				t.Fatalf("entry = %+v, want balanced 2500 in the trust book", e)
			}
		})
	}
}
