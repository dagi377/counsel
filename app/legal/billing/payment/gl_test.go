package payment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// recGL is an in-memory operating ledger that records who posted each entry.
type recGL struct {
	*ledgerfake.Fake
	clk     clock.Clock
	posters []string
	ids     []ledger.EntryID
}

func testGL() *recGL {
	loc, _ := time.LoadLocation("America/Toronto")
	clk := clockfake.New(time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC), loc)
	return &recGL{Fake: ledgerfake.New(clk.Today(context.Background()), books.AcctOperatingBank, books.AcctARFees, AcctUnapplied), clk: clk}
}

// as is PGService.GL: every Post through it records by.
func (g *recGL) as(by string) ledger.Ledger { return postAs{g: g, by: by} }

type postAs struct {
	ledger.Ledger
	g  *recGL
	by string
}

func (p postAs) Post(ctx context.Context, tx pgx.Tx, e ledger.Entry) (ledger.EntryID, error) {
	id, err := p.g.Fake.Post(ctx, tx, e)
	if err == nil {
		p.g.posters = append(p.g.posters, p.by)
		p.g.ids = append(p.g.ids, id)
	}
	return id, err
}

func (g *recGL) bal(t *testing.T, acct string) int64 {
	t.Helper()
	b, err := g.Balance(context.Background(), acct, ledger.Date{Year: 2100, Month: 1, Day: 1})
	if err != nil {
		t.Fatal(err)
	}
	return int64(b)
}

// oneAlloc is an AllocationStore over one operating payment and one open bill.
type oneAlloc struct {
	pay  AllocPayment
	bill AllocBill
	rows []AppliedAllocation
}

func (m *oneAlloc) LockPayment(context.Context, pgx.Tx, string) (AllocPayment, error) {
	return m.pay, nil
}
func (m *oneAlloc) AppliedUnder(context.Context, pgx.Tx, string) (bool, error)  { return false, nil }
func (m *oneAlloc) LockBill(context.Context, pgx.Tx, string) (AllocBill, error) { return m.bill, nil }
func (m *oneAlloc) Settle(context.Context, pgx.Tx, string) error                { return nil }
func (m *oneAlloc) Insert(_ context.Context, _ pgx.Tx, a AppliedAllocation) (string, error) {
	m.rows = append(m.rows, a)
	return "alloc-2", nil
}

