package matter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	idA      = "0190a000-0000-7000-8000-00000000000a"
	idB      = "0190a000-0000-7000-8000-00000000000b"
	idC      = "0190a000-0000-7000-8000-00000000000c"
	idHidden = "0190a000-0000-7000-8000-0000000000ff"
)

// stubQuery is a matter.Query that counts calls. Hidden matters stand for rows the caller's
// RLS session cannot read: List leaves them out and Get reports them not found.
type stubQuery struct {
	mu      sync.Mutex
	matters []Matter
	hidden  map[ID]bool
	calls   int
}

// listPage is the openapi list page shape, {data, next_cursor} as GET /bills declares it.
// GET /matters declares no 200 schema of its own, so its pages are checked against this shape.
var listPage = jsonschema.MustCompileString("list-page.json",
	`{"type":"object","required":["data","next_cursor"],"properties":{"data":{"type":"array"},"next_cursor":{"type":["string","null"]}}}`)

// validatePage checks that body is a whole openapi list page.
func validatePage(t *testing.T, body []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if err := listPage.Validate(v); err != nil {
		t.Errorf("body is not an openapi list page: %v (%s)", err, body)
	}
}

func (q *stubQuery) count() {
	q.mu.Lock()
	q.calls++
	q.mu.Unlock()
}

func (q *stubQuery) Calls() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.calls
}

func (q *stubQuery) Get(_ context.Context, id ID) (Matter, error) {
	q.count()
	for _, m := range q.matters {
		if m.ID == id && !q.hidden[id] {
			return m, nil
		}
	}
	return Matter{}, fmt.Errorf("get %s: %w", id, ErrNotFound)
}

func (q *stubQuery) List(context.Context, Filter) ([]Matter, error) {
	q.count()
	var out []Matter
	for _, m := range q.matters {
		if !q.hidden[m.ID] {
			out = append(out, m)
		}
	}
	return out, nil
}

func (q *stubQuery) Members(context.Context, ID) ([]Member, error) {
	q.count()
	return nil, nil
}

// stubAccess holds capabilities per user; walled subjects are not visible to anyone.
type stubAccess struct {
	caps   map[string][]string
	walled map[string]bool
}

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	held := false
	for _, c := range a.caps[p.UserID] {
		if c == capability {
			held = true
		}
	}
	if s.ID != "" && a.walled[s.ID] {
		return false, identity.ErrNotVisible
	}
	return held, nil
}

var testKey = []byte("0123456789abcdef0123456789abcdef")

func newRig(t *testing.T) (*HTTP, *stubQuery, http.Handler) {
	t.Helper()
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	q := &stubQuery{
		matters: []Matter{
			{ID: idA, Number: "2026-001", ShortName: "Alpha", ClientID: "c1", PracticeAreaID: "pa", ResponsibleLawyerID: "u9",
				Status: Open, Visibility: FirmOpen, OpenDate: day, RetentionClass: DefaultRetentionClass, CreatedAt: day, RowVersion: 3},
			{ID: idB, Number: "2026-002", ShortName: "Beta", ClientID: "c2", PracticeAreaID: "pa", ResponsibleLawyerID: "u9",
				Status: Pending, Visibility: TeamOnly, RetentionClass: DefaultRetentionClass, CreatedAt: day, RowVersion: 1},
			{ID: idC, Number: "2026-003", ShortName: "Gamma", ClientID: "c3", PracticeAreaID: "pa", ResponsibleLawyerID: "u9",
				Status: Open, Visibility: FirmOpen, RetentionClass: DefaultRetentionClass, CreatedAt: day, RowVersion: 1},
			{ID: idHidden, Number: "2026-004", ShortName: "Hidden", ClientID: "c4", PracticeAreaID: "pa", ResponsibleLawyerID: "u9",
				Status: Open, Visibility: Restricted, RetentionClass: DefaultRetentionClass, CreatedAt: day, RowVersion: 1},
		},
		hidden: map[ID]bool{idHidden: true},
	}
	codec, err := api.NewCodec(testKey)
	if err != nil {
		t.Fatal(err)
	}
	h := &HTTP{
		Query: q,
		Access: stubAccess{
			caps: map[string][]string{
				"viewer":    {CapView, CapDirectory},
				"directory": {CapDirectory},
				"nobody":    nil,
				"editor":    {CapView, CapDirectory, CapEdit, CapOpen},
			},
			walled: map[string]bool{idHidden: true},
		},
		Cursors:  codec,
		Problems: &server.Problems{},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			if u == "" {
				return registry.Principal{}, false
			}
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, true
		},
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return h, q, m.Handler()
}

// openAPISchemas compiles response schemas from api/openapi.yaml.
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

// checkProblem asserts rec is a problem+json body that validates against the Problem schema
// and carries status and code.
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

