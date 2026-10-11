package boot

import (
	"fmt"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/ledger"
)

// BillingPorts are the billing services' dependencies that the composition root's caller
// supplies.
type BillingPorts struct {
	// GL is the kernel ledger: the approval effect posts the bill's accrual entry on it
	// (BIL-J-003) and the void effect reverses that entry.
	GL ledger.Ledger
	// Prefix is the bill number series prefix from config, e.g. "INV".
	Prefix string
	// VoidSink is told about each void inside the gate tx (time entries freed for
	// re-billing). Optional.
	VoidSink bill.VoidedSink
}

// billingSvcs are the billing services built at boot.
type billingSvcs struct {
	Approve *bill.ApproveEffect
	Void    *bill.VoidEffect
}

// billingServices builds the bill approval and void gate effects over the billing Postgres
// stores. The approval reads its journal lines from bill.PGStore and posts on the GL; the
// void finds that entry through bill.PGStore and reverses it on the same GL.
func billingServices(k *kboot.Kernel, p BillingPorts) (billingSvcs, error) {
	if k == nil {
		return billingSvcs{}, fmt.Errorf("billing services: kernel: %w", ErrMissingDependency)
	}
	for _, d := range []struct {
		name string
		nil  bool
	}{
		{"kernel Clock", k.Clock == nil},
		{"GL", p.GL == nil},
		{"Prefix", p.Prefix == ""},
	} {
		if d.nil {
			return billingSvcs{}, fmt.Errorf("billing services: %s: %w", d.name, ErrMissingDependency)
		}
	}
	if _, err := bill.FormatNumber(p.Prefix, 2000, 1); err != nil {
		return billingSvcs{}, fmt.Errorf("billing services: Prefix: %w", err)
	}
	return billingSvcs{
		Approve: &bill.ApproveEffect{
			Store:    bill.PGStore{},
			FeesBook: bill.PGFeesBook{},
			Journal:  bill.PGStore{},
			Ledger:   p.GL,
			Prefix:   p.Prefix,
			Now:      k.Clock.Now,
		},
		Void: &bill.VoidEffect{
			Store:   bill.PGStore{},
			Entries: bill.PGStore{},
			Ledger:  p.GL,
			Sink:    p.VoidSink,
			Now:     k.Clock.Now,
		},
	}, nil
}
