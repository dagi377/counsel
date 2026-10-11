// Package paymentfake is an in-memory payment.Service for tests in other packages.
package paymentfake

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/dagi377/counsel/app/legal/billing/payment"
)

var _ payment.Service = (*Fake)(nil)

type bill struct {
	client  string
	fee     int64 // open fee cents
	tax     int64 // open tax cents
	applied map[string]payment.Allocation
}

// Fake keeps bills, payments and allocations in memory.
type Fake struct {
	mu       sync.Mutex
	bills    map[string]*bill
	payments map[string]*payment.Payment
	allocs   map[string][]payment.Allocation
	dedup    map[string]string
	seq      int
}

// New returns an empty Fake.
func New() *Fake {
	return &Fake{bills: map[string]*bill{}, payments: map[string]*payment.Payment{}, allocs: map[string][]payment.Allocation{}, dedup: map[string]string{}}
}

// AddBill adds an open bill for client with fee and tax cents outstanding.
func (f *Fake) AddBill(id, client string, fee, tax int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bills[id] = &bill{client: client, fee: fee, tax: tax}
}

// Open returns a bill's open fee and tax cents.
func (f *Fake) Open(id string) (fee, tax int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.bills[id]
	if b == nil {
		return 0, 0
	}
	return b.fee, b.tax
}

// rhu divides num by den rounding half up; num and den are non-negative, den > 0.
func rhu(num, den int64) int64 { return (2*num + den) / (2 * den) }

// Capture records and allocates c.
func (f *Fake) Capture(_ context.Context, c payment.Capture) (payment.Payment, []payment.Allocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := c.IdempotencyKey
	if c.Provider != "" {
		key = c.Provider + "\x00" + c.ProviderEventID
	}
	if id, ok := f.dedup[key]; ok && key != "" {
		return *f.payments[id], append([]payment.Allocation(nil), f.allocs[id]...), nil
	}
	if c.Kind == payment.KindRetainer {
		return payment.Payment{}, nil, payment.ErrRetainerToOperating
	}
	if c.AmountCents <= 0 || c.ClientID == "" {
		return payment.Payment{}, nil, payment.ErrInvalid
	}
	b, ok := f.bills[c.BillID]
	if !ok {
		return payment.Payment{}, nil, fmt.Errorf("paymentfake: bill %s: %w", c.BillID, payment.ErrNotFound)
	}
	if b.client != c.ClientID {
		return payment.Payment{}, nil, payment.ErrOtherClient
	}
	f.seq++
	p := &payment.Payment{ID: "pay_" + strconv.Itoa(f.seq), Capture: c, Status: payment.StatusCaptured}
	applied := min(c.AmountCents, b.fee+b.tax)
	var as []payment.Allocation
	if applied > 0 {
		tax := rhu(applied*b.tax, b.fee+b.tax)
		a := payment.Allocation{PaymentID: p.ID, BillID: c.BillID, FeeCents: applied - tax, TaxCents: tax}
		b.fee -= a.FeeCents
		b.tax -= a.TaxCents
		as = append(as, a)
	}
	if rest := c.AmountCents - applied; rest > 0 {
		as = append(as, payment.Allocation{PaymentID: p.ID, FeeCents: rest})
	}
	f.payments[p.ID] = p
	f.allocs[p.ID] = as
	if key != "" {
		f.dedup[key] = p.ID
	}
	return *p, append([]payment.Allocation(nil), as...), nil
}

// Reverse restores each allocation to its bill's buckets.
func (f *Fake) Reverse(_ context.Context, id string, reason payment.ReversalReason) (payment.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.payments[id]
	if !ok {
		return payment.Payment{}, fmt.Errorf("paymentfake: payment %s: %w", id, payment.ErrNotFound)
	}
	if p.Status == payment.StatusReversed {
		return *p, payment.ErrReversed
	}
	for _, a := range f.allocs[id] {
		if b := f.bills[a.BillID]; b != nil {
			b.fee += a.FeeCents
			b.tax += a.TaxCents
		}
	}
	p.Status, p.Reason = payment.StatusReversed, reason
	return *p, nil
}

// Get returns one payment.
func (f *Fake) Get(_ context.Context, id string) (payment.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.payments[id]
	if !ok {
		return payment.Payment{}, fmt.Errorf("paymentfake: payment %s: %w", id, payment.ErrNotFound)
	}
	return *p, nil
}

// Allocations lists a payment's allocations.
func (f *Fake) Allocations(_ context.Context, id string) ([]payment.Allocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	as, ok := f.allocs[id]
	if !ok {
		return nil, fmt.Errorf("paymentfake: payment %s: %w", id, payment.ErrNotFound)
	}
	return append([]payment.Allocation(nil), as...), nil
}
