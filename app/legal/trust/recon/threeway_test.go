package recon

import (
	"context"
	"errors"
	"testing"

	"github.com/dagi377/counsel/kernel/flags"
)

func verifiedCtx(t *testing.T) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: FlagBylaw9Verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(FlagBylaw9Verified, true); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

func TestComputeThreeWayFlagOff(t *testing.T) {
	_, err := ComputeThreeWay(context.Background(), 1000000, 1000000, []int64{1000000}, nil)
	if !errors.Is(err, ErrUnverified) {
		t.Fatalf("got %v, want ErrUnverified", err)
	}
}

func TestComputeThreeWay(t *testing.T) {
	cheque := ReconcilingItem{Kind: OutstandingCheque, Reference: "chq-101", AmountCents: -500}
	cases := []struct {
		name      string
		bank      int64
		book      int64
		ledgers   []int64
		items     []ReconcilingItem
		wantErr   error
		wantAdj   int64
		wantLedge int64
	}{
		{"all agree", 1000000, 1000000, []int64{600000, 400000}, nil, nil, 1000000, 1000000},
		{"bank 500 over book, no item", 1000500, 1000000, []int64{1000000}, nil, ErrUnbalanced, 1000500, 1000000},
		{"bank 500 over book, cheque outstanding", 1000500, 1000000, []int64{1000000}, []ReconcilingItem{cheque}, nil, 1000000, 1000000},
		{"ledgers 500 short of book", 1000000, 1000000, []int64{999500}, nil, ErrUnbalanced, 1000000, 999500},
		{"item without reference", 1000500, 1000000, []int64{1000000}, []ReconcilingItem{{Kind: OutstandingCheque, AmountCents: -500}}, ErrInvalidItem, 0, 0},
		{"item with unknown kind", 1000500, 1000000, []int64{1000000}, []ReconcilingItem{{Kind: "plug", Reference: "x", AmountCents: -500}}, ErrInvalidItem, 0, 0},
		{"item with zero amount", 1000000, 1000000, []int64{1000000}, []ReconcilingItem{{Kind: BankError, Reference: "x"}}, ErrInvalidItem, 0, 0},
	}
	ctx := verifiedCtx(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeThreeWay(ctx, tc.bank, tc.book, tc.ledgers, tc.items)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err %v, want %v", err, tc.wantErr)
			}
			if got.AdjustedBankCents != tc.wantAdj || got.ClientLedgerCents != tc.wantLedge {
				t.Fatalf("adjusted %d ledgers %d, want %d %d", got.AdjustedBankCents, got.ClientLedgerCents, tc.wantAdj, tc.wantLedge)
			}
			if tc.wantErr != ErrInvalidItem && got.Balanced() != (tc.wantErr == nil) {
				t.Fatalf("balanced %v with err %v", got.Balanced(), err)
			}
		})
	}
}

