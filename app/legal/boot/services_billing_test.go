package boot

import (
	"errors"
	"strings"
	"testing"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/registry"
)

func testBillingPorts() BillingPorts {
	return BillingPorts{GL: ledgerfake.New(clock.Date{Year: 2026, Month: 10, Day: 11}), Prefix: "INV"}
}

func TestBillingServicesEffects(t *testing.T) {
	s, err := billingServices(testKernel(), testBillingPorts())
	if err != nil {
		t.Fatalf("billingServices: %v", err)
	}
	tests := []struct {
		name    string
		slug    string
		effects map[string]registry.EffectFunc
	}{
		{"approval", bill.GateBillApproval, bill.Effects(s.Approve)},
		{"void", bill.GateBillVoid, bill.VoidEffects(s.Void)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.effects[tt.slug] == nil {
				t.Fatalf("effects have no non-nil func for %s", tt.slug)
			}
		})
	}
	if got := bill.Effects(s.Approve); len(got) != 1 {
		t.Fatalf("bill.Effects = %d slugs, want 1 (billing.bill_approval)", len(got))
	}
	wired := []struct {
		name string
		ok   bool
	}{
		{"approve Store is PGStore", s.Approve.Store == bill.PGStore{}},
		{"approve FeesBook is PGFeesBook", s.Approve.FeesBook == bill.PGFeesBook{}},
		{"approve Journal is PGStore", s.Approve.Journal == bill.PGStore{}},
		{"approve Ledger is the GL", s.Approve.Ledger != nil},
		{"approve Prefix", s.Approve.Prefix == "INV"},
		{"approve Now", s.Approve.Now != nil},
		{"void Entries is PGStore", s.Void.Entries == bill.PGStore{}},
		{"void Store is PGStore", s.Void.Store == bill.PGStore{}},
		{"void Ledger is the GL", s.Void.Ledger != nil},
		{"void Now", s.Void.Now != nil},
	}
	for _, w := range wired {
		t.Run(w.name, func(t *testing.T) {
			if !w.ok {
				t.Fatalf("%s: not wired", w.name)
			}
		})
	}
}

func TestBillingServicesMissingDependency(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(k *kboot.Kernel, p *BillingPorts)
		want   string
	}{
		{"nil Clock", func(k *kboot.Kernel, _ *BillingPorts) { k.Clock = nil }, "kernel Clock"},
		{"nil GL", func(_ *kboot.Kernel, p *BillingPorts) { p.GL = nil }, "GL"},
		{"empty Prefix", func(_ *kboot.Kernel, p *BillingPorts) { p.Prefix = "" }, "Prefix"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, p := testKernel(), testBillingPorts()
			tt.mutate(k, &p)
			_, err := billingServices(k, p)
			if !errors.Is(err, ErrMissingDependency) {
				t.Fatalf("err = %v, want ErrMissingDependency", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %q, want it to name %q", err, tt.want)
			}
		})
	}
	p := testBillingPorts()
	p.Prefix = "IN-V"
	if _, err := billingServices(testKernel(), p); err == nil || !strings.Contains(err.Error(), "Prefix") {
		t.Fatalf("bad prefix err = %v, want an error naming Prefix", err)
	}
}
