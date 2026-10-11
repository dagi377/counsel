//go:build integration

package webhook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/webhook"
)

func TestPGTargetsOnPostgres(t *testing.T) {
	pool := queueDB(t)
	ctx := context.Background()
	var id string
	err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`INSERT INTO platform.webhook (url, events, secret_ref) VALUES ('https://example.test/hook', '{}', 'ref:target')
			 RETURNING id::text`).Scan(&id)
	})
	if err != nil {
		t.Fatalf("insert webhook: %v", err)
	}
	for _, tc := range []struct {
		name    string
		id      string
		want    webhook.Target
		wantErr error
	}{
		{"known", id, webhook.Target{URL: "https://example.test/hook", SecretRef: "ref:target"}, nil},
		{"unknown", "00000000-0000-7000-8000-000000000000", webhook.Target{}, webhook.ErrUnknownEndpoint},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := webhook.PGTargets{DB: pool}.Target(ctx, tc.id)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Target err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("Target = %+v, want %+v", got, tc.want)
			}
		})
	}
}
