package work_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/work"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
	kwork "github.com/dagi377/counsel/kernel/work"
	"github.com/dagi377/counsel/kernel/work/workfake"
)

const (
	matterA      = "0190a000-0000-7000-8000-00000000000a"
	matterHidden = "0190a000-0000-7000-8000-0000000000ff"
	workflow     = "0190b000-0000-7000-8000-000000000001"
	statusTodo   = "0190b000-0000-7000-8000-0000000000a1"
	statusDoing  = "0190b000-0000-7000-8000-0000000000a2"
	statusDone   = "0190b000-0000-7000-8000-0000000000a3"
	userBob      = "0190c000-0000-7000-8000-0000000000b0"
	userCarol    = "0190c000-0000-7000-8000-0000000000c0"
	createJSON   = `{"type":"task","title":"Draft reply","workflow_id":"` + workflow + `","status_id":"` + statusTodo +
		`","matter_id":"` + matterA + `","assignee_user_id":"` + userBob + `","priority":2,"due_at":"2026-10-20T17:00:00Z"}`
)

// counting wraps the workfake and counts every Service call.
type counting struct {
	*workfake.Fake
	calls int
	fail  error
}

func (c *counting) Create(ctx context.Context, tx pgx.Tx, it kwork.Item) (kwork.ItemID, error) {
	c.calls++
	if c.fail != nil {
		return "", c.fail
	}
	return c.Fake.Create(ctx, tx, it)
}

func (c *counting) Get(ctx context.Context, id kwork.ItemID) (kwork.Item, error) {
	c.calls++
	return c.Fake.Get(ctx, id)
}

func (c *counting) List(ctx context.Context, f kwork.Filter) ([]kwork.Item, error) {
	c.calls++
	return c.Fake.List(ctx, f)
}

func (c *counting) Move(ctx context.Context, tx pgx.Tx, id kwork.ItemID, v int, to, by string) error {
	c.calls++
	return c.Fake.Move(ctx, tx, id, v, to, by)
}

