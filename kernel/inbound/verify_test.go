package inbound_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/inbound"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

var (
	secretOld = []byte("test-secret-old")
	secretNew = []byte("test-secret-new")
)

// hmacHook mounts one provider verified by HMACVerifier over db.
func hmacHook(t *testing.T, db *memDB) http.Handler {
	t.Helper()
	recv := inbound.NewPG(db, func() time.Time { return now })
	v := inbound.HMACVerifier{Secrets: [][]byte{secretOld, secretNew}, Now: func() time.Time { return now }}
	err := recv.Register(inbound.Provider{
		ID:     "payments_fake",
		Verify: v.Verify,
		Handle: func(ctx context.Context, tx pgx.Tx, e inbound.InboundEvent) error {
			_, err := tx.Exec(ctx, "INSERT INTO effect", e.EventID)
			return err
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	recv.Routes(r)
	return r
}

// rule: ARC-U-033
func TestARC_U_033_SignatureAndTimestampCheck(t *testing.T) {
	const body = `{"amount_cents":12500}`
	tests := []struct {
		name      string
		at        time.Time
		sig       func(at time.Time) string
		body      string
		want      int
		wantRows  int
		wantEff   int
		dropTSHdr bool
	}{
		{name: "valid", at: now, sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_1", at, []byte(body)) }, body: body, want: http.StatusNoContent, wantRows: 1, wantEff: 1},
		{name: "valid with rotated secret", at: now, sig: func(at time.Time) string { return "v1,AAAA " + inbound.Sign(secretNew, "evt_1", at, []byte(body)) }, body: body, want: http.StatusNoContent, wantRows: 1, wantEff: 1},
		{name: "at the 5 min edge", at: now.Add(-inbound.MaxSkew), sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_1", at, []byte(body)) }, body: body, want: http.StatusNoContent, wantRows: 1, wantEff: 1},
		{name: "replayed 6 min later", at: now.Add(-6 * time.Minute), sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_1", at, []byte(body)) }, body: body, want: http.StatusUnauthorized},
		{name: "6 min in the future", at: now.Add(6 * time.Minute), sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_1", at, []byte(body)) }, body: body, want: http.StatusUnauthorized},
		{name: "wrong secret", at: now, sig: func(at time.Time) string { return inbound.Sign([]byte("other"), "evt_1", at, []byte(body)) }, body: body, want: http.StatusUnauthorized},
		{name: "tampered body", at: now, sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_1", at, []byte(body)) }, body: `{"amount_cents":99999}`, want: http.StatusUnauthorized},
		{name: "signed for another id", at: now, sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_2", at, []byte(body)) }, body: body, want: http.StatusUnauthorized},
		{name: "timestamp changed after signing", at: now, sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_1", at.Add(-time.Second), []byte(body)) }, body: body, want: http.StatusUnauthorized},
		{name: "unknown version", at: now, sig: func(at time.Time) string { return "v9" + inbound.Sign(secretOld, "evt_1", at, []byte(body))[2:] }, body: body, want: http.StatusUnauthorized},
		{name: "no signature", at: now, sig: func(time.Time) string { return "" }, body: body, want: http.StatusUnauthorized},
		{name: "no timestamp", at: now, sig: func(at time.Time) string { return inbound.Sign(secretOld, "evt_1", at, []byte(body)) }, body: body, want: http.StatusUnauthorized, dropTSHdr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &memDB{events: map[string]bool{}}
			h := hmacHook(t, db)
			r := httptest.NewRequest(http.MethodPost, "/hooks/payments_fake", strings.NewReader(tt.body))
			r.Header.Set(inbound.HeaderWebhookID, "evt_1")
			if !tt.dropTSHdr {
				r.Header.Set(inbound.HeaderWebhookTimestamp, strconv.FormatInt(tt.at.Unix(), 10))
			}
			r.Header.Set(inbound.HeaderWebhookSignature, tt.sig(tt.at))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tt.want {
				t.Fatalf("status = %d, want %d", w.Code, tt.want)
			}
			if len(db.events) != tt.wantRows || db.effects != tt.wantEff {
				t.Fatalf("rows = %d effects = %d, want %d and %d", len(db.events), db.effects, tt.wantRows, tt.wantEff)
			}
		})
	}
}

// rule: ARC-U-033
func TestHMACVerifierErrors(t *testing.T) {
	body := []byte(`{}`)
	v := inbound.HMACVerifier{Secrets: [][]byte{secretOld}, Now: func() time.Time { return now }}
	tests := []struct {
		name string
		at   time.Time
		sig  string
		want error
	}{
		{name: "stale", at: now.Add(-10 * time.Minute), sig: inbound.Sign(secretOld, "evt_1", now.Add(-10*time.Minute), body), want: inbound.ErrStaleTimestamp},
		{name: "bad signature", at: now, sig: inbound.Sign([]byte("x"), "evt_1", now, body), want: inbound.ErrBadSignature},
		{name: "ok", at: now, sig: inbound.Sign(secretOld, "evt_1", now, body)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/hooks/payments_fake", nil)
			r.Header.Set(inbound.HeaderWebhookID, "evt_1")
			r.Header.Set(inbound.HeaderWebhookTimestamp, strconv.FormatInt(tt.at.Unix(), 10))
			r.Header.Set(inbound.HeaderWebhookSignature, tt.sig)
			id, err := v.Verify(r, body)
			if !errors.Is(err, tt.want) || (tt.want == nil && err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if tt.want == nil && id != "evt_1" {
				t.Fatalf("id = %q, want evt_1", id)
			}
			if err != nil && (strings.Contains(err.Error(), string(secretOld)) || strings.Contains(err.Error(), tt.sig)) {
				t.Fatalf("error leaks secret or signature: %v", err)
			}
		})
	}
}
