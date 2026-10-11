package party_test

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

	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const idHidden = "0190a000-0000-7000-8000-0000000000ff"

// stubContacts is an in-memory party.Contacts with uuid ids that counts calls. Hidden
// contacts stand for rows the caller's RLS session cannot read.
type stubContacts struct {
	mu       sync.Mutex
	contacts []party.Contact
	hidden   map[party.ContactID]bool
	dupName  string
	calls    int
}

func (s *stubContacts) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *stubContacts) Create(_ context.Context, c party.NewContact) (party.Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if c.Kind != party.Person && c.Kind != party.Organization {
		return party.Contact{}, party.ErrInvalid
	}
	if c.LegalName == s.dupName {
		return party.Contact{}, fmt.Errorf("create: %w", party.ErrDuplicate)
	}
	out := party.Contact{ID: party.ContactID(fmt.Sprintf("0190a000-0000-7000-8000-%012d", len(s.contacts)+1)),
		Kind: c.Kind, LegalName: c.LegalName, DisplayName: c.DisplayName, PreferredLanguage: "en",
		IsClient: c.IsClient, State: party.Proposed, CreatedAt: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), RowVersion: 1}
	s.contacts = append(s.contacts, out)
	return out, nil
}

func (s *stubContacts) Get(_ context.Context, id party.ContactID) (party.Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	for _, c := range s.contacts {
		if c.ID == id && !s.hidden[id] {
			return c, nil
		}
	}
	return party.Contact{}, fmt.Errorf("get: %w", party.ErrNotFound)
}

func (s *stubContacts) Search(context.Context, string) ([]party.Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return nil, nil
}

func (s *stubContacts) List(_ context.Context, f party.Filter) ([]party.Contact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	out := []party.Contact{}
	for _, c := range s.contacts {
		if s.hidden[c.ID] || (f.After != "" && c.ID <= f.After) {
			continue
		}
		if len(out) == f.PageSize() {
			break
		}
		out = append(out, c)
	}
	return out, nil
}

type stubAccess struct{ caps map[string][]string }

func (a stubAccess) Can(_ context.Context, p registry.Principal, capability string, s registry.Subject) (bool, error) {
	if s.ID == idHidden {
		return false, identity.ErrNotVisible
	}
	for _, c := range a.caps[p.UserID] {
		if c == capability {
			return true, nil
		}
	}
	return false, nil
}

func newRig(t *testing.T, n int) (*stubContacts, api.CursorCodec, http.Handler) {
	t.Helper()
	st := &stubContacts{hidden: map[party.ContactID]bool{idHidden: true}, dupName: "Dup Co"}
	for i := 0; i < n; i++ {
		if _, err := st.Create(context.Background(), party.NewContact{Kind: party.Person, LegalName: "L", DisplayName: "D"}); err != nil {
			t.Fatal(err)
		}
	}
	st.contacts = append(st.contacts, party.Contact{ID: idHidden, Kind: party.Person, LegalName: "H", DisplayName: "H", State: party.Active, RowVersion: 1})
	st.calls = 0
	codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	h := &party.HTTP{
		Contacts: st,
		Access: stubAccess{caps: map[string][]string{
			"viewer": {party.CapView},
			"editor": {party.CapView, party.CapEdit},
		}},
		Cursors:  codec,
		Problems: &server.Problems{},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-Test-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		},
	}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return st, codec, m.Handler()
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

