package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/trustfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

// memStore is an in-memory EffectStore. It also stands in for the gate's stored payload:
// MarkAuthorized keeps the payload Apply approved, and ApprovedPayload returns it.
type memStore struct {
	w        map[string]Held
	approved map[string]json.RawMessage // gate id -> approved payload
}

func (m *memStore) ApprovedPayload(_ context.Context, _ pgx.Tx, gateID, withdrawalID string) (json.RawMessage, error) {
	p, ok := m.approved[gateID]
	if !ok || m.w[withdrawalID].GateID != gateID {
		return nil, ErrNotFound
	}
	return p, nil
}

func (m *memStore) Lock(_ context.Context, _ pgx.Tx, id string) (Held, error) {
	h, ok := m.w[id]
	if !ok {
		return Held{}, ErrNotFound
	}
	return h, nil
}

func (m *memStore) MarkAuthorized(_ context.Context, _ pgx.Tx, id, by, gateID string, at time.Time) error {
	h := m.w[id]
	h.Status, h.AuthorizedBy, h.GateID, h.AuthorizedAt, h.ExpiresAt = StatusAuthorized, by, gateID, at, at.Add(AuthorizationTTL)
	m.w[id] = h
	if m.approved == nil {
		m.approved = map[string]json.RawMessage{}
	}
	m.approved[gateID], _ = json.Marshal(PayloadFor(h))
	return nil
}

// ReleaseExpiredOnLedger cancels authorisations on ledger expired at now, as PGStore does.
func (m *memStore) ReleaseExpiredOnLedger(_ context.Context, _ pgx.Tx, ledger trust.LedgerID, now time.Time) (int64, error) {
	var n int64
	for id, h := range m.w {
		if h.LedgerID == ledger && h.Status == StatusAuthorized && (h.ExpiresAt.IsZero() || !now.Before(h.ExpiresAt)) {
			h.Status = StatusRejected
			m.w[id] = h
			n++
		}
	}
	return n, nil
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
		Now: func() time.Time { return execDay.Add(9 * time.Hour) }, Approvals: store}
	return effectRig{e: e, tf: tf, store: store, audit: a, led: led}
}

// approved is the gate approval of the rig's withdrawal w1 on its current terms.
func (r effectRig) approved(by registry.Principal) registry.Approved {
	return approvedPayload(termsPayload(r.store.w["w1"]), by)
}

// termsPayload is the gate payload that approves exactly h's terms.
func termsPayload(h Held) json.RawMessage {
	p, _ := json.Marshal(PayloadFor(h))
	return p
}

