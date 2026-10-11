package gates_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/audit/auditfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	kindBill  = "billing.bill_approval"
	kindTrust = "trust.withdrawal"
	capBill   = "billing.approve"
	capTrust  = "trust.authorize"

	gateBillOpen   = "0190c000-0000-7000-8000-000000000001"
	gateBillMine   = "0190c000-0000-7000-8000-000000000002"
	gateTrustOpen  = "0190c000-0000-7000-8000-000000000003"
	gateBillDone   = "0190c000-0000-7000-8000-000000000004"
	gateHidden     = "0190c000-0000-7000-8000-000000000005"
	gateMissing    = "0190c000-0000-7000-8000-0000000000ff"
	subjectA       = "0190b000-0000-7000-8000-000000000001"
	subjectHidden  = "0190b000-0000-7000-8000-0000000000ff"
	hashA          = "sha256:0000000000000000000000000000000000000000000000000000000000000001"
	listSchemaPath = "#/paths/~1gates/get/responses/200/content/application~1json/schema"
	gateSchemaPath = "#/components/schemas/Gate"
)

// users maps a test user to the capabilities they hold.
var users = map[string][]string{
	"partner": {capBill},
	"alice":   {capBill},
	"trustee": {capTrust},
	"both":    {capBill, capTrust},
	"reader":  {"contacts.view"},
}

type testAccessT struct{}

// Can reports the user's capability; a hidden subject is not visible to anyone.
func (testAccessT) Can(_ context.Context, p registry.Principal, c string, s registry.Subject) (bool, error) {
	if s.ID == subjectHidden {
		return false, identity.ErrNotVisible
	}
	u := p.UserID
	if p.Kind == registry.PrincipalAgent {
		u = p.ActingFor
	}
	return slices.Contains(users[u], c), nil
}

// testPrincipal reads the X-Test-User header; an agent is "agent:<acting-for>".
func testPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get("X-Test-User")
	if u == "" {
		return registry.Principal{}, false
	}
	if a, ok := strings.CutPrefix(u, "agent:"); ok {
		return registry.Principal{Kind: registry.PrincipalAgent, RunID: "run-1", ActingFor: a}, true
	}
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, true
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, nil)
}

// store is an in-memory gate reader and lister that counts its calls.
type store struct {
	mu      sync.Mutex
	gates   map[gate.GateID]gate.Gate
	calls   int
	filters []gates.ListFilter
}

func (s *store) Get(_ context.Context, id gate.GateID) (gate.Gate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	g, ok := s.gates[id]
	if !ok {
		return gate.Gate{}, gate.ErrNotFound
	}
	return g, nil
}

