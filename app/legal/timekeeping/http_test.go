package timekeeping_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"time"

	"github.com/jackc/pgx/v5"

	tk "github.com/dagi377/counsel/app/legal/timekeeping"
	"github.com/dagi377/counsel/app/legal/timekeeping/timekeepingfake"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	matterA      = "0190a000-0000-7000-8000-00000000000a"
	matterHidden = "0190a000-0000-7000-8000-0000000000ff"
)

type txStub struct{}

// listPage is the openapi list page shape, {data, next_cursor} as GET /bills declares it.
// GET /time-entries and GET /expenses declares no 200 schema of its own, so its pages are checked against this shape.
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

func (txStub) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, nil)
}

// countingEntries counts every service call on the fake.
type countingEntries struct {
	*timekeepingfake.Fake
	calls int
}

func (c *countingEntries) Record(ctx context.Context, tx pgx.Tx, e tk.TimeEntry, by tk.Actor) (tk.EntryID, error) {
	c.calls++
	return c.Fake.Record(ctx, tx, e, by)
}

func (c *countingEntries) List(ctx context.Context, f tk.EntryFilter) ([]tk.TimeEntry, error) {
	c.calls++
	return c.Fake.List(ctx, f)
}

func (c *countingEntries) Get(ctx context.Context, id tk.EntryID) (tk.TimeEntry, error) {
	c.calls++
	return c.Fake.Get(ctx, id)
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

type rig struct {
	h       http.Handler
	entries *countingEntries
}

func newRig(t *testing.T) rig {
	t.Helper()
	f := &countingEntries{Fake: timekeepingfake.New([]string{"para"}, []string{"research"})}
	ctx := context.Background()
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, u := range []string{"alice", "alice", "bob"} {
		if _, err := f.Fake.Record(ctx, nil, tk.TimeEntry{MatterID: matterA, UserID: u, WorkDate: day, Minutes: 30, Narrative: "n"}, tk.Actor{UserID: u}); err != nil {
			t.Fatal(err)
		}
	}
	// te-4 is alice's entry on a bill.
	if _, err := f.Fake.Record(ctx, nil, tk.TimeEntry{MatterID: matterA, UserID: "alice", WorkDate: day, Minutes: 30, Narrative: "n"}, tk.Actor{UserID: "alice"}); err != nil {
		t.Fatal(err)
	}
	for _, to := range []tk.State{tk.StateApproved, tk.StateOnDraftBill} {
		if err := f.Fake.Transition(ctx, nil, "te-4", to, tk.Actor{UserID: "partner"}); err != nil {
			t.Fatal(err)
		}
	}
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &tk.HTTP{Entries: f, DB: txStub{}, Cursors: codec, Idem: idemfake.New(), Problems: &server.Problems{},
		Access: stubAccess{caps: map[string][]string{
			"alice": {tk.CapRecord}, "bob": {tk.CapRecord}, "para": {tk.CapRecord},
			"manager": {tk.CapRecord, tk.CapViewAll}, "reader": nil,
		}},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		},
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	f.calls = 0
	return rig{h: m.Handler(), entries: f}
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

const recordJSON = `{"matter_id":"` + matterA + `","work_date":"2026-10-02","minutes":45,"narrative":"Draft"}`

func TestARC_I_051_TimeEntryProblems(t *testing.T) {
	key := map[string]string{idem.HeaderName: "key-00000001"}
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
		{"list without principal", "GET", "/v1/time-entries", "", "", nil, 401, tk.CodeUnauthenticated, true},
		{"post without principal", "POST", "/v1/time-entries", "", recordJSON, key, 401, tk.CodeUnauthenticated, true},
		{"patch without principal", "PATCH", "/v1/time-entries/te-1", "", `{"minutes":5}`, ifm(`"1"`), 401, tk.CodeUnauthenticated, true},
		{"list without time.record", "GET", "/v1/time-entries", "reader", "", nil, 403, tk.CodeMissingCapability, true},
		{"post without time.record", "POST", "/v1/time-entries", "reader", recordJSON, key, 403, tk.CodeMissingCapability, true},
		{"patch without time.record", "PATCH", "/v1/time-entries/te-1", "reader", `{"minutes":5}`, ifm(`"1"`), 403, tk.CodeMissingCapability, true},
		// rule: ARC-I-007
		{"post missing idempotency key", "POST", "/v1/time-entries", "alice", recordJSON, nil, 400, idem.CodeKeyRequired, true},
		{"post unknown body field", "POST", "/v1/time-entries", "alice", `{"matter_id":"` + matterA + `","colour":"red"}`, key, 400, server.CodeValidationFailed, true},
		{"post malformed matter id", "POST", "/v1/time-entries", "alice", `{"matter_id":"x","work_date":"2026-10-02","minutes":5,"narrative":"n"}`, key, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-042
		{"post on walled matter is 404", "POST", "/v1/time-entries", "alice", `{"matter_id":"` + matterHidden + `","work_date":"2026-10-02","minutes":5,"narrative":"n"}`, key, 404, tk.CodeNotFound, true},
		{"post activity out of paralegal scope", "POST", "/v1/time-entries", "para", `{"matter_id":"` + matterA + `","work_date":"2026-10-02","minutes":5,"narrative":"n","activity_code":"court"}`, key, 201, "", false},
		{"patch malformed id", "PATCH", "/v1/time-entries/te%201", "alice", `{"minutes":5}`, ifm(`"1"`), 400, server.CodeValidationFailed, true},
		{"patch unknown body field", "PATCH", "/v1/time-entries/te-1", "alice", `{"colour":"red"}`, ifm(`"1"`), 400, server.CodeValidationFailed, true},
		{"patch missing If-Match", "PATCH", "/v1/time-entries/te-1", "alice", `{"minutes":5}`, nil, 428, server.CodePreconditionRequired, true},
		{"patch stale version", "PATCH", "/v1/time-entries/te-1", "alice", `{"minutes":5}`, ifm(`"7"`), 412, server.CodeVersionConflict, false},
		{"patch another user's entry is 404", "PATCH", "/v1/time-entries/te-3", "alice", `{"minutes":5}`, ifm(`"1"`), 404, tk.CodeNotFound, false},
		{"patch entry on a bill", "PATCH", "/v1/time-entries/te-4", "alice", `{"minutes":5}`, ifm(`"3"`), 409, tk.CodeLockedToBill, false},
		{"tampered cursor", "GET", "/v1/time-entries?cursor=AAAA.BBBB", "alice", "", nil, 400, api.CodeBadCursor, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			rec := do(r.h, tc.method, tc.path, tc.user, tc.body, tc.hdr)
			if tc.code == "" {
				if rec.Code != tc.status {
					t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
				}
				return
			}
			checkProblem(t, rec, tc.status, tc.code)
			if tc.zeroCalls && r.entries.calls != 0 {
				t.Errorf("service calls = %d, want 0", r.entries.calls)
			}
		})
	}
}

func TestTimeEntryPatchBumpsVersion(t *testing.T) {
	r := newRig(t)
	rec := do(r.h, "PATCH", "/v1/time-entries/te-1", "alice", `{"minutes":5}`, map[string]string{"If-Match": `"1"`})
	if rec.Code != http.StatusOK || rec.Header().Get("ETag") != `"2"` {
		t.Fatalf("patch = %d %s, ETag %q", rec.Code, rec.Body, rec.Header().Get("ETag"))
	}
}

func TestARC_I_008_TimeEntryIdempotency(t *testing.T) {
	r := newRig(t)
	hdr := map[string]string{idem.HeaderName: "key-00000042"}
	first := do(r.h, "POST", "/v1/time-entries", "alice", recordJSON, hdr)
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	tests := []struct {
		name   string
		body   string
		status int
		code   string
	}{
		{"same key, same body replays", recordJSON, http.StatusCreated, ""},
		// rule: ARC-I-008
		// The rule sets 422 for a reused key with a different body (as kernel/idem does).
		{"same key, different body", `{"matter_id":"` + matterA + `","work_date":"2026-10-02","minutes":60,"narrative":"Draft"}`, http.StatusUnprocessableEntity, idem.CodeKeyReused},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(r.h, "POST", "/v1/time-entries", "alice", tc.body, hdr)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code == "" && (rec.Body.String() != first.Body.String() || rec.Header().Get(idem.ReplayedHeader) != "true") {
				t.Errorf("replay = %s, want %s", rec.Body, first.Body)
			}
		})
	}
	all, _ := r.entries.Fake.List(context.Background(), tk.EntryFilter{UserID: "alice"})
	if len(all) != 4 {
		t.Errorf("alice entries = %d, want 4 (one recorded)", len(all))
	}
}

func TestTimeEntryListOwnOrAll(t *testing.T) {
	tests := []struct {
		name  string
		user  string
		query string
		pages []int
	}{
		{"own entries only", "alice", "", []int{3}},
		{"view_all sees everyone", "manager", "", []int{4}},
		{"pages of two", "manager", "?limit=2", []int{2, 2}},
		{"limit above maximum is capped", "manager", "?limit=5000", []int{4}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			path := "/v1/time-entries" + tc.query
			for i, want := range tc.pages {
				rec := do(r.h, "GET", path, tc.user, "", nil)
				validatePage(t, rec.Body.Bytes())
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
				for _, it := range page.Items {
					if tc.user == "alice" && it["user_id"] != "alice" {
						t.Errorf("alice sees %v", it)
					}
				}
				if last := i == len(tc.pages)-1; (page.Next != "") == last {
					t.Fatalf("page %d next = %q", i, page.Next)
				}
				path = "/v1/time-entries?cursor=" + page.Next
			}
		})
	}
}

func TestARC_I_012_TimeEntryWritesDeclareNoGateState(t *testing.T) {
	writes, err := server.DeclaredWrites(newRig(t).h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 2 {
		t.Fatalf("writes = %+v, want POST and PATCH", writes)
	}
}
