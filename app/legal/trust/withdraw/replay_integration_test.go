//go:build integration

package withdraw

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/registry"
)

// claimEffect claims the gate's effect the way kernel/gate does before running a handler: the
// first claim inserts the gate.effect_execution row, a replay finds it and runs nothing.
func claimEffect(ctx context.Context, tx pgx.Tx, gateID string) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO gate.effect_execution (approval_id, handler) VALUES ($1::uuid, $2)
ON CONFLICT (approval_id) DO NOTHING`, gateID, GateKind)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// replayCounts returns the trust transactions posted for the withdrawal (unique index
// trust_txn_once_per_withdrawal) and the effect_execution rows for its gate.
func (env itEnv) replayCounts(t *testing.T, wid, gid string) (txns, effects int) {
	t.Helper()
	err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM trust.trust_transaction WHERE withdrawal_id = $1::uuid`, wid).Scan(&txns); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM gate.effect_execution WHERE approval_id = $1::uuid`, gid).Scan(&effects)
	})
	if err != nil {
		t.Fatal(err)
	}
	return txns, effects
}

// The withdrawal effect is replayed 3 times concurrently, each in its own transaction. It
// posts once: one trust transaction and one gate.effect_execution row, and the ledger moves by
// the withdrawal amount only.
func TestWithdrawalEffectReplayOnce(t *testing.T) {
	const balance, amount, replays = 10000, 4000, 3
	tests := []struct {
		name string
		// claimed runs the kernel claim before the effect; without it the effect's own
		// withdrawal lock and status check must hold the line.
		claimed bool
		// applied is how many of the concurrent replays run the effect to completion.
		applied int
	}{
		{"replays through the kernel claim", true, 1},
		{"replays that bypass the claim", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newItEnv(t, balance)
			wid, gid := env.request(t, amount)
			e := env.effect()
			p := env.terms(t, wid)
			g := registry.Approved{GateID: gid, Kind: GateKind, Payload: p,
				Approvers: []registry.Principal{{Kind: registry.PrincipalUser, UserID: env.users["lawyer1"]}}}
			executor := registry.Principal{Kind: registry.PrincipalUser, UserID: env.users["bookkeeper"]}

			// effect authorises the withdrawal and posts it, in one transaction.
			effect := func(ctx context.Context, tx pgx.Tx) error {
				if err := e.Apply(ctx, tx, g); err != nil {
					return err
				}
				_, err := e.Execute(ctx, tx, wid, executor, env.now)
				return err
			}
			if !tt.claimed {
				// The first run claims and applies; the replays below skip the claim.
				if err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
					if _, err := claimEffect(ctx, tx, gid); err != nil {
						return err
					}
					return effect(ctx, tx)
				}); err != nil {
					t.Fatalf("first run: %v", err)
				}
			}

			start := make(chan struct{})
			errs := make([]error, replays)
			ran := make([]bool, replays)
			var wg sync.WaitGroup
			for i := range replays {
				wg.Go(func() {
					<-start
					errs[i] = env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
						if tt.claimed {
							ok, err := claimEffect(ctx, tx, gid)
							if err != nil || !ok {
								return err
							}
						}
						ran[i] = true
						return effect(ctx, tx)
					})
				})
			}
			close(start)
			wg.Wait()

			var ranOK int
			for i, err := range errs {
				switch {
				case err == nil && ran[i]:
					ranOK++
				case err == nil:
				case !tt.claimed && errors.Is(err, ErrNotAuthorized):
					// The replay found the withdrawal already executed and refused.
				default:
					t.Fatalf("replay %d: %v", i, err)
				}
			}
			if ranOK != tt.applied {
				t.Fatalf("replays that applied = %d, want %d", ranOK, tt.applied)
			}
			txns, effects := env.replayCounts(t, wid, gid)
			if txns != 1 || effects != 1 {
				t.Fatalf("trust transactions = %d, effect executions = %d; want 1 and 1", txns, effects)
			}
			if got := env.posted(t); got != balance-amount {
				t.Fatalf("ledger = %d, want %d", got, balance-amount)
			}
		})
	}
}
