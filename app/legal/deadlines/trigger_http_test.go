package deadlines

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

type bookStub struct{ calls int }

func (b *bookStub) Get(_ context.Context, id deadline.DeadlineID) (deadline.Deadline, error) {
	return deadline.Deadline{ID: id, Proposed: deadline.Proposed{Subject: registry.Subject{Kind: "matter", ID: "m1"}}}, nil
}

func (b *bookStub) ChangeTrigger(context.Context, pgx.Tx, deadline.DeadlineID, deadline.Date) error {
	b.calls++
	return nil
}

type txStub struct{}

func (txStub) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return fn(ctx, nil)
}

type accessStub bool

func (a accessStub) Can(context.Context, registry.Principal, string, registry.Subject) (bool, error) {
	return bool(a), nil
}

// rule: ARC-I-007
// rule: ARC-I-051
func TestTriggerRoute(t *testing.T) {
	tests := []struct {
		name    string
		ready   bool
		allowed bool
		key     string
		body    string
		status  int
		calls   int
	}{
		{name: "changes the trigger", ready: true, allowed: true, key: "key-00000001", body: `{"trigger_on":"2026-10-20"}`, status: 200, calls: 1},
		{name: "needs an idempotency key", ready: true, allowed: true, body: `{"trigger_on":"2026-10-20"}`, status: 400},
		{name: "needs the capability", ready: true, key: "key-00000001", body: `{"trigger_on":"2026-10-20"}`, status: 403},
		{name: "bad date", ready: true, allowed: true, key: "key-00000001", body: `{"trigger_on":"20 Oct"}`, status: 400},
		{name: "not wired", key: "key-00000001", body: `{"trigger_on":"2026-10-20"}`, status: 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := &bookStub{}
			h := &TriggerHTTP{}
			if tt.ready {
				h = &TriggerHTTP{Book: book, DB: txStub{}, Access: accessStub(tt.allowed), Idem: idemfake.New(),
					Problems: &server.Problems{}, Principal: func(*http.Request) (registry.Principal, bool) {
						return registry.Principal{Kind: registry.PrincipalUser, UserID: "u1"}, true
					}}
			}
			r := chi.NewRouter()
			h.Routes(r)
			req := httptest.NewRequest(http.MethodPost, "/deadlines/d1/trigger", strings.NewReader(tt.body))
			if tt.key != "" {
				req.Header.Set(idem.HeaderName, tt.key)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.status || book.calls != tt.calls {
				t.Fatalf("status %d calls %d, want %d and %d: %s", rec.Code, book.calls, tt.status, tt.calls, rec.Body)
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
func TestTriggerRouteMatterAccess(t *testing.T) {
	tests := []struct {
		name   string
		user   string
		status int
		calls  int
	}{
		{name: "walled matter is not found", user: "walled", status: 404},
		{name: "no capability on the matter", user: "nocap", status: 403},
		{name: "allowed user", user: "lawyer", status: 200, calls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			book := &bookStub{}
			h := &TriggerHTTP{Book: book, DB: txStub{}, Access: matterAccess{}, Idem: idemfake.New(),
				Problems: &server.Problems{}, Principal: func(*http.Request) (registry.Principal, bool) {
					return registry.Principal{Kind: registry.PrincipalUser, UserID: tt.user}, true
				}}
			r := chi.NewRouter()
			h.Routes(r)
			req := httptest.NewRequest(http.MethodPost, "/deadlines/d1/trigger", strings.NewReader(`{"trigger_on":"2026-10-20"}`))
			req.Header.Set(idem.HeaderName, "key-00000001")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tt.status || book.calls != tt.calls {
				t.Fatalf("status %d calls %d, want %d and %d: %s", rec.Code, book.calls, tt.status, tt.calls, rec.Body)
			}
		})
	}
}
