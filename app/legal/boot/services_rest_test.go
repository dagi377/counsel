package boot

import (
	"context"
	"errors"
	"github.com/dagi377/counsel/app/legal/matter"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/ledger/ledgerfake"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
)

// readFake is dbfake with the plain queries db.Pool has; they are never reached at boot.
type readFake struct{ *dbfake.Fake }

var errNoRead = errors.New("readFake: no plain reads")

func (readFake) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, errNoRead }
func (readFake) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }

type fakePoster struct{}

func (fakePoster) Receive(context.Context, pgx.Tx, trust.Receipt) (trust.TxnID, error) {
	return "", nil
}

type fakeRefunds struct{}

func (fakeRefunds) LockRefundable(context.Context, pgx.Tx, string) (payment.Refundable, error) {
	return payment.Refundable{}, nil
}
func (fakeRefunds) RefundByApproval(context.Context, pgx.Tx, string) (string, error) { return "", nil }
func (fakeRefunds) RecordRefund(context.Context, pgx.Tx, payment.RefundRecord) (string, error) {
	return "", nil
}
func (fakeRefunds) Funds(context.Context, pgx.Tx, string, string) (withdraw.Funds, error) {
	return withdraw.Funds{}, nil
}
func (fakeRefunds) Request(context.Context, pgx.Tx, withdraw.Request) (string, error) { return "", nil }

type fakeLeadStores struct{}

func (fakeLeadStores) Form(context.Context, string) (intake.Form, error) { return intake.Form{}, nil }
func (fakeLeadStores) ByIdempotencyKey(context.Context, string, string) (intake.Lead, error) {
	return intake.Lead{}, intake.ErrNotFound
}
func (fakeLeadStores) Insert(_ context.Context, l intake.Lead, _ string) (intake.Lead, error) {
	return l, nil
}

func testRestPorts() *RestPorts {
	return &RestPorts{
		Leads:    &LeadPorts{Forms: fakeLeadStores{}, Leads: fakeLeadStores{}},
		Receipts: &ReceiptPorts{Poster: fakePoster{}},
		Refunds: &RefundPorts{Store: fakeRefunds{}, Trust: fakeRefunds{},
			Ledger: ledgerfake.New(clock.Date{Year: 2026, Month: 10, Day: 11})},
	}
}

func restKernel() *kboot.Kernel {
	k := testKernel()
	k.DB = readFake{dbfake.New()}
	k.Emitter = outboxfake.New()
	k.Jobs = kboot.NewJobs()
	return k
}

func TestRegisterRestWiresEveryPiece(t *testing.T) {
	w := &wiring{opts: Options{Rest: testRestPorts()}}
	k := restKernel()
	if err := w.registerRest(context.Background(), k); err != nil {
		t.Fatalf("registerRest: %v", err)
	}
	s := w.Wired()
	if s == nil {
		t.Fatal("registerRest kept no services")
	}
	tests := []struct {
		name string
		ok   bool
	}{
		{"break-glass grants", s.BreakGlass != nil},
		{"sign-off", s.SignOff != nil},
		{"portal sessions", s.Sessions != nil},
		{"deadline book emits", s.Book != nil && s.Book.Events != nil},
		{"filing store emits", s.Filings != nil && s.Filings.Events != nil},
		{"sole sync job", len(k.Jobs.Periodic()) == 1},
		{"intake leads publish", s.Leads != nil && s.Leads.Events != nil},
		{"matter store emits", s.Events.Matters != nil && s.Events.Matters.Events != nil},
		{"document write points emit", s.Events.Revisions != nil && s.Events.Revisions.Events != nil &&
			s.Events.CourtForms != nil && s.Events.CourtForms.Events != nil},
		{"inbound email emits", s.Events.Inbound != nil && s.Events.Inbound.Events != nil},
		{"calendar emits", s.Events.Calendar != nil && s.Events.Calendar.Events != nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("%s not wired", tt.name)
			}
		})
	}
}

func TestRegisterRestNilBuildsNothing(t *testing.T) {
	w := &wiring{}
	if err := w.registerRest(context.Background(), &kboot.Kernel{}); err != nil || w.Wired() != nil {
		t.Fatalf("registerRest(nil Rest) = %v, %v; want nil, nothing built", err, w.Wired())
	}
}

