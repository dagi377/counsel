package deadline_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/deadline/deadlinefake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// cfg is a test configuration; the kernel takes every app name from it.
var cfg = deadline.HTTPConfig{CapView: "t.view", CapEdit: "t.edit", SubjectKind: "t.case", SubjectKey: "case_id",
	DeadlineKind: "t.deadline", ConfirmGate: "t.confirm"}

const (
	caseA      = "0190a000-0000-7000-8000-0000000000a1"
	caseWall   = "0190a000-0000-7000-8000-0000000000af"
	dlHidden   = "0190d000-0000-7000-8000-0000000000ff"
	dlMissing  = "0190d000-0000-7000-8000-0000000000fe"
	key1       = "0190e000-0000-7000-8000-000000000301"
	problemRef = "#/components/schemas/Problem"
)

var users = map[string][]string{
	"clerk":  {cfg.CapEdit, cfg.CapView},
	"viewer": {cfg.CapView},
	"reader": {"contacts.view"},
}

type testAccess struct{}

func (testAccess) Can(_ context.Context, p registry.Principal, c string, s registry.Subject) (bool, error) {
	if s.ID == dlHidden || s.ID == caseWall {
		return false, identity.ErrNotVisible
	}
	return slices.Contains(users[p.UserID], c), nil
}

func testPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get("X-Test-User")
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// book wraps the deadline fake: it computes with the fake, stores with uuid ids, lists by
// id and counts every call. Confirm is effect-only and must never be called by a route.
type book struct {
	*deadlinefake.Fake
	stored   []deadline.Deadline
	calls    int
	confirms int
}

func (b *book) Compute(ctx context.Context, pack, rule string, trigger deadline.Date, s deadline.Subject) ([]deadline.Proposed, error) {
	b.calls++
	return b.Fake.Compute(ctx, pack, rule, trigger, s)
}

func (b *book) Save(_ context.Context, _ pgx.Tx, p deadline.Proposed) (deadline.DeadlineID, error) {
	b.calls++
	id := deadline.DeadlineID(fmt.Sprintf("0190d000-0000-7000-8000-%012d", len(b.stored)+1))
	b.stored = append(b.stored, deadline.Deadline{ID: id, Proposed: p, State: deadline.StateProposed})
	return id, nil
}

func (b *book) Confirm(context.Context, pgx.Tx, deadline.DeadlineID, deadline.Principal) error {
	b.confirms++
	return nil
}

func (b *book) Get(_ context.Context, id deadline.DeadlineID) (deadline.Deadline, error) {
	b.calls++
	for _, d := range b.stored {
		if d.ID == id {
			return d, nil
		}
	}
	return deadline.Deadline{}, deadline.ErrNotFound
}

func (b *book) ListPage(_ context.Context, f deadline.ListFilter) ([]deadline.Deadline, error) {
	b.calls++
	var out []deadline.Deadline
	for _, d := range b.stored {
		if d.ID > f.After && (f.Subject.ID == "" || d.Proposed.Subject.ID == f.Subject.ID) &&
			(f.State == "" || d.State == f.State) && len(out) < f.Limit {
			out = append(out, d)
		}
	}
	return out, nil
}

// countingGates counts Open calls and gives the opened gate a uuid id.
type countingGates struct {
	*gatefake.Fake
	opens    int
	subjects []gate.Subject
}

func (g *countingGates) Open(ctx context.Context, tx pgx.Tx, kind string, s gate.Subject, payload json.RawMessage, p gate.Principal) (gate.GateID, error) {
	g.opens++
	g.subjects = append(g.subjects, s)
	return g.Fake.Open(ctx, tx, kind, s, payload, p)
}

func (g *countingGates) Get(ctx context.Context, id gate.GateID) (gate.Gate, error) {
	out, err := g.Fake.Get(ctx, id)
	out.ID = "0190f000-0000-7000-8000-000000000001"
	return out, err
}

type rig struct {
	h     http.Handler
	book  *book
	gates *countingGates
}

