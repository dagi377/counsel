package boot

import (
	"github.com/dagi377/counsel/app/legal/billing/bill"
	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/billing/settings"
	"github.com/dagi377/counsel/app/legal/books"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/registry"
)

// moneyEffects is the per-kind effect table of the billing, trust and accounting gate kinds
// that have a dedicated effect in place of ApprovalEffect (GE2, GE3). One line per kind. The
// trust.transfer and billing.payment_allocation effects post on the books, so they are built
// only with the gate ports (p); without them the kind stays unregistered rather than approving
// with no posting. trust.regulatory_release audits each release: without the pool's audit
// queries it fails closed.
func moneyEffects(k *kboot.Kernel, p *GatePorts) map[string]registry.EffectFunc {
	fx := map[string]registry.EffectFunc{}
	fx[trust.CashGateKind] = trust.CashGateEffect
	if k == nil || k.Clock == nil {
		return fx
	}
	fx[bill.KindDelivery] = (&bill.DeliveryEffect{Bills: bill.NewPGService(k.DB, k.Clock.Now)}).Apply
	fx[recon.GateSignOff] = (&recon.SignOffEffect{Store: signOffStore(k), Now: k.Clock.Now}).Apply
	fx[payment.KindPaymentPlan] = (&payment.PlanEffect{Store: payment.PGPlans{}}).Apply
	fx[bill.KindPrebillReview] = (&bill.PrebillEffect{Store: bill.PGPrebill{}, Now: k.Clock.Now}).Apply
	fx[settings.KindSettingsChange] = (&settings.Effect{Store: settings.PGStore{}}).Apply
	fx[books.KindOperatingReconciliation] = (&books.OpReconEffect{Store: books.PGOpRecon{}, Now: k.Clock.Now}).Apply
	fx[trust.GateRegulatoryRelease] = (&trust.ReleaseEffect{Store: trust.PGReleases{}, Audit: releaseAudit(k), Now: k.Clock.Now}).Apply
	if p != nil && p.Billing.GL != nil {
		fx[payment.KindAllocation] = (&payment.AllocationEffect{Store: payment.PGAllocations{}, Ledger: p.Billing.GL, Clock: k.Clock}).Apply
	}
	if p != nil && p.Trust.GL != nil && p.Trust.Signers != nil {
		fx[withdraw.TransferGateKind] = (&withdraw.TransferEffect{
			Store:     withdraw.PGTransferStore{},
			Transfers: &withdraw.Transfers{Store: withdraw.PGTransferStore{}, GL: p.Trust.GL, Loc: k.Clock.Location()},
			Signers:   p.Trust.Signers,
			Now:       k.Clock.Now,
		}).Apply
	}
	return fx
}

// signOffStore is the reconciliation sign-off store. With the pool's plain queries it audits
// the sole-practitioner exception; without them the preparer is always refused.
func signOffStore(k *kboot.Kernel) *recon.PGSignOff {
	q, ok := k.DB.(readDB)
	if !ok {
		return recon.NewPGSignOff(nil)
	}
	return recon.NewPGSignOff(q).WithAudit(audit.New(q, AuditMasker(), audit.Options{}))
}

// releaseAudit is the audit log of regulatory releases, nil without the pool's plain queries.
func releaseAudit(k *kboot.Kernel) audit.Log {
	q, ok := k.DB.(readDB)
	if !ok {
		return nil
	}
	return audit.New(q, AuditMasker(), audit.Options{})
}

// withMoneyEffects adds moneyEffects to fx. A kind fx already handles is a duplicate error.
func withMoneyEffects(k *kboot.Kernel, p *GatePorts, fx map[string]registry.EffectFunc) (map[string]registry.EffectFunc, error) {
	return mergeEffects([]effectSource{{"gates", fx}, {"money", moneyEffects(k, p)}})
}
