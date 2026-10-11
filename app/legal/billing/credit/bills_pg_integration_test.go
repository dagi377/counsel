//go:build integration

package credit

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// connRunner runs fn in one transaction on conn, rolled back afterwards.
type connRunner struct{ conn *pgx.Conn }

func (r connRunner) InTx(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	tx, err := r.conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return fn(ctx, tx)
}

// rule: BIL-C-082
// The creditfake TestNotFound case against PGBills on the real schema: an unknown or
// malformed bill id is not found.
func TestBIL_C_082_PGBillsIntegration(t *testing.T) {
	conn := noteTestConn(t)
	bills := PGBills{DB: connRunner{conn: conn}}
	for _, id := range []string{"00000000-0000-7000-8000-000000000001", "not-a-uuid"} {
		t.Run(id, func(t *testing.T) {
			if _, err := bills.Bill(context.Background(), id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("Bill(%s) = %v, want ErrNotFound", id, err)
			}
		})
	}
}
