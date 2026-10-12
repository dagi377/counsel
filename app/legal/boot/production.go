package boot

import (
	"context"
	"crypto/rand"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/dagi377/counsel/app/legal/agent"
	"github.com/dagi377/counsel/app/legal/agent/trigger"
	"github.com/dagi377/counsel/app/legal/billing/tax"
	"github.com/dagi377/counsel/app/legal/conflict"
	"github.com/dagi377/counsel/app/legal/matter"
	"github.com/dagi377/counsel/app/legal/party"
	legalroles "github.com/dagi377/counsel/app/legal/roles"
	"github.com/dagi377/counsel/app/legal/trust"
	"github.com/dagi377/counsel/kernel/ai"
	"github.com/dagi377/counsel/kernel/ai/httpclient"
	"github.com/dagi377/counsel/kernel/api"
	"github.com/dagi377/counsel/kernel/audit"
	kboot "github.com/dagi377/counsel/kernel/boot"
	"github.com/dagi377/counsel/kernel/idem"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/identity/login"
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
	// Roles and Assignments decide capabilities. Nil Roles is the catalog the app registers
	// (its capabilities, roles, subject kinds and gate approvers); nil Assignments reads each
	// member's role from Postgres (identity.PGAssignments).
	Roles       *identity.Roles
	Assignments identity.RoleAssignments
	// CursorKey signs list cursors. Nil uses a random key per process: cursors then do not
	// survive a restart, which only restarts a page walk.
	CursorKey []byte
	// WebhookSecrets, when set, turns on outbound webhook delivery (PORT-07).
	WebhookSecrets webhook.SecretSource
	// Login, when it configures a sign-in path, mounts the member sign-in routes and makes
	// the session cookie authenticate requests (Principal, when set, still wins). A config
	// that fails login.Config.Validate, such as a dev login outside a dev environment,
	// fails boot.
	Login *Login
	// SealKey, when set, turns on the gate effects (gatePortsPG): it is the AES-256 key that
	// seals trust account numbers. Nil leaves Options.Gates unset, so no app gate kind with
	// a money effect is registered (fail closed).
	SealKey *trust.SealKey
	// SealKeyBase64 is the active seal key as standard base64 of 32 bytes, from the secret
	// environment ($COUNSEL_SEAL_KEY); SealKeyFile names a mounted file holding the same.
	// Either sets SealKey when it is nil. Neither is ever logged or formatted into an error.
	SealKeyBase64 Secret
	SealKeyFile   string
	// Dev marks a dev environment. Outside dev, boot refuses to start without a seal key;
	// in dev a missing key is replaced by a random per-process key, with a warning.
	Dev bool
	// BillPrefix is the bill number series prefix; empty uses DefaultBillPrefix.
	BillPrefix string

	// Model names the model provider and models the agent runs on (spec/ai/mocks.md sec. 8).
	// Nil outside dev leaves the agent features not ready (Readiness) without stopping boot;
	// in dev nil is the in-process mock. ModelClient, when set, is used as is (tests: a mock).
	Model       *ai.Config
	ModelClient ai.ModelClient
	// ModelEnv is the deployment environment the model config is checked for: a live model is
	// refused in test and CI. ModelKey resolves Model.KeyRef; the key is never logged.
	ModelEnv string
	ModelKey httpclient.KeyFunc
	// ModelFixtures are the mock model's fixtures (testdata/ai layout); nil holds none.
	ModelFixtures fs.FS
	// FirmAccount is the payee id of the firm's general account, which fee transfers out of
	// trust pay. Empty leaves the agent features not ready.
	FirmAccount string
	// Jobs enqueues the queued agent runs and the job features' triggers. Nil leaves the
	// queued features not ready.
	Jobs trigger.Enqueuer
	// Readiness, when set, receives every agent feature that cannot run and why.
	Readiness *AgentReadiness
	// Docs is the cell document bucket. An unset URL falls back to the bucket environment
	// variables (services_docs.go); with neither the document routes answer 503.
	Docs DocsBucketConfig
}

// ModelConfig is the model section of the deployment config (kernel/ai).
type ModelConfig = ai.Config

