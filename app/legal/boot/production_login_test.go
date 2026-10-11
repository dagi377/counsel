package boot

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/identity/login"
	"github.com/dagi377/counsel/kernel/server"
)

// TestARC_I_036_ProductionLogin checks the binaries' sign-in wiring: the sign-in routes are
// mounted outside the versioned API, the dev login is off unless asked for, and a dev login
// outside a dev environment fails boot.
func TestARC_I_036_ProductionLogin(t *testing.T) {
	tests := []struct {
		name      string
		cfg       *Login
		wantErr   error
		wantSvc   bool
		devStatus int // POST /auth/dev-login, when a service is built
	}{
		{"no config: no sign-in", nil, nil, false, 0},
		{"empty config: no sign-in", &Login{}, nil, false, 0},
		{"dev login refused in production", &Login{DevLogin: true, DevUserEmail: "dev@example.test"}, login.ErrDevLoginInProduction, false, 0},
		// The pool never connects, so a mounted dev login fails at the session insert: 500, not 404.
		{"dev login on in dev", &Login{Dev: true, DevLogin: true, DevUserEmail: "dev@example.test"}, nil, true, http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux := server.New(nil)
			k := &kboot.Kernel{DB: noConnPool{dbfake.New()}, Server: mux}
			svc, err := Production{Login: tc.cfg}.login(k, &server.Problems{})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("login err = %v, want %v", err, tc.wantErr)
			}
			if (svc != nil) != tc.wantSvc {
				t.Fatalf("service built = %v, want %v", svc != nil, tc.wantSvc)
			}
			if !tc.wantSvc {
				return
			}
			// rule: ARC-I-017
			// The step-up source the gate decision routes need: zero without a session.
			if mfa := svc.StepUp(httptest.NewRequest(http.MethodPost, "/v1/gates/g/decisions", nil)); !mfa.IsZero() {
				t.Errorf("StepUp without a session = %v, want zero", mfa)
			}
			rec := httptest.NewRecorder()
			mux.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, login.PathDevLogin, nil))
			if rec.Code != tc.devStatus {
				t.Errorf("dev login = %d, want %d", rec.Code, tc.devStatus)
			}
			if len(mux.Routes()) != 0 {
				t.Errorf("sign-in routes leaked into the versioned API: %v", mux.Routes())
			}
			rec = httptest.NewRecorder()
			mux.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/matters", nil))
			if rec.Code != http.StatusNotFound {
				t.Errorf("unmounted api route = %d", rec.Code)
			}
		})
	}
}
