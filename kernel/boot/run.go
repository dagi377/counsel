package boot

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/registry/schemas"
	"github.com/dagi377/counsel/kernel/report"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/webhook"
)

// env is what Run takes from the outside world. Tests replace any part of it.
type env struct {
	// open connects the request database.
	open func(ctx context.Context, cfg db.Config) (db.DB, error)
	// registry returns the empty registry that apps record into and the kernel seals.
	registry func() (registry.Booter, error)
	// identity returns the access checker and principal resolver report runs are authorized
	// with. Nil, or a nil Access, keeps reports on denyAll.
	identity func(pool db.DB) (identity.Access, PrincipalResolver, error)
	// gates returns the gate engine, reading from pool.
	gates func(pool db.DB) (gate.Engine, error)
	// outbox returns the outbox relay over pool, which carries every outbox consumer (notify
	// delivery, webhook queueing). Nil leaves the kernel with no outbox.
	outbox func(pool db.DB) (*outbox.Deliverer, error)
	// server runs the server role.
	server func(ctx context.Context, cfg Config, k *Kernel) error
	// worker runs the worker role.
	worker func(ctx context.Context, k *Kernel, cfg WorkerConfig) error
	// apply applies the migration files of one migrate mode.
	apply applier
	// inserter opens the insert-only job client apps enqueue on. Nil leaves Kernel.Enqueuer
	// nil.
	inserter openInserter
}

func productionEnv() env {
	return env{
		open: func(ctx context.Context, cfg db.Config) (db.DB, error) { return db.Open(ctx, cfg) },
		registry: func() (registry.Booter, error) {
			s, err := registry.LoadSchemas(schemas.FS)
			if err != nil {
				return nil, err
			}
			return registry.NewBoot(s), nil
		},
		gates:  pgGates,
		outbox: pgOutbox,
		server: runServer,
		worker: func(ctx context.Context, k *Kernel, cfg WorkerConfig) error {
			return runWorker(ctx, k, cfg, newRiverClient)
		},
		apply:    pgApply,
		inserter: pgInserter,
	}
}

// Run validates cfg, opens the database, builds the kernel engines, lets every app register,
// applies the sealed registrations, adds the kernel's jobs, starts every app and runs
// cfg.Role until ctx is cancelled.
func Run(ctx context.Context, cfg Config, apps ...App) error {
	return run(ctx, cfg, productionEnv(), apps...)
}

