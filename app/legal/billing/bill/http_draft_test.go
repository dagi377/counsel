package bill_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const matterNoWIP = "0190a000-0000-7000-8000-0000000000e0"

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// fakeDrafts drafts into the bill fake and records each request.
type fakeDrafts struct {
	bills   *countingBills
	creates []bill.DraftRequest
	edits   int
	n       int
}

func (d *fakeDrafts) Create(ctx context.Context, _ pgx.Tx, r bill.DraftRequest) (bill.ID, error) {
	d.creates = append(d.creates, r)
	if r.MatterID == matterNoWIP {
		return "", fmt.Errorf("draft: %w", bill.ErrNoUnbilledWIP)
	}
	d.n++
	id := bill.ID(fmt.Sprintf("0190d000-0000-7000-8000-%012d", d.n))
	b := bill.Bill{ID: id, MatterID: r.MatterID, PayerID: payerA, Status: bill.StatusDraft, PreparedBy: r.PreparedBy,
		PeriodFrom: day(1), PeriodTo: r.PeriodTo, FeesCents: 10000, TaxRatePPM: 130000, TaxCents: 1300,
		TotalCents: 11300, BalanceCents: 11300, CreatedAt: day(30)}
	return id, d.bills.Fake.Put(b)
}

// Edit applies the period, then the discount through Update, which recomputes the total.
func (d *fakeDrafts) Edit(ctx context.Context, tx pgx.Tx, id bill.ID, e bill.DraftEdit) error {
	d.edits++
	b, err := d.bills.Fake.Get(ctx, id)
	if err != nil {
		return err
	}
	if b.Status != bill.StatusDraft {
		return bill.ErrNotEditable
	}
	if e.PeriodTo != nil {
		b.PeriodTo = *e.PeriodTo
		if err := d.bills.Fake.Put(b); err != nil {
			return err
		}
	}
	_, err = d.bills.Fake.Update(ctx, tx, id, bill.Change{DiscountCents: e.DiscountCents})
	return err
}

func (d *fakeDrafts) calls() int { return len(d.creates) + d.edits }

type draftRig struct {
	h      http.Handler
	bills  *countingBills
	drafts *fakeDrafts
}

func newDraftRig(t *testing.T) draftRig {
	t.Helper()
	bills := seedBills(t)
	drafts := &fakeDrafts{bills: bills}
	h := &bill.DraftHTTP{Bills: bills, Drafts: drafts, DB: txStub{}, Access: testAccess, Idem: idemfake.New(),
		Problems: &server.Problems{}, Principal: func(r *http.Request) (registry.Principal, bool) {
			if r.Header.Get("X-Test-User") == "bot" {
				return registry.Principal{Kind: registry.PrincipalAgent, UserID: "bot", ActingFor: "alice"}, true
			}
			return testPrincipal(r)
		}}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return draftRig{h: m.Handler(), bills: bills, drafts: drafts}
}

const draftJSON = `{"matter_id":"` + matterA + `","period_to":"2026-09-30"}`

