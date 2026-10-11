package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// memTransferGate is an in-memory TransferGateStore over memTransfers' status map.
type memTransferGate struct {
	m    *memTransfers
	rows map[string]HeldTransfer
}

func (s memTransferGate) LockTransfer(_ context.Context, _ pgx.Tx, id string) (HeldTransfer, error) {
	h, ok := s.rows[id]
	if !ok {
		return HeldTransfer{}, ErrNotFound
	}
	h.Status = s.m.status[id]
	return h, nil
}

func (s memTransferGate) MarkAuthorized(_ context.Context, _ pgx.Tx, id, _ string) error {
	if s.m.status[id] != "pending_authorization" {
		return ErrNotAuthorized
	}
	s.m.status[id] = "authorized"
	return nil
}

// The trust.transfer gate effect authorises the transfer and posts it through Transfers.Post.
func TestTransferEffectPostsThroughTransfers(t *testing.T) {
	ledgers := map[trust.LedgerID]trust.Ledger{
		"led-a": {ID: "led-a", AccountID: "acct-1", MatterID: "M1"},
		"led-b": {ID: "led-b", AccountID: "acct-1", MatterID: "M2"},
	}
	row := HeldTransfer{ID: "x1", FromLedger: "led-b", ToLedger: "led-a", AccountID: "acct-1", AmountCents: 2500,
		FromMatterID: "M2", ToMatterID: "M1"}
	terms := func(mod func(p *TransferPayload)) []byte {
		amount := int64(2500)
		p := TransferPayload{TransferID: "x1", FromMatterID: "M2", ToMatterID: "M1", AmountCents: &amount, Reason: "same client, new matter"}
		mod(&p)
		b, _ := json.Marshal(p)
		return b
	}
	payload := terms(func(*TransferPayload) {})
	other := int64(2400)
	lawyer := registry.Principal{Kind: registry.PrincipalUser, UserID: "lawyer1"}
	clerk := registry.Principal{Kind: registry.PrincipalUser, UserID: "clerk1"}
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "lawyer1"}
	tests := []struct {
		name     string
		approver registry.Principal
		preparer registry.Principal
		payload  []byte
		status   string
		avail    int64
		flagOff  bool
		want     error
		code     string
		wantLegs int
	}{
		{"posts", lawyer, clerk, payload, "pending_authorization", 2500, false, nil, "", 2},
		{"over available", lawyer, clerk, payload, "pending_authorization", 2499, false, ErrTrustOverdraw, CodeInsufficientTrustFunds, 0},
		{"preparer approves", lawyer, lawyer, payload, "pending_authorization", 2500, false, ErrPreparerApproves, CodeNotAuthorized, 0},
		{"agent approves", agent, clerk, payload, "pending_authorization", 2500, false, ErrNotAuthorized, CodeNotAuthorized, 0},
		{"not a signatory", clerk, lawyer, payload, "pending_authorization", 2500, false, ErrSignerNotAuthorized, CodeNotAuthorized, 0},
		{"already authorized", lawyer, clerk, payload, "authorized", 2500, false, ErrNotAuthorized, CodeNotAuthorized, 0},
		{"no reason", lawyer, clerk, []byte(`{"transfer_id":"x1"}`), "pending_authorization", 2500, false, ErrInvalid, CodeNotAuthorized, 0},
		{"flag off fails closed", lawyer, clerk, payload, "pending_authorization", 2500, true, ErrUnverified, "", 0},
		{"approved amount differs", lawyer, clerk, terms(func(p *TransferPayload) { p.AmountCents = &other }), "pending_authorization", 2500, false, ErrTermsMismatch, CodeNotAuthorized, 0},
		{"approved amount missing", lawyer, clerk, terms(func(p *TransferPayload) { p.AmountCents = nil }), "pending_authorization", 2500, false, ErrTermsMismatch, CodeNotAuthorized, 0},
		{"approved source matter differs", lawyer, clerk, terms(func(p *TransferPayload) { p.FromMatterID = "M9" }), "pending_authorization", 2500, false, ErrTermsMismatch, CodeNotAuthorized, 0},
		{"approved destination matter differs", lawyer, clerk, terms(func(p *TransferPayload) { p.ToMatterID = "M9" }), "pending_authorization", 2500, false, ErrTermsMismatch, CodeNotAuthorized, 0},
		{"approved destination matter missing", lawyer, clerk, terms(func(p *TransferPayload) { p.ToMatterID = "" }), "pending_authorization", 2500, false, ErrTermsMismatch, CodeNotAuthorized, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &memTransfers{ledgers: ledgers, avail: map[trust.LedgerID]int64{"led-b": tt.avail}, status: map[string]string{"x1": tt.status}}
			gl := &glLog{}
			e := &TransferEffect{
				Store:     memTransferGate{m: st, rows: map[string]HeldTransfer{"x1": row}},
				Transfers: &Transfers{Store: st, GL: gl, Loc: time.UTC},
				Signers:   sigs{"lawyer1": true},
				Now:       func() time.Time { return execDay.Add(15 * time.Hour) },
			}
			ctx := context.Background()
			if !tt.flagOff {
				ctx = verifiedCtx(t)
			}
			err := e.Apply(ctx, nil, registry.Approved{GateID: "gate-t", Kind: TransferGateKind, Payload: tt.payload,
				Preparer: tt.preparer, Approvers: []registry.Principal{tt.approver}})
			if !errors.Is(err, tt.want) {
				t.Fatalf("apply = %v, want %v", err, tt.want)
			}
			if tt.code != "" {
				if !errors.Is(err, gate.ErrEffectFailed) {
					t.Fatalf("apply = %v, want effect failed", err)
				}
				assertCode(t, err, tt.code)
			}
			if len(st.legs) != tt.wantLegs {
				t.Fatalf("legs = %d, want %d", len(st.legs), tt.wantLegs)
			}
			if tt.want != nil {
				if len(gl.entries) != 0 || st.status["x1"] == "posted" {
					t.Fatal("refused transfer posted")
				}
				return
			}
			if st.status["x1"] != "posted" || len(gl.entries) != 1 {
				t.Fatalf("status = %s, entries = %d", st.status["x1"], len(gl.entries))
			}
			for _, l := range st.legs {
				if l.ApprovalID != "gate-t" || l.PostedBy != "lawyer1" || !l.TxnDate.Equal(execDay) {
					t.Fatalf("leg = %+v", l)
				}
			}
		})
	}
}
