package server_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/server"
)

func TestMuxMountRoot(t *testing.T) {
	ok := func(r chi.Router) {
		r.Get("/auth/login", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}
	tests := []struct {
		name    string
		mounts  []server.Mounter
		wantErr error
	}{
		{"root route served", []server.Mounter{ok}, nil},
		{"duplicate refused", []server.Mounter{ok, ok}, server.ErrDuplicateRoute},
		{"api path refused", []server.Mounter{func(r chi.Router) { r.Get("/v1/x", http.NotFound) }}, server.ErrDuplicateRoute},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := server.New(nil)
			var err error
			for _, mt := range tc.mounts {
				if err = m.MountRoot(mt); err != nil {
					break
				}
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("MountRoot err = %v, want %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if len(m.Routes()) != 0 {
				t.Errorf("root routes listed as API routes: %v", m.Routes())
			}
			rec := httptest.NewRecorder()
			m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
			if rec.Code != http.StatusTeapot {
				t.Errorf("GET /auth/login = %d", rec.Code)
			}
		})
	}
}
