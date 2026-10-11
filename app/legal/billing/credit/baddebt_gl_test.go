package credit

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

type writeOffMap map[string]WriteOff

func (m writeOffMap) Lock(_ context.Context, _ pgx.Tx, id string) (WriteOff, error) {
	w, ok := m[id]
	if !ok {
		return WriteOff{}, ErrNotFound
	}
	return w, nil
}

func badDebtLedger() *ledgerfake.Fake {
	all := []string{
		books.AcctBadDebt, books.AcctSalesTaxPayable, books.AcctARFees, books.AcctARInterest,
		books.AcctTrustBank, books.AcctClientTrustLedger,
	}
	return ledgerfake.New(ledger.Date{Year: 2026, Month: 11, Day: 15}, all...)
}

func withInterest(w WriteOff, cents int64) WriteOff {
	w.InterestCents = cents
	return w
}

// rule: BIL-C-205
// BIL-J-009: Dr OP-6100 pre-tax open + interest, Dr OP-2100 open tax / Cr OP-1100, Cr OP-1110.
func TestGLBadDebt(t *testing.T) {
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatal(err)
	}
	on := time.Date(2026, 11, 15, 0, 0, 0, 0, loc)
	approved := func(id string, amt int64) WriteOff {
		return WriteOff{ID: id, BillID: "b1", AmountCents: amt, Status: StatusApproved}
	}
	tests := []struct {
		name      string
		w         WriteOff
		adj       BadDebtAdjustment
		wantLines []ledger.Line
		wantErr   error
	}{
		{
			name: "BIL-X-014 with HST relief", w: approved("w1", 59000),
			adj: BadDebtAdjustment{BillID: "b1", TaxCents: 6500, On: on},
			wantLines: []ledger.Line{
				{Account: books.AcctBadDebt, Side: ledger.Debit, Amount: 52500},
				{Account: books.AcctSalesTaxPayable, Side: ledger.Debit, Amount: 6500},
				{Account: books.AcctARFees, Side: ledger.Credit, Amount: 59000},
			},
		},
		{
			name: "no tax: no HST line", w: approved("w2", 5000),
			adj: BadDebtAdjustment{BillID: "b1", On: on},
			wantLines: []ledger.Line{
				{Account: books.AcctBadDebt, Side: ledger.Debit, Amount: 5000},
				{Account: books.AcctARFees, Side: ledger.Credit, Amount: 5000},
			},
		},
		{
			name: "all tax: no expense line", w: approved("w3", 130),
			adj: BadDebtAdjustment{BillID: "b1", TaxCents: 130, On: on},
			wantLines: []ledger.Line{
				{Account: books.AcctSalesTaxPayable, Side: ledger.Debit, Amount: 130},
				{Account: books.AcctARFees, Side: ledger.Credit, Amount: 130},
			},
		},
		{
			name: "interest comes off OP-1110", w: withInterest(approved("w9", 59500), 500),
			adj: BadDebtAdjustment{BillID: "b1", TaxCents: 6500, On: on},
			wantLines: []ledger.Line{
				{Account: books.AcctBadDebt, Side: ledger.Debit, Amount: 53000},
				{Account: books.AcctSalesTaxPayable, Side: ledger.Debit, Amount: 6500},
				{Account: books.AcctARFees, Side: ledger.Credit, Amount: 59000},
				{Account: books.AcctARInterest, Side: ledger.Credit, Amount: 500},
			},
		},
		{
			name: "interest only: no fees AR line", w: withInterest(approved("w10", 700), 700),
			adj: BadDebtAdjustment{BillID: "b1", On: on},
			wantLines: []ledger.Line{
				{Account: books.AcctBadDebt, Side: ledger.Debit, Amount: 700},
				{Account: books.AcctARInterest, Side: ledger.Credit, Amount: 700},
			},
		},
		{name: "interest above amount", w: withInterest(approved("w11", 100), 101), adj: BadDebtAdjustment{BillID: "b1", On: on}, wantErr: ErrInvalidAmount},
		{name: "negative interest", w: withInterest(approved("w12", 100), -1), adj: BadDebtAdjustment{BillID: "b1", On: on}, wantErr: ErrInvalidAmount},
		{name: "tax and interest above amount", w: withInterest(approved("w13", 100), 50),
			adj: BadDebtAdjustment{BillID: "b1", TaxCents: 51, On: on}, wantErr: ErrInvalidAmount},
		{name: "draft write-off", w: WriteOff{ID: "w4", BillID: "b1", AmountCents: 100, Status: StatusDraft},
			adj: BadDebtAdjustment{BillID: "b1", On: on}, wantErr: ErrInvalidState},
		{name: "other bill", w: approved("w5", 100), adj: BadDebtAdjustment{BillID: "b2", On: on}, wantErr: ErrInvalidState},
		{name: "no date", w: approved("w6", 100), adj: BadDebtAdjustment{BillID: "b1"}, wantErr: ErrInvalidState},
		{name: "tax above amount", w: approved("w7", 100), adj: BadDebtAdjustment{BillID: "b1", TaxCents: 101, On: on}, wantErr: ErrInvalidAmount},
		{name: "negative tax", w: approved("w8", 100), adj: BadDebtAdjustment{BillID: "b1", TaxCents: -1, On: on}, wantErr: ErrInvalidAmount},
		{name: "unknown write-off", w: WriteOff{ID: "missing"}, adj: BadDebtAdjustment{BillID: "b1", On: on}, wantErr: ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := badDebtLedger()
			store := writeOffMap{}
			if tt.w.ID != "missing" {
				store[tt.w.ID] = tt.w
			}
			g := GLBadDebt{Ledger: l, WriteOffs: store}
			err := g.PostBadDebt(context.Background(), nil, tt.w.ID, tt.adj)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("PostBadDebt err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if _, ok := l.Entry("je-1"); ok {
					t.Fatal("an entry was posted on error")
				}
				return
			}
			e, ok := l.Entry("je-1")
			if !ok {
				t.Fatal("no entry posted")
			}
			if e.Book != books.BookOperating || e.SourceKind != SourceWriteOff || e.SourceID != tt.w.ID {
				t.Fatalf("entry = %s %s/%s, want operating write_off/%s", e.Book, e.SourceKind, e.SourceID, tt.w.ID)
			}
			if e.Date != (ledger.Date{Year: 2026, Month: 11, Day: 15}) {
				t.Fatalf("date = %v", e.Date)
			}
			if !reflect.DeepEqual(e.Lines, tt.wantLines) {
				t.Fatalf("lines = %+v\nwant %+v", e.Lines, tt.wantLines)
			}
			// Same write-off again: the ledger returns the first entry and posts nothing.
			if err := g.PostBadDebt(context.Background(), nil, tt.w.ID, tt.adj); err != nil {
				t.Fatalf("repeat: %v", err)
			}
			if _, ok := l.Entry("je-2"); ok {
				t.Fatal("repeat posted a second entry")
			}
			end := ledger.Date{Year: 2026, Month: 11, Day: 30}
			if bal, _ := l.Balance(context.Background(), books.AcctARFees, end); int64(bal) != -(tt.w.AmountCents - tt.w.InterestCents) {
				t.Fatalf("AR balance = %d, want %d", bal, -(tt.w.AmountCents - tt.w.InterestCents))
			}
			if bal, _ := l.Balance(context.Background(), books.AcctARInterest, end); int64(bal) != -tt.w.InterestCents {
				t.Fatalf("AR interest balance = %d, want %d", bal, -tt.w.InterestCents)
			}
		})
	}
}

