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
	draft := WriteOff{ID: "w1", BillID: "b1", AmountCents: 56500, Reason: "uncollectible", Status: StatusDraft, PreparedBy: "clerk", ArmsLength: ArmsLengthYes}
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

// rule: BIL-C-205
// rule: BIL-T-080
// ETA s.231 relief is on the written-off amount less its interest: interest is not
// consideration for the taxable supply.
func TestBIL_T_080_BadDebtReliefExcludesInterest(t *testing.T) {
	c := Calc{M: moneyfake.New()}
	on := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	bill := Bill{ID: "b1", Delivered: true, OpenCents: 60000, ResponsibleUser: "lawyer"}
	lawyer := Approver{UserID: "lawyer"}
	tests := []struct {
		name     string
		amount   int64
		interest int64
		wantTax  int64
		wantErr  error
	}{
		{"no interest: relief on the whole amount", 56500, 0, 6500, nil},
		{"some interest: relief on amount less interest", 60000, 3500, 6500, nil},
		{"all interest: no relief", 3500, 3500, 0, nil},
		{"interest above the amount", 3500, 3501, 0, ErrInvalidAmount},
		{"negative interest", 3500, -1, 0, ErrInvalidAmount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := WriteOff{ID: "w1", BillID: "b1", AmountCents: tt.amount, InterestCents: tt.interest, Reason: "uncollectible", Status: StatusDraft, PreparedBy: "clerk", ArmsLength: ArmsLengthYes}
			_, adj, err := c.ApproveWriteOff(w, bill, 13000, 113000, lawyer, on)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && adj.TaxCents != tt.wantTax {
				t.Fatalf("tax = %d, want %d", adj.TaxCents, tt.wantTax)
			}
		})
	}
}

// rule: BIL-T-080
// ETA s.231 relief needs an arm's-length determination recorded on the write-off. Not
// checked fails closed; not at arm's length approves with no relief and a recorded reason.
func TestBIL_T_080_WriteOffArmsLength(t *testing.T) {
	c := Calc{M: moneyfake.New()}
	on := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	bill := Bill{ID: "b1", Delivered: true, OpenCents: 56500, ResponsibleUser: "lawyer"}
	lawyer := Approver{UserID: "lawyer"}
	tests := []struct {
		name       string
		arms       ArmsLength
		wantTax    int64
		wantReason string
		wantErr    error
	}{
		{"at arm's length: relief", ArmsLengthYes, 6500, "", nil},
		{"not at arm's length: no relief, reason recorded", ArmsLengthNo, 0, NoReliefNotArmsLength, nil},
		{"not checked: refused", "", 0, "", ErrArmsLengthUnchecked},
		{"unknown value: refused", "maybe", 0, "", ErrArmsLengthUnchecked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := WriteOff{ID: "w1", BillID: "b1", AmountCents: 56500, Reason: "uncollectible", Status: StatusDraft,
				PreparedBy: "clerk", ArmsLength: tt.arms}
			got, adj, err := c.ApproveWriteOff(w, bill, 13000, 113000, lawyer, on)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if adj.TaxCents != tt.wantTax || adj.NoReliefReason != tt.wantReason {
				t.Fatalf("adj = %+v, want tax %d reason %q", adj, tt.wantTax, tt.wantReason)
			}
			if got.ArmsLength != tt.arms || got.Status != StatusApproved {
				t.Fatalf("write-off = %+v", got)
			}
		})
	}
}