// Login is the member sign-in configuration (kernel/identity/login).
type Login = login.Config

// pgConn is the kernel DB with plain statements and queries, as db.Pool provides.
type pgConn interface {
	readDB
	outbox.Conn
	idem.Conn
}

// bindProduction fills every Options group left nil from Postgres on the kernel. It runs
// first, so the later Register steps see the filled groups.
func (w *wiring) bindProduction(ctx context.Context, k *kboot.Kernel) error {
	if w.opts.Production == nil {
		return nil
	}
	// A copy: boot fills SealKey and Roles without touching the caller's config.
	pc := *w.opts.Production
	p := &pc
	if p.SealKey == nil {
		key, err := p.resolveSealKey()
		if err != nil {
			return fmt.Errorf("production: %w", err)
		}
		p.SealKey = key
	}
	if p.Roles == nil {
		// The checker is built now, before the app registers; loadRoles fills this catalog.
		p.Roles = identity.NewRoles()
		w.roles = p.Roles
	}
	opts, err := p.options(k)
	if err != nil {
		return fmt.Errorf("production: %w", err)
	}
	if opts.HTTP != nil && opts.HTTP.DocsBucket == nil {
		if b := p.Docs.Bucket(); b != nil {
			opts.HTTP.DocsBucket = b
		}
	}
	if opts.HTTP != nil && p.Principal == nil {
		svc, err := p.login(k, opts.HTTP.Problems)
		if err != nil {
			return fmt.Errorf("production: %w", err)
		}
		if svc != nil {
			opts.HTTP.Principal = svc.Authenticate
			// rule: ARC-I-017
			opts.HTTP.StepUp = svc.StepUp
		}
	}
	// The agent layer is built after sign-in, so the action route authenticates like the rest.
	if w.opts.HTTP == nil {
		ag, err := p.bindAgent(ctx, k, &opts, w.opts.Agent)
		if err != nil {
			return fmt.Errorf("production: %w", err)
		}
		// An agent that is not ready registers nothing, even a wiring the caller preset.
		opts.Agent = ag
		w.opts.Agent = ag
	}
	w.opts = merge(w.opts, opts)
	return nil
}

// rootMounter is the kernel server's mount for routes outside the versioned API.
type rootMounter interface {
	MountRoot(m server.Mounter) error
}

// login builds the sign-in service on the kernel DB and mounts its routes. It is nil when
// no sign-in path is configured: then nobody authenticates and every route answers 401.
func (p Production) login(k *kboot.Kernel, problems server.ProblemWriter) (*login.Service, error) {
	if p.Login == nil || !p.Login.Enabled() {
		slog.Warn("no member sign-in configured: every API route answers 401")
		return nil, nil
	}
	store := login.PGStore{DB: k.DB}
	svc, err := login.New(*p.Login, login.Deps{Store: store, Users: store, Problems: problems})
	if err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	rm, ok := k.Server.(rootMounter)
	if !ok {
		return nil, fmt.Errorf("login: kernel server %T cannot mount sign-in routes: %w", k.Server, ErrMissingDependency)
	}
	if err := rm.MountRoot(svc.Routes); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}
	return svc, nil
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
		// rule: ARC-I-018
		// Capabilities come from the member's role in core.app_user, read per check.
		assign = identity.PGAssignments{DB: k.DB}
	}
	principal := p.Principal
	if principal == nil {
		principal = noPrincipal
	}
	access := identity.NewChecker(roles, assign)
	auditLog := audit.New(conn, AuditMasker(), audit.Options{})
	o := Options{
		HTTP: &HTTPPorts{
			Access:    access,
			Cursors:   cursors,
			Problems:  &server.Problems{},
			Principal: principal,
			// No sign-in service: no request ever passed step-up, so every gate decision
			// answers 401 step_up_required (fail closed). bindProduction replaces it.
			StepUp: noStepUp,
			Idem:   idem.NewPG(conn),
			Audit:  auditLog,
		},
		Clearance: &ClearancePorts{Search: conflict.NewPGSearcher(k.DB), Roles: roles, Assignments: assign},
		Rest:      &RestPorts{},
	}
	if p.SealKey != nil {
		g, err := gatePortsPG(k, conn, *p.SealKey, p.BillPrefix, auditLog, access)
		if err != nil {
			return Options{}, err
		}
		o.Gates = g
	}
	if p.WebhookSecrets != nil {
		o.Webhooks = &WebhookPorts{Secrets: p.WebhookSecrets}
	}
	return o, nil
}

