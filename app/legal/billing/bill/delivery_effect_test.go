package bill

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// memDeliverer is an in-memory Deliverer over one bill.
type memDeliverer struct {
	b     Bill
	calls int
}

func (m *memDeliverer) LockedBill(_ context.Context, _ pgx.Tx, id ID) (Bill, error) {
	if id != m.b.ID {
		return Bill{}, ErrNotFound
	}
	return m.b, nil
}

func (m *memDeliverer) Deliver(_ context.Context, _ pgx.Tx, _ ID, ev DeliveryEvidence) (Bill, error) {
	m.calls++
	m.b.DeliveredAt, m.b.DeliveryChannel, m.b.Status = ev.AcceptedAt, ev.Channel, StatusDelivered
	return m.b, nil
}

func TestDeliveryEffect(t *testing.T) {
	clerk := registry.Principal{Kind: registry.PrincipalUser, UserID: "clerk"}
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: "lawyer"}
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "clerk"}
	on := time.Date(2026, 10, 9, 0, 0, 0, 0, business)
	approvedBill := Bill{ID: "b-1", Status: StatusApproved, SignedBy: "lawyer"}
	delivered := approvedBill
	delivered.Status, delivered.DeliveredAt, delivered.DeliveryChannel = StatusDelivered, on.Add(9*time.Hour), ChannelMail
	const ok = `{"bill_id":"b-1","channel":"print","delivered_on":"2026-10-09","evidence_ref":"post-123"}`
	tests := []struct {
		name      string
		b         Bill
		payload   string
		preparer  registry.Principal
		approver  registry.Principal
		want      error
		wantCalls int
	}{
		{"approval records delivery", approvedBill, ok, clerk, lawyer, nil, 1},
		// rule: ARC-I-012
		{"repeat is a no-op", delivered, ok, clerk, lawyer, nil, 0},
		{"delivered on another date fails closed", delivered, `{"bill_id":"b-1","channel":"print","delivered_on":"2026-10-08","evidence_ref":"p"}`, clerk, lawyer, ErrAlreadyDelivered, 0},
		// rule: ARC-I-019
		{"agent refused", approvedBill, ok, clerk, agent, ErrAgentApproves, 0},
		// rule: ARC-I-027
		{"preparer refused", approvedBill, ok, clerk, clerk, ErrPreparerApproves, 0},
		{"agent's acting user refused", approvedBill, ok, agent, clerk, ErrPreparerApproves, 0},
		{"unknown channel", approvedBill, `{"bill_id":"b-1","channel":"fax","delivered_on":"2026-10-09","evidence_ref":"p"}`, clerk, lawyer, ErrInvalid, 0},
		{"no evidence", approvedBill, `{"bill_id":"b-1","channel":"email","delivered_on":"2026-10-09"}`, clerk, lawyer, ErrInvalid, 0},
		{"unknown bill", approvedBill, `{"bill_id":"b-9","channel":"email","delivered_on":"2026-10-09","evidence_ref":"p"}`, clerk, lawyer, ErrNotFound, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &memDeliverer{b: tt.b}
			e := &DeliveryEffect{Bills: m}
			err := e.Apply(context.Background(), nil, registry.Approved{GateID: "g-1", Kind: KindDelivery,
				Payload: []byte(tt.payload), Preparer: tt.preparer, Approvers: []registry.Principal{tt.approver}})
			if !errors.Is(err, tt.want) {
				t.Fatalf("apply = %v, want %v", err, tt.want)
			}
			if m.calls != tt.wantCalls {
				t.Fatalf("deliver calls = %d, want %d", m.calls, tt.wantCalls)
			}
			if tt.wantCalls == 1 && (!m.b.DeliveredAt.Equal(on) || m.b.DeliveryChannel != ChannelMail) {
				t.Fatalf("delivered = %v %s", m.b.DeliveredAt, m.b.DeliveryChannel)
			}
		})
	}
}
