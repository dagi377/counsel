package interest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/legalflags/legalflagstest"
	"github.com/dagi377/counsel/kernel/money"
)

type memCharges struct{ rows []Charge }

func (m *memCharges) Add(_ context.Context, c Charge) error {
	for _, r := range m.rows {
		if r.BillID == c.BillID && r.Period.From.Equal(c.Period.From) {
			return ErrAlreadyPosted
		}
	}
	m.rows = append(m.rows, c)
	return nil
}

// rule: BIL-L-010 (verify)
func TestBIL_L_010_NoAgreedRateNoInterest(t *testing.T) {
	loc := toronto(t)
	day := func(y int, mo time.Month, d int) time.Time { return time.Date(y, mo, d, 0, 0, 0, 0, loc) }
	delivered := day(2026, 1, 15).Add(10 * time.Hour)
	tests := []struct {
		name     string
		flagOn   bool
		b        Bill
		p        Period
		want     error
		wantDays int
		wantAmt  money.Cents
	}{
		{"no rate, long overdue", true, Bill{ID: "b1", DeliveredAt: delivered, Principal: 100000}, Period{day(2026, 3, 1), day(2027, 3, 1)}, ErrNoAgreedRate, 0, 0},
		{"no rate, flag off", false, Bill{ID: "b1", DeliveredAt: delivered, Principal: 100000}, Period{day(2026, 3, 1), day(2027, 3, 1)}, ErrNoAgreedRate, 0, 0},
		{"agreed rate, flag off", false, Bill{ID: "b2", DeliveredAt: delivered, Principal: 100000, RatePPM: 60000}, Period{day(2026, 3, 1), day(2026, 4, 1)}, ErrAccrualUnverified, 0, 0},
		{"agreed rate, before start", true, Bill{ID: "b3", DeliveredAt: delivered, Principal: 100000, RatePPM: 60000}, Period{day(2026, 1, 20), day(2026, 2, 15)}, ErrNotDue, 0, 0},
		// 100000 * 60000 * 14 / (1e6 * 365) = 230.1 -> 230
		{"agreed rate, clipped to start", true, Bill{ID: "b4", DeliveredAt: delivered, Principal: 100000, RatePPM: 60000}, Period{day(2026, 2, 1), day(2026, 3, 1)}, nil, 14, 230},
		// 100000 * 60000 * 31 / (1e6 * 365) = 509.6 -> 510
		// BIL-C-070: an agreed due date later than delivery + 1 month moves the start to it.
		// 100000 * 60000 * 10 / (1e6 * 365) = 164.4 -> 164
		{"agreed rate, due date after one month", true, Bill{ID: "b6", DeliveredAt: delivered, Principal: 100000, RatePPM: 60000, DueDate: day(2026, 3, 20)}, Period{day(2026, 3, 1), day(2026, 3, 30)}, nil, 10, 164},
		{"agreed rate, period before due date", true, Bill{ID: "b7", DeliveredAt: delivered, Principal: 100000, RatePPM: 60000, DueDate: day(2026, 3, 20)}, Period{day(2026, 3, 1), day(2026, 3, 20)}, ErrNotDue, 0, 0},
		// An earlier due date never starts interest before one month after delivery.
		{"agreed rate, due date before one month", true, Bill{ID: "b8", DeliveredAt: delivered, Principal: 100000, RatePPM: 60000, DueDate: day(2026, 1, 30)}, Period{day(2026, 2, 1), day(2026, 3, 1)}, nil, 14, 230},
		{"agreed rate, full month", true, Bill{ID: "b5", DeliveredAt: delivered, Principal: 100000, RatePPM: 60000}, Period{day(2026, 3, 1), day(2026, 4, 1)}, nil, 31, 510},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := legalflagstest.Context(t, FlagSolicitorsActVerified, tt.flagOn)
			store := &memCharges{}
			s := &Posting{Start: Start{Calendar: Months{Loc: loc}}, Calc: Calc{}, Store: store}
			c, err := s.Post(ctx, tt.b, tt.p)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Post = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				if len(store.rows) != 0 {
					t.Fatalf("stored %d charges on error", len(store.rows))
				}
				return
			}
			if c.Days != tt.wantDays || c.Amount != tt.wantAmt {
				t.Fatalf("days %d amount %d, want %d %d", c.Days, c.Amount, tt.wantDays, tt.wantAmt)
			}
			if _, err := s.Post(ctx, tt.b, tt.p); !errors.Is(err, ErrAlreadyPosted) {
				t.Fatalf("second post = %v, want ErrAlreadyPosted", err)
			}
		})
	}
}

// rule: BIL-C-204
// rule: BIL-L-049
func TestBIL_C_204_InterestLineCarriesNoHST(t *testing.T) {
	tests := []struct {
		name     string
		c        Charge
		wantDesc string
	}{
		{"spec example 247 cents at 3%", Charge{BillID: "b1", RatePPM: 30000, Amount: 247}, "Interest at 3.00% p.a. (Solicitors Act s.33)"},
		{"BIL-X-009 1475 cents at 5.25%", Charge{BillID: "b2", RatePPM: 52500, Amount: 1475}, "Interest at 5.25% p.a. (Solicitors Act s.33)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := ChargeLine(tt.c)
			if l.TaxCents != 0 || l.TaxCode != TaxExempt {
				t.Fatalf("tax %d code %q, want 0 %q", l.TaxCents, l.TaxCode, TaxExempt)
			}
			if l.AmountCents != int64(tt.c.Amount) || l.Description != tt.wantDesc {
				t.Fatalf("line %+v", l)
			}
		})
	}
}
