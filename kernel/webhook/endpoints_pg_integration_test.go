//go:build integration

package webhook_test

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/webhook"
)

func TestPGEndpointsOnPostgres(t *testing.T) {
	pool := queueDB(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	var want []string
	for _, h := range []struct {
		status string
		exp    *time.Time
		listed bool
	}{
		{"active", nil, true},
		{"active", &future, true},
		{"active", &past, false},
		{"disabled", nil, false},
		{"expired", &past, false},
	} {
		var id string
		err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`INSERT INTO platform.webhook (url, events, secret_ref, status, expires_at)
				 VALUES ('https://example.test/hook', '{}', 'ref:test', $1, $2) RETURNING id::text`,
				h.status, h.exp).Scan(&id)
		})
		if err != nil {
			t.Fatalf("insert webhook: %v", err)
		}
		if h.listed {
			want = append(want, id)
		}
	}
	sort.Strings(want)

	got, err := webhook.PGEndpoints{DB: pool, Now: func() time.Time { return now }}.Endpoints(ctx)
	if err != nil {
		t.Fatalf("Endpoints: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Endpoints = %v, want %v", got, want)
	}
}
