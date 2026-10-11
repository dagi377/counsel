package access_test

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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/kernel/api"
	kaudit "github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	userAdmin   = "0190d000-0000-7000-8000-000000000001"
	userAuditor = "0190d000-0000-7000-8000-000000000002"
	userNobody  = "0190d000-0000-7000-8000-000000000003"
	userClerk   = "0190d000-0000-7000-8000-000000000004"
	userHidden  = "0190d000-0000-7000-8000-0000000000ff"
	userMissing = "0190d000-0000-7000-8000-0000000000ee"

	roleAdmin   = "0190c000-0000-7000-8000-000000000001"
	roleClerk   = "0190c000-0000-7000-8000-000000000002"
	roleMissing = "0190c000-0000-7000-8000-0000000000ee"
)

// stubDirectory is an in-memory access.Directory that counts calls. Admin roles are those
// whose id is roleAdmin; demoting the only active admin is refused.
type stubDirectory struct {
	mu    sync.Mutex
	users []access.User
	calls int
}

func (d *stubDirectory) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func (d *stubDirectory) Users(_ context.Context, f access.Filter) ([]access.User, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	out := []access.User{}
	for _, u := range d.users {
		if u.ID == userHidden || (f.After != "" && u.ID <= f.After) {
			continue
		}
		if len(out) == f.Limit {
			break
		}
		out = append(out, u)
	}
	return out, nil
}

func (d *stubDirectory) User(_ context.Context, id string) (access.User, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	for _, u := range d.users {
		if u.ID == id && id != userHidden {
			return u, nil
		}
	}
	return access.User{}, fmt.Errorf("user: %w", access.ErrNotFound)
}

func (d *stubDirectory) CheckRoleChange(_ context.Context, userID, roleID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if roleID != roleAdmin && roleID != roleClerk {
		return access.ErrUnknownRole
	}
	admins := 0
	var target access.User
	for _, u := range d.users {
		if u.Active && u.RoleID == roleAdmin {
			admins++
		}
		if u.ID == userID {
			target = u
		}
	}
	if target.RoleID == roleAdmin && roleID != roleAdmin && admins <= 1 {
		return access.ErrLastAdmin
	}
	return nil
}

func (d *stubDirectory) role(id string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, u := range d.users {
		if u.ID == id {
			return u.RoleID
		}
	}
	return ""
}

type stubRoles struct{ calls int }

func (s *stubRoles) Roles(context.Context) ([]access.Role, error) {
	s.calls++
	return []access.Role{
		{ID: roleAdmin, Key: "admin", Name: "Admin", Capabilities: []string{access.CapRoles, access.CapUsers}},
		{ID: roleClerk, Key: "clerk", Name: "Clerk"},
	}, nil
}

type stubAudit struct {
	events []kaudit.Event
	calls  int
}

func (s *stubAudit) Events(_ context.Context, f access.Filter) ([]kaudit.Event, error) {
	s.calls++
	after := int64(0)
	if f.After != "" {
		n, err := strconv.ParseInt(f.After, 10, 64)
		if err != nil {
			return nil, err
		}
		after = n
	}
	out := []kaudit.Event{}
	for _, e := range s.events {
		if int64(e.Seq) <= after {
			continue
		}
		if len(out) == f.Limit {
			break
		}
		out = append(out, e)
	}
	return out, nil
}

// countingMasker masks every detail value except "action_ref".
type countingMasker struct{ calls int }

