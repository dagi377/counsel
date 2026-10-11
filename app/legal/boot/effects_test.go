package boot

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal"
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
)

type fakeWriteOffs struct{}

func (fakeWriteOffs) Lock(context.Context, pgx.Tx, string) (credit.WriteOff, error) {
	return credit.WriteOff{}, nil
}
func (fakeWriteOffs) MarkAwaitingSecond(context.Context, pgx.Tx, string, string) error { return nil }
func (fakeWriteOffs) SaveApproved(context.Context, pgx.Tx, credit.WriteOff) error      { return nil }
func (fakeWriteOffs) AwaitingSecond(context.Context, pgx.Tx, string) (string, error)   { return "", nil }

type fakeWriteOffBills struct{}

func (fakeWriteOffBills) WriteOffBill(context.Context, pgx.Tx, string) (credit.WriteOffBill, error) {
	return credit.WriteOffBill{}, nil
}

type fakeBadDebt struct{}

func (fakeBadDebt) PostBadDebt(context.Context, pgx.Tx, string, credit.BadDebtAdjustment) error {
	return nil
}

type fakeOpener struct{}

func (fakeOpener) Open(context.Context, pgx.Tx, string, gate.Subject, json.RawMessage, gate.Principal) (gate.GateID, error) {
	return "g-1", nil
}

func testCreditPorts() CreditPorts {
	return CreditPorts{
		Store:  fakeWriteOffs{},
		Bills:  fakeWriteOffBills{},
		Ledger: fakeBadDebt{},
		Gates:  fakeOpener{},
		Second: credit.SecondApproval{ManagingLawyer: func(string) bool { return false }},
	}
}

func testGatePorts() *GatePorts {
	return &GatePorts{Trust: testTrustPorts(), Billing: testBillingPorts(), Credit: testCreditPorts()}
}

func testEffects(t *testing.T) map[string]registry.EffectFunc {
	t.Helper()
	k := testKernel()
	s, err := buildServices(k, *testGatePorts())
	if err != nil {
		t.Fatalf("buildServices: %v", err)
	}
	fx, err := effects(k, s)
	if err != nil {
		t.Fatalf("effects: %v", err)
	}
	return fx
}

func TestEffectsTable(t *testing.T) {
	fx := testEffects(t)
	want := []string{
		bill.GateBillApproval,
		bill.GateBillVoid,
		credit.GateKindCreditNote,
		credit.GateKindWriteOffSecond,
		signatory.KindChange,
		signatory.KindSigningAuthority,
		withdraw.GateKind,
	}
	slices.Sort(want)
	var got []string
	for slug, fn := range fx {
		if fn == nil {
			t.Errorf("effect for %s is nil", slug)
		}
		got = append(got, slug)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("effects = %v, want %v", got, want)
	}
}

func TestMergeEffectsDuplicateSlug(t *testing.T) {
	tests := []struct {
		name    string
		sources []effectSource
		wantErr bool
		want    []string // slugs, or the names the error must contain
	}{
		{
			name: "disjoint sources merge",
			sources: []effectSource{
				{"a", map[string]registry.EffectFunc{"billing.one": noopEffect}},
				{"b", map[string]registry.EffectFunc{"billing.two": noopEffect}},
			},
			want: []string{"billing.one", "billing.two"},
		},
		{
			name: "one slug from two sources",
			sources: []effectSource{
				{"bill", map[string]registry.EffectFunc{"billing.one": noopEffect}},
				{"credit", map[string]registry.EffectFunc{"billing.two": noopEffect, "billing.one": noopEffect}},
			},
			wantErr: true,
			want:    []string{"billing.one", "bill", "credit"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mergeEffects(tt.sources)
			if tt.wantErr {
				if !errors.Is(err, ErrDuplicateEffect) {
					t.Fatalf("err = %v, want ErrDuplicateEffect", err)
				}
				for _, w := range tt.want {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("err = %q, want it to name %q", err, w)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("mergeEffects: %v", err)
			}
			var slugs []string
			for s := range got {
				slugs = append(slugs, s)
			}
			slices.Sort(slugs)
			if !slices.Equal(slugs, tt.want) {
				t.Fatalf("slugs = %v, want %v", slugs, tt.want)
			}
		})
	}
}

func noopEffect(context.Context, pgx.Tx, registry.Approved) error { return nil }

func TestCreditServicesMissingDependency(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(k *kboot.Kernel, p *CreditPorts)
		want   string
	}{
		{"nil Clock", func(k *kboot.Kernel, _ *CreditPorts) { k.Clock = nil }, "kernel Clock"},
		{"nil Store", func(_ *kboot.Kernel, p *CreditPorts) { p.Store = nil }, "Store"},
		{"nil Bills", func(_ *kboot.Kernel, p *CreditPorts) { p.Bills = nil }, "Bills"},
		{"nil Ledger", func(_ *kboot.Kernel, p *CreditPorts) { p.Ledger = nil }, "Ledger"},
		{"nil Gates", func(_ *kboot.Kernel, p *CreditPorts) { p.Gates = nil }, "Gates"},
		{"nil ManagingLawyer", func(_ *kboot.Kernel, p *CreditPorts) { p.Second.ManagingLawyer = nil }, "Second.ManagingLawyer"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, p := testKernel(), testCreditPorts()
			tt.mutate(k, &p)
			_, err := creditServices(k, p)
			if !errors.Is(err, ErrMissingDependency) {
				t.Fatalf("err = %v, want ErrMissingDependency", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %q, want it to name %q", err, tt.want)
			}
		})
	}
}

