package comms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

type filerStub struct{ calls int }

func (f *filerStub) FileEmail(context.Context, string, string, string) (bool, error) {
	f.calls++
	return true, nil
}

type filingsStub struct{}

func (filingsStub) IssueClaim(context.Context, string, string, time.Time) (Filing, error) {
	return Filing{}, nil
}
func (filingsStub) RecordAffidavitOfService(context.Context, Filing) (Filing, error) {
	return Filing{}, nil
}

// wallAccess allows every capability except on the walled matter m-walled.
type wallAccess struct{}

func (wallAccess) Can(_ context.Context, _ registry.Principal, _ string, s registry.Subject) (bool, error) {
	if s.ID == "m-walled" {
		return false, identity.ErrNotVisible
	}
	return true, nil
}

// rule: ARC-I-007
// rule: ARC-I-042
func TestFileEmailRoute(t *testing.T) {
	tests := []struct {
		name   string
		ready  bool
		key    string
		body   string
		status int
		calls  int
	}{
		{name: "files the email", ready: true, key: "key-00000001", body: `{"matter_id":"m1"}`, status: 201, calls: 1},
		{name: "needs an idempotency key", ready: true, body: `{"matter_id":"m1"}`, status: 400},
		{name: "walled matter is not found", ready: true, key: "key-00000001", body: `{"matter_id":"m-walled"}`, status: 404},
		{name: "unknown field", ready: true, key: "key-00000001", body: `{"matter_id":"m1","x":1}`, status: 400},
		{name: "not wired", key: "key-00000001", body: `{"matter_id":"m1"}`, status: 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filer := &filerStub{}
			h := &FilingHTTP{}
			if tt.ready {
				h = &FilingHTTP{Filings: filingsStub{}, Emails: filer, Access: wallAccess{}, Idem: idemfake.New(),
					Problems: &server.Problems{}, Principal: func(*http.Request) (registry.Principal, bool) {
						return registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}, true
					}}
			}
			r := chi.NewRouter()
			h.Routes(r)
			req := httptest.NewRequest(http.MethodPost, "/emails/c1/filings", strings.NewReader(tt.body))
			if tt.key != "" {
				req.Header.Set(idem.HeaderName, tt.key)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.status || filer.calls != tt.calls {
				t.Fatalf("status %d calls %d, want %d and %d: %s", rec.Code, filer.calls, tt.status, tt.calls, rec.Body)
			}
		})
	}
}
