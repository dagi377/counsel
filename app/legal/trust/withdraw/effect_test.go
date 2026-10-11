package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/trustfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// memStore is an in-memory EffectStore.
type memStore struct{ w map[string]Held }

func (m *memStore) Lock(_ context.Context, _ pgx.Tx, id string) (Held, error) {
	h, ok := m.w[id]
	if !ok {
		return Held{}, ErrNotFound
	}
	return h, nil
}

func (m *memStore) MarkAuthorized(_ context.Context, _ pgx.Tx, id, by, gateID string, at time.Time) error {
	h := m.w[id]
	h.Status, h.AuthorizedBy, h.GateID, h.AuthorizedAt = StatusAuthorized, by, gateID, at
	m.w[id] = h
	return nil
}

func (m *memStore) MarkExecuted(_ context.Context, _ pgx.Tx, id, by string, on time.Time) error {
	h := m.w[id]
	h.Status, h.ExecutedBy, h.ExecutedOn = StatusExecuted, by, on
	m.w[id] = h
	return nil
}

type sigs map[string]bool

func (s sigs) IsSignatory(_ context.Context, _ pgx.Tx, _ trust.AccountID, u string) (bool, error) {
	return s[u], nil
}

type firm bool

func (f firm) SolePractitioner(context.Context) (bool, error) { return bool(f), nil }

type audit struct{ actions []string }

func (a *audit) Record(_ context.Context, _ pgx.Tx, action, _, _ string) error {
	a.actions = append(a.actions, action)
	return nil
}

var execDay = time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)

type effectRig struct {
	e     *Effect
	tf    *trustfake.Fake
	store *memStore
	audit *audit
	led   trust.LedgerID
}

