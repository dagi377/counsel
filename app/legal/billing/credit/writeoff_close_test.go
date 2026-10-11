package credit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

type closerFake struct {
	wo      WriteOff
	gateID  string
	closed  Status
	lockErr error
}

func (c *closerFake) Lock(context.Context, pgx.Tx, string) (WriteOff, error) { return c.wo, c.lockErr }
func (c *closerFake) CreditGate(context.Context, pgx.Tx, string) (string, error) {
	return c.gateID, nil
}
func (c *closerFake) Close(_ context.Context, _ pgx.Tx, _ string, to Status) error {
	if c.wo.Status != StatusDraft {
		return ErrInvalidState
	}
	c.closed = to
	return nil
}

type withdrawerFake struct{ by gate.Principal }

func (w *withdrawerFake) Withdraw(_ context.Context, _ pgx.Tx, _ gate.GateID, by gate.Principal) error {
	w.by = by
	return nil
}

func TestWriteOffOnReject(t *testing.T) {
	wo := func(kind string) json.RawMessage {
		b, _ := json.Marshal(creditNotePayload{BillID: "b-1", AmountCents: 500, Kind: kind, Reason: "r"})
		return b
	}
	tests := []struct {
		name    string
		payload json.RawMessage
		status  Status
		want    Status
		wantErr bool
	}{
		// rule: BIL-C-085
		{name: "write-off closes rejected", payload: wo(payloadKindWriteOff), status: StatusDraft, want: StatusRejected},
		{name: "credit note closes nothing", payload: wo(EntryCredit), status: StatusDraft},
		{name: "bad payload", payload: json.RawMessage(`{`), status: StatusDraft, wantErr: true},
		{name: "approved draft refused", payload: wo(payloadKindWriteOff), status: StatusApproved, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &closerFake{wo: WriteOff{ID: "w-1", Status: tt.status}}
			err := RejectHooks(c)[GateKindCreditNote](context.Background(), nil, registry.Rejected{
				GateID: "g-1", Subject: registry.Subject{Kind: SubjectWriteOff, ID: "w-1"}, Payload: tt.payload})
			if (err != nil) != tt.wantErr {
				t.Fatalf("OnReject err = %v, wantErr %v", err, tt.wantErr)
			}
			if c.closed != tt.want {
				t.Fatalf("closed = %q, want %q", c.closed, tt.want)
			}
		})
	}
}

func TestWithdrawWriteOff(t *testing.T) {
	preparer := registry.Principal{Kind: registry.PrincipalUser, UserID: "u-prep"}
	tests := []struct {
		name    string
		by      registry.Principal
		status  Status
		gateID  string
		wantErr error
	}{
		{name: "preparer withdraws", by: preparer, status: StatusDraft, gateID: "g-1"},
		// rule: ARC-I-016
		{name: "other user refused", by: registry.Principal{Kind: registry.PrincipalUser, UserID: "u-2"}, status: StatusDraft, gateID: "g-1", wantErr: gate.ErrNotPreparer},
		{name: "agent refused", by: registry.Principal{Kind: registry.PrincipalAgent, ActingFor: "u-prep", UserID: "u-prep"}, status: StatusDraft, gateID: "g-1", wantErr: gate.ErrNotPreparer},
		{name: "approved refused", by: preparer, status: StatusApproved, gateID: "g-1", wantErr: ErrInvalidState},
		{name: "unsubmitted refused", by: preparer, status: StatusDraft, wantErr: ErrInvalidState},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &closerFake{wo: WriteOff{ID: "w-1", Status: tt.status, PreparedBy: "u-prep"}, gateID: tt.gateID}
			g := &withdrawerFake{}
			err := WithdrawWriteOff(context.Background(), nil, c, g, "w-1", tt.by)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (c.closed != StatusWithdrawn || g.by.UserID != "u-prep") {
				t.Fatalf("closed = %q, withdrawn by %+v", c.closed, g.by)
			}
			if tt.wantErr != nil && c.closed != "" {
				t.Fatalf("closed %q on error", c.closed)
			}
		})
	}
}
