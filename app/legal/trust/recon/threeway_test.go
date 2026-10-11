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
