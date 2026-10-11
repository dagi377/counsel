package withdraw_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	matterA      = "0190a000-0000-7000-8000-00000000000a"
	matterHidden = "0190a000-0000-7000-8000-0000000000ff"
	accountA     = "0190a000-0000-7000-8000-0000000000aa"
	ledgerA      = "0190c000-0000-7000-8000-000000000001"
	ledgerHidden = "0190c000-0000-7000-8000-000000000002"
	ledgerMiss   = "0190c000-0000-7000-8000-0000000000ee"
	wdOpen       = "0190d000-0000-7000-8000-000000000001"
	wdExecuted   = "0190d000-0000-7000-8000-000000000002"
	reqHidden    = "0190d000-0000-7000-8000-000000000003"
	billA        = "0190b000-0000-7000-8000-000000000001"
)

// svc is a withdraw.Service fake that counts every call and returns uuid ids.
type svc struct {
	mu    sync.Mutex
	calls map[string]int
	byID  map[string]withdraw.Withdrawal
	err   error
}

func (s *svc) count(m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[m]++
}

func (s *svc) total() int {
	n := 0
	for _, c := range s.calls {
		n += c
	}
	return n
}

func (s *svc) Request(_ context.Context, r withdraw.Request) (withdraw.Withdrawal, error) {
	s.count("Request")
	if s.err != nil {
		return withdraw.Withdrawal{}, s.err
	}
	w := withdraw.Withdrawal{ID: "0190d000-0000-7000-8000-000000000100", Request: r, Status: withdraw.StatusRequested}
	s.byID[w.ID] = w
	return w, nil
}

// Authorize is effect-owned: a route that calls it sets gate-owned state.
func (s *svc) Authorize(context.Context, string, string, time.Time) (withdraw.Withdrawal, error) {
	s.count("Authorize")
	return withdraw.Withdrawal{}, errors.New("effect-owned")
}

func (s *svc) RecordExecution(context.Context, string, string, time.Time) (withdraw.Withdrawal, error) {
	s.count("RecordExecution")
	return withdraw.Withdrawal{}, errors.New("route-owned elsewhere")
}

func (s *svc) Get(_ context.Context, id string) (withdraw.Withdrawal, error) {
	s.count("Get")
	w, ok := s.byID[id]
	if !ok {
		return withdraw.Withdrawal{}, withdraw.ErrNotFound
	}
	return w, nil
}

func (s *svc) Available(context.Context, string, string) (int64, error) {
	s.count("Available")
	return 0, nil
}

type ledgers map[string]trust.Ledger

func (l ledgers) Ledger(_ context.Context, id trust.LedgerID) (trust.Ledger, error) {
	got, ok := l[string(id)]
	if !ok {
		return trust.Ledger{}, trust.ErrNotFound
	}
	return got, nil
}

// cancels is the Cancels fake: it keeps the gate each request stored.
type cancels struct {
	cancelled []string
	gates     map[string]string
}

func (c *cancels) Row(_ context.Context, id string) (withdraw.Row, error) {
	return withdraw.Row{Version: 1, LedgerID: ledgerA, GateID: c.gates[id]}, nil
}

func (c *cancels) RecordGate(_ context.Context, _ pgx.Tx, id, gateID string) error {
	if c.gates[id] != "" {
		return withdraw.ErrNotFound
	}
	c.gates[id] = gateID
	return nil
}

func (c *cancels) Cancel(_ context.Context, _ pgx.Tx, id string, _ registry.Principal) (int64, error) {
	c.cancelled = append(c.cancelled, id)
	return 2, nil
}

// countingGates counts calls on the gate engine fake and hands out uuid gate ids, as the
// Postgres engine does (the fake numbers them).
type countingGates struct {
	*gatefake.Fake
	opens, withdraws int
	kinds            []string
	ids              map[gate.GateID]gate.GateID
}

