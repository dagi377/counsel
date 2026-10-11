package docs_test

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

	"github.com/dagi377/counsel/app/legal/docs"
	"github.com/dagi377/counsel/kernel/api"
	kdocs "github.com/dagi377/counsel/kernel/docs"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	matterA     = "0190a000-0000-7000-8000-0000000000a1"
	matterWall  = "0190a000-0000-7000-8000-0000000000af"
	folderA     = "0190c000-0000-7000-8000-0000000000f1"
	docA        = "0190d000-0000-7000-8000-000000000001"
	docB        = "0190d000-0000-7000-8000-000000000002"
	docHidden   = "0190d000-0000-7000-8000-0000000000ff"
	versionA    = "0190e000-0000-7000-8000-0000000000a1"
	versionB    = "0190e000-0000-7000-8000-0000000000b1"
	key1        = "0190e000-0000-7000-8000-000000000301"
	problemRef  = "#/components/schemas/Problem"
	sampleBytes = "aGVsbG8=" // "hello"
)

var users = map[string][]string{
	"editor": {docs.CapEdit, docs.CapView},
	"viewer": {docs.CapView},
	"reader": {"contacts.view"},
}

type testAccess struct{}

func (testAccess) Can(_ context.Context, p registry.Principal, c string, s registry.Subject) (bool, error) {
	if s.ID == docHidden || s.ID == matterWall {
		return false, identity.ErrNotVisible
	}
	return slices.Contains(users[p.UserID], c), nil
}

func testPrincipal(r *http.Request) (registry.Principal, bool) {
	u := r.Header.Get("X-Test-User")
	return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
}

// fakeDocs is an in-memory DocumentStore and Pages that counts every call.
type fakeDocs struct {
	docs     []docs.Document
	versions map[string]kdocs.VersionID
	pages    map[kdocs.VersionID][][]string
	uploads  []docs.Upload
	filters  []docs.ListFilter
	calls    int
	nextID   int
}

