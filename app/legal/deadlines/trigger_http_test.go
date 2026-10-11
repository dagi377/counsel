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
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

type bookStub struct{ calls int }

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
