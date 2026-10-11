package boot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/clock"
)

type fakeBalances map[bill.ID]int64

func (f fakeBalances) BalanceCents(_ context.Context, id bill.ID) (int64, error) {
	b, ok := f[id]
	if !ok {
		return 0, bill.ErrNotFound
	}
	return b, nil
}

type fakeFeeBills map[string]bill.FeeBillState // by matter/bill

func (f fakeFeeBills) FeeBill(_ context.Context, matterID, billID string) (bill.FeeBillState, error) {
	b, ok := f[matterID+"/"+billID]
	if !ok {
		return bill.FeeBillState{}, bill.ErrNotFound
	}
	return b, nil
}

type fakeAccounts map[string][]string

func (f fakeAccounts) TrustAccountsOf(_ context.Context, matterID string) ([]string, error) {
	return f[matterID], nil
}

type fakeTime struct {
	entries []timekeeping.PricedEntry
	err     error
}

func (f fakeTime) MatterTime(context.Context, string) ([]timekeeping.PricedEntry, error) {
	return f.entries, f.err
}

type fakeWIP []string

func (f fakeWIP) UnbilledEntryIDs(context.Context, string, clock.Date) ([]string, error) {
	return f, nil
}

func billingSources() BillingAgentSources {
	return BillingAgentSources{
		Balances:      fakeBalances{"b1": 12_345},
		FeeBills:      fakeFeeBills{},
		TrustAccounts: fakeAccounts{"m1": {"a1"}},
		Time:          fakeTime{},
		WIP:           fakeWIP{"e1"},
		FirmAccount:   "firm-general",
	}
}

func TestWireBillingAgentPorts(t *testing.T) {
	noFirm := billingSources()
	noFirm.FirmAccount = "  "
	noWIP := billingSources()
	noWIP.WIP = nil
	noFees := billingSources()
	noFees.FeeBills = nil
	tests := []struct {
		name    string
		src     BillingAgentSources
		wantErr error
	}{
		{"every source wires every port", billingSources(), nil},
		{"no firm general account fails closed", noFirm, ErrMissingDependency},
		{"no unbilled WIP reader fails closed", noWIP, ErrMissingDependency},
		{"no fee bill reader fails closed", noFees, ErrMissingDependency},
		{"no sources fails closed", BillingAgentSources{}, ErrMissingDependency},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				p AgentPorts
				w agent.WriteDeps
			)
			err := WireBillingAgentPorts(tc.src, &p, &w)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			set := p.BillBalances != nil && p.TrustAccounts != nil && p.MatterTime != nil && p.UnbilledWIP != nil &&
				w.FirmAccount != nil && w.Bills != nil
			if want := tc.wantErr == nil; set != want {
				t.Fatalf("ports set = %v, want %v", set, want)
			}
			if tc.wantErr != nil {
				return
			}
			got, err := w.FirmAccount(context.Background())
			if err != nil || got != "firm-general" {
				t.Fatalf("firm account = %q, %v", got, err)
			}
			if b, err := p.BillBalances.BalanceCents(context.Background(), "b1"); err != nil || b != 12_345 {
				t.Fatalf("balance = %d, %v", b, err)
			}
		})
	}
	if err := WireBillingAgentPorts(billingSources(), nil, &agent.WriteDeps{}); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("nil ports: err = %v", err)
	}
}

func TestNewBillingAgentSourcesNeedsDB(t *testing.T) {
	if _, err := NewBillingAgentSources(nil, "firm"); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("err = %v, want ErrMissingDependency", err)
	}
}

func TestFirmAccountFailsClosed(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		want    string
		wantErr error
	}{
		{"configured", " firm-general ", "firm-general", nil},
		{"empty", "", "", ErrMissingDependency},
		{"blank", "   ", "", ErrMissingDependency},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := firmAccount(tc.id)(context.Background())
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("got %q, %v; want %q, %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

func TestFeeBills(t *testing.T) {
	delivered := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	src := fakeFeeBills{
		"m1/b1": {DeliveredAt: delivered, BalanceDueCents: 50_000},
		"m1/b2": {BalanceDueCents: 50_000},
		"m1/b3": {DeliveredAt: delivered, BalanceDueCents: -1_000},
	}
	tests := []struct {
		name    string
		matter  string
		bill    string
		want    withdraw.FeeBill
		wantErr error
	}{
		// rule: BIL-L-037
		{"delivered bill", "m1", "b1", withdraw.FeeBill{DeliveredAt: delivered, BalanceDueCents: 50_000}, nil},
		{"undelivered bill stays undelivered", "m1", "b2", withdraw.FeeBill{BalanceDueCents: 50_000}, nil},
		{"overpaid bill has nothing due", "m1", "b3", withdraw.FeeBill{DeliveredAt: delivered}, nil},
		{"bill on another matter is not found", "m2", "b1", withdraw.FeeBill{}, bill.ErrNotFound},
		{"unknown bill is not found", "m1", "b9", withdraw.FeeBill{}, bill.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := feeBills{src: src}.FeeBill(context.Background(), tc.matter, tc.bill)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestMatterTime(t *testing.T) {
	entry := timekeeping.TimeEntry{ID: "e1", MatterID: "m1", State: timekeeping.StateApproved, Billable: true}
	boom := errors.New("boom")
	tests := []struct {
		name    string
		src     fakeTime
		want    []agent.PricedEntry
		wantErr error
	}{
		{"priced entries carry their cents", fakeTime{entries: []timekeeping.PricedEntry{{Entry: entry, RateCents: 30_000, AmountCents: 15_000}}},
			[]agent.PricedEntry{{Entry: entry, RateCents: 30_000, AmountCents: 15_000}}, nil},
		{"no entries", fakeTime{}, []agent.PricedEntry{}, nil},
		{"negative amount fails the read", fakeTime{entries: []timekeeping.PricedEntry{{Entry: entry, RateCents: 30_000, AmountCents: -1}}}, nil, agent.ErrInvalidInput},
		{"reader error passes through", fakeTime{err: boom}, nil, boom},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := matterTime{src: tc.src}.MatterTime(context.Background(), "m1")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d entries, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i].Entry.ID != tc.want[i].Entry.ID || got[i].RateCents != tc.want[i].RateCents || got[i].AmountCents != tc.want[i].AmountCents {
					t.Fatalf("entry %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}
