package draft

import (
	"errors"
	"testing"
	"time"
)

// rule: BIL-L-002
func TestBIL_L_002_ItemizedDisbursementDeliveryBlock(t *testing.T) {
	sep2 := time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)
	sep3 := time.Date(2026, time.September, 3, 0, 0, 0, 0, time.UTC)
	fee := Line{ID: "f1", Kind: LineFee, Description: "Drafting and closing of purchase", AmountCents: 250000}
	land := Line{ID: "d1", Kind: LineAgentDis, Description: "Land registry fee", ItemDate: &sep2, AmountCents: 7000}
	agg := func(items ...Item) Line {
		return Line{ID: "d2", Kind: LineOwnCost, Description: "Disbursements", ItemCount: 2, Items: items, AmountCents: 15000}
	}
	tests := []struct {
		name  string
		lines []Line
		want  error
	}{
		{"lump-sum fee with narrative and dated disbursement", []Line{fee, land}, nil},
		{"aggregated disbursement with no items", []Line{fee, {ID: "d2", Kind: LineOwnCost, Description: "Disbursements", AmountCents: 15000}}, ErrDeliveryBlocked},
		{"aggregated count without items", []Line{fee, agg()}, ErrDeliveryBlocked},
		{"aggregated with every item itemized", []Line{fee, agg(Item{sep2, "Courier", 5000}, Item{sep3, "Searches", 10000})}, nil},
		{"aggregated item missing date", []Line{fee, agg(Item{sep2, "Courier", 5000}, Item{time.Time{}, "Searches", 10000})}, ErrDeliveryBlocked},
		{"aggregated item missing description", []Line{fee, agg(Item{sep2, "Courier", 5000}, Item{sep3, " ", 10000})}, ErrDeliveryBlocked},
		{"aggregated item missing amount", []Line{fee, agg(Item{sep2, "Courier", 5000}, Item{sep3, "Searches", 0})}, ErrDeliveryBlocked},
		{"aggregated with fewer items than count", []Line{fee, agg(Item{sep2, "Courier", 15000})}, ErrDeliveryBlocked},
		{"single disbursement without date", []Line{fee, {ID: "d3", Kind: LineAgentDis, Description: "Filing fee", AmountCents: 16000}}, ErrDeliveryBlocked},
		{"single disbursement without description", []Line{fee, {ID: "d3", Kind: LineAgentDis, ItemDate: &sep2, AmountCents: 16000}}, ErrDeliveryBlocked},
		{"lump-sum fee without narrative", []Line{{ID: "f2", Kind: LineFee, AmountCents: 250000}, land}, ErrDeliveryBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CheckDelivery(tt.lines); !errors.Is(err, tt.want) {
				t.Fatalf("CheckDelivery = %v, want %v", err, tt.want)
			}
		})
	}
}