// newEffectRig funds a ledger with balance cents and records withdrawal "w1" of amount cents
// prepared by "prep". "lawyer1" and "lawyer2" are signatories; "bookkeeper" is not.
func newEffectRig(t *testing.T, balance, amount int64, solo bool) effectRig {
	t.Helper()
	ctx := context.Background()
	tf := trustfake.New(trustfake.Limits{}, nil)
	acct, err := tf.OpenAccount(ctx, nil, trust.Account{Type: trust.AccountMixed, Title: "Mixed trust"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tf.AddSignatory(ctx, nil, acct, "lawyer1"); err != nil {
		t.Fatal(err)
	}
	led, err := tf.OpenLedger(ctx, nil, trust.Ledger{AccountID: acct, MatterID: "M1", ClientID: "C1"})
	if err != nil {
		t.Fatal(err)
	}
	if balance > 0 {
		if _, err := tf.Receive(ctx, nil, trust.Receipt{LedgerID: led, TxnDate: execDay, Method: trust.MethodEFT,
			Counterparty: "Client", AmountCents: balance, Purpose: "retainer", ClearedOn: execDay, PostedBy: "prep"}); err != nil {
			t.Fatal(err)
		}
	}
	store := &memStore{w: map[string]Held{"w1": {LedgerID: led, Withdrawal: Withdrawal{ID: "w1", Status: StatusRequested,
		Request: Request{TrustAccountID: string(acct), MatterID: "M1", AmountCents: amount, Payee: "Vendor Inc",
			Method: MethodCheque, Reason: "disbursement", PreparedBy: "prep"}}}}}
	a := &audit{}
	e := &Effect{Ledgers: tf, Store: store, Signers: sigs{"lawyer1": true, "lawyer2": true}, Firm: firm(solo), Audit: a,
		Now: func() time.Time { return execDay.Add(9 * time.Hour) }}
	return effectRig{e: e, tf: tf, store: store, audit: a, led: led}
}

func approved(by registry.Principal) registry.Approved {
	p, _ := json.Marshal(EffectPayload{WithdrawalID: "w1"})
	return registry.Approved{GateID: "gate-1", Kind: GateKind, Version: 1, Payload: p,
		Preparer: registry.Principal{Kind: registry.PrincipalUser, UserID: "prep"}, Approvers: []registry.Principal{by}}
}

func user(id string) registry.Principal {
	return registry.Principal{Kind: registry.PrincipalUser, UserID: id}
}

func posted(t *testing.T, r effectRig) int64 {
	t.Helper()
	b, err := r.tf.Balance(context.Background(), r.led)
	if err != nil {
		t.Fatal(err)
	}
	return b.PostedCents
}

// rule: ARC-I-027
func TestARC_I_027_WithdrawalOnlyViaGateEffect(t *testing.T) {
	agent := registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: "lawyer1"}
	tests := []struct {
		name      string
		solo      bool
		approver  registry.Principal
		executor  registry.Principal
		applyErr  error
		execErr   error
		code      string
		wantAudit bool
	}{
		{"signatory approves, bookkeeper executes", false, user("lawyer1"), user("bookkeeper"), nil, nil, "", false},
		{"signatory approves, other lawyer executes", false, user("lawyer1"), user("lawyer2"), nil, nil, "", false},
		{"authorizer executes in a 2-person firm", false, user("lawyer1"), user("lawyer1"), nil, ErrNotAuthorized, CodeExecutorIsAuthorizer, false},
		{"sole practitioner executes own, audited", true, user("lawyer1"), user("lawyer1"), nil, nil, "", true},
		{"preparer approves", false, user("prep"), user("lawyer2"), ErrPreparerApproves, nil, CodeNotAuthorized, false},
		{"agent approves", false, agent, user("lawyer2"), ErrNotAuthorized, nil, CodeNotAuthorized, false},
		{"non-signatory approves", false, user("bookkeeper"), user("lawyer2"), ErrSignerNotAuthorized, nil, CodeNotAuthorized, false},
		{"agent executes", false, user("lawyer1"), agent, nil, ErrNotAuthorized, CodeNotAuthorized, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, tt.solo)
			ctx := context.Background()
			err := r.e.Apply(ctx, nil, approved(tt.approver))
			if !errors.Is(err, tt.applyErr) {
				t.Fatalf("apply = %v, want %v", err, tt.applyErr)
			}
			if tt.applyErr != nil {
				assertCode(t, err, tt.code)
				if r.store.w["w1"].Status != StatusRequested || posted(t, r) != 10000 {
					t.Fatal("refused approval changed state")
				}
				// Without an authorisation nothing can be executed.
				if _, err := r.e.Execute(ctx, nil, "w1", tt.executor, execDay); !errors.Is(err, ErrNotAuthorized) {
					t.Fatalf("execute unauthorized = %v", err)
				}
				return
			}
			if w := r.store.w["w1"]; w.Status != StatusAuthorized || w.AuthorizedBy != tt.approver.UserID || w.GateID != "gate-1" {
				t.Fatalf("after apply = %+v", w)
			}
			if posted(t, r) != 10000 {
				t.Fatal("authorisation posted money")
			}
			txn, err := r.e.Execute(ctx, nil, "w1", tt.executor, execDay)
			if !errors.Is(err, tt.execErr) {
				t.Fatalf("execute = %v, want %v", err, tt.execErr)
			}
			if tt.execErr != nil {
				assertCode(t, err, tt.code)
				if posted(t, r) != 10000 || r.store.w["w1"].Status != StatusAuthorized {
					t.Fatal("refused execution posted")
				}
				return
			}
			txns, _ := r.tf.Txns(ctx, r.led)
			last := txns[len(txns)-1]
			if last.ID != txn || last.AmountCents != -4000 || last.ApprovalID != "gate-1" || last.WithdrawalID != "w1" || last.PostedBy != tt.executor.UserID {
				t.Fatalf("posted txn = %+v", last)
			}
			if posted(t, r) != 6000 || r.store.w["w1"].Status != StatusExecuted {
				t.Fatalf("after execute: posted %d, status %s", posted(t, r), r.store.w["w1"].Status)
			}
			if got := len(r.audit.actions) == 1 && r.audit.actions[0] == AuditSoloException; got != tt.wantAudit {
				t.Fatalf("audit = %v, want solo exception %v", r.audit.actions, tt.wantAudit)
			}
			// Executing twice never posts twice.
			if _, err := r.e.Execute(ctx, nil, "w1", tt.executor, execDay); !errors.Is(err, ErrNotAuthorized) || posted(t, r) != 6000 {
				t.Fatalf("second execute = %v, posted %d", err, posted(t, r))
			}
		})
	}
}

