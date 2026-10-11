// Package paymentfake is an in-memory payment.Service for tests in other packages.
package paymentfake

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
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
	// Cash is the cash limit every cash payment is checked against; while nil a cash payment
	// fails closed with payment.ErrCashUnchecked.
	Cash payment.CashLimit
	// NewID names a new payment; while nil payments are named pay_<n>.
	NewID func() string

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

// rounder is the kernel rounding function (BIL-C-002).
var rounder = moneyfake.New()

// Capture records and allocates c.
func (f *Fake) Capture(ctx context.Context, c payment.Capture) (payment.Payment, []payment.Allocation, error) {
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
	if c.AmountCents <= 0 || c.ClientID == "" || c.ProcessorFeeCents < 0 || c.ProcessorFeeCents > c.AmountCents {
		return payment.Payment{}, nil, payment.ErrInvalid
	}
	b, ok := f.bills[c.BillID]
	if !ok {
		return payment.Payment{}, nil, fmt.Errorf("paymentfake: bill %s: %w", c.BillID, payment.ErrNotFound)
	}
	if b.client != c.ClientID {
		return payment.Payment{}, nil, payment.ErrOtherClient
	}
	if err := payment.CheckCash(ctx, nil, f.Cash, c); err != nil {
		return payment.Payment{}, nil, err
	}
	f.seq++
	p := &payment.Payment{ID: "pay_" + strconv.Itoa(f.seq), Capture: c, Status: payment.StatusCaptured}
	if f.NewID != nil {
		p.ID = f.NewID()
	}
	applied := min(c.AmountCents, b.fee+b.tax)
	var as []payment.Allocation
	if applied > 0 {
		tax, err := rounder.Rhu(applied*b.tax, b.fee+b.tax)
		if err != nil {
			return payment.Payment{}, nil, fmt.Errorf("allocate tax: %w", err)
		}
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
func (f *Fake) Reverse(_ context.Context, id string, reason payment.ReversalReason, by payment.Actor) (payment.Payment, error) {
	// A reversal records the human who made it; an agent never reverses.
	if by.Agent {
		return payment.Payment{}, payment.ErrAgentReverse
	}
	if by.UserID == "" {
		return payment.Payment{}, fmt.Errorf("paymentfake: reverse %s: %w", id, payment.ErrInvalid)
	}
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
	p.Status, p.Reason, p.ReversedBy = payment.StatusReversed, reason, by.UserID
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

// List returns payments matching flt, ordered by id.
func (f *Fake) List(_ context.Context, flt payment.ListFilter) ([]payment.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []payment.Payment
	for _, p := range f.payments {
		c := p.Capture
		switch {
		case p.ID <= flt.After,
			flt.ClientID != "" && c.ClientID != flt.ClientID,
			!flt.ReceivedFrom.IsZero() && c.ReceivedOn.Before(flt.ReceivedFrom),
			!flt.ReceivedTo.IsZero() && c.ReceivedOn.After(flt.ReceivedTo):
			continue
		}
		out = append(out, *p)
	}
	slices.SortFunc(out, func(a, b payment.Payment) int { return strings.Compare(a.ID, b.ID) })
	if flt.Limit > 0 && len(out) > flt.Limit {
		out = out[:flt.Limit]
	}
	return out, nil
}
