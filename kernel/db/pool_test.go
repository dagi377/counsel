//go:build integration

package db

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestInTxHashesAtInUTC shows the audit chain hash input is zone-independent: with the
// caller's session zone America/Toronto, the jsonb text Postgres hashes (as the audit.chain
// trigger does) equals the text audit verification rebuilds from the stored timestamp in UTC.
func TestInTxHashesAtInUTC(t *testing.T) {
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	at := time.Date(2026, 10, 11, 23, 30, 0, 123456000, time.UTC) // previous day in Toronto
	tests := []struct {
		name string
		zone string
	}{
		{"caller in Toronto", "America/Toronto"},
		{"caller in UTC", "UTC"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			cfg.ConnConfig.RuntimeParams["timezone"] = tc.zone
			p, err := OpenConfig(ctx, cfg)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			defer p.Close()

			var trigger []byte
			err = p.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				if err := p.Bind(ctx, tx, Session{UserID: "u1", PrincipalKind: "user"}); err != nil {
					return err
				}
				return tx.QueryRow(ctx,
					"SELECT jsonb_build_object('seq', 1, 'at', $1::timestamptz)::text",
					at).Scan(&trigger)
			})
			if err != nil {
				t.Fatalf("in tx: %v", err)
			}
			// What verification rebuilds from the stored row.
			verify := []byte(`{"at": "` + at.UTC().Format("2006-01-02T15:04:05.999999") + `+00:00", "seq": 1}`)
			if sha256.Sum256(trigger) != sha256.Sum256(verify) {
				t.Fatalf("hash input differs:\ntrigger %s\nverify  %s", trigger, verify)
			}
		})
	}
}