func (f *fakeDocs) List(_ context.Context, fl docs.ListFilter) ([]docs.Document, error) {
	f.calls++
	f.filters = append(f.filters, fl)
	var out []docs.Document
	for _, d := range f.docs {
		if d.ID > fl.After && (fl.MatterID == "" || d.MatterID == fl.MatterID) && len(out) < fl.Limit {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *fakeDocs) Document(_ context.Context, id string) (docs.Document, error) {
	f.calls++
	for _, d := range f.docs {
		if d.ID == id {
			return d, nil
		}
	}
	return docs.Document{}, docs.ErrNotFound
}

func (f *fakeDocs) Upload(_ context.Context, u docs.Upload) (docs.Document, kdocs.VersionID, error) {
	f.calls++
	f.uploads = append(f.uploads, u)
	f.nextID++
	d := docs.Document{ID: fmt.Sprintf("0190d000-0000-7000-8000-%012d", 100+f.nextID), MatterID: u.MatterID, Kind: u.Kind, FolderID: u.FolderID}
	f.docs = append(f.docs, d)
	return d, kdocs.VersionID(fmt.Sprintf("0190e000-0000-7000-8000-%012d", 100+f.nextID)), nil
}

func (f *fakeDocs) HasVersion(_ context.Context, id string, v kdocs.VersionID) (bool, error) {
	f.calls++
	return f.versions[id] == v, nil
}

func (f *fakeDocs) Lines(_ context.Context, v kdocs.VersionID, page int) ([]kdocs.Line, error) {
	f.calls++
	ps, ok := f.pages[v]
	if !ok {
		return nil, kdocs.ErrNotFound
	}
	if page < 1 || page > len(ps) {
		return nil, kdocs.ErrBadRange
	}
	var out []kdocs.Line
	for i, t := range ps[page-1] {
		out = append(out, kdocs.Line{Page: page, No: i + 1, Text: t})
	}
	return out, nil
}

type rig struct {
	h   http.Handler
	svc *fakeDocs
}

func newRig(t *testing.T) rig {
	t.Helper()
	codec, err := api.NewCodec(bytes.Repeat([]byte("k"), api.MinCursorKeyLen))
	if err != nil {
		t.Fatal(err)
	}
	svc := &fakeDocs{
		docs: []docs.Document{
			{ID: docA, MatterID: matterA, Kind: docs.KindResearchMemo, FolderID: folderA},
			{ID: docB, MatterID: matterA, Kind: docs.KindPrecedent, FolderID: folderA},
		},
		versions: map[string]kdocs.VersionID{docA: versionA, docB: versionB},
		pages:    map[kdocs.VersionID][][]string{versionA: {{"first line", "second line"}}},
	}
	h := &docs.HTTP{Documents: svc, Pages: svc, Access: testAccess{}, Cursors: codec, Idem: idemfake.New(),
		Problems: &server.Problems{}, Principal: testPrincipal}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return rig{h: m.Handler(), svc: svc}
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
	validate(t, problemRef, rec.Body.Bytes())
	var p api.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.Code != code || p.Status != status {
		t.Errorf("problem = %+v, want %d %s", p, status, code)
	}
}

func uploadBody(kind, extra string) string {
	return `{"matter_id":"` + matterA + `","folder_id":"` + folderA + `","kind":"` + kind + `","content":"` + sampleBytes + `"` + extra + `}`
}

var withKey = map[string]string{idem.HeaderName: key1}

func pagePath(doc, version, page string) string {
	return "/v1/documents/" + doc + "/versions/" + version + "/pages/" + page
}

func TestARC_I_051_DocumentProblems(t *testing.T) {
	memo := docs.KindResearchMemo
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
		{"list without principal", "GET", "/v1/documents", "", "", nil, 401, docs.CodeUnauthenticated, true},
		{"upload without principal", "POST", "/v1/documents", "", uploadBody(memo, ""), withKey, 401, docs.CodeUnauthenticated, true},
		{"page without principal", "GET", pagePath(docA, versionA, "1"), "", "", nil, 401, docs.CodeUnauthenticated, true},
		// rule: ARC-I-051
		{"list without documents.view", "GET", "/v1/documents", "reader", "", nil, 403, docs.CodeMissingCapability, true},
		{"upload without documents.edit", "POST", "/v1/documents", "viewer", uploadBody(memo, ""), withKey, 403, docs.CodeMissingCapability, true},
		{"page without documents.view", "GET", pagePath(docA, versionA, "1"), "reader", "", nil, 403, docs.CodeMissingCapability, true},
		// rule: ARC-I-042
		{"page of walled document is 404", "GET", pagePath(docHidden, versionA, "1"), "viewer", "", nil, 404, docs.CodeNotFound, true},
		{"list of walled matter is 404", "GET", "/v1/documents?matter_id=" + matterWall, "viewer", "", nil, 404, docs.CodeNotFound, true},
		{"upload to walled matter is 404", "POST", "/v1/documents", "editor",
			strings.Replace(uploadBody(memo, ""), matterA, matterWall, 1), withKey, 404, docs.CodeNotFound, true},
		{"page out of range", "GET", pagePath(docA, versionA, "2"), "viewer", "", nil, 404, docs.CodeNotFound, false},
		{"page of another document's version", "GET", pagePath(docA, versionB, "1"), "viewer", "", nil, 404, docs.CodeNotFound, false},
		{"page zero", "GET", pagePath(docA, versionA, "0"), "viewer", "", nil, 400, server.CodeValidationFailed, true},
		{"page malformed document id", "GET", pagePath("nope", versionA, "1"), "viewer", "", nil, 400, server.CodeValidationFailed, true},
		{"list tampered cursor", "GET", "/v1/documents?cursor=abc.def", "viewer", "", nil, 400, api.CodeBadCursor, true},
		{"list malformed matter_id", "GET", "/v1/documents?matter_id=nope", "viewer", "", nil, 400, server.CodeValidationFailed, true},
		// rule: ARC-I-007
		{"upload missing Idempotency-Key", "POST", "/v1/documents", "editor", uploadBody(memo, ""), nil, 400, idem.CodeKeyRequired, true},
		{"upload unknown field", "POST", "/v1/documents", "editor", uploadBody(memo, `,"text":"x"`), withKey, 400, server.CodeValidationFailed, true},
		{"upload empty content", "POST", "/v1/documents", "editor",
			strings.Replace(uploadBody(memo, ""), sampleBytes, "", 1), withKey, 400, server.CodeValidationFailed, true},
		{"upload unknown kind", "POST", "/v1/documents", "editor", uploadBody("tax_return", ""), withKey, 422, docs.CodeUnknownKind, true},
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

func TestDocumentsUnconfiguredAnswer503(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&docs.HTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	checkProblem(t, do(m.Handler(), "GET", "/v1/documents", "viewer", "", nil), 503, docs.CodeUnavailable)
}

func TestDocumentList(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantIDs   []string
		wantNext  bool
		wantLimit int
	}{
		{"all", "", []string{docA, docB}, false, api.DefaultLimit + 1},
		{"page of one", "?limit=1", []string{docA}, true, 2},
		// The limit above the openapi maximum is capped.
		{"limit capped", "?limit=100000", []string{docA, docB}, false, api.MaxLimit + 1},
		{"by matter", "?matter_id=" + matterA, []string{docA, docB}, false, api.DefaultLimit + 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			rec := do(r.h, "GET", "/v1/documents"+tc.query, "viewer", "", nil)
			if rec.Code != 200 {
				t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
			}
			var page struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
				NextCursor *string `json:"next_cursor"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, d := range page.Data {
				ids = append(ids, d.ID)
			}
			if !slices.Equal(ids, tc.wantIDs) || (page.NextCursor != nil) != tc.wantNext {
				t.Errorf("ids = %v next = %v, want %v next %v", ids, page.NextCursor, tc.wantIDs, tc.wantNext)
			}
			if got := r.svc.filters[0].Limit; got != tc.wantLimit {
				t.Errorf("service limit = %d, want %d", got, tc.wantLimit)
			}
			if tc.wantNext {
				rec = do(r.h, "GET", "/v1/documents?cursor="+*page.NextCursor, "viewer", "", nil)
				if rec.Code != 200 || !strings.Contains(rec.Body.String(), docB) {
					t.Errorf("next page = %d %s, want docB", rec.Code, rec.Body)
				}
			}
		})
	}
}

func TestDocumentPageText(t *testing.T) {
	r := newRig(t)
	rec := do(r.h, "GET", pagePath(docA, versionA, "1"), "viewer", "", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	var got struct {
		Page  int `json:"page"`
		Lines []struct {
			No   int    `json:"no"`
			Text string `json:"text"`
		} `json:"lines"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Page != 1 || len(got.Lines) != 2 || got.Lines[0].No != 1 || got.Lines[1].No != 2 || got.Lines[1].Text != "second line" {
		t.Errorf("page = %+v, want two numbered lines", got)
	}
}

// rule: ARC-I-007
func TestARC_I_007_DocumentUploadIdempotency(t *testing.T) {
	tests := []struct {
		name        string
		second      string
		wantStatus  int
		wantUploads int
	}{
		{"same key same body replays", uploadBody(docs.KindResearchMemo, ""), 201, 1},
		{"same key different body refused", uploadBody(docs.KindPrecedent, ""), 422, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			first := do(r.h, "POST", "/v1/documents", "editor", uploadBody(docs.KindResearchMemo, ""), withKey)
			if first.Code != 201 {
				t.Fatalf("first = %d (%s)", first.Code, first.Body)
			}
			second := do(r.h, "POST", "/v1/documents", "editor", tc.second, withKey)
			if second.Code != tc.wantStatus {
				t.Fatalf("second = %d, want %d (%s)", second.Code, tc.wantStatus, second.Body)
			}
			if tc.wantStatus == 201 && second.Body.String() != first.Body.String() {
				t.Errorf("replay body = %s, want %s", second.Body, first.Body)
			}
			if len(r.svc.uploads) != tc.wantUploads {
				t.Errorf("uploads = %d, want %d", len(r.svc.uploads), tc.wantUploads)
			}
		})
	}
}

func TestDocumentUploadRecordsUpload(t *testing.T) {
	r := newRig(t)
	rec := do(r.h, "POST", "/v1/documents", "editor", uploadBody(docs.KindResearchMemo, `,"filename":"memo.txt"`), withKey)
	if rec.Code != 201 {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body)
	}
	u := r.svc.uploads[0]
	if u.MatterID != matterA || string(u.FolderID) != folderA || string(u.Content) != "hello" || u.By != "editor" || u.IdempotencyKey != key1 {
		t.Errorf("upload = %+v", u)
	}
	var got struct {
		ID        string `json:"id"`
		VersionID string `json:"version_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.ID == "" || got.VersionID == "" {
		t.Errorf("body = %s, want id and version_id", rec.Body)
	}
}