func (s *store) List(_ context.Context, f gates.ListFilter) ([]gate.Gate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.filters = append(s.filters, f)
	var out []gate.Gate
	for _, g := range s.gates {
		switch {
		case g.ID <= f.After,
			f.States != nil && !slices.Contains(f.States, g.Status),
			f.Kinds != nil && !slices.Contains(f.Kinds, g.Kind),
			f.NotPreparedBy != "" && g.Preparer.UserID == f.NotPreparedBy,
			g.Subject.ID == subjectHidden:
			continue
		}
		out = append(out, g)
	}
	slices.SortFunc(out, func(a, b gate.Gate) int { return strings.Compare(string(a.ID), string(b.ID)) })
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func seedStore() *store {
	opened := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	mk := func(id, kind string, st gate.Status, preparer, subject string) gate.Gate {
		return gate.Gate{ID: gate.GateID(id), Kind: kind, Version: 1, Status: st,
			Subject: gate.Subject{Kind: "billing.bill", ID: subject}, Payload: json.RawMessage(`{"bill_id":"` + subject + `"}`),
			PayloadHash: hashA, Preparer: registry.Principal{Kind: registry.PrincipalUser, UserID: preparer},
			CreatedAt: opened, ExpiresAt: opened.Add(168 * time.Hour)}
	}
	s := &store{gates: map[gate.GateID]gate.Gate{}}
	for _, g := range []gate.Gate{
		mk(gateBillOpen, kindBill, gate.StatusPending, "alice", subjectA),
		mk(gateBillMine, kindBill, gate.StatusPending, "partner", subjectA),
		mk(gateTrustOpen, kindTrust, gate.StatusPending, "alice", subjectA),
		mk(gateBillDone, kindBill, gate.StatusApproved, "alice", subjectA),
		mk(gateHidden, kindBill, gate.StatusPending, "alice", subjectHidden),
	} {
		s.gates[g.ID] = g
	}
	return s
}

func catalog() *gates.Catalog {
	return gates.NewCatalog([]gates.SpecKind{
		{Kind: gates.Kind{Code: kindBill, SpecGate: "G-B2", Approver: capBill}, Risk: gate.RiskHigh, MinApprovers: 1},
		{Kind: gates.Kind{Code: kindTrust, SpecGate: "G-T1", Approver: capTrust}, Risk: gate.RiskIrreversible, MinApprovers: 2},
	})
}

type readRig struct {
	h     http.Handler
	store *store
	audit *auditfake.Fake
}

func newReadRig(t *testing.T) readRig {
	t.Helper()
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	s := seedStore()
	log := auditfake.New()
	h := &gates.HTTP{Gates: s, List: s, Kinds: catalog(), Access: testAccessT{}, Audit: log, DB: txStub{},
		Cursors: codec, Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return readRig{h: m.Handler(), store: s, audit: log}
}

func do(h http.Handler, method, path, user, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

// openAPISchemas compiles schemas from api/openapi.yaml.
var openAPISchemas = sync.OnceValues(func() (*jsonschema.Compiler, error) {
	raw, err := os.ReadFile("../../../api/openapi.yaml")
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

// validate checks body against the schema at ref (a JSON pointer into openapi.json).
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
	if p.Code != code || p.Status != status {
		t.Errorf("problem = %+v, want %d %s", p, status, code)
	}
}

func TestARC_I_051_GateReadProblems(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		user      string
		status    int
		code      string
		zeroCalls bool
	}{
		{"list without principal", "/v1/gates", "", 401, gates.CodeUnauthenticated, true},
		{"get without principal", "/v1/gates/" + gateBillOpen, "", 401, gates.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"list without any approver capability", "/v1/gates", "reader", 403, gates.CodeMissingCapability, true},
		{"get without any approver capability", "/v1/gates/" + gateBillOpen, "reader", 403, gates.CodeMissingCapability, true},
		{"get without the kind's capability", "/v1/gates/" + gateTrustOpen, "partner", 403, gates.CodeMissingCapability, false},
		{"get malformed id", "/v1/gates/nope", "partner", 404, gates.CodeNotFound, true},
		{"get unknown gate", "/v1/gates/" + gateMissing, "partner", 404, gates.CodeNotFound, false},
		// rule: ARC-I-042
		{"get walled subject is 404", "/v1/gates/" + gateHidden, "partner", 404, gates.CodeNotFound, false},
		{"list tampered cursor", "/v1/gates?cursor=abc.def", "partner", 400, api.CodeBadCursor, true},
		{"list bad state", "/v1/gates?state=nope", "partner", 400, server.CodeValidationFailed, true},
		{"list bad decidable_by_me", "/v1/gates?decidable_by_me=maybe", "partner", 400, server.CodeValidationFailed, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReadRig(t)
			rec := do(r.h, "GET", tc.path, tc.user, "", nil)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.store.calls != 0 {
				t.Errorf("gate calls = %d, want 0", r.store.calls)
			}
			if n := len(r.audit.Events()); n != 0 {
				t.Errorf("audit events = %d, want 0", n)
			}
		})
	}
}

func TestGateRoutesUnwiredAnswer503(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&gates.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/v1/gates", "/v1/gates/" + gateBillOpen} {
		checkProblem(t, do(m.Handler(), "GET", p, "partner", "", nil), 503, gates.CodeUnavailable)
	}
}

// rule: ARC-I-051
func TestARC_I_051_GateInboxFilter(t *testing.T) {
	tests := []struct {
		name  string
		query string
		user  string
		want  []string
	}{
		{"default: open gates of held kinds, not my own", "", "partner", []string{gateBillOpen}},
		{"preparer never sees own gate in inbox", "", "alice", []string{gateBillMine}},
		{"trust approver sees trust only", "", "trustee", []string{gateTrustOpen}},
		{"both kinds", "", "both", []string{gateBillOpen, gateBillMine, gateTrustOpen}},
		{"kind outside held kinds is empty", "?kind=" + kindTrust, "partner", []string{}},
		{"decidable_by_me=false lists all visible", "?decidable_by_me=false", "partner",
			[]string{gateBillOpen, gateBillMine, gateTrustOpen, gateBillDone}},
		{"state filter", "?decidable_by_me=false&state=approved", "partner", []string{gateBillDone}},
		{"agent counts as its acting-for user", "", "agent:alice", []string{gateBillMine}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReadRig(t)
			rec := do(r.h, "GET", "/v1/gates"+tc.query, tc.user, "", nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			validate(t, listSchemaPath, rec.Body.Bytes())
			var page struct {
				Data []gates.View `json:"data"`
				Next *string      `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, g := range page.Data {
				got = append(got, g.ID)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("ids = %v, want %v", got, tc.want)
			}
			if page.Next != nil {
				t.Errorf("next_cursor = %q, want null", *page.Next)
			}
		})
	}
}

func TestGateListPagesAndCapsLimit(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantLimit int
		wantN     int
		wantNext  bool
	}{
		{"limit above maximum is capped", "?decidable_by_me=false&limit=5000", api.MaxLimit + 1, 4, false},
		{"default limit", "?decidable_by_me=false", api.DefaultLimit + 1, 4, false},
		{"small page has a next cursor", "?decidable_by_me=false&limit=3", 4, 3, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReadRig(t)
			rec := do(r.h, "GET", "/v1/gates"+tc.query, "partner", "", nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			validate(t, listSchemaPath, rec.Body.Bytes())
			if got := r.store.filters[0].Limit; got != tc.wantLimit {
				t.Errorf("lister limit = %d, want %d", got, tc.wantLimit)
			}
			var page struct {
				Data []gates.View `json:"data"`
				Next *string      `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Data) != tc.wantN || (page.Next != nil) != tc.wantNext {
				t.Fatalf("got %d items, next %v; want %d, %v", len(page.Data), page.Next, tc.wantN, tc.wantNext)
			}
			if !tc.wantNext {
				return
			}
			rec = do(r.h, "GET", "/v1/gates?decidable_by_me=false&cursor="+*page.Next, "partner", "", nil)
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Data) != 1 || page.Data[0].ID != gateBillDone {
				t.Errorf("second page = %+v, want [%s]", page.Data, gateBillDone)
			}
		})
	}
}

