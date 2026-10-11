package timekeeping_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// fakeExpenses is an in-memory Expenses that counts every call. fail, when set, is returned
// by Record.
type fakeExpenses struct {
	mu    sync.Mutex
	rows  map[tk.ExpenseID]tk.Expense
	n     int
	calls int
	fail  error
}

func (f *fakeExpenses) Record(_ context.Context, _ pgx.Tx, e tk.Expense, by tk.Actor) (tk.ExpenseID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.fail != nil {
		return "", f.fail
	}
	if by.Agent {
		return "", tk.ErrAgentForbidden
	}
	e.UserID = by.UserID
	if err := tk.ValidateExpense(e); err != nil {
		return "", err
	}
	f.n++
	e.ID = tk.ExpenseID(fmt.Sprintf("ex-%02d", f.n))
	e.Status, e.RowVersion = tk.ExpenseDraft, 1
	f.rows[e.ID] = e
	return e.ID, nil
}

func (f *fakeExpenses) Get(_ context.Context, id tk.ExpenseID) (tk.Expense, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	e, ok := f.rows[id]
	if !ok {
		return tk.Expense{}, tk.ErrNotFound
	}
	return e, nil
}

func (f *fakeExpenses) List(_ context.Context, flt tk.ExpenseFilter) ([]tk.Expense, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	out := []tk.Expense{}
	for _, e := range f.rows {
		if (flt.UserID == "" || e.UserID == flt.UserID) && (flt.After == "" || e.ID > flt.After) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if flt.Limit > 0 && len(out) > flt.Limit {
		out = out[:flt.Limit]
	}
	return out, nil
}

type expenseRig struct {
	h   http.Handler
	exp *fakeExpenses
}

func newExpenseRig(t *testing.T) expenseRig {
	t.Helper()
	f := &fakeExpenses{rows: map[tk.ExpenseID]tk.Expense{}}
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, u := range []string{"alice", "alice", "bob"} {
		e := tk.Expense{MatterID: matterA, ExpenseDate: day, AmountPreTaxCents: 1250, PaidFrom: "operating", Billable: true}
		if _, err := f.Record(context.Background(), nil, e, tk.Actor{UserID: u}); err != nil {
			t.Fatal(err)
		}
	}
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &tk.ExpenseHTTP{Expenses: f, DB: txStub{}, Cursors: codec, Idem: idemfake.New(), Problems: &server.Problems{},
		Access: stubAccess{caps: map[string][]string{
			"alice": {tk.CapRecord}, "bob": {tk.CapRecord}, "bot": {tk.CapRecord},
			"manager": {tk.CapRecord, tk.CapViewAll}, "reader": nil,
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
	f.calls = 0
	return expenseRig{h: m.Handler(), exp: f}
}

const expenseJSON = `{"matter_id":"` + matterA + `","expense_date":"2026-10-02","vendor":"Courier","amount_pre_tax_cents":4500,"vendor_tax_cents":585,"paid_from":"card"}`

func TestARC_I_051_ExpenseProblems(t *testing.T) {
	key := map[string]string{idem.HeaderName: "key-00000001"}
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
		{"list without principal", "GET", "/v1/expenses", "", "", nil, 401, tk.CodeUnauthenticated, true},
		{"post without principal", "POST", "/v1/expenses", "", expenseJSON, key, 401, tk.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"list without time.record", "GET", "/v1/expenses", "reader", "", nil, 403, tk.CodeMissingCapability, true},
		{"post without time.record", "POST", "/v1/expenses", "reader", expenseJSON, key, 403, tk.CodeMissingCapability, true},
		// rule: ARC-I-007
		{"post missing idempotency key", "POST", "/v1/expenses", "alice", expenseJSON, nil, 400, idem.CodeKeyRequired, true},
		{"post unknown body field", "POST", "/v1/expenses", "alice", `{"matter_id":"` + matterA + `","status":"approved"}`, key, 400, server.CodeValidationFailed, true},
		{"post malformed matter id", "POST", "/v1/expenses", "alice", `{"matter_id":"x","expense_date":"2026-10-02","amount_pre_tax_cents":5,"paid_from":"card"}`, key, 400, server.CodeValidationFailed, true},
		{"post fractional cents", "POST", "/v1/expenses", "alice", `{"matter_id":"` + matterA + `","expense_date":"2026-10-02","amount_pre_tax_cents":5.5,"paid_from":"card"}`, key, 400, server.CodeValidationFailed, true},
		{"post zero amount", "POST", "/v1/expenses", "alice", `{"matter_id":"` + matterA + `","expense_date":"2026-10-02","amount_pre_tax_cents":0,"paid_from":"card"}`, key, 400, server.CodeValidationFailed, true},
		{"post unknown paid_from", "POST", "/v1/expenses", "alice", `{"matter_id":"` + matterA + `","expense_date":"2026-10-02","amount_pre_tax_cents":5,"paid_from":"cash"}`, key, 400, server.CodeValidationFailed, true},
		{"post bad date", "POST", "/v1/expenses", "alice", `{"matter_id":"` + matterA + `","expense_date":"02/10/2026","amount_pre_tax_cents":5,"paid_from":"card"}`, key, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-042
		{"post on walled matter is 404", "POST", "/v1/expenses", "alice", `{"matter_id":"` + matterHidden + `","expense_date":"2026-10-02","amount_pre_tax_cents":5,"paid_from":"card"}`, key, 404, tk.CodeNotFound, true},
		{"agent never records", "POST", "/v1/expenses", "bot", expenseJSON, key, 403, tk.CodeMissingCapability, false},
		{"tampered cursor", "GET", "/v1/expenses?cursor=AAAA.BBBB", "alice", "", nil, 400, api.CodeBadCursor, true},
		{"post records", "POST", "/v1/expenses", "alice", expenseJSON, key, 201, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newExpenseRig(t)
			rec := do(r.h, tc.method, tc.path, tc.user, tc.body, tc.hdr)
			if tc.code == "" {
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
				}
				return
			}
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.exp.calls != 0 {
				t.Errorf("service calls = %d, want 0", r.exp.calls)
			}
		})
	}
}

func TestExpenseServiceErrorsMapToProblems(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"locked to bill", fmt.Errorf("record: %w", tk.ErrBilledLocked), 409, tk.CodeLockedToBill},
		{"invalid", fmt.Errorf("record: %w", tk.ErrInvalid), 422, server.CodeValidationFailed},
		{"not found", tk.ErrNotFound, 404, tk.CodeNotFound},
		{"unexpected", errors.New("boom"), 500, server.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newExpenseRig(t)
			r.exp.fail = tc.err
			rec := do(r.h, "POST", "/v1/expenses", "alice", expenseJSON, map[string]string{idem.HeaderName: "key-00000009"})
			checkProblem(t, rec, tc.status, tc.code)
			if strings.Contains(rec.Body.String(), "boom") {
				t.Errorf("error text leaked: %s", rec.Body)
			}
		})
	}
}

