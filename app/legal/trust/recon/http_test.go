package recon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	userViewer = "viewer"
	userNobody = "nobody"

	acctID     = "0193a000-0000-7000-8000-000000000001"
	acctOther  = "0193a000-0000-7000-8000-000000000002"
	acctHidden = "0193a000-0000-7000-8000-0000000000ff"
	signer     = "0193c000-0000-7000-8000-000000000001"
)

func reconID(i int) string { return fmt.Sprintf("0193b000-0000-7000-8000-%012d", i) }

// stubLists is an in-memory recon.Lists that counts calls.
type stubLists struct {
	mu    sync.Mutex
	rows  []recon.Reconciliation
	calls int
}

func (s *stubLists) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubLists) List(_ context.Context, f recon.ListFilter) ([]recon.Reconciliation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	out := []recon.Reconciliation{}
	for _, c := range s.rows {
		if (f.After != "" && c.ID <= f.After) || (f.AccountID != "" && c.AccountID != f.AccountID) {
			continue
		}
		if len(out) == f.Limit {
			break
		}
		out = append(out, c)
	}
	return out, nil
}

type stubAccess struct{}

func (stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == acctHidden {
		return false, identity.ErrNotVisible
	}
	return p.UserID == userViewer && capability == recon.CapView, nil
}

func newRig(t *testing.T, n int) (*stubLists, http.Handler) {
	t.Helper()
	st := &stubLists{}
	at := time.Date(2026, 4, 3, 14, 0, 0, 0, time.UTC)
	for i := 1; i <= n; i++ {
		c := recon.Reconciliation{ID: reconID(i), AccountID: acctID, Period: recon.Period{Year: 2026, Month: time.Month(i%12 + 1)},
			Result: recon.ThreeWay{BankCents: 10_000, AdjustedBankCents: 9_500, BookCents: 9_500, ClientLedgerCents: 9_500,
				Items: []recon.ReconcilingItem{{Kind: recon.OutstandingCheque, AmountCents: -500}}},
			Status: recon.StatusPrepared, PreparedBy: "p"}
		if i%2 == 0 {
			c.AccountID, c.Status, c.ApprovedBy, c.ApprovedAt = acctOther, recon.StatusApproved, signer, &at
		}
		st.rows = append(st.rows, c)
	}
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &recon.Reads{Lists: st, Access: stubAccess{}, Cursors: codec, Problems: &server.Problems{},
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

func TestARC_I_051_ReconciliationListProblems(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		user   string
		status int
		code   string
	}{
		{"without principal", "/v1/trust/reconciliations", "", 401, recon.CodeUnauthenticated},
		{"without trust.view", "/v1/trust/reconciliations", userNobody, 403, recon.CodeMissingCapability},
		// rule: ARC-I-042
		{"hidden account is 404, not 403", "/v1/trust/reconciliations?account_id=" + acctHidden, userNobody, 404, recon.CodeNotFound},
		{"malformed account_id", "/v1/trust/reconciliations?account_id=nope", userViewer, 400, server.CodeValidationFailed},
		{"tampered cursor", "/v1/trust/reconciliations?cursor=AAAA.BBBB", userViewer, 400, api.CodeBadCursor},
		{"non-integer limit", "/v1/trust/reconciliations?limit=ten", userViewer, 400, server.CodeValidationFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, h := newRig(t, 2)
			checkProblem(t, do(h, http.MethodGet, tc.path, tc.user), tc.status, tc.code)
			if st.Calls() != 0 {
				t.Errorf("service calls = %d, want 0", st.Calls())
			}
		})
	}
}

func TestReconciliationListUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&recon.Reads{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), http.MethodGet, "/v1/trust/reconciliations", userViewer), 503, recon.CodeUnavailable)
}

func TestReconciliationListReadOnly(t *testing.T) {
	st, h := newRig(t, 1)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if rec := do(h, method, "/v1/trust/reconciliations", userViewer); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, rec.Code)
		}
	}
	if writes, err := server.DeclaredWrites(h); err != nil || len(writes) != 0 || st.Calls() != 0 {
		t.Errorf("writes = %+v (%v), calls = %d, want none", writes, err, st.Calls())
	}
}

func TestReconciliationView(t *testing.T) {
	_, h := newRig(t, 2)
	rec := do(h, http.MethodGet, "/v1/trust/reconciliations?account_id="+acctOther, userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %s (%v)", rec.Body, err)
	}
	validate(t, "#/components/schemas/Reconciliation", page.Items[0])
	type view struct {
		Status      string `json:"status"`
		SignedBy    string `json:"signed_by"`
		Month       string `json:"month"`
		Balanced    bool   `json:"balanced"`
		Outstanding int64  `json:"outstanding_items_cents"`
		Adjusted    int64  `json:"adjusted_bank_cents"`
	}
	var got view
	if err := json.Unmarshal(page.Items[0], &got); err != nil {
		t.Fatal(err)
	}
	want := view{Status: "signed", SignedBy: signer, Month: "2026-03", Balanced: true, Outstanding: -500, Adjusted: 9500}
	if got != want {
		t.Errorf("view = %+v, want %+v", got, want)
	}
}

func TestReconciliationListPages(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		query     string
		wantItems []int
	}{
		{"one page", 3, "", []int{3}},
		{"pages of two", 5, "?limit=2", []int{2, 2, 1}},
		{"limit above maximum is capped", 3, "?limit=5000", []int{3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, h := newRig(t, tc.n)
			path := "/v1/trust/reconciliations" + tc.query
			seen := map[string]bool{}
			for i, want := range tc.wantItems {
				rec := do(h, http.MethodGet, path, userViewer)
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d status = %d (%s)", i, rec.Code, rec.Body)
				}
				var page struct {
					Items []json.RawMessage `json:"items"`
					Next  string            `json:"next"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if len(page.Items) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Items), want)
				}
				for _, it := range page.Items {
					validate(t, "#/components/schemas/Reconciliation", it)
					var v struct{ ID string }
					_ = json.Unmarshal(it, &v)
					if seen[v.ID] {
						t.Errorf("page %d repeats %s", i, v.ID)
					}
					seen[v.ID] = true
				}
				last := i == len(tc.wantItems)-1
				if (page.Next != "") == last {
					t.Fatalf("page %d next = %q, last = %v", i, page.Next, last)
				}
				path = "/v1/trust/reconciliations?cursor=" + page.Next
			}
		})
	}
}
