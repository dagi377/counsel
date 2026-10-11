package draftfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/draft"
)

// rule: BIL-C-022
func TestBIL_C_022_DiscountSpread(t *testing.T) {
	f := New()
	tests := []struct {
		name   string
		lines  []draft.Line
		d      draft.Discount
		shares []int64
	}{
		{
			name: "BIL-X-005 fixed $100 over $1,200 / $360",
			lines: []draft.Line{
				{ID: "a", Kind: draft.LineFee, SortOrder: 1, AmountCents: 120000},
				{ID: "b", Kind: draft.LineFee, SortOrder: 2, AmountCents: 36000},
			},
			d:      draft.Discount{Kind: draft.DiscountFixed, AmountCents: 10000, Base: draft.BaseFeesAndOwn},
			shares: []int64{7692, 2308},
		},
		{
			name: "10% on fees + own cost, agent untouched",
			lines: []draft.Line{
				{ID: "f", Kind: draft.LineFee, SortOrder: 1, AmountCents: 100000},
				{ID: "o", Kind: draft.LineOwnCost, SortOrder: 2, AmountCents: 20000},
				{ID: "g", Kind: draft.LineAgentDis, SortOrder: 3, AmountCents: 7000},
			},
			d:      draft.Discount{Kind: draft.DiscountPercent, PPM: 100000, Base: draft.BaseFeesAndOwn},
			shares: []int64{10000, 2000, 0},
		},
		{
			name: "tie goes to earliest line",
			lines: []draft.Line{
				{ID: "x", Kind: draft.LineFee, SortOrder: 1, AmountCents: 100},
				{ID: "y", Kind: draft.LineFee, SortOrder: 2, AmountCents: 100},
			},
			d:      draft.Discount{Kind: draft.DiscountFixed, AmountCents: 1, Base: draft.BaseFeesOnly},
			shares: []int64{1, 0},
		},
		{
			name: "fixed larger than base caps at base",
			lines: []draft.Line{
				{ID: "x", Kind: draft.LineFee, SortOrder: 1, AmountCents: 100000},
			},
			d:      draft.Discount{Kind: draft.DiscountFixed, AmountCents: 500000, Base: draft.BaseFeesOnly},
			shares: []int64{100000},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.ApplyDiscount(tt.lines, tt.d)
			if err != nil {
				t.Fatalf("ApplyDiscount() = %v", err)
			}
			for i, w := range tt.shares {
				if got[i].DiscountShareCents != w {
					t.Errorf("line %s share = %d, want %d", got[i].ID, got[i].DiscountShareCents, w)
				}
				if f.Net(got[i]) < 0 {
					t.Errorf("line %s negative", got[i].ID)
				}
			}
			if tt.lines[0].DiscountShareCents != 0 {
				t.Errorf("input mutated")
			}
		})
	}
	if _, err := f.ApplyDiscount(nil, draft.Discount{Kind: "bogus"}); !errors.Is(err, draft.ErrInvalidDiscount) {
		t.Errorf("bogus kind = %v", err)
	}
}

// rule: BIL-C-041
func TestBIL_C_041_Markup(t *testing.T) {
	tests := []struct {
		name string
		kind draft.LineKind
		cost int64
		ppm  int64
		want int64
		err  error
	}{
		{"own 10%", draft.LineOwnCost, 5000, 100000, 500, nil},
		{"BIL-X-016 $25 + 15%", draft.LineOwnCost, 2500, 150000, 375, nil},
		{"agent with markup", draft.LineAgentDis, 5000, 100000, 0, draft.ErrMarkupOnAgent},
		{"agent no markup", draft.LineAgentDis, 5000, 0, 0, nil},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.Markup(tt.kind, tt.cost, tt.ppm)
			if !errors.Is(err, tt.err) || got != tt.want {
				t.Errorf("Markup() = %d, %v; want %d, %v", got, err, tt.want, tt.err)
			}
		})
	}
}

// rule: BIL-C-023
func TestBIL_C_023_Reduction(t *testing.T) {
	line := draft.Line{ID: "a", Kind: draft.LineFee, AmountCents: 1000, SourceTimeEntryID: "te1"}
	tests := []struct {
		name string
		r    draft.Reduction
		net  int64
		err  error
	}{
		{"courtesy", draft.Reduction{Cents: 200, Reason: draft.ReasonCourtesy}, 800, nil},
		{"no reason", draft.Reduction{Cents: 200}, 0, draft.ErrReasonRequired},
		{"other without text", draft.Reduction{Cents: 200, Reason: draft.ReasonOther}, 0, draft.ErrReasonRequired},
		{"below zero", draft.Reduction{Cents: 1001, Reason: draft.ReasonScope}, 0, draft.ErrNegativeLine},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.ApplyReduction(line, tt.r)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && (f.Net(got) != tt.net || got.SourceTimeEntryID != "te1") {
				t.Errorf("net = %d, want %d", f.Net(got), tt.net)
			}
		})
	}
}

// rule: BIL-L-002
func TestBIL_L_002_CheckDelivery(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		l    draft.Line
		err  error
	}{
		{"single", draft.Line{Kind: draft.LineOwnCost, ItemCount: 1}, nil},
		{"itemized", draft.Line{Kind: draft.LineOwnCost, ItemCount: 2, Items: []draft.Item{{Date: day, Description: "copy"}, {Date: day, Description: "courier"}}}, nil},
		{"aggregated bare", draft.Line{Kind: draft.LineAgentDis, ItemCount: 3}, draft.ErrDeliveryBlocked},
		{"item missing date", draft.Line{Kind: draft.LineOwnCost, ItemCount: 1 + 1, Items: []draft.Item{{Description: "a"}, {Date: day, Description: "b"}}}, draft.ErrDeliveryBlocked},
		{"fee lines ignored", draft.Line{Kind: draft.LineFee, ItemCount: 5}, nil},
	}
	f := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := f.CheckDelivery([]draft.Line{tt.l}); !errors.Is(err, tt.err) {
				t.Errorf("CheckDelivery() = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestFakeStore(t *testing.T) {
	ctx := context.Background()
	f := New()
	tests := []struct {
		name   string
		bill   string
		freeze bool
		err    error
	}{
		{"draft saves", "b1", false, nil},
		{"frozen refuses", "b2", true, draft.ErrNotDraft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.freeze {
				f.Freeze(tt.bill)
			}
			if err := f.SaveLines(ctx, tt.bill, []draft.Line{{ID: "x"}}); !errors.Is(err, tt.err) {
				t.Fatalf("SaveLines() = %v, want %v", err, tt.err)
			}
		})
	}
	if got, err := f.Lines(ctx, "b1"); err != nil || len(got) != 1 {
		t.Errorf("Lines(b1) = %v, %v", got, err)
	}
	if _, err := f.Lines(ctx, "nope"); !errors.Is(err, draft.ErrNotFound) {
		t.Errorf("Lines(nope) = %v", err)
	}
}
