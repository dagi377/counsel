package inbound_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/inbound"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// memDB keeps platform.inbound_event and an effect count in memory. A tx's writes apply
// only when fn returns nil.
type memDB struct {
	events  map[string]bool
	effects int
	down    bool
}

type memTx struct {
	pgx.Tx
	db      *memDB
	events  map[string]bool
	effects int
}

func (t *memTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.Contains(sql, "INSERT INTO platform.inbound_event"):
		k := fmt.Sprint(args[0], "/", args[1])
		if t.db.events[k] || t.events[k] {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		t.events[k] = true
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(sql, "UPDATE platform.inbound_event"):
		return pgconn.NewCommandTag("UPDATE 1"), nil
	case strings.Contains(sql, "effect"):
		t.effects++
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	}
	return pgconn.CommandTag{}, errors.New("unexpected sql")
}

func (d *memDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if d.down {
		return errors.New("primary failing over")
	}
	tx := &memTx{db: d, events: map[string]bool{}}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	for k := range tx.events {
		d.events[k] = true
	}
	d.effects += tx.effects
	return nil
}

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// verify reads webhook-id and checks webhook-timestamp against MaxSkew; a body of "bad"
// fails the signature.
func verify(r *http.Request, body []byte) (string, error) {
	ts, err := strconv.ParseInt(r.Header.Get("webhook-timestamp"), 10, 64)
	if err != nil {
		return "", inbound.ErrBadSignature
	}
	if d := now.Sub(time.Unix(ts, 0)); d > inbound.MaxSkew || d < -inbound.MaxSkew {
		return "", inbound.ErrStaleTimestamp
	}
	if string(body) == `"bad"` {
		return "", inbound.ErrBadSignature
	}
	return r.Header.Get("webhook-id"), nil
}

// appProvider is what an app registers through Registry.InboundProvider.
func appProvider(id string) registry.Provider {
	return registry.Provider{
		ID:     id,
		Verify: verify,
		Handle: func(ctx context.Context, tx pgx.Tx, e registry.InboundEvent) error {
			_, err := tx.Exec(ctx, "INSERT INTO effect", e.EventID)
			return err
		},
	}
}

// setup registers the app's providers the way boot does: from Registry.InboundProvider.
func setup(t *testing.T, db *memDB) http.Handler {
	t.Helper()
	rec := &registry.Recorder{}
	rec.InboundProvider(appProvider("payments_fake"))
	rec.InboundProvider(appProvider("esign_fake"))
	recv := inbound.NewPG(db, func() time.Time { return now })
	for _, c := range rec.Method(registry.MethodInboundProvider) {
		if err := recv.Register(inbound.FromRegistry(c.Args[0].(registry.Provider))); err != nil {
			t.Fatal(err)
		}
	}
	r := chi.NewRouter()
	recv.Routes(r)
	return r
}

