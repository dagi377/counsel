package bill_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/bill/billfake"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	matterA      = "0190a000-0000-7000-8000-00000000000a"
	matterHidden = "0190a000-0000-7000-8000-0000000000ff"
	payerA       = "0190a000-0000-7000-8000-0000000000c1"
	billDraft    = "0190b000-0000-7000-8000-000000000001"
	billApproved = "0190b000-0000-7000-8000-000000000002"
	billDeliv    = "0190b000-0000-7000-8000-000000000003"
	billHidden   = "0190b000-0000-7000-8000-000000000004"
	billMissing  = "0190b000-0000-7000-8000-0000000000ee"
)

// countingBills counts every service call on the fake.
type countingBills struct {
	*billfake.Fake
	mu    sync.Mutex
	calls map[string]int
}

func (c *countingBills) count(m string) {
	c.mu.Lock()
	c.calls[m]++
	c.mu.Unlock()
}

func (c *countingBills) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.calls {
		n += v
	}
	return n
}

func (c *countingBills) Get(ctx context.Context, id bill.ID) (bill.Bill, error) {
	c.count("Get")
	return c.Fake.Get(ctx, id)
}

func (c *countingBills) List(ctx context.Context, f bill.Filter) ([]bill.Bill, error) {
	c.count("List")
	return c.Fake.List(ctx, f)
}

type stubAccess struct{ caps map[string][]string }

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == matterHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a.caps[p.UserID] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

var testAccess = stubAccess{caps: map[string][]string{
	"alice": {bill.CapView, bill.CapDraft}, "partner": {bill.CapView, bill.CapDraft, bill.CapApprove},
	"viewer": {bill.CapView}, "reader": nil, "bot": {bill.CapDraft},
}}

func testPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get("X-Test-User")
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
}

func testCodec(t *testing.T) api.CursorCodec {
	t.Helper()
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func day(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }

// seedBills stores a draft, an approved, a delivered and a walled bill.
func seedBills(t *testing.T) *countingBills {
	t.Helper()
	f := billfake.New(func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) })
	lines := []bill.Line{
		{Ordinal: 1, ID: "0190c000-0000-7000-8000-000000000001", Source: bill.SourceTime, QuantityMilli: 1500, RateCents: 30000, AmountCents: 45000, Taxable: true},
		{Ordinal: 2, ID: "0190c000-0000-7000-8000-000000000002", Source: bill.SourceExpense, QuantityMilli: 1000, RateCents: 5000, AmountCents: 5000, Taxable: true},
		{Ordinal: 3, ID: "0190c000-0000-7000-8000-000000000003", Source: bill.SourceFlat, QuantityMilli: 1000, RateCents: 50000, AmountCents: 50000, Taxable: true},
	}
	mk := func(id, matter string, st bill.Status) bill.Bill {
		return bill.Bill{ID: bill.ID(id), MatterID: matter, PayerID: payerA, Status: st, PreparedBy: "alice",
			PeriodFrom: day(1), PeriodTo: day(30), Lines: lines, FeesCents: 95000, TaxableDisbCents: 5000,
			TaxRatePPM: 130000, TaxCents: 13000, TotalCents: 113000, BalanceCents: 113000, CreatedAt: day(30)}
	}
	ap := mk(billApproved, matterA, bill.StatusApproved)
	ap.Number, ap.ApprovedBy, ap.ApprovedAt, ap.SignedBy = "INV-2026-0001", payerA, day(30), "partner"
	dl := mk(billDeliv, matterA, bill.StatusDelivered)
	dl.Number, dl.ApprovedBy, dl.ApprovedAt, dl.DeliveredAt, dl.DeliveryChannel = "INV-2026-0002", payerA, day(30), day(30), bill.ChannelMail
	for _, b := range []bill.Bill{mk(billDraft, matterA, bill.StatusDraft), ap, dl, mk(billHidden, matterHidden, bill.StatusDraft)} {
		if err := f.Put(b); err != nil {
			t.Fatal(err)
		}
	}
	return &countingBills{Fake: f, calls: map[string]int{}}
}

