package webhook_test

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/webhook"
)

const (
	userAdmin  = "0190d000-0000-7000-8000-000000000001"
	userNobody = "0190d000-0000-7000-8000-000000000002"

	hookActive  = "0190c000-0000-7000-8000-000000000001"
	hookExpired = "0190c000-0000-7000-8000-000000000002"
	hookHidden  = "0190c000-0000-7000-8000-0000000000ff"
	hookMissing = "0190c000-0000-7000-8000-0000000000ee"

	signingSecret = "whsec_never-in-a-body-0123456789"
)

var httpNow = time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)

// stubEndpoints is an in-memory EndpointReads plus ExpiryStore that counts calls. Its rows
// hold a secret the routes must never echo.
type stubEndpoints struct {
	mu      sync.Mutex
	rows    []webhook.EndpointInfo
	secrets map[string]string
	calls   int
	setExp  int
}

func (s *stubEndpoints) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubEndpoints) Endpoints(_ context.Context, f webhook.EndpointFilter) ([]webhook.EndpointInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	out := []webhook.EndpointInfo{}
	for _, e := range s.rows {
		if e.ID == hookHidden || (f.After != "" && e.ID <= f.After) {
			continue
		}
		if len(out) == f.Limit {
			break
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *stubEndpoints) Endpoint(_ context.Context, id string) (webhook.EndpointInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for _, e := range s.rows {
		if e.ID == id && id != hookHidden {
			return e, nil
		}
	}
	return webhook.EndpointInfo{}, fmt.Errorf("endpoint %s: %w", id, webhook.ErrUnknownEndpoint)
}

func (s *stubEndpoints) Expiring(context.Context, time.Time) ([]webhook.EndpointExpiry, error) {
	return nil, nil
}

func (s *stubEndpoints) ClaimNotice(context.Context, string, time.Time, time.Duration) (bool, error) {
	return false, nil
}

func (s *stubEndpoints) SetExpiry(_ context.Context, id string, at *time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.setExp++
	for i := range s.rows {
		if s.rows[i].ID == id {
			s.rows[i].ExpiresAt = at
			s.rows[i].Version++
			return nil
		}
	}
	return webhook.ErrUnknownEndpoint
}

func (s *stubEndpoints) events(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.rows {
		if e.ID == id {
			return e.Events
		}
	}
	return nil
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
	g.gates[id] = gate.Gate{ID: id, Kind: kind, Version: 1, Subject: s, Payload: payload,
		PayloadHash: "sha256:" + hex.EncodeToString(sum[:]), Preparer: p, Status: gate.StatusPending,
		CreatedAt: httpNow, ExpiresAt: httpNow.Add(168 * time.Hour)}
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

// effectOnly stands for the effect-owned endpoint writes (create with its secret, change the
// events). A call outside a gate effect fails the test.
type effectOnly struct {
	t     *testing.T
	calls int
}

func (e *effectOnly) AddEndpoint(ctx context.Context, _ webhook.Endpoint) error {
	e.calls++
	// rule: ARC-I-012
	if err := gate.RequireEffect(ctx); err != nil {
		e.t.Errorf("effect-owned AddEndpoint called outside a gate effect")
		return err
	}
	return nil
}

type stubAccess struct{ caps map[string][]string }

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == hookHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a.caps[p.UserID] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

type httpRig struct {
	h       http.Handler
	eps     *stubEndpoints
	gates   *stubGates
	effects *effectOnly
}

func (r httpRig) calls() int { return r.eps.Calls() + len(r.gates.Opens()) }

func newHTTPRig(t *testing.T, n int) httpRig {
	t.Helper()
	past := httpNow.Add(-time.Hour)
	future := httpNow.Add(30 * 24 * time.Hour)
	rg := httpRig{eps: &stubEndpoints{secrets: map[string]string{}}, gates: &stubGates{gates: map[gate.GateID]gate.Gate{}},
		effects: &effectOnly{t: t}}
	rg.eps.rows = []webhook.EndpointInfo{
		{ID: hookActive, URL: "https://example.test/a", Events: []string{"work.item_created"}, Status: webhook.StatusActive, ExpiresAt: &future, Version: 2},
		{ID: hookExpired, URL: "https://example.test/b", Events: []string{"work.item_created"}, Status: webhook.StatusExpired, ExpiresAt: &past, Version: 1},
	}
	for i := 0; i < n; i++ {
		rg.eps.rows = append(rg.eps.rows, webhook.EndpointInfo{ID: fmt.Sprintf("0190c000-0000-7000-8000-%012d", 100+i),
			URL: "https://example.test/x", Status: webhook.StatusActive, Version: 1})
	}
	rg.eps.rows = append(rg.eps.rows, webhook.EndpointInfo{ID: hookHidden, URL: "https://example.test/h", Status: webhook.StatusActive, Version: 1})
	for _, e := range rg.eps.rows {
		rg.eps.secrets[e.ID] = signingSecret
	}
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &webhook.HTTP{
		Endpoints: rg.eps, Expiry: rg.eps, DB: txStub{}, Gates: rg.gates, Idem: idemfake.New(),
		Access:   stubAccess{caps: map[string][]string{userAdmin: {webhook.CapIntegrations}}},
		Cursors:  codec,
		Problems: &server.Problems{},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		},
		Now:   func() time.Time { return httpNow },
		NewID: func() string { return "0190c000-0000-7000-8000-0000000000aa" },
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	rg.h = m.Handler()
	t.Cleanup(func() {
		if rg.effects.calls != 0 {
			t.Errorf("effect-owned endpoint writes = %d, want 0", rg.effects.calls)
		}
	})
	return rg
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
	validate(t, "#/components/schemas/Problem", rec.Body.Bytes())
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code {
		t.Errorf("code = %q, want %q", p.Code, code)
	}
}

func doHTTP(h http.Handler, method, path, user, body string, hdr map[string]string) *httptest.ResponseRecorder {
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

func hdrs(key, ifMatch string) map[string]string {
	h := map[string]string{}
	if key != "" {
		h[idem.HeaderName] = key
	}
	if ifMatch != "" {
		h["If-Match"] = ifMatch
	}
	return h
}

const createJSON = `{"url":"https://example.test/new","events":["work.item_created"]}`

func TestARC_I_051_WebhookEndpointRoutesProblems(t *testing.T) {
	key := hdrs("key-00000001", "")
	patchOK := hdrs("key-00000001", `"2"`)
	activePath := "/v1/webhook-endpoints/" + hookActive
	renew := `{"expires_at":"2027-01-01T00:00:00Z"}`
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
		{"list without principal", "GET", "/v1/webhook-endpoints", "", "", nil, 401, webhook.CodeUnauthenticated, true},
		{"create without principal", "POST", "/v1/webhook-endpoints", "", createJSON, key, 401, webhook.CodeUnauthenticated, true},
		{"patch without principal", "PATCH", activePath, "", renew, patchOK, 401, webhook.CodeUnauthenticated, true},
		{"list without platform.integrations", "GET", "/v1/webhook-endpoints", userNobody, "", nil, 403, webhook.CodeMissingCapability, true},
		{"create without platform.integrations", "POST", "/v1/webhook-endpoints", userNobody, createJSON, key, 403, webhook.CodeMissingCapability, true},
		{"patch without platform.integrations", "PATCH", activePath, userNobody, renew, patchOK, 403, webhook.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"patch hidden endpoint is 404, not 403", "PATCH", "/v1/webhook-endpoints/" + hookHidden, userNobody, renew, patchOK, 404, webhook.CodeNotFound, true},
		{"patch unknown endpoint", "PATCH", "/v1/webhook-endpoints/" + hookMissing, userAdmin, renew, patchOK, 404, webhook.CodeNotFound, false},
		{"tampered cursor", "GET", "/v1/webhook-endpoints?cursor=AAAA.BBBB", userAdmin, "", nil, 400, api.CodeBadCursor, true},
		// rule: ARC-I-007
		{"create missing idempotency key", "POST", "/v1/webhook-endpoints", userAdmin, createJSON, nil, 400, idem.CodeKeyRequired, true},
		{"patch missing idempotency key", "PATCH", activePath, userAdmin, renew, hdrs("", `"2"`), 400, idem.CodeKeyRequired, true},
		{"create unknown body field", "POST", "/v1/webhook-endpoints", userAdmin, `{"url":"https://example.test/n","events":["work.item_created"],"secret":"x"}`, key, 400, server.CodeValidationFailed, true},
		{"create http url", "POST", "/v1/webhook-endpoints", userAdmin, `{"url":"http://example.test/n","events":["work.item_created"]}`, key, 400, server.CodeValidationFailed, true},
		{"create no events", "POST", "/v1/webhook-endpoints", userAdmin, `{"url":"https://example.test/n","events":[]}`, key, 400, server.CodeValidationFailed, true},
		{"create past expiry", "POST", "/v1/webhook-endpoints", userAdmin, `{"url":"https://example.test/n","events":["work.item_created"],"expires_at":"2020-01-01T00:00:00Z"}`, key, 422, server.CodeValidationFailed, true},
		{"patch malformed id", "PATCH", "/v1/webhook-endpoints/nope", userAdmin, renew, patchOK, 400, server.CodeValidationFailed, true},
		{"patch unknown body field", "PATCH", activePath, userAdmin, `{"url":"https://x.test"}`, patchOK, 400, server.CodeValidationFailed, true},
		{"patch empty body", "PATCH", activePath, userAdmin, `{}`, patchOK, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-006
		{"patch missing If-Match", "PATCH", activePath, userAdmin, renew, hdrs("key-00000001", ""), 428, server.CodePreconditionRequired, true},
		{"patch stale version", "PATCH", activePath, userAdmin, renew, hdrs("key-00000001", `"1"`), 412, server.CodeVersionConflict, false},
		{"patch expired endpoint", "PATCH", "/v1/webhook-endpoints/" + hookExpired, userAdmin, renew, hdrs("key-00000001", `"1"`), 409, webhook.CodeWebhookExpired, false},
		{"patch expiry in the past", "PATCH", activePath, userAdmin, `{"expires_at":"2020-01-01T00:00:00Z"}`, patchOK, 422, server.CodeValidationFailed, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newHTTPRig(t, 0)
			checkProblem(t, doHTTP(rg.h, tc.method, tc.path, tc.user, tc.body, tc.hdr), tc.status, tc.code)
			if tc.zeroCalls && rg.calls() != 0 {
				t.Errorf("service calls = %d, want 0", rg.calls())
			}
			if len(rg.gates.Opens()) != 0 || rg.eps.setExp != 0 {
				t.Errorf("gates opened = %v, expiry writes = %d; want none", rg.gates.Opens(), rg.eps.setExp)
			}
		})
	}
}

func TestWebhookEndpointRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&webhook.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, doHTTP(m.Handler(), "GET", "/v1/webhook-endpoints", userAdmin, "", nil), 503, webhook.CodeUnavailable)
}

// rule: ARC-I-012
func TestARC_I_012_WebhookEndpointGateOwned(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		hdr        map[string]string
		wantStatus int
		wantGate   bool
	}{
		{"create opens the scope gate", "POST", "/v1/webhook-endpoints", createJSON, hdrs("key-00000003", ""), 202, true},
		// rule: ARC-I-049
		{"widening events opens the scope gate", "PATCH", "/v1/webhook-endpoints/" + hookActive,
			`{"events":["work.item_created","work.item_moved"]}`, hdrs("key-00000003", `"2"`), 202, true},
		{"expiry-only change applies now", "PATCH", "/v1/webhook-endpoints/" + hookActive,
			`{"expires_at":"2027-01-01T00:00:00Z"}`, hdrs("key-00000003", `"2"`), 200, false},
		{"never expire applies now", "PATCH", "/v1/webhook-endpoints/" + hookActive,
			`{"expires_at":null}`, hdrs("key-00000003", `"2"`), 200, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newHTTPRig(t, 0)
			rec := doHTTP(rg.h, tc.method, tc.path, userAdmin, tc.body, tc.hdr)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.wantStatus, rec.Body)
			}
			// The signing secret never appears in a response body.
			if strings.Contains(rec.Body.String(), signingSecret) {
				t.Fatalf("secret leaked: %s", rec.Body)
			}
			if !tc.wantGate {
				if len(rg.gates.Opens()) != 0 || rg.eps.setExp != 1 || rec.Header().Get("ETag") != `"3"` {
					t.Fatalf("gates %v, expiry writes %d, ETag %q; want none, 1, \"3\"", rg.gates.Opens(), rg.eps.setExp, rec.Header().Get("ETag"))
				}
				return
			}
			validate(t, "#/components/schemas/Gate", rec.Body.Bytes())
			var g struct {
				Kind    string         `json:"kind"`
				Payload map[string]any `json:"payload"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &g); err != nil {
				t.Fatal(err)
			}
			if g.Kind != webhook.GateIntegrationScope || g.Payload["integration_id"] == nil || g.Payload["data_leaves_canada"] != true {
				t.Errorf("gate = %+v", g)
			}
			if rg.eps.setExp != 0 || len(rg.eps.events(hookActive)) != 1 {
				t.Errorf("endpoint changed before the gate decided: expiry writes %d, events %v", rg.eps.setExp, rg.eps.events(hookActive))
			}
		})
	}
	writes, err := server.DeclaredWrites(newHTTPRig(t, 0).h)
	if err != nil {
		t.Fatal(err)
	}
	for _, wr := range writes {
		for _, s := range wr.Sets {
			if s == "webhook_endpoint.events" || s == "webhook_endpoint.active" {
				t.Errorf("%s %s declares gate-owned %s", wr.Method, wr.Pattern, s)
			}
		}
	}
}

// rule: ARC-I-007
func TestARC_I_007_WebhookEndpointIdempotency(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		path    string
		body    string
		other   string
		ifMatch string
	}{
		{"create", "POST", "/v1/webhook-endpoints", createJSON, `{"url":"https://example.test/other","events":["work.item_created"]}`, ""},
		{"widen", "PATCH", "/v1/webhook-endpoints/" + hookActive, `{"events":["work.item_moved"]}`, `{"events":["work.item_closed"]}`, `"2"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newHTTPRig(t, 0)
			hdr := hdrs("key-00000042", tc.ifMatch)
			first := doHTTP(rg.h, tc.method, tc.path, userAdmin, tc.body, hdr)
			if first.Code != http.StatusAccepted {
				t.Fatalf("first status = %d (%s)", first.Code, first.Body)
			}
			again := doHTTP(rg.h, tc.method, tc.path, userAdmin, tc.body, hdr)
			if again.Code != http.StatusAccepted || again.Body.String() != first.Body.String() || again.Header().Get(idem.ReplayedHeader) != "true" {
				t.Errorf("replay = %d %s, want the first body", again.Code, again.Body)
			}
			// The kernel idem store answers 422 for a reused key with a different body (ARC-I-008).
			checkProblem(t, doHTTP(rg.h, tc.method, tc.path, userAdmin, tc.other, hdr), http.StatusUnprocessableEntity, idem.CodeKeyReused)
			if opens := rg.gates.Opens(); len(opens) != 1 {
				t.Errorf("gates opened = %v, want one", opens)
			}
		})
	}
}

