package batch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
)

// memCombined stores combined drafts by idempotency key.
type memCombined struct{ byKey map[string]string }

func (m *memCombined) CreateCombined(_ context.Context, key string, _ Combined) (string, bool, error) {
	if id, ok := m.byKey[key]; ok {
		return id, false, nil
	}
	id := fmt.Sprintf("cb%d", len(m.byKey)+1)
	m.byKey[key] = id
	return id, true, nil
}

// rule: BIL-C-111
func TestCombinedClientBill(t *testing.T) {
	sep := clock.Date{Year: 2026, Month: time.September, Day: 30}
	hst := func(base int64) []TaxBase { return []TaxBase{{Code: "HST-ON", RatePPM: 130000, BaseCents: base}} }
	parts := []MatterPart{
		{MatterID: "m3", FeesCents: 33333, TaxBases: hst(33333)},
		{MatterID: "m1", FeesCents: 100000, DisbCents: 5000, DiscountCents: 1000, TaxBases: hst(104000)},
		{MatterID: "m2", FeesCents: 33333, DisbCents: 1200, TaxBases: hst(33333)},
	}
	tests := []struct {
		name        string
		parts       []MatterPart
		wantMatters []string
		wantSub     []int64
		wantTax     int64 // rhu(170666 x 13%) once on the whole bill
		wantErr     error
	}{
		{"three matters keep subtotals and tax lines", parts, []string{"m1", "m2", "m3"}, []int64{104000, 34533, 33333}, 22187, nil},
		{"duplicate matter", []MatterPart{{MatterID: "m1"}, {MatterID: "m1"}}, nil, nil, 0, ErrInvalid},
		{"no matters", nil, nil, nil, 0, ErrInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &memCombined{byKey: map[string]string{}}
			id, created, c, err := Combine(context.Background(), d, "run1", "c1", sep, tt.parts)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Combine err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if !created || id == "" {
				t.Fatalf("first run: id %q created %v", id, created)
			}
			if len(c.Matters) != len(tt.wantMatters) {
				t.Fatalf("matters = %d, want %d", len(c.Matters), len(tt.wantMatters))
			}
			var tax, total int64
			for i, m := range c.Matters {
				if m.MatterID != tt.wantMatters[i] || m.SubtotalCents != tt.wantSub[i] {
					t.Fatalf("matter %d = %s subtotal %d, want %s %d", i, m.MatterID, m.SubtotalCents, tt.wantMatters[i], tt.wantSub[i])
				}
				if len(m.TaxLines) != 1 || m.TaxLines[0].TaxCents != m.TaxCents || m.TotalCents != m.SubtotalCents+m.TaxCents {
					t.Fatalf("matter %s tax lines %+v tax %d total %d", m.MatterID, m.TaxLines, m.TaxCents, m.TotalCents)
				}
				tax += m.TaxCents
				total += m.TotalCents
			}
			if c.TaxCents != tt.wantTax || tax != tt.wantTax || c.TotalCents != total {
				t.Fatalf("tax %d (matters %d) total %d (matters %d), want tax %d", c.TaxCents, tax, c.TotalCents, total, tt.wantTax)
			}

			// rule: BIL-C-110
			id2, created2, _, err := Combine(context.Background(), d, "run1", "c1", sep, tt.parts)
			if err != nil || created2 || id2 != id || len(d.byKey) != 1 {
				t.Fatalf("rerun: id %q created %v err %v drafts %d, want %q false nil 1", id2, created2, err, len(d.byKey), id)
			}
		})
	}
}