func (g *countingGates) Open(ctx context.Context, tx pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.opens++
	g.kinds = append(g.kinds, kind)
	id, err := g.Fake.Open(ctx, tx, kind, s, payload, p)
	out := gate.GateID("0190f000-0000-7000-8000-00000000010" + string(rune('0'+len(g.ids))))
	g.ids[out] = id
	return out, err
}

func (g *countingGates) Withdraw(ctx context.Context, tx pgx.Tx, id gate.GateID, by gate.Principal) error {
	g.withdraws++
	return g.Fake.Withdraw(ctx, tx, g.ids[id], by)
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

type reqStubAccess map[string][]string

func (a reqStubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == matterHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a[withdraw.PreparerOf(p)] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

// The "" entry lets an agent with no acting user past access, so the routes' own preparer
// check is what refuses it.
var testAccess = reqStubAccess{"": {withdraw.CapRequest}, "alice": {withdraw.CapRequest}, "bob": {withdraw.CapRequest}, "viewer": {"trust.view"}}

// testPrincipal is X-Test-User; with X-Test-Agent it is that agent run acting for the user
// ("none" for no acting user).
func testPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get("X-Test-User")
	if run := r.Header.Get("X-Test-Agent"); run != "" {
		if u == "none" {
			u = ""
		}
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: run, ActingFor: u}, true
	}
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
}

type rig struct {
	h       http.Handler
	svc     *svc
	gates   *countingGates
	cancels *cancels
	openGID gate.GateID
}

func newReqRig(t *testing.T, svcErr error) rig {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	eng := gatefake.New(now, gatefake.Caps{})
	for _, kind := range []string{withdraw.GateKind, withdraw.GateFeeKind} {
		if err := eng.Register(gate.GateKind{Slug: kind, Approver: "trust.approve", Risk: gate.RiskHigh, MinApprovers: 1,
			TTL: 168 * time.Hour, Effect: func(context.Context, pgx.Tx, registry.Approved) error { return nil }}); err != nil {
			t.Fatal(err)
		}
	}
	g := &countingGates{Fake: eng, ids: map[gate.GateID]gate.GateID{}}
	gid, err := g.Open(context.Background(), nil, withdraw.GateKind, gate.Subject{Kind: withdraw.SubjectWithdrawal, ID: wdOpen},
		json.RawMessage(`{}`), registry.Principal{Kind: registry.PrincipalUser, UserID: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	s := &svc{calls: map[string]int{}, err: svcErr, byID: map[string]withdraw.Withdrawal{
		wdOpen: {ID: wdOpen, Status: withdraw.StatusRequested, GateID: string(gid),
			Request: withdraw.Request{MatterID: matterA, AmountCents: 500, Payee: "Payee", Method: withdraw.MethodCheque, Reason: "refund", PreparedBy: "alice"}},
		wdExecuted: {ID: wdExecuted, Status: withdraw.StatusExecuted,
			Request: withdraw.Request{MatterID: matterA, AmountCents: 500, Payee: "Payee", Method: withdraw.MethodCheque, PreparedBy: "alice"}},
		reqHidden: {ID: reqHidden, Status: withdraw.StatusRequested, Request: withdraw.Request{MatterID: matterHidden}},
	}}
	g.opens, g.kinds = 0, nil
	c := &cancels{gates: map[string]string{wdOpen: string(gid)}}
	h := &withdraw.RequestHTTP{Withdrawals: s, Gates: g, Cancels: c, DB: txStub{}, Access: testAccess,
		Ledgers: ledgers{ledgerA: {ID: ledgerA, AccountID: accountA, MatterID: matterA},
			ledgerHidden: {ID: ledgerHidden, AccountID: accountA, MatterID: matterHidden}},
		Idem: idemfake.New(), Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return rig{h: m.Handler(), svc: s, gates: g, cancels: c, openGID: gid}
}

func reqDo(h http.Handler, path, user, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(body)))
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

var reqOpenAPISchemas = sync.OnceValues(func() (*jsonschema.Compiler, error) {
	raw, err := os.ReadFile("../../../../api/openapi.yaml")
	if err != nil {
		return nil, err
	}
	var doc any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	js, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	if err := c.AddResource("openapi.json", bytes.NewReader(js)); err != nil {
		return nil, err
	}
	return c, nil
})

func reqValidate(t *testing.T, ref string, body []byte) {
	t.Helper()
	c, err := reqOpenAPISchemas()
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile("openapi.json" + ref)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if err := s.Validate(v); err != nil {
		t.Errorf("body does not validate against %s: %v", ref, err)
	}
}

func reqCheckProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, api.ProblemContentType)
	}
	reqValidate(t, "#/components/schemas/Problem", rec.Body.Bytes())
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code {
		t.Errorf("code = %q, want %q", p.Code, code)
	}
}

