package trust_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/trustfake"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	reqMatter       = "0190a000-0000-7000-8000-00000000000a"
	reqMatterHidden = "0190a000-0000-7000-8000-0000000000ff"
	reqJSON         = `{"matter_id":"` + reqMatter + `","amount_cents":250000,"reason":"Initial retainer","due_on":"2026-10-20"}`
)

type reqTxStub struct{}

func (reqTxStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

type reqAccess struct{ caps map[string][]string }

func (a reqAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == reqMatterHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a.caps[p.UserID] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

func newRequestRig(t *testing.T) (http.Handler, *trustfake.Requests) {
	t.Helper()
	f := trustfake.NewRequests()
	for _, u := range []string{"alice", "alice", "bob"} {
		q := trust.Request{MatterID: reqMatter, AmountCents: 100000}
		if _, err := f.Create(context.Background(), nil, q, trust.RequestActor{UserID: u}); err != nil {
			t.Fatal(err)
		}
	}
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &trust.RequestHTTP{Requests: f, DB: reqTxStub{}, Cursors: codec, Idem: idemfake.New(), Problems: &server.Problems{},
		Access: reqAccess{caps: map[string][]string{
			"alice": {trust.CapRequest, trust.CapView}, "bot": {trust.CapRequest, trust.CapView},
			"viewer": {trust.CapView}, "preparer": {trust.CapRequest}, "reader": nil,
		}},
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
	f.Calls = 0
	return m.Handler(), f
}

func doRequest(h http.Handler, method, path, user, body string, hdr map[string]string) *httptest.ResponseRecorder {
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

func checkRequestProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
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

func TestARC_I_051_TrustRequestProblems(t *testing.T) {
	key := map[string]string{idem.HeaderName: "key-00000001"}
	tests := []struct {
		name   string
		method string
		path   string
		user   string
		body   string
		hdr    map[string]string
		status int
		code   string
	}{
		{"list without principal", "GET", "/v1/trust/requests", "", "", nil, 401, trust.CodeUnauthenticated},
		{"post without principal", "POST", "/v1/trust/requests", "", reqJSON, key, 401, trust.CodeUnauthenticated},
		// rule: ARC-I-051
		{"list without trust.view", "GET", "/v1/trust/requests", "preparer", "", nil, 403, trust.CodeMissingCapability},
		{"post without trust.request", "POST", "/v1/trust/requests", "viewer", reqJSON, key, 403, trust.CodeMissingCapability},
		{"post with no capability", "POST", "/v1/trust/requests", "reader", reqJSON, key, 403, trust.CodeMissingCapability},
		// rule: ARC-I-007
		{"post missing idempotency key", "POST", "/v1/trust/requests", "alice", reqJSON, nil, 400, idem.CodeKeyRequired},
		{"post unknown body field", "POST", "/v1/trust/requests", "alice", `{"matter_id":"` + reqMatter + `","amount_cents":5,"status":"approved"}`, key, 400, server.CodeValidationFailed},
		{"post approver in body", "POST", "/v1/trust/requests", "alice", `{"matter_id":"` + reqMatter + `","amount_cents":5,"approved_by":"bob"}`, key, 400, server.CodeValidationFailed},
		{"post malformed matter id", "POST", "/v1/trust/requests", "alice", `{"matter_id":"x","amount_cents":5}`, key, 400, server.CodeValidationFailed},
		{"post fractional cents", "POST", "/v1/trust/requests", "alice", `{"matter_id":"` + reqMatter + `","amount_cents":5.5}`, key, 400, server.CodeValidationFailed},
		{"post zero amount", "POST", "/v1/trust/requests", "alice", `{"matter_id":"` + reqMatter + `","amount_cents":0}`, key, 400, server.CodeValidationFailed},
		{"post bad due date", "POST", "/v1/trust/requests", "alice", `{"matter_id":"` + reqMatter + `","amount_cents":5,"due_on":"20/10/2026"}`, key, 400, server.CodeValidationFailed},
		// rule: ARC-I-042
		{"post on walled matter is 404", "POST", "/v1/trust/requests", "alice", `{"matter_id":"` + reqMatterHidden + `","amount_cents":5}`, key, 404, trust.CodeNotFound},
		{"tampered cursor", "GET", "/v1/trust/requests?cursor=AAAA.BBBB", "alice", "", nil, 400, api.CodeBadCursor},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, f := newRequestRig(t)
			rec := doRequest(h, tc.method, tc.path, tc.user, tc.body, tc.hdr)
			checkRequestProblem(t, rec, tc.status, tc.code)
			if f.Calls != 0 {
				t.Errorf("service calls = %d, want 0", f.Calls)
			}
		})
	}
}

func TestTrustRequestCreateIsDraft(t *testing.T) {
	tests := []struct {
		name      string
		user      string
		preparer  string
		viaAgent  bool
		wantDueOn string
	}{
		{"lawyer prepares", "alice", "alice", false, "2026-10-20"},
		// rule: ARC-I-051
		// The agent may draft for the person it acts for; the draft has no approver.
		{"agent drafts for alice", "bot", "alice", true, "2026-10-20"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newRequestRig(t)
			rec := doRequest(h, "POST", "/v1/trust/requests", tc.user, reqJSON, map[string]string{idem.HeaderName: "key-00000002"})
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var got struct {
				Status          string  `json:"status"`
				PreparedBy      string  `json:"prepared_by"`
				PreparedByAgent bool    `json:"prepared_by_agent"`
				AmountCents     int64   `json:"amount_cents"`
				DueOn           string  `json:"due_on"`
				ApprovedBy      *string `json:"approved_by"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != "draft" || got.PreparedBy != tc.preparer || got.PreparedByAgent != tc.viaAgent ||
				got.AmountCents != 250000 || got.DueOn != tc.wantDueOn {
				t.Errorf("created = %+v", got)
			}
			if got.ApprovedBy != nil {
				t.Errorf("a new request has an approver: %s", rec.Body)
			}
		})
	}
}

func TestTrustRequestServiceErrorsMapToProblems(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid", fmt.Errorf("create: %w", trust.ErrInvalid), 422, server.CodeValidationFailed},
		{"not found", trust.ErrNotFound, 404, trust.CodeNotFound},
		{"self approval", trust.ErrSelfApproval, 403, trust.CodeMissingCapability},
		{"unexpected", errors.New("boom"), 500, server.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, f := newRequestRig(t)
			f.Fail = tc.err
			rec := doRequest(h, "POST", "/v1/trust/requests", "alice", reqJSON, map[string]string{idem.HeaderName: "key-00000009"})
			checkRequestProblem(t, rec, tc.status, tc.code)
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("error text leaked: %s", rec.Body)
			}
		})
	}
}

func TestARC_I_008_TrustRequestIdempotency(t *testing.T) {
	h, f := newRequestRig(t)
	hdr := map[string]string{idem.HeaderName: "key-00000042"}
	first := doRequest(h, "POST", "/v1/trust/requests", "alice", reqJSON, hdr)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	calls := f.Calls
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", reqJSON, http.StatusCreated, ""},
		// rule: ARC-I-008
		// The rule sets 422 for a reused key with a different body (as kernel/idem does).
		{"same key, different body", `{"matter_id":"` + reqMatter + `","amount_cents":9999}`, http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := doRequest(h, "POST", "/v1/trust/requests", "alice", tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" && (rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true") {
				t.Errorf("replay = %s, want %s", rec.Body, first.Body)
			}
		})
	}
	if f.Calls != calls {
		t.Errorf("service calls after replay = %d, want %d", f.Calls, calls)
	}
}

func TestTrustRequestListPages(t *testing.T) {
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
			h, _ := newRequestRig(t)
			path := "/v1/trust/requests" + tc.query
			for i, want := range tc.pages {
				rec := doRequest(h, "GET", path, "viewer", "", nil)
				var page struct {
					Items []map[string]any `json:"data"`
					Next  string           `json:"next_cursor"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != 200 {
					t.Fatalf("page %d: %d %s", i, rec.Code, rec.Body)
				}
				if len(page.Items) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Items), want)
				}
				if last := i == len(tc.pages)-1; (page.Next != "") == last {
					t.Fatalf("page %d next = %q", i, page.Next)
				}
				path = "/v1/trust/requests?cursor=" + page.Next
			}
		})
	}
}