// TestBIL_L_042_ReceiptThenApplyThenReverse walks one payment through capture, an approved
// allocation and a reversal, and checks every journal balances and the running balances.
func TestBIL_L_042_ReceiptThenApplyThenReverse(t *testing.T) {
	ctx := context.Background()
	on := time.Date(2026, 10, 11, 0, 0, 0, 0, mustZone(t))
	// Capture 100000 against bill b1 (90400 open): 90400 applies at capture, 9600 stays unapplied.
	c := Capture{ClientID: "c1", BillID: "b1", Kind: KindBillPayment, Method: "eft", RecordedBy: "clerk",
		ReceivedOn: on, AmountCents: 100000}
	gl := testGL()
	tx := &scriptTx{row: map[string][]any{insertPaymentSQL: {"pay-1"}, insertAllocationSQL: {"alloc-1"},
		lockBillSQL: openBillRow("c1", "delivered")}}
	if _, _, err := (PGService{DB: txOnce{tx}, Payers: payers{"c1": "Client One"}, GL: gl.as}).Capture(ctx, c); err != nil {
		t.Fatalf("Capture: %v", err)
	}
	// Approve allocating the 9600 left to bill b2 (9600 open, no tax).
	st := &oneAlloc{pay: AllocPayment{ID: "pay-1", ClientID: "c1", AmountCents: 100000, AllocatedCents: 90400,
		DepositTo: DepositOperating, Status: "posted"}, bill: AllocBill{PayerID: "c1", Status: "delivered", OpenCents: 9600}}
	payload, _ := json.Marshal(map[string]any{"payment_id": "pay-1", "allocations": []map[string]any{
		{"target_type": "bill", "target_id": "b2", "amount_cents": 9600}}})
	g := registry.Approved{GateID: "gate-1", Kind: KindAllocation, Subject: registry.Subject{Kind: SubjectKind, ID: "pay-1"},
		Payload: payload, Preparer: registry.Principal{Kind: registry.PrincipalUser, UserID: "clerk"},
		Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: "lawyer"}}}
	if err := (&AllocationEffect{Store: st, Ledger: gl.as("lawyer"), Clock: gl.clk}).Apply(ctx, nil, g); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	afterApply := [3]int64{gl.bal(t, books.AcctOperatingBank), gl.bal(t, AcctUnapplied), gl.bal(t, books.AcctARFees)}
	// Reverse: the two applied bill rows come back as -1 x rows.
	rtx := &scriptTx{row: map[string][]any{humanSQL: {true}, lockPaymentSQL: {"posted", int64(100000)},
		getPaymentSQL: {"pay-1", on, "c1", int64(100000), "eft", "", "", "clerk", "reversed", "nsf", "", "boss", "b1"}},
		rows: map[string][][]any{reverseAllocationsSQL: {{"alloc-3", "bill", int64(-90400)}, {"alloc-4", "bill", int64(-9600)}}}}
	if _, err := (PGService{DB: txOnce{rtx}, GL: gl.as, Clock: gl.clk}).Reverse(ctx, "pay-1", ReasonNSF, Actor{UserID: "boss"}); err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	tests := []struct {
		name string
		got  [3]int64 // bank, unapplied, AR
		want [3]int64
	}{
		// rule: BIL-L-042
		// Dr bank 100000 / Cr unapplied 100000, then Dr unapplied / Cr AR for each bill row.
		{"after capture and allocation", afterApply, [3]int64{100000, 0, -100000}},
		// rule: BIL-I-019
		{"after reversal", [3]int64{gl.bal(t, books.AcctOperatingBank), gl.bal(t, AcctUnapplied), gl.bal(t, books.AcctARFees)}, [3]int64{0, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("bank, unapplied, AR = %v, want %v", tt.got, tt.want)
			}
		})
	}
	// Six journals: receipt, capture-time apply, approved apply, two reversed applies, receipt reversal.
	wantPosters := []string{"clerk", "clerk", "lawyer", "boss", "boss", "boss"}
	if len(gl.posters) != len(wantPosters) {
		t.Fatalf("posted %d journals by %v, want %v", len(gl.posters), gl.posters, wantPosters)
	}
	for i, id := range gl.ids {
		e, ok := gl.Entry(id)
		if !ok || gl.posters[i] != wantPosters[i] || e.Book != books.BookOperating {
			t.Fatalf("journal %d = %+v by %s, want operating by %s", i, e, gl.posters[i], wantPosters[i])
		}
		var dr, cr ledger.Cents
		for _, l := range e.Lines {
			// rule: BIL-L-034
			// Trust money never touches these accounts.
			if l.Account == books.AcctTrustBank || l.Account == books.AcctClientTrustLedger {
				t.Fatalf("journal %d touches trust account %s", i, l.Account)
			}
			if l.Side == ledger.Debit {
				dr += l.Amount
			} else {
				cr += l.Amount
			}
		}
		if dr != cr || dr <= 0 {
			t.Fatalf("journal %d debits %d credits %d: unbalanced", i, dr, cr)
		}
	}
}

func TestBIL_L_034_TrustMoneyPostsNothing(t *testing.T) {
	tests := []struct {
		name string
		run  func(gl *recGL) error
		err  error
	}{
		{"retainer capture refused", func(gl *recGL) error {
			_, _, err := PGService{DB: txOnce{&scriptTx{}}, Payers: payers{"c1": "Client One"}, GL: gl.as}.Capture(context.Background(),
				Capture{ClientID: "c1", BillID: "b1", Kind: KindRetainer, Method: "eft", RecordedBy: "u1", AmountCents: 100})
			return err
		}, ErrRetainerToOperating},
		{"trust deposit allocation refused", func(gl *recGL) error {
			payload, _ := json.Marshal(map[string]any{"payment_id": "pay-1", "allocations": []map[string]any{
				{"target_type": "bill", "target_id": "b2", "amount_cents": 100}}})
			st := &oneAlloc{pay: AllocPayment{ID: "pay-1", ClientID: "c1", AmountCents: 100, DepositTo: "trust"}}
			return (&AllocationEffect{Store: st, Ledger: gl.as("lawyer"), Clock: gl.clk}).Apply(context.Background(), nil,
				registry.Approved{GateID: "g", Kind: KindAllocation, Payload: payload,
					Preparer:  registry.Principal{Kind: registry.PrincipalUser, UserID: "clerk"},
					Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: "lawyer"}}})
		}, ErrTrustMoney},
		{"capture without GL fails closed", func(gl *recGL) error {
			_, _, err := PGService{DB: txOnce{&scriptTx{}}, Payers: payers{"c1": "Client One"}}.Capture(context.Background(),
				Capture{ClientID: "c1", BillID: "b1", Kind: KindBillPayment, Method: "eft", RecordedBy: "u1", AmountCents: 100})
			return err
		}, ErrNoGL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gl := testGL()
			if err := tt.run(gl); !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if len(gl.posters) != 0 {
				t.Fatalf("posted %d journals, want none", len(gl.posters))
			}
		})
	}
}

func mustZone(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}