const (
	createPath = "/v1/trust/withdrawals"
	wrSchema   = "#/components/schemas/WithdrawalRequest"
	goodBody   = `{"ledger_id":"` + ledgerA + `","amount_cents":500,"purpose_category":"refund","payee":"Client","method":"cheque"}`
)

func cancelPath(id string) string { return "/v1/trust/withdrawals/" + id + "/cancel" }

func key(n string) map[string]string {
	return map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000" + n}
}

func keyMatch(n, v string) map[string]string {
	h := key(n)
	h["If-Match"] = v
	return h
}

func TestARC_I_051_WithdrawalRequestProblems(t *testing.T) {
	hiddenBody := `{"ledger_id":"` + ledgerHidden + `","amount_cents":500,"purpose_category":"refund","payee":"Client","method":"cheque"}`
	missBody := `{"ledger_id":"` + ledgerMiss + `","amount_cents":500,"purpose_category":"refund","payee":"Client","method":"cheque"}`
	tests := []struct {
		name      string
		path      string
		user      string
		body      string
		hdr       map[string]string
		svcErr    error
		status    int
		code      string
		zeroCalls bool
	}{
		{"request without principal", createPath, "", goodBody, key("101"), nil, 401, withdraw.CodeUnauthenticated, true},
		{"cancel without principal", cancelPath(wdOpen), "", "", keyMatch("102", `"1"`), nil, 401, withdraw.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"request without trust.request", createPath, "viewer", goodBody, key("103"), nil, 403, withdraw.CodeMissingCapability, true},
		{"cancel without trust.request", cancelPath(wdOpen), "viewer", "", keyMatch("104", `"1"`), nil, 403, withdraw.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"request on walled ledger is 404", createPath, "alice", hiddenBody, key("105"), nil, 404, withdraw.CodeNotFound, true},
		{"request on missing ledger is 404", createPath, "alice", missBody, key("106"), nil, 404, withdraw.CodeNotFound, true},
		{"cancel walled withdrawal is 404", cancelPath(reqHidden), "alice", "", keyMatch("107", `"1"`), nil, 404, withdraw.CodeNotFound, false},
		{"unknown body field", createPath, "alice", `{"ledger_id":"` + ledgerA + `","amount_cents":500,"purpose_category":"refund","payee":"C","method":"cheque","status":"authorized"}`, key("108"), nil, 400, server.CodeValidationFailed, true},
		{"malformed ledger id", createPath, "alice", `{"ledger_id":"nope","amount_cents":500,"purpose_category":"refund","payee":"C","method":"cheque"}`, key("109"), nil, 400, server.CodeValidationFailed, true},
		{"cancel malformed id", cancelPath("nope"), "alice", "", keyMatch("110", `"1"`), nil, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-006
		{"cancel missing If-Match", cancelPath(wdOpen), "alice", "", key("111"), nil, 428, server.CodePreconditionRequired, true},
		{"cancel stale version", cancelPath(wdOpen), "alice", "", keyMatch("112", `"9"`), nil, 412, server.CodeVersionConflict, false},
		// rule: ARC-I-007
		{"request missing idempotency key", createPath, "alice", goodBody, nil, nil, 400, idem.CodeKeyRequired, true},
		{"cancel missing idempotency key", cancelPath(wdOpen), "alice", "", map[string]string{"If-Match": `"1"`}, nil, 400, idem.CodeKeyRequired, true},
		{"cancel executed withdrawal", cancelPath(wdExecuted), "alice", "", keyMatch("113", `"1"`), nil, 409, withdraw.CodeAlreadyExecuted, false},
		// rule: ARC-I-025
		{"overdraw", createPath, "alice", goodBody, key("114"), withdraw.ErrTrustOverdraw, 409, withdraw.CodeTrustOverdraw, false},
		{"rules unverified", createPath, "alice", goodBody, key("115"), withdraw.ErrUnverified, 409, withdraw.CodeUnverified, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReqRig(t, tc.svcErr)
			rec := reqDo(r.h, tc.path, tc.user, tc.body, tc.hdr)
			reqCheckProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.svc.total() != 0 {
				t.Errorf("service calls = %v, want 0", r.svc.calls)
			}
			if r.gates.opens != 0 || r.gates.withdraws != 0 || len(r.cancels.cancelled) != 0 {
				t.Errorf("gate opens = %d withdraws = %d cancels = %v, want none", r.gates.opens, r.gates.withdraws, r.cancels.cancelled)
			}
		})
	}
}

