//go:build integration

package boot

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
)

// TestPGMatterAndGatePortsPostgres runs the Postgres matter updater, walls and GL on an
// empty migrated firm: an unknown matter is not found, nobody is walled from it, and an
// unbalanced entry never reaches the journal.
func TestPGMatterAndGatePortsPostgres(t *testing.T) {
	pool := billingPortsDB(t)
	ctx := context.Background()
	const missing = "00000000-0000-0000-0000-000000000009"
	name := "Renamed"
	by := registry.Principal{Kind: registry.PrincipalUser, UserID: "00000000-0000-0000-0000-000000000001"}
	if _, err := matter.NewPGUpdater(pool).Update(ctx, missing, 1, matter.PlainChange{ShortName: &name}, by); !errors.Is(err, matter.ErrNotFound) {
		t.Fatalf("Update unknown matter = %v, want ErrNotFound", err)
	}
	walled, err := matter.NewPGWalls(pool).Walled(ctx, by.UserID, missing)
	if err != nil || walled {
		t.Fatalf("Walled = %v, %v", walled, err)
	}
	gl := PGGL{Balances: ledger.Balances{Q: pool}}
	err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		_, err := gl.Post(ctx, tx, ledger.Entry{Book: "fees", SourceKind: "bill", SourceID: missing,
			Lines: []ledger.Line{{Account: "OP-1100", Side: ledger.Debit, Amount: 100}, {Account: "OP-4000", Side: ledger.Credit, Amount: 99}}})
		return err
	})
	if !errors.Is(err, ledger.ErrUnbalanced) {
		t.Fatalf("Post unbalanced = %v, want ErrUnbalanced", err)
	}
}
