package gates_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	dGateBill    = "0190d000-0000-7000-8000-000000000001"
	dGateTrust   = "0190d000-0000-7000-8000-000000000002"
	dGateHidden  = "0190d000-0000-7000-8000-000000000003"
	dGateGone    = "0190d000-0000-7000-8000-000000000004"
	dGateMissing = "0190d000-0000-7000-8000-0000000000ff"
)

// uuidEngine gives the fake engine's gates uuid ids, counts calls and can fail Decide with
// a fixed error.
type uuidEngine struct {
	*gatefake.Fake
	ids      map[gate.GateID]gate.GateID // uuid -> fake id
	gets     int
	decides  int
	withdraw int
	failWith error
}

func (e *uuidEngine) Get(ctx context.Context, id gate.GateID) (gate.Gate, error) {
	e.gets++
	fid, ok := e.ids[id]
	if !ok {
		return gate.Gate{}, gate.ErrNotFound
	}
	g, err := e.Fake.Get(ctx, fid)
	g.ID = id
	return g, err
}

func (e *uuidEngine) Decide(ctx context.Context, tx pgx.Tx, id gate.GateID, version int, d gate.Decision, by gate.Principal) error {
	e.decides++
	if e.failWith != nil {
		return fmt.Errorf("decide %s: %w", id, e.failWith)
	}
	return e.Fake.Decide(ctx, tx, e.ids[id], version, d, by)
}

func (e *uuidEngine) Withdraw(ctx context.Context, tx pgx.Tx, id gate.GateID, by gate.Principal) error {
	e.withdraw++
	return e.Fake.Withdraw(ctx, tx, e.ids[id], by)
}

func (e *uuidEngine) calls() int { return e.gets + e.decides + e.withdraw }

type decideRig struct {
	h       http.Handler
	eng     *uuidEngine
	effects *int
	now     *time.Time
	hashes  map[string]string
}

func newDecideRig(t *testing.T, trustShort bool) decideRig {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	effects := 0
	fake := gatefake.New(clock, gatefake.Caps(users))
	reg := func(slug, approver string, risk registry.Risk, effect registry.EffectFunc) {
		if err := fake.Register(gate.GateKind{Slug: slug, Approver: approver, Risk: risk, MinApprovers: 1,
			TTL: 24 * time.Hour, Effect: effect}); err != nil {
			t.Fatal(err)
		}
	}
	reg(kindBill, capBill, gate.RiskHigh, func(context.Context, pgx.Tx, registry.Approved) error { effects++; return nil })
	reg(kindTrust, capTrust, gate.RiskHigh, func(context.Context, pgx.Tx, registry.Approved) error {
		if trustShort {
			return fmt.Errorf("withdraw: %w", trust.ErrInsufficientFunds)
		}
		effects++
		return nil
	})
	eng := &uuidEngine{Fake: fake, ids: map[gate.GateID]gate.GateID{}}
	hashes := map[string]string{}
	alice := registry.Principal{Kind: registry.PrincipalUser, UserID: "alice"}
	for _, o := range []struct{ id, kind, subject string }{
		{dGateBill, kindBill, subjectA}, {dGateTrust, kindTrust, subjectA},
		{dGateHidden, kindBill, subjectHidden}, {dGateGone, kindBill, subjectA},
	} {
		payload := json.RawMessage(`{"bill_id":"` + o.subject + `"}`)
		fid, err := fake.Open(context.Background(), nil, o.kind, gate.Subject{Kind: "billing.bill", ID: o.subject}, payload, alice)
		if err != nil {
			t.Fatal(err)
		}
		eng.ids[gate.GateID(o.id)] = fid
		hashes[o.id], _ = gatefake.PayloadHash(payload)
	}
	if err := fake.Withdraw(context.Background(), nil, eng.ids[dGateGone], alice); err != nil {
		t.Fatal(err)
	}
	h := &gates.DecideHTTP{Gates: eng, Kinds: catalog(), Access: testAccessT{}, DB: txStub{}, Idem: idemfake.New(),
		Problems: &server.Problems{}, Principal: testPrincipal, Now: clock,
		StepUp: func(r *http.Request) time.Time {
			switch r.Header.Get("X-Test-StepUp") {
			case "none":
				return time.Time{}
			case "stale":
				return now.Add(-time.Hour)
			}
			return now.Add(-time.Minute)
		}}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return decideRig{h: m.Handler(), eng: eng, effects: &effects, now: &now, hashes: hashes}
}

