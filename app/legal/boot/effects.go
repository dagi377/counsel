package boot

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// ErrDuplicateEffect is returned when two effect sources handle the same gate kind. The error
// text names the slug and both sources.
var ErrDuplicateEffect = errors.New("legal boot: two effects for one gate kind")

// GatePorts are the dependencies of the gate effect services. With Options.Gates nil no
// effect is built, so every app gate kind stays unwired and none is registered.
type GatePorts struct {
	Trust   TrustPorts
	Billing BillingPorts
	Credit  CreditPorts
}

// CreditPorts are the write-off effects' dependencies. No production implementation of the
// stores exists yet (Q-BOOT-13-PORTS), so the composition root takes them from its caller.
type CreditPorts struct {
	Calc   credit.Calc                 // credit arithmetic; the zero value uses money.Default
	Config credit.SecondApprovalConfig // the firm's write-off second-approval threshold
	Store  credit.WriteOffStore
	Bills  credit.WriteOffBills
	Ledger credit.BadDebtPoster
	Gates  credit.GateOpener // opens the managing lawyer's second-approval gate
	Second credit.SecondApproval
	// Notes, when set, is the credit-note effect. billing.credit_note carries both credit
	// notes and write-offs, so boot joins the two with credit.MergeEffects (PORT-17).
	Notes *credit.NoteEffect
}

// creditSvcs are the write-off services built at boot.
type creditSvcs struct {
	WriteOff *credit.WriteOffEffect
	Second   credit.SecondApproval
	Notes    *credit.NoteEffect
}

// creditServices builds the G-B5 write-off effect and its second approval.
func creditServices(k *kboot.Kernel, p CreditPorts) (creditSvcs, error) {
	if k == nil {
		return creditSvcs{}, fmt.Errorf("credit services: kernel: %w", ErrMissingDependency)
	}
	for _, d := range []struct {
		name string
		nil  bool
	}{
		{"kernel Clock", k.Clock == nil},
		{"Store", p.Store == nil},
		{"Bills", p.Bills == nil},
		{"Ledger", p.Ledger == nil},
		{"Gates", p.Gates == nil},
		// A nil ManagingLawyer fails every second approval closed; boot refuses it so a
		// missing role lookup is a boot error, not a gate that can never be approved.
		{"Second.ManagingLawyer", p.Second.ManagingLawyer == nil},
	} {
		if d.nil {
			return creditSvcs{}, fmt.Errorf("credit services: %s: %w", d.name, ErrMissingDependency)
		}
	}
	return creditSvcs{
		WriteOff: &credit.WriteOffEffect{
			Calc:   p.Calc,
			Config: p.Config,
			Store:  p.Store,
			Bills:  p.Bills,
			Ledger: p.Ledger,
			Gates:  p.Gates,
			Clock:  k.Clock,
		},
		Second: p.Second,
		Notes:  p.Notes,
	}, nil
}

// svcs are the services whose gate effects boot registers.
type svcs struct {
	trust   trustSvcs
	billing billingSvcs
	credit  creditSvcs
}

// buildServices builds every service that owns a gate effect.
func buildServices(k *kboot.Kernel, p GatePorts) (svcs, error) {
	t, err := trustServices(k, p.Trust)
	if err != nil {
		return svcs{}, err
	}
	b, err := billingServices(k, p.Billing)
	if err != nil {
		return svcs{}, err
	}
	c, err := creditServices(k, p.Credit)
	if err != nil {
		return svcs{}, err
	}
	return svcs{trust: t, billing: b, credit: c}, nil
}

// effectSource is one feature package's effect table.
type effectSource struct {
	name    string
	effects map[string]registry.EffectFunc
}

// effects is the gate effect table of s: the signatory, trust withdrawal, bill approval and
// void, and write-off effects, keyed by gate kind slug. The core.trust_signatory effect comes
// from the kernel's identity admin gates, built with CoreGateEffects on k.
func effects(k *kboot.Kernel, s svcs) (map[string]registry.EffectFunc, error) {
	ag, err := k.AdminGates(CoreGateEffects(s.trust.Signatories)...)
	if err != nil {
		return nil, err
	}
	fx, err := creditEffects(s.credit)
	if err != nil {
		return nil, err
	}
	return mergeEffects([]effectSource{
		{"core", map[string]registry.EffectFunc{signatory.KindChange: ag.Effects()[signatory.KindChange]}},
		{"signatory alias", map[string]registry.EffectFunc{
			signatory.KindSigningAuthority: signatoryEffects(s.trust.Signatories)[signatory.KindSigningAuthority],
		}},
		{"withdraw", map[string]registry.EffectFunc{withdraw.GateKind: s.trust.Withdraw.Apply}},
		{"withdraw fee", map[string]registry.EffectFunc{withdraw.GateFeeKind: withdraw.FeeEffect(s.trust.Withdraw)}},
		{"bill", bill.Effects(s.billing.Approve)},
		{"bill void", bill.VoidEffects(s.billing.Void)},
		{"credit", fx},
	})
}

