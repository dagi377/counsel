// Package draftfake is an in-memory draft.Calc and draft.Store for tests.
package draftfake

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/kernel/money"
	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

var (
	_ draft.Calc  = (*Fake)(nil)
	_ draft.Store = (*Fake)(nil)
)

// Fake implements draft arithmetic and keeps lines per bill in memory.
type Fake struct {
	mu     sync.Mutex
	bills  map[string][]draft.Line
	frozen map[string]bool
}

// New returns an empty Fake.
func New() *Fake { return &Fake{bills: map[string][]draft.Line{}, frozen: map[string]bool{}} }

// rounder is the kernel rounding function (BIL-C-002).
var rounder = moneyfake.New()

// Net implements draft.Calc.
func (f *Fake) Net(l draft.Line) int64 {
	n := l.AmountCents + l.MarkupCents - l.LineDiscountCents - l.DiscountShareCents
	if l.Reduction != nil {
		n -= l.Reduction.Cents
	}
	return n
}

// ApplyReduction implements draft.Calc.
func (f *Fake) ApplyReduction(l draft.Line, r draft.Reduction) (draft.Line, error) {
	// rule: BIL-C-023
	if r.Reason == "" || (r.Reason == draft.ReasonOther && strings.TrimSpace(r.ReasonText) == "") {
		return l, fmt.Errorf("reduce line %s: %w", l.ID, draft.ErrReasonRequired)
	}
	out := l
	out.Reduction = &r
	if f.Net(out) < 0 {
		return l, fmt.Errorf("reduce line %s: %w", l.ID, draft.ErrNegativeLine)
	}
	return out, nil
}

func eligible(l draft.Line, d draft.Discount) bool {
	switch d.Base {
	case draft.BaseFeesOnly:
		return l.Kind == draft.LineFee || l.Kind == draft.LineFlat
	case draft.BaseOneLine:
		return l.ID == d.LineID && l.Kind != draft.LineAgentDis
	default:
		return l.Kind != draft.LineAgentDis
	}
}

// ApplyDiscount implements draft.Calc.
func (f *Fake) ApplyDiscount(lines []draft.Line, d draft.Discount) ([]draft.Line, error) {
	out := slices.Clone(lines)
	var idx []int
	var base int64
	for i := range out {
		out[i].DiscountShareCents = 0
		if eligible(out[i], d) {
			idx = append(idx, i)
			base += f.Net(out[i])
		}
	}
	// rule: BIL-C-021
	var total int64
	switch d.Kind {
	case draft.DiscountPercent:
		if d.PPM < 0 || d.PPM > 1_000_000 {
			return lines, fmt.Errorf("discount ppm %d: %w", d.PPM, draft.ErrInvalidDiscount)
		}
		t, err := rounder.Rhu(base*d.PPM, money.PPMScale)
		if err != nil {
			return lines, fmt.Errorf("discount: %w", err)
		}
		total = t
	case draft.DiscountFixed:
		if d.AmountCents < 0 {
			return lines, fmt.Errorf("discount amount: %w", draft.ErrInvalidDiscount)
		}
		total = min(d.AmountCents, base)
	default:
		return lines, fmt.Errorf("discount kind %q: %w", d.Kind, draft.ErrInvalidDiscount)
	}
	if total == 0 || base == 0 {
		return out, nil
	}
	// rule: BIL-C-022
	type rem struct {
		i    int
		frac int64
	}
	var rems []rem
	var given int64
	for _, i := range idx {
		w := f.Net(out[i])
		share := total * w / base
		out[i].DiscountShareCents = share
		given += share
		rems = append(rems, rem{i, total * w % base})
	}
	sort.SliceStable(rems, func(a, b int) bool {
		if rems[a].frac != rems[b].frac {
			return rems[a].frac > rems[b].frac
		}
		return out[rems[a].i].SortOrder < out[rems[b].i].SortOrder
	})
	for k := 0; given < total; k++ {
		out[rems[k].i].DiscountShareCents++
		given++
	}
	return out, nil
}

// Markup implements draft.Calc.
func (f *Fake) Markup(kind draft.LineKind, costCents, ppm int64) (int64, error) {
	// rule: BIL-C-041
	if kind == draft.LineAgentDis && ppm != 0 {
		return 0, draft.ErrMarkupOnAgent
	}
	if kind != draft.LineOwnCost {
		return 0, nil
	}
	return rounder.Rhu(costCents*ppm, money.PPMScale)
}

// CheckDelivery implements draft.Calc.
func (f *Fake) CheckDelivery(lines []draft.Line) error {
	for _, l := range lines {
		if l.Kind != draft.LineOwnCost && l.Kind != draft.LineAgentDis {
			continue
		}
		// rule: BIL-L-002
		if l.ItemCount > 1 {
			if len(l.Items) != l.ItemCount {
				return fmt.Errorf("line %s: %d of %d items itemized: %w", l.ID, len(l.Items), l.ItemCount, draft.ErrDeliveryBlocked)
			}
			for _, it := range l.Items {
				if it.Date.IsZero() || strings.TrimSpace(it.Description) == "" {
					return fmt.Errorf("line %s: item without date or description: %w", l.ID, draft.ErrDeliveryBlocked)
				}
			}
		}
	}
	return nil
}

// Freeze marks a bill as past draft; later SaveLines return draft.ErrNotDraft.
func (f *Fake) Freeze(billID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frozen[billID] = true
}

// Lines implements draft.Store.
func (f *Fake) Lines(_ context.Context, billID string) ([]draft.Line, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.bills[billID]
	if !ok {
		return nil, fmt.Errorf("lines %s: %w", billID, draft.ErrNotFound)
	}
	return slices.Clone(l), nil
}

// SaveLines implements draft.Store.
func (f *Fake) SaveLines(_ context.Context, billID string, lines []draft.Line) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.frozen[billID] {
		return fmt.Errorf("save lines %s: %w", billID, draft.ErrNotDraft)
	}
	f.bills[billID] = slices.Clone(lines)
	return nil
}