func newRig(t *testing.T) rig {
	t.Helper()
	fake := deadlinefake.New()
	err := fake.LoadPack(context.Background(), deadline.Pack{ID: "civ", Version: "1",
		Rules: []deadline.Rule{{ID: "defence", Trigger: "served", Mode: deadline.Calendar, Days: 20, Roll: deadline.RollForward}}})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }
	eng := gatefake.New(now, gatefake.Caps{})
	if err := eng.Register(gate.GateKind{Slug: cfg.ConfirmGate, Approver: "t.verify", Risk: gate.RiskHigh, MinApprovers: 1,
		TTL: 24 * time.Hour, Effect: func(context.Context, pgx.Tx, registry.Approved) error { return nil }}); err != nil {
		t.Fatal(err)
	}
	b := &book{Fake: fake}
	gates := &countingGates{Fake: eng}
	h := &deadline.HTTP{Config: cfg, Engine: b, Lister: b, Gates: gates, DB: txStub{}, Access: testAccess{},
		Cursors: codec, Idem: idemfake.New(), Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return rig{h: m.Handler(), book: b, gates: gates}
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

var openAPISchemas = sync.OnceValues(func() (*jsonschema.Compiler, error) {
	raw, err := os.ReadFile("../../api/openapi.yaml")
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
	validate(t, problemRef, rec.Body.Bytes())
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code || p.Status != status {
		t.Errorf("problem = %+v, want %d %s", p, status, code)
	}
}

func proposeBody(subject, rule, extra string) string {
	return `{"case_id":"` + subject + `","title":"Statement of defence","pack_id":"civ","rule_id":"` + rule +
		`","trigger_date":"2026-10-02"` + extra + `}`
}

var withKey = map[string]string{idem.HeaderName: key1}

func TestARC_I_051_DeadlineProblems(t *testing.T) {
	ok := proposeBody(caseA, "defence", "")
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
		{"list without principal", "GET", "/v1/deadlines", "", "", nil, 401, deadline.CodeUnauthenticated, true},
		{"propose without principal", "POST", "/v1/deadlines", "", ok, withKey, 401, deadline.CodeUnauthenticated, true},
		{"get without principal", "GET", "/v1/deadlines/" + dlMissing, "", "", nil, 401, deadline.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"list without view", "GET", "/v1/deadlines", "reader", "", nil, 403, deadline.CodeMissingCapability, true},
		{"propose without edit", "POST", "/v1/deadlines", "viewer", ok, withKey, 403, deadline.CodeMissingCapability, true},
		{"get without view", "GET", "/v1/deadlines/" + dlMissing, "reader", "", nil, 403, deadline.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"get walled deadline is 404", "GET", "/v1/deadlines/" + dlHidden, "viewer", "", nil, 404, deadline.CodeNotFound, true},
		{"propose on walled subject is 404", "POST", "/v1/deadlines", "clerk", proposeBody(caseWall, "defence", ""), withKey, 404, deadline.CodeNotFound, true},
		{"list walled subject is 404", "GET", "/v1/deadlines?case_id=" + caseWall, "viewer", "", nil, 404, deadline.CodeNotFound, true},
		{"get unknown deadline", "GET", "/v1/deadlines/" + dlMissing, "viewer", "", nil, 404, deadline.CodeNotFound, false},
		{"get malformed id", "GET", "/v1/deadlines/nope", "viewer", "", nil, 400, server.CodeValidationFailed, true},
		{"list tampered cursor", "GET", "/v1/deadlines?cursor=abc.def", "viewer", "", nil, 400, api.CodeBadCursor, true},
		{"list bad state", "GET", "/v1/deadlines?state=done", "viewer", "", nil, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-007
		{"propose missing Idempotency-Key", "POST", "/v1/deadlines", "clerk", ok, nil, 400, idem.CodeKeyRequired, true},
		{"propose unknown field", "POST", "/v1/deadlines", "clerk", proposeBody(caseA, "defence", `,"state":"confirmed"`), withKey, 400, server.CodeValidationFailed, true},
		{"propose bad date", "POST", "/v1/deadlines", "clerk", strings.Replace(ok, "2026-10-02", "10/02/2026", 1), withKey, 400, server.CodeValidationFailed, true},
		{"propose malformed subject", "POST", "/v1/deadlines", "clerk", proposeBody("nope", "defence", ""), withKey, 400, server.CodeValidationFailed, true},
		{"propose unknown rule", "POST", "/v1/deadlines", "clerk", proposeBody(caseA, "appeal", ""), withKey, 422, deadline.CodeUnknownRule, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			rec := do(r.h, tc.method, tc.path, tc.user, tc.body, tc.hdr)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.book.calls != 0 {
				t.Errorf("service calls = %d, want 0", r.book.calls)
			}
			if r.gates.opens != 0 || r.book.confirms != 0 {
				t.Errorf("gate opens = %d confirms = %d, want 0", r.gates.opens, r.book.confirms)
			}
		})
	}
}

func TestDeadlinesUnconfiguredAnswer503(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&deadline.HTTP{Config: cfg}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/deadlines", "viewer", "", nil), 503, deadline.CodeUnavailable)
}

// rule: ARC-I-012
func TestARC_I_012_ProposeSavesProposedAndOpensGate(t *testing.T) {
	r := newRig(t)
	rec := do(r.h, "POST", "/v1/deadlines", "clerk", proposeBody(caseA, "defence", ""), withKey)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	validate(t, "#/components/schemas/Gate", rec.Body.Bytes())
	var g struct {
		Kind    string `json:"kind"`
		Payload struct {
			CaseID string `json:"case_id"`
			DueOn  string `json:"due_on"`
			Title  string `json:"title"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
		t.Fatal(err)
	}
	// 2026-10-02 + 20 calendar days is Thursday 2026-10-22.
	if g.Kind != cfg.ConfirmGate || g.Payload.CaseID != caseA || g.Payload.DueOn != "2026-10-22" || g.Payload.Title == "" {
		t.Errorf("gate = %+v", g)
	}
	if len(r.book.stored) != 1 || r.book.stored[0].State != deadline.StateProposed {
		t.Fatalf("stored = %+v, want one proposed", r.book.stored)
	}
	if r.gates.opens != 1 || r.gates.subjects[0] != (gate.Subject{Kind: cfg.DeadlineKind, ID: string(r.book.stored[0].ID)}) {
		t.Errorf("gate opens = %d subjects = %v", r.gates.opens, r.gates.subjects)
	}
	// Confirmation is the gate effect's, never the route's.
	if r.book.confirms != 0 {
		t.Errorf("Confirm calls = %d, want 0", r.book.confirms)
	}
	get := do(r.h, "GET", "/v1/deadlines/"+string(r.book.stored[0].ID), "viewer", "", nil)
	if get.Code != 200 || !strings.Contains(get.Body.String(), `"state":"proposed"`) {
		t.Errorf("get = %d %s, want proposed", get.Code, get.Body)
	}
}

// rule: ARC-I-007
func TestARC_I_007_ProposeIdempotency(t *testing.T) {
	tests := []struct {
		name       string
		second     string
		wantStatus int
	}{
		{"same key same body replays", proposeBody(caseA, "defence", ""), http.StatusAccepted},
		{"same key different body refused", strings.Replace(proposeBody(caseA, "defence", ""), "2026-10-02", "2026-10-05", 1), http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			first := do(r.h, "POST", "/v1/deadlines", "clerk", proposeBody(caseA, "defence", ""), withKey)
			if first.Code != http.StatusAccepted {
				t.Fatalf("first = %d (%s)", first.Code, first.Body)
			}
			second := do(r.h, "POST", "/v1/deadlines", "clerk", tc.second, withKey)
			if second.Code != tc.wantStatus {
				t.Fatalf("second = %d, want %d (%s)", second.Code, tc.wantStatus, second.Body)
			}
			if tc.wantStatus == http.StatusAccepted && second.Body.String() != first.Body.String() {
				t.Errorf("replay body differs")
			}
			if r.gates.opens != 1 || len(r.book.stored) != 1 {
				t.Errorf("opens = %d stored = %d, want 1 and 1", r.gates.opens, len(r.book.stored))
			}
		})
	}
}

func TestDeadlineList(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		want     int
		wantNext bool
	}{
		{"all", "", 2, false},
		{"page of one", "?limit=1", 1, true},
		{"limit capped", "?limit=100000", 2, false},
		{"by subject", "?case_id=" + caseA, 2, false},
		{"by state", "?state=confirmed", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			for i, key := range []string{key1, "0190e000-0000-7000-8000-000000000302"} {
				body := strings.Replace(proposeBody(caseA, "defence", ""), "2026-10-02", fmt.Sprintf("2026-10-0%d", i+2), 1)
				if rec := do(r.h, "POST", "/v1/deadlines", "clerk", body, map[string]string{idem.HeaderName: key}); rec.Code != 202 {
					t.Fatalf("seed = %d (%s)", rec.Code, rec.Body)
				}
			}
			rec := do(r.h, "GET", "/v1/deadlines"+tc.query, "viewer", "", nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var page struct {
				Data       []map[string]any `json:"data"`
				NextCursor *string          `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Data) != tc.want || (page.NextCursor != nil) != tc.wantNext {
				t.Errorf("got %d next %v, want %d next %v", len(page.Data), page.NextCursor, tc.want, tc.wantNext)
			}
		})
	}
}