// rule: ARC-I-012
func TestARC_I_012_WithdrawalRequestOpensGateOnly(t *testing.T) {
	tests := []struct {
		name string
		body string
		kind string
	}{
		{"withdrawal opens trust.withdrawal", goodBody, withdraw.GateKind},
		{"bill_id opens the fee gate", `{"ledger_id":"` + ledgerA + `","amount_cents":500,"purpose_category":"fees","payee":"Firm","method":"eft","bill_id":"` + billA + `","direction_document_id":"` + billA + `"}`, withdraw.GateFeeKind},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReqRig(t, nil)
			rec := reqDo(r.h, createPath, "alice", tc.body, key("201"))
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			reqValidate(t, wrSchema, rec.Body.Bytes())
			var got struct {
				Status string `json:"status"`
				GateID string `json:"gate_id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != "awaiting_authorization" || got.GateID == "" {
				t.Errorf("body = %+v, want awaiting_authorization with a gate", got)
			}
			if r.gates.opens != 1 || r.gates.kinds[0] != tc.kind {
				t.Errorf("gates = %v, want one %s", r.gates.kinds, tc.kind)
			}
			// The request never authorizes: Authorize is the gate effect.
			if r.svc.calls["Authorize"] != 0 || r.svc.calls["RecordExecution"] != 0 {
				t.Errorf("effect-owned calls = %v", r.svc.calls)
			}
		})
	}
}

func TestWithdrawalCancelWithdrawsGate(t *testing.T) {
	r := newReqRig(t, nil)
	rec := reqDo(r.h, cancelPath(wdOpen), "alice", "", keyMatch("301", `"1"`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	reqValidate(t, wrSchema, rec.Body.Bytes())
	g, err := r.gates.Fake.Get(context.Background(), r.gates.ids[r.openGID])
	if err != nil {
		t.Fatal(err)
	}
	if g.Status != gate.StatusWithdrawn || len(r.cancels.cancelled) != 1 {
		t.Errorf("gate = %s cancels = %v, want withdrawn and one cancel", g.Status, r.cancels.cancelled)
	}
	if r.svc.calls["Authorize"] != 0 {
		t.Errorf("Authorize called")
	}
}

func TestARC_I_008_WithdrawalRequestIdempotency(t *testing.T) {
	r := newReqRig(t, nil)
	hdr := key("401")
	first := reqDo(r.h, createPath, "alice", goodBody, hdr)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", goodBody, http.StatusAccepted, ""},
		// rule: ARC-I-008
		// A reused key with a different body answers 422, as kernel/idem does.
		{"same key, different body", `{"ledger_id":"` + ledgerA + `","amount_cents":600,"purpose_category":"refund","payee":"Client","method":"cheque"}`, http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := reqDo(r.h, createPath, "alice", tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" && rec.Body.String() != first.Body.String() {
				t.Errorf("replay = %s, want %s", rec.Body, first.Body)
			}
		})
	}
	if r.gates.opens != 1 || r.svc.calls["Request"] != 1 {
		t.Errorf("opens = %d requests = %d, want 1 and 1", r.gates.opens, r.svc.calls["Request"])
	}
}

func TestARC_I_012_WithdrawalRequestWritesDeclared(t *testing.T) {
	writes, err := server.DeclaredWrites(newReqRig(t, nil).h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("writes = %+v, want request and cancel", writes)
	}
	for _, w := range writes {
		for _, k := range w.Sets {
			if k == "withdrawal.status=authorized" {
				t.Errorf("%s declares gate-owned %s", w.Pattern, k)
			}
		}
	}
}

// rule: ARC-I-027
// The preparer of a withdrawal is the acting user: the user, or the user an agent run acts
// for; never the agent and never empty. The gate the request opens is stored on it.
func TestARC_I_027_WithdrawalPreparerIsActingUser(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		agent    string
		status   int
		preparer string
	}{
		{name: "user prepares", user: "alice", status: http.StatusAccepted, preparer: "alice"},
		{name: "agent run prepares for its acting user", user: "alice", agent: "run-1", status: http.StatusAccepted, preparer: "alice"},
		{name: "agent run with no acting user refused", user: "none", agent: "run-1", status: http.StatusForbidden},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReqRig(t, nil)
			hdr := key(fmt.Sprintf("27%d", i))
			if tc.agent != "" {
				hdr["X-Test-Agent"] = tc.agent
			}
			rec := reqDo(r.h, createPath, tc.user, goodBody, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body, tc.status)
			}
			if tc.status != http.StatusAccepted {
				if r.svc.calls["Request"] != 0 || r.gates.opens != 0 {
					t.Fatalf("refused request reached service %v / gates %d", r.svc.calls, r.gates.opens)
				}
				return
			}
			var got struct {
				ID     string `json:"id"`
				GateID string `json:"gate_id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if w := r.svc.byID[got.ID]; w.Request.PreparedBy != tc.preparer {
				t.Errorf("PreparedBy = %q, want %q", w.Request.PreparedBy, tc.preparer)
			}
			if r.cancels.gates[got.ID] != got.GateID || got.GateID == "" {
				t.Errorf("stored gate = %q, want %q", r.cancels.gates[got.ID], got.GateID)
			}
		})
	}
}

