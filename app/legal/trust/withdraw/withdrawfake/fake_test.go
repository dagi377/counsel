package withdrawfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/withdraw"
)

var t0 = time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)

func req(amount int64) withdraw.Request {
	return withdraw.Request{TrustAccountID: "ta1", MatterID: "m1", AmountCents: amount, Payee: "Acme Ltd", Method: withdraw.MethodCheque, PreparedBy: "clerk"}
}

func TestRequest(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*withdraw.Request)
		want error
	}{
		{name: "ok", mod: func(*withdraw.Request) {}, want: nil},
		{name: "cash payee", mod: func(r *withdraw.Request) { r.Payee = "Cash" }, want: withdraw.ErrBearerPayee},
		{name: "bearer payee", mod: func(r *withdraw.Request) { r.Payee = "bearer" }, want: withdraw.ErrBearerPayee},
		{name: "zero amount", mod: func(r *withdraw.Request) { r.AmountCents = 0 }, want: withdraw.ErrInvalid},
		{name: "uncleared", mod: func(r *withdraw.Request) { r.AmountCents = 12000 }, want: withdraw.ErrUnclearedFunds},
		{name: "overdraw", mod: func(r *withdraw.Request) { r.AmountCents = 99000 }, want: withdraw.ErrTrustOverdraw},
		{name: "eft without 9A", mod: func(r *withdraw.Request) { r.Method = withdraw.MethodEFT }, want: withdraw.ErrNoEFTAuthorization},
		{name: "eft with 9A", mod: func(r *withdraw.Request) { r.Method, r.EFTAuthorizationID = withdraw.MethodEFT, "f9a" }, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(time.Hour)
			f.SetBalance("ta1", "m1", 10000, 5000)
			r := req(4000)
			tt.mod(&r)
			if _, err := f.Request(context.Background(), r); !errors.Is(err, tt.want) {
				t.Fatalf("Request err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestRequestIdempotent(t *testing.T) {
	f := New(time.Hour)
	f.SetBalance("ta1", "m1", 10000, 0)
	r := req(4000)
	r.IdempotencyKey = "k1"
	a, _ := f.Request(context.Background(), r)
	b, _ := f.Request(context.Background(), r)
	if a.ID != b.ID {
		t.Fatalf("ids %s and %s, want one withdrawal", a.ID, b.ID)
	}
}

func TestLifecycle(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		cleared  int64
		amount   int64
		approver string
		executor string
		execAt   time.Time
		wantAuth error
		wantExec error
	}{
		{name: "executed", cleared: 10000, amount: 4000, approver: "lawyer", executor: "bookkeeper", execAt: t0.Add(time.Minute)},
		{name: "preparer authorizes", cleared: 10000, amount: 4000, approver: "clerk", wantAuth: withdraw.ErrPreparerApproves},
		{name: "authorizer executes", cleared: 10000, amount: 4000, approver: "lawyer", executor: "lawyer", execAt: t0, wantExec: withdraw.ErrNotAuthorized},
		{name: "expired", cleared: 10000, amount: 4000, approver: "lawyer", executor: "bookkeeper", execAt: t0.Add(2 * time.Hour), wantExec: withdraw.ErrNotAuthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New(time.Hour)
			f.SetBalance("ta1", "m1", tt.cleared, 0)
			w, err := f.Request(ctx, req(tt.amount))
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.Authorize(ctx, w.ID, tt.approver, t0)
			if !errors.Is(err, tt.wantAuth) {
				t.Fatalf("Authorize err = %v, want %v", err, tt.wantAuth)
			}
			if err != nil {
				return
			}
			if _, err := f.RecordExecution(ctx, w.ID, tt.executor, tt.execAt); !errors.Is(err, tt.wantExec) {
				t.Fatalf("RecordExecution err = %v, want %v", err, tt.wantExec)
			}
			if tt.wantExec == nil {
				got, _ := f.Available(ctx, "ta1", "m1")
				if got != tt.cleared-tt.amount {
					t.Fatalf("Available = %d, want %d", got, tt.cleared-tt.amount)
				}
			}
		})
	}
}

func TestConcurrentAuthorizationsOverdraw(t *testing.T) {
	ctx := context.Background()
	f := New(time.Hour)
	f.SetBalance("ta1", "m1", 500000, 0)
	a, _ := f.Request(ctx, req(300000))
	b, _ := f.Request(ctx, req(300000))
	if _, err := f.Authorize(ctx, a.ID, "lawyer", t0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Authorize(ctx, b.ID, "lawyer", t0); !errors.Is(err, withdraw.ErrTrustOverdraw) {
		t.Fatalf("second Authorize err = %v, want ErrTrustOverdraw", err)
	}
	if _, err := f.Get(ctx, "nope"); !errors.Is(err, withdraw.ErrNotFound) {
		t.Fatalf("Get err = %v, want ErrNotFound", err)
	}
}
