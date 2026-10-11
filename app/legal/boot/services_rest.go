package boot

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/dagi377/counsel/app/legal/billing/payment"
	"github.com/dagi377/counsel/app/legal/comms"
	"github.com/dagi377/counsel/app/legal/matter/matterkind"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/app/legal/trust/recon"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/deadline"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/ledger"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/portal"
	"github.com/dagi377/counsel/kernel/registry"
)

// BreakGlassSubjectColumn is the audit.break_glass column that holds a grant's matter id.
const BreakGlassSubjectColumn = "matter_id"

// KindSoleSync is the job kind that recomputes core.firm.sole_practitioner.
const KindSoleSync = "firm.sole_practitioner_sync"

// SoleSyncInterval is how often the sole-practitioner flag is recomputed. Licensee status
// also changes with the calendar (a licence expires_on passes), so a timer is needed even
// when no row changes.
const SoleSyncInterval = 15 * time.Minute

// RestPorts are the dependencies of the services boot wires last. Setting it (even empty)
// builds the break-glass grants, the reconciliation sign-off, the portal sessions, the event
// emitting deadline book and filing store, and the sole-practitioner sync on the kernel DB,
// clock, emitter and job set. A nil group below leaves its piece out.
type RestPorts struct {
	// Renew, when set, adds the subscription renewal job. The job still runs only while
	// FlagSourceSync is on (off by default).
	Renew *RenewPorts
	// Receipts, when set, wires the trust.receipt_classification effect (HTTP-16b).
	Receipts *ReceiptPorts
	// Refunds, when set, wires the billing.refund effect (REFUND-EFFECT).
	Refunds *RefundPorts
}

// ReceiptPorts are the receipt classification effect's dependencies.
type ReceiptPorts struct {
	// Poster posts a classified trust receipt to its client ledger (trust.LedgerStore).
	Poster trust.ReceiptPoster
}

// RefundPorts are the refund effect's dependencies. No Postgres RefundStore or TrustRefunds
// exists yet, so the composition root takes them from its caller.
type RefundPorts struct {
	Store  payment.RefundStore
	Trust  payment.TrustRefunds
	Ledger ledger.Ledger
}

// RestServices are the services the rest step builds. Later boot steps and the kernel read
// them through Wired.
type RestServices struct {
	BreakGlass *identity.BreakGlass
	SignOff    *recon.PGSignOff
	Sessions   portal.SessionStore
	// Book and Filings publish the deadline trigger events through the kernel emitter
	// (DL-EVENTS).
	Book    *deadline.PGBook
	Filings *comms.FilingStore
	// Emails files emails to matters and publishes email.filed (DRAFT-FIX).
	Emails *comms.EmailFilingStore
	// Contacts creates contacts and publishes party.organization_client_created; Walls is
	// the wall resolver publishing roles.walled_matter_accessed; LeadEvents is the emitter
	// intake.Leads publishes identity.portal_submission through (EVENTS-WIRE-2).
	Contacts   *party.Store
	Walls      *roles.WallResolver
	LeadEvents outbox.Emitter
}

// AuditMasker is the audit masker the legal app stores detail through. It keeps only ids,
// integers and codes under declared keys; grant_id must survive so a break-glass review can
// find its grant.
func AuditMasker() audit.AllowMasker {
	return audit.NewAllowMasker(map[string]audit.Kind{
		"grant_id": audit.KindID,
		"user_id":  audit.KindID,
	})
}

// readDB is the kernel DB with plain (non-tx) queries, as db.Pool provides.
type readDB interface {
	db.DB
	audit.Querier
}

// restServices builds the rest services on the kernel DB, clock and emitter.
func restServices(k *kboot.Kernel) (RestServices, error) {
	if err := requireDeps("rest services", []dep{
		{"kernel DB", k.DB == nil},
		{"kernel Clock", k.Clock == nil},
		{"kernel Emitter", k.Emitter == nil},
	}); err != nil {
		return RestServices{}, err
	}
	// The sign-off reads outside a tx and audit Verify reads the chain: both need the pool's
	// plain queries (db.Pool has them).
	q, ok := k.DB.(readDB)
	if !ok {
		return RestServices{}, fmt.Errorf("rest services: kernel DB has no plain queries: %w", ErrMissingDependency)
	}
	log := audit.New(q, AuditMasker(), audit.Options{})
	grants := identity.PGGrants{DB: k.DB, SubjectKind: matterkind.Subject, SubjectColumn: BreakGlassSubjectColumn}
	ev := newEventStores(k.DB, k.Emitter)
	return RestServices{
		BreakGlass: identity.NewBreakGlassWith(k.Clock, log, grants),
		SignOff:    recon.NewPGSignOff(q).WithAudit(log),
		Sessions:   portal.PGSessions{DB: k.DB},
		Book:       ev.Book,
		Filings:    ev.Filings,
		Emails:     ev.Emails,
		Contacts:   ev.Contacts,
		Walls:      ev.Walls,
		LeadEvents: ev.OwnTx,
	}, nil
}

