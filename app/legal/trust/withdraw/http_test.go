package withdraw_test

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

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	userViewer = "viewer"
	userNobody = "nobody"

	wdHidden = "0192a000-0000-7000-8000-0000000000ff"
	ledgerID = "0192b000-0000-7000-8000-000000000001"
	preparer = "0192c000-0000-7000-8000-000000000001"
	approver = "0192c000-0000-7000-8000-000000000002"
	gateID   = "0192d000-0000-7000-8000-000000000001"
)

func wdID(i int) string { return fmt.Sprintf("0192a000-0000-7000-8000-%012d", i) }

// stubLists is an in-memory withdraw.Lists that counts calls.
type stubLists struct {
	mu    sync.Mutex
	rows  []withdraw.Listed
	calls int
}

func (s *stubLists) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubLists) List(_ context.Context, f withdraw.ListFilter) ([]withdraw.Listed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	out := []withdraw.Listed{}
	for _, l := range s.rows {
		if l.ID == wdHidden || (f.After != "" && l.ID <= f.After) || (f.Status != "" && l.Status != f.Status) {
			continue
		}
		if len(out) == f.Limit {
			break
		}
		out = append(out, l)
	}
	return out, nil
}

func (s *stubLists) Listed(_ context.Context, id string) (withdraw.Listed, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for _, l := range s.rows {
		if l.ID == id && id != wdHidden {
			return l, nil
		}
	}
	return withdraw.Listed{}, fmt.Errorf("listed: %w", withdraw.ErrNotFound)
}

type stubAccess struct{}

func (stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == wdHidden {
		return false, identity.ErrNotVisible
	}
	return p.UserID == userViewer && capability == withdraw.CapView, nil
}

func newRig(t *testing.T, n int) (*stubLists, http.Handler) {
	t.Helper()
	st := &stubLists{}
	for i := 1; i <= n; i++ {
		l := withdraw.Listed{LedgerID: ledgerID, Version: 1, Withdrawal: withdraw.Withdrawal{ID: wdID(i), Status: withdraw.StatusRequested,
			GateID: gateID, Request: withdraw.Request{AmountCents: int64(100 * i), Payee: "Vendor Inc.", Method: withdraw.MethodCheque,
				Reason: "disbursement", PreparedBy: preparer}}}
		if i%2 == 0 {
			l.Status, l.AuthorizedBy = withdraw.StatusAuthorized, approver
		}
		st.rows = append(st.rows, l)
	}
	st.rows = append(st.rows, withdraw.Listed{Withdrawal: withdraw.Withdrawal{ID: wdHidden}})
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &withdraw.Reads{Lists: st, Access: stubAccess{}, Cursors: codec, Problems: &server.Problems{},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		}}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return st, m.Handler()
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

func do(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestARC_I_051_WithdrawalReadRoutesProblems(t *testing.T) {
	one := "/v1/trust/withdrawals/" + wdID(1)
	tests := []struct {
		name      string
		path      string
		user      string
		status    int
		code      string
		zeroCalls bool
	}{
		{"list without principal", "/v1/trust/withdrawals", "", 401, withdraw.CodeUnauthenticated, true},
		{"get without principal", one, "", 401, withdraw.CodeUnauthenticated, true},
		{"list without trust.view", "/v1/trust/withdrawals", userNobody, 403, withdraw.CodeMissingCapability, true},
		{"get without trust.view", one, userNobody, 403, withdraw.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"hidden withdrawal is 404, not 403", "/v1/trust/withdrawals/" + wdHidden, userNobody, 404, withdraw.CodeNotFound, true},
		{"unknown withdrawal", "/v1/trust/withdrawals/" + wdID(99), userViewer, 404, withdraw.CodeNotFound, false},
		{"malformed id", "/v1/trust/withdrawals/nope", userViewer, 404, withdraw.CodeNotFound, true},
		{"unknown status", "/v1/trust/withdrawals?status=lost", userViewer, 400, server.CodeValidationFailed, true},
		{"tampered cursor", "/v1/trust/withdrawals?cursor=AAAA.BBBB", userViewer, 400, api.CodeBadCursor, true},
		{"non-integer limit", "/v1/trust/withdrawals?limit=ten", userViewer, 400, server.CodeValidationFailed, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, h := newRig(t, 2)
			checkProblem(t, do(h, http.MethodGet, tc.path, tc.user), tc.status, tc.code)
			if tc.zeroCalls && st.Calls() != 0 {
				t.Errorf("service calls = %d, want 0", st.Calls())
			}
		})
	}
}

func TestWithdrawalReadRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&withdraw.Reads{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), http.MethodGet, "/v1/trust/withdrawals", userViewer), 503, withdraw.CodeUnavailable)
}

func TestWithdrawalViewHasGateNoAuthorize(t *testing.T) {
	st, h := newRig(t, 2)
	rec := do(h, http.MethodGet, "/v1/trust/withdrawals/"+wdID(2), userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	validate(t, "#/components/schemas/WithdrawalRequest", rec.Body.Bytes())
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v["gate_id"] != gateID || v["status"] != "authorized" {
		t.Errorf("view = %v, want gate %s, authorized", v, gateID)
	}
	for k := range v {
		if strings.Contains(k, "action") || strings.Contains(k, "link") {
			t.Errorf("view carries %q; authorizing is a gate decision", k)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		for _, path := range []string{"/v1/trust/withdrawals/" + wdID(2), "/v1/trust/withdrawals/" + wdID(2) + "/authorize"} {
			if rec := do(h, method, path, userViewer); rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want no route", method, path, rec.Code)
			}
		}
	}
	writes, err := server.DeclaredWrites(h)
	if err != nil || len(writes) != 0 {
		t.Errorf("writes = %+v (%v), want none", writes, err)
	}
	if st.Calls() != 1 {
		t.Errorf("service calls = %d, want 1", st.Calls())
	}
}

func TestWithdrawalListPages(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		query     string
		wantItems []int
	}{
		{"one page", 3, "", []int{3}},
		{"pages of two", 5, "?limit=2", []int{2, 2, 1}},
		{"limit above maximum is capped", 3, "?limit=5000", []int{3}},
		{"by status", 4, "?status=awaiting_authorization", []int{2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, h := newRig(t, tc.n)
			path := "/v1/trust/withdrawals" + tc.query
			seen := map[string]bool{}
			for i, want := range tc.wantItems {
				rec := do(h, http.MethodGet, path, userViewer)
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d status = %d (%s)", i, rec.Code, rec.Body)
				}
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
					validate(t, "#/components/schemas/WithdrawalRequest", it)
					var v struct{ ID string }
					_ = json.Unmarshal(it, &v)
					if v.ID == wdHidden || seen[v.ID] {
						t.Errorf("page %d repeats or leaks %s", i, v.ID)
					}
					seen[v.ID] = true
				}
				last := i == len(tc.wantItems)-1
				if (page.Next != "") == last {
					t.Fatalf("page %d next = %q, last = %v", i, page.Next, last)
				}
				path = "/v1/trust/withdrawals?cursor=" + page.Next
			}
		})
	}
}
