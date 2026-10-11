//go:build integration

package bill_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dagi377/counsel/app/legal/billing/bill"
)

type poolRunner struct{ pool *pgxpool.Pool }

func (r poolRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return fn(ctx, tx) })
}

// The billfake contract cases that need no gate or seeded matter run against PGService:
// unknown bills, List paging and Submit of a missing bill. COUNSEL_TEST_DB must be migrated.
func TestPGServiceContract(t *testing.T) {
	dsn := os.Getenv("COUNSEL_TEST_DB")
	if dsn == "" {
		t.Skip("COUNSEL_TEST_DB not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := bill.NewPGService(poolRunner{pool}, time.Now)
	const missing = bill.ID("00000000-0000-0000-0000-00000000dead")
	tests := []struct {
		name string
		run  func() error
		want error
	}{
		{"get unknown", func() error { _, err := s.Get(ctx, missing); return err }, bill.ErrNotFound},
		{"get malformed id", func() error { _, err := s.Get(ctx, "zz"); return err }, bill.ErrNotFound},
		{"submit unknown", func() error {
			return poolRunner{pool}.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error { _, err := s.Submit(ctx, tx, missing); return err })
		}, bill.ErrNotFound},
		{"list with limit", func() error { _, err := s.List(ctx, bill.Filter{Limit: 1}); return err }, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.run(); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}
