//go:build integration

package webhook_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/webhook"
)

// insertExpiring adds a platform.webhook row with status and expires_at and returns its id.
func insertExpiring(t *testing.T, pool *db.Pool, status string, exp *time.Time) string {
	t.Helper()
	var id string
	err := pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`INSERT INTO platform.webhook (url, events, secret_ref, status, expires_at)
			 VALUES ('https://example.test/hook', '{}', 'ref:test', $1, $2) RETURNING id::text`,
			status, exp).Scan(&id)
	})
	if err != nil {
		t.Fatalf("insert webhook: %v", err)
	}
	return id
}

// rule: ARC-I-049
func TestARC_I_049_PGExpiryOnPostgres(t *testing.T) {
	pool := queueDB(t)
	ctx := context.Background()
	store := webhook.PGExpiry{DB: pool}
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	day := 24 * time.Hour
	soon, later := now.Add(10*day), now.Add(40*day)
	active := insertExpiring(t, pool, "active", &soon)
	insertExpiring(t, pool, "disabled", &soon)
	insertExpiring(t, pool, "active", &later)
	insertExpiring(t, pool, "active", nil)

	eps, err := store.Expiring(ctx, now.Add(14*day))
	if err != nil {
		t.Fatalf("Expiring: %v", err)
	}
	if len(eps) != 1 || eps[0].ID != active || !eps[0].ExpiresAt.Equal(soon) {
		t.Fatalf("Expiring = %+v, want only %s at %v", eps, active, soon)
	}

	tests := []struct {
		name  string
		set   bool
		to    *time.Time
		exp   time.Time
		lead  time.Duration
		first bool
	}{
		{name: "first claim", exp: soon, lead: 14 * day, first: true},
		{name: "second claim false", exp: soon, lead: 14 * day, first: false},
		{name: "shorter lead claims", exp: soon, lead: day, first: true},
		{name: "renew", set: true, to: &later},
		{name: "stale expiry claims nothing", exp: soon, lead: day, first: false},
		{name: "claim after renew", exp: later, lead: 14 * day, first: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				if err := store.SetExpiry(ctx, active, tc.to); err != nil {
					t.Fatalf("SetExpiry: %v", err)
				}
				return
			}
			first, err := store.ClaimNotice(ctx, active, tc.exp, tc.lead)
			if err != nil || first != tc.first {
				t.Fatalf("ClaimNotice = %v, %v; want %v", first, err, tc.first)
			}
		})
	}

	if err := store.SetExpiry(ctx, "00000000-0000-7000-8000-000000000000", &later); !errors.Is(err, webhook.ErrUnknownEndpoint) {
		t.Fatalf("SetExpiry unknown = %v, want ErrUnknownEndpoint", err)
	}
}
