package draft_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/billing/batch"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/draft"
	"github.com/dagi377/counsel/app/legal/billing/draft/draftfake"
	"github.com/dagi377/counsel/kernel/clock"
)

// TestCalculatorContract runs the Calc contract against the fake and the production
// Calculator: every case gives the same result or the same sentinel error.
//
// rule: BIL-C-023
// rule: BIL-C-041
func TestCalculatorContract(t *testing.T) {
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	fee := draft.Line{ID: "l1", Kind: draft.LineFee, SortOrder: 1, AmountCents: 10_000, Taxable: true, Description: "Review of pleadings"}
	own := draft.Line{ID: "l2", Kind: draft.LineOwnCost, SortOrder: 2, AmountCents: 5_000, Taxable: true, ItemDate: &day, Description: "courier"}
	agentDis := draft.Line{ID: "l3", Kind: draft.LineAgentDis, SortOrder: 3, AmountCents: 2_500, ItemDate: &day, Description: "filing fee"}
	lines := []draft.Line{fee, own, agentDis}
	tests := []struct {
		name string
		run  func(c draft.Calc) (any, error)
		want error
	}{
		{"net", func(c draft.Calc) (any, error) { return c.Net(fee), nil }, nil},
		{"write-down", func(c draft.Calc) (any, error) {
			l, err := c.ApplyReduction(fee, draft.Reduction{Cents: 1_000, Reason: draft.ReasonCourtesy})
			return c.Net(l), err
		}, nil},
		{"write-down past zero", func(c draft.Calc) (any, error) {
			return c.ApplyReduction(fee, draft.Reduction{Cents: 10_001, Reason: draft.ReasonCourtesy})
		}, draft.ErrNegativeLine},
		{"other without text", func(c draft.Calc) (any, error) {
			return c.ApplyReduction(fee, draft.Reduction{Cents: 1, Reason: draft.ReasonOther})
		}, draft.ErrReasonRequired},
		{"markup on own cost", func(c draft.Calc) (any, error) { return c.Markup(draft.LineOwnCost, 1_001, 100_000) }, nil},
		{"markup on agent", func(c draft.Calc) (any, error) { return c.Markup(draft.LineAgentDis, 1_000, 100_000) }, draft.ErrMarkupOnAgent},
		{"ten percent discount", func(c draft.Calc) (any, error) {
			out, err := c.ApplyDiscount(lines, draft.Discount{Kind: draft.DiscountPercent, PPM: 100_000, Base: draft.BaseFeesAndOwn})
			var shares []int64
			for _, l := range out {
				shares = append(shares, l.DiscountShareCents)
			}
			return shares, err
		}, nil},
		{"delivery", func(c draft.Calc) (any, error) { return nil, c.CheckDelivery(lines) }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want, wantErr := tc.run(draftfake.New())
			got, err := tc.run(draft.Calculator{})
			if !errors.Is(err, tc.want) || !errors.Is(wantErr, tc.want) {
				t.Fatalf("err = %v, fake err = %v, want %v", err, wantErr, tc.want)
			}
			if err == nil && !equal(got, want) {
				t.Fatalf("calculator = %v, fake = %v", got, want)
			}
		})
	}
}

func equal(a, b any) bool {
	as, ok := a.([]int64)
	if !ok {
		return a == b
	}
	bs, _ := b.([]int64)
	return slices.Equal(as, bs)
}

// TestStoreContract runs the Store contract against the fake: unknown drafts are not found,
// saved lines read back, and a frozen draft refuses changes.
func TestStoreContract(t *testing.T) {
	ctx := context.Background()
	f := draftfake.New()
	line := draft.Line{ID: "l1", Kind: draft.LineFee, SortOrder: 1, AmountCents: 10_000, SourceTimeEntryID: "te1"}
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"unknown draft", func() error { _, err := f.Lines(ctx, "nope"); return err }, draft.ErrNotFound},
		{"save and read", func() error {
			if err := f.SaveLines(ctx, "b1", []draft.Line{line}); err != nil {
				return err
			}
			got, err := f.Lines(ctx, "b1")
			if err != nil || len(got) != 1 || got[0].ID != "l1" {
				return errors.New("lines not read back")
			}
			return nil
		}, nil},
		{"frozen draft", func() error { f.Freeze("b1"); return f.SaveLines(ctx, "b1", []draft.Line{line}) }, draft.ErrNotDraft},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestPGFailClosed: the Postgres store and batch refuse to run without their wiring.
//
// rule: BIL-C-110
func TestPGFailClosed(t *testing.T) {
	ctx := context.Background()
	day := clock.Date{Year: 2026, Month: time.September, Day: 30}
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"store lines without a database", func() error { _, err := draft.PGStore{}.Lines(ctx, "b1"); return err }, draft.ErrNotFound},
		{"store save without a database", func() error { return draft.PGStore{}.SaveLines(ctx, "b1", nil) }, draft.ErrNotFound},
		{"batch without a preparer", func() error {
			_, _, err := draft.PGBatch{}.Create(ctx, "k", batch.Target{MatterID: "m1"}, day)
			return err
		}, bill.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}
