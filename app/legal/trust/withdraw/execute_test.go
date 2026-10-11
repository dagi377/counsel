package withdraw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// directTx runs fn with no real transaction; the fakes ignore tx.
type directTx struct{}

func (directTx) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

type bankLog struct{ matches []BankMatch }

func (b *bankLog) Match(_ context.Context, _ pgx.Tx, m BankMatch) error {
	b.matches = append(b.matches, m)
	return nil
}

func newExecution(r effectRig, bank *bankLog) *Execution {
	return &Execution{DB: directTx{}, Effect: r.e, Bank: bank, Problems: &server.Problems{},
		Principal: func(req *http.Request) (registry.Principal, bool) {
			id := req.Header.Get("X-Test-User")
			if id == "" {
				return registry.Principal{}, false
			}
			if id == "agent" {
				return registry.Principal{Kind: registry.PrincipalAgent, UserID: "lawyer2"}, true
			}
			return user(id), true
		}}
}

// rule: ARC-I-012
func TestARC_I_012_RouteTableHasOnlyExecutionWrite(t *testing.T) {
	r := chi.NewRouter()
	(&Execution{}).Routes(r)
	var got []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	want := []string{http.MethodPost + " " + ExecutionPath}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("routes = %v, want %v", got, want)
	}
}

// rule: ARC-I-012
// rule: ARC-I-027
func TestARC_I_012_ExecutionOnlyAfterAuthorization(t *testing.T) {
	const balance, amount = 500000, 120000
	cases := []struct {
		name       string
		authorize  string // signatory who approves first; "" leaves the withdrawal requested
		expired    bool
		flagOff    bool
		executor   string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "not authorized", executor: "lawyer2", wantStatus: http.StatusConflict, wantCode: CodeNotAuthorized},
		{name: "executor is authorizer", authorize: "lawyer1", executor: "lawyer1", wantStatus: http.StatusConflict, wantCode: CodeNotAuthorized},
		{name: "authorization expired", authorize: "lawyer1", expired: true, executor: "lawyer2", wantStatus: http.StatusConflict, wantCode: CodeNotAuthorized},
		{name: "agent executes", authorize: "lawyer1", executor: "agent", wantStatus: http.StatusConflict, wantCode: CodeNotAuthorized},
		{name: "flag off fails closed", authorize: "lawyer1", flagOff: true, executor: "lawyer2", wantStatus: http.StatusConflict, wantCode: CodeUnverified},
		{name: "unauthenticated", authorize: "lawyer1", executor: "", wantStatus: http.StatusUnauthorized, wantCode: "unauthenticated"},
		{name: "missing bank ref", authorize: "lawyer1", executor: "lawyer2", body: `{"executed_on":"2026-03-16"}`, wantStatus: http.StatusUnprocessableEntity, wantCode: server.CodeValidationFailed},
		{name: "bad date", authorize: "lawyer1", executor: "lawyer2", body: `{"executed_on":"16/03/2026","bank_ref":"B1"}`, wantStatus: http.StatusBadRequest, wantCode: server.CodeInvalidDate},
		{name: "distinct executor posts", authorize: "lawyer1", executor: "lawyer2", wantStatus: http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newEffectRig(t, balance, amount, false)
			if tc.authorize != "" {
				if err := rig.e.Apply(context.Background(), nil, approved(user(tc.authorize))); err != nil {
					t.Fatalf("authorize: %v", err)
				}
			}
			if tc.expired {
				h := rig.store.w["w1"]
				h.ExpiresAt = execDay
				rig.store.w["w1"] = h
			}
			bank := &bankLog{}
			r := chi.NewRouter()
			newExecution(rig, bank).Routes(r)
			body := tc.body
			if body == "" {
				body = `{"executed_on":"2026-03-16","bank_ref":"BANK-77","instrument_no":"1042"}`
			}
			req := httptest.NewRequest(http.MethodPost, "/trust/withdrawals/w1/execution", strings.NewReader(body))
			ctx := context.Background()
			if !tc.flagOff {
				ctx = verifiedCtx(t)
			}
			req = req.WithContext(ctx)
			if tc.executor != "" {
				req.Header.Set("X-Test-User", tc.executor)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body)
			}
			if tc.wantStatus != http.StatusCreated {
				var p map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &p)
				if p["code"] != tc.wantCode {
					t.Fatalf("code = %v, want %s", p["code"], tc.wantCode)
				}
				if got := posted(t, rig); got != balance {
					t.Fatalf("posted = %d, want %d (nothing posted)", got, balance)
				}
				if len(bank.matches) != 0 {
					t.Fatalf("bank matched %d lines, want 0", len(bank.matches))
				}
				return
			}
			if got := posted(t, rig); got != balance-amount {
				t.Fatalf("posted = %d, want %d", got, balance-amount)
			}
			h := rig.store.w["w1"]
			if h.Status != StatusExecuted || h.ExecutedBy != "lawyer2" || !h.ExecutedOn.Equal(execDay) {
				t.Fatalf("withdrawal = %+v, want executed by lawyer2 on %s", h.Withdrawal, execDay)
			}
			if len(bank.matches) != 1 || bank.matches[0].BankRef != "BANK-77" || bank.matches[0].TxnID == "" {
				t.Fatalf("bank matches = %+v, want one match of BANK-77 to the posted txn", bank.matches)
			}
			txns, err := rig.tf.Txns(context.Background(), rig.led)
			if err != nil {
				t.Fatal(err)
			}
			last := txns[len(txns)-1]
			if last.ID != bank.matches[0].TxnID || !last.TxnDate.Equal(execDay) || last.AmountCents != -amount {
				t.Fatalf("posted txn = %+v, want -%d dated %s", last, amount, execDay)
			}
		})
	}
}