func (c *counting) SetApprover(ctx context.Context, tx pgx.Tx, id kwork.ItemID, approver string, agent bool) error {
	c.calls++
	return c.Fake.SetApprover(ctx, tx, id, approver, agent)
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

type stubAccess map[string][]string

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == matterHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a[p.UserID] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

type rig struct {
	h      http.Handler
	svc    *counting
	hidden kwork.ItemID
}

func newRig(t *testing.T) rig {
	t.Helper()
	ctx := context.Background()
	f := workfake.New()
	f.AddTransition(kwork.Transition{WorkflowID: workflow, From: statusTodo, To: statusDoing})
	f.AddTransition(kwork.Transition{WorkflowID: workflow, From: statusDoing, To: statusDone})
	var hidden kwork.ItemID
	for i, m := range []string{matterA, matterA, matterHidden} {
		id, err := f.Create(ctx, nil, kwork.Item{Type: "task", Title: "seed", WorkflowID: workflow, StatusID: statusTodo,
			Subject: kwork.Subject{Kind: work.SubjectMatter, ID: m}, AssigneeUserID: userBob, ReporterID: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		if i == 2 {
			hidden = id
		}
	}
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	c := &counting{Fake: f}
	h := &work.HTTP{Items: c, DB: txStub{}, Cursors: codec, Idem: idemfake.New(), Problems: &server.Problems{},
		Access: stubAccess{"alice": {work.CapView, work.CapEdit}, "viewer": {work.CapView}, "editor": {work.CapEdit},
			"bot": {work.CapView, work.CapEdit}},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			if u == "bot" {
				return registry.Principal{Kind: registry.PrincipalAgent, UserID: u, ActingFor: "alice"}, true
			}
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		},
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return rig{h: m.Handler(), svc: c, hidden: hidden}
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

func checkProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
		t.Errorf("Content-Type = %q, want %q", ct, api.ProblemContentType)
	}
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code || p.Status != status {
		t.Errorf("problem = %+v, want %d %s", p, status, code)
	}
}

func TestARC_I_051_WorkItemProblems(t *testing.T) {
	key := map[string]string{idem.HeaderName: "key-00000001"}
	match := map[string]string{"If-Match": `"1"`}
	item1 := "/v1/work-items/item-1"
	moveJSON := `{"status_id":"` + statusDoing + `"}`
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
		{"list without principal", "GET", "/v1/work-items", "", "", nil, 401, work.CodeUnauthenticated, true},
		{"post without principal", "POST", "/v1/work-items", "", createJSON, key, 401, work.CodeUnauthenticated, true},
		{"patch without principal", "PATCH", item1, "", moveJSON, match, 401, work.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"list without work.view", "GET", "/v1/work-items", "editor", "", nil, 403, work.CodeMissingCapability, true},
		{"post without work.edit", "POST", "/v1/work-items", "viewer", createJSON, key, 403, work.CodeMissingCapability, true},
		{"patch without work.edit", "PATCH", item1, "viewer", moveJSON, match, 403, work.CodeMissingCapability, true},
		// rule: ARC-I-007
		{"post missing idempotency key", "POST", "/v1/work-items", "alice", createJSON, nil, 400, idem.CodeKeyRequired, true},
		{"post unknown body field", "POST", "/v1/work-items", "alice", `{"type":"task","title":"x","workflow_id":"` + workflow + `","status_id":"` + statusTodo + `","approver_id":"` + userCarol + `"}`, key, 400, server.CodeValidationFailed, true},
		{"post unknown type", "POST", "/v1/work-items", "alice", `{"type":"epic","title":"x","workflow_id":"` + workflow + `","status_id":"` + statusTodo + `"}`, key, 400, server.CodeValidationFailed, true},
		{"post malformed matter id", "POST", "/v1/work-items", "alice", `{"type":"task","title":"x","workflow_id":"` + workflow + `","status_id":"` + statusTodo + `","matter_id":"m1"}`, key, 400, server.CodeValidationFailed, true},
		{"post blank title", "POST", "/v1/work-items", "alice", `{"type":"task","title":" ","workflow_id":"` + workflow + `","status_id":"` + statusTodo + `"}`, key, 400, server.CodeValidationFailed, true},
		{"post priority out of range", "POST", "/v1/work-items", "alice", `{"type":"task","title":"x","workflow_id":"` + workflow + `","status_id":"` + statusTodo + `","priority":9}`, key, 400, server.CodeValidationFailed, true},
		{"post bad due_at", "POST", "/v1/work-items", "alice", `{"type":"task","title":"x","workflow_id":"` + workflow + `","status_id":"` + statusTodo + `","due_at":"tomorrow"}`, key, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-042
		{"post on walled matter is 404", "POST", "/v1/work-items", "alice", `{"type":"task","title":"x","workflow_id":"` + workflow + `","status_id":"` + statusTodo + `","matter_id":"` + matterHidden + `"}`, key, 404, work.CodeNotFound, true},
		{"patch item on walled matter is 404", "PATCH", "/v1/work-items/item-3", "alice", moveJSON, match, 404, work.CodeNotFound, false},
		{"patch malformed id", "PATCH", "/v1/work-items/bad%20id", "alice", moveJSON, match, 400, server.CodeValidationFailed, true},
		{"patch unknown body field", "PATCH", item1, "alice", `{"title":"new"}`, match, 400, server.CodeValidationFailed, true},
		{"patch empty body", "PATCH", item1, "alice", `{}`, match, 400, server.CodeValidationFailed, true},
		{"patch malformed status id", "PATCH", item1, "alice", `{"status_id":"done"}`, match, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-006
		{"patch missing If-Match", "PATCH", item1, "alice", moveJSON, nil, 428, server.CodePreconditionRequired, true},
		{"patch stale version", "PATCH", item1, "alice", moveJSON, map[string]string{"If-Match": `"7"`}, 412, server.CodeVersionConflict, false},
		{"patch unknown item", "PATCH", "/v1/work-items/item-99", "alice", moveJSON, match, 404, work.CodeNotFound, false},
		{"patch transition not in workflow", "PATCH", item1, "alice", `{"status_id":"` + statusDone + `"}`, match, 409, work.CodeTransitionNotAllowed, false},
		// rule: ARC-I-051
		{"patch assignee as approver", "PATCH", item1, "alice", `{"approver_id":"` + userBob + `"}`, match, 422, work.CodeSelfApproval, false},
		{"tampered cursor", "GET", "/v1/work-items?cursor=AAAA.BBBB", "alice", "", nil, 400, api.CodeBadCursor, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			rec := do(r.h, tc.method, tc.path, tc.user, tc.body, tc.hdr)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.svc.calls != 0 {
				t.Errorf("service calls = %d, want 0", r.svc.calls)
			}
		})
	}
}

func TestWorkItemCreate(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		reporter string
	}{
		{"user reports", "alice", "alice"},
		{"agent reports for the user it acts for", "bot", "alice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			rec := do(r.h, "POST", "/v1/work-items", tc.user, createJSON, map[string]string{idem.HeaderName: "key-00000002"})
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var got struct {
				Reporter string `json:"reporter_id"`
				Matter   string `json:"matter_id"`
				Status   string `json:"status_id"`
				Priority int    `json:"priority"`
				DueAt    string `json:"due_at"`
				Version  int    `json:"version"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Reporter != tc.reporter || got.Matter != matterA || got.Status != statusTodo || got.Priority != 2 ||
				got.DueAt != "2026-10-20T17:00:00Z" || got.Version != 1 || rec.Header().Get("ETag") != `"1"` {
				t.Errorf("created = %+v etag %s", got, rec.Header().Get("ETag"))
			}
		})
	}
}

func TestWorkItemPatch(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		status   string
		approver string
		version  int
	}{
		{"move along the workflow", `{"status_id":"` + statusDoing + `"}`, statusDoing, "", 2},
		{"set an approver", `{"approver_id":"` + userCarol + `"}`, statusTodo, userCarol, 2},
		{"move and set an approver", `{"status_id":"` + statusDoing + `","approver_id":"` + userCarol + `"}`, statusDoing, userCarol, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			rec := do(r.h, "PATCH", "/v1/work-items/item-1", "alice", tc.body, map[string]string{"If-Match": `"1"`})
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var got struct {
				Status   string `json:"status_id"`
				Approver string `json:"approver_id"`
				Version  int    `json:"version"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.status || got.Approver != tc.approver || got.Version != tc.version {
				t.Errorf("patched = %+v", got)
			}
		})
	}
}

func TestARC_I_008_WorkItemIdempotency(t *testing.T) {
	r := newRig(t)
	hdr := map[string]string{idem.HeaderName: "key-00000042"}
	first := do(r.h, "POST", "/v1/work-items", "alice", createJSON, hdr)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	calls := r.svc.calls
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", createJSON, http.StatusCreated, ""},
		// rule: ARC-I-008
		// The rule sets 422 for a reused key with a different body (as kernel/idem does).
		{"same key, different body", strings.Replace(createJSON, "Draft reply", "Other", 1), http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(r.h, "POST", "/v1/work-items", "alice", tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" && rec.Body.String() != first.Body.String() {
				t.Errorf("replay = %s, want %s", rec.Body, first.Body)
			}
		})
	}
	if r.svc.calls != calls {
		t.Errorf("service calls after replay = %d, want %d", r.svc.calls, calls)
	}
}