// TestOutstandingItemsByLaw9 checks the By-law 9 three-way comparison through the one
// outstanding-items function: bank closing balance + outstanding deposits - outstanding
// cheques = book balance = the sum of the client ledgers, and the read and write views both
// report that same net.
func TestOutstandingItemsByLaw9(t *testing.T) {
	deposit := ReconcilingItem{Kind: OutstandingDeposit, Reference: "dep-7", AmountCents: 2500}
	cheque := ReconcilingItem{Kind: OutstandingCheque, Reference: "chq-101", AmountCents: -4000}
	tests := []struct {
		name         string
		bank, book   int64
		ledgers      []int64
		items        []ReconcilingItem
		wantItems    int64
		wantBalanced bool
	}{
		{"no items", 100000, 100000, []int64{60000, 40000}, nil, 0, true},
		{"deposit only", 97500, 100000, []int64{100000}, []ReconcilingItem{deposit}, 2500, true},
		{"cheque only", 104000, 100000, []int64{70000, 30000}, []ReconcilingItem{cheque}, -4000, true},
		{"deposit and cheque", 101500, 100000, []int64{50000, 50000}, []ReconcilingItem{deposit, cheque}, -1500, true},
		{"ledgers disagree with book", 101500, 100000, []int64{50000, 49999}, []ReconcilingItem{deposit, cheque}, -1500, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tw, err := ComputeThreeWay(verifiedCtx(t), tc.bank, tc.book, tc.ledgers, tc.items)
			if tc.wantBalanced != (err == nil) {
				t.Fatalf("err = %v, want balanced %v", err, tc.wantBalanced)
			}
			got := tw.OutstandingItemsCents()
			if got != tc.wantItems {
				t.Fatalf("outstanding items = %d, want %d", got, tc.wantItems)
			}
			// bank + outstanding deposits - outstanding cheques = adjusted bank.
			var deposits, cheques int64
			for _, it := range tc.items {
				if it.AmountCents > 0 {
					deposits += it.AmountCents
				} else {
					cheques -= it.AmountCents
				}
			}
			if tw.BankCents+deposits-cheques != tw.AdjustedBankCents || tw.BankCents+got != tw.AdjustedBankCents {
				t.Fatalf("bank %d + items %d != adjusted bank %d", tw.BankCents, got, tw.AdjustedBankCents)
			}
			if tc.wantBalanced && (tw.AdjustedBankCents != tw.BookCents || tw.BookCents != tw.ClientLedgerCents) {
				t.Fatalf("balanced but adjusted %d, book %d, ledgers %d", tw.AdjustedBankCents, tw.BookCents, tw.ClientLedgerCents)
			}
			rec := Reconciliation{AccountID: "a", Period: Period{Year: 2026, Month: 8}, Result: tw}
			if r, w := reconOf(rec).OutstandingItemsCents, viewOf(rec, 1).OutstandingItemsCents; r != got || w != got {
				t.Fatalf("read view %d, write view %d, want %d", r, w, got)
			}
		})
	}
}

// rule: BIL-I-009
// TestBIL_I_009_StoredAdjustedBalanceMustMatchItems: under By-law 9 a stored reconciliation
// whose adjusted bank balance is not the bank balance plus its items is unbalanced, even when
// the stored adjusted, book and client totals agree, so no view reports it as balanced.
func TestBIL_I_009_StoredAdjustedBalanceMustMatchItems(t *testing.T) {
	deposit := ReconcilingItem{Kind: OutstandingDeposit, Reference: "dep-7", AmountCents: 2500}
	tests := []struct {
		name         string
		tw           ThreeWay
		wantAgree    bool
		wantBalanced bool
	}{
		{"items explain the adjusted balance", ThreeWay{BankCents: 97500, AdjustedBankCents: 100000, BookCents: 100000, ClientLedgerCents: 100000, Items: []ReconcilingItem{deposit}}, true, true},
		{"no items and no difference", ThreeWay{BankCents: 100000, AdjustedBankCents: 100000, BookCents: 100000, ClientLedgerCents: 100000}, true, true},
		{"stored adjusted balance without its items", ThreeWay{BankCents: 97500, AdjustedBankCents: 100000, BookCents: 100000, ClientLedgerCents: 100000}, false, false},
		{"items do not add up to the adjusted balance", ThreeWay{BankCents: 100000, AdjustedBankCents: 100000, BookCents: 100000, ClientLedgerCents: 100000, Items: []ReconcilingItem{deposit}}, false, false},
		{"items agree but ledgers do not", ThreeWay{BankCents: 97500, AdjustedBankCents: 100000, BookCents: 100000, ClientLedgerCents: 99999, Items: []ReconcilingItem{deposit}}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tw.ItemsAgree(); got != tc.wantAgree {
				t.Fatalf("ItemsAgree = %v, want %v", got, tc.wantAgree)
			}
			if got := tc.tw.Balanced(); got != tc.wantBalanced {
				t.Fatalf("Balanced = %v, want %v", got, tc.wantBalanced)
			}
			rec := Reconciliation{Result: tc.tw}
			if reconOf(rec).Balanced != tc.wantBalanced || viewOf(rec, 1).Balanced != tc.wantBalanced {
				t.Fatalf("views report balanced %v/%v, want %v", reconOf(rec).Balanced, viewOf(rec, 1).Balanced, tc.wantBalanced)
			}
		})
	}
}
