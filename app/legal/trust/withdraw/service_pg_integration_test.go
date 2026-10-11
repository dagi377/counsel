//go:build integration

package withdraw

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

// matterOf is the matter of env's client ledger.
func (env itEnv) matterOf(t *testing.T) string {
	t.Helper()
	var m string
	if err := env.pool.InTx(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT matter_id::text FROM trust.client_ledger WHERE id = $1::uuid`, string(env.ledger)).Scan(&m)
	}); err != nil {
		t.Fatal(err)
	}
	return m
}

// rule: BIL-C-053 (verify)
// The withdrawfake TestRequest cases against PGService: a request within the available
// balance is recorded as requested; a bearer payee, an EFT without Form 9A and an overdraw
// are refused. Recording a request holds nothing: Available is unchanged until the gate
// effect authorizes it.
func TestBIL_C_053_PGServiceIntegration(t *testing.T) {
	env := newItEnv(t, 10000)
	ctx := verifiedCtx(t)
	s := PGService{DB: env.pool, Ledgers: env.store}
	base := Request{TrustAccountID: string(env.acct), MatterID: env.matterOf(t), Payee: "Vendor Inc",
		Method: MethodCheque, Reason: "disbursement", PreparedBy: env.users["prep"]}
	tests := []struct {
		name string
		edit func(*Request)
		want error
	}{
		{"within balance", func(r *Request) { r.AmountCents = 4000 }, nil},
		{"bearer payee", func(r *Request) { r.AmountCents = 100; r.Payee = "bearer" }, ErrBearerPayee},
		{"eft without form 9a", func(r *Request) { r.AmountCents = 100; r.Method = MethodEFT }, ErrNoEFTAuthorization},
		{"overdraw", func(r *Request) { r.AmountCents = 10001 }, ErrTrustOverdraw},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			tt.edit(&r)
			w, err := s.Request(ctx, r)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Request = %v, want %v", err, tt.want)
			}
			if tt.want != nil {
				return
			}
			got, err := s.Get(ctx, w.ID)
			if err != nil || got.Status != StatusRequested || got.Request.AmountCents != r.AmountCents {
				t.Fatalf("Get = %+v, %v", got, err)
			}
		})
	}
	avail, err := s.Available(ctx, base.TrustAccountID, base.MatterID)
	if err != nil || avail != 10000 {
		t.Fatalf("Available = %d, %v; want 10000", avail, err)
	}
	if _, err := s.Authorize(ctx, "w", env.users["lawyer1"], env.now); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("Authorize = %v, want ErrNotAuthorized", err)
	}
}
