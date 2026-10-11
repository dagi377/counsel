package credit

import (
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/money/moneyfake"
)

// rule: BIL-C-205
func TestBIL_C_205_WriteOffBadDebtAdjustment(t *testing.T) {
	c := Calc{M: moneyfake.New()}
	on := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	bill := Bill{ID: "b1", Delivered: true, OpenCents: 56500, ResponsibleUser: "lawyer"}
	draft := WriteOff{ID: "w1", BillID: "b1", AmountCents: 56500, Reason: "uncollectible", Status: StatusDraft, PreparedBy: "clerk"}
	lawyer := Approver{UserID: "lawyer"}
	tests := []struct {
		name    string
		w       WriteOff
		b       Bill
		by      Approver
		wantTax int64
		wantErr error
	}{
		{"BIL-X-014: lawyer approves, open tax $65.00", draft, bill, lawyer, 6500, nil},
		{"requester cannot approve", draft, bill, Approver{UserID: "clerk"}, 0, ErrSelfApproval},
		{"lawyer who requested cannot approve", func() WriteOff { w := draft; w.PreparedBy = "lawyer"; return w }(), bill, lawyer, 0, ErrSelfApproval},
		{"agent cannot approve", draft, bill, Approver{UserID: "lawyer", IsAgent: true}, 0, ErrAgentApproval},
		{"other user is not the responsible lawyer", draft, bill, Approver{UserID: "other"}, 0, ErrNotResponsible},
		{"more than the open balance", func() WriteOff { w := draft; w.AmountCents = 56501; return w }(), bill, lawyer, 0, ErrExceedsBalance},
		{"undelivered bill", draft, Bill{ID: "b1", OpenCents: 56500, ResponsibleUser: "lawyer"}, lawyer, 0, ErrBillNotDelivered},
		{"already approved", func() WriteOff { w := draft; w.Status = StatusApproved; return w }(), bill, lawyer, 0, ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, adj, err := c.ApproveWriteOff(tt.w, tt.b, 13000, 113000, tt.by, on)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if adj.TaxCents != tt.wantTax || adj.BillID != "b1" || !adj.On.Equal(on) {
				t.Fatalf("adjustment = %+v, want %d on %v", adj, tt.wantTax, on)
			}
			if got.Status != StatusApproved || got.ApprovedBy != tt.by.UserID || got.ApprovedAt == nil || !got.ApprovedAt.Equal(on) {
				t.Fatalf("write-off = %+v, want approved by %s", got, tt.by.UserID)
			}
		})
	}
}