// readUnwired reads unwired_gates.txt: one slug per line, # comments and blank lines skipped.
func readUnwired(t *testing.T) []string {
	t.Helper()
	f, err := os.Open("unwired_gates.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// unwiredDrift reports every difference between the listed slugs and the unwired ones: a
// listed slug that is wired now, and an unwired slug that is not listed.
func unwiredDrift(listed, unwired []string) []string {
	var out []string
	for _, s := range listed {
		if !slices.Contains(unwired, s) {
			out = append(out, "wired but listed: "+s)
		}
	}
	for _, s := range unwired {
		if !slices.Contains(listed, s) {
			out = append(out, "unwired but not listed: "+s)
		}
	}
	return out
}

func TestUnwiredDrift(t *testing.T) {
	tests := []struct {
		name    string
		listed  []string
		unwired []string
		want    []string
	}{
		{"equal", []string{"a.x", "b.y"}, []string{"a.x", "b.y"}, nil},
		{"wired slug still listed", []string{"a.x", "b.y"}, []string{"b.y"}, []string{"wired but listed: a.x"}},
		{"unlisted unwired slug", []string{"b.y"}, []string{"a.x", "b.y"}, []string{"unwired but not listed: a.x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := unwiredDrift(tt.listed, tt.unwired); !slices.Equal(got, tt.want) {
				t.Fatalf("drift = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestUnwiredGatesOnlyShrink holds unwired_gates.txt to the kinds boot leaves unwired. Wiring
// a kind fails it until its line is deleted; a new kind with no effect fails it too.
func TestUnwiredGatesOnlyShrink(t *testing.T) {
	listed := readUnwired(t)
	if !slices.IsSorted(listed) || len(slices.Compact(slices.Clone(listed))) != len(listed) {
		t.Fatal("unwired_gates.txt must be sorted with no duplicates")
	}
	w := &wiring{opts: Options{Gates: testGatePorts()}}
	k := testKernel()
	k.Registry = recorderBooter{&registry.Recorder{}}
	if err := w.registerGates(context.Background(), k); err != nil {
		t.Fatalf("registerGates: %v", err)
	}
	if w.catalog == nil {
		t.Fatal("registerGates kept no catalog")
	}
	if _, err := w.catalog.Kind(context.Background(), "conflict.clearance"); err != nil {
		t.Fatalf("catalog has no conflict.clearance (unwired kinds stay readable): %v", err)
	}
	for _, d := range unwiredDrift(listed, w.unwired) {
		t.Error(d)
	}
}

func legalFlagNames(t *testing.T) []string {
	t.Helper()
	s := flags.New()
	if err := legal.DefineFlags(s); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range s.Defs() {
		out = append(out, d.Name)
	}
	return out
}

func TestBootSealsWithGateEffects(t *testing.T) {
	schemas, err := registry.LoadSchemas(os.DirFS("../../../spec/kernel"))
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	all := legalFlagNames(t)
	tests := []struct {
		name      string
		on        []string
		gates     *GatePorts
		wantKinds int // registered app gate kinds
	}{
		{name: "no flags, no gate ports", wantKinds: 0},
		{name: "no flags, gate ports", gates: testGatePorts(), wantKinds: 6},
		{name: "every legal flag, gate ports", on: all, gates: testGatePorts(), wantKinds: 6},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k0 := testKernel()
			b := registry.NewBoot(schemas)
			_, err := kboot.BuildWith(context.Background(), kboot.Deps{Registry: b, DB: k0.DB, Clock: k0.Clock}, tt.on,
				App(Options{Gates: tt.gates}))
			if err != nil {
				t.Fatalf("BuildWith (Seal): %v", err)
			}
			var n int
			for _, c := range b.Method(registry.MethodGateKind) {
				g := c.Args[0].(registry.GateKind)
				if g.Effect == nil {
					t.Errorf("gate kind %s reached the registry with a nil Effect", g.Slug)
				}
				if strings.HasPrefix(g.Slug, "core.") {
					t.Errorf("app registered kernel gate kind %s", g.Slug)
				}
				n++
			}
			if n != tt.wantKinds {
				t.Fatalf("registered %d gate kinds, want %d", n, tt.wantKinds)
			}
		})
	}
}

func TestRegisterGatesErrors(t *testing.T) {
	tests := []struct {
		name    string
		k       *kboot.Kernel
		ports   *GatePorts
		wantErr error
	}{
		{"no registry", &kboot.Kernel{}, testGatePorts(), ErrNoRegistry},
		{"missing port", func() *kboot.Kernel {
			k := testKernel()
			k.Registry = recorderBooter{&registry.Recorder{}}
			return k
		}(), &GatePorts{Trust: testTrustPorts(), Billing: testBillingPorts()}, ErrMissingDependency},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &wiring{opts: Options{Gates: tt.ports}}
			if err := w.registerGates(context.Background(), tt.k); !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

// The catalog must know every slug the effect table wires.
func TestEffectSlugsAreSpecKinds(t *testing.T) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		t.Fatal(err)
	}
	cat := gates.NewCatalog(kinds)
	for slug := range testEffects(t) {
		if _, err := cat.Kind(context.Background(), slug); err != nil {
			t.Errorf("effect slug %s: %v", slug, err)
		}
	}
}