func decision(verdict string, version int, hash, extra string) string {
	return fmt.Sprintf(`{"decision":%q,"version":%d,"payload_hash":%q%s}`, verdict, version, hash, extra)
}

func hdrs(ifMatch, key string, more ...string) map[string]string {
	m := map[string]string{}
	if ifMatch != "" {
		m["If-Match"] = ifMatch
	}
	if key != "" {
		m[idem.HeaderName] = key
	}
	for i := 0; i+1 < len(more); i += 2 {
		m[more[i]] = more[i+1]
	}
	return m
}

const key1 = "0190e000-0000-7000-8000-000000000201"

func TestARC_I_051_GateDecisionProblems(t *testing.T) {
	dec := func(id string) string { return "/v1/gates/" + id + "/decisions" }
	tests := []struct {
		name       string
		path       string
		user       string
		body       func(r decideRig) string
		hdr        map[string]string
		expire     bool
		status     int
		code       string
		zeroCalls  bool
		trustShort bool
	}{
		{name: "no principal", path: dec(dGateBill), status: 401, code: gates.CodeUnauthenticated, zeroCalls: true},
		// rule: ARC-I-017
		{name: "agent principal", path: dec(dGateBill), user: "agent:partner", status: 403, code: gates.CodeAgentCannotDecide, zeroCalls: true},
		// rule: ARC-I-051
		{name: "no approver capability", path: dec(dGateBill), user: "reader", status: 403, code: gates.CodeMissingCapability, zeroCalls: true},
		// rule: ARC-I-018
		{name: "not the kind's capability", path: dec(dGateTrust), user: "partner", status: 403, code: gates.CodeMissingCapability},
		{name: "malformed id", path: dec("nope"), user: "partner", status: 400, code: server.CodeValidationFailed, zeroCalls: true},
		// rule: ARC-I-006
		{name: "missing If-Match", path: dec(dGateBill), user: "partner", hdr: hdrs("", key1), status: 428, code: server.CodePreconditionRequired, zeroCalls: true},
		// rule: ARC-I-007
		{name: "missing Idempotency-Key", path: dec(dGateBill), user: "partner", hdr: hdrs(`"1"`, ""), status: 400, code: idem.CodeKeyRequired, zeroCalls: true},
		{name: "unknown body field", path: dec(dGateBill), user: "partner",
			body: func(r decideRig) string { return decision("approve", 1, r.hashes[dGateBill], `,"effect":"run"`) }, status: 400, code: server.CodeValidationFailed},
		{name: "reject without reason", path: dec(dGateBill), user: "partner",
			body: func(r decideRig) string { return decision("reject", 1, r.hashes[dGateBill], "") }, status: 400, code: server.CodeValidationFailed},
		{name: "unknown gate", path: dec(dGateMissing), user: "partner", status: 404, code: gates.CodeNotFound},
		// rule: ARC-I-042
		{name: "walled subject is 404", path: dec(dGateHidden), user: "partner", status: 404, code: gates.CodeNotFound},
		{name: "stale If-Match", path: dec(dGateBill), user: "partner", hdr: hdrs(`"7"`, key1), status: 412, code: server.CodeVersionConflict},
		// rule: ARC-I-015
		{name: "stale body version", path: dec(dGateBill), user: "partner",
			body: func(r decideRig) string { return decision("approve", 7, r.hashes[dGateBill], "") }, status: 412, code: server.CodeVersionConflict},
		// rule: ARC-I-016
		{name: "self-approval", path: dec(dGateBill), user: "alice", status: 403, code: gates.CodeSelfApproval},
		// rule: ARC-I-017
		{name: "no step-up", path: dec(dGateBill), user: "partner", hdr: hdrs(`"1"`, key1, "X-Test-StepUp", "none"), status: 403, code: gates.CodeStepUpRequired},
		{name: "high risk with stale step-up", path: dec(dGateBill), user: "partner", hdr: hdrs(`"1"`, key1, "X-Test-StepUp", "stale"), status: 403, code: gates.CodeStepUpRequired},
		{name: "payload changed", path: dec(dGateBill), user: "partner",
			body: func(decideRig) string { return decision("approve", 1, hashA, "") }, status: 409, code: gates.CodePayloadChanged},
		{name: "withdrawn gate", path: dec(dGateGone), user: "partner", status: 409, code: gates.CodeGateNotOpen},
		// rule: ARC-I-022
		{name: "expired gate", path: dec(dGateBill), user: "partner", expire: true, status: 409, code: gates.CodeGateExpired},
		{name: "insufficient trust funds", path: dec(dGateTrust), user: "trustee", trustShort: true, status: 409, code: gates.CodeInsufficientTrustFunds},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDecideRig(t, tc.trustShort)
			if tc.expire {
				*r.now = r.now.Add(48 * time.Hour)
			}
			hdr := tc.hdr
			if hdr == nil {
				hdr = hdrs(`"1"`, key1)
			}
			id := tc.path[len("/v1/gates/") : len(tc.path)-len("/decisions")]
			body := decision("approve", 1, r.hashes[id], "")
			if tc.body != nil {
				body = tc.body(r)
			}
			rec := do(r.h, "POST", tc.path, tc.user, body, hdr)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.eng.calls() != 0 {
				t.Errorf("engine calls = %d, want 0", r.eng.calls())
			}
			if *r.effects != 0 {
				t.Errorf("effects = %d, want 0", *r.effects)
			}
		})
	}
}

