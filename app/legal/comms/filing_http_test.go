package comms

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
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

type filingsStub struct{ calls *int }

func (f filingsStub) IssueClaim(context.Context, string, string, time.Time) (Filing, error) {
	if f.calls != nil {
		*f.calls++
	}
	return Filing{}, nil
}
func (f filingsStub) RecordAffidavitOfService(context.Context, Filing) (Filing, error) {
	if f.calls != nil {
		*f.calls++
	}
	return Filing{}, nil
}
func (filingsStub) MatterOfFiling(context.Context, string) (string, error) { return "m1", nil }

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

// matterAccess answers per user on a matter subject: "walled" is behind an ethical wall,
// "nocap" lacks the capability on the matter; an empty subject is always allowed.
type matterAccess struct{}

func (matterAccess) Can(_ context.Context, p registry.Principal, _ string, s registry.Subject) (bool, error) {
	if s == (registry.Subject{}) {
		return true, nil
	}
	switch p.UserID {
	case "walled":
		return false, identity.ErrNotVisible
	case "nocap":
		return false, nil
	}
	return true, nil
}

// rule: ARC-I-042
// rule: ARC-I-051
func TestFilingRoutesMatterAccess(t *testing.T) {
	routes := []struct {
		path, body string
		okStatus   int
	}{
		{path: "/filings/f1/issue", body: `{"court_file_no":"CV-26-1","issued_on":"2026-10-09"}`, okStatus: 200},
		{path: "/filings/affidavits-of-service", body: `{"matter_id":"m1","document_id":"d1","court":"ONSC","filed_on":"2026-10-09"}`, okStatus: 201},
		{path: "/emails/c1/filings", body: `{"matter_id":"m1"}`, okStatus: 201},
	}
	users := []struct {
		name, user string
		status     int
		calls      int
	}{
		{name: "walled matter is not found", user: "walled", status: 404},
		{name: "no capability on the matter", user: "nocap", status: 403},
		{name: "allowed user", user: "lawyer", calls: 1},
	}
	for _, rt := range routes {
		for _, u := range users {
			t.Run(rt.path+" "+u.name, func(t *testing.T) {
				calls := 0
				filer := &filerStub{}
				h := &FilingHTTP{Filings: filingsStub{calls: &calls}, Emails: filer, Access: matterAccess{},
					Idem: idemfake.New(), Problems: &server.Problems{},
					Principal: func(*http.Request) (registry.Principal, bool) {
						return registry.Principal{Kind: registry.PrincipalUser, UserID: u.user}, true
					}}
				r := chi.NewRouter()
				h.Routes(r)
				req := httptest.NewRequest(http.MethodPost, rt.path, strings.NewReader(rt.body))
				req.Header.Set(idem.HeaderName, "key-00000001")
				req = req.WithContext(clock.WithClock(req.Context(), clockfake.New(time.Date(2026, 10, 10, 13, 0, 0, 0, time.UTC), toronto(t))))
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				want := u.status
				if want == 0 {
					want = rt.okStatus
				}
				if got := calls + filer.calls; rec.Code != want || got != u.calls {
					t.Fatalf("status %d calls %d, want %d and %d: %s", rec.Code, got, want, u.calls, rec.Body)
				}
			})
		}
	}
}
