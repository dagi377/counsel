package recon

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// memSignOff is an in-memory SignOffStore over one row. Sign refuses the row's preparer, as
// PGSignOff.Sign does outside the sole-practitioner exception.
type memSignOff struct {
	st    SignOffState
	signs []SignOffRecord
}

func (m *memSignOff) LoadForSignOff(_ context.Context, _ pgx.Tx, id string) (SignOffState, error) {
	if id != "rec-1" {
		return SignOffState{}, ErrNotFound
	}
	return m.st, nil
}

func (m *memSignOff) Sign(_ context.Context, _ pgx.Tx, r SignOffRecord) error {
	if r.By.UserID == m.st.PreparedBy {
		return ErrSelfApproval
	}
	if m.st.Status != "draft" && m.st.Status != "ready" {
		return ErrInvalidState
	}
	m.signs = append(m.signs, r)
	m.st.Status, m.st.ApprovalID = "signed", r.ApprovalID
	return nil
}

func TestSignOffEffect(t *testing.T) {
	clerk := registry.Principal{Kind: registry.PrincipalUser, UserID: "clerk"}
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: "lawyer"}
	opener := registry.Principal{Kind: registry.PrincipalUser, UserID: "opener"}
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "opener"}
	payload := func(adjusted int64) []byte {
		b, _ := json.Marshal(map[string]any{"reconciliation_id": "rec-1", "adjusted_bank_cents": adjusted,
			"journal_balance_cents": 10000, "client_listing_total_cents": 10000})
		return b
	}
	ready := SignOffState{PreparedBy: "clerk", Status: "ready", AdjustedBankCents: 10000, JournalBalanceCents: 10000,
		ClientListingTotalCents: 10000, PackageSHA256: []byte{1, 2, 3}}
	signed := ready
	signed.Status, signed.ApprovalID = "signed", "gate-1"
	tests := []struct {
		name      string
		flagOff   bool
		st        SignOffState
		subject   string
		payload   []byte
		preparer  registry.Principal
		approver  registry.Principal
		want      error
		wantSigns int
	}{
		{"approval signs the row", false, ready, "rec-1", payload(10000), opener, lawyer, nil, 1},
		// rule: ARC-I-012
		{"repeat under the same gate is a no-op", false, signed, "rec-1", payload(10000), opener, lawyer, nil, 0},
		{"signed under another gate fails closed", false, func() SignOffState { s := signed; s.ApprovalID = "gate-0"; return s }(), "rec-1", payload(10000), opener, lawyer, ErrInvalidState, 0},
		// rule: ARC-I-019
		{"agent refused", false, ready, "rec-1", payload(10000), opener, agent, ErrAgentApproval, 0},
		// rule: ARC-I-021
		{"gate preparer refused", false, ready, "rec-1", payload(10000), opener, opener, ErrSelfApproval, 0},
		{"agent's acting user refused", false, ready, "rec-1", payload(10000), agent, opener, ErrSelfApproval, 0},
		{"row preparer refused by Sign", false, ready, "rec-1", payload(10000), opener, clerk, ErrSelfApproval, 0},
		// rule: BIL-I-009
		{"approved figures differ", false, ready, "rec-1", payload(9999), opener, lawyer, ErrUnbalanced, 0},
		{"payload names another row", false, ready, "rec-2", payload(10000), opener, lawyer, ErrInvalidState, 0},
		// rule: BIL-I-025
		{"by-law 9 flag off fails closed", true, ready, "rec-1", payload(10000), opener, lawyer, ErrUnverified, 0},
	}
	at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &memSignOff{st: tt.st}
			e := &SignOffEffect{Store: store, Now: func() time.Time { return at }}
			ctx := context.Background()
			if !tt.flagOff {
				ctx = verifiedCtx(t)
			}
			err := e.Apply(ctx, nil, registry.Approved{GateID: "gate-1", Kind: GateSignOff,
				Subject: registry.Subject{Kind: SubjectReconciliation, ID: tt.subject}, Payload: tt.payload,
				Preparer: tt.preparer, Approvers: []registry.Principal{tt.approver}})
			if !errors.Is(err, tt.want) {
				t.Fatalf("apply = %v, want %v", err, tt.want)
			}
			if len(store.signs) != tt.wantSigns {
				t.Fatalf("signs = %d, want %d", len(store.signs), tt.wantSigns)
			}
			if tt.wantSigns == 1 {
				r := store.signs[0]
				if r.ApprovalID != "gate-1" || r.By.UserID != "lawyer" || !r.At.Equal(at) || len(r.PackageSHA256) == 0 {
					t.Fatalf("sign = %+v", r)
				}
			}
		})
	}
}