func TestWorkItemListPages(t *testing.T) {
	tests := []struct {
		name  string
		query string
		pages []int
	}{
		{"one page", "", []int{3}},
		{"pages of two", "?limit=2", []int{2, 1}},
		{"limit above maximum is capped", "?limit=5000", []int{3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			path := "/v1/work-items" + tc.query
			seen := map[string]bool{}
			for i, want := range tc.pages {
				rec := do(r.h, "GET", path, "viewer", "", nil)
				var page struct {
					Items []struct {
						ID string `json:"id"`
					} `json:"data"`
					Next string `json:"next_cursor"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != 200 {
					t.Fatalf("page %d: %d %s", i, rec.Code, rec.Body)
				}
				if len(page.Items) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Items), want)
				}
				for _, it := range page.Items {
					if seen[it.ID] {
						t.Errorf("item %s on two pages", it.ID)
					}
					seen[it.ID] = true
				}
				if last := i == len(tc.pages)-1; (page.Next != "") == last {
					t.Fatalf("page %d next = %q", i, page.Next)
				}
				path = "/v1/work-items?cursor=" + page.Next
			}
		})
	}
}

func TestWorkItemServiceErrorsMapToProblems(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid", kwork.ErrInvalidItem, 422, server.CodeValidationFailed},
		{"unexpected", errors.New("boom"), 500, server.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.svc.fail = tc.err
			rec := do(r.h, "POST", "/v1/work-items", "alice", createJSON, map[string]string{idem.HeaderName: "key-00000009"})
			checkProblem(t, rec, tc.status, tc.code)
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("error text leaked: %s", rec.Body)
			}
		})
	}
}

func TestWorkItemRoutesAnswer503Unwired(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&work.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/work-items", "alice", "", nil), http.StatusServiceUnavailable, work.CodeUnavailable)
}

func TestARC_I_012_WorkItemWritesDeclareNoGateState(t *testing.T) {
	writes, err := server.DeclaredWrites(newRig(t).h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("writes = %+v, want POST and PATCH /work-items", writes)
	}
}