func TestARC_I_051_BillDraftProblems(t *testing.T) {
	key := map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000001"}
	ifm := func(v string) map[string]string { return map[string]string{"If-Match": v} }
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
		{"post without principal", "POST", "/v1/bills", "", draftJSON, key, 401, bill.CodeUnauthenticated, true},
		{"patch without principal", "PATCH", "/v1/bills/" + billDraft, "", `{"discount_cents":5}`, ifm(`"1"`), 401, bill.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"post without billing.draft", "POST", "/v1/bills", "viewer", draftJSON, key, 403, bill.CodeMissingCapability, true},
		{"patch without billing.draft", "PATCH", "/v1/bills/" + billDraft, "viewer", `{"discount_cents":5}`, ifm(`"1"`), 403, bill.CodeMissingCapability, true},
		// rule: ARC-I-007
		{"post missing idempotency key", "POST", "/v1/bills", "alice", draftJSON, nil, 400, idem.CodeKeyRequired, true},
		{"post unknown body field", "POST", "/v1/bills", "alice", `{"matter_id":"` + matterA + `","period_to":"2026-09-30","total_cents":1}`, key, 400, server.CodeValidationFailed, true},
		{"post malformed matter id", "POST", "/v1/bills", "alice", `{"matter_id":"x","period_to":"2026-09-30"}`, key, 400, server.CodeValidationFailed, true},
		{"post bad line action", "POST", "/v1/bills", "alice", `{"matter_id":"` + matterA + `","period_to":"2026-09-30","line_actions":[{"source_id":"` + matterA + `","action":"approve"}]}`, key, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-042
		{"post on walled matter is 404", "POST", "/v1/bills", "alice", `{"matter_id":"` + matterHidden + `","period_to":"2026-09-30"}`, key, 404, bill.CodeNotFound, true},
		{"post with no unbilled WIP", "POST", "/v1/bills", "alice", `{"matter_id":"` + matterNoWIP + `","period_to":"2026-09-30"}`, key, 422, bill.CodeNoUnbilledWIP, false},
		{"patch malformed id", "PATCH", "/v1/bills/nope", "alice", `{"discount_cents":5}`, ifm(`"1"`), 400, server.CodeValidationFailed, true},
		{"patch missing If-Match", "PATCH", "/v1/bills/" + billDraft, "alice", `{"discount_cents":5}`, nil, 428, server.CodePreconditionRequired, true},
		{"patch carrying status", "PATCH", "/v1/bills/" + billDraft, "alice", `{"status":"approved"}`, ifm(`"1"`), 400, server.CodeValidationFailed, true},
		{"patch carrying number", "PATCH", "/v1/bills/" + billDraft, "alice", `{"number":"INV-1"}`, ifm(`"1"`), 400, server.CodeValidationFailed, true},
		{"patch carrying totals", "PATCH", "/v1/bills/" + billDraft, "alice", `{"total_cents":1}`, ifm(`"1"`), 400, server.CodeValidationFailed, true},
		{"patch stale version", "PATCH", "/v1/bills/" + billDraft, "alice", `{"discount_cents":5}`, ifm(`"9"`), 412, server.CodeVersionConflict, false},
		{"patch approved bill", "PATCH", "/v1/bills/" + billApproved, "alice", `{"discount_cents":5}`, ifm(`"1"`), 409, bill.CodeBillNotEditable, false},
		{"patch walled bill is 404", "PATCH", "/v1/bills/" + billHidden, "alice", `{"discount_cents":5}`, ifm(`"1"`), 404, bill.CodeNotFound, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDraftRig(t)
			rec := do(r.h, tc.method, tc.path, tc.user, tc.body, tc.hdr)
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && (r.bills.total() != 0 || r.drafts.calls() != 0) {
				t.Errorf("service calls = %v drafts %d, want 0", r.bills.calls, r.drafts.calls())
			}
			if tc.method == "PATCH" && r.drafts.edits != 0 {
				t.Errorf("Edit called %d times on a refused patch", r.drafts.edits)
			}
		})
	}
}

func TestBillDraftCreated(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		preparer string
	}{
		{"user drafts", "alice", "alice"},
		{"agent drafts for its user", "bot", "alice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDraftRig(t)
			rec := do(r.h, "POST", "/v1/bills", tc.user, draftJSON, map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000002"})
			if rec.Code != http.StatusCreated || rec.Header().Get("ETag") != `"1"` || rec.Header().Get("Location") == "" {
				t.Fatalf("post = %d %s ETag %q", rec.Code, rec.Body, rec.Header().Get("ETag"))
			}
			validate(t, billSchema, rec.Body.Bytes())
			if len(r.drafts.creates) != 1 || r.drafts.creates[0].PreparedBy != tc.preparer {
				t.Errorf("creates = %+v, want preparer %s", r.drafts.creates, tc.preparer)
			}
		})
	}
}

func TestARC_I_008_BillDraftIdempotency(t *testing.T) {
	r := newDraftRig(t)
	hdr := map[string]string{idem.HeaderName: "0190e000-0000-7000-8000-000000000042"}
	first := do(r.h, "POST", "/v1/bills", "alice", draftJSON, hdr)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", draftJSON, http.StatusCreated, ""},
		// rule: ARC-I-008
		// The rule sets 422 for a reused key with a different body (as kernel/idem does).
		{"same key, different body", `{"matter_id":"` + matterA + `","period_to":"2026-09-29"}`, http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(r.h, "POST", "/v1/bills", "alice", tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" && (rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true") {
				t.Errorf("replay = %s, want %s", rec.Body, first.Body)
			}
		})
	}
	if len(r.drafts.creates) != 1 {
		t.Errorf("Create calls = %d, want 1", len(r.drafts.creates))
	}
}

func TestBillPatchRecomputesTotals(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		total int64
		to    string
	}{
		{"discount recomputes total", `{"discount_cents":1000}`, 112000, "2026-09-30"},
		{"period only", `{"period_to":"2026-09-29"}`, 113000, "2026-09-29"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDraftRig(t)
			rec := do(r.h, "PATCH", "/v1/bills/"+billDraft, "alice", tc.body, map[string]string{"If-Match": `"1"`})
			if rec.Code != http.StatusOK || rec.Header().Get("ETag") != `"2"` {
				t.Fatalf("patch = %d %s ETag %q", rec.Code, rec.Body, rec.Header().Get("ETag"))
			}
			validate(t, billSchema, rec.Body.Bytes())
			var got struct {
				Total    int64  `json:"total_cents"`
				PeriodTo string `json:"period_to"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Total != tc.total || got.PeriodTo != tc.to {
				t.Errorf("bill = %+v, want total %d to %s", got, tc.total, tc.to)
			}
		})
	}
}

func TestARC_I_012_BillDraftWritesDeclareNoGateState(t *testing.T) {
	writes, err := server.DeclaredWrites(newDraftRig(t).h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("writes = %+v, want POST and PATCH", writes)
	}
}