func hook(h http.Handler, provider, id string, at time.Time, body string) int {
	r := httptest.NewRequest(http.MethodPost, "/hooks/"+provider, strings.NewReader(body))
	r.Header.Set("webhook-id", id)
	r.Header.Set("webhook-timestamp", strconv.FormatInt(at.Unix(), 10))
	r.Header.Set("webhook-signature", "v1,test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// rule: BIL-I-020
func TestBIL_I_020_DuplicateWebhookOneRowOneEffect(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		body     string
	}{
		{name: "payment event", provider: "payments_fake", body: `{"type":"payment.succeeded"}`},
		{name: "signature event", provider: "esign_fake", body: `{"type":"envelope.completed"}`},
		{name: "any other type", provider: "payments_fake", body: `{"type":"payout.paid"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &memDB{events: map[string]bool{}}
			h := setup(t, db)
			for i := range 2 {
				if code := hook(h, tt.provider, "evt_1", now, tt.body); code != http.StatusNoContent {
					t.Fatalf("call %d = %d, want 204", i+1, code)
				}
			}
			if len(db.events) != 1 || db.effects != 1 {
				t.Fatalf("inbound_event rows = %d, effects = %d; want 1, 1", len(db.events), db.effects)
			}
		})
	}
}

func TestDedupKeyIsProviderAndEventID(t *testing.T) {
	db := &memDB{events: map[string]bool{}}
	h := setup(t, db)
	calls := []struct{ provider, id string }{
		{"payments_fake", "evt_1"}, {"esign_fake", "evt_1"}, {"payments_fake", "evt_2"}, {"payments_fake", "evt_1"},
	}
	for _, c := range calls {
		if code := hook(h, c.provider, c.id, now, `{}`); code != http.StatusNoContent {
			t.Fatalf("%s/%s = %d", c.provider, c.id, code)
		}
	}
	if len(db.events) != 3 || db.effects != 3 {
		t.Fatalf("rows = %d, effects = %d; want 3, 3", len(db.events), db.effects)
	}
}

// rule: ARC-U-033
func TestARC_U_033_VerifiedStoredBefore2xx(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		at       time.Time
		body     string
		down     bool
		want     int
		wantRows int
	}{
		{name: "fresh valid", provider: "payments_fake", at: now, body: `{}`, want: http.StatusNoContent, wantRows: 1},
		{name: "replayed 6 min later", provider: "payments_fake", at: now.Add(-6 * time.Minute), body: `{}`, want: http.StatusUnauthorized},
		{name: "bad signature", provider: "payments_fake", at: now, body: `"bad"`, want: http.StatusUnauthorized},
		{name: "db failing over", provider: "payments_fake", at: now, body: `{}`, down: true, want: http.StatusServiceUnavailable},
		{name: "unknown provider", provider: "nobody", at: now, body: `{}`, want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &memDB{events: map[string]bool{}, down: tt.down}
			h := setup(t, db)
			if code := hook(h, tt.provider, "evt_1", tt.at, tt.body); code != tt.want {
				t.Fatalf("status = %d, want %d", code, tt.want)
			}
			if len(db.events) != tt.wantRows {
				t.Fatalf("rows = %d, want %d", len(db.events), tt.wantRows)
			}
		})
	}
}

func TestRegisterProvider(t *testing.T) {
	ok := inbound.FromRegistry(appProvider("payments_fake"))
	tests := []struct {
		name    string
		p       inbound.Provider
		wantErr bool
		is      error
	}{
		{name: "empty id", p: inbound.Provider{Verify: ok.Verify, Handle: ok.Handle}, wantErr: true},
		{name: "bad id", p: inbound.Provider{ID: "Bad-Id", Verify: ok.Verify, Handle: ok.Handle}, wantErr: true},
		{name: "no handler", p: inbound.Provider{ID: "x_y", Verify: ok.Verify}, wantErr: true},
		{name: "duplicate", p: ok, wantErr: true, is: inbound.ErrDuplicateProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recv := inbound.NewPG(&memDB{}, nil)
			if err := recv.Register(ok); err != nil {
				t.Fatal(err)
			}
			err := recv.Register(tt.p)
			if (err != nil) != tt.wantErr || (tt.is != nil && !errors.Is(err, tt.is)) {
				t.Fatalf("Register = %v", err)
			}
		})
	}
}

func TestReceiveEmptyEventID(t *testing.T) {
	recv := inbound.NewPG(&memDB{events: map[string]bool{}}, func() time.Time { return now })
	p := inbound.FromRegistry(appProvider("payments_fake"))
	if err := recv.Register(p); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/hooks/payments_fake", nil)
	r.Header.Set("webhook-timestamp", strconv.FormatInt(now.Unix(), 10))
	body, _ := json.Marshal(map[string]string{})
	if _, err := recv.Receive(context.Background(), "payments_fake", r, body); !errors.Is(err, inbound.ErrNoEventID) {
		t.Fatalf("Receive = %v, want ErrNoEventID", err)
	}
}
