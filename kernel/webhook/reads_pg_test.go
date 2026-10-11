package webhook_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/idem/idemfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/webhook"
)

var _ webhook.VersionedExpiry = webhook.PGExpiry{}

// hookRow is one in-memory platform.webhook row.
type hookRow struct {
	url       string
	events    []string
	status    string
	expiresAt *time.Time
	version   int
	secretRef string
}

// webhookTable answers PGEndpointReads' selects and PGExpiry.SetExpiryAt from in-memory rows.
// bumpAfterRead simulates a write by someone else right after the next single-row read.
type webhookTable struct {
	pgx.Tx
	rows          map[string]*hookRow
	sqls          []string
	bumpAfterRead bool
}

type hookRows struct {
	pgx.Rows
	ids []string
	t   *webhookTable
	i   int
}

func (r *hookRows) Next() bool             { r.i++; return r.i <= len(r.ids) }
func (r *hookRows) Scan(dest ...any) error { return r.t.scan(r.ids[r.i-1], dest) }
func (r *hookRows) Err() error             { return nil }
func (r *hookRows) Close()                 {}

type hookRow1 struct {
	t  *webhookTable
	id string
}

func (r hookRow1) Scan(dest ...any) error {
	if _, ok := r.t.rows[r.id]; !ok {
		return pgx.ErrNoRows
	}
	if err := r.t.scan(r.id, dest); err != nil {
		return err
	}
	if r.t.bumpAfterRead {
		r.t.rows[r.id].version++
		r.t.bumpAfterRead = false
	}
	return nil
}

func (t *webhookTable) scan(id string, dest []any) error {
	if len(dest) != 6 {
		return fmt.Errorf("scan wants 6 columns, got %d", len(dest))
	}
	row := t.rows[id]
	*dest[0].(*string) = id
	*dest[1].(*string) = row.url
	*dest[2].(*[]string) = row.events
	*dest[3].(*string) = row.status
	*dest[4].(**time.Time) = row.expiresAt
	*dest[5].(*int) = row.version
	return nil
}

func (t *webhookTable) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	t.sqls = append(t.sqls, sql)
	after, limit := args[0].(string), args[1].(int)
	var ids []string
	for id := range t.rows {
		if id > after {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return &hookRows{ids: ids, t: t}, nil
}

func (t *webhookTable) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.sqls = append(t.sqls, sql)
	return hookRow1{t: t, id: args[0].(string)}
}

func (t *webhookTable) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.sqls = append(t.sqls, sql)
	if !strings.Contains(sql, "row_version = $3") {
		return pgconn.CommandTag{}, fmt.Errorf("unexpected exec: %s", sql)
	}
	row, ok := t.rows[args[0].(string)]
	if !ok || row.version != args[2].(int) {
		return pgconn.NewCommandTag("UPDATE 0"), nil
	}
	row.expiresAt, row.version = args[1].(*time.Time), row.version+1
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func newWebhookTable() *webhookTable {
	future := httpNow.Add(30 * 24 * time.Hour)
	return &webhookTable{rows: map[string]*hookRow{
		hookActive: {url: "https://example.test/a", events: []string{"work.item_created"}, status: webhook.StatusActive,
			expiresAt: &future, version: 3, secretRef: signingSecret},
		hookExpired: {url: "https://example.test/b", events: []string{"work.item_created"}, status: webhook.StatusExpired,
			version: 1, secretRef: signingSecret},
	}}
}

func TestPGEndpointReads(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		id      string
		version int
		err     error
	}{
		{"row_version is the endpoint version", hookActive, 3, nil},
		{"other endpoint", hookExpired, 1, nil},
		{"unknown id", hookMissing, 0, webhook.ErrUnknownEndpoint},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := newWebhookTable()
			got, err := webhook.PGEndpointReads{DB: endpointsDB{tbl}}.Endpoint(ctx, tt.id)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if err == nil && (got.ID != tt.id || got.Version != tt.version) {
				t.Fatalf("Endpoint = %+v, want id %s version %d", got, tt.id, tt.version)
			}
			for _, sql := range tbl.sqls {
				if strings.Contains(sql, "secret_ref") || !strings.Contains(sql, "row_version") {
					t.Fatalf("read selects secret_ref or misses row_version: %s", sql)
				}
			}
		})
	}
	page, err := webhook.PGEndpointReads{DB: endpointsDB{newWebhookTable()}}.Endpoints(ctx, webhook.EndpointFilter{After: hookActive, Limit: 5})
	if err != nil || len(page) != 1 || page[0].ID != hookExpired {
		t.Fatalf("Endpoints after %s = %+v, %v; want only %s", hookActive, page, err, hookExpired)
	}
}

// rule: ARC-I-006
// TestARC_I_006_WebhookIfMatchOnPostgres drives PATCH over the Postgres reads and expiry:
// a missing If-Match is 428, a stale one 412, a write between the check and the renew 412,
// and a current one renews and bumps row_version.
func TestARC_I_006_WebhookIfMatchOnPostgres(t *testing.T) {
	body := `{"expires_at":"2027-01-01T00:00:00Z"}`
	tests := []struct {
		name        string
		ifMatch     string
		race        bool
		status      int
		wantVersion int
	}{
		{"missing If-Match", "", false, http.StatusPreconditionRequired, 3},
		{"stale If-Match", `"2"`, false, http.StatusPreconditionFailed, 3},
		{"changed after the check", `"3"`, true, http.StatusPreconditionFailed, 4},
		{"current If-Match renews", `"3"`, false, http.StatusOK, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tbl := newWebhookTable()
			tbl.bumpAfterRead = tt.race
			d := endpointsDB{tbl}
			codec, err := api.NewCodec([]byte("0123456789abcdef0123456789abcdef"))
			if err != nil {
				t.Fatal(err)
			}
			h := &webhook.HTTP{
				Endpoints: webhook.PGEndpointReads{DB: d}, Expiry: webhook.PGExpiry{DB: d}, DB: txStub{},
				Gates: &stubGates{}, Idem: idemfake.New(), Cursors: codec, Problems: &server.Problems{},
				Access: stubAccess{caps: map[string][]string{userAdmin: {webhook.CapIntegrations}}},
				Principal: func(r *http.Request) (registry.Principal, bool) {
					return registry.Principal{Kind: registry.PrincipalUser, UserID: r.Header.Get("X-Test-User")}, true
				},
				Now: func() time.Time { return httpNow },
			}
			m := server.New(nil)
			if err := m.Mount(h.Routes); err != nil {
				t.Fatal(err)
			}
			rec := doHTTP(m.Handler(), http.MethodPatch, "/v1/webhook-endpoints/"+hookActive, userAdmin, body, hdrs("key-00000001", tt.ifMatch))
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.status, rec.Body)
			}
			if got := tbl.rows[hookActive].version; got != tt.wantVersion {
				t.Fatalf("row_version = %d, want %d", got, tt.wantVersion)
			}
			if tt.status == http.StatusOK && rec.Header().Get("ETag") != `"4"` {
				t.Fatalf("ETag = %q, want \"4\"", rec.Header().Get("ETag"))
			}
			if strings.Contains(rec.Body.String(), signingSecret) {
				t.Fatal("secret in body")
			}
		})
	}
}
