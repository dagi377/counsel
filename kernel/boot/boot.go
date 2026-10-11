// Package boot builds the kernel, lets each app register with it, and runs one process role.
// The cmd/counsel* binaries parse flags into a Config and call Run; they hold no logic.
package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/dagi377/counsel/kernel/flags"

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
	Flags *flags.Set
}

// App is one application that registers with the kernel at boot.
type App struct {
	Name     string
	Register func(ctx context.Context, k *Kernel) error
}

// Build creates the kernel and calls every app's Register in order.
func Build(ctx context.Context, apps ...App) (*Kernel, error) {
	k := &Kernel{Flags: flags.New()}
	for _, a := range apps {
		if a.Register == nil {
			return nil, fmt.Errorf("register app %q: nil Register: %w", a.Name, ErrConfig)
		}
		if err := a.Register(ctx, k); err != nil {
			return nil, fmt.Errorf("register app %q: %w", a.Name, err)
		}
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
