package books_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/gates/decider"
	"github.com/dagi377/counsel/kernel/registry"
)

// memOpRecon is an in-memory books.OpReconStore over one reconciliation.
type memOpRecon struct {
	r     books.OpRecon
	signs int
}

func (m *memOpRecon) LockRecon(context.Context, pgx.Tx, string, string) (books.OpRecon, error) {
	return m.r, nil
}

func (m *memOpRecon) SignRecon(_ context.Context, _ pgx.Tx, _, _, gateID string, _ time.Time) error {
	m.r.Status, m.r.ApprovalID = "signed", gateID
	m.signs++
	return nil
}

func TestOpReconEffect(t *testing.T) {
	user := func(id string) registry.Principal {
		return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
	}
	payload := func(bank, book int64) json.RawMessage {
		b, _ := json.Marshal(map[string]any{"account_id": "acct", "period_end": "2026-09-30", "bank_balance_cents": bank, "book_balance_cents": book})
		return b
	}
	tests := []struct {
		name      string
		payload   json.RawMessage
		adjusted  int64
		approvers []registry.Principal
		repeat    bool
		want      int
		err       error
	}{
		{"reconciled month signed", payload(50000, 48000), 48000, []registry.Principal{user("lawyer")}, false, 1, nil},
		// rule: ARC-I-012
		{"repeat is a no-op", payload(50000, 48000), 48000, []registry.Principal{user("lawyer")}, true, 1, nil},
		// rule: BIL-I-009
		{"approved figures differ refused", payload(50001, 48000), 48000, []registry.Principal{user("lawyer")}, false, 0, books.ErrReconMismatch},
		{"unreconciled month refused", payload(50000, 48000), 47000, []registry.Principal{user("lawyer")}, false, 0, books.ErrReconMismatch},
		// rule: ARC-I-021
		{"row preparer refused", payload(50000, 48000), 48000, []registry.Principal{user("bookkeeper")}, false, 0, decider.ErrPreparerApproves},
		// rule: ARC-I-019
		{"agent approver refused", payload(50000, 48000), 48000, []registry.Principal{{Kind: registry.PrincipalAgent}}, false, 0, decider.ErrAgentApproves},
		// rule: ARC-I-027
		{"gate preparer refused", payload(50000, 48000), 48000, []registry.Principal{user("clerk")}, false, 0, decider.ErrPreparerApproves},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &memOpRecon{r: books.OpRecon{ID: "r1", PreparedBy: "bookkeeper", Status: "ready",
				BankBalanceCents: 50000, AdjustedBankCents: tt.adjusted, BookBalanceCents: 48000}}
			e := &books.OpReconEffect{Store: st, Now: time.Now}
			g := registry.Approved{GateID: "gate-1", Kind: books.KindOperatingReconciliation, Payload: tt.payload, Preparer: user("clerk"), Approvers: tt.approvers}
			err := e.Apply(context.Background(), nil, g)
			if tt.repeat && err == nil {
				err = e.Apply(context.Background(), nil, g)
			}
			if !errors.Is(err, tt.err) || (tt.err == nil && err != nil) {
				t.Fatalf("Apply = %v, want %v", err, tt.err)
			}
			if st.signs != tt.want {
				t.Fatalf("signs = %d, want %d", st.signs, tt.want)
			}
		})
	}
}