func (m *countingMasker) Mask(d map[string]string) map[string]string {
	m.calls++
	out := map[string]string{}
	for k, v := range d {
		if k == "action_ref" {
			out[k] = v
			continue
		}
		out[k] = kaudit.Masked
	}
	return out
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

// effectOnly stands for the effect-owned role assignment (the core.role_change effect). A
// call outside a gate effect fails the test.
type effectOnly struct {
	t     *testing.T
	calls int
}

func (e *effectOnly) AssignRole(ctx context.Context, _, _ string) error {
	e.calls++
	// rule: ARC-I-012
	if err := gate.RequireEffect(ctx); err != nil {
		e.t.Errorf("effect-owned AssignRole called outside a gate effect")
		return err
	}
	return nil
}

type stubAccess struct{ caps map[string][]string }

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == userHidden {
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
	dir     *stubDirectory
	roles   *stubRoles
	audit   *stubAudit
	masker  *countingMasker
	gates   *stubGates
	effects *effectOnly
}

func (r rig) calls() int {
	return r.dir.Calls() + r.roles.calls + r.audit.calls + len(r.gates.Opens())
}

func newRig(t *testing.T, n int) rig {
	t.Helper()
	rg := rig{dir: &stubDirectory{}, roles: &stubRoles{}, audit: &stubAudit{}, masker: &countingMasker{},
		gates: &stubGates{gates: map[gate.GateID]gate.Gate{}}, effects: &effectOnly{t: t}}
	rg.dir.users = []access.User{
		{ID: userAdmin, Email: "a@example.com", DisplayName: "A", RoleID: roleAdmin, Active: true, Version: 3},
		{ID: userAuditor, Email: "b@example.com", DisplayName: "B", RoleID: roleClerk, Active: true, Version: 1},
		{ID: userNobody, Email: "c@example.com", DisplayName: "C", RoleID: roleClerk, Active: true, Version: 1},
		{ID: userClerk, Email: "d@example.com", DisplayName: "D", RoleID: roleClerk, Active: true, Version: 2},
	}
	for i := 0; i < n; i++ {
		rg.dir.users = append(rg.dir.users, access.User{ID: fmt.Sprintf("0190d000-0000-7000-8000-%012d", 100+i),
			Email: "x@example.com", DisplayName: "X", RoleID: roleClerk, Active: true, Version: 1})
	}
	rg.dir.users = append(rg.dir.users, access.User{ID: userHidden, RoleID: roleClerk, Version: 1})
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 3; i++ {
		rg.audit.events = append(rg.audit.events, kaudit.Event{Seq: kaudit.Seq(i), At: at, ActorKind: "user", ActorID: userAdmin,
			Action: "gate.decide", Subject: kaudit.Subject{Kind: "core.user", ID: userClerk},
			Detail: map[string]string{"action_ref": "r1", "note": "client Jane Doe"}})
	}
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &access.HTTP{
		Users: rg.dir, Roles: rg.roles, Audit: rg.audit, Masker: rg.masker, DB: txStub{}, Gates: rg.gates,
		Idem: idemfake.New(),
		Access: stubAccess{caps: map[string][]string{
			userAdmin:   {access.CapUsers, access.CapRoles},
			userAuditor: {access.CapAuditLog},
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
			t.Errorf("effect-owned role writes = %d, want 0", rg.effects.calls)
		}
	})
	return rg
}

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

func roleBody(role string) string { return fmt.Sprintf(`{"role_id":%q}`, role) }

func putHdr(key, ifMatch string) map[string]string {
	h := map[string]string{}
	if key != "" {
		h[idem.HeaderName] = key
	}
	if ifMatch != "" {
		h["If-Match"] = ifMatch
	}
	return h
}

func TestARC_I_051_AdminRoutesProblems(t *testing.T) {
	ok := putHdr("key-00000001", `"2"`)
	rolesPath := "/v1/users/" + userClerk + "/roles"
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
		{"users without principal", "GET", "/v1/users", "", "", nil, 401, access.CodeUnauthenticated, true},
		{"roles without principal", "GET", "/v1/roles", "", "", nil, 401, access.CodeUnauthenticated, true},
		{"audit without principal", "GET", "/v1/audit-events", "", "", nil, 401, access.CodeUnauthenticated, true},
		{"put without principal", "PUT", rolesPath, "", roleBody(roleAdmin), ok, 401, access.CodeUnauthenticated, true},
		{"users without admin.users", "GET", "/v1/users", userAuditor, "", nil, 403, access.CodeMissingCapability, true},
		{"roles without admin.roles", "GET", "/v1/roles", userAuditor, "", nil, 403, access.CodeMissingCapability, true},
		{"audit without admin.audit_log", "GET", "/v1/audit-events", userAdmin, "", nil, 403, access.CodeMissingCapability, true},
		{"put without admin.roles", "PUT", rolesPath, userAuditor, roleBody(roleAdmin), ok, 403, access.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"put on hidden user is 404, not 403", "PUT", "/v1/users/" + userHidden + "/roles", userNobody, roleBody(roleAdmin), ok, 404, access.CodeNotFound, true},
		{"put on unknown user", "PUT", "/v1/users/" + userMissing + "/roles", userAdmin, roleBody(roleAdmin), ok, 404, access.CodeNotFound, false},
		{"tampered users cursor", "GET", "/v1/users?cursor=AAAA.BBBB", userAdmin, "", nil, 400, api.CodeBadCursor, true},
		{"tampered audit cursor", "GET", "/v1/audit-events?cursor=AAAA.BBBB", userAuditor, "", nil, 400, api.CodeBadCursor, true},
		{"bad limit", "GET", "/v1/users?limit=x", userAdmin, "", nil, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-007
		{"put missing idempotency key", "PUT", rolesPath, userAdmin, roleBody(roleAdmin), putHdr("", `"2"`), 400, idem.CodeKeyRequired, true},
		{"put unknown body field", "PUT", rolesPath, userAdmin, `{"role_id":"` + roleAdmin + `","colour":"red"}`, ok, 400, server.CodeValidationFailed, true},
		{"put malformed role id", "PUT", rolesPath, userAdmin, roleBody("admin"), ok, 400, server.CodeValidationFailed, true},
		{"put malformed user id", "PUT", "/v1/users/nope/roles", userAdmin, roleBody(roleAdmin), ok, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-006
		{"put missing If-Match", "PUT", rolesPath, userAdmin, roleBody(roleAdmin), putHdr("key-00000001", ""), 428, server.CodePreconditionRequired, true},
		{"put stale version", "PUT", rolesPath, userAdmin, roleBody(roleAdmin), putHdr("key-00000001", `"1"`), 412, server.CodeVersionConflict, false},
		{"put unknown role", "PUT", rolesPath, userAdmin, roleBody(roleMissing), ok, 422, access.CodeUnknownRole, false},
		{"put demotes the last admin", "PUT", "/v1/users/" + userAdmin + "/roles", userAdmin, roleBody(roleClerk), putHdr("key-00000001", `"3"`), 409, access.CodeLastAdmin, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newRig(t, 0)
			checkProblem(t, do(rg.h, tc.method, tc.path, tc.user, tc.body, tc.hdr), tc.status, tc.code)
			if tc.zeroCalls && rg.calls() != 0 {
				t.Errorf("service calls = %d, want 0", rg.calls())
			}
			if len(rg.gates.Opens()) != 0 {
				t.Errorf("gates opened = %v, want none", rg.gates.Opens())
			}
		})
	}
}

func TestAdminRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&access.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/users", "/v1/roles", "/v1/audit-events"} {
		checkProblem(t, do(m.Handler(), "GET", path, userAdmin, "", nil), 503, access.CodeUnavailable)
	}
}