// rule: ARC-I-012
func TestGateDecisionSentinelsMapToProblems(t *testing.T) {
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{gates.ErrCoolingOff, 409, gates.CodeCoolingOff},
		{gate.ErrExpired, 409, gates.CodeGateExpired},
		{gate.ErrNotPending, 409, gates.CodeGateNotOpen},
		{fmt.Errorf("%w: %w", gate.ErrEffectFailed, trust.ErrInsufficientFunds), 409, gates.CodeInsufficientTrustFunds},
		{gate.ErrSelfApproval, 403, gates.CodeSelfApproval},
		{gate.ErrMFARequired, 403, gates.CodeStepUpRequired},
		{gate.ErrAgentApprover, 403, gates.CodeAgentCannotDecide},
		{gate.ErrStaleVersion, 412, server.CodeVersionConflict},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			r := newDecideRig(t, false)
			r.eng.failWith = tc.err
			rec := do(r.h, "POST", "/v1/gates/"+dGateBill+"/decisions", "partner",
				decision("approve", 1, r.hashes[dGateBill], ""), hdrs(`"1"`, key1))
			checkProblem(t, rec, tc.status, tc.code)
		})
	}
}

// rule: ARC-I-012
// rule: ARC-I-007
func TestGateDecisionRunsEffectOnceAndReplays(t *testing.T) {
	tests := []struct {
		name      string
		verdict   string
		extra     string
		wantState string
		effects   int
	}{
		{"approve runs the effect", "approve", "", "executed", 1},
		{"return", "return", `,"reason":"fix the rate"`, "returned", 0},
		{"reject", "reject", `,"reason":"not billable"`, "rejected", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDecideRig(t, false)
			body := decision(tc.verdict, 1, r.hashes[dGateBill], tc.extra)
			var first string
			for i := 0; i < 2; i++ {
				rec := do(r.h, "POST", "/v1/gates/"+dGateBill+"/decisions", "partner", body, hdrs(`"1"`, key1))
				if rec.Code != 200 {
					t.Fatalf("status = %d: %s", rec.Code, rec.Body)
				}
				validate(t, gateSchemaPath, rec.Body.Bytes())
				if i == 0 {
					first = rec.Body.String()
				} else if rec.Body.String() != first {
					t.Errorf("replay body = %s, want %s", rec.Body, first)
				}
			}
			var v gates.View
			if err := json.Unmarshal([]byte(first), &v); err != nil {
				t.Fatal(err)
			}
			if v.State != tc.wantState {
				t.Errorf("state = %q, want %q", v.State, tc.wantState)
			}
			if r.eng.decides != 1 || *r.effects != tc.effects {
				t.Errorf("decides = %d, effects = %d; want 1, %d", r.eng.decides, *r.effects, tc.effects)
			}
			// rule: ARC-I-008
			other := decision("reject", 1, r.hashes[dGateBill], `,"reason":"other"`)
			rec := do(r.h, "POST", "/v1/gates/"+dGateBill+"/decisions", "partner", other, hdrs(`"1"`, key1))
			checkProblem(t, rec, 422, idem.CodeKeyReused)
			if r.eng.decides != 1 {
				t.Errorf("decides after reuse = %d, want 1", r.eng.decides)
			}
		})
	}
}

