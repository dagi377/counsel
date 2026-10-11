package boot

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/webhook"
)

// Production is the Postgres composition the binaries run. The pool opens inside kernel boot,
// so its ports are built at Register on the kernel DB, clock and gate engine; an Options group
// the caller already set is kept.
type Production struct {
	// Principal authenticates a request. Nil authenticates nobody: every route answers 401,
	// never 503.
	Principal func(r *http.Request) (registry.Principal, bool)
	// Roles and Assignments decide capabilities. Nil gives an empty catalog and nobody a
	// role, so every capability check fails closed.
	Roles       *identity.Roles
	Assignments identity.RoleAssignments
	// CursorKey signs list cursors. Nil uses a random key per process: cursors then do not
	// survive a restart, which only restarts a page walk.
	CursorKey []byte
	// WebhookSecrets, when set, turns on outbound webhook delivery (PORT-07).
	WebhookSecrets webhook.SecretSource
}

// pgConn is the kernel DB with plain statements and queries, as db.Pool provides.
type pgConn interface {
	readDB
	outbox.Conn
	idem.Conn
}

// bindProduction fills every Options group left nil from Postgres on the kernel. It runs
// first, so the later Register steps see the filled groups.
func (w *wiring) bindProduction(_ context.Context, k *kboot.Kernel) error {
	p := w.opts.Production
	if p == nil {
		return nil
	}
	opts, err := p.options(k)
	if err != nil {
		return fmt.Errorf("production: %w", err)
	}
	w.opts = merge(w.opts, opts)
	return nil
}

// options builds the Postgres Options graph on k. It never connects: every port reads k.DB
// only when called.
func (p Production) options(k *kboot.Kernel) (Options, error) {
	if k == nil || k.DB == nil || k.Clock == nil {
		return Options{}, fmt.Errorf("kernel DB and Clock: %w", ErrMissingDependency)
	}
	conn, ok := k.DB.(pgConn)
	if !ok {
		return Options{}, fmt.Errorf("kernel DB %T has no plain statements: %w", k.DB, ErrMissingDependency)
	}
	if k.Emitter == nil {
		// The outbox writer is the kernel emitter: events commit with the tx that caused them.
		k.Emitter = outbox.NewPG(conn)
	}
	key := p.CursorKey
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return Options{}, fmt.Errorf("cursor key: %w", err)
		}
	}
	cursors, err := api.NewCodec(key)
	if err != nil {
		return Options{}, fmt.Errorf("cursor codec: %w", err)
	}
	roles, assign := p.Roles, p.Assignments
	if roles == nil {
		roles = identity.NewRoles()
	}
	if assign == nil {
		assign = identity.StaticAssignments{}
	}
	principal := p.Principal
	if principal == nil {
		principal = noPrincipal
	}
	o := Options{
		HTTP: &HTTPPorts{
			Access:    identity.NewChecker(roles, assign),
			Cursors:   cursors,
			Problems:  &server.Problems{},
			Principal: principal,
			Idem:      idem.NewPG(conn),
			Audit:     audit.New(conn, AuditMasker(), audit.Options{}),
		},
		Clearance: &ClearancePorts{Search: conflict.NewPGSearcher(k.DB), Roles: roles, Assignments: assign},
		Rest:      &RestPorts{},
	}
	if p.WebhookSecrets != nil {
		o.Webhooks = &WebhookPorts{Secrets: p.WebhookSecrets}
	}
	return o, nil
}

// noPrincipal authenticates nobody.
func noPrincipal(*http.Request) (registry.Principal, bool) { return registry.Principal{}, false }

// merge is base with every nil group taken from add.
func merge(base, add Options) Options {
	if base.HTTP == nil {
		base.HTTP = add.HTTP
	}
	if base.Clearance == nil {
		base.Clearance = add.Clearance
	}
	if base.Rest == nil {
		base.Rest = add.Rest
	}
	if base.Webhooks == nil {
		base.Webhooks = add.Webhooks
	}
	if base.Gates == nil {
		base.Gates = add.Gates
	}
	if base.Agent == nil {
		base.Agent = add.Agent
	}
	return base
}

// moduleMounts are the module route groups served over the kernel DB from the shared HTTP
// ports: matter reads, contacts and the firm tax registrations (MATTER-TAX-API).
func moduleMounts(k *kboot.Kernel, p HTTPPorts) []mount {
	if k == nil || k.DB == nil {
		return nil
	}
	contacts := party.NewStore(k.DB)
	matters := matter.NewStore(k.DB, contacts)
	return []mount{
		(&matter.HTTP{Query: matters, Access: p.Access, Cursors: p.Cursors, Problems: p.Problems,
			Principal: p.Principal}).Routes,
		(&party.HTTP{Contacts: contacts, Access: p.Access, Cursors: p.Cursors, Problems: p.Problems,
			Principal: p.Principal}).Routes,
		(&tax.RegistrationsHTTP{Store: tax.PGRegistrations{DB: k.DB}, Access: p.Access, Problems: p.Problems,
			Principal: p.Principal}).Routes,
	}
}

// designatedOrPG is d, or the Postgres firm setting on the kernel DB when d is nil.
func designatedOrPG(k *kboot.Kernel, d trigger.DesignatedLawyers) trigger.DesignatedLawyers {
	if d == nil && k != nil && k.DB != nil {
		return agent.NewPGDesignatedLawyers(k.DB)
	}
	return d
}