// rule: ARC-I-051
func TestGateDetailAuditsEachView(t *testing.T) {
	tests := []struct {
		name  string
		id    string
		user  string
		views int
	}{
		{"approver views once", gateBillOpen, "partner", 1},
		{"approver views twice", gateBillOpen, "partner", 2},
		{"preparer without the kind's capability", gateTrustOpen, "alice", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newReadRig(t)
			for i := 0; i < tc.views; i++ {
				rec := do(r.h, "GET", "/v1/gates/"+tc.id, tc.user, "", nil)
				if rec.Code != 200 {
					t.Fatalf("status = %d: %s", rec.Code, rec.Body)
				}
				validate(t, gateSchemaPath, rec.Body.Bytes())
				if et := rec.Header().Get("ETag"); et != `"1"` {
					t.Errorf("ETag = %q, want \"1\"", et)
				}
				var v gates.View
				if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
					t.Fatal(err)
				}
				if v.PayloadHash != hashA || v.ID != tc.id || v.State != "open" {
					t.Errorf("view = %+v", v)
				}
			}
			ev := r.audit.Events()
			if len(ev) != tc.views {
				t.Fatalf("audit events = %d, want %d", len(ev), tc.views)
			}
			for _, e := range ev {
				if e.Action != gates.ActionViewed || e.Subject.ID != tc.id || e.ActorID != tc.user {
					t.Errorf("audit event = %+v", e)
				}
			}
		})
	}
}
