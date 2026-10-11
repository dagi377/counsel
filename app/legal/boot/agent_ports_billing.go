package boot

import (
	"context"
	"fmt"
	"strings"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/db"
)

// BillingAgentSources are the module readers the billing and trust agent ports adapt. Each
// is the owning module's Postgres reader in production (NewBillingAgentSources) and a fake in
// tests.
type BillingAgentSources struct {
	Balances      agent.BillBalances // bill.PGBalances
	FeeBills      feeBillSource      // bill.PGBalances
	TrustAccounts agent.MatterTrustAccounts
	Time          matterTimeSource // timekeeping.PGWIP
	WIP           agent.UnbilledWIP
	// FirmAccount is the payee id of the firm's general account (config). Empty fails every
	// fee transfer to the firm closed.
	FirmAccount string
}

// feeBillSource reads the bill a fee transfer out of trust pays (bill.PGBalances).
type feeBillSource interface {
	FeeBill(ctx context.Context, matterID, billID string) (bill.FeeBillState, error)
}

// matterTimeSource lists a matter's priced time entries (timekeeping.PGWIP).
type matterTimeSource interface {
	MatterTime(ctx context.Context, matterID string) ([]timekeeping.PricedEntry, error)
}

// NewBillingAgentSources returns the Postgres readers over pool. firmAccount is the firm
// general account's payee id from config.
func NewBillingAgentSources(pool db.DB, firmAccount string) (BillingAgentSources, error) {
	if pool == nil {
		return BillingAgentSources{}, fmt.Errorf("billing agent ports: DB: %w", ErrMissingDependency)
	}
	balances := bill.PGBalances{DB: pool}
	wip := timekeeping.PGWIP{DB: pool}
	return BillingAgentSources{
		Balances:      balances,
		FeeBills:      balances,
		TrustAccounts: trust.PGMatterAccounts{DB: pool},
		Time:          wip,
		WIP:           wip,
		FirmAccount:   firmAccount,
	}, nil
}

// missing names the sources s lacks.
func (s BillingAgentSources) missing() []string {
	var out []string
	for _, v := range []struct {
		name string
		nil  bool
	}{
		{"Balances", s.Balances == nil},
		{"FeeBills", s.FeeBills == nil},
		{"TrustAccounts", s.TrustAccounts == nil},
		{"Time", s.Time == nil},
		{"WIP", s.WIP == nil},
		{"FirmAccount", strings.TrimSpace(s.FirmAccount) == ""},
	} {
		if v.nil {
			out = append(out, v.name)
		}
	}
	return out
}

// WireBillingAgentPorts sets the billing and trust ports of p (BillBalances, TrustAccounts,
// MatterTime, UnbilledWIP) and of w (FirmAccount, Bills) from s. A missing source fails boot
// with ErrMissingDependency, and nothing is set.
func WireBillingAgentPorts(s BillingAgentSources, p *AgentPorts, w *agent.WriteDeps) error {
	if p == nil || w == nil {
		return fmt.Errorf("billing agent ports: no ports to fill: %w", ErrMissingDependency)
	}
	if m := s.missing(); len(m) > 0 {
		return fmt.Errorf("billing agent ports: %s: %w", strings.Join(m, ", "), ErrMissingDependency)
	}
	p.BillBalances = s.Balances
	p.TrustAccounts = s.TrustAccounts
	p.MatterTime = matterTime{src: s.Time}
	p.UnbilledWIP = s.WIP
	w.FirmAccount = firmAccount(s.FirmAccount)
	w.Bills = feeBills{src: s.FeeBills}
	return nil
}

// matterTime adapts timekeeping's priced entries to agent.MatterTime.
type matterTime struct{ src matterTimeSource }

var _ agent.MatterTime = matterTime{}

// MatterTime implements agent.MatterTime. A negative stored rate or amount fails the read:
// money on a time entry is never below zero (time_entry CHECKs).
func (m matterTime) MatterTime(ctx context.Context, matterID string) ([]agent.PricedEntry, error) {
	got, err := m.src.MatterTime(ctx, matterID)
	if err != nil {
		return nil, err
	}
	out := make([]agent.PricedEntry, 0, len(got))
	for _, e := range got {
		if e.RateCents < 0 || e.AmountCents < 0 {
			return nil, fmt.Errorf("matter time %s: entry %s: negative amount: %w", matterID, e.Entry.ID, agent.ErrInvalidInput)
		}
		out = append(out, agent.PricedEntry{Entry: e.Entry, RateCents: e.RateCents, AmountCents: e.AmountCents})
	}
	return out, nil
}

// feeBills adapts bill.PGBalances to agent.FeeBills.
type feeBills struct{ src feeBillSource }

var _ agent.FeeBills = feeBills{}

// FeeBill implements agent.FeeBills. Fees leave trust for the firm only on a delivered bill
// and up to its balance due (By-Law 9 s.9(1)); an undelivered or void bill reads with a zero
// DeliveredAt, and a balance due below zero reads as zero, so no fee can be paid on it.
func (f feeBills) FeeBill(ctx context.Context, matterID, billID string) (withdraw.FeeBill, error) {
	// rule: BIL-L-037
	b, err := f.src.FeeBill(ctx, matterID, billID)
	if err != nil {
		return withdraw.FeeBill{}, err
	}
	return withdraw.FeeBill{DeliveredAt: b.DeliveredAt, BalanceDueCents: max(b.BalanceDueCents, 0)}, nil
}

// firmAccount returns the firm general account's configured payee id.
func firmAccount(id string) func(context.Context) (string, error) {
	id = strings.TrimSpace(id)
	return func(context.Context) (string, error) {
		if id == "" {
			return "", fmt.Errorf("firm general account: %w", ErrMissingDependency)
		}
		return id, nil
	}
}

// compile-time checks that the Postgres readers satisfy the ports they back.
var (
	_ agent.BillBalances        = bill.PGBalances{}
	_ feeBillSource             = bill.PGBalances{}
	_ agent.MatterTrustAccounts = trust.PGMatterAccounts{}
	_ matterTimeSource          = timekeeping.PGWIP{}
	_ agent.UnbilledWIP         = timekeeping.PGWIP{}
)
