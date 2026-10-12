package boot

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/work"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// opsEffects are the dedicated effects of the platform, intake, work and access gate kinds
// (GE2 group c), one line per kind. Each runs after ApprovalEffect, which releases a drafted
// subject (the tool-gap kinds' document draft) and is a no-op on any other subject.
func opsEffects(k *kboot.Kernel) (map[string]registry.EffectFunc, error) {
	if k == nil {
		return nil, fmt.Errorf("ops effects: kernel: %w", ErrMissingDependency)
	}
	loc, err := time.LoadLocation("America/Toronto")
	if err != nil {
		return nil, fmt.Errorf("ops effects: zone: %w", err)
	}
	// The clock is read when an effect runs; withRelease fails closed while it is unset.
	now := func() time.Time { return k.Clock.Now() }
	users := identity.UserEvents{Emitter: k.Emitter}
	rel := func(fx registry.EffectFunc) registry.EffectFunc { return withRelease(k, fx) }
	wallMin, err := specMinApprovers(matter.KindWallLift)
	if err != nil {
		return nil, err
	}
	// intake.open_matter opens the matter through the matter.opening effect, built when the
	// effect runs so it sees the kernel's DB, emitter and clock (it fails closed without them).
	opening := func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		m := matter.NewStore(k.DB, nil)
		m.Events = k.Emitter
		return m.OpeningEffect(k.Clock)(ctx, tx, g)
	}
	return map[string]registry.EffectFunc{
		intake.GateLeadDecision:     rel(intake.LeadDecisionEffect(now)),
		intake.GateConsultOutcome:   rel(intake.ConsultOutcomeEffect(now)),
		intake.GateFeeWaiver:        rel(intake.FeeWaiverEffect(now)),
		intake.GateOpenMatter:       rel(intake.OpenMatterEffect(now, opening)),
		work.GateWorkPlan:           rel(work.PlanEffect(now)),
		work.GateWorkflowPublish:    rel(work.WorkflowPublishEffect(now)),
		work.GateExternalSync:       rel(work.ExternalSyncEffect(now)),
		work.GateAbsence:            rel(work.AbsenceEffect(now)),
		matter.KindWallLift:         rel(matter.WallLiftEffect(now, wallMin)),
		intake.GatePublicWording:    rel(intake.PublicWordingEffect(now)),
		work.GateWorkOutput:         rel(work.OutputEffect(now)),
		work.GateAutomation:         rel(work.AutomationEffect()),
		matter.GateClosing:          rel(matter.ClosingEffect(now, loc)),
		access.KindOffboarding:      rel(access.OffboardingEffect(now, users, offboardingHandover)),
		matter.KindMatterVisibility: rel(matter.MatterVisibilityEffect(now)),
	}, nil
}

// withRelease runs ApprovalEffect (release the drafted subject) and then the kind's effect,
// both in the gate's tx. With no kernel clock it fails closed.
func withRelease(k *kboot.Kernel, fx registry.EffectFunc) registry.EffectFunc {
	return func(ctx context.Context, tx pgx.Tx, g registry.Approved) error {
		if k.Clock == nil {
			return fmt.Errorf("approve %s (gate %s): kernel Clock: %w", g.Kind, g.GateID, ErrMissingDependency)
		}
		if err := ApprovalEffect(ctx, tx, g); err != nil {
			return err
		}
		return fx(ctx, tx, g)
	}
}

// withOpsEffects adds opsEffects to fx; a kind fx already handles is an ErrDuplicateEffect.
func withOpsEffects(k *kboot.Kernel, fx map[string]registry.EffectFunc) (map[string]registry.EffectFunc, error) {
	ops, err := opsEffects(k)
	if err != nil {
		return nil, err
	}
	return mergeEffects([]effectSource{{"app", fx}, {"ops", ops}})
}

// registerCoreKinds hands the kernel every core.* spec kind fx has an effect for; the kernel
// registers them with the gate engine (KERNEL-CORE).
func registerCoreKinds(k *kboot.Kernel, fx map[string]registry.EffectFunc) error {
	kinds, err := gates.CoreKinds(gates.SpecData(), fx)
	if err != nil {
		return err
	}
	for _, g := range kinds {
		if err := k.CoreGateKind(g); err != nil {
			return err
		}
	}
	return nil
}

// OpsKinds are the kinds opsEffects handles, sorted.
func OpsKinds() []string {
	return slices.Sorted(slices.Values([]string{
		intake.GateLeadDecision, intake.GatePublicWording, work.GateWorkOutput, work.GateAutomation,
		matter.GateClosing, access.KindOffboarding, matter.KindMatterVisibility,
		intake.GateConsultOutcome, intake.GateFeeWaiver, intake.GateOpenMatter, work.GateWorkPlan,
		work.GateWorkflowPublish, work.GateExternalSync, work.GateAbsence, matter.KindWallLift,
	}))
}

// offboardingHandover moves the leaver's live matters and open work items to the hand-over
// user; each module writes only its own tables.
func offboardingHandover(ctx context.Context, tx gates.Execer, from, to string, at time.Time) error {
	if err := matter.ReassignResponsible(ctx, tx, from, to, at); err != nil {
		return err
	}
	return work.ReassignOpen(ctx, tx, from, to, at)
}

// specMinApprovers is the spec's min_approvers for kind.
func specMinApprovers(kind string) (int, error) {
	kinds, err := gates.ParseKinds(gates.SpecData())
	if err != nil {
		return 0, fmt.Errorf("ops effects: %w", err)
	}
	for _, g := range kinds {
		if g.Code == kind {
			return max(g.MinApprovers, 1), nil
		}
	}
	return 0, fmt.Errorf("ops effects: kind %s not in the spec: %w", kind, ErrMissingDependency)
}
