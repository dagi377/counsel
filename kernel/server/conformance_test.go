package server_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/inbound"
	"github.com/dagi377/counsel/kernel/server"
)

const (
	openAPIPath  = "../../api/openapi.yaml"
	unservedPath = "../../api/unserved.txt"
)

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }

func TestMuxMount(t *testing.T) {
	tests := []struct {
		name    string
		mounts  []server.Mounter
		want    []server.Route
		wantErr error
	}{
		{
			name:   "routes listed under the base path, sorted",
			mounts: []server.Mounter{func(r chi.Router) { r.Post("/b", ok); r.Get("/a/{id}", ok); r.Get("/b", ok) }},
			want: []server.Route{
				{Method: "GET", Pattern: "/v1/a/{id}"},
				{Method: "GET", Pattern: "/v1/b"},
				{Method: "POST", Pattern: "/v1/b"},
			},
		},
		{
			name: "two modules, distinct routes",
			mounts: []server.Mounter{
				func(r chi.Router) { r.Get("/a", ok) },
				func(r chi.Router) { r.Post("/a", ok) },
			},
			want: []server.Route{{Method: "GET", Pattern: "/v1/a"}, {Method: "POST", Pattern: "/v1/a"}},
		},
		{
			name: "duplicate method and pattern is refused and leaves the mux unchanged",
			mounts: []server.Mounter{
				func(r chi.Router) { r.Get("/a", ok) },
				func(r chi.Router) { r.Get("/a", ok); r.Get("/c", ok) },
			},
			want:    []server.Route{{Method: "GET", Pattern: "/v1/a"}},
			wantErr: server.ErrDuplicateRoute,
		},
		{
			name:   "nil mounter is a no-op",
			mounts: []server.Mounter{nil},
			want:   []server.Route{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := server.New(nil)
			var err error
			for _, mt := range tc.mounts {
				if e := m.Mount(mt); e != nil {
					err = e
				}
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Mount error = %v, want %v", err, tc.wantErr)
			}
			if got := m.Routes(); !slices.Equal(got, tc.want) {
				t.Errorf("Routes() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMuxHandler(t *testing.T) {
	m := server.New(&server.Problems{})
	if err := m.Mount(func(r chi.Router) {
		r.Get("/ping", ok)
		r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("client detail") })
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, method, path string
		status             int
		problem            bool
	}{
		{"mounted route served under the base path", "GET", "/v1/ping", 200, false},
		{"route outside the base path is not served", "GET", "/ping", 404, true},
		{"unknown path is a problem", "GET", "/v1/nope", 404, true},
		{"wrong method is a problem", "POST", "/v1/ping", 405, true},
		{"panic is a 500 problem without the panic value", "GET", "/v1/boom", 500, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			m.Handler().ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if !tc.problem {
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != api.ProblemContentType {
				t.Errorf("Content-Type = %q, want %q", ct, api.ProblemContentType)
			}
			var p api.Problem
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Status != tc.status {
				t.Errorf("body = %s, want problem with status %d", rec.Body, tc.status)
			}
			if strings.Contains(rec.Body.String(), "client detail") {
				t.Errorf("problem body leaks the panic value: %s", rec.Body)
			}
		})
	}
}

func TestConformance(t *testing.T) {
	r := func(m, p string) server.Route { return server.Route{Method: m, Pattern: p} }
	doc := []server.Route{r("GET", "/v1/a"), r("POST", "/v1/a"), r("GET", "/v1/b/{id}")}
	tests := []struct {
		name             string
		served, unserved []server.Route
		wantN            int
		wantSubstr       string
	}{
		{"all served", doc, nil, 0, ""},
		{"missing one, listed unserved", doc[:2], doc[2:], 0, ""},
		{"served but not documented", append(slices.Clone(doc), r("GET", "/v1/extra")), nil, 1, "served but not in openapi: GET /v1/extra"},
		{"documented, not served, not listed", doc[:2], nil, 1, "in openapi but not served or listed unserved: GET /v1/b/{id}"},
		{"listed unserved but now served: the list only shrinks", doc, doc[2:], 1, "listed unserved but now served"},
		{"listed unserved but gone from openapi", doc, []server.Route{r("GET", "/v1/gone")}, 1, "listed unserved but not in openapi: GET /v1/gone"},
		{"method is part of the key", []server.Route{r("PUT", "/v1/a")}, doc, 1, "served but not in openapi: PUT /v1/a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := server.Conformance(tc.served, doc, tc.unserved)
			if len(got) != tc.wantN {
				t.Fatalf("Conformance = %q, want %d mismatches", got, tc.wantN)
			}
			if tc.wantSubstr != "" && !strings.Contains(got[0], tc.wantSubstr) {
				t.Errorf("Conformance = %q, want %q", got, tc.wantSubstr)
			}
		})
	}
}

func TestParseRouteList(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []server.Route
		wantErr bool
	}{
		{"comments and blanks skipped", "# head\n\nget /a\nPOST /b/{id}\n", []server.Route{{Method: "GET", Pattern: "/v1/a"}, {Method: "POST", Pattern: "/v1/b/{id}"}}, false},
		{"missing path", "GET\n", nil, true},
		{"relative path", "GET a\n", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := server.ParseRouteList([]byte(tc.in))
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestKernelRoutesConform mounts every kernel module's routes and checks them against
// api/openapi.yaml and the unserved list. App routes are checked with these in
// app/legal's conformance test, which runs the full two-way check.
func TestKernelRoutesConform(t *testing.T) {
	documented, unserved := loadContract(t)
	m := server.New(nil)
	for _, mt := range kernelMounters() {
		if err := m.Mount(mt); err != nil {
			t.Fatalf("Mount: %v", err)
		}
	}
	served := m.Routes()
	if len(served) == 0 {
		t.Fatal("no kernel routes mounted")
	}
	// Only the served-side checks apply: an operation an app serves is legitimately missing here.
	for _, msg := range server.Conformance(served, documented, unserved) {
		if strings.HasPrefix(msg, "served but not in openapi") || strings.HasPrefix(msg, "listed unserved but now served") {
			t.Error(msg)
		}
	}
}

func TestUnservedListIsValid(t *testing.T) {
	documented, unserved := loadContract(t)
	doc := map[server.Route]bool{}
	for _, rt := range documented {
		doc[rt] = true
	}
	seen := map[server.Route]bool{}
	for _, rt := range unserved {
		if !doc[rt] {
			t.Errorf("unserved.txt lists %s %s, which is not in openapi.yaml", rt.Method, rt.Pattern)
		}
		if seen[rt] {
			t.Errorf("unserved.txt lists %s %s twice", rt.Method, rt.Pattern)
		}
		seen[rt] = true
	}
}

// kernelMounters is every kernel module's route mounter. Zero-value receivers are enough
// to list routes; handlers are not called.
func kernelMounters() []server.Mounter {
	return []server.Mounter{
		inbound.NewPG(nil, nil).Routes,
	}
}

func loadContract(t *testing.T) (documented, unserved []server.Route) {
	t.Helper()
	spec, err := os.ReadFile(openAPIPath)
	if err != nil {
		t.Fatal(err)
	}
	if documented, err = server.OpenAPIRoutes(spec); err != nil {
		t.Fatal(err)
	}
	list, err := os.ReadFile(unservedPath)
	if err != nil {
		t.Fatal(err)
	}
	if unserved, err = server.ParseRouteList(list); err != nil {
		t.Fatal(err)
	}
	return documented, unserved
}