func TestTrustRequestRoutesAnswer503Unwired(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&trust.RequestHTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	rec := doRequest(m.Handler(), "GET", "/v1/trust/requests", "alice", "", nil)
	checkRequestProblem(t, rec, http.StatusServiceUnavailable, trust.CodeUnavailable)
}

func TestARC_I_012_TrustRequestWritesDeclareNoGateState(t *testing.T) {
	h, _ := newRequestRig(t)
	writes, err := server.DeclaredWrites(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 {
		t.Fatalf("writes = %+v, want POST /trust/requests", writes)
	}
}

func TestCheckApprover(t *testing.T) {
	q := trust.Request{MatterID: reqMatter, AmountCents: 1, PreparedBy: "alice"}
	tests := []struct {
		name string
		by   trust.RequestActor
		want error
	}{
		{"another lawyer approves", trust.RequestActor{UserID: "bob"}, nil},
		// rule: ARC-I-051
		{"preparer never approves", trust.RequestActor{UserID: "alice"}, trust.ErrSelfApproval},
		{"no approver", trust.RequestActor{}, trust.ErrSelfApproval},
		{"agent never approves", trust.RequestActor{UserID: "bob", Agent: true}, trust.ErrAgentApproval},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := trust.CheckApprover(q, tc.by); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateRequest(t *testing.T) {
	ok := trust.Request{MatterID: reqMatter, AmountCents: 1, PreparedBy: "alice"}
	tests := []struct {
		name  string
		edit  func(q *trust.Request)
		valid bool
	}{
		{"valid", func(*trust.Request) {}, true},
		{"no matter", func(q *trust.Request) { q.MatterID = "" }, false},
		{"no preparer", func(q *trust.Request) { q.PreparedBy = "" }, false},
		{"zero amount", func(q *trust.Request) { q.AmountCents = 0 }, false},
		{"negative amount", func(q *trust.Request) { q.AmountCents = -5 }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := ok
			tc.edit(&q)
			err := trust.ValidateRequest(q)
			if (err == nil) != tc.valid || (err != nil && !errors.Is(err, trust.ErrInvalid)) {
				t.Errorf("err = %v, valid want %v", err, tc.valid)
			}
		})
	}
}
