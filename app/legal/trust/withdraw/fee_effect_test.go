package withdraw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

func TestFeeEffectFlagOff(t *testing.T) {
	// rule: BIL-L-035 (verify)
	err := FeeEffect(&Effect{})(context.Background(), nil, registry.Approved{GateID: "g"})
	if !errors.Is(err, ErrUnverified) {
		t.Fatalf("err = %v, want ErrUnverified", err)
	}
}

func TestRequestGate(t *testing.T) {
	tests := []struct {
		name     string
		r        Request
		wantKind string
		wantBill bool
	}{
		{"disbursement opens trust.withdrawal", Request{MatterID: "m", AmountCents: 100, Payee: "p", Method: MethodCheque, Reason: "disbursement"}, GateKind, false},
		// rule: BIL-L-035 (verify)
		{"fee opens the fee gate", Request{MatterID: "m", AmountCents: 100, Payee: "p", Method: MethodEFT, Reason: "fees", BillID: "b"}, GateFeeKind, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequestGateKind(tt.r); got != tt.wantKind {
				t.Fatalf("kind = %s, want %s", got, tt.wantKind)
			}
			raw, err := RequestGatePayload("w-1", "l-1", tt.r)
			if err != nil {
				t.Fatal(err)
			}
			var p map[string]any
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.UseNumber()
			if err := dec.Decode(&p); err != nil {
				t.Fatal(err)
			}
			if p["withdrawal_id"] != "w-1" || p["amount_cents"] != json.Number("100") {
				t.Fatalf("payload = %s", raw)
			}
			if _, ok := p["bill_id"]; ok != tt.wantBill {
				t.Fatalf("bill_id present = %v", ok)
			}
		})
	}
}

type openFake struct {
	kind     string
	preparer gate.Principal
}

func (o *openFake) Open(_ context.Context, _ pgx.Tx, kind string, _ gate.Subject, _ json.RawMessage, p gate.Principal) (gate.GateID, error) {
	o.kind, o.preparer = kind, p
	return "g-1", nil
}

func (o *openFake) Withdraw(context.Context, pgx.Tx, gate.GateID, gate.Principal) error { return nil }

// execTx is a pgx.Tx that records Exec calls; any other method panics.
type execTx struct {
	pgx.Tx
	args [][]any
}

func (x *execTx) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	x.args = append(x.args, args)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func TestOpenRequestGate(t *testing.T) {
	tests := []struct {
		name     string
		r        Request
		wantKind string
	}{
		{"disbursement", Request{MatterID: "m", AmountCents: 100, Payee: "p", Method: MethodCheque, Reason: "disbursement", PreparedBy: "u-1"}, GateKind},
		{"fee", Request{MatterID: "m", AmountCents: 100, Payee: "p", Method: MethodCheque, Reason: "fees", BillID: "b", PreparedBy: "u-1"}, GateFeeKind},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, tx := &openFake{}, &execTx{}
			gid, err := openRequestGate(context.Background(), tx, o, "w-1", "l-1", tt.r)
			if err != nil {
				t.Fatal(err)
			}
			// rule: ARC-I-019
			// The gate is prepared by the request's person, so that person never decides it.
			if o.kind != tt.wantKind || o.preparer.Kind != gate.PrincipalUser || o.preparer.UserID != "u-1" {
				t.Fatalf("opened %s by %+v", o.kind, o.preparer)
			}
			// The gate id is stored on the withdrawal in the same tx.
			if gid != "g-1" || len(tx.args) != 1 || tx.args[0][0] != "w-1" || tx.args[0][1] != "g-1" {
				t.Fatalf("gate %s, stored %v", gid, tx.args)
			}
		})
	}
}