func run(ctx context.Context, cfg Config, e env, apps ...App) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	if cfg.Role == RoleMigrate {
		// Migrations change the schema the apps register against, so no app is built.
		return runMigrate(ctx, cfg, e.apply)
	}
	if cfg.Role != RoleServer && cfg.Role != RoleWorker {
		// Import builds the kernel with no database until its task lands.
		if _, err := BuildWith(ctx, Deps{}, cfg.Flags, apps...); err != nil {
			return err
		}
		return fmt.Errorf("run %s: %w", cfg.Role, ErrNotImplemented)
	}

	pool, err := e.open(ctx, cfg.dbConfig())
	if err != nil {
		// db errors name no DSN; cfg is never formatted.
		return fmt.Errorf("run %s: open database: %w", cfg.Role, err)
	}
	defer pool.Close()
	clk, err := clock.New(cfg.Zone)
	if err != nil {
		return fmt.Errorf("run %s: %w", cfg.Role, err)
	}
	reg, err := e.registry()
	if err != nil {
		return fmt.Errorf("run %s: registry: %w", cfg.Role, err)
	}
	gates, err := e.gates(pool)
	if err != nil {
		return fmt.Errorf("run %s: %w", cfg.Role, err)
	}
	var (
		access  identity.Access
		resolve PrincipalResolver
	)
	if e.identity != nil {
		if access, resolve, err = e.identity(pool); err != nil {
			return fmt.Errorf("run %s: identity: %w", cfg.Role, err)
		}
	}
	var relay *outbox.Deliverer
	if e.outbox != nil {
		if relay, err = e.outbox(pool); err != nil {
			return fmt.Errorf("run %s: %w", cfg.Role, err)
		}
	}
	var enq JobInserter
	if e.inserter != nil {
		ins, closeIns, err := e.inserter(ctx, cfg.dbConfig())
		if err != nil {
			return fmt.Errorf("run %s: %w", cfg.Role, err)
		}
		defer closeIns()
		enq = ins
	}
	deps := Deps{
		Enqueuer: enq,
		Outbox:   relay,
		Registry: reg,
		DB:       pool,
		Clock:    clk,
		Gates:    gates,
		// Reports check capabilities through identity; with no identity they fail closed.
		Reports: report.New(time.Now, reportAuthorizer(access, resolve), audit.New(nil, nil, audit.Options{})),
		Server:  server.New(nil),
		Events:  webhook.NewTypes(),
		Notify:  notify.New(notify.PGStore{DB: pool}, outbox.NewPG(nil)),
	}
	k, err := BuildWith(ctx, deps, cfg.Flags, apps...)
	if err != nil {
		return fmt.Errorf("run %s: %w", cfg.Role, err)
	}
	if err := apply(k); err != nil {
		return fmt.Errorf("run %s: %w", cfg.Role, err)
	}
	// Notify and webhook consumers join the outbox relay after every app's Register, so an
	// app's sender and dispatcher are in place.
	if err := addNotifyJobs(k); err != nil {
		return fmt.Errorf("run %s: %w", cfg.Role, err)
	}
	if err := addWebhookJobs(k); err != nil {
		return fmt.Errorf("run %s: %w", cfg.Role, err)
	}
	if err := addKernelJobs(k); err != nil {
		return fmt.Errorf("run %s: %w", cfg.Role, err)
	}
	for _, a := range apps {
		if a.Start == nil {
			continue
		}
		if err := a.Start(ctx, k); err != nil {
			return fmt.Errorf("run %s: start app %q: %w", cfg.Role, a.Name, err)
		}
	}
	slog.InfoContext(ctx, "kernel built", "role", string(cfg.Role), "apps", len(apps), "flags_on", len(cfg.Flags))

	if cfg.Role == RoleServer {
		return e.server(ctx, cfg, k)
	}
	return e.worker(ctx, k, cfg.workerConfig())
}

// dbConfig is the database part of cfg.
func (c Config) dbConfig() db.Config {
	return db.Config{PooledDSN: c.DatabaseURL, DirectDSN: c.DirectDatabaseURL}
}

// workerConfig is the worker role's part of cfg.
func (c Config) workerConfig() WorkerConfig {
	n := c.MaxWorkers
	if n <= 0 {
		n = defaultMaxWorkers
	}
	return WorkerConfig{DB: c.dbConfig(), MaxWorkers: n}
}

// denyAll is the report Authorizer until identity is wired at boot: every check fails closed.
type denyAll struct{}

func (denyAll) Can(context.Context, string, string) (bool, error) { return false, nil }

// pgGates returns the Postgres gate engine, reading gates from pool.
func pgGates(pool db.DB) (gate.Engine, error) {
	if pool == nil {
		return nil, fmt.Errorf("gate engine: no database: %w", ErrConfig)
	}
	g := gate.NewPG()
	g.SetDB(pool)
	return g, nil
}

// pgOutbox returns the Postgres outbox relay over pool. The relay reads and marks outbox rows
// outside any caller's transaction, so pool must also run plain statements (outbox.Conn).
func pgOutbox(pool db.DB) (*outbox.Deliverer, error) {
	conn, ok := pool.(outbox.Conn)
	if !ok {
		return nil, fmt.Errorf("outbox relay: database cannot run plain statements: %w", ErrConfig)
	}
	pg := outbox.NewPG(conn)
	return &outbox.Deliverer{Relay: pg, Inbox: pg, DB: pool}, nil
}