func approvedPayload(p json.RawMessage, by registry.Principal) registry.Approved {
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
			err := r.e.Apply(ctx, nil, r.approved(tt.approver))
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
			err := r.e.Apply(ctx, nil, r.approved(user("lawyer1")))
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
		}, ErrAuthorizationExpired},
		{"unknown withdrawal", func(r effectRig) { delete(r.store.w, "w1") }, ErrNotFound},
		{"no expiry recorded", func(r effectRig) {
			h := r.store.w["w1"]
			h.ExpiresAt = time.Time{}
			r.store.w["w1"] = h
		}, ErrAuthorizationExpired},
		{"amount changed after approval", func(r effectRig) {
			h := r.store.w["w1"]
			h.Request.AmountCents = 3000
			r.store.w["w1"] = h
		}, ErrTermsMismatch},
		{"payee changed after approval", func(r effectRig) {
			h := r.store.w["w1"]
			h.Request.Payee = "Someone Else Ltd"
			r.store.w["w1"] = h
		}, ErrTermsMismatch},
		{"method changed after approval", func(r effectRig) {
			h := r.store.w["w1"]
			h.Request.Method = MethodEFT
			r.store.w["w1"] = h
		}, ErrTermsMismatch},
		{"no approved payload stored", func(r effectRig) { delete(r.store.approved, "gate-1") }, ErrTermsMismatch},
		{"no approved payload source", func(r effectRig) { r.e.Approvals = nil }, ErrTermsMismatch},
		{"uncleared funds", func(r effectRig) {
			// The gate approved 15000 (its stored payload says so); Disburse finds it uncleared.
			h := r.store.w["w1"]
			h.Request.AmountCents = 15000
			r.store.w["w1"] = h
			r.store.approved[h.GateID] = termsPayload(h)
			if _, err := r.tf.Receive(context.Background(), nil, trust.Receipt{LedgerID: r.led, TxnDate: execDay, Method: trust.MethodCheque,
				Counterparty: "Client", AmountCents: 10000, Purpose: "retainer", PostedBy: "prep"}); err != nil {
				panic(err)
			}
		}, ErrUnclearedFunds},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, false)
			if err := r.e.Apply(context.Background(), nil, r.approved(user("lawyer1"))); err != nil {
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
			g := r.approved(user("lawyer1"))
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

// rowLockDB is a store fake that models Postgres row locks: a lock taken in a transaction is
// held until that transaction commits or rolls back, and writes are visible to other
// transactions only after commit. It serves both EffectStore and TrustLedgers, so Apply's
// withdrawal lock, ledger lock and hold all go through it.
type rowLockDB struct {
	tf    *trustfake.Fake
	mu    sync.Mutex             // guards locks and w
	locks map[string]*sync.Mutex // row locks by "table:id"
	w     map[string]Held        // committed withdrawals
}

// lockTx is one fake transaction. Apply only passes it through as a pgx.Tx; its pgx methods
// are never called.
type lockTx struct {
	pgx.Tx
	held   []string
	staged map[string]Held
}

func (d *rowLockDB) begin() *lockTx { return &lockTx{staged: map[string]Held{}} }

// lock takes the row lock for key in tx, waiting while another transaction holds it.
func (d *rowLockDB) lock(tx pgx.Tx, key string) *lockTx {
	t := tx.(*lockTx)
	if slices.Contains(t.held, key) {
		return t
	}
	d.mu.Lock()
	m, ok := d.locks[key]
	if !ok {
		m = &sync.Mutex{}
		d.locks[key] = m
	}
	d.mu.Unlock()
	m.Lock()
	t.held = append(t.held, key)
	return t
}

// end commits (or rolls back) tx and releases its locks.
func (d *rowLockDB) end(t *lockTx, commit bool) {
	d.mu.Lock()
	if commit {
		maps.Copy(d.w, t.staged)
	}
	locks := make([]*sync.Mutex, 0, len(t.held))
	for _, k := range t.held {
		locks = append(locks, d.locks[k])
	}
	d.mu.Unlock()
	for _, m := range slices.Backward(locks) {
		m.Unlock()
	}
}

// read returns the withdrawal as tx sees it: its own staged write, else the committed row.
func (d *rowLockDB) read(t *lockTx, id string) (Held, bool) {
	if h, ok := t.staged[id]; ok {
		return h, true
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	h, ok := d.w[id]
	return h, ok
}

type rowLockStore struct{ *rowLockDB }

func (s rowLockStore) Lock(_ context.Context, tx pgx.Tx, id string) (Held, error) {
	h, ok := s.read(s.lock(tx, "withdrawal:"+id), id)
	if !ok {
		return Held{}, ErrNotFound
	}
	return h, nil
}

func (s rowLockStore) MarkAuthorized(_ context.Context, tx pgx.Tx, id, by, gateID string, at time.Time) error {
	t := s.lock(tx, "withdrawal:"+id)
	h, _ := s.read(t, id)
	h.Status, h.AuthorizedBy, h.GateID, h.AuthorizedAt = StatusAuthorized, by, gateID, at
	t.staged[id] = h
	return nil
}

func (s rowLockStore) MarkExecuted(_ context.Context, tx pgx.Tx, id, by string, on time.Time) error {
	t := s.lock(tx, "withdrawal:"+id)
	h, _ := s.read(t, id)
	h.Status, h.ExecutedBy, h.ExecutedOn = StatusExecuted, by, on
	t.staged[id] = h
	return nil
}

type rowLockLedgers struct{ *rowLockDB }

// LockLedger takes the ledger row lock, then reads the balance and the authorised holds the
// way sumsSQL does: committed rows plus this transaction's own writes.
func (l rowLockLedgers) LockLedger(ctx context.Context, tx pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error) {
	t := l.lock(tx, "ledger:"+string(id))
	runtime.Gosched() // widen the window a missing lock would leave open
	b, _, err := l.tf.LockLedger(ctx, tx, id)
	if err != nil {
		return trust.Balance{}, 0, err
	}
	l.mu.Lock()
	ids := slices.Collect(maps.Keys(l.w))
	l.mu.Unlock()
	ids = append(ids, slices.Collect(maps.Keys(t.staged))...)
	slices.Sort(ids)
	var held int64
	for _, w := range slices.Compact(ids) {
		if h, _ := l.read(t, w); h.LedgerID == id && h.Status == StatusAuthorized {
			held += h.Request.AmountCents
		}
	}
	b.AvailableCents = trust.AvailableCents(b.ClearedCents, b.ProtectedCents, b.MinimumCents, held)
	return b, held, nil
}

func (l rowLockLedgers) Disburse(ctx context.Context, tx pgx.Tx, d trust.Disbursement) (trust.TxnID, error) {
	l.lock(tx, "ledger:"+string(d.LedgerID))
	return l.tf.Disburse(ctx, tx, d)
}

// rule: ARC-I-024
// rule: ARC-I-025
// Two authorisations race on one ledger. Apply runs in each caller's tx and takes the ledger
// row lock, so the second waits for the first to commit its hold and then sees less available.
func TestARC_I_025_ConcurrentApplyLocksLedger(t *testing.T) {
	cases := []struct {
		name               string
		balance, each      int64
		n                  int
		wantOK, wantRefuse int
	}{
		{name: "two of 300000 on 500000", balance: 500000, each: 300000, n: 2, wantOK: 1, wantRefuse: 1},
		{name: "two of 300000 on 600000", balance: 600000, each: 300000, n: 2, wantOK: 2, wantRefuse: 0},
		{name: "five of 100000 on 250000", balance: 250000, each: 100000, n: 5, wantOK: 2, wantRefuse: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for range 50 { // repeat: one lucky interleaving proves nothing
				rig := newEffectRig(t, tc.balance, tc.each, false)
				db := &rowLockDB{tf: rig.tf, locks: map[string]*sync.Mutex{}, w: map[string]Held{}}
				ids := make([]string, tc.n)
				for i := range ids {
					h := rig.store.w["w1"]
					h.ID = fmt.Sprintf("w%d", i+1)
					ids[i], db.w[h.ID] = h.ID, h
				}
				rig.e.Store, rig.e.Ledgers = rowLockStore{db}, rowLockLedgers{db}
				start := make(chan struct{})
				errs := make([]error, tc.n)
				var wg sync.WaitGroup
				gates := make([]registry.Approved, tc.n)
				for i, id := range ids {
					gates[i] = approvedPayload(termsPayload(db.w[id]), user("lawyer1"))
				}
				for i := range ids {
					wg.Go(func() {
						g := gates[i]
						tx := db.begin()
						<-start
						errs[i] = rig.e.Apply(context.Background(), tx, g)
						db.end(tx, errs[i] == nil)
					})
				}
				close(start)
				wg.Wait()
				var ok, refused int
				for _, err := range errs {
					switch {
					case err == nil:
						ok++
					case errors.Is(err, ErrTrustOverdraw):
						refused++
						assertCode(t, err, CodeInsufficientTrustFunds)
					default:
						t.Fatalf("apply: %v", err)
					}
				}
				if ok != tc.wantOK || refused != tc.wantRefuse {
					t.Fatalf("ok = %d, refused = %d; want %d, %d", ok, refused, tc.wantOK, tc.wantRefuse)
				}
				var held int64
				for _, h := range db.w {
					if h.Status == StatusAuthorized {
						held += h.Request.AmountCents
					}
				}
				if held > tc.balance {
					t.Fatalf("authorised holds %d exceed the ledger's %d", held, tc.balance)
				}
			}
		})
	}
}

// rule: ARC-I-019
// The approved gate binds amount, payee, method and matter: Apply refuses a payload that does
// not state the withdrawal's terms exactly, and authorises nothing.
func TestARC_I_019_ApplyBindsApprovedTerms(t *testing.T) {
	amount := func(v int64) *int64 { return &v }
	tests := []struct {
		name string
		mod  func(p *EffectPayload)
		want error
	}{
		{"exact terms authorise", func(*EffectPayload) {}, nil},
		{"approved amount differs", func(p *EffectPayload) { p.AmountCents = amount(4001) }, ErrTermsMismatch},
		{"approved amount missing", func(p *EffectPayload) { p.AmountCents = nil }, ErrTermsMismatch},
		{"approved payee differs", func(p *EffectPayload) { p.Payee = "Other Payee" }, ErrTermsMismatch},
		{"approved payee missing", func(p *EffectPayload) { p.Payee = "" }, ErrTermsMismatch},
		{"approved method differs", func(p *EffectPayload) { p.Method = MethodEFT }, ErrTermsMismatch},
		{"approved method missing", func(p *EffectPayload) { p.Method = "" }, ErrTermsMismatch},
		{"approved matter differs", func(p *EffectPayload) { p.MatterID = "M2" }, ErrTermsMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 4000, false)
			p := PayloadFor(r.store.w["w1"])
			tt.mod(&p)
			raw, _ := json.Marshal(p)
			err := r.e.Apply(context.Background(), nil, approvedPayload(raw, user("lawyer1")))
			if !errors.Is(err, tt.want) {
				t.Fatalf("apply = %v, want %v", err, tt.want)
			}
			if tt.want == nil {
				if r.store.w["w1"].Status != StatusAuthorized || !r.store.w["w1"].ExpiresAt.Equal(execDay.Add(9*time.Hour+AuthorizationTTL)) {
					t.Fatalf("authorised row = %+v, want authorized expiring after %v", r.store.w["w1"].Withdrawal, AuthorizationTTL)
				}
				return
			}
			if !errors.Is(err, ErrNotAuthorized) || !errors.Is(err, gate.ErrEffectFailed) {
				t.Fatalf("apply = %v, want a failed effect matching ErrNotAuthorized", err)
			}
			if r.store.w["w1"].Status != StatusRequested {
				t.Fatalf("status = %s after refused apply", r.store.w["w1"].Status)
			}
		})
	}
}