func do(h http.Handler, method, path, user string, body []byte, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
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

type pageBody struct {
	Items []map[string]any `json:"data"`
	Next  string           `json:"next_cursor"`
}

func TestARC_I_042_MatterReadsAuthz(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		user      string
		status    int
		code      string
		wantCalls bool
	}{
		{"list without principal", "/v1/matters", "", 401, CodeUnauthenticated, false},
		{"get without principal", "/v1/matters/" + idA, "", 401, CodeUnauthenticated, false},
		{"list without capability", "/v1/matters", "nobody", 403, CodeMissingCapability, false},
		{"get without capability", "/v1/matters/" + idA, "nobody", 403, CodeMissingCapability, false},
		{"get walled matter is 404, not 403", "/v1/matters/" + idHidden, "viewer", 404, CodeNotFound, false},
		{"get walled matter without capability is 404", "/v1/matters/" + idHidden, "nobody", 404, CodeNotFound, false},
		{"get unknown matter", "/v1/matters/0190a000-0000-7000-8000-000000000999", "viewer", 404, CodeNotFound, true},
		{"get malformed id", "/v1/matters/not-a-uuid", "viewer", 404, CodeNotFound, false},
		{"list tampered cursor", "/v1/matters?cursor=abc.def", "viewer", 400, api.CodeBadCursor, false},
		{"list non-integer limit", "/v1/matters?limit=ten", "viewer", 400, server.CodeValidationFailed, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, q, h := newRig(t)
			rec := do(h, http.MethodGet, tc.path, tc.user, nil, nil)
			checkProblem(t, rec, tc.status, tc.code)
			if got := q.Calls() > 0; got != tc.wantCalls {
				t.Errorf("service called = %v, want %v", got, tc.wantCalls)
			}
		})
	}
}

func TestMatterReadsUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/matters", "/v1/matters/" + idA} {
		checkProblem(t, do(m.Handler(), http.MethodGet, path, "viewer", nil, nil), 503, CodeUnavailable)
	}
}

func TestMatterGetFields(t *testing.T) {
	content := []string{"client_id", "practice_area_id", "responsible_lawyer_id", "status", "visibility", "retention_class", "created_at", "version"}
	tests := []struct {
		name     string
		user     string
		wantFull bool
	}{
		{"viewer sees the full record", "viewer", true},
		{"directory-only sees number and name", "directory", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, h := newRig(t)
			rec := do(h, http.MethodGet, "/v1/matters/"+idA, tc.user, nil, nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got["id"] != idA || got["number"] != "2026-001" || got["short_name"] != "Alpha" {
				t.Errorf("body = %v", got)
			}
			for _, f := range content {
				if _, ok := got[f]; ok != tc.wantFull {
					t.Errorf("field %s present = %v, want %v", f, ok, tc.wantFull)
				}
			}
			if etag := rec.Header().Get("ETag"); (etag == `"3"`) != tc.wantFull {
				t.Errorf("ETag = %q, want full=%v", etag, tc.wantFull)
			}
			if tc.wantFull && got["open_date"] != "2026-10-01" {
				t.Errorf("open_date = %v", got["open_date"])
			}
		})
	}
}

func TestMatterListPagesAndFields(t *testing.T) {
	_, _, h := newRig(t)
	tests := []struct {
		name     string
		user     string
		limit    string
		wantNums []string
		wantNext bool
		wantFull bool
	}{
		{"viewer, default limit, hidden matter left out", "viewer", "", []string{"2026-001", "2026-002", "2026-003"}, false, true},
		{"directory-only gets number and name", "directory", "", []string{"2026-001", "2026-002", "2026-003"}, false, false},
		{"limit 2 pages", "viewer", "2", []string{"2026-001", "2026-002"}, true, true},
		{"limit above maximum is capped", "viewer", "100000", []string{"2026-001", "2026-002", "2026-003"}, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := "/v1/matters"
			if tc.limit != "" {
				path += "?limit=" + tc.limit
			}
			rec := do(h, http.MethodGet, path, tc.user, nil, nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			validatePage(t, rec.Body.Bytes())
			var p pageBody
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			var nums []string
			for _, it := range p.Items {
				nums = append(nums, it["number"].(string))
				if _, ok := it["client_id"]; ok != tc.wantFull {
					t.Errorf("client_id present = %v, want %v", ok, tc.wantFull)
				}
			}
			if strings.Join(nums, ",") != strings.Join(tc.wantNums, ",") {
				t.Errorf("numbers = %v, want %v", nums, tc.wantNums)
			}
			if (p.Next != "") != tc.wantNext {
				t.Errorf("next = %q, want next=%v", p.Next, tc.wantNext)
			}
		})
	}
}

func TestMatterListFollowsCursor(t *testing.T) {
	_, _, h := newRig(t)
	rec := do(h, http.MethodGet, "/v1/matters?limit=2", "viewer", nil, nil)
	validatePage(t, rec.Body.Bytes())
	var first pageBody
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil || first.Next == "" {
		t.Fatalf("first page: %v next=%q", err, first.Next)
	}
	rec = do(h, http.MethodGet, "/v1/matters?cursor="+first.Next, "viewer", nil, nil)
	validatePage(t, rec.Body.Bytes())
	var second pageBody
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0]["number"] != "2026-003" || second.Next != "" {
		t.Errorf("second page = %+v", second)
	}
	tampered := strings.Replace(first.Next, first.Next[:4], "AAAA", 1)
	checkProblem(t, do(h, http.MethodGet, "/v1/matters?cursor="+tampered, "viewer", nil, nil), 400, api.CodeBadCursor)
}
