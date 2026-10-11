package webhook_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/webhook"
)

var _ boot.WebhookEndpoints = webhook.PGEndpoints{}

const testSecret = "whsec_do-not-leak-0123456789"

// endpointRow is one scripted platform.webhook row.
type endpointRow struct {
	id        string
	status    string
	expiresAt *time.Time
	secretRef string
}

// endpointsTx answers the endpoint query from scripted rows, applying the statement's
// filter (status = 'active' AND (expires_at IS NULL OR expires_at > $1)) and returning
// only the columns it selects. Methods PGEndpoints does not call panic.
type endpointsTx struct {
	pgx.Tx
	rows []endpointRow
	fail error
	sql  string
	args []any
}

func (f *endpointsTx) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	f.sql, f.args = sql, args
	if f.fail != nil {
		return nil, f.fail
	}
	now := args[0].(time.Time)
	out := &endpointRows{}
	for _, r := range f.rows {
		if r.status == "active" && (r.expiresAt == nil || r.expiresAt.After(now)) {
			out.ids = append(out.ids, r.id)
		}
	}
	return out, nil
}

type endpointRows struct {
	pgx.Rows
	ids []string
	i   int
}

func (r *endpointRows) Next() bool { r.i++; return r.i <= len(r.ids) }
func (r *endpointRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return errors.New("endpointRows: scan wants exactly the id")
	}
	*dest[0].(*string) = r.ids[r.i-1]
	return nil
}
func (r *endpointRows) Err() error { return nil }
func (r *endpointRows) Close()     {}

// endpointsDB is a db.DB whose InTx hands fn the scripted tx.
type endpointsDB struct{ tx pgx.Tx }

func (d endpointsDB) InTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	return fn(ctx, d.tx)
}
func (endpointsDB) Bind(context.Context, pgx.Tx, db.Session) error { return nil }
func (endpointsDB) Ping(context.Context) error                     { return nil }
func (endpointsDB) Close()                                         {}

func TestPGEndpoints(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	boom := errors.New("connection reset")
	rows := []endpointRow{
		{id: "c", status: "active", expiresAt: at(time.Hour), secretRef: testSecret},
		{id: "a", status: "active", secretRef: testSecret},
		{id: "b", status: "active", expiresAt: at(-time.Hour), secretRef: testSecret},
		{id: "d", status: "disabled", secretRef: testSecret},
		{id: "e", status: "expired", expiresAt: at(-time.Hour), secretRef: testSecret},
		{id: "f", status: "active", expiresAt: at(0), secretRef: testSecret},
	}
	tests := []struct {
		name string
		rows []endpointRow
		fail error
		want []string
		err  error
	}{
		{"active unexpired only, sorted", rows, nil, []string{"a", "c"}, nil},
		{"no endpoints", nil, nil, nil, nil},
		{"query error is wrapped", rows, boom, nil, boom},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			prev := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(prev) })

			tx := &endpointsTx{rows: tc.rows, fail: tc.fail}
			got, err := webhook.PGEndpoints{DB: endpointsDB{tx}, Now: func() time.Time { return now }}.Endpoints(context.Background())
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if err != nil && err.Error() == tc.err.Error() {
				t.Fatalf("err %q carries no context", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Endpoints = %v, want %v", got, tc.want)
			}
			if a := tx.args; len(a) != 1 || !a[0].(time.Time).Equal(now) {
				t.Fatalf("query args = %v, want [now]", a)
			}
			if strings.Contains(tx.sql, "secret_ref") {
				t.Fatalf("endpoint query reads secret_ref: %s", tx.sql)
			}
			if strings.Contains(logs.String(), testSecret) || (err != nil && strings.Contains(err.Error(), testSecret)) {
				t.Fatalf("secret leaked: logs %q, err %v", logs.String(), err)
			}
		})
	}
}
