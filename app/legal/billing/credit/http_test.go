package credit_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/billing/credit/creditfake"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	userViewer = "0190d000-0000-7000-8000-000000000001"
	userBiller = "0190d000-0000-7000-8000-000000000002"
	userNobody = "0190d000-0000-7000-8000-000000000003"

	billOpen      = "0190e000-0000-7000-8000-000000000001"
	billDraft     = "0190e000-0000-7000-8000-000000000002"
	billHidden    = "0190e000-0000-7000-8000-0000000000ff"
	noteHidden    = "0190f000-0000-7000-8000-0000000000ff"
	noteRequested = "0190f000-0000-7000-8000-000000000001"
)

// stubReads is an in-memory credit.Reads that counts calls.
type stubReads struct {
	mu      sync.Mutex
	entries []credit.Entry
	calls   int
}

func (s *stubReads) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubReads) Entries(_ context.Context, f credit.ListFilter) ([]credit.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	out := []credit.Entry{}
	for _, e := range s.entries {
		if e.ID == noteHidden || (f.After != "" && e.ID <= f.After) || (f.BillID != "" && e.BillID != f.BillID) {
			continue
		}
		if len(out) == f.Limit {
			break
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *stubReads) Entry(_ context.Context, id string) (credit.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for _, e := range s.entries {
		if e.ID == id && id != noteHidden {
			return e, nil
		}
	}
	return credit.Entry{}, fmt.Errorf("entry: %w", credit.ErrNotFound)
}

// countingBills wraps the creditfake bills and counts reads.
type countingBills struct {
	*creditfake.Fake
	mu    sync.Mutex
	calls int
}

func (b *countingBills) Bill(ctx context.Context, id string) (credit.Bill, error) {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	return b.Fake.Bill(ctx, id)
}

func (b *countingBills) Calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// stubGates records Open calls and never runs an effect.
type stubGates struct {
	mu    sync.Mutex
	gates map[gate.GateID]gate.Gate
	opens []string
}

func (g *stubGates) Open(_ context.Context, _ pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.opens = append(g.opens, kind)
	id := gate.GateID(fmt.Sprintf("0190b000-0000-7000-8000-%012d", len(g.opens)))
	sum := sha256.Sum256(payload)
	at := time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)
	g.gates[id] = gate.Gate{ID: id, Kind: kind, Version: 1, Subject: s, Payload: payload,
		PayloadHash: "sha256:" + hex.EncodeToString(sum[:]), Preparer: p, Status: gate.StatusPending,
		CreatedAt: at, ExpiresAt: at.Add(72 * time.Hour)}
	return id, nil
}

func (g *stubGates) Get(_ context.Context, id gate.GateID) (gate.Gate, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out, ok := g.gates[id]
	if !ok {
		return gate.Gate{}, gate.ErrNotFound
	}
	return out, nil
}

func (g *stubGates) Opens() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.opens...)
}

// effectOnly stands for the effect-owned credit.Service writes (approval assigns the number
// and issues the note). A call outside a gate effect fails the test.
type effectOnly struct {
	*creditfake.Fake
	t     *testing.T
	calls int
}

func (e *effectOnly) guard(ctx context.Context, method string) error {
	e.calls++
	// rule: ARC-I-012
	if err := gate.RequireEffect(ctx); err != nil {
		e.t.Errorf("effect-owned %s called outside a gate effect", method)
		return err
	}
	return nil
}

func (e *effectOnly) ApproveNote(ctx context.Context, _ string, _ credit.Approver) (credit.Note, error) {
	return credit.Note{}, e.guard(ctx, "ApproveNote")
}

func (e *effectOnly) ApproveWriteOff(ctx context.Context, _ string, _ credit.Approver) (credit.WriteOff, error) {
	return credit.WriteOff{}, e.guard(ctx, "ApproveWriteOff")
}

type stubAccess struct{ caps map[string][]string }

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == billHidden || s.ID == noteHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a.caps[p.UserID] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

type rig struct {
	h       http.Handler
	reads   *stubReads
	bills   *countingBills
	gates   *stubGates
	effects *effectOnly
}

func (r rig) calls() int { return r.reads.Calls() + r.bills.Calls() + len(r.gates.Opens()) }

