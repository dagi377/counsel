package booksfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/books"
)

// rule: BIL-I-002
func TestBIL_I_002_CheckEntry(t *testing.T) {
	tests := []struct {
		name string
		e    books.Entry
		want error
	}{
		{"operating balanced", books.Entry{Book: books.BookOperating, Lines: []books.Line{
			{Account: books.AcctARFees, DebitCents: 11300},
			{Account: books.AcctFeeRevenue, CreditCents: 10000},
			{Account: books.AcctSalesTaxPayable, CreditCents: 1300},
		}}, nil},
		{"trust balanced", books.Entry{Book: books.BookTrust, Lines: []books.Line{
			{Account: books.AcctTrustBank, DebitCents: 500},
			{Account: books.AcctClientTrustLedger, CreditCents: 500},
		}}, nil},
		{"operating entry with trust line", books.Entry{Book: books.BookOperating, Lines: []books.Line{
			{Account: books.AcctOperatingBank, DebitCents: 500},
			{Account: books.AcctTrustBank, CreditCents: 500},
		}}, books.ErrCrossBook},
		{"unknown account", books.Entry{Book: books.BookOperating, Lines: []books.Line{
			{Account: "OP-9999", DebitCents: 1},
		}}, books.ErrUnknownAccount},
		{"unbalanced", books.Entry{Book: books.BookOperating, Lines: []books.Line{
			{Account: books.AcctOperatingBank, DebitCents: 501},
			{Account: books.AcctARFees, CreditCents: 500},
		}}, books.ErrUnbalanced},
		{"empty", books.Entry{Book: books.BookOperating}, books.ErrUnbalanced},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.CheckEntry(tt.e); !errors.Is(err, tt.want) {
				t.Errorf("CheckEntry() = %v, want %v", err, tt.want)
			}
		})
	}
	if n := len(f.Accounts()); n != 22 {
		t.Errorf("Accounts() has %d accounts, want 22", n)
	}
}

// rule: BIL-I-013
func TestBIL_I_013_Reconcile(t *testing.T) {
	tests := []struct {
		name string
		s    books.Snapshot
		n    int
	}{
		{"all match", books.Snapshot{ARBalanceCents: 100, OpenBillBalancesCents: 100, ClientCreditLedger: 5, ClientCreditSubledger: 5}, 0},
		{"ar off", books.Snapshot{ARBalanceCents: 100, OpenBillBalancesCents: 99}, 1},
		{"credit and wip off", books.Snapshot{ClientCreditLedger: 1, WIPLedgerCents: 3, UnbilledTimeValueCents: 2}, 2},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Reconcile(context.Background(), tt.s)
			if len(got) != tt.n {
				t.Fatalf("Reconcile() = %v, want %d mismatches", got, tt.n)
			}
			if (tt.n > 0) != errors.Is(err, books.ErrMismatch) {
				t.Errorf("Reconcile() err = %v", err)
			}
		})
	}
}

func TestFakeVendorBills(t *testing.T) {
	ctx := context.Background()
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		bill    books.VendorBill
		action  func(f *Fake, id string) (books.VendorBill, error)
		second  func(f *Fake, id string) (books.VendorBill, error)
		want    books.VendorBillStatus
		wantErr error
	}{
		{"pay", books.VendorBill{Vendor: "v", BillDate: day, AmountCents: 1000}, (*Fake).payCtx, nil, books.VendorBillPaid, nil},
		{"void", books.VendorBill{Vendor: "v", BillDate: day, AmountCents: 1000}, (*Fake).voidCtx, nil, books.VendorBillVoid, nil},
		{"void after paid", books.VendorBill{Vendor: "v", BillDate: day, AmountCents: 1000}, (*Fake).payCtx, (*Fake).voidCtx, "", books.ErrBadStatus},
		{"zero amount", books.VendorBill{Vendor: "v", BillDate: day}, nil, nil, "", books.ErrInvalid},
		{"negative tax", books.VendorBill{Vendor: "v", BillDate: day, AmountCents: 1, TaxCents: -1}, nil, nil, "", books.ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			b, err := f.Create(ctx, tt.bill)
			if err == nil && tt.action != nil {
				b, err = tt.action(f, b.ID)
			}
			if err == nil && tt.second != nil {
				b, err = tt.second(f, b.ID)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && b.Status != tt.want {
				t.Errorf("status = %s, want %s", b.Status, tt.want)
			}
		})
	}
	if _, err := New().Get(ctx, "missing"); !errors.Is(err, books.ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func (f *Fake) payCtx(id string) (books.VendorBill, error) {
	return f.MarkPaid(context.Background(), id)
}

func (f *Fake) voidCtx(id string) (books.VendorBill, error) {
	return f.Void(context.Background(), id)
}
