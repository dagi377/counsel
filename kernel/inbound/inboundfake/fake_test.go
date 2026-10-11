package inboundfake

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/inbound"
)

// provider returns a test provider that reads the event id from the Webhook-Id header and
// refuses any request whose Webhook-Signature header is not "ok".
func provider(id string, effects *int, handleErr error) inbound.Provider {
	return inbound.Provider{
		ID: id,
		Verify: func(r *http.Request, _ []byte) (string, error) {
			switch r.Header.Get("Webhook-Signature") {
			case "ok":
				return r.Header.Get("Webhook-Id"), nil
			case "stale":
				return "", inbound.ErrStaleTimestamp
			default:
				return "", inbound.ErrBadSignature
			}
		},
		Handle: func(context.Context, pgx.Tx, inbound.InboundEvent) error {
			if handleErr != nil {
				return handleErr
			}
			*effects++
			return nil
		},
	}
}

func req(eventID, sig string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/hooks/p", nil)
	r.Header.Set("Webhook-Id", eventID)
	r.Header.Set("Webhook-Signature", sig)
	return r
}

type delivery struct {
	provider, eventID, sig string
}

func TestReceive(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name       string
		handleErr  error
		deliveries []delivery
		wantErr    error // error of the last delivery
		wantDup    bool  // Duplicate of the last delivery
		wantRows   int
		wantEffect int
	}{
		{name: "first delivery", deliveries: []delivery{{"p", "e1", "ok"}}, wantRows: 1, wantEffect: 1},
		{name: "same event twice is one row and one effect", deliveries: []delivery{{"p", "e1", "ok"}, {"p", "e1", "ok"}}, wantDup: true, wantRows: 1, wantEffect: 1},
		{name: "same event id from another provider is distinct", deliveries: []delivery{{"p", "e1", "ok"}, {"q", "e1", "ok"}}, wantRows: 2, wantEffect: 2},
		{name: "bad signature records nothing", deliveries: []delivery{{"p", "e1", "bad"}}, wantErr: inbound.ErrBadSignature},
		{name: "stale timestamp records nothing", deliveries: []delivery{{"p", "e1", "stale"}}, wantErr: inbound.ErrStaleTimestamp},
		{name: "unknown provider", deliveries: []delivery{{"z", "e1", "ok"}}, wantErr: inbound.ErrUnknownProvider},
		{name: "empty event id", deliveries: []delivery{{"p", "", "ok"}}, wantErr: inbound.ErrNoEventID},
		{name: "failed effect records nothing", handleErr: boom, deliveries: []delivery{{"p", "e1", "ok"}}, wantErr: boom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) })
			effects := 0
			for _, id := range []string{"p", "q"} {
				if err := f.Register(provider(id, &effects, tt.handleErr)); err != nil {
					t.Fatal(err)
				}
			}
			var res inbound.Result
			var err error
			for _, d := range tt.deliveries {
				res, err = f.Receive(context.Background(), d.provider, req(d.eventID, d.sig), []byte("{}"))
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if res.Duplicate != tt.wantDup {
				t.Fatalf("Duplicate = %v, want %v", res.Duplicate, tt.wantDup)
			}
			if got := f.Events(); got != tt.wantRows {
				t.Fatalf("rows = %d, want %d", got, tt.wantRows)
			}
			if effects != tt.wantEffect {
				t.Fatalf("effects = %d, want %d", effects, tt.wantEffect)
			}
		})
	}
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name string
		ids  []string
		want error
	}{
		{name: "distinct", ids: []string{"a", "b"}},
		{name: "duplicate", ids: []string{"a", "a"}, want: inbound.ErrDuplicateProvider},
		{name: "empty", ids: []string{""}, want: inbound.ErrUnknownProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(nil)
			n := 0
			var err error
			for _, id := range tt.ids {
				err = f.Register(provider(id, &n, nil))
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
