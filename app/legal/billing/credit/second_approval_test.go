package credit

import (
	"errors"
	"testing"
)

// rule: BIL-C-085
func TestBIL_C_085_SecondApprovalAboveThreshold(t *testing.T) {
	tests := []struct {
		name   string
		amount int64
		cfg    SecondApprovalConfig
		want   bool
	}{
		{"BIL-X-014: $590.00 needs one approval", 59000, SecondApprovalConfig{}, false},
		{"exactly $1,000.00 needs one approval", 100000, SecondApprovalConfig{}, false},
		{"one cent above the default needs two", 100001, SecondApprovalConfig{}, true},
		{"firm threshold $500.00, $590.00 needs two", 59000, SecondApprovalConfig{WriteOffSecondApprovalCents: 50000}, true},
		{"firm threshold $500.00, exactly $500.00 needs one", 50000, SecondApprovalConfig{WriteOffSecondApprovalCents: 50000}, false},
		{"negative config falls back to default", 100000, SecondApprovalConfig{WriteOffSecondApprovalCents: -1}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NeedsSecondApproval(tt.amount, tt.cfg); got != tt.want {
				t.Fatalf("NeedsSecondApproval(%d, %+v) = %v, want %v", tt.amount, tt.cfg, got, tt.want)
			}
		})
	}
}

// rule: BIL-C-085
func TestBIL_C_085_CheckSecondApprover(t *testing.T) {
	managing := SecondApproval{ManagingLawyer: func(id string) bool { return id == "ml" || id == "ml2" || id == "clerk" }}
	w := WriteOff{ID: "w1", BillID: "b1", AmountCents: 150000, Status: StatusDraft, PreparedBy: "clerk", ArmsLength: ArmsLengthYes}
	lawyer := Approver{UserID: "lawyer"}
	tests := []struct {
		name    string
		s       SecondApproval
		first   Approver
		second  Approver
		wantErr error
	}{
		{"managing lawyer after the responsible lawyer", managing, lawyer, Approver{UserID: "ml"}, nil},
		{"second approver is not a managing lawyer", managing, lawyer, Approver{UserID: "other"}, ErrSecondApprovalRequired},
		{"no second approver", managing, lawyer, Approver{}, ErrSecondApprovalRequired},
		{"same person approves twice", managing, Approver{UserID: "ml"}, Approver{UserID: "ml"}, ErrSelfApproval},
		{"preparer cannot give the second approval", managing, lawyer, Approver{UserID: "clerk"}, ErrSelfApproval},
		{"agent cannot give the second approval", managing, lawyer, Approver{UserID: "ml", IsAgent: true}, ErrAgentApproval},
		{"agent first approval is refused", managing, Approver{UserID: "lawyer", IsAgent: true}, Approver{UserID: "ml"}, ErrAgentApproval},
		{"preparer first approval is refused", managing, Approver{UserID: "clerk"}, Approver{UserID: "ml"}, ErrSelfApproval},
		{"missing first approval is refused", managing, Approver{}, Approver{UserID: "ml"}, ErrSelfApproval},
		{"two managing lawyers", managing, Approver{UserID: "ml2"}, Approver{UserID: "ml"}, nil},
		{"no capability lookup fails closed", SecondApproval{}, lawyer, Approver{UserID: "ml"}, ErrSecondApprovalRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.CheckSecondApprover(w, tt.first, tt.second)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
