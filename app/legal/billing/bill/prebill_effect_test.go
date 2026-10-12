package bill_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// memPrebill is an in-memory bill.PrebillStore over one draft.
type memPrebill struct {
	draft   bill.PrebillDraft
	records []bill.PrebillRecord
}

func (m *memPrebill) LockDraft(_ context.Context, _ pgx.Tx, _, matterID, _ string) (bill.PrebillDraft, error) {
	if matterID != m.draft.MatterID {
		return bill.PrebillDraft{}, bill.ErrNotFound
	}
	return m.draft, nil
}

func (m *memPrebill) MarkPrebill(_ context.Context, _ pgx.Tx, r bill.PrebillRecord) error {
	m.records = append(m.records, r)
	m.draft.ApprovalID = r.ApprovalID
	return nil
}

func TestPrebillEffect(t *testing.T) {
	const matter = "0192a000-0000-7000-8000-0000000000f1"
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	payload, _ := json.Marshal(map[string]string{"matter_id": matter, "period_from": "2026-09-01", "period_to": "2026-09-30"})
	tests := []struct {
		name      string
		status    bill.Status
		approvers []registry.Principal
		repeat    bool
		want      int
		err       error
	}{
		{"draft becomes pre-bill approved", bill.StatusDraft, []registry.Principal{user("lawyer")}, false, 1, nil},
		// rule: ARC-I-012
		{"repeat is a no-op", bill.StatusDraft, []registry.Principal{user("lawyer")}, true, 1, nil},
		{"approved bill refused", bill.StatusApproved, []registry.Principal{user("lawyer")}, false, 0, bill.ErrInvalid},
		// rule: ARC-I-019
		{"agent approver refused", bill.StatusDraft, []registry.Principal{{Kind: registry.PrincipalAgent}}, false, 0, decider.ErrAgentApproves},
		// rule: ARC-I-027
		{"preparer refused", bill.StatusDraft, []registry.Principal{user("clerk")}, false, 0, decider.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &memPrebill{draft: bill.PrebillDraft{ID: "b1", MatterID: matter, Status: tt.status}}
			e := &bill.PrebillEffect{Store: st, Now: func() time.Time { return time.Date(2026, 10, 9, 15, 0, 0, 0, time.UTC) }}
			g := registry.Approved{GateID: "gate-1", Kind: bill.KindPrebillReview, Payload: payload, Preparer: user("clerk"), Approvers: tt.approvers}
			err := e.Apply(context.Background(), nil, g)
			if tt.repeat && err == nil {
				err = e.Apply(context.Background(), nil, g)
			}
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Apply = %v, want %v", err, tt.err)
			}
			if len(st.records) != tt.want {
				t.Fatalf("records = %d, want %d", len(st.records), tt.want)
			}
			if tt.want == 1 && (st.records[0].ApprovedBy != "lawyer" || st.records[0].BillID != "b1") {
				t.Fatalf("record = %+v", st.records[0])
			}
		})
	}
}
