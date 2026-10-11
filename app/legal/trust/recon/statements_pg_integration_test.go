//go:build integration

package recon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust/recon"
)

// TestPGStatementsClosingIntegration reads the latest March import's closing balance and
// ignores an import with no stored balance.
func TestPGStatementsClosingIntegration(t *testing.T) {
	env := newSignOffEnv(t)
	ctx := context.Background()
	err := env.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, imp := range []struct {
			sha     string
			period  string
			closing *int64
			at      string
		}{
			{"01", "[2026-03-01,2026-04-01)", ptr(100_00), "2026-04-02T10:00:00Z"},
			{"02", "[2026-03-01,2026-04-01)", ptr(123_45), "2026-04-03T10:00:00Z"},
			{"03", "[2026-03-01,2026-04-01)", nil, "2026-04-04T10:00:00Z"},
			{"04", "[2026-04-01,2026-05-01)", ptr(999_99), "2026-05-02T10:00:00Z"},
		} {
			if _, err := tx.Exec(ctx, `INSERT INTO trust.bank_import(account_id, file_ref, sha256, period, imported_by, imported_at, closing_cents)
VALUES ($1::uuid, 'ref:test', decode($2, 'hex'), $3::daterange, $4::uuid, $5::timestamptz, $6)`,
				env.account, imp.sha, imp.period, env.prep, imp.at, imp.closing); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed imports: %v", err)
	}
	s := recon.NewPGStatements(env.pool)
	tests := []struct {
		name    string
		p       recon.Period
		want    int64
		wantErr error
	}{
		{name: "latest march import with a balance", p: recon.Period{Year: 2026, Month: time.March}, want: 123_45},
		{name: "april", p: recon.Period{Year: 2026, Month: time.April}, want: 999_99},
		{name: "no may import", p: recon.Period{Year: 2026, Month: time.May}, wantErr: recon.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Closing(ctx, env.account, tt.p)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Fatalf("Closing = %d, %v; want %d, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func ptr(n int64) *int64 { return &n }