// A write-off entry never posts to the trust book, whatever the amounts.
func TestBadDebtEntryNeverTrust(t *testing.T) {
	on := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	for _, tax := range []int64{0, 1, 65, 100} {
		e, err := BadDebtEntry(WriteOff{ID: "w1", BillID: "b1", AmountCents: 100, Status: StatusApproved},
			BadDebtAdjustment{BillID: "b1", TaxCents: tax, On: on})
		if err != nil {
			t.Fatal(err)
		}
		if e.Book == books.BookTrust || e.Book != books.BookOperating {
			t.Fatalf("tax %d: book %s", tax, e.Book)
		}
		var dr, cr ledger.Cents
		for _, l := range e.Lines {
			if strings.HasPrefix(l.Account, "TR-") || l.Account == books.AcctTrustBank || l.Account == books.AcctClientTrustLedger {
				t.Fatalf("tax %d: trust account %s", tax, l.Account)
			}
			if l.Side == ledger.Debit {
				dr += l.Amount
			} else {
				cr += l.Amount
			}
		}
		if dr != cr {
			t.Fatalf("tax %d: unbalanced %d/%d", tax, dr, cr)
		}
	}
}

func TestGLBadDebtNeedsDeps(t *testing.T) {
	err := GLBadDebt{}.PostBadDebt(context.Background(), nil, "w1", BadDebtAdjustment{})
	if !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v", err)
	}
}

// rule: BIL-C-205
// The store reads the draft's interest bucket, so GLBadDebt posts it to OP-1110.
func TestPGWriteOffLockReadsInterest(t *testing.T) {
	tests := []struct {
		name     string
		interest int64
	}{
		{"no interest", 0},
		{"interest bucket", 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := newDraftTx()
			tx.drafts["w1"].split.Interest = tt.interest
			w, err := PGWriteOffs{}.Lock(context.Background(), tx, "w1")
			if err != nil {
				t.Fatalf("Lock: %v", err)
			}
			if w.InterestCents != tt.interest {
				t.Fatalf("InterestCents = %d, want %d", w.InterestCents, tt.interest)
			}
		})
	}
}
