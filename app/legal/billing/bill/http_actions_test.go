package bill_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// The effect-owned service methods: a route that calls one sets gate-owned state.
func (c *countingBills) Approve(ctx context.Context, tx pgx.Tx, a bill.Approval) (bill.Bill, error) {
	c.count("Approve")
	return c.Fake.Approve(ctx, tx, a)
}

func (c *countingBills) Deliver(ctx context.Context, tx pgx.Tx, id bill.ID, ev bill.DeliveryEvidence) (bill.Bill, error) {
	c.count("Deliver")
	return c.Fake.Deliver(ctx, tx, id, ev)
}

func (c *countingBills) Update(ctx context.Context, tx pgx.Tx, id bill.ID, ch bill.Change) (bill.Bill, error) {
	c.count("Update")
	return c.Fake.Update(ctx, tx, id, ch)
}

// submitSpy records the tx of each Submit and can fail it.
type submitSpy struct {
	*countingBills
	tx   pgx.Tx
	fail error
}

// Submit is the route's own write: draft to pending_approval, in the gate's tx.
func (s *submitSpy) Submit(ctx context.Context, tx pgx.Tx, id bill.ID) (bill.Bill, error) {
	s.count("Submit")
	s.tx = tx
	if s.fail != nil {
		return bill.Bill{}, s.fail
	}
	return s.Fake.Submit(ctx, tx, id)
}

// countingGates counts Open calls on the gate engine fake and records the tx of the last.
type countingGates struct {
	*gatefake.Fake
	opens    int
	openTx   pgx.Tx
	failOpen error
}

func (g *countingGates) Open(ctx context.Context, tx pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.opens++
	g.openTx = tx
	if g.failOpen != nil {
		return "", g.failOpen
	}
	return g.Fake.Open(ctx, tx, kind, s, payload, p)
}

// Get returns the gate with a uuid id, as the Postgres engine has (the fake numbers them).
func (g *countingGates) Get(ctx context.Context, id gate.GateID) (gate.Gate, error) {
	out, err := g.Fake.Get(ctx, id)
	out.ID = "0190f000-0000-7000-8000-000000000001"
	return out, err
}

type actionRig struct {
	h       http.Handler
	bills   *countingBills
	gates   *countingGates
	outbox  *outboxfake.Fake
	effects *int
}