// rule: ARC-I-012
func TestARC_I_012_RoleChangeOnlyProposes(t *testing.T) {
	rg := newRig(t, 0)
	rec := do(rg.h, "PUT", "/v1/users/"+userClerk+"/roles", userAdmin, roleBody(roleAdmin), putHdr("key-00000002", `"2"`))
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
	if g.Kind != access.GateRoleChange || g.Payload["role_id"] != roleAdmin {
		t.Errorf("gate = %+v, want %s for role %s", g, access.GateRoleChange, roleAdmin)
	}
	if got := rg.dir.role(userClerk); got != roleClerk {
		t.Errorf("assigned role after 202 = %s, want unchanged %s", got, roleClerk)
	}
	writes, err := server.DeclaredWrites(rg.h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || writes[0].Method != http.MethodPut || len(writes[0].Sets) != 0 {
		t.Errorf("writes = %+v, want PUT /users/{user_id}/roles declaring nothing", writes)
	}
}

// rule: ARC-I-007
func TestARC_I_007_RoleChangeIdempotency(t *testing.T) {
	rg := newRig(t, 0)
	path := "/v1/users/" + userClerk + "/roles"
	hdr := putHdr("key-00000042", `"2"`)
	first := do(rg.h, "PUT", path, userAdmin, roleBody(roleAdmin), hdr)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status = %d (%s)", first.Code, first.Body)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", roleBody(roleAdmin), http.StatusAccepted, ""},
		// The kernel idem store answers 422 for a reused key with a different body (ARC-I-008).
		{"same key, different body", roleBody(roleClerk), http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(rg.h, "PUT", path, userAdmin, tc.body, hdr)
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

func TestListRoles(t *testing.T) {
	rg := newRig(t, 0)
	rec := do(rg.h, "GET", "/v1/roles", userAdmin, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	var out struct {
		Data []struct {
			Key          string   `json:"key"`
			Capabilities []string `json:"capabilities"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Data) != 2 || out.Data[0].Key != "admin" || len(out.Data[0].Capabilities) != 2 || out.Data[1].Capabilities == nil {
		t.Errorf("roles = %+v", out.Data)
	}
}

// rule: ARC-I-044
func TestAuditEventsMasked(t *testing.T) {
	rg := newRig(t, 0)
	rec := do(rg.h, "GET", "/v1/audit-events?limit=2", userAuditor, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "Jane Doe") {
		t.Fatalf("unmasked detail in body: %s", rec.Body)
	}
	var page struct {
		Items []struct {
			Seq    int64             `json:"seq"`
			Detail map[string]string `json:"detail"`
		} `json:"data"`
		Next string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Next == "" || page.Items[0].Detail["action_ref"] != "r1" || page.Items[0].Detail["note"] != kaudit.Masked {
		t.Fatalf("page = %+v", page)
	}
	if rg.masker.calls != 2 {
		t.Errorf("masker calls = %d, want 2", rg.masker.calls)
	}
	rec = do(rg.h, "GET", "/v1/audit-events?cursor="+page.Next, userAuditor, "", nil)
	page.Items, page.Next = nil, ""
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Seq != 3 || page.Next != "" {
		t.Fatalf("second page = %+v", page)
	}
}

func TestListUsersPages(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		query     string
		wantItems []int
	}{
		{"one page", 0, "", []int{4}},
		{"pages of three", 3, "?limit=3", []int{3, 3, 1}},
		{"limit above maximum is capped", api.MaxLimit, "?limit=5000", []int{api.MaxLimit, 4}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rg := newRig(t, tc.n)
			path := "/v1/users" + tc.query
			seen := map[string]bool{}
			for i, want := range tc.wantItems {
				rec := do(rg.h, "GET", path, userAdmin, "", nil)
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d status = %d (%s)", i, rec.Code, rec.Body)
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
					if it.ID == userHidden || seen[it.ID] {
						t.Errorf("page %d repeats or leaks %s", i, it.ID)
					}
					seen[it.ID] = true
				}
				last := i == len(tc.wantItems)-1
				if (page.Next != "") == last {
					t.Fatalf("page %d next = %q, last = %v", i, page.Next, last)
				}
				path = "/v1/users?cursor=" + page.Next
			}
		})
	}
}
