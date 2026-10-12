package payment_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// memAllocations is an in-memory payment.AllocationStore.
type memAllocations struct {
	pay     payment.AllocPayment
	bills   map[string]payment.AllocBill
	rows    []payment.AppliedAllocation
	settled []string
}

func (m *memAllocations) LockPayment(_ context.Context, _ pgx.Tx, id string) (payment.AllocPayment, error) {
	if id != m.pay.ID {
		return payment.AllocPayment{}, payment.ErrNotFound
	}
	return m.pay, nil
}

func (m *memAllocations) AppliedUnder(_ context.Context, _ pgx.Tx, gateID string) (bool, error) {
	for _, r := range m.rows {
		if r.ApprovalID == gateID {
			return true, nil
		}
	}
	return false, nil
}

func (m *memAllocations) LockBill(_ context.Context, _ pgx.Tx, id string) (payment.AllocBill, error) {
	b, ok := m.bills[id]
	if !ok {
		return payment.AllocBill{}, payment.ErrNotFound
	}
	return b, nil
}

func (m *memAllocations) Insert(_ context.Context, _ pgx.Tx, a payment.AppliedAllocation) (string, error) {
	m.rows = append(m.rows, a)
	m.pay.AllocatedCents += a.Total()
	return fmt.Sprintf("0192a000-0000-7000-8000-%012d", len(m.rows)), nil
}

func (m *memAllocations) Settle(_ context.Context, _ pgx.Tx, id string) error {
	m.settled = append(m.settled, id)
	return nil
}

func TestAllocationEffect(t *testing.T) {
	const (
		pay    = "0192a000-0000-7000-8000-0000000000a1"
		billA  = "0192a000-0000-7000-8000-0000000000b1"
		client = "0192a000-0000-7000-8000-0000000000c1"
		gateID = "0192a000-0000-7000-8000-0000000000d1"
	)
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	agent := registry.Principal{Kind: registry.PrincipalAgent, ActingFor: "clerk"}
	payload := func(rows ...map[string]any) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"payment_id": pay, "allocations": rows})
		return b
	}
	bill := func(cents int64) map[string]any {
		return map[string]any{"target_type": "bill", "target_id": billA, "amount_cents": cents}
	}
	unapplied := func(cents int64) map[string]any {
		return map[string]any{"target_type": "unapplied", "amount_cents": cents}
	}
	tests := []struct {
		name      string
		deposit   string
		payload   json.RawMessage
		approvers []registry.Principal
		repeat    bool
		wantRows  int
		wantTax   int64
		err       error
	}{
		{"bill and unapplied applied, tax split", "operating", payload(bill(11300), unapplied(700)), []registry.Principal{user("lawyer")}, false, 2, 1300, nil},
		// rule: ARC-I-012
		{"repeat is a no-op", "operating", payload(bill(11300)), []registry.Principal{user("lawyer")}, true, 1, 1300, nil},
		// rule: DM-045
		{"more than the payment refused", "operating", payload(bill(11300), unapplied(800)), []registry.Principal{user("lawyer")}, false, 0, 0, payment.ErrInvalid},
		// rule: DM-047
		{"more than open on the bill refused", "operating", payload(bill(11301)), []registry.Principal{user("lawyer")}, false, 0, 0, payment.ErrInvalid},
		// rule: BIL-L-034
		{"trust ledger target refused", "operating", payload(map[string]any{"target_type": "trust_ledger", "target_id": billA, "amount_cents": 100}), []registry.Principal{user("lawyer")}, false, 0, 0, payment.ErrTrustMoney},
		{"trust deposit refused", "trust", payload(bill(100)), []registry.Principal{user("lawyer")}, false, 0, 0, payment.ErrTrustMoney},
		// rule: ARC-I-019
		{"agent approver refused", "operating", payload(bill(100)), []registry.Principal{agent}, false, 0, 0, decider.ErrAgentApproves},
		// rule: ARC-I-027
		{"preparer approver refused", "operating", payload(bill(100)), []registry.Principal{user("clerk")}, false, 0, 0, decider.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			clk := clockfake.New(time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC), mustToronto(t))
			gl := ledgerfake.New(clk.Today(ctx), books.AcctOperatingBank, books.AcctARFees, books.AcctClientCredit)
			st := &memAllocations{
				pay:   payment.AllocPayment{ID: pay, ClientID: client, AmountCents: 12000, DepositTo: tt.deposit, Status: "received"},
				bills: map[string]payment.AllocBill{billA: {PayerID: client, Status: "delivered", OpenCents: 11300, OpenTax: 1300}},
			}
			e := &payment.AllocationEffect{Store: st, Ledger: gl, Clock: clk}
			g := registry.Approved{GateID: gateID, Kind: payment.KindAllocation, Subject: registry.Subject{Kind: payment.SubjectKind, ID: pay},
				Payload: tt.payload, Preparer: user("clerk"), Approvers: tt.approvers}
			err := e.Apply(ctx, nil, g)
			if tt.repeat && err == nil {
				err = e.Apply(ctx, nil, g)
			}
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Apply = %v, want %v", err, tt.err)
			}
			if len(st.rows) != tt.wantRows {
				t.Fatalf("rows = %d, want %d", len(st.rows), tt.wantRows)
			}
			if tt.wantRows > 0 {
				if st.rows[0].TaxCents != tt.wantTax || st.rows[0].By != "lawyer" || len(st.settled) != 1 {
					t.Fatalf("row = %+v settled %v", st.rows[0], st.settled)
				}
				// rule: BIL-L-042
				bank, _ := gl.Balance(ctx, books.AcctOperatingBank, clk.Today(ctx))
				if int64(bank) != st.pay.AllocatedCents {
					t.Fatalf("bank = %d, want %d", bank, st.pay.AllocatedCents)
				}
			}
		})
	}
}
