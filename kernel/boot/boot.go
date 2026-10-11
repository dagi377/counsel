// Package boot builds the kernel, lets each app register with it, and runs one process role.
// The cmd/counsel* binaries parse flags into a Config and call Run; they hold no logic.
package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/migrations"

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
// Wait-version applies nothing: it waits until the database has SchemaVersion, so an app pod
// (as an init container) starts only on the schema its image expects.
const (
	MigrateExpand      MigrateMode = "expand"
	MigrateContract    MigrateMode = "contract"
	MigrateWaitVersion MigrateMode = "wait-version"
)

// waitPoll is how often wait-version reads the schema version.
const waitPoll = 2 * time.Second

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
	// SchemaVersion is the migration file wait-version waits for (migrate only); empty means
	// the newest migration built into this binary.
	SchemaVersion string
	Timeout       time.Duration // stop Run after this long; 0 means no limit
	ImportFrom    string        // import only: source export path
}

// Validate checks the fields the role needs.
func (c Config) Validate() error {
	switch c.Role {
	case RoleServer, RoleWorker, RoleImport:
	case RoleMigrate:
		switch c.MigrateMode {
		case MigrateExpand, MigrateContract, MigrateWaitVersion:
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
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	if cfg.Role == RoleMigrate && cfg.MigrateMode == MigrateWaitVersion {
		return waitVersion(ctx, cfg)
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

// waitVersion blocks until the database at cfg.DatabaseURL has the expected schema version,
// reconnecting after any connection or query error. It needs only read access, so app pods run
// it with their own credentials. Errors from pgx can name the DSN host and user, so they are
// never logged; only ctx ending or an unknown version stops the wait.
func waitVersion(ctx context.Context, cfg Config) error {
	want := cfg.SchemaVersion
	if want == "" {
		want = migrations.Latest()
	}
	if !slices.Contains(migrations.Names(), want) {
		return fmt.Errorf("wait for schema version %q: %w", want, migrations.ErrUnknownVersion)
	}
	slog.InfoContext(ctx, "waiting for schema version", "version", want)
	for {
		conn, err := pgx.Connect(ctx, cfg.DatabaseURL)
		if err == nil {
			err = migrations.Wait(ctx, conn, want, waitPoll)
			_ = conn.Close(context.WithoutCancel(ctx))
			if err == nil {
				slog.InfoContext(ctx, "schema version present", "version", want)
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for schema version %s: %w", want, ctx.Err())
		case <-time.After(waitPoll):
			slog.WarnContext(ctx, "schema version not readable yet, retrying", "version", want)
		}
	}
}
