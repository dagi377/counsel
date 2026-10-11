package boot

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/access"
	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/deadlines"
	"github.com/dagi377/counsel/app/legal/gates"
	"github.com/dagi377/counsel/app/legal/intake"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/platform"
	"github.com/dagi377/counsel/app/legal/work"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/registry"
)

// opsEffects are the dedicated effects of the platform, intake, work and access gate kinds
// (GE2 group c) and of every platform.* kind (GE3), one line per kind. Each runs after ApprovalEffect, which releases a drafted
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
	return map[string]registry.EffectFunc{
		intake.GateLeadDecision:     rel(intake.LeadDecisionEffect(now)),
		intake.GatePublicWording:    rel(intake.PublicWordingEffect(now)),
		work.GateWorkOutput:         rel(work.OutputEffect(now)),
		work.GateAutomation:         rel(work.AutomationEffect()),
		matter.GateClosing:          rel(matter.ClosingEffect(now, loc)),
		access.KindOffboarding:      rel(access.OffboardingEffect(now, users)),
		matter.KindMatterVisibility: rel(matter.MatterVisibilityEffect(now)),

		platform.GatePortalMessage:     rel(platform.PortalMessageEffect(now)),
		platform.GateOutgoingEmail:     rel(platform.OutgoingEmailEffect(now)),
		platform.GateSummaryAccuracy:   rel(platform.SummaryAccuracyEffect(now)),
		platform.GateFileDestruction:   rel(platform.FileDestructionEffect(now)),
		agent.GateSkillAutonomy:        rel(agent.SkillAutonomyEffect(now)),
		access.KindAccessChange:        rel(access.AccessChangeEffect(now)),
		platform.GateIntegrationScope:  rel(platform.IntegrationScopeEffect(now)),
		platform.GateExport:            rel(platform.ExportEffect(now)),
		comms.GateGuestAccess:          rel(comms.GuestAccessEffect(now)),
		platform.GateResearchMemo:      rel(platform.ResearchMemoEffect(now)),
		platform.GateSecurityAnswer:    rel(platform.SecurityAnswerEffect(now)),
		platform.GateOutboundConnector: rel(platform.OutboundConnectorEffect(now)),
		platform.GatePaymentProvider:   rel(platform.PaymentProviderEffect(now)),
		platform.GateImportBalances:    rel(platform.ImportBalancesEffect(now)),
		deadlines.GateRulePack:         rel(deadlines.RulePackEffect(now)),
		platform.GateVendorSupport:     rel(platform.VendorSupportEffect(now)),
		platform.GateEscrowDestination: rel(platform.EscrowDestinationEffect(now)),
		platform.GateServiceCredit:     rel(platform.ServiceCreditEffect(now)),
		platform.GatePublishedPrice:    rel(platform.PublishedPriceEffect(now)),
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
		platform.GatePortalMessage, platform.GateOutgoingEmail, platform.GateSummaryAccuracy,
		platform.GateFileDestruction, agent.GateSkillAutonomy, access.KindAccessChange,
		platform.GateIntegrationScope, platform.GateExport, comms.GateGuestAccess,
		platform.GateResearchMemo, platform.GateSecurityAnswer, platform.GateOutboundConnector,
		platform.GatePaymentProvider, platform.GateImportBalances, deadlines.GateRulePack,
		platform.GateVendorSupport, platform.GateEscrowDestination, platform.GateServiceCredit,
		platform.GatePublishedPrice,
	}))
}
