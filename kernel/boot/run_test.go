package boot

import (
	"context"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dagi377/counsel/kernel/db"
	"github.com/dagi377/counsel/kernel/db/dbfake"
	"github.com/dagi377/counsel/kernel/gate"
	"github.com/dagi377/counsel/kernel/gate/gatefake"
	"github.com/dagi377/counsel/kernel/notify"
	"github.com/dagi377/counsel/kernel/outbox"
	"github.com/dagi377/counsel/kernel/outbox/outboxfake"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/kernel/server"
)

// runRig is a fake env that records the order of boot steps.
type runRig struct {
	steps  []string
	opened db.Config
	worker WorkerConfig
	// consumers are the outbox consumer names the kernel had when its role ran.
	consumers []string
	openErr   error
}

func (r *runRig) env(t *testing.T) env {
	t.Helper()
	schemas, err := registry.LoadSchemas(os.DirFS("../../spec/kernel"))
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	return env{
		open: func(_ context.Context, cfg db.Config) (db.DB, error) {
			r.opened = cfg
			if r.openErr != nil {
				return nil, r.openErr
			}
			r.steps = append(r.steps, "open")
			return dbfake.New(), nil
		},
		registry: func() (registry.Booter, error) { return registry.NewBoot(schemas), nil },
		gates:    func(db.DB) (gate.Engine, error) { return gatefake.New(time.Now, nil), nil },
		outbox: func(pool db.DB) (*outbox.Deliverer, error) {
			ob := outboxfake.New()
			return &outbox.Deliverer{Relay: ob, Inbox: ob, DB: pool}, nil
		},
		server: func(_ context.Context, _ Config, k *Kernel) error {
			r.steps = append(r.steps, "role:server")
			for _, c := range k.Outbox.Consumers {
				r.consumers = append(r.consumers, c.Name)
			}
			return nil
		},
		worker: func(_ context.Context, _ *Kernel, cfg WorkerConfig) error {
			r.steps = append(r.steps, "role:worker")
			r.worker = cfg
			return nil
		},
	}
}

func baseConfig(role Role) Config {
	return Config{Role: role, Addr: "127.0.0.1:0", Zone: "America/Toronto",
		DatabaseURL:       "postgres://app:s3cr3t-pw@pooler:6432/counsel",
		DirectDatabaseURL: "postgres://app:s3cr3t-pw@db:5432/counsel"}
}

// routeApp registers one route and, on Start, records whether apply already mounted it.
func routeApp(r *runRig, startErr error) App {
	return App{
		Name: "fakeapp",
		Register: func(_ context.Context, k *Kernel) error {
			r.steps = append(r.steps, "register")
			k.Registry.Routes(func(rt chi.Router) { rt.Get("/ping", func(http.ResponseWriter, *http.Request) {}) })
			return nil
		},
		Start: func(_ context.Context, k *Kernel) error {
			mounted := slices.Contains(k.Server.Routes(), server.Route{Method: "GET", Pattern: "/v1/ping"})
			r.steps = append(r.steps, "start:applied="+map[bool]string{true: "yes", false: "no"}[mounted])
			return startErr
		},
	}
}

func TestRunOrder(t *testing.T) {
	errStart := errors.New("start failed")
	errOpen := errors.New("dial refused")
	tests := []struct {
		name      string
		role      Role
		startErr  error
		openErr   error
		wantSteps []string
		wantErr   error
		wantMsg   string
	}{
		{name: "server: start runs after apply and before the role", role: RoleServer,
			wantSteps: []string{"open", "register", "start:applied=yes", "role:server"}},
		{name: "worker: same order", role: RoleWorker,
			wantSteps: []string{"open", "register", "start:applied=yes", "role:worker"}},
		{name: "start error stops run and names the app", role: RoleServer, startErr: errStart,
			wantSteps: []string{"open", "register", "start:applied=yes"}, wantErr: errStart, wantMsg: `"fakeapp"`},
		{name: "open error stops run before register", role: RoleServer, openErr: errOpen,
			wantSteps: nil, wantErr: errOpen, wantMsg: "open database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := &runRig{openErr: tt.openErr}
			err := run(context.Background(), baseConfig(tt.role), rig.env(t), routeApp(rig, tt.startErr))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tt.wantMsg) {
					t.Errorf("run() = %v, want it to mention %s", err, tt.wantMsg)
				}
				if strings.Contains(err.Error(), "s3cr3t") {
					t.Errorf("run() error carries the database URL: %v", err)
				}
			}
			if !slices.Equal(rig.steps, tt.wantSteps) {
				t.Errorf("steps = %v, want %v", rig.steps, tt.wantSteps)
			}
		})
	}
}