// rule: ARC-I-024
func TestARC_I_024_EffectRechecksBalance(t *testing.T) {
	tests := []struct {
		name     string
		balance  int64
		amount   int64
		drain    int64 // paid out of the ledger between authorisation and execution
		applyErr error
		execErr  error
	}{
		{"ledger 10000, approved withdrawal 12000", 10000, 12000, 0, ErrTrustOverdraw, nil},
		{"ledger 40000, withdrawal 50000", 40000, 50000, 0, ErrTrustOverdraw, nil},
		{"balance drops after authorisation", 10000, 8000, 5000, nil, ErrTrustOverdraw},
		{"exact balance", 10000, 10000, 0, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, tt.balance, tt.amount, false)
			ctx := context.Background()
			err := r.e.Apply(ctx, nil, approved(user("lawyer1")))
			if !errors.Is(err, tt.applyErr) {
				t.Fatalf("apply = %v, want %v", err, tt.applyErr)
			}
			if tt.applyErr != nil {
				if !errors.Is(err, gate.ErrEffectFailed) {
					t.Fatalf("apply = %v, want effect failed", err)
				}
				assertCode(t, err, CodeInsufficientTrustFunds)
				if r.store.w["w1"].Status != StatusRequested || posted(t, r) != tt.balance {
					t.Fatal("failed effect applied")
				}
				return
			}
			if tt.drain > 0 {
				if _, err := r.tf.Disburse(ctx, nil, trust.Disbursement{LedgerID: r.led, WithdrawalID: "w0", ApprovalID: "gate-0",
					TxnDate: execDay, Method: trust.MethodCheque, Counterparty: "X", AmountCents: tt.drain, Purpose: "p", PostedBy: "lawyer2"}); err != nil {
					t.Fatal(err)
				}
			}
			before := posted(t, r)
			_, err = r.e.Execute(ctx, nil, "w1", user("bookkeeper"), execDay)
			if !errors.Is(err, tt.execErr) {
				t.Fatalf("execute = %v, want %v", err, tt.execErr)
			}
			if tt.execErr != nil {
				if !errors.Is(err, gate.ErrEffectFailed) || !errors.Is(err, trust.ErrInsufficientFunds) {
					t.Fatalf("execute = %v, want effect failed with insufficient funds", err)
				}
				assertCode(t, err, CodeInsufficientTrustFunds)
				if posted(t, r) != before || r.store.w["w1"].Status != StatusAuthorized {
					t.Fatal("failed execution posted")
				}
				return
			}
			if posted(t, r) != tt.balance-tt.amount {
				t.Fatalf("posted = %d", posted(t, r))
			}
		})
	}
}

func TestExecuteGuards(t *testing.T) {
	tests := []struct {
		name string
		mod  func(r effectRig)
		want error
	}{
		{"expired authorisation", func(r effectRig) {
			h := r.store.w["w1"]
			h.ExpiresAt = execDay
			r.store.w["w1"] = h
		}, ErrNotAuthorized},
		{"unknown withdrawal", func(r effectRig) { delete(r.store.w, "w1") }, ErrNotFound},
		{"uncleared funds", func(r effectRig) {
			h := r.store.w["w1"]
			h.Request.AmountCents = 15000
			r.store.w["w1"] = h
			if _, err := r.tf.Receive(context.Background(), nil, trust.Receipt{LedgerID: r.led, TxnDate: execDay, Method: trust.MethodCheque,
				Counterparty: "Client", AmountCents: 10000, Purpose: "retainer", PostedBy: "prep"}); err != nil {
				panic(err)
			}
		}, ErrUnclearedFunds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, false)
			if err := r.e.Apply(context.Background(), nil, approved(user("lawyer1"))); err != nil {
				t.Fatal(err)
			}
			tt.mod(r)
			if _, err := r.e.Execute(context.Background(), nil, "w1", user("bookkeeper"), execDay); !errors.Is(err, tt.want) {
				t.Fatalf("execute = %v, want %v", err, tt.want)
			}
			if posted(t, r) < 10000 {
				t.Fatal("refused execution posted")
			}
		})
	}
}

func TestApplyBadPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
	}{
		{"not json", "{"},
		{"no id", "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, false)
			g := approved(user("lawyer1"))
			g.Payload = json.RawMessage(tt.payload)
			if err := r.e.Apply(context.Background(), nil, g); !errors.Is(err, gate.ErrEffectFailed) {
				t.Fatalf("apply = %v, want effect failed", err)
			}
		})
	}
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var ee *EffectError
	if !errors.As(err, &ee) || ee.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}
