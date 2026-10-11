package reconfake

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/recon"
)

var sep = recon.Period{Year: 2026, Month: time.September}

func statement() recon.Statement {
	return recon.Statement{AccountID: "acct1", Period: sep, OpeningCents: 100000, ClosingCents: 150000,
		Lines: []recon.StatementLine{{ID: "l1", AmountCents: 70000}, {ID: "l2", AmountCents: -20000}}}
}

func TestImportStatement(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		setup   func(f *Fake)
		mutate  func(s *recon.Statement)
		wantErr error
	}{
		{name: "ok"},
		{name: "lines do not sum", mutate: func(s *recon.Statement) { s.ClosingCents++ }, wantErr: recon.ErrStatementMismatch},
		{name: "duplicate", setup: func(f *Fake) { _, _ = f.ImportStatement(ctx, statement()) }, wantErr: recon.ErrDuplicateStatement},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			if tt.setup != nil {
				tt.setup(f)
			}
			s := statement()
			if tt.mutate != nil {
				tt.mutate(&s)
			}
			if _, err := f.ImportStatement(ctx, s); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestPrepareApprove(t *testing.T) {
	ctx := context.Background()
	cheque := []recon.ReconcilingItem{{Kind: recon.OutstandingCheque, Reference: "chq 12", AmountCents: -5000}}
	tests := []struct {
		name       string
		book       int64
		clients    int64
		items      []recon.ReconcilingItem
		by         recon.Approver
		wantErr    error
		wantLocked bool
	}{
		{name: "balanced, other user approves", book: 150000, clients: 150000, by: recon.Approver{UserID: "u2"}, wantLocked: true},
		{name: "balanced with outstanding cheque", book: 145000, clients: 145000, items: cheque, by: recon.Approver{UserID: "u2"}, wantLocked: true},
		{name: "book differs", book: 149999, clients: 149999, by: recon.Approver{UserID: "u2"}, wantErr: recon.ErrUnbalanced},
		{name: "client ledgers differ", book: 150000, clients: 149000, by: recon.Approver{UserID: "u2"}, wantErr: recon.ErrUnbalanced},
		{name: "preparer approves", book: 150000, clients: 150000, by: recon.Approver{UserID: "u1"}, wantErr: recon.ErrSelfApproval},
		{name: "agent approves", book: 150000, clients: 150000, by: recon.Approver{UserID: "u2", IsAgent: true}, wantErr: recon.ErrAgentApproval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := New()
			if _, err := f.ImportStatement(ctx, statement()); err != nil {
				t.Fatal(err)
			}
			f.SetBook("acct1", sep, tt.book, tt.clients)
			r, err := f.Prepare(ctx, "acct1", sep, tt.items, "u1")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Approve(ctx, r.ID, tt.by); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			locked, err := f.Locked(ctx, "acct1", sep)
			if err != nil || locked != tt.wantLocked {
				t.Fatalf("Locked = %v, %v; want %v", locked, err, tt.wantLocked)
			}
			if tt.wantLocked {
				if _, err := f.Prepare(ctx, "acct1", sep, nil, "u1"); !errors.Is(err, recon.ErrPeriodLocked) {
					t.Fatalf("prepare after lock err = %v", err)
				}
				if err := f.Approve(ctx, r.ID, tt.by); !errors.Is(err, recon.ErrInvalidState) {
					t.Fatalf("second approve err = %v", err)
				}
			}
		})
	}
}

func TestNotFound(t *testing.T) {
	ctx := context.Background()
	f := New()
	tests := []struct {
		name string
		op   func() error
	}{
		{name: "get", op: func() error { _, err := f.Get(ctx, "x"); return err }},
		{name: "approve", op: func() error { return f.Approve(ctx, "x", recon.Approver{UserID: "u2"}) }},
		{name: "prepare without statement", op: func() error { _, err := f.Prepare(ctx, "acct1", sep, nil, "u1"); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.op(); !errors.Is(err, recon.ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
}