// rule: ARC-I-027
// Cancel withdraws the gate stored on the withdrawal, and only its preparer may cancel.
func TestWithdrawalCancelOnlyPreparerWithdrawsStoredGate(t *testing.T) {
	tests := []struct {
		name        string
		user        string
		storedOnly  bool
		status      int
		wantWithdrn bool
	}{
		{name: "preparer cancels", user: "alice", status: http.StatusOK, wantWithdrn: true},
		{name: "stored gate id is withdrawn", user: "alice", storedOnly: true, status: http.StatusOK, wantWithdrn: true},
		{name: "another user may not cancel", user: "bob", status: http.StatusForbidden},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReqRig(t, nil)
			if tc.storedOnly {
				w := r.svc.byID[wdOpen]
				w.GateID = ""
				r.svc.byID[wdOpen] = w
			}
			rec := reqDo(r.h, cancelPath(wdOpen), tc.user, "", keyMatch(fmt.Sprintf("31%d", i), `"1"`))
			if rec.Code != tc.status {
				t.Fatalf("status = %d (%s), want %d", rec.Code, rec.Body, tc.status)
			}
			g, err := r.gates.Fake.Get(context.Background(), r.gates.ids[r.openGID])
			if err != nil {
				t.Fatal(err)
			}
			if got := g.Status == gate.StatusWithdrawn; got != tc.wantWithdrn {
				t.Errorf("gate %s, withdrawn want %v", g.Status, tc.wantWithdrn)
			}
			if (len(r.cancels.cancelled) == 1) != tc.wantWithdrn {
				t.Errorf("cancels = %v", r.cancels.cancelled)
			}
		})
	}
}
