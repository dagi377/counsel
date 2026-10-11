//go:build integration

package trust

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

// seededHold is one authorised withdrawal and the approved gate bound to it.
type seededHold struct{ withdrawal, approval string }

// seedAuthorizedWithdrawals inserts n withdrawals of cents each on ledger, each authorised by
// its own approved trust.withdrawal gate (G11). The initiator and the authoriser differ
// (DM-078), and every authorisation passes the DM-064 hold check when it is made.
func seedAuthorizedWithdrawals(ctx context.Context, t *testing.T, tx pgx.Tx, ledger LedgerID, n int, cents int64, initiator, authorizer string) []seededHold {
	t.Helper()
	holds := make([]seededHold, 0, n)
	for i := 0; i < n; i++ {
		var w, gate string
		if err := tx.QueryRow(ctx, `SELECT core.uuidv7()::text`).Scan(&w); err != nil {
			t.Fatalf("withdrawal id: %v", err)
		}
		if err := tx.QueryRow(ctx, `INSERT INTO gate.approval_request
  (gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, status, expires_at, closed_at)
VALUES ('trust.withdrawal', 'G11', 'trust.withdrawal_request', $1::uuid, sha256($1::text::bytea), $2::uuid, 'approved',
  now() + interval '1 day', now())
RETURNING id::text`, w, initiator).Scan(&gate); err != nil {
			t.Fatalf("approval %d: %v", i, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO trust.withdrawal_request
  (id, ledger_id, purpose_category, payee, amount_cents, method, initiated_by, approval_id, authorized_by, status)
VALUES ($1::uuid, $2::uuid, 'disbursement', 'Vendor', $3, 'cheque', $4::uuid, $5::uuid, $6::uuid, 'authorized')`,
			w, string(ledger), cents, initiator, gate, authorizer); err != nil {
			t.Fatalf("withdrawal %d: %v", i, err)
		}
		holds = append(holds, seededHold{withdrawal: w, approval: gate})
	}
	return holds
}

// rule: BIL-I-010
func TestBIL_I_010_ConcurrentWithdrawalsOneLedger(t *testing.T) {
	const (
		workers = 32
		amount  = int64(10000)
	)
	tests := []struct {
		name  string
		start int64 // starting balance; the 32 authorised holds total 32 * amount
	}{
		{"balance covers every hold", workers * amount},
		{"balance covers every hold plus half an amount", workers*amount + amount/2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, user, client, matters := trustDB(t)
			ctx := context.Background()
			loc, err := time.LoadLocation("America/Toronto")
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 3, 16, 12, 0, 0, 0, loc)
			clk := clockfake.New(now, loc)
			gl := ledgerfake.New(clock.DateOf(now, loc), books.AcctTrustBank, books.AcctClientTrustLedger)
			accts := NewAccountStore(pool, itUsers{}, itSealer{})
			s := NewLedgerStore(pool, accts, gl, clk)

			var ledger LedgerID
			var holds []seededHold
			err = pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				var authorizer string
				if err := tx.QueryRow(ctx, `INSERT INTO core.app_user(email, display_name, role_id)
SELECT 'a@example.com', 'A', role_id FROM core.app_user WHERE id = $1::uuid RETURNING id::text`, user).Scan(&authorizer); err != nil {
					return err
				}
				acct, err := accts.OpenAccountNumber(ctx, tx, Account{Type: AccountMixed, Institution: "Bank", AccountLast4: "1234",
					Title: "Mixed trust", OpenedOn: now}, "000111234")
				if err != nil {
					return err
				}
				if err := accts.AddApprovedSignatory(ctx, tx, acct, user, "00000000-0000-7000-8000-000000000001", now); err != nil {
					return err
				}
				if ledger, err = s.OpenLedger(ctx, tx, Ledger{AccountID: acct, MatterID: matters[0], ClientID: client}); err != nil {
					return err
				}
				if _, err := s.Receive(ctx, tx, Receipt{LedgerID: ledger, TxnDate: now, Method: MethodEFT, Counterparty: "Client A",
					AmountCents: tt.start, Purpose: "retainer", ClearedOn: now, PostedBy: user}); err != nil {
					return err
				}
				holds = seedAuthorizedWithdrawals(ctx, t, tx, ledger, workers, amount, user, authorizer)
				return nil
			})
			if err != nil {
				t.Fatalf("seed: %v", err)
			}

			var (
				wg        sync.WaitGroup
				mu        sync.Mutex
				committed int
				rejected  []error
			)
			startGun := make(chan struct{})
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(h seededHold) {
					defer wg.Done()
					<-startGun
					err := s.InLedgerTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
						_, err := s.Disburse(ctx, tx, Disbursement{LedgerID: ledger, WithdrawalID: h.withdrawal, ApprovalID: h.approval, TxnDate: now,
							Method: MethodCheque, Counterparty: "Vendor", AmountCents: amount, Purpose: "disbursement", PostedBy: user})
						return err
					})
					mu.Lock()
					defer mu.Unlock()
					if err == nil {
						committed++
						return
					}
					rejected = append(rejected, err)
				}(holds[i])
			}
			close(startGun)
			wg.Wait()

			// rule: BIL-I-010
			if int64(committed)*amount > tt.start {
				t.Fatalf("committed %d * %d = %d exceeds the starting balance %d", committed, amount, int64(committed)*amount, tt.start)
			}
			if committed == 0 {
				t.Fatal("no withdrawal committed: the ledger lock starved every caller")
			}
			if committed+len(rejected) != workers {
				t.Fatalf("committed %d + rejected %d != %d", committed, len(rejected), workers)
			}
			// rule: BIL-I-010
			// Every refusal is 409 insufficient_trust_funds: never a 500, a deadlock or a raw check violation.
			for _, err := range rejected {
				if !errors.Is(err, ErrInsufficientFunds) {
					t.Fatalf("rejected with %v, want ErrInsufficientFunds", err)
				}
			}
			b, err := s.Balance(ctx, ledger)
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.start - int64(committed)*amount; b.PostedCents != want || b.PostedCents < 0 {
				t.Fatalf("ledger balance = %d, want %d", b.PostedCents, want)
			}
			var rows int
			if err := pool.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM trust.trust_transaction WHERE ledger_id = $1::uuid AND kind = 'disbursement'`,
					string(ledger)).Scan(&rows)
			}); err != nil {
				t.Fatal(err)
			}
			if rows != committed {
				t.Fatalf("disbursement rows = %d, committed = %d", rows, committed)
			}
		})
	}
}
