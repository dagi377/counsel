package trust_test

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

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	userViewer = "viewer"
	userNobody = "nobody"

	acctID       = "0191a000-0000-7000-8000-000000000001"
	matterID     = "0191b000-0000-7000-8000-000000000001"
	clientID     = "0191c000-0000-7000-8000-000000000001"
	postedBy     = "0191d000-0000-7000-8000-000000000001"
	ledgerHidden = "0191e000-0000-7000-8000-0000000000ff"
	matterHidden = "0191b000-0000-7000-8000-0000000000ff"
)

func ledgerID(i int) trust.LedgerID {
	return trust.LedgerID(fmt.Sprintf("0191e000-0000-7000-8000-%012d", i))
}

// stubTrust is an in-memory trust.Lists and trust.LedgerReads that counts calls. Its
// balances are fixed per ledger, so a test sees whether the handler reports them as given.
type stubTrust struct {
	mu       sync.Mutex
	ledgers  []trust.Ledger
	balances map[trust.LedgerID]trust.Balance
	txns     map[trust.LedgerID][]trust.Txn
	calls    int
}

func (s *stubTrust) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubTrust) ListAccounts(context.Context) ([]trust.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return []trust.Account{{ID: acctID, Type: trust.AccountMixed, Institution: "Bank", AccountLast4: "1234",
		AccountNo: "999991234", OpenedOn: time.Date(2026, 1, 5, 5, 0, 0, 0, time.UTC)}}, nil
}

func (s *stubTrust) ListLedgers(_ context.Context, f trust.LedgerFilter) ([]trust.Ledger, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	out := []trust.Ledger{}
	for _, l := range s.ledgers {
		if l.ID == ledgerHidden || (f.After != "" && l.ID <= f.After) || (f.MatterID != "" && l.MatterID != f.MatterID) {
			continue
		}
		if len(out) == f.Limit {
			break
		}
		out = append(out, l)
	}
	return out, nil
}

func (s *stubTrust) Balance(_ context.Context, id trust.LedgerID) (trust.Balance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	b, ok := s.balances[id]
	if !ok {
		return trust.Balance{}, trust.ErrNotFound
	}
	return b, nil
}

func (s *stubTrust) Txns(_ context.Context, id trust.LedgerID) ([]trust.Txn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	t, ok := s.txns[id]
	if !ok || id == ledgerHidden {
		return nil, fmt.Errorf("txns: %w", trust.ErrNotFound)
	}
	return t, nil
}

type stubAccess struct{}

func (stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == ledgerHidden || s.ID == matterHidden {
		return false, identity.ErrNotVisible
	}
	return p.UserID == userViewer && capability == trust.CapView, nil
}

