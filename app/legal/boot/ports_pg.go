package boot

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/app/legal/billing/credit"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/trust/signatory"
	"github.com/dagi377/counsel/app/legal/trust/withdraw"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/webhook"
)

// MatterReader is the matter module's read port (matter.Store implements it).
type MatterReader interface {
	Get(ctx context.Context, id matter.ID) (matter.Matter, error)
}

// MatterLawyers is the matter-side ResponsibleLawyers adapter (PORT-18): billing reads a
// bill's responsible lawyer through the matter module's own reader and never queries a
// matter table (ARC-I-041). The read runs in its own RLS transaction, not the caller's.
type MatterLawyers struct {
	Matters MatterReader
}

var _ credit.ResponsibleLawyers = MatterLawyers{}

// ResponsibleLawyer implements credit.ResponsibleLawyers. A nil reader or an empty lawyer
// fails closed.
func (l MatterLawyers) ResponsibleLawyer(ctx context.Context, _ pgx.Tx, matterID string) (string, error) {
	if l.Matters == nil {
		return "", fmt.Errorf("responsible lawyer of %s: matter reader: %w", matterID, ErrMissingDependency)
	}
	m, err := l.Matters.Get(ctx, matter.ID(matterID))
	if err != nil {
		return "", fmt.Errorf("responsible lawyer of %s: %w", matterID, err)
	}
	if m.ResponsibleLawyerID == "" {
		return "", fmt.Errorf("responsible lawyer of %s: none set: %w", matterID, matter.ErrNotFound)
	}
	return m.ResponsibleLawyerID, nil
}

// WithPostgres fills every nil trust port that has a Postgres implementation: the signatory
// service and directory, the withdrawal signatory check (PGSigners) and the firm policy.
// Ports with no Postgres implementation (Seal, Audit) stay as given.
func (p TrustPorts) WithPostgres(pool db.DB) TrustPorts {
	if p.Users == nil {
		p.Users = signatory.NewPGDirectory(pool)
	}
	if p.Signatories == nil {
		p.Signatories = signatory.NewPGService(pool)
	}
	if p.Signers == nil {
		p.Signers = withdraw.PGSigners{}
	}
	if p.Firm == nil {
		p.Firm = withdraw.NewPGFirm(pool)
	}
	return p
}

// WithPostgres fills the nil write-off ports with their Postgres implementations: the
// write-off store and bill reader (PGWriteOffs reading the responsible lawyer through
// MatterLawyers over matters) and the bad-debt poster GLBadDebt{Ledger, WriteOffs} on gl
// (PORT-16). A nil matters fails every write-off closed.
func (p CreditPorts) WithPostgres(gl ledger.Ledger, matters MatterReader) CreditPorts {
	lawyers := MatterLawyers{Matters: matters}
	wo := credit.PGWriteOffs{Lawyers: lawyers}
	if p.Store == nil {
		p.Store = wo
	}
	if p.Bills == nil {
		p.Bills = wo
	}
	if p.Ledger == nil && gl != nil {
		p.Ledger = credit.GLBadDebt{Ledger: gl, WriteOffs: wo}
	}
	return p
}

// signatoryEffects returns the signing-authority effects of svc. A Postgres service runs them
// on the gate tx (signatory.TxEffects, PORT-11), so the change commits with the decision;
// any other service uses its plain Effects.
func signatoryEffects(svc signatory.Service) map[string]registry.EffectFunc {
	if pg, ok := svc.(*signatory.PGService); ok {
		return signatory.TxEffects(pg)
	}
	return signatory.Effects(svc)
}

// WebhookPorts configure outbound webhook delivery (PORT-07).
type WebhookPorts struct {
	// Secrets resolves an endpoint's secret_ref to its signing secret. Required.
	Secrets webhook.SecretSource
}

// wireWebhooks sets the kernel's webhook poster to a signed HTTPPoster whose endpoints come
// from platform.webhook (PGTargets). A poster already set is kept.
func wireWebhooks(k *kboot.Kernel, p *WebhookPorts) error {
	if p == nil || k.WebhookPoster != nil {
		return nil
	}
	if p.Secrets == nil || k.DB == nil {
		return fmt.Errorf("webhooks: Secrets and kernel DB: %w", ErrMissingDependency)
	}
	k.WebhookPoster = &webhook.HTTPPoster{Targets: webhook.PGTargets{DB: k.DB}, Secrets: p.Secrets}
	return nil
}