func newActionRig(t *testing.T) actionRig {
	t.Helper()
	bills := seedBills(t)
	now := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	effects := 0
	eng := gatefake.New(now, gatefake.Caps{})
	for _, kind := range []string{bill.GateBillApproval, bill.GateBillVoid} {
		err := eng.Register(gate.GateKind{Slug: kind, Approver: bill.CapApprove, Risk: gate.RiskHigh, MinApprovers: 1,
			TTL: 168 * time.Hour, Effect: func(context.Context, pgx.Tx, registry.Approved) error { effects++; return nil }})
		if err != nil {
			t.Fatal(err)
		}
	}
	gates := &countingGates{Fake: eng}
	ob := outboxfake.New()
	h := &bill.ActionHTTP{Bills: bills, Gates: gates, Outbox: ob, DB: txStub{}, Access: testAccess,
		Idem: idemfake.New(), Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return actionRig{h: m.Handler(), bills: bills, gates: gates, outbox: ob, effects: &effects}
}

func TestARC_I_051_BillActionProblems(t *testing.T) {
	both := func(v string) map[string]string {
		return map[string]string{"If-Match": v, idem.HeaderName: "0190e000-0000-7000-8000-000000000101"}
	}
	path := func(id, act string) string { return "/v1/bills/" + id + "/" + act }
	tests := []struct {
		name      string
		path      string
		user      string
		body      string
		hdr       map[string]string
		status    int
		code      string
		zeroCalls bool
	}{
		{"submit without principal", path(billDraft, "submit"), "", "", both(`"1"`), 401, bill.CodeUnauthenticated, true},
		{"void without principal", path(billApproved, "void"), "", `{"reason_code":"typo"}`, both(`"1"`), 401, bill.CodeUnauthenticated, true},
		{"deliver without principal", path(billApproved, "deliver"), "", `{"channel":"email"}`, both(`"1"`), 401, bill.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"submit without billing.draft", path(billDraft, "submit"), "viewer", "", both(`"1"`), 403, bill.CodeMissingCapability, true},
		{"void without billing.approve", path(billApproved, "void"), "alice", `{"reason_code":"typo"}`, both(`"1"`), 403, bill.CodeMissingCapability, true},
		{"deliver without billing.draft", path(billApproved, "deliver"), "viewer", `{"channel":"email"}`, both(`"1"`), 403, bill.CodeMissingCapability, true},
		{"submit malformed id", path("nope", "submit"), "alice", "", both(`"1"`), 400, server.CodeValidationFailed, true},
		// rule: ARC-I-006
		{"submit missing If-Match", path(billDraft, "submit"), "alice", "", map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000102"}, 428, server.CodePreconditionRequired, true},
		{"void missing If-Match", path(billApproved, "void"), "partner", `{"reason_code":"typo"}`, map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000103"}, 428, server.CodePreconditionRequired, true},
		{"deliver missing If-Match", path(billApproved, "deliver"), "alice", `{"channel":"email"}`, map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000104"}, 428, server.CodePreconditionRequired, true},
		// rule: ARC-I-007
		{"submit missing idempotency key", path(billDraft, "submit"), "alice", "", map[string]string{"If-Match": `"1"`}, 400, idem.CodeKeyRequired, true},
		{"void missing idempotency key", path(billApproved, "void"), "partner", `{"reason_code":"typo"}`, map[string]string{"If-Match": `"1"`}, 400, idem.CodeKeyRequired, true},
		{"deliver missing idempotency key", path(billApproved, "deliver"), "alice", `{"channel":"email"}`, map[string]string{"If-Match": `"1"`}, 400, idem.CodeKeyRequired, true},
		{"submit unknown body field", path(billDraft, "submit"), "alice", `{"approve":true}`, both(`"1"`), 400, server.CodeValidationFailed, true},
		{"void unknown body field", path(billApproved, "void"), "partner", `{"reason_code":"typo","status":"voided"}`, both(`"1"`), 400, server.CodeValidationFailed, true},
		{"deliver unknown body field", path(billApproved, "deliver"), "alice", `{"channel":"email","delivered_at":"2026-10-01T00:00:00Z"}`, both(`"1"`), 400, server.CodeValidationFailed, true},
		{"submit stale version", path(billDraft, "submit"), "alice", "", both(`"9"`), 412, server.CodeVersionConflict, false},
		{"void stale version", path(billApproved, "void"), "partner", `{"reason_code":"typo"}`, both(`"9"`), 412, server.CodeVersionConflict, false},
		{"deliver stale version", path(billApproved, "deliver"), "alice", `{"channel":"email"}`, both(`"9"`), 412, server.CodeVersionConflict, false},
		// rule: ARC-I-042
		{"submit walled bill is 404", path(billHidden, "submit"), "alice", "", both(`"1"`), 404, bill.CodeNotFound, false},
		{"submit approved bill", path(billApproved, "submit"), "alice", "", both(`"1"`), 409, bill.CodeBillNotSubmittable, false},
		{"void delivered bill", path(billDeliv, "void"), "partner", `{"reason_code":"typo"}`, both(`"1"`), 409, bill.CodeBillDeliveredUseCreditNote, false},
		{"void draft bill", path(billDraft, "void"), "partner", `{"reason_code":"typo"}`, both(`"1"`), 409, bill.CodeBillNotApproved, false},
		{"deliver draft bill", path(billDraft, "deliver"), "alice", `{"channel":"email"}`, both(`"1"`), 409, bill.CodeBillNotApproved, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newActionRig(t)
			rec := do(r.h, "POST", tc.path, tc.user, tc.body, tc.hdr)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.bills.total() != 0 {
				t.Errorf("service calls = %v, want 0", r.bills.calls)
			}
			if r.gates.opens != 0 || len(r.outbox.Messages()) != 0 {
				t.Errorf("gate opens = %d, outbox = %d, want 0", r.gates.opens, len(r.outbox.Messages()))
			}
		})
	}
}

// rule: ARC-I-012
// rule: BIL-C-120
func TestARC_I_012_BillActionsOpenGatesOnly(t *testing.T) {
	tests := []struct {
		name string
		path string
		user string
		body string
		kind string
	}{
		{"submit opens bill approval", "/v1/bills/" + billDraft + "/submit", "alice", "", bill.GateBillApproval},
		{"submit with delivery method", "/v1/bills/" + billDraft + "/submit", "alice", `{"delivery_method":"portal"}`, bill.GateBillApproval},
		{"void opens bill void", "/v1/bills/" + billApproved + "/void", "partner", `{"reason_code":"typo"}`, bill.GateBillVoid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newActionRig(t)
			rec := do(r.h, "POST", tc.path, tc.user, tc.body, map[string]string{"If-Match": `"1"`, idem.HeaderName: "0190e000-0000-7000-8000-000000000201"})
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			validate(t, "#/components/schemas/Gate", rec.Body.Bytes())
			var g struct {
				Kind  string `json:"kind"`
				State string `json:"state"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
				t.Fatal(err)
			}
			if g.Kind != tc.kind || g.State != "open" {
				t.Errorf("gate = %+v, want open %s", g, tc.kind)
			}
			for _, m := range []string{"Approve", "Deliver", "Update"} {
				if r.bills.calls[m] != 0 {
					t.Errorf("%s called %d times", m, r.bills.calls[m])
				}
			}
			if *r.effects != 0 || r.gates.opens != 1 {
				t.Errorf("effects = %d opens = %d, want 0 and 1", *r.effects, r.gates.opens)
			}
			b, _ := r.bills.Fake.Get(context.Background(), bill.ID(tc.path[len("/v1/bills/"):len("/v1/bills/")+36]))
			if b.Status == bill.StatusVoid || (tc.kind == bill.GateBillApproval && b.Status != bill.StatusPendingApproval) {
				t.Errorf("route moved the bill to %s", b.Status)
			}
		})
	}
}

func TestBillDeliverQueuesOnly(t *testing.T) {
	r := newActionRig(t)
	rec := do(r.h, "POST", "/v1/bills/"+billApproved+"/deliver", "alice", `{"channel":"email"}`,
		map[string]string{"If-Match": `"1"`, idem.HeaderName: "0190e000-0000-7000-8000-000000000301"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	validate(t, billSchema, rec.Body.Bytes())
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, set := got["delivered_at"]; set {
		t.Errorf("delivered_at set by the route: %v", got["delivered_at"])
	}
	msgs := r.outbox.Messages()
	if len(msgs) != 1 || msgs[0].Type != bill.MessageDeliveryRequested {
		t.Fatalf("outbox = %+v, want one %s", msgs, bill.MessageDeliveryRequested)
	}
	if r.bills.calls["Deliver"] != 0 {
		t.Errorf("Deliver called %d times", r.bills.calls["Deliver"])
	}
	b, _ := r.bills.Fake.Get(context.Background(), billApproved)
	if !b.DeliveredAt.IsZero() || b.Status != bill.StatusApproved {
		t.Errorf("bill = %s delivered %v", b.Status, b.DeliveredAt)
	}
}

func TestARC_I_008_BillActionIdempotency(t *testing.T) {
	r := newActionRig(t)
	hdr := map[string]string{"If-Match": `"1"`, idem.HeaderName: "0190e000-0000-7000-8000-000000000401"}
	path := "/v1/bills/" + billApproved + "/void"
	first := do(r.h, "POST", path, "partner", `{"reason_code":"typo"}`, hdr)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", `{"reason_code":"typo"}`, http.StatusAccepted, ""},
		// rule: ARC-I-008
		// The rule sets 422 for a reused key with a different body (as kernel/idem does).
		{"same key, different body", `{"reason_code":"duplicate"}`, http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(r.h, "POST", path, "partner", tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" && (rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true") {
				t.Errorf("replay = %s, want %s", rec.Body, first.Body)
			}
		})
	}
	if r.gates.opens != 1 {
		t.Errorf("gate opens = %d, want 1", r.gates.opens)
	}
}

func TestARC_I_012_BillActionWritesDeclared(t *testing.T) {
	writes, err := server.DeclaredWrites(newActionRig(t).h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 3 {
		t.Fatalf("writes = %+v, want submit, void and deliver", writes)
	}
}

// txMark is a distinct transaction value, so a test can see two calls shared one tx.
type txMark struct{ pgx.Tx }

// rollbackTx runs fn in one marked tx and, when fn fails, restores the bill fake to its
// state before the tx, as a Postgres rollback would.
type rollbackTx struct {
	bills *countingBills
	id    bill.ID
}

func (r rollbackTx) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	before, _ := r.bills.Fake.Get(ctx, r.id)
	if err := fn(ctx, &txMark{}); err != nil {
		if perr := r.bills.Fake.Put(before); perr != nil {
			return perr
		}
		return err
	}
	return nil
}

// rule: BIL-C-120
func TestBIL_C_120_SubmitMovesDraftInGateTx(t *testing.T) {
	tests := []struct {
		name       string
		failOpen   error
		failSubmit error
		status     int
		code       string
		wantStatus bill.Status
		wantOpens  int
	}{
		{name: "draft moves to pending_approval with the gate", status: http.StatusAccepted, wantStatus: bill.StatusPendingApproval, wantOpens: 1},
		{name: "gate open fails: the move rolls back", failOpen: errors.New("engine down"),
			status: http.StatusInternalServerError, code: server.CodeInternal, wantStatus: bill.StatusDraft, wantOpens: 1},
		{name: "bill left draft meanwhile: no gate", failSubmit: fmt.Errorf("submit: %w", bill.ErrNotSubmittable),
			status: http.StatusConflict, code: bill.CodeBillNotSubmittable, wantStatus: bill.StatusDraft, wantOpens: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newActionRig(t)
			r.gates.failOpen = tc.failOpen
			spy := &submitSpy{countingBills: r.bills, fail: tc.failSubmit}
			h := &bill.ActionHTTP{Bills: spy, Gates: r.gates, Outbox: r.outbox, DB: rollbackTx{bills: r.bills, id: billDraft},
				Access: testAccess, Idem: idemfake.New(), Problems: &server.Problems{}, Principal: testPrincipal}
			m := server.New(nil)
			if err := m.Mount(h.Routes); err != nil {
				t.Fatal(err)
			}
			rec := do(m.Handler(), "POST", "/v1/bills/"+billDraft+"/submit", "alice", `{"delivery_method":"mail"}`,
				map[string]string{"If-Match": `"1"`, idem.HeaderName: "0190e000-0000-7000-8000-000000000501"})
			if tc.code != "" {
				checkProblem(t, rec, tc.status, tc.code)
			} else if rec.Code != tc.status {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			if r.bills.calls["Submit"] != 1 || r.bills.calls["Approve"] != 0 {
				t.Errorf("Submit = %d, Approve = %d; want 1, 0", r.bills.calls["Submit"], r.bills.calls["Approve"])
			}
			if r.gates.opens != tc.wantOpens {
				t.Errorf("gate opens = %d, want %d", r.gates.opens, tc.wantOpens)
			}
			if tc.wantOpens > 0 && (spy.tx == nil || spy.tx != r.gates.openTx) {
				t.Errorf("Submit tx %p and Open tx %p differ", spy.tx, r.gates.openTx)
			}
			b, _ := r.bills.Fake.Get(context.Background(), billDraft)
			if b.Status != tc.wantStatus {
				t.Errorf("bill status = %s, want %s", b.Status, tc.wantStatus)
			}
		})
	}
}
