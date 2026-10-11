package withdraw

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// feeBills is a FeeBills over a map of bills.
type feeBills map[string]FeeBill

func (f feeBills) FeeBill(_ context.Context, _ pgx.Tx, id string) (FeeBill, error) {
	b, ok := f[id]
	if !ok {
		return FeeBill{}, ErrNotFound
	}
	return b, nil
}

// rule: BIL-L-035 (verify)
// rule: BIL-L-037 (verify)
// By-Law 9 s.9(1): money for fees leaves trust only for a delivered bill on the same matter,
// up to its unpaid balance. The trust effect re-checks the bill at authorization.
func TestFeeWithdrawalBillRecheckedAtAuthorization(t *testing.T) {
	day := execDay.Add(9 * time.Hour)
	bills := feeBills{
		"bill-ok":       {MatterID: "M1", DeliveredAt: day.Add(-24 * time.Hour), BalanceDueCents: 4000},
		"bill-small":    {MatterID: "M1", DeliveredAt: day.Add(-24 * time.Hour), BalanceDueCents: 3999},
		"bill-draft":    {MatterID: "M1", BalanceDueCents: 9000},
		"bill-later":    {MatterID: "M1", DeliveredAt: day.Add(time.Hour), BalanceDueCents: 9000},
		"bill-other":    {MatterID: "M2", DeliveredAt: day.Add(-24 * time.Hour), BalanceDueCents: 9000},
		"bill-nomatter": {DeliveredAt: day.Add(-24 * time.Hour), BalanceDueCents: 9000},
	}
	tests := []struct {
		name    string
		reason  string
		billID  string
		bills   FeeBills
		wantErr error
	}{
		{name: "delivered bill on the matter, amount equals balance", reason: "fees_to_operating", billID: "bill-ok", bills: bills},
		{name: "agent fee purpose", reason: "fees", billID: "bill-ok", bills: bills},
		{name: "amount above unpaid balance", reason: "fees_to_operating", billID: "bill-small", bills: bills, wantErr: ErrExceedsBillBalance},
		{name: "bill not delivered", reason: "fees_to_operating", billID: "bill-draft", bills: bills, wantErr: ErrBillNotDelivered},
		{name: "bill delivered after authorization", reason: "fees_to_operating", billID: "bill-later", bills: bills, wantErr: ErrBillNotDelivered},
		{name: "bill on another matter", reason: "fees_to_operating", billID: "bill-other", bills: bills, wantErr: ErrBillMatter},
		{name: "bill reader gives no matter", reason: "fees_to_operating", billID: "bill-nomatter", bills: bills, wantErr: ErrBillMatter},
		{name: "fee purpose without a bill", reason: "fees_to_operating", bills: bills, wantErr: ErrBillMatter},
		{name: "bill named on a disbursement is still checked", reason: "disbursement", billID: "bill-other", bills: bills, wantErr: ErrBillMatter},
		{name: "no bill reader fails closed", reason: "fees_to_operating", billID: "bill-ok", wantErr: ErrNotAuthorized},
		{name: "unknown bill", reason: "fees_to_operating", billID: "nope", bills: bills, wantErr: ErrNotFound},
		{name: "disbursement needs no bill", reason: "disbursement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, false)
			h := r.store.w["w1"]
			h.Request.Reason, h.Request.BillID = tt.reason, tt.billID
			r.store.w["w1"] = h
			r.e.Bills = tt.bills
			err := r.e.Apply(context.Background(), nil, r.approved(user("lawyer1")))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Apply err = %v, want %v", err, tt.wantErr)
			}
			if got := r.store.w["w1"].Status; (err == nil) != (got == StatusAuthorized) {
				t.Fatalf("status = %s after err %v", got, err)
			}
		})
	}
}

// rule: ARC-I-019
// The approval binds the bill: a gate approved for one bill never authorizes another.
func TestFeeWithdrawalApprovalBindsBill(t *testing.T) {
	r := newEffectRig(t, 10000, 4000, false)
	h := r.store.w["w1"]
	h.Request.Reason, h.Request.BillID = "fees_to_operating", "bill-ok"
	r.store.w["w1"] = h
	r.e.Bills = feeBills{"bill-ok": {MatterID: "M1", DeliveredAt: execDay, BalanceDueCents: 4000},
		"bill-2": {MatterID: "M1", DeliveredAt: execDay, BalanceDueCents: 4000}}
	other := h
	other.Request.BillID = "bill-2"
	err := r.e.Apply(context.Background(), nil, approvedPayload(termsPayload(other), user("lawyer1")))
	if !errors.Is(err, ErrTermsMismatch) {
		t.Fatalf("Apply err = %v, want ErrTermsMismatch", err)
	}
}
