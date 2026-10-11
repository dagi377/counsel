package boot

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/signatory/signatoryfake"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/clock/clockfake"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
)

type fakeSealer struct{}

func (fakeSealer) Seal(_ context.Context, b []byte) ([]byte, error) { return b, nil }

type fakeSigners struct{}

func (fakeSigners) IsSignatory(context.Context, pgx.Tx, trust.AccountID, string) (bool, error) {
	return true, nil
}

type fakeFirm struct{}

func (fakeFirm) SolePractitioner(context.Context) (bool, error) { return false, nil }

type fakeAudit struct{}

func (fakeAudit) Record(context.Context, pgx.Tx, string, string, string) error { return nil }

func testKernel() *kboot.Kernel {
	loc, _ := time.LoadLocation("America/Toronto")
	return &kboot.Kernel{DB: dbfake.New(), Clock: clockfake.New(time.Date(2026, 10, 11, 9, 0, 0, 0, loc), loc)}
}

func testTrustPorts() TrustPorts {
	sigs := signatoryfake.New()
	return TrustPorts{
		GL:          ledgerfake.New(clock.Date{Year: 2026, Month: 10, Day: 11}),
		Users:       sigs,
		Seal:        fakeSealer{},
		Signatories: sigs,
		Signers:     fakeSigners{},
		Firm:        fakeFirm{},
		Audit:       fakeAudit{},
	}
}

func TestTrustServicesBuildsEveryField(t *testing.T) {
	s, err := trustServices(testKernel(), testTrustPorts())
	if err != nil {
		t.Fatalf("trustServices: %v", err)
	}
	tests := []struct {
		name string
		ok   bool
	}{
		{"Accounts", s.Accounts != nil},
		{"Ledgers", s.Ledgers != nil},
		{"Signatories", s.Signatories != nil},
		{"Withdraw", s.Withdraw != nil},
		{"Withdraw.Store", s.Withdraw != nil && s.Withdraw.Store != nil},
		{"Withdraw.Signers", s.Withdraw != nil && s.Withdraw.Signers != nil},
		{"Withdraw.Firm", s.Withdraw != nil && s.Withdraw.Firm != nil},
		{"Withdraw.Audit", s.Withdraw != nil && s.Withdraw.Audit != nil},
		{"Withdraw.Now", s.Withdraw != nil && s.Withdraw.Now != nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("%s is nil", tt.name)
			}
		})
	}
	got, ok := s.Withdraw.Ledgers.(*trust.LedgerStore)
	if !ok || got != s.Ledgers {
		t.Fatalf("withdraw Effect.Ledgers = %T %p, want the trust LedgerStore %p", s.Withdraw.Ledgers, got, s.Ledgers)
	}
}

func TestTrustServicesMissingDependency(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(k *kboot.Kernel, p *TrustPorts)
		want   string
	}{
		{"nil DB", func(k *kboot.Kernel, _ *TrustPorts) { k.DB = nil }, "kernel DB"},
		{"nil Clock", func(k *kboot.Kernel, _ *TrustPorts) { k.Clock = nil }, "kernel Clock"},
		{"nil GL", func(_ *kboot.Kernel, p *TrustPorts) { p.GL = nil }, "GL"},
		{"nil Users", func(_ *kboot.Kernel, p *TrustPorts) { p.Users = nil }, "Users"},
		{"nil Seal", func(_ *kboot.Kernel, p *TrustPorts) { p.Seal = nil }, "Seal"},
		{"nil Signatories", func(_ *kboot.Kernel, p *TrustPorts) { p.Signatories = nil }, "Signatories"},
		{"nil Signers", func(_ *kboot.Kernel, p *TrustPorts) { p.Signers = nil }, "Signers"},
		{"nil Firm", func(_ *kboot.Kernel, p *TrustPorts) { p.Firm = nil }, "Firm"},
		{"nil Audit", func(_ *kboot.Kernel, p *TrustPorts) { p.Audit = nil }, "Audit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, p := testKernel(), testTrustPorts()
			tt.mutate(k, &p)
			_, err := trustServices(k, p)
			if !errors.Is(err, ErrMissingDependency) {
				t.Fatalf("err = %v, want ErrMissingDependency", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %q, want it to name %q", err, tt.want)
			}
		})
	}
}