// noStepUp is a request that never passed step-up authentication.
func noStepUp(*http.Request) time.Time { return time.Time{} }

// loadRoles fills the catalog bindProduction built from every capability, role, subject
// kind and gate kind the app registered, then validates it. It runs after every other
// Register step, before the kernel serves a request.
func (w *wiring) loadRoles(_ context.Context, k *kboot.Kernel) error {
	if w.roles == nil {
		return nil
	}
	lister, ok := k.Registry.(interface{ Calls() []registry.Call })
	if !ok {
		return fmt.Errorf("production roles: registry %T cannot list its calls: %w", k.Registry, ErrMissingDependency)
	}
	w.roles.Load(lister.Calls())
	// rule: ARC-I-018
	// Built-in roles hold each capability at its spec level; custom roles come from
	// core.role_grant per check (identity.PGAssignments).
	if err := legalroles.ApplyLevels(w.roles); err != nil {
		return fmt.Errorf("production roles: %w", err)
	}
	if err := w.roles.Validate(); err != nil {
		return fmt.Errorf("production roles: %w", err)
	}
	return nil
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
// ports: matter reads and writes, contacts and the firm tax registrations (MATTER-TAX-API).
func moduleMounts(k *kboot.Kernel, p HTTPPorts, approvers map[string]int) []mount {
	if k == nil || k.DB == nil {
		return nil
	}
	contacts := party.NewStore(k.DB)
	matters := matter.NewStore(k.DB, contacts)
	reads := matter.HTTP{Query: matters, Access: p.Access, Cursors: p.Cursors, Problems: p.Problems,
		Principal: p.Principal}
	out := []mount{
		(&reads).Routes,
		(&party.HTTP{Contacts: contacts, Access: p.Access, Cursors: p.Cursors, Problems: p.Problems,
			Principal: p.Principal}).Routes,
		(&tax.RegistrationsHTTP{Store: tax.PGRegistrations{DB: k.DB}, Access: p.Access, Problems: p.Problems,
			Principal: p.Principal}).Routes,
	}
	if w := matterWrites(k, p, reads, approvers); w != nil {
		out = append(out, w.WriteRoutes)
	}
	out = append(out, docsMounts(k, p)...)
	return out
}

// matterWrites is POST /matters and PATCH /matters/{matter_id} over Postgres: plain fields
// through matter.PGUpdater (If-Match, walls under RLS), gate-owned changes through the gate
// engine, the status dry run through a Lifecycle reading walls and conflict runs from
// Postgres, and Idempotency-Key on the shared store. Nil when a port is missing.
func matterWrites(k *kboot.Kernel, p HTTPPorts, reads matter.HTTP, approvers map[string]int) *matter.Writes {
	if p.DB == nil || p.Gates == nil || p.Idem == nil || k.Clock == nil {
		return nil
	}
	return &matter.Writes{
		HTTP:    reads,
		DB:      p.DB,
		Gates:   p.Gates,
		Updates: matter.NewPGUpdater(k.DB),
		Lifecycle: &matter.Lifecycle{
			Clock:     k.Clock,
			Conflicts: conflict.NewChecksWith(conflict.NewPGSearcher(k.DB), conflict.NewPGRuns(k.DB), k.Clock.Now),
			Walls:     matter.NewPGWalls(k.DB),
		},
		Idem:         p.Idem,
		MinApprovers: approvers,
	}
}

// designatedOrPG is d, or the Postgres firm setting on the kernel DB when d is nil.
func designatedOrPG(k *kboot.Kernel, d trigger.DesignatedLawyers) trigger.DesignatedLawyers {
	if d == nil && k != nil && k.DB != nil {
		return agent.NewPGDesignatedLawyers(k.DB)
	}
	return d
}