func TestGateWithdraw(t *testing.T) {
	wd := func(id string) string { return "/v1/gates/" + id + "/withdraw" }
	tests := []struct {
		name   string
		path   string
		user   string
		body   string
		hdr    map[string]string
		status int
		code   string
	}{
		{"preparer withdraws", wd(dGateBill), "alice", "", nil, 200, ""},
		{"agent for the preparer withdraws", wd(dGateBill), "agent:alice", "", nil, 200, ""},
		{"no principal", wd(dGateBill), "", "", nil, 401, gates.CodeUnauthenticated},
		{"non-preparer", wd(dGateBill), "partner", "", nil, 403, gates.CodeNotPreparer},
		{"malformed id", wd("nope"), "alice", "", nil, 400, server.CodeValidationFailed},
		{"missing If-Match", wd(dGateBill), "alice", "", hdrs("", key1), 428, server.CodePreconditionRequired},
		{"missing Idempotency-Key", wd(dGateBill), "alice", "", hdrs(`"1"`, ""), 400, idem.CodeKeyRequired},
		{"unknown body field", wd(dGateBill), "alice", `{"state":"withdrawn"}`, nil, 400, server.CodeValidationFailed},
		{"stale If-Match", wd(dGateBill), "alice", "", hdrs(`"3"`, key1), 412, server.CodeVersionConflict},
		{"walled subject is 404", wd(dGateHidden), "alice", "", nil, 404, gates.CodeNotFound},
		{"already withdrawn", wd(dGateGone), "alice", "", nil, 409, gates.CodeGateNotOpen},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDecideRig(t, false)
			hdr := tc.hdr
			if hdr == nil {
				hdr = hdrs(`"1"`, key1)
			}
			rec := do(r.h, "POST", tc.path, tc.user, tc.body, hdr)
			if tc.code != "" {
				checkProblem(t, rec, tc.status, tc.code)
				return
			}
			if rec.Code != tc.status {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			validate(t, gateSchemaPath, rec.Body.Bytes())
			var v gates.View
			if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
				t.Fatal(err)
			}
			if v.State != "withdrawn" || r.eng.withdraw != 1 {
				t.Errorf("state = %q, withdraw calls = %d", v.State, r.eng.withdraw)
			}
		})
	}
}

func TestDecideRoutesUnwiredAnswer503(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&gates.DecideHTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/v1/gates/" + dGateBill + "/decisions", "/v1/gates/" + dGateBill + "/withdraw"} {
		checkProblem(t, do(m.Handler(), "POST", p, "partner", "", hdrs(`"1"`, key1)), 503, gates.CodeUnavailable)
	}
}
