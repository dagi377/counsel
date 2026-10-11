package trustapply

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

type store struct {
	bill  Bill
	avail int64
}

func (s *store) LockBill(_ context.Context, id string) (Bill, error) {
	if id != s.bill.ID {
		return Bill{}, errors.New("not found")
	}
	return s.bill, nil
}

// LockFunds holds back a protected amount so the test sees Available, not raw cleared cents.
func (s *store) LockFunds(context.Context, string, string) (withdraw.Funds, error) {
	return withdraw.Funds{ClearedCents: s.avail + 1000, ProtectedCents: 1000}, nil
}

func day(d int) time.Time { return time.Date(2026, 3, d, 0, 0, 0, 0, time.UTC) }

func applier(t *testing.T, s *store) Applier {
	t.Helper()
	return Applier{Store: s}
}

func delivered(on time.Time) Bill {
	return Bill{ID: "b1", MatterID: "m1", DeliveredOn: on, BalanceCents: 100000, ReservedCents: 30000}
}

func transfer(amount int64) Transfer {
	return Transfer{BillID: "b1", TrustAccountID: "t1", MatterID: "m1", AmountCents: amount, On: day(10)}
}

func check(t *testing.T, flag bool, b Bill, avail int64, tr Transfer) error {
	t.Helper()
	_, err := applier(t, &store{bill: b, avail: avail}).Check(legalflagstest.Context(t, FlagBylaw9Verified, flag), tr)
	return err
}

// rule: BIL-L-035
func TestBIL_L_035_DeliveredOnly(t *testing.T) {
	tests := []struct {
		name string
		bill Bill
		err  error
	}{
		{"undelivered bill", delivered(time.Time{}), ErrNotDelivered},
		{"delivered after transfer date", delivered(day(11)), ErrNotDelivered},
		{"delivered same day", delivered(day(10)), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := check(t, true, tc.bill, 1_000_000, transfer(1000)); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

// rule: BIL-C-050
func TestBIL_C_050_AmountCap(t *testing.T) {
	tests := []struct {
		name   string
		amount int64
		avail  int64
		err    error
	}{
		{"at bill cap", 70000, 100000, nil},
		{"over bill balance less reserved", 70001, 100000, ErrExceedsBill},
		{"at trust available", 50000, 50000, nil},
		{"over trust available", 50001, 50000, ErrTrustOverdraw},
		{"zero amount", 0, 50000, ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := check(t, true, delivered(day(1)), tc.avail, transfer(tc.amount)); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
	tr := transfer(100)
	tr.MatterID = "m2"
	if err := check(t, true, delivered(day(1)), 100000, tr); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other matter err = %v, want ErrInvalid", err)
	}
}

// rule: BIL-C-051
func TestBIL_C_051_SecondAuthorizationFails(t *testing.T) {
	s := &store{bill: delivered(day(1)), avail: 60000}
	a := applier(t, s)
	ctx := legalflagstest.Context(t, FlagBylaw9Verified, true)
	if _, err := a.Check(ctx, transfer(60000)); err != nil {
		t.Fatalf("first: %v", err)
	}
	// The first authorization reserves the funds; the second sees the reduced balance.
	s.avail -= 60000
	s.bill.ReservedCents += 60000
	if _, err := a.Check(ctx, transfer(10000)); !errors.Is(err, ErrTrustOverdraw) {
		t.Fatalf("second err = %v, want ErrTrustOverdraw", err)
	}
}

// rule: BIL-C-054
func TestBIL_C_054_ReservedCapsTransfer(t *testing.T) {
	tests := []struct {
		name     string
		reserved int64
		amount   int64
		err      error
	}{
		{"nothing reserved", 0, 100000, nil},
		{"reservation blocks", 100000, 1, ErrExceedsBill},
		{"cancelled reservation released", 0, 1, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := delivered(day(1))
			b.ReservedCents = tc.reserved
			if err := check(t, true, b, 1_000_000, transfer(tc.amount)); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

// rule: BIL-C-055
func TestBIL_C_055_ExcessToDueToTrust(t *testing.T) {
	tests := []struct {
		name            string
		amount, balance int64
		want            Application
	}{
		{"fits", 500, 800, Application{AppliedCents: 500}},
		{"exact", 800, 800, Application{AppliedCents: 800}},
		{"excess", 1000, 800, Application{AppliedCents: 800, DueToTrustCents: 200, ReturnTaskDue: day(10)}},
		{"bill already paid", 300, 0, Application{DueToTrustCents: 300, ReturnTaskDue: day(10)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Apply(tc.amount, tc.balance, day(10))
			if err != nil || got != tc.want {
				t.Fatalf("Apply = %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

// rule: BIL-I-011
func TestBIL_I_011_TransferInvariant(t *testing.T) {
	tests := []struct {
		name string
		bill Bill
		amt  int64
		err  error
	}{
		{"undelivered", delivered(time.Time{}), 1, ErrNotDelivered},
		{"reserved plus new over balance", delivered(day(1)), 70001, ErrExceedsBill},
		{"within balance", delivered(day(1)), 70000, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := check(t, true, tc.bill, 1_000_000, transfer(tc.amt)); !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
		})
	}
}

func TestFlagOffByDefault(t *testing.T) {
	if err := check(t, false, delivered(day(1)), 1_000_000, transfer(1)); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}
