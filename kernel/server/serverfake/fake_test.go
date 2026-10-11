package serverfake

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/server"
)

func ok(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func TestFakeMount(t *testing.T) {
	tests := []struct {
		name    string
		mounts  []server.Mounter
		want    []server.Route
		wantErr error
	}{
		{"none", nil, []server.Route{}, nil},
		{
			"two modules",
			[]server.Mounter{
				func(r chi.Router) { r.Get("/v1/b", ok) },
				func(r chi.Router) { r.Get("/v1/a", ok); r.Post("/v1/a", ok) },
			},
			[]server.Route{{Method: "GET", Pattern: "/v1/a"}, {Method: "POST", Pattern: "/v1/a"}, {Method: "GET", Pattern: "/v1/b"}},
			nil,
		},
		{
			"duplicate",
			[]server.Mounter{
				func(r chi.Router) { r.Get("/v1/a", ok) },
				func(r chi.Router) { r.Get("/v1/a", ok) },
			},
			[]server.Route{{Method: "GET", Pattern: "/v1/a"}},
			server.ErrDuplicateRoute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			var err error
			for _, m := range tt.mounts {
				if e := f.Mount(m); e != nil {
					err = e
				}
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			got := f.Routes()
			if len(got) != len(tt.want) {
				t.Fatalf("Routes() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("Routes() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestFakeServeAndProblem(t *testing.T) {
	f := New()
	if err := f.Mount(func(r chi.Router) {
		r.Get("/v1/x", ok)
		r.Get("/v1/missing", func(w http.ResponseWriter, r *http.Request) {
			f.WriteProblem(w, r, api.Problem{Title: "Not found", Status: 404, Code: "not_found"})
		})
	}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path   string
		status int
		ctype  string
	}{
		{"/v1/x", http.StatusNoContent, ""},
		{"/v1/missing", http.StatusNotFound, api.ProblemContentType},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			f.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != tt.status || rec.Header().Get("Content-Type") != tt.ctype {
				t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Content-Type"))
			}
			if tt.ctype != "" {
				var p api.Problem
				if err := json.NewDecoder(rec.Body).Decode(&p); err != nil || p.Code != "not_found" {
					t.Fatalf("problem body: %+v %v", p, err)
				}
			}
		})
	}
}
