// Package boot builds the kernel, lets each app register with it, and runs one process role.
// The cmd/counsel* binaries parse flags into a Config and call Run; they hold no logic.
package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/report"
	"github.com/dagi377/counsel/kernel/server"
	"github.com/dagi377/counsel/kernel/webhook"

	// Every kernel package, so each can register itself from its own files.
	_ "github.com/dagi377/counsel/kernel/all"
)

// Role is the process role a binary runs.
type Role string

// Process roles, one per binary.
const (
	RoleServer  Role = "server"
	RoleWorker  Role = "worker"
	RoleMigrate Role = "migrate"
	RoleImport  Role = "import"
)

// MigrateMode selects which half of an expand-contract migration may run.
type MigrateMode string

// Migrate modes. Expand is the default; contract (destructive) steps run only when asked.
const (
	MigrateExpand   MigrateMode = "expand"
	MigrateContract MigrateMode = "contract"
)

// Sentinel errors.
var (
	ErrConfig         = errors.New("boot: invalid config")
	ErrNotImplemented = errors.New("boot: role not implemented yet")
)

// Config is everything a binary passes to Run.
type Config struct {
	Role        Role
	Addr        string      // listen address (server)
	DatabaseURL string      // Postgres DSN; never logged
	Flags       []string    // flag names to turn on; each must be defined by an app
	MigrateMode MigrateMode // migrate only
	ImportFrom  string      // import only: source export path
}

// Validate checks the fields the role needs.
func (c Config) Validate() error {
	switch c.Role {
	case RoleServer, RoleWorker, RoleImport:
	case RoleMigrate:
		switch c.MigrateMode {
		case MigrateExpand, MigrateContract:
		default:
			return fmt.Errorf("migrate mode %q: %w", c.MigrateMode, ErrConfig)
		}
	default:
		return fmt.Errorf("role %q: %w", c.Role, ErrConfig)
	}
	return nil
}

// Kernel is what an app sees at registration. Fields are added as kernel modules land.
type Kernel struct {
	Flags    *flags.Set
	Registry registry.Booter // nil when built without one (Build)
	DB       db.DB
	Clock    clock.Clock
	Gates    gate.Engine
	Reports  report.Engine
	Server   server.Server
	Events   *webhook.Types
	Jobs     *Jobs // River workers and periodic jobs apps add at Register

	sealed bool // Registry.Seal passed; apply runs only then
}

// App is one application that registers with the kernel at boot.
type App struct {
	Name string
	// Define declares the app's feature flags. It runs before any flag is turned on and
	// before any Register, so a Register can read the flags it gates on. Optional.
	Define func(s *flags.Set) error
	// Register adds the app's registrations to the kernel. Required.
	Register func(ctx context.Context, k *Kernel) error
	// Start runs once the kernel is built and sealed, before the role runs. Optional.
	Start func(ctx context.Context, k *Kernel) error
}

// Deps are the kernel dependencies BuildWith hands to every app.
type Deps struct {
	Registry registry.Booter // sealed after every Register; nil skips Seal
	DB       db.DB
	Clock    clock.Clock
	Gates    gate.Engine
	Reports  report.Engine
	Server   server.Server
	Events   *webhook.Types
}

// Build creates the kernel and calls every app's Register in order.
func Build(ctx context.Context, apps ...App) (*Kernel, error) {
	return BuildWith(ctx, Deps{}, nil, apps...)
}

// BuildWith builds the kernel in a fixed order: every app's Define, then the flags in on are
// turned on, then every app's Register (with the flag set in ctx), then Registry.Seal. A flag
// in on that no app defined fails before any Register.
func BuildWith(ctx context.Context, deps Deps, on []string, apps ...App) (*Kernel, error) {
	k := &Kernel{
		Flags: flags.New(), Registry: deps.Registry, DB: deps.DB, Clock: deps.Clock,
		Gates: deps.Gates, Reports: deps.Reports, Server: deps.Server, Events: deps.Events,
		Jobs: NewJobs(),
	}
	for _, a := range apps {
		if a.Register == nil {
			return nil, fmt.Errorf("register app %q: nil Register: %w", a.Name, ErrConfig)
		}
	}
	for _, a := range apps {
		if a.Define == nil {
			continue
		}
		if err := a.Define(k.Flags); err != nil {
			return nil, fmt.Errorf("define flags of app %q: %w", a.Name, err)
		}
	}
	for _, name := range on {
		if err := k.Flags.Set(name, true); err != nil {
			return nil, fmt.Errorf("turn on flag %q: %w: %w", name, ErrConfig, err)
		}
	}
	ctx = flags.WithSet(ctx, k.Flags)
	// rule: ARC-I-040
	// Each app registers only its own modules' surface; the kernel seals the lot once.
	for _, a := range apps {
		if err := a.Register(ctx, k); err != nil {
			return nil, fmt.Errorf("register app %q: %w", a.Name, err)
		}
	}
	if k.Registry != nil {
		if err := k.Registry.Seal(); err != nil {
			return nil, fmt.Errorf("seal registry: %w", err)
		}
		k.sealed = true
	}
	return k, nil
}

// Run validates cfg, builds the kernel, turns on the requested flags and runs cfg.Role.
func Run(ctx context.Context, cfg Config, apps ...App) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	k, err := Build(ctx, apps...)
	if err != nil {
		return err
	}
	for _, name := range cfg.Flags {
		if err := k.Flags.Set(name, true); err != nil {
			return fmt.Errorf("turn on flag: %w", err)
		}
	}
	ctx = flags.WithSet(ctx, k.Flags)
	slog.InfoContext(ctx, "kernel built", "role", string(cfg.Role), "apps", len(apps), "flags_on", len(cfg.Flags))
	return fmt.Errorf("run %s: %w", cfg.Role, ErrNotImplemented)
}
