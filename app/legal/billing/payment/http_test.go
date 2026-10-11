package payment_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/payment/paymentfake"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	clientA    = "0190a000-0000-7000-8000-0000000000c1"
	clientB    = "0190a000-0000-7000-8000-0000000000c2"
	billA      = "0190b000-0000-7000-8000-000000000001"
	billB      = "0190b000-0000-7000-8000-000000000002"
	payHidden  = "0190f000-0000-7000-8000-0000000000ff"
	payMissing = "0190f000-0000-7000-8000-0000000000fe"
	key1       = "0190e000-0000-7000-8000-000000000301"

	paymentSchema = "#/components/schemas/Payment"
	listSchema    = "#/paths/~1payments/get/responses/200/content/application~1json/schema"
)

var users = map[string][]string{
	"clerk":  {payment.CapPayments, payment.CapView},
	"viewer": {payment.CapView},
	"reader": {"contacts.view"},
}

type testAccess struct{}

func (testAccess) Can(_ context.Context, p registry.Principal, c string, s registry.Subject) (bool, error) {
	if s.ID == payHidden {
		return false, identity.ErrNotVisible
	}
	return slices.Contains(users[p.UserID], c), nil
}

func testPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get("X-Test-User")
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
}

// countingPayments wraps the fake and counts every service call.
type countingPayments struct {
	*paymentfake.Fake
	calls   int
	filters []payment.ListFilter
}

func (c *countingPayments) Capture(ctx context.Context, cp payment.Capture) (payment.Payment, []payment.Allocation, error) {
	c.calls++
	return c.Fake.Capture(ctx, cp)
}

func (c *countingPayments) Get(ctx context.Context, id string) (payment.Payment, error) {
	c.calls++
	return c.Fake.Get(ctx, id)
}

func (c *countingPayments) List(ctx context.Context, f payment.ListFilter) ([]payment.Payment, error) {
	c.calls++
	c.filters = append(c.filters, f)
	return c.Fake.List(ctx, f)
}

type rig struct {
	h   http.Handler
	svc *countingPayments
}