// rule: ARC-I-025
// An expired authorisation's hold is released: the next authorisation on the ledger sees the
// money again, and the expired withdrawal can no longer execute.
func TestARC_I_025_ExpiredHoldReleased(t *testing.T) {
	tests := []struct {
		name        string
		expiresAt   time.Time
		wantErr     error
		wantW1Still Status
	}{
		{"expired hold released", execDay, nil, StatusRejected},
		{"hold with no expiry released", time.Time{}, nil, StatusRejected},
		{"live hold kept", execDay.Add(48 * time.Hour), ErrTrustOverdraw, StatusAuthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newEffectRig(t, 10000, 8000, false)
			w1 := r.store.w["w1"]
			w1.Status, w1.GateID, w1.AuthorizedBy, w1.ExpiresAt = StatusAuthorized, "gate-0", "lawyer1", tt.expiresAt
			r.store.w["w1"] = w1
			w2 := w1
			w2.ID, w2.Status, w2.GateID, w2.AuthorizedBy, w2.ExpiresAt = "w2", StatusRequested, "", "", time.Time{}
			r.store.w["w2"] = w2
			r.e.Ledgers = holdingLedgers{TrustLedgers: r.e.Ledgers, store: &lockedStore{memStore: r.store}}
			err := r.e.Apply(context.Background(), nil, approvedPayload(termsPayload(w2), user("lawyer1")))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("apply w2 = %v, want %v", err, tt.wantErr)
			}
			if got := r.store.w["w1"].Status; got != tt.wantW1Still {
				t.Fatalf("w1 status = %s, want %s", got, tt.wantW1Still)
			}
		})
	}
}