func newRig(t *testing.T, ledgers, lines int) (*stubTrust, http.Handler) {
	t.Helper()
	st := &stubTrust{balances: map[trust.LedgerID]trust.Balance{}, txns: map[trust.LedgerID][]trust.Txn{}}
	for i := 1; i <= ledgers; i++ {
		id := ledgerID(i)
		st.ledgers = append(st.ledgers, trust.Ledger{ID: id, AccountID: acctID, MatterID: matterID, ClientID: clientID})
		// The posted balance deliberately differs from any sum of the lines: the handler must
		// report Balance, never recompute it.
		st.balances[id] = trust.Balance{LedgerID: id, PostedCents: int64(1000 * i), ClearedCents: int64(900 * i),
			ProtectedCents: 100, AvailableCents: int64(900*i - 100)}
		var txns []trust.Txn
		for j := 1; j <= lines; j++ {
			txns = append(txns, trust.Txn{ID: trust.TxnID(fmt.Sprintf("0191f000-0000-7000-%04d-%012d", i, j)), AccountID: acctID,
				LedgerID: id, Kind: trust.KindReceipt, TxnDate: time.Date(2026, 3, j, 5, 0, 0, 0, time.UTC),
				Method: trust.MethodEFT, AmountCents: 7, PostedBy: postedBy})
		}
		st.txns[id] = txns
	}
	st.ledgers = append(st.ledgers, trust.Ledger{ID: ledgerHidden, AccountID: acctID, MatterID: matterID, ClientID: clientID})
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &trust.HTTP{Lists: st, Ledgers: st, Access: stubAccess{}, Cursors: codec, Problems: &server.Problems{},
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

const ledgerListSchema = "#/paths/~1trust~1ledgers/get/responses/200/content/application~1json/schema"

// validateLedgerList validates the whole GET /trust/ledgers body against its openapi
// response schema, and fails on any ledger field TrustLedger does not declare (the schema
// allows extra properties, so an undeclared field would otherwise pass unchecked).
func validateLedgerList(t *testing.T, body []byte) {
	t.Helper()
	validate(t, ledgerListSchema, body)
	raw, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	declared := doc.Components.Schemas["TrustLedger"].Properties
	var page struct {
		Items []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	for i, it := range page.Items {
		for k := range it {
			if _, ok := declared[k]; !ok {
				t.Errorf("ledger %d field %q is not declared on openapi TrustLedger", i, k)
			}
		}
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

func TestARC_I_051_TrustReadRoutesProblems(t *testing.T) {
	lines := "/v1/trust/ledgers/" + string(ledgerID(1)) + "/lines"
	tests := []struct {
		name      string
		path      string
		user      string
		status    int
		code      string
		zeroCalls bool
	}{
		{"accounts without principal", "/v1/trust/accounts", "", 401, trust.CodeUnauthenticated, true},
		{"ledgers without principal", "/v1/trust/ledgers", "", 401, trust.CodeUnauthenticated, true},
		{"lines without principal", lines, "", 401, trust.CodeUnauthenticated, true},
		{"accounts without trust.view", "/v1/trust/accounts", userNobody, 403, trust.CodeMissingCapability, true},
		{"ledgers without trust.view", "/v1/trust/ledgers", userNobody, 403, trust.CodeMissingCapability, true},
		{"lines without trust.view", lines, userNobody, 403, trust.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"hidden ledger lines are 404, not 403", "/v1/trust/ledgers/" + ledgerHidden + "/lines", userNobody, 404, trust.CodeNotFound, true},
		{"ledgers of hidden matter are 404", "/v1/trust/ledgers?matter_id=" + matterHidden, userViewer, 404, trust.CodeNotFound, true},
		{"unknown ledger lines", "/v1/trust/ledgers/0191e000-0000-7000-8000-0000000000ee/lines", userViewer, 404, trust.CodeNotFound, false},
		{"malformed ledger id", "/v1/trust/ledgers/nope/lines", userViewer, 404, trust.CodeNotFound, true},
		{"malformed matter_id", "/v1/trust/ledgers?matter_id=nope", userViewer, 400, server.CodeValidationFailed, true},
		{"tampered ledgers cursor", "/v1/trust/ledgers?cursor=AAAA.BBBB", userViewer, 400, api.CodeBadCursor, true},
		{"tampered lines cursor", lines + "?cursor=AAAA.BBBB", userViewer, 400, api.CodeBadCursor, true},
		{"non-integer limit", "/v1/trust/ledgers?limit=ten", userViewer, 400, server.CodeValidationFailed, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, h := newRig(t, 2, 2)
			checkProblem(t, do(h, http.MethodGet, tc.path, tc.user), tc.status, tc.code)
			if tc.zeroCalls && st.Calls() != 0 {
				t.Errorf("service calls = %d, want 0", st.Calls())
			}
		})
	}
}

func TestTrustReadRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&trust.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), http.MethodGet, "/v1/trust/accounts", userViewer), 503, trust.CodeUnavailable)
}

func TestTrustReadRoutesServeNoWrites(t *testing.T) {
	st, h := newRig(t, 1, 1)
	for _, path := range []string{"/v1/trust/accounts", "/v1/trust/ledgers", "/v1/trust/ledgers/" + string(ledgerID(1)) + "/lines"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
			if rec := do(h, method, path, userViewer); rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, path, rec.Code)
			}
		}
	}
	writes, err := server.DeclaredWrites(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 0 || st.Calls() != 0 {
		t.Errorf("writes = %+v, calls = %d, want none", writes, st.Calls())
	}
}

