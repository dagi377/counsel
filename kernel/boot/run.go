package boot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/dagi377/counsel/kernel/audit"
	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/report"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/webhook"
)

// schemaDir is where the registry schemas live, relative to the working directory.
const schemaDir = "spec/kernel"

// env is what Run takes from the outside world. Tests replace any part of it.
type env struct {
	// open connects the request database.
	open func(ctx context.Context, cfg db.Config) (db.DB, error)
	// registry returns the empty registry that apps record into and the kernel seals.
	registry func() (registry.Booter, error)
	// gates returns the gate engine, reading from pool.
	gates func(pool db.DB) (gate.Engine, error)
	// server runs the server role.
	server func(ctx context.Context, cfg Config, k *Kernel) error
	// worker runs the worker role.
	worker func(ctx context.Context, k *Kernel, cfg WorkerConfig) error
}

func productionEnv() env {
	return env{
		open: func(ctx context.Context, cfg db.Config) (db.DB, error) { return db.Open(ctx, cfg) },
		registry: func() (registry.Booter, error) {
			s, err := registry.LoadSchemas(os.DirFS(schemaDir))
			if err != nil {
				return nil, err
			}
			return registry.NewBoot(s), nil
		},
		gates:  pgGates,
		server: runServer,
		worker: func(ctx context.Context, k *Kernel, cfg WorkerConfig) error {
			return runWorker(ctx, k, cfg, newRiverClient)
		},
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
	if cfg.Role != RoleServer && cfg.Role != RoleWorker {
		// Migrate and import build the kernel with no database until their tasks land.
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
	deps := Deps{
		Registry: reg,
		DB:       pool,
		Clock:    clk,
		Gates:    gates,
		// Reports fail closed until identity wiring supplies an authorizer.
		Reports: report.New(time.Now, denyAll{}, audit.New(nil, nil, audit.Options{})),
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