func do(h http.Handler, method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	if user != "" {
		req.Header.Set("X-Test-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const personJSON = `{"kind":"person","legal_name":"Ada Lovelace","display_name":"Ada"}`

func TestARC_I_051_ContactRoutesProblems(t *testing.T) {
	first := "0190a000-0000-7000-8000-000000000001"
	tests := []struct {
		name      string
		method    string
		path      string
		user      string
		body      string
		status    int
		code      string
		zeroCalls bool
	}{
		{"list without principal", "GET", "/v1/contacts", "", "", 401, party.CodeUnauthenticated, true},
		{"post without principal", "POST", "/v1/contacts", "", personJSON, 401, party.CodeUnauthenticated, true},
		{"get without principal", "GET", "/v1/contacts/" + first, "", "", 401, party.CodeUnauthenticated, true},
		{"list without contacts.view", "GET", "/v1/contacts", "nobody", "", 403, party.CodeMissingCapability, true},
		{"post without contacts.edit", "POST", "/v1/contacts", "viewer", personJSON, 403, party.CodeMissingCapability, true},
		{"get without contacts.view", "GET", "/v1/contacts/" + first, "nobody", "", 403, party.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"get hidden contact is 404, not 403", "GET", "/v1/contacts/" + idHidden, "nobody", "", 404, party.CodeNotFound, true},
		{"get hidden contact with capability", "GET", "/v1/contacts/" + idHidden, "viewer", "", 404, party.CodeNotFound, true},
		{"get unknown contact", "GET", "/v1/contacts/0190a000-0000-7000-8000-0000000000ee", "viewer", "", 404, party.CodeNotFound, false},
		{"get malformed id", "GET", "/v1/contacts/nope", "viewer", "", 400, server.CodeValidationFailed, true},
		{"post unknown body field", "POST", "/v1/contacts", "editor", `{"kind":"person","legal_name":"A","display_name":"A","colour":"red"}`, 400, server.CodeValidationFailed, true},
		{"post missing name", "POST", "/v1/contacts", "editor", `{"kind":"person"}`, 400, server.CodeValidationFailed, true},
		{"post unknown kind", "POST", "/v1/contacts", "editor", `{"kind":"robot","legal_name":"A","display_name":"A"}`, 422, server.CodeValidationFailed, false},
		{"post duplicate", "POST", "/v1/contacts", "editor", `{"kind":"organization","legal_name":"Dup Co","display_name":"Dup"}`, 409, party.CodeDuplicateContact, false},
		{"tampered cursor", "GET", "/v1/contacts?cursor=AAAA.BBBB", "viewer", "", 400, api.CodeBadCursor, true},
		{"non-integer limit", "GET", "/v1/contacts?limit=ten", "viewer", "", 400, server.CodeValidationFailed, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, _, h := newRig(t, 2)
			checkProblem(t, do(h, tc.method, tc.path, tc.user, tc.body), tc.status, tc.code)
			if tc.zeroCalls && st.Calls() != 0 {
				t.Errorf("service calls = %d, want 0", st.Calls())
			}
		})
	}
}

func TestContactRoutesUnconfiguredFailClosed(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&party.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/contacts", "viewer", ""), 503, party.CodeUnavailable)
}

func TestContactCreateAndGet(t *testing.T) {
	_, _, h := newRig(t, 0)
	rec := do(h, "POST", "/v1/contacts", "editor", personJSON)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	validate(t, "#/components/schemas/Id", []byte(fmt.Sprintf("%q", created["id"])))
	if created["state"] != string(party.Proposed) || rec.Header().Get("ETag") != `"1"` {
		t.Errorf("created = %v, ETag %q", created, rec.Header().Get("ETag"))
	}
	got := do(h, "GET", "/v1/contacts/"+created["id"].(string), "viewer", "")
	if got.Code != http.StatusOK || got.Body.String() != rec.Body.String() {
		t.Errorf("get = %d %s, want %s", got.Code, got.Body, rec.Body)
	}
}

func TestContactListPages(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		query     string
		wantItems []int
		wantNext  bool
	}{
		{"empty", 0, "", []int{0}, false},
		{"one page", 3, "", []int{3}, false},
		{"pages of two", 5, "?limit=2", []int{2, 2, 1}, false},
		{"limit above maximum is capped", 3, "?limit=5000", []int{3}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, h := newRig(t, tc.n)
			path := "/v1/contacts" + tc.query
			seen := map[string]bool{}
			for i, want := range tc.wantItems {
				rec := do(h, "GET", path, "viewer", "")
				if rec.Code != http.StatusOK {
					t.Fatalf("page %d status = %d (%s)", i, rec.Code, rec.Body)
				}
				var page struct {
					Items []map[string]any `json:"items"`
					Next  string           `json:"next"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				if len(page.Items) != want {
					t.Fatalf("page %d items = %d, want %d", i, len(page.Items), want)
				}
				for _, it := range page.Items {
					id := it["id"].(string)
					if id == idHidden || seen[id] {
						t.Errorf("page %d repeats or leaks %s", i, id)
					}
					seen[id] = true
				}
				last := i == len(tc.wantItems)-1
				if (page.Next != "") == last {
					t.Fatalf("page %d next = %q, last = %v", i, page.Next, last)
				}
				path = "/v1/contacts?cursor=" + page.Next
			}
		})
	}
}

func TestContactListLimitCapped(t *testing.T) {
	_, codec, _ := newRig(t, 0)
	if got := codec.(*api.Codec).Clamp(5000); got != 200 {
		t.Errorf("Clamp(5000) = %d, want the openapi maximum 200", got)
	}
	if got := (party.Filter{Limit: 5000}).PageSize(); got != party.ListLimit {
		t.Errorf("PageSize = %d, want %d", got, party.ListLimit)
	}
}

func TestARC_I_012_ContactPostDeclaresNoGateState(t *testing.T) {
	_, _, h := newRig(t, 0)
	writes, err := server.DeclaredWrites(h)
	if err != nil {
		t.Fatal(err)
	}
	if len(writes) != 1 || writes[0].Method != http.MethodPost || len(writes[0].Sets) != 1 || writes[0].Sets[0] != "contact.created" {
		t.Errorf("writes = %+v, want POST /contacts declaring contact.created", writes)
	}
}
