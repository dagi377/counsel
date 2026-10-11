// Package boot builds the kernel, lets each app register with it, and runs one process role.
// The cmd/counsel* binaries parse flags into a Config and call Run; they hold no logic.
package boot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dagi377/counsel/kernel/clock"
	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/identity"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
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
// Wait-version applies nothing: it waits until the database has SchemaVersion, so an app pod
// (as an init container) starts only on the schema its image expects.
const (
	MigrateExpand      MigrateMode = "expand"
	MigrateContract    MigrateMode = "contract"
	MigrateWaitVersion MigrateMode = "wait-version"
)

// Sentinel errors.
var (
	ErrConfig         = errors.New("boot: invalid config")
	ErrNotImplemented = errors.New("boot: role not implemented yet")
	ErrConnect        = errors.New("boot: cannot connect to the database")
)

// Config is everything a binary passes to Run.
type Config struct {
	Role        Role
	Addr        string // listen address (server)
	DatabaseURL string // Postgres DSN through the pooler; never logged
	// DirectDatabaseURL is the Postgres DSN that bypasses the pooler, for the job queue and
	// migrations (ARC-U-011). Never logged.
	DirectDatabaseURL string
	Zone              string      // IANA business time zone; the binary passes it, the kernel names none
	MaxWorkers        int         // worker only: concurrent jobs on the default queue; 0 means 10
	Flags             []string    // flag names to turn on; each must be defined by an app
	MigrateMode       MigrateMode // migrate only
	// SchemaVersion is the migration file wait-version waits for (migrate only); empty means
	// the newest migration built into this binary.
	SchemaVersion string
	Timeout       time.Duration // stop Run after this long; 0 means no limit
	ImportFrom    string        // import only: source export path
}

// Validate checks the fields the role needs.
func (c Config) Validate() error {
	switch c.Role {
	case RoleServer, RoleWorker:
		if c.DatabaseURL == "" {
			return fmt.Errorf("role %s: database url is empty: %w", c.Role, ErrConfig)
		}
		if c.DirectDatabaseURL == "" {
			return fmt.Errorf("role %s: direct database url is empty: %w", c.Role, ErrConfig)
		}
		if c.Zone == "" {
			return fmt.Errorf("role %s: zone is empty: %w", c.Role, ErrConfig)
		}
	case RoleImport:
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
	Flags    *flags.Set
	Registry registry.Booter // nil when built without one (Build)
	DB       db.DB
	Clock    clock.Clock
	Gates    gate.Engine
	Reports  report.Engine
	Server   server.Server
	Events   *webhook.Types
	Jobs     *Jobs // River workers and periodic jobs apps add at Register
	// Outbox delivers pending outbox messages to the registered consumers (kind outbox.relay).
	// Nil leaves the relay unscheduled.
	Outbox *outbox.Deliverer
	// Emitter writes outbox events in a caller's transaction (kernel jobs that emit events).
	Emitter outbox.Emitter
	// Notify is the notification service; its delivery worker runs as an outbox consumer.
	Notify *notify.Service
	// NotifySender delivers rendered notifications. Nil means a sender that only counts.
	NotifySender notify.Sender
	// WebhookExpiry is the outbound endpoint expiry store (kind webhook.endpoint_expiry).
	WebhookExpiry webhook.ExpiryStore
	// Dispatcher queues outbound webhook deliveries in order per (endpoint, subject). Nil
	// leaves kind webhook.deliver out.
	Dispatcher WebhookQueue
	// WebhookEndpoints lists the endpoints to drain; WebhookPoster sends one delivery. Both
	// are required when Dispatcher is set.
	WebhookEndpoints WebhookEndpoints
	WebhookPoster    WebhookPoster
	// AdminStore applies approved role changes and user activations. Admin is the identity
	// admin gates built by AdminGates.
	AdminStore identity.AdminStore
	Admin      identity.AdminGates

	sealed bool // Registry.Seal passed; apply runs only then
	// coreKinds are the core.* gate kinds apps handed to CoreGateKind (KERNEL-CORE).
	coreKinds []registry.GateKind
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
	Outbox   *outbox.Deliverer
	Emitter  outbox.Emitter
	Notify   *notify.Service
	// NotifySender is optional; nil keeps the count-only sender.
	NotifySender  notify.Sender
	WebhookExpiry webhook.ExpiryStore
	// Dispatcher is optional; nil leaves outbound webhook delivery off.
	Dispatcher       WebhookQueue
	WebhookEndpoints WebhookEndpoints
	WebhookPoster    WebhookPoster
	// AdminStore is optional; it applies approved identity admin changes.
	AdminStore identity.AdminStore
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
		Jobs: NewJobs(), Outbox: deps.Outbox, Emitter: deps.Emitter, Notify: deps.Notify,
		NotifySender: deps.NotifySender, WebhookExpiry: deps.WebhookExpiry,
		Dispatcher: deps.Dispatcher, WebhookEndpoints: deps.WebhookEndpoints, WebhookPoster: deps.WebhookPoster,
		AdminStore: deps.AdminStore,
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