func newRig(t *testing.T) rig {
	t.Helper()
	fake := paymentfake.New()
	n := 0
	fake.NewID = func() string { n++; return fmt.Sprintf("0190f000-0000-7000-8000-%012d", n) }
	fake.AddBill(billA, clientA, 10000, 1300)
	fake.AddBill(billB, clientB, 5000, 650)
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	svc := &countingPayments{Fake: fake}
	h := &payment.HTTP{Payments: svc, Access: testAccess{}, Cursors: codec, Idem: idemfake.New(),
		Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return rig{h: m.Handler(), svc: svc}
}

// seed records n payments for clientA on billA and resets the call count.
func (r rig) seed(t *testing.T, n int) []string {
	t.Helper()
	var ids []string
	for i := 0; i < n; i++ {
		p, _, err := r.svc.Fake.Capture(context.Background(), payment.Capture{ClientID: clientA, BillID: billA,
			Kind: payment.KindBillPayment, AmountCents: 1000, Method: "eft", IdempotencyKey: fmt.Sprint("seed", i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	return ids
}

func do(h http.Handler, method, path, user, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

func checkProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) api.Problem {
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
	return p
}

func payBody(extra string) string {
	return `{"received_at":"2026-10-01T15:00:00Z","payer_contact_id":"` + clientA + `","amount_cents":5000,"method":"eft"` + extra + `}`
}

var withKey = map[string]string{idem.HeaderName: key1}

func TestARC_I_051_PaymentProblems(t *testing.T) {
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
		{"list without principal", "GET", "/v1/payments", "", "", nil, 401, payment.CodeUnauthenticated, true},
		{"create without principal", "POST", "/v1/payments", "", payBody(""), withKey, 401, payment.CodeUnauthenticated, true},
		{"get without principal", "GET", "/v1/payments/" + payMissing, "", "", nil, 401, payment.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"list without billing.view", "GET", "/v1/payments", "reader", "", nil, 403, payment.CodeMissingCapability, true},
		{"create without billing.payments", "POST", "/v1/payments", "viewer", payBody(""), withKey, 403, payment.CodeMissingCapability, true},
		{"get without billing.view", "GET", "/v1/payments/" + payMissing, "reader", "", nil, 403, payment.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"get walled payment is 404", "GET", "/v1/payments/" + payHidden, "viewer", "", nil, 404, payment.CodeNotFound, true},
		{"get unknown payment", "GET", "/v1/payments/" + payMissing, "viewer", "", nil, 404, payment.CodeNotFound, false},
		{"get malformed id", "GET", "/v1/payments/nope", "viewer", "", nil, 400, server.CodeValidationFailed, true},
		{"list tampered cursor", "GET", "/v1/payments?cursor=abc.def", "viewer", "", nil, 400, api.CodeBadCursor, true},
		{"list bad date", "GET", "/v1/payments?received_from=10/01/2026", "viewer", "", nil, 400, server.CodeValidationFailed, true},
		{"list malformed client_id", "GET", "/v1/payments?client_id=nope", "viewer", "", nil, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-007
		{"create missing Idempotency-Key", "POST", "/v1/payments", "clerk", payBody(""), nil, 400, idem.CodeKeyRequired, true},
		{"create unknown field", "POST", "/v1/payments", "clerk", payBody(`,"account":"trust"`), withKey, 400, server.CodeValidationFailed, true},
		{"create bad method", "POST", "/v1/payments", "clerk", strings.Replace(payBody(""), "eft", "bitcoin", 1), withKey, 400, server.CodeValidationFailed, true},
		{"create negative amount", "POST", "/v1/payments", "clerk", strings.Replace(payBody(""), "5000", "-1", 1), withKey, 400, server.CodeValidationFailed, true},
		{"create two bills", "POST", "/v1/payments", "clerk",
			payBody(`,"allocations":[{"target_type":"bill","target_id":"` + billA + `","amount_cents":1},{"target_type":"bill","target_id":"` + billB + `","amount_cents":1}]`),
			withKey, 422, server.CodeValidationFailed, true},
		{"create on another client's bill", "POST", "/v1/payments", "clerk",
			payBody(`,"allocations":[{"target_type":"bill","target_id":"` + billB + `","amount_cents":5000}]`), withKey, 422, server.CodeValidationFailed, false},
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

func TestPaymentRoutesUnwiredAnswer503(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&payment.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, path string }{{"GET", "/v1/payments"}, {"POST", "/v1/payments"}, {"GET", "/v1/payments/" + payMissing}} {
		checkProblem(t, do(m.Handler(), c.method, c.path, "clerk", payBody(""), withKey), 503, payment.CodeUnavailable)
	}
}

func TestPaymentTrustBodyPointsAtTrustReceipts(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"trust_ledger allocation", payBody(`,"allocations":[{"target_type":"trust_ledger","target_id":"` + billA + `","amount_cents":5000}]`)},
		{"trust_ledger among others", payBody(`,"allocations":[{"target_type":"unapplied","amount_cents":1},{"target_type":"trust_ledger","amount_cents":4999}]`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			p := checkProblem(t, do(r.h, "POST", "/v1/payments", "clerk", tc.body, withKey), 422, payment.CodeUseTrustReceipts)
			if !strings.Contains(p.Detail, "/trust/receipts") {
				t.Errorf("detail = %q, want it to point at /trust/receipts", p.Detail)
			}
			if r.svc.calls != 0 {
				t.Errorf("service calls = %d, want 0", r.svc.calls)
			}
		})
	}
}

// rule: ARC-I-007
// rule: ARC-I-008
func TestPaymentCreateIdempotent(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantState string
		wantTypes []string
	}{
		{"applied to one bill", payBody(`,"allocations":[{"target_type":"bill","target_id":"` + billA + `","amount_cents":5000}]`), "allocated", []string{"bill"}},
		{"overpayment leaves unapplied", strings.Replace(payBody(`,"allocations":[{"target_type":"bill","target_id":"`+billA+`","amount_cents":20000}]`), "5000", "20000", 1), "allocated", []string{"bill", "unapplied"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			first := do(r.h, "POST", "/v1/payments", "clerk", tc.body, withKey)
			if first.Code != 201 {
				t.Fatalf("status = %d: %s", first.Code, first.Body)
			}
			validate(t, paymentSchema, first.Body.Bytes())
			again := do(r.h, "POST", "/v1/payments", "clerk", tc.body, withKey)
			if again.Code != 201 || again.Body.String() != first.Body.String() {
				t.Errorf("replay = %d %s, want the first response", again.Code, again.Body)
			}
			if r.svc.calls != 1 {
				t.Errorf("Capture calls = %d, want 1", r.svc.calls)
			}
			var v struct {
				Status      string `json:"status"`
				Allocations []struct {
					TargetType string `json:"target_type"`
				} `json:"allocations"`
			}
			if err := json.Unmarshal(first.Body.Bytes(), &v); err != nil {
				t.Fatal(err)
			}
			var types []string
			for _, a := range v.Allocations {
				types = append(types, a.TargetType)
			}
			if v.Status != tc.wantState || !slices.Equal(types, tc.wantTypes) {
				t.Errorf("status %q, allocations %v; want %q, %v", v.Status, types, tc.wantState, tc.wantTypes)
			}
			reused := do(r.h, "POST", "/v1/payments", "clerk", strings.Replace(tc.body, "eft", "wire", 1), withKey)
			checkProblem(t, reused, 422, idem.CodeKeyReused)
			if r.svc.calls != 1 {
				t.Errorf("Capture calls after reuse = %d, want 1", r.svc.calls)
			}
		})
	}
}

func TestPaymentListAndGet(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		seed      int
		wantN     int
		wantNext  bool
		wantLimit int
	}{
		{"default page", "", 3, 3, false, api.DefaultLimit + 1},
		{"limit above maximum is capped", "?limit=5000", 2, 2, false, api.MaxLimit + 1},
		{"small page has next cursor", "?limit=2", 3, 2, true, 3},
		{"client filter", "?client_id=" + clientB, 2, 0, false, api.DefaultLimit + 1},
		{"date filter", "?received_from=2026-01-01&received_to=2026-12-31", 2, 0, false, api.DefaultLimit + 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			ids := r.seed(t, tc.seed)
			rec := do(r.h, "GET", "/v1/payments"+tc.query, "viewer", "", nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			validate(t, listSchema, rec.Body.Bytes())
			var page struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
				Next *string `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if got := r.svc.filters[0].Limit; got != tc.wantLimit {
				t.Errorf("service limit = %d, want %d", got, tc.wantLimit)
			}
			if len(page.Data) != tc.wantN || (page.Next != nil) != tc.wantNext {
				t.Fatalf("got %d items, next %v; want %d, %v", len(page.Data), page.Next, tc.wantN, tc.wantNext)
			}
			if tc.wantNext {
				rec = do(r.h, "GET", "/v1/payments?cursor="+*page.Next, "viewer", "", nil)
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if len(page.Data) != 1 || page.Data[0].ID != ids[2] {
					t.Errorf("second page = %+v, want [%s]", page.Data, ids[2])
				}
			}
			if len(ids) > 0 {
				get := do(r.h, "GET", "/v1/payments/"+ids[0], "viewer", "", nil)
				if get.Code != 200 || get.Header().Get("ETag") == "" {
					t.Fatalf("get = %d %s", get.Code, get.Body)
				}
				validate(t, paymentSchema, get.Body.Bytes())
			}
		})
	}
}