func TestRestServicesMissingDependency(t *testing.T) {
	tests := []struct {
		name string
		edit func(k *kboot.Kernel)
	}{
		{"no DB", func(k *kboot.Kernel) { k.DB = nil }},
		{"DB without plain reads", func(k *kboot.Kernel) { k.DB = dbfake.New() }},
		{"no clock", func(k *kboot.Kernel) { k.Clock = nil }},
		{"no emitter", func(k *kboot.Kernel) { k.Emitter = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k := restKernel()
			tt.edit(k)
			_, err := restServices(k)
			if !errors.Is(err, ErrJobDependency) && !errors.Is(err, ErrMissingDependency) {
				t.Fatalf("restServices = %v, want a missing-dependency error", err)
			}
		})
	}
}

func TestRestEffectsMissingPorts(t *testing.T) {
	tests := []struct {
		name string
		p    *RestPorts
		ok   bool
	}{
		{"nil ports", nil, true},
		{"receipts without poster", &RestPorts{Receipts: &ReceiptPorts{}}, false},
		// rule: BIL-C-064
		// A nil Store defaults to payment.PGRefunds; a missing ledger still fails closed.
		{"refunds without store", &RestPorts{Refunds: &RefundPorts{Trust: fakeRefunds{}, Ledger: ledgerfake.New(clock.Date{Year: 2026, Month: 10, Day: 11})}}, true},
		{"refunds without ledger", &RestPorts{Refunds: &RefundPorts{Trust: fakeRefunds{}}}, false},
		{"all ports", testRestPorts(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := restEffects(testKernel(), tt.p)
			if (err == nil) != tt.ok {
				t.Fatalf("restEffects err = %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

// TestEveryGateKindWithEffectResolves registers the gates with every effect port set: each
// registered kind has an effect, and the receipt classification and refund kinds are wired.
func TestEveryGateKindWithEffectResolves(t *testing.T) {
	rec := &registry.Recorder{}
	k := testKernel()
	k.Registry = recorderBooter{rec}
	w := &wiring{opts: Options{Gates: testGatePorts(), Rest: testRestPorts()}}
	if err := w.registerGates(context.Background(), k); err != nil {
		t.Fatalf("registerGates: %v", err)
	}
	got := map[string]bool{}
	for _, c := range rec.Calls() {
		if c.Method != registry.MethodGateKind {
			continue
		}
		g := c.Args[0].(registry.GateKind)
		if g.Effect == nil {
			t.Errorf("gate kind %s registered with no effect", g.Slug)
		}
		got[g.Slug] = true
	}
	for _, slug := range []string{trust.GateReceiptClassification, payment.GateKindRefund} {
		t.Run(slug, func(t *testing.T) {
			if !got[slug] {
				t.Fatalf("%s not registered", slug)
			}
		})
	}
}

// rule: ARC-I-044
func TestAuditMaskerKeepsGrantID(t *testing.T) {
	const id = "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"
	got := AuditMasker().Mask(map[string]string{"grant_id": id, "user_id": id, "reason": "Client Jane Doe"})
	tests := []struct{ key, want string }{
		{"grant_id", id},
		{"user_id", id},
		{"reason", audit.Masked},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got[tt.key] != tt.want {
				t.Fatalf("%s = %q, want %q", tt.key, got[tt.key], tt.want)
			}
		})
	}
}

func TestStartSyncsSoleOnlyWithRest(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want int
	}{
		{"no rest", Options{}, 1},
		{"rest", Options{Rest: &RestPorts{}}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(startHooks(tt.opts)); got != tt.want {
				t.Fatalf("start hooks = %d, want %d", got, tt.want)
			}
		})
	}
}

// intake.Leads is built on the Postgres stores when no override is given, and the
// matter.opening effect is wired with the rest services.
func TestRegisterRestLeadsAndOpeningUnconditional(t *testing.T) {
	w := &wiring{opts: Options{Rest: &RestPorts{}}}
	k := restKernel()
	if err := w.registerRest(context.Background(), k); err != nil {
		t.Fatalf("registerRest: %v", err)
	}
	fx, err := restEffects(k, &RestPorts{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		ok   bool
	}{
		{"leads without ports", w.Wired().Leads != nil && w.Wired().Leads.Events != nil},
		{"matter.opening effect", fx[matter.GateOpening] != nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.ok {
				t.Fatalf("%s not wired", tt.name)
			}
		})
	}
}
