package tax

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

type memRegistrations struct {
	reg  Registrations
	sets int
}

func (m *memRegistrations) Get(context.Context) (Registrations, error) { return m.reg, nil }

func (m *memRegistrations) Set(_ context.Context, version int, provinces []string, _ registry.Principal) (Registrations, error) {
	if version != m.reg.Version {
		return Registrations{}, server.ErrPreconditionFailed
	}
	m.sets++
	m.reg = Registrations{Provinces: provinces, Version: version + 1}
	return m.reg, nil
}

// capAccess grants each user the capabilities listed for it.
type capAccess map[string][]string

func (a capAccess) Can(_ context.Context, p registry.Principal, c string, _ registry.Subject) (bool, error) {
	for _, have := range a[p.UserID] {
		if have == c {
			return true, nil
		}
	}
	return false, nil
}

func registrationsRig(t *testing.T) (http.Handler, *memRegistrations) {
	t.Helper()
	store := &memRegistrations{reg: Registrations{Provinces: []string{"QC"}, Version: 2}}
	h := &RegistrationsHTTP{Store: store, Problems: &server.Problems{},
		Access: capAccess{"lawyer": {CapRegistrationsView, CapRegistrationsEdit}, "clerk": {CapRegistrationsView}},
		Principal: func(r *http.Request) (registry.Principal, bool) {
			u := r.Header.Get("X-User")
			return registry.Principal{Kind: registry.PrincipalUser, UserID: u}, u != ""
		}}
	m := server.New(nil)
	if err := m.Mount(h.Routes); err != nil {
		t.Fatal(err)
	}
	return m.Handler(), store
}

// rule: BIL-T-020
// rule: ARC-I-006
func TestBIL_T_020_FirmTaxRegistrationsRoutes(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		user    string
		ifMatch string
		body    string
		status  int
		code    string
		want    []string
	}{
		{name: "get", method: "GET", user: "clerk", status: 200, want: []string{"QC"}},
		{name: "get unauthenticated", method: "GET", status: 401, code: CodeUnauthenticated},
		{name: "set two provinces, sorted", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{"provinces":["SK","MB"]}`, status: 200, want: []string{"MB", "SK"}},
		{name: "clear", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{"provinces":[]}`, status: 200, want: []string{}},
		{name: "clerk cannot set", method: "PATCH", user: "clerk", ifMatch: `"2"`, body: `{"provinces":["MB"]}`, status: 403, code: CodeMissingCapability},
		{name: "missing If-Match", method: "PATCH", user: "lawyer", body: `{"provinces":["MB"]}`, status: 428, code: server.CodePreconditionRequired},
		{name: "stale If-Match", method: "PATCH", user: "lawyer", ifMatch: `"1"`, body: `{"provinces":["MB"]}`, status: 412, code: server.CodeVersionConflict},
		{name: "unknown code", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{"provinces":["ZZ"]}`, status: 400, code: server.CodeValidationFailed},
		{name: "export is not a registration", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{"provinces":["EX"]}`, status: 400, code: server.CodeValidationFailed},
		{name: "lower case", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{"provinces":["mb"]}`, status: 400, code: server.CodeValidationFailed},
		{name: "duplicate", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{"provinces":["MB","MB"]}`, status: 400, code: server.CodeValidationFailed},
		{name: "missing provinces", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{}`, status: 400, code: server.CodeValidationFailed},
		{name: "unknown field", method: "PATCH", user: "lawyer", ifMatch: `"2"`, body: `{"provinces":[],"x":1}`, status: 400, code: server.CodeValidationFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, store := registrationsRig(t)
			req := httptest.NewRequest(tc.method, server.BasePath+"/firm/tax-registrations", strings.NewReader(tc.body))
			if tc.user != "" {
				req.Header.Set("X-User", tc.user)
			}
			if tc.ifMatch != "" {
				req.Header.Set("If-Match", tc.ifMatch)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}
			if tc.code != "" {
				var p struct{ Code string }
				if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.Code != tc.code {
					t.Errorf("code = %q (%v), want %q", p.Code, err, tc.code)
				}
				if store.sets != 0 {
					t.Errorf("store sets = %d, want 0", store.sets)
				}
				return
			}
			var got registrationsView
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Provinces, tc.want) {
				t.Errorf("provinces = %q, want %q", got.Provinces, tc.want)
			}
			if etag := rec.Header().Get("ETag"); etag != server.ETag(int64(got.Version)) {
				t.Errorf("ETag = %q, version %d", etag, got.Version)
			}
		})
	}
}

func TestFirmTaxRegistrationsNotConfigured(t *testing.T) {
	m := server.New(nil)
	if err := m.Mount((&RegistrationsHTTP{}).Routes); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest("GET", server.BasePath+"/firm/tax-registrations", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}