// restEffects are the gate effects of p: receipt classification and refund. k supplies the
// clock.
func restEffects(k *kboot.Kernel, p *RestPorts) (map[string]registry.EffectFunc, error) {
	out := map[string]registry.EffectFunc{}
	if p == nil {
		return out, nil
	}
	if p.Receipts != nil {
		if p.Receipts.Poster == nil {
			return nil, fmt.Errorf("receipt effect: Poster: %w", ErrMissingDependency)
		}
		fx := (&trust.ReceiptClassificationEffect{Journal: trust.PGReceiptJournal{}, Ledgers: p.Receipts.Poster}).Effects()
		out[trust.GateReceiptClassification] = fx[trust.GateReceiptClassification]
	}
	if p.Refunds != nil {
		if err := requireDeps("refund effect", []dep{
			{"kernel Clock", k.Clock == nil},
			{"Store", p.Refunds.Store == nil},
			{"Trust", p.Refunds.Trust == nil},
			{"Ledger", p.Refunds.Ledger == nil},
		}); err != nil {
			return nil, err
		}
		fx := (&payment.RefundEffect{Store: p.Refunds.Store, Trust: p.Refunds.Trust, Ledger: p.Refunds.Ledger, Clock: k.Clock}).Effects()
		out[payment.GateKindRefund] = fx[payment.GateKindRefund]
	}
	return out, nil
}

// withRestEffects adds the rest effects to fx. A slug both handle is ErrDuplicateEffect.
func withRestEffects(k *kboot.Kernel, p *RestPorts, fx map[string]registry.EffectFunc) (map[string]registry.EffectFunc, error) {
	rest, err := restEffects(k, p)
	if err != nil {
		return nil, err
	}
	return mergeEffects([]effectSource{{"gates", fx}, {"rest", rest}})
}

// registerRest builds the rest services, keeps them on the wiring, and adds the renewal and
// sole-practitioner jobs. With Options.Rest nil it builds nothing.
func (w *wiring) registerRest(_ context.Context, k *kboot.Kernel) error {
	if w.opts.Rest == nil {
		return nil
	}
	s, err := restServices(k)
	if err != nil {
		return err
	}
	w.rest = &s
	if w.opts.Rest.Renew != nil {
		if err := addRenewJobs(k, *w.opts.Rest.Renew); err != nil {
			return err
		}
	}
	return addSoleSyncJob(k, k.DB)
}

// Wired returns the rest services the app built at Register, or nil before it ran.
func (w *wiring) Wired() *RestServices { return w.rest }

// syncSoleSQL sets core.firm.sole_practitioner to whether the firm has exactly one active
// licensee, as recon's sign-off counts them. A row already right is not touched.
const syncSoleSQL = `UPDATE core.firm f SET sole_practitioner = s.sole
FROM (SELECT count(*) = 1 AS sole FROM core.app_user u
  WHERE u.principal_kind = 'human' AND u.licence_type IN ('lawyer', 'paralegal')
    AND u.licence_status = 'good_standing'
    AND u.status = 'active' AND u.offboarded_at IS NULL AND u.archived_at IS NULL
    AND (u.expires_on IS NULL OR u.expires_on >= (now() AT TIME ZONE 'America/Toronto')::date)) s
WHERE f.singleton AND f.sole_practitioner IS DISTINCT FROM s.sole`

// SyncSolePractitioner recomputes core.firm.sole_practitioner from the active licensee count.
// Boot runs it at Start and every SoleSyncInterval.
func SyncSolePractitioner(ctx context.Context, d db.DB) error {
	return d.InTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, syncSoleSQL); err != nil {
			return fmt.Errorf("sync sole practitioner: %w", err)
		}
		return nil
	})
}

type soleSyncArgs struct{}

func (soleSyncArgs) Kind() string { return KindSoleSync }

type soleSyncWorker struct {
	river.WorkerDefaults[soleSyncArgs]
	db db.DB
}

func (w soleSyncWorker) Work(ctx context.Context, _ *river.Job[soleSyncArgs]) error {
	return SyncSolePractitioner(ctx, w.db)
}

// addSoleSyncJob adds kind firm.sole_practitioner_sync, run every SoleSyncInterval.
func addSoleSyncJob(k *kboot.Kernel, d db.DB) error {
	if k.Jobs == nil {
		return fmt.Errorf("sole sync job: kernel job set: %w", ErrJobDependency)
	}
	if err := kboot.AddWorker[soleSyncArgs](k.Jobs, soleSyncWorker{db: d}); err != nil {
		return fmt.Errorf("sole sync job: %w", err)
	}
	k.Jobs.AddPeriodic(periodicEvery(SoleSyncInterval, soleSyncArgs{}), KindSoleSync)
	return nil
}

// syncSoleAtStart recomputes the sole-practitioner flag once at Start.
func syncSoleAtStart(ctx context.Context, k *kboot.Kernel) error {
	if k.DB == nil {
		return fmt.Errorf("sole sync: kernel DB: %w", ErrMissingDependency)
	}
	return SyncSolePractitioner(ctx, k.DB)
}