func TestWebhookEndpointListPages(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		query     string
		wantItems []int
	}{
		{"one page", 0, "", []int{2}},
		{"pages of two", 3, "?limit=2", []int{2, 2, 1}},
		{"limit above maximum is capped", api.MaxLimit, "?limit=5000", []int{api.MaxLimit, 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newHTTPRig(t, tc.n)
			path := "/v1/webhook-endpoints" + tc.query
			seen := map[string]bool{}
			for i, want := range tc.wantItems {
				rec := doHTTP(rg.h, "GET", path, userAdmin, "", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d status = %d (%s)", i, rec.Code, rec.Body)
				}
				if strings.Contains(rec.Body.String(), signingSecret) {
					t.Fatalf("secret leaked: %s", rec.Body)
				}
				var page struct {
					Items []struct {
						ID string `json:"id"`
					} `json:"data"`
					Next string `json:"next_cursor"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if len(page.Items) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Items), want)
				}
				for _, it := range page.Items {
					if it.ID == hookHidden || seen[it.ID] {
						t.Errorf("page %d repeats or leaks %s", i, it.ID)
					}
					seen[it.ID] = true
				}
				last := i == len(tc.wantItems)-1
				if (page.Next != "") == last {
					t.Fatalf("page %d next = %q, last = %v", i, page.Next, last)
				}
				path = "/v1/webhook-endpoints?cursor=" + page.Next
			}
		})
	}
}
