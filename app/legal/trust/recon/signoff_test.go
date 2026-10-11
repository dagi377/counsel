package recon_test

// This test integrates sign-off with the posting-date check through the in-memory service,
// so it needs no Postgres and carries no integration build tag. The database side of the
// same lock (trigger DM-054 on trust.trust_transaction) is covered by the migration.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/recon/reconfake"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/flags"
)

func bylaw9Ctx(t *testing.T, on bool) context.Context {
	t.Helper()
	s := flags.New()
	if err := s.Define(flags.Def{Name: recon.FlagBylaw9Verified}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(recon.FlagBylaw9Verified, on); err != nil {
		t.Fatal(err)
	}
	return flags.WithSet(context.Background(), s)
}

// prepared returns a fake with a balanced, prepared March 2026 reconciliation for acct-1.
func prepared(t *testing.T, ctx context.Context) (*reconfake.Fake, string) {
	t.Helper()
	f := reconfake.New()
	march := recon.Period{Year: 2026, Month: time.March}
	if _, err := f.ImportStatement(ctx, recon.Statement{AccountID: "acct-1", Period: march, OpeningCents: 0, ClosingCents: 100000,
		Lines: []recon.StatementLine{{ID: "l1", AmountCents: 100000}}}); err != nil {
		t.Fatal(err)
	}
	f.SetBook("acct-1", march, 100000, 100000)
	r, err := f.Prepare(ctx, "acct-1", march, nil, "preparer")
	if err != nil {
		t.Fatal(err)
	}
	return f, r.ID
}

// rule: BIL-I-025
func TestBIL_I_025_SignOffLocksMonth(t *testing.T) {
	d := func(m time.Month, day int) clock.Date { return clock.Date{Year: 2026, Month: m, Day: day} }
	tests := []struct {
		name     string
		flagOn   bool
		signer   recon.Approver
		account  string
		date     clock.Date
		wantSign error
		wantPost error
	}{
		{"signed month refuses posting dated in it", true, recon.Approver{UserID: "approver"}, "acct-1", d(time.March, 15), nil, recon.ErrPeriodLocked},
		{"signed month refuses posting on its last day", true, recon.Approver{UserID: "approver"}, "acct-1", d(time.March, 31), nil, recon.ErrPeriodLocked},
		{"correction posts in the current month", true, recon.Approver{UserID: "approver"}, "acct-1", d(time.April, 2), nil, nil},
		{"other account is not locked", true, recon.Approver{UserID: "approver"}, "acct-2", d(time.March, 15), nil, nil},
		{"preparer cannot sign, month stays open", true, recon.Approver{UserID: "preparer"}, "acct-1", d(time.March, 15), recon.ErrSelfApproval, nil},
		{"agent cannot sign, month stays open", true, recon.Approver{UserID: "approver", IsAgent: true}, "acct-1", d(time.March, 15), recon.ErrAgentApproval, nil},
		{"flag off fails closed", false, recon.Approver{UserID: "approver"}, "acct-1", d(time.March, 15), recon.ErrUnverified, recon.ErrUnverified},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := bylaw9Ctx(t, tt.flagOn)
			f, id := prepared(t, ctx)
			if err := recon.SignOff(ctx, f, id, tt.signer); !errors.Is(err, tt.wantSign) {
				t.Fatalf("sign off: %v, want %v", err, tt.wantSign)
			}
			if err := recon.CheckPostingDate(ctx, f, tt.account, tt.date); !errors.Is(err, tt.wantPost) {
				t.Fatalf("posting %s: %v, want %v", tt.date, err, tt.wantPost)
			}
		})
	}
}

// rule: BIL-I-025
func TestBIL_I_025_PeriodOf(t *testing.T) {
	tests := []struct {
		d    clock.Date
		want recon.Period
	}{
		{clock.Date{Year: 2026, Month: time.March, Day: 1}, recon.Period{Year: 2026, Month: time.March}},
		{clock.Date{Year: 2026, Month: time.December, Day: 31}, recon.Period{Year: 2026, Month: time.December}},
	}
	for _, tt := range tests {
		if got := recon.PeriodOf(tt.d); got != tt.want {
			t.Fatalf("PeriodOf(%s) = %v, want %v", tt.d, got, tt.want)
		}
	}
}