// creditEffects is the write-off effect table, joined with the credit-note effect when one is
// built: both handle billing.credit_note, dispatched on the payload kind (PORT-17).
func creditEffects(c creditSvcs) (map[string]registry.EffectFunc, error) {
	wo := credit.Effects(c.WriteOff, c.Second)
	if c.Notes == nil {
		return wo, nil
	}
	return credit.MergeEffects(credit.NoteEffects(c.Notes), wo)
}

// mergeEffects merges the sources into one table. A slug handled by two sources is an error
// naming it, so one effect never silently replaces another.
func mergeEffects(sources []effectSource) (map[string]registry.EffectFunc, error) {
	out := map[string]registry.EffectFunc{}
	from := map[string]string{}
	for _, src := range sources {
		slugs := make([]string, 0, len(src.effects))
		for slug := range src.effects {
			slugs = append(slugs, slug)
		}
		slices.Sort(slugs)
		for _, slug := range slugs {
			if prev, dup := from[slug]; dup {
				return nil, fmt.Errorf("gate kind %q from %s and %s: %w", slug, prev, src.name, ErrDuplicateEffect)
			}
			from[slug] = src.name
			out[slug] = src.effects[slug]
		}
	}
	return out, nil
}

// wiring is the state boot steps share: Register fills it, later steps read it.
type wiring struct {
	opts Options
	// catalog holds every spec gate kind, wired or not. Clearance checks G2 decisions on it.
	catalog *gates.Catalog
	// unwired are the app gate kinds with no effect, sorted; they are not registered.
	unwired []string
	// checks runs conflict checks; clearance is the only conflict.Clearer and decides G2 on
	// those checks' runs. Both are built when Options.Clearance is set.
	checks    *conflict.Checks
	clearance *conflict.Clearance
	// rest holds the services registerRest built.
	rest *RestServices
	// roles is the capability catalog Production built empty; loadRoles fills it from the
	// app's registrations once every Register step ran.
	roles *identity.Roles
}

// registerGates registers the gate kinds that have an effect (gates.RegisterWired), so no
// kind reaches the registry with a nil Effect, and keeps the catalog of every kind.
func (w *wiring) registerGates(_ context.Context, k *kboot.Kernel) error {
	if k.Registry == nil {
		return ErrNoRegistry
	}
	var fx map[string]registry.EffectFunc
	if w.opts.Gates != nil {
		s, err := buildServices(k, *w.opts.Gates)
		if err != nil {
			return fmt.Errorf("gate effects: %w", err)
		}
		if fx, err = effects(k, s); err != nil {
			return fmt.Errorf("gate effects: %w", err)
		}
	}
	fx, err := withRestEffects(k, w.opts.Rest, fx)
	if err != nil {
		return fmt.Errorf("gate effects: %w", err)
	}
	if fx, err = withLegalEffects(fx); err != nil {
		return fmt.Errorf("gate effects: %w", err)
	}
	// GATE-EFFECTS: every remaining app kind gets the approval effect, so no spec or feature
	// kind is left unregistered ("unknown kind" in production).
	if fx, err = withApprovalEffects(fx); err != nil {
		return fmt.Errorf("gate effects: %w", err)
	}
	var reg registry.Registry = k.Registry
	if w.opts.Gates != nil {
		// WO-FIX: a rejected G-B5 write-off gate closes its draft as rejected.
		reg = rejectFilter{Registry: reg, hooks: credit.RejectHooks(credit.PGWriteOffs{})}
	}
	cat, unwired, err := gates.RegisterWired(reg, gates.SpecData(), fx)
	if err != nil {
		return err
	}
	w.catalog, w.unwired = cat, unwired
	return nil
}
