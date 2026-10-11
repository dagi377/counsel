//go:build integration

package recon_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/flags"
)

func verifiedRecon(t *testing.T) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: recon.FlagBylaw9Verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(recon.FlagBylaw9Verified, true); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

// rule: BIL-I-009
// The reconfake TestPrepareApprove cases against PGService: a month with an imported
// statement is prepared over the open draft (bank from the statement, book and client
// ledgers from the journal); a month without one is not found; the service never approves.
func TestBIL_I_009_PGServiceIntegration(t *testing.T) {
	env := newSignOffEnv(t)
	ctx := verifiedRecon(t)
	err := env.pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO trust.bank_import(account_id, file_ref, sha256, period, imported_by, imported_at, closing_cents)
VALUES ($1::uuid, 'ref:test', decode('aa', 'hex'), '[2026-03-01,2026-04-01)'::daterange, $2::uuid, now(), 0)`, env.account, env.prep)
		return err
	})
	if err != nil {
		t.Fatalf("seed import: %v", err)
	}
	s := recon.PGService{DB: env.pool}
	march := recon.Period{Year: 2026, Month: time.March}
	tests := []struct {
		name    string
		p       recon.Period
		wantErr error
	}{
		{"march, balanced", march, nil},
		{"may, no statement", recon.Period{Year: 2026, Month: time.May}, recon.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec, err := s.Prepare(ctx, env.account, tt.p, nil, env.prep)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Prepare = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if rec.ID != env.march || !rec.Result.Balanced() {
				t.Fatalf("Prepare = %+v, want the March draft, balanced", rec)
			}
			got, err := s.Get(ctx, rec.ID)
			if err != nil || got.Status != recon.StatusPrepared || got.PreparedBy != env.prep {
				t.Fatalf("Get = %+v, %v", got, err)
			}
		})
	}
	if err := s.Approve(ctx, env.march, recon.Approver{UserID: env.lawyer}); !errors.Is(err, recon.ErrEffectOnly) {
		t.Fatalf("Approve = %v, want ErrEffectOnly", err)
	}
	if locked, err := s.Locked(ctx, env.account, march); err != nil || locked {
		t.Fatalf("Locked = %v, %v; want false", locked, err)
	}
}