func newRig(t *testing.T, n int) rig {
	t.Helper()
	fake := creditfake.New()
	fake.PutBill(credit.Bill{ID: billOpen, Delivered: true, OpenCents: 50_000})
	fake.PutBill(credit.Bill{ID: billDraft})
	rg := rig{reads: &stubReads{}, bills: &countingBills{Fake: fake}, gates: &stubGates{gates: map[gate.GateID]gate.Gate{}},
		effects: &effectOnly{Fake: fake, t: t}}
	rg.reads.entries = append(rg.reads.entries, credit.Entry{ID: noteRequested, BillID: billOpen, Kind: credit.EntryCredit,
		AmountCents: 10_000, HSTCents: 1_300, ReasonCode: "billing_error", Status: credit.EntryRequested,
		GateID: "0190b000-0000-7000-8000-0000000000aa"})
	for i := 0; i < n; i++ {
		rg.reads.entries = append(rg.reads.entries, credit.Entry{ID: fmt.Sprintf("0190f000-0000-7000-8000-%012d", i+2),
			Number: fmt.Sprintf("CN-%04d", i+1), BillID: billOpen, Kind: credit.EntryWriteOff, AmountCents: 500,
			Status: credit.EntryIssued})
	}
	rg.reads.entries = append(rg.reads.entries, credit.Entry{ID: noteHidden, BillID: billOpen, Kind: credit.EntryCredit, Status: credit.EntryIssued})
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &credit.HTTP{
		Reads: rg.reads, Bills: rg.bills, DB: txStub{}, Gates: rg.gates, Idem: idemfake.New(),
		Access: stubAccess{caps: map[string][]string{
			userViewer: {credit.CapView},
			userBiller: {credit.CapView, credit.CapCredit},
		}},
		Cursors:  codec,
		Problems: &server.Problems{},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		},
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	rg.h = m.Handler()
	t.Cleanup(func() {
		if rg.effects.calls != 0 {
			t.Errorf("effect-owned credit writes = %d, want 0", rg.effects.calls)
		}
	})
	return rg
}

var openAPISchemas = sync.OnceValues(func() (*jsonschema.Compiler, error) {
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

func validate(t *testing.T, ref string, body []byte) {
	t.Helper()
	c, err := openAPISchemas()
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

func checkProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, api.ProblemContentType)
	}
	validate(t, "#/components/schemas/Problem", rec.Body.Bytes())
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code {
		t.Errorf("code = %q, want %q", p.Code, code)
	}
}

func do(h http.Handler, method, path, user, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
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

func reqJSON(bill string, cents int64) string {
	return fmt.Sprintf(`{"bill_id":%q,"kind":"credit","amount_cents":%d,"reason_code":"billing_error"}`, bill, cents)
}

func TestARC_I_051_CreditNoteRoutesProblems(t *testing.T) {
	key := map[string]string{idem.HeaderName: "key-00000001"}
	tests := []struct {
		name      string
		method    string
		path      string
		user      string
		body      string
		hdr       map[string]string
		status    int
		code      string
		zeroCalls bool
	}{
		{"list without principal", "GET", "/v1/credit-notes", "", "", nil, 401, credit.CodeUnauthenticated, true},
		{"post without principal", "POST", "/v1/credit-notes", "", reqJSON(billOpen, 100), key, 401, credit.CodeUnauthenticated, true},
		{"get without principal", "GET", "/v1/credit-notes/" + noteRequested, "", "", nil, 401, credit.CodeUnauthenticated, true},
		{"list without billing.view", "GET", "/v1/credit-notes", userNobody, "", nil, 403, credit.CodeMissingCapability, true},
		{"post without billing.credit", "POST", "/v1/credit-notes", userViewer, reqJSON(billOpen, 100), key, 403, credit.CodeMissingCapability, true},
		{"get without billing.view", "GET", "/v1/credit-notes/" + noteRequested, userNobody, "", nil, 403, credit.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"get hidden note is 404, not 403", "GET", "/v1/credit-notes/" + noteHidden, userNobody, "", nil, 404, credit.CodeNotFound, true},
		{"list hidden bill is 404", "GET", "/v1/credit-notes?bill_id=" + billHidden, userViewer, "", nil, 404, credit.CodeNotFound, true},
		{"post on hidden bill is 404", "POST", "/v1/credit-notes", userBiller, reqJSON(billHidden, 100), key, 404, credit.CodeNotFound, true},
		{"get unknown note", "GET", "/v1/credit-notes/0190f000-0000-7000-8000-0000000000ee", userViewer, "", nil, 404, credit.CodeNotFound, false},
		{"get malformed id", "GET", "/v1/credit-notes/nope", userViewer, "", nil, 400, server.CodeValidationFailed, true},
		{"list malformed bill_id", "GET", "/v1/credit-notes?bill_id=nope", userViewer, "", nil, 400, server.CodeValidationFailed, true},
		{"tampered cursor", "GET", "/v1/credit-notes?cursor=AAAA.BBBB", userViewer, "", nil, 400, api.CodeBadCursor, true},
		// rule: ARC-I-007
		{"post missing idempotency key", "POST", "/v1/credit-notes", userBiller, reqJSON(billOpen, 100), nil, 400, idem.CodeKeyRequired, true},
		{"post unknown body field", "POST", "/v1/credit-notes", userBiller, `{"bill_id":"` + billOpen + `","kind":"credit","amount_cents":1,"reason_code":"x","colour":"red"}`, key, 400, server.CodeValidationFailed, true},
		{"post malformed bill_id", "POST", "/v1/credit-notes", userBiller, reqJSON("nope", 100), key, 400, server.CodeValidationFailed, true},
		{"post zero amount", "POST", "/v1/credit-notes", userBiller, reqJSON(billOpen, 0), key, 400, server.CodeValidationFailed, true},
		{"post unknown bill", "POST", "/v1/credit-notes", userBiller, reqJSON("0190e000-0000-7000-8000-0000000000ee", 100), key, 404, credit.CodeNotFound, false},
		{"post undelivered bill", "POST", "/v1/credit-notes", userBiller, reqJSON(billDraft, 100), key, 409, credit.CodeBillNotDelivered, false},
		// rule: BIL-C-082
		{"post credit above bill balance", "POST", "/v1/credit-notes", userBiller, reqJSON(billOpen, 50_001), key, 409, credit.CodeCreditExceedsBill, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newRig(t, 2)
			checkProblem(t, do(rg.h, tc.method, tc.path, tc.user, tc.body, tc.hdr), tc.status, tc.code)
			if tc.zeroCalls && rg.calls() != 0 {
				t.Errorf("service calls = %d, want 0", rg.calls())
			}
			if tc.method == "POST" && len(rg.gates.Opens()) != 0 {
				t.Errorf("gates opened = %v, want none", rg.gates.Opens())
			}
		})
	}
}

func TestCreditNoteRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&credit.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/credit-notes", userViewer, "", nil), 503, credit.CodeUnavailable)
}

func TestARC_I_012_CreditNoteRequestOpensGate(t *testing.T) {
	rg := newRig(t, 0)
	rec := do(rg.h, "POST", "/v1/credit-notes", userBiller, reqJSON(billOpen, 50_000), map[string]string{idem.HeaderName: "key-00000002"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	validate(t, "#/components/schemas/Gate", rec.Body.Bytes())
	var g struct {
		Kind    string         `json:"kind"`
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	if g.Kind != credit.GateCreditNote || g.Payload["trust_effect"] != "none" {
		t.Errorf("gate = %+v, want %s with trust_effect none", g, credit.GateCreditNote)
	}
	writes, err := server.DeclaredWrites(rg.h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || writes[0].Method != http.MethodPost || len(writes[0].Sets) != 0 {
		t.Errorf("writes = %+v, want POST /credit-notes declaring nothing", writes)
	}
}

func TestARC_I_007_CreditNoteIdempotency(t *testing.T) {
	rg := newRig(t, 0)
	hdr := map[string]string{idem.HeaderName: "key-00000042"}
	first := do(rg.h, "POST", "/v1/credit-notes", userBiller, reqJSON(billOpen, 100), hdr)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d (%s)", first.Code, first.Body)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", reqJSON(billOpen, 100), http.StatusAccepted, ""},
		// The kernel idem store answers 422 for a reused key with a different body (ARC-I-008).
		{"same key, different body", reqJSON(billOpen, 200), http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(rg.h, "POST", "/v1/credit-notes", userBiller, tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" {
				if rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true" {
					t.Errorf("replay body = %s, want %s", rec.Body, first.Body)
				}
				return
			}
			checkProblem(t, rec, tc.status, tc.code)
		})
	}
	if opens := rg.gates.Opens(); len(opens) != 1 {
		t.Errorf("gates opened = %v, want one", opens)
	}
}

func TestCreditNoteGetRequestedHasNoNumber(t *testing.T) {
	rg := newRig(t, 1)
	rec := do(rg.h, "GET", "/v1/credit-notes/"+noteRequested, userViewer, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	validate(t, "#/components/schemas/CreditNote", rec.Body.Bytes())
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if n, ok := v["number"]; !ok || n != nil || v["status"] != credit.EntryRequested {
		t.Errorf("note = %v, want a requested note with number null", v)
	}
}

func TestCreditNoteListPages(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		query     string
		wantItems []int
	}{
		{"one page", 2, "", []int{3}},
		{"pages of two", 4, "?limit=2", []int{2, 2, 1}},
		{"limit above maximum is capped", 3, "?limit=5000", []int{4}},
		{"by bill", 2, "?bill_id=" + billDraft, []int{0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newRig(t, tc.n)
			path := "/v1/credit-notes" + tc.query
			seen := map[string]bool{}
			for i, want := range tc.wantItems {
				rec := do(rg.h, "GET", path, userViewer, "", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d status = %d (%s)", i, rec.Code, rec.Body)
				}
				validate(t, "#/paths/~1credit-notes/get/responses/200/content/application~1json/schema", rec.Body.Bytes())
				var page struct {
					Items []json.RawMessage `json:"data"`
					Next  string            `json:"next_cursor"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if len(page.Items) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Items), want)
				}
				for _, it := range page.Items {
					validate(t, "#/components/schemas/CreditNote", it)
					var v struct{ ID string }
					_ = json.Unmarshal(it, &v)
					if v.ID == noteHidden || seen[v.ID] {
						t.Errorf("page %d repeats or leaks %s", i, v.ID)
					}
					seen[v.ID] = true
				}
				last := i == len(tc.wantItems)-1
				if (page.Next != "") == last {
					t.Fatalf("page %d next = %q, last = %v", i, page.Next, last)
				}
				path = "/v1/credit-notes?cursor=" + page.Next
			}
		})
	}
}