func TestARC_I_008_ExpenseIdempotency(t *testing.T) {
	r := newExpenseRig(t)
	hdr := map[string]string{idem.HeaderName: "key-00000042"}
	first := do(r.h, "POST", "/v1/expenses", "alice", expenseJSON, hdr)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	var created struct {
		Status string `json:"status"`
		Amount int64  `json:"amount_pre_tax_cents"`
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "draft" || created.Amount != 4500 || created.UserID != "alice" {
		t.Errorf("created = %+v", created)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", expenseJSON, http.StatusCreated, ""},
		// rule: ARC-I-008
		// The rule sets 422 for a reused key with a different body (as kernel/idem does).
		{"same key, different body", `{"matter_id":"` + matterA + `","expense_date":"2026-10-02","amount_pre_tax_cents":9999,"paid_from":"card"}`, http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(r.h, "POST", "/v1/expenses", "alice", tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" && (rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true") {
				t.Errorf("replay = %s, want %s", rec.Body, first.Body)
			}
		})
	}
	all, _ := r.exp.List(context.Background(), tk.ExpenseFilter{UserID: "alice"})
	if len(all) != 3 {
		t.Errorf("alice expenses = %d, want 3 (one recorded)", len(all))
	}
}

func TestExpenseListOwnOrAll(t *testing.T) {
	tests := []struct {
		name  string
		user  string
		query string
		pages []int
	}{
		{"own expenses only", "alice", "", []int{2}},
		{"view_all sees everyone", "manager", "", []int{3}},
		{"pages of two", "manager", "?limit=2", []int{2, 1}},
		{"limit above maximum is capped", "manager", "?limit=5000", []int{3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newExpenseRig(t)
			path := "/v1/expenses" + tc.query
			for i, want := range tc.pages {
				rec := do(r.h, "GET", path, tc.user, "", nil)
				var page struct {
					Items []map[string]any `json:"items"`
					Next  string           `json:"next"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil || rec.Code != 200 {
					t.Fatalf("page %d: %d %s", i, rec.Code, rec.Body)
				}
				if len(page.Items) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Items), want)
				}
				for _, it := range page.Items {
					if tc.user == "alice" && it["user_id"] != "alice" {
						t.Errorf("alice sees %v", it)
					}
				}
				if last := i == len(tc.pages)-1; (page.Next != "") == last {
					t.Fatalf("page %d next = %q", i, page.Next)
				}
				path = "/v1/expenses?cursor=" + page.Next
			}
		})
	}
}

func TestExpenseRoutesAnswer503Unwired(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&tk.ExpenseHTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	rec := do(m.Handler(), "GET", "/v1/expenses", "alice", "", nil)
	checkProblem(t, rec, http.StatusServiceUnavailable, tk.CodeUnavailable)
}

func TestARC_I_012_ExpenseWritesDeclareNoGateState(t *testing.T) {
	writes, err := server.DeclaredWrites(newExpenseRig(t).h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 {
		t.Fatalf("writes = %+v, want POST /expenses", writes)
	}
}

func TestValidateExpense(t *testing.T) {
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ok := tk.Expense{MatterID: matterA, UserID: "u", ExpenseDate: day, AmountPreTaxCents: 1, PaidFrom: "unpaid"}
	tests := []struct {
		name  string
		edit  func(e *tk.Expense)
		valid bool
	}{
		{"valid", func(*tk.Expense) {}, true},
		{"no matter", func(e *tk.Expense) { e.MatterID = "" }, false},
		{"no date", func(e *tk.Expense) { e.ExpenseDate = time.Time{} }, false},
		{"zero amount", func(e *tk.Expense) { e.AmountPreTaxCents = 0 }, false},
		{"negative tax", func(e *tk.Expense) { e.VendorTaxCents = -1 }, false},
		{"bad paid_from", func(e *tk.Expense) { e.PaidFrom = "cash" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := ok
			tc.edit(&e)
			err := tk.ValidateExpense(e)
			if (err == nil) != tc.valid || (err != nil && !errors.Is(err, tk.ErrInvalid)) {
				t.Errorf("err = %v, valid want %v", err, tc.valid)
			}
		})
	}
}