func TestTrustAccountsList(t *testing.T) {
	_, h := newRig(t, 0, 0)
	rec := do(h, http.MethodGet, "/v1/trust/accounts", userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	var page struct {
		Items []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
		t.Fatalf("page = %s (%v)", rec.Body, err)
	}
	validate(t, "#/components/schemas/TrustAccount", page.Items[0])
	if bytes.Contains(rec.Body.Bytes(), []byte("999991234")) || !bytes.Contains(rec.Body.Bytes(), []byte(`"****1234"`)) {
		t.Errorf("body = %s, want the account number masked", rec.Body)
	}
}

func TestTrustLedgersBalanceFromService(t *testing.T) {
	_, h := newRig(t, 2, 3)
	rec := do(h, http.MethodGet, "/v1/trust/ledgers?matter_id="+matterID, userViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	validateLedgerList(t, rec.Body.Bytes())
	var page struct {
		Items []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	want := []struct{ balance, available int64 }{{1000, 800}, {2000, 1700}}
	if len(page.Items) != len(want) {
		t.Fatalf("items = %d, want %d", len(page.Items), len(want))
	}
	for i, raw := range page.Items {
		validate(t, "#/components/schemas/TrustLedger", raw)
		var v struct {
			Balance   int64 `json:"balance_cents"`
			Available int64 `json:"available_cents"`
		}
		_ = json.Unmarshal(raw, &v)
		if v.Balance != want[i].balance || v.Available != want[i].available {
			t.Errorf("ledger %d = %+v, want %+v from Balance", i, v, want[i])
		}
	}
}

// rule: BIL-I-010
func TestTrustLedgerOverdraftIsReported(t *testing.T) {
	tests := []struct {
		name                 string
		posted, available    int64
		wantBalance, wantAvl int64
		wantOverdrawn        bool
		wantShortfall        int64
	}{
		{"in funds", 1000, 800, 1000, 800, false, 0},
		{"exactly zero", 0, 0, 0, 0, false, 0},
		{"holds exceed cleared money", 500, -250, 500, 0, true, 250},
		{"posted balance below zero", -300, -400, 0, 0, true, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, h := newRig(t, 1, 0)
			id := ledgerID(1)
			st.balances[id] = trust.Balance{LedgerID: id, PostedCents: tc.posted, AvailableCents: tc.available}
			rec := do(h, http.MethodGet, "/v1/trust/ledgers?matter_id="+matterID, userViewer)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			validateLedgerList(t, rec.Body.Bytes())
			var page struct {
				Items []json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
				t.Fatalf("page = %s (%v)", rec.Body, err)
			}
			validate(t, "#/components/schemas/TrustLedger", page.Items[0])
			var v struct {
				Balance   int64  `json:"balance_cents"`
				Available int64  `json:"available_cents"`
				Overdrawn *bool  `json:"overdrawn"`
				Shortfall *int64 `json:"shortfall_cents"`
			}
			if err := json.Unmarshal(page.Items[0], &v); err != nil {
				t.Fatal(err)
			}
			if v.Overdrawn == nil || v.Shortfall == nil {
				t.Fatalf("overdrawn and shortfall_cents must always be present: %s", page.Items[0])
			}
			if *v.Overdrawn != tc.wantOverdrawn || *v.Shortfall != tc.wantShortfall {
				t.Errorf("overdrawn=%v shortfall=%d, want %v %d", *v.Overdrawn, *v.Shortfall, tc.wantOverdrawn, tc.wantShortfall)
			}
			if v.Balance != tc.wantBalance || v.Available != tc.wantAvl {
				t.Errorf("balance=%d available=%d, want %d %d", v.Balance, v.Available, tc.wantBalance, tc.wantAvl)
			}
			// The true signed balance is recoverable: lowest reported amount - shortfall.
			if got := min(v.Balance, v.Available) - *v.Shortfall; got != min(tc.posted, tc.available) {
				t.Errorf("signed balance = %d, want %d", got, min(tc.posted, tc.available))
			}
		})
	}
}

func TestTrustListPages(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		ref       string
		wantItems []int
	}{
		{"ledgers one page", "/v1/trust/ledgers", "TrustLedger", []int{3}},
		{"ledgers pages of two", "/v1/trust/ledgers?limit=2", "TrustLedger", []int{2, 1}},
		{"ledgers limit above maximum is capped", "/v1/trust/ledgers?limit=5000", "TrustLedger", []int{3}},
		{"lines pages of two", "/v1/trust/ledgers/" + string(ledgerID(1)) + "/lines?limit=2", "TrustJournalLine", []int{2, 2, 1}},
		{"lines limit above maximum is capped", "/v1/trust/ledgers/" + string(ledgerID(1)) + "/lines?limit=5000", "TrustJournalLine", []int{5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, h := newRig(t, 3, 5)
			path := tc.path
			seen := map[string]bool{}
			for i, want := range tc.wantItems {
				rec := do(h, http.MethodGet, path, userViewer)
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d status = %d (%s)", i, rec.Code, rec.Body)
				}
				if tc.ref == "TrustLedger" {
					validateLedgerList(t, rec.Body.Bytes())
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
					validate(t, "#/components/schemas/"+tc.ref, it)
					var v struct{ ID string }
					_ = json.Unmarshal(it, &v)
					if v.ID == ledgerHidden || seen[v.ID] {
						t.Errorf("page %d repeats or leaks %s", i, v.ID)
					}
					seen[v.ID] = true
				}
				last := i == len(tc.wantItems)-1
				if (page.Next != "") == last {
					t.Fatalf("page %d next = %q, last = %v", i, page.Next, last)
				}
				base := tc.path
				if j := bytes.IndexByte([]byte(base), '?'); j >= 0 {
					base = base[:j]
				}
				path = base + "?cursor=" + page.Next
			}
		})
	}
}