// holdingLedgers is the trust ledger as the database computes it: available subtracts
// authorised-not-executed withdrawals. mu serialises Apply the way the row lock does.
type holdingLedgers struct {
	TrustLedgers
	store *lockedStore
}

func (h holdingLedgers) LockLedger(ctx context.Context, tx pgx.Tx, id trust.LedgerID) (trust.Balance, int64, error) {
	b, _, err := h.TrustLedgers.LockLedger(ctx, tx, id)
	if err != nil {
		return trust.Balance{}, 0, err
	}
	var held int64
	for _, w := range h.store.w {
		if w.LedgerID == id && w.Status == StatusAuthorized {
			held += w.Request.AmountCents
		}
	}
	b.AvailableCents = trust.AvailableCents(b.ClearedCents, b.ProtectedCents, b.MinimumCents, held)
	return b, held, nil
}

type lockedStore struct {
	*memStore
	mu sync.Mutex
}

// rule: ARC-I-025
func TestARC_I_025_ConcurrentAuthorizationsHoldFunds(t *testing.T) {
	cases := []struct {
		name               string
		balance, each      int64
		wantOK, wantRefuse int
	}{
		{name: "second overdraws", balance: 500000, each: 300000, wantOK: 1, wantRefuse: 1},
		{name: "both fit", balance: 600000, each: 300000, wantOK: 2, wantRefuse: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rig := newEffectRig(t, tc.balance, tc.each, false)
			w2 := rig.store.w["w1"]
			w2.ID = "w2"
			rig.store.w["w2"] = w2
			ls := &lockedStore{memStore: rig.store}
			rig.e.Ledgers = holdingLedgers{TrustLedgers: rig.e.Ledgers, store: ls}
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i, id := range []string{"w1", "w2"} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					p, _ := json.Marshal(EffectPayload{WithdrawalID: id})
					g := approved(user("lawyer1"))
					g.Payload = p
					ls.mu.Lock()
					defer ls.mu.Unlock()
					errs[i] = rig.e.Apply(context.Background(), nil, g)
				}()
			}
			wg.Wait()
			var ok, refused int
			for _, err := range errs {
				switch {
				case err == nil:
					ok++
				case errors.Is(err, ErrTrustOverdraw):
					refused++
					if p := problemFor(err); p.Status != http.StatusConflict || p.Code != CodeTrustOverdraw {
						t.Fatalf("problem = %d %s, want 409 %s", p.Status, p.Code, CodeTrustOverdraw)
					}
				default:
					t.Fatalf("apply: %v", err)
				}
			}
			if ok != tc.wantOK || refused != tc.wantRefuse {
				t.Fatalf("ok = %d, refused = %d; want %d, %d", ok, refused, tc.wantOK, tc.wantRefuse)
			}
		})
	}
}

func TestExecutionRecordFailsClosedWhenUnverified(t *testing.T) {
	rig := newEffectRig(t, 500000, 1000, false)
	x := newExecution(rig, &bankLog{})
	_, err := x.Record(context.Background(), "w1", user("lawyer2"), Confirmation{ExecutedOn: execDay, BankRef: "B"})
	if !errors.Is(err, ErrUnverified) {
		t.Fatalf("err = %v, want ErrUnverified", err)
	}
}