func TestRunWorkerConfig(t *testing.T) {
	tests := []struct {
		name       string
		maxWorkers int
		want       int
	}{
		{"default", 0, 10},
		{"negative is default", -1, 10},
		{"set", 3, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := &runRig{}
			cfg := baseConfig(RoleWorker)
			cfg.MaxWorkers = tt.maxWorkers
			if err := run(context.Background(), cfg, rig.env(t), routeApp(rig, nil)); err != nil {
				t.Fatalf("run() = %v", err)
			}
			want := db.Config{PooledDSN: cfg.DatabaseURL, DirectDSN: cfg.DirectDatabaseURL}
			if rig.worker.MaxWorkers != tt.want || rig.worker.DB != want {
				t.Errorf("worker config = %+v, want MaxWorkers %d and DB %+v", rig.worker, tt.want, want)
			}
			if rig.opened != want {
				t.Errorf("opened %+v, want %+v", rig.opened, want)
			}
		})
	}
}

func TestRunRolesNotImplemented(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"import", Config{Role: RoleImport}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := &runRig{}
			app := App{Name: "a", Register: func(context.Context, *Kernel) error { return nil }}
			err := run(context.Background(), tt.cfg, rig.env(t), app)
			if !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("run() = %v, want ErrNotImplemented", err)
			}
			if slices.Contains(rig.steps, "open") {
				t.Errorf("%s opened the database", tt.name)
			}
		})
	}
}

func TestRunBadZone(t *testing.T) {
	rig := &runRig{}
	cfg := baseConfig(RoleServer)
	cfg.Zone = "Not/AZone"
	err := run(context.Background(), cfg, rig.env(t), routeApp(rig, nil))
	if err == nil || slices.Contains(rig.steps, "register") {
		t.Fatalf("run() = %v, steps %v; want an error before any register", err, rig.steps)
	}
}

func TestPGGatesIsTheEngine(t *testing.T) {
	tests := []struct {
		name    string
		pool    db.DB
		wantErr error
	}{
		{"database wired", dbfake.New(), nil},
		{"no database", nil, ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := pgGates(tt.pool)
			if !errors.Is(err, tt.wantErr) || (tt.wantErr == nil && err != nil) {
				t.Fatalf("pgGates = %v, want %v", err, tt.wantErr)
			}
			if errors.Is(err, ErrNotImplemented) {
				t.Fatal("pgGates still returns ErrNotImplemented")
			}
			if tt.wantErr == nil {
				if _, ok := eng.(*gate.PG); !ok {
					t.Fatalf("pgGates = %T, want *gate.PG", eng)
				}
			}
		})
	}
}

// Run loads the registry schemas from the binary, so it boots from any working directory.
func TestProductionRegistryFromAnyDir(t *testing.T) {
	tests := []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{"empty temp dir", func(t *testing.T) string { return t.TempDir() }},
		{"filesystem root", func(*testing.T) string { return "/" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.dir(t))
			reg, err := productionEnv().registry()
			if err != nil {
				t.Fatalf("registry() from %s: %v", tt.name, err)
			}
			if reg == nil {
				t.Fatal("registry() = nil")
			}
		})
	}
}

// Run puts the notification delivery consumer on the outbox relay, so a notification
// emitted by an app is delivered by the worker (addNotifyJobs runs inside Run).
func TestRunAddsNotifyConsumer(t *testing.T) {
	rig := &runRig{}
	if err := run(context.Background(), baseConfig(RoleServer), rig.env(t), routeApp(rig, nil)); err != nil {
		t.Fatalf("run() = %v", err)
	}
	if !slices.Contains(rig.consumers, notify.ConsumerName) {
		t.Fatalf("outbox consumers = %v, want %s", rig.consumers, notify.ConsumerName)
	}
}

func TestPGOutboxNeedsPlainStatements(t *testing.T) {
	if _, err := pgOutbox(dbfake.New()); !errors.Is(err, ErrConfig) {
		t.Fatalf("pgOutbox(fake without Exec) = %v, want %v", err, ErrConfig)
	}
	if _, err := pgOutbox(&db.Pool{}); err != nil {
		t.Fatalf("pgOutbox(*db.Pool) = %v, want nil", err)
	}
}