func newReadRig(t *testing.T) (http.Handler, *countingBills) {
	t.Helper()
	bills := seedBills(t)
	h := &bill.HTTP{Bills: bills, Access: testAccess, Cursors: testCodec(t), Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return m.Handler(), bills
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

// openAPISchemas compiles schemas from api/openapi.yaml.
var openAPISchemas = sync.OnceValues(func() (*jsonschema.Compiler, error) {
	raw, err := os.ReadFile(openAPIPath)
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

const openAPIPath = "../../../../api/openapi.yaml"

// validate checks body against the schema at ref (a JSON pointer into openapi.json).
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
	if p.Code != code || p.Status != status {
		t.Errorf("problem = %+v, want %d %s", p, status, code)
	}
}

const (
	billSchema     = "#/components/schemas/Bill"
	billListSchema = "#/paths/~1bills/get/responses/200/content/application~1json/schema"
	lineListSchema = "#/paths/~1bills~1{bill_id}~1lines/get/responses/200/content/application~1json/schema"
)

func TestARC_I_051_BillReadProblems(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		user      string
		status    int
		code      string
		zeroCalls bool
	}{
		{"list without principal", "/v1/bills", "", 401, bill.CodeUnauthenticated, true},
		{"get without principal", "/v1/bills/" + billDraft, "", 401, bill.CodeUnauthenticated, true},
		{"lines without principal", "/v1/bills/" + billDraft + "/lines", "", 401, bill.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"list without billing.view", "/v1/bills", "reader", 403, bill.CodeMissingCapability, true},
		{"get without billing.view", "/v1/bills/" + billDraft, "reader", 403, bill.CodeMissingCapability, true},
		{"lines without billing.view", "/v1/bills/" + billDraft + "/lines", "reader", 403, bill.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"get on walled matter is 404", "/v1/bills/" + billHidden, "viewer", 404, bill.CodeNotFound, false},
		{"lines on walled matter is 404", "/v1/bills/" + billHidden + "/lines", "viewer", 404, bill.CodeNotFound, false},
		{"list filtered by walled matter is 404", "/v1/bills?matter_id=" + matterHidden, "viewer", 404, bill.CodeNotFound, true},
		{"get missing bill", "/v1/bills/" + billMissing, "viewer", 404, bill.CodeNotFound, false},
		{"get malformed id", "/v1/bills/not-a-uuid", "viewer", 400, server.CodeValidationFailed, true},
		{"lines malformed id", "/v1/bills/not-a-uuid/lines", "viewer", 400, server.CodeValidationFailed, true},
		{"list malformed matter id", "/v1/bills?matter_id=x", "viewer", 400, server.CodeValidationFailed, true},
		{"list unknown status", "/v1/bills?status=lost", "viewer", 400, server.CodeValidationFailed, true},
		{"list tampered cursor", "/v1/bills?cursor=AAAA.BBBB", "viewer", 400, api.CodeBadCursor, true},
		{"lines tampered cursor", "/v1/bills/" + billDraft + "/lines?cursor=AAAA.BBBB", "viewer", 400, api.CodeBadCursor, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, bills := newReadRig(t)
			rec := do(h, "GET", tc.path, tc.user, "", nil)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && bills.total() != 0 {
				t.Errorf("service calls = %v, want 0", bills.calls)
			}
		})
	}
}

func TestBillsUnwiredAnswer503(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&bill.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/bills", "viewer", "", nil), 503, bill.CodeUnavailable)
}

type listPage struct {
	Data       []map[string]any `json:"data"`
	NextCursor *string          `json:"next_cursor"`
}

func TestBillList(t *testing.T) {
	tests := []struct {
		name  string
		query string
		pages []int
		want  string // status every item has, when set
	}{
		{"hidden matter left out", "", []int{3}, ""},
		{"by matter", "?matter_id=" + matterA, []int{3}, ""},
		{"by status draft", "?status=draft", []int{1}, "draft"},
		{"by status approved", "?status=approved", []int{1}, "approved"},
		{"returned matches nothing", "?status=returned", []int{0}, ""},
		{"pages of two", "?limit=2", []int{2, 1}, ""},
		{"limit above maximum is capped", "?limit=5000", []int{3}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newReadRig(t)
			path := "/v1/bills" + tc.query
			for i, want := range tc.pages {
				rec := do(h, "GET", path, "viewer", "", nil)
				if rec.Code != 200 {
					t.Fatalf("page %d: %d %s", i, rec.Code, rec.Body)
				}
				validate(t, billListSchema, rec.Body.Bytes())
				var page listPage
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if len(page.Data) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Data), want)
				}
				for _, it := range page.Data {
					if it["matter_id"] == matterHidden || (tc.want != "" && it["status"] != tc.want) {
						t.Errorf("unexpected item %v", it)
					}
				}
				if last := i == len(tc.pages)-1; (page.NextCursor != nil) == last {
					t.Fatalf("page %d next = %v", i, page.NextCursor)
				}
				if page.NextCursor != nil {
					path = "/v1/bills?limit=2&cursor=" + *page.NextCursor
				}
			}
		})
	}
}

func TestBillGetReturnsStoredTotals(t *testing.T) {
	tests := []struct {
		id     string
		status string
		number bool
		etag   string
	}{
		{billDraft, "draft", false, `"1"`},
		{billApproved, "approved", true, `"1"`},
		{billDeliv, "delivered", true, `"1"`},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			h, _ := newReadRig(t)
			rec := do(h, "GET", "/v1/bills/"+tc.id, "viewer", "", nil)
			if rec.Code != 200 || rec.Header().Get("ETag") != tc.etag {
				t.Fatalf("get = %d %s ETag %q", rec.Code, rec.Body, rec.Header().Get("ETag"))
			}
			validate(t, billSchema, rec.Body.Bytes())
			var got struct {
				Status     string  `json:"status"`
				Number     *string `json:"number"`
				TotalCents int64   `json:"total_cents"`
				Balance    int64   `json:"balance_cents"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.status || (got.Number != nil) != tc.number || got.TotalCents != 113000 || got.Balance != 113000 {
				t.Errorf("bill = %+v", got)
			}
		})
	}
}

func TestBillLinesPaged(t *testing.T) {
	h, _ := newReadRig(t)
	path := "/v1/bills/" + billDraft + "/lines?limit=2"
	var seen []string
	for i, want := range []int{2, 1} {
		rec := do(h, "GET", path, "viewer", "", nil)
		if rec.Code != 200 {
			t.Fatalf("page %d: %d %s", i, rec.Code, rec.Body)
		}
		validate(t, lineListSchema, rec.Body.Bytes())
		var page listPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Data) != want {
			t.Fatalf("page %d lines = %d, want %d", i, len(page.Data), want)
		}
		for _, l := range page.Data {
			seen = append(seen, l["source_type"].(string))
		}
		if page.NextCursor != nil {
			path = "/v1/bills/" + billDraft + "/lines?cursor=" + *page.NextCursor
		}
	}
	if len(seen) != 3 || seen[0] != "time" || seen[2] != "flat" {
		t.Errorf("lines = %v", seen)
	}
}
