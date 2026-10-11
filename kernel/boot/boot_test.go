package boot

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/kernel/registry"
	"github.com/dagi377/counsel/migrations"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{name: "server", cfg: Config{Role: RoleServer, DatabaseURL: "pooled", DirectDatabaseURL: "direct", Zone: "Etc/UTC"}},
		{name: "worker", cfg: Config{Role: RoleWorker, DatabaseURL: "pooled", DirectDatabaseURL: "direct", Zone: "Etc/UTC"}},
		{name: "server no database url", cfg: Config{Role: RoleServer, DirectDatabaseURL: "direct", Zone: "Etc/UTC"}, want: ErrConfig},
		{name: "server no direct database url", cfg: Config{Role: RoleServer, DatabaseURL: "pooled", Zone: "Etc/UTC"}, want: ErrConfig},
		{name: "server no zone", cfg: Config{Role: RoleServer, DatabaseURL: "pooled", DirectDatabaseURL: "direct"}, want: ErrConfig},
		{name: "worker no database url", cfg: Config{Role: RoleWorker, DirectDatabaseURL: "direct", Zone: "Etc/UTC"}, want: ErrConfig},
		{name: "worker no zone", cfg: Config{Role: RoleWorker, DatabaseURL: "pooled", DirectDatabaseURL: "direct"}, want: ErrConfig},
		{name: "import", cfg: Config{Role: RoleImport}},
		{name: "migrate expand", cfg: Config{Role: RoleMigrate, MigrateMode: MigrateExpand, DatabaseURL: "db"}},
		{name: "migrate contract direct only", cfg: Config{Role: RoleMigrate, MigrateMode: MigrateContract, DirectDatabaseURL: "direct"}},
		{name: "migrate wait-version", cfg: Config{Role: RoleMigrate, MigrateMode: MigrateWaitVersion, DatabaseURL: "db"}},
		{name: "migrate no mode", cfg: Config{Role: RoleMigrate, DatabaseURL: "db"}, want: ErrConfig},
		{name: "unknown role", cfg: Config{Role: "x"}, want: ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); !errors.Is(err, tt.want) {
				t.Fatalf("Validate() = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestBuildCallsAppRegister(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name      string
		apps      func(calls *[]string) []App
		wantCalls []string
		wantErr   error
	}{
		{name: "no apps", apps: func(*[]string) []App { return nil }},
		{name: "in order", apps: func(c *[]string) []App {
			return []App{
				{Name: "a", Register: func(context.Context, *Kernel) error { *c = append(*c, "a"); return nil }},
				{Name: "b", Register: func(context.Context, *Kernel) error { *c = append(*c, "b"); return nil }},
			}
		}, wantCalls: []string{"a", "b"}},
		{name: "error stops boot", apps: func(c *[]string) []App {
			return []App{
				{Name: "a", Register: func(context.Context, *Kernel) error { *c = append(*c, "a"); return errBoom }},
				{Name: "b", Register: func(context.Context, *Kernel) error { *c = append(*c, "b"); return nil }},
			}
		}, wantCalls: []string{"a"}, wantErr: errBoom},
		{name: "nil register", apps: func(*[]string) []App { return []App{{Name: "a"}} }, wantErr: ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls []string
			_, err := Build(context.Background(), tt.apps(&calls)...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Build() err = %v, want %v", err, tt.wantErr)
			}
			if len(calls) != len(tt.wantCalls) {
				t.Fatalf("calls = %v, want %v", calls, tt.wantCalls)
			}
			for i := range calls {
				if calls[i] != tt.wantCalls[i] {
					t.Fatalf("calls = %v, want %v", calls, tt.wantCalls)
				}
			}
		})
	}
}

func TestRunFlags(t *testing.T) {
	// Run builds through BuildWith, which turns flags on before any Register, so the app
	// declares its flag in Define (ARC-I-040).
	define := App{Name: "a",
		Define:   func(s *flags.Set) error { return s.Define(flags.Def{Name: "a.flag", Owner: "test"}) },
		Register: func(context.Context, *Kernel) error { return nil },
	}
	tests := []struct {
		name  string
		flags []string
		want  error
	}{
		{name: "no flags", want: ErrNotImplemented},
		{name: "defined flag", flags: []string{"a.flag"}, want: ErrNotImplemented},
		{name: "undefined flag", flags: []string{"b.flag"}, want: flags.ErrUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Import still builds with no database and stops at ErrNotImplemented, so it checks
			// flag handling without one.
			err := Run(context.Background(), Config{Role: RoleImport, Flags: tt.flags}, define)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Run() = %v, want %v", err, tt.want)
			}
		})
	}
}

// wait-version applies nothing and fails fast on a version this binary does not ship; with an
// unreachable database it keeps retrying until ctx ends.
func TestRunWaitVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    error
	}{
		{name: "unknown version", version: "9999_nothing.sql", want: migrations.ErrUnknownVersion},
		{name: "database unreachable until timeout", want: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			cfg := Config{Role: RoleMigrate, MigrateMode: MigrateWaitVersion, SchemaVersion: tt.version,
				DatabaseURL: "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"}
			err := Run(ctx, cfg)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Run() = %v, want %v", err, tt.want)
			}
		})
	}
}

// rule: ARC-I-040
// Each app defines its flags, then the requested flags turn on, then each app registers its
// own surface, then the registry seals once. A Register sees the flags that are on.
func TestARC_I_040_BuildWithOrder(t *testing.T) {
	tests := []struct {
		name      string
		on        []string
		wantSteps []string
		wantErr   error
	}{
		{
			name:      "flag off: gated registration skipped",
			wantSteps: []string{"define a", "define b", "register a off", "register b"},
		},
		{
			name:      "flag on: gated registration runs",
			on:        []string{"a.gated"},
			wantSteps: []string{"define a", "define b", "register a on", "register b"},
		},
		{
			name:      "undefined flag fails before any register",
			on:        []string{"x.unknown"},
			wantSteps: []string{"define a", "define b"},
			wantErr:   ErrConfig,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var steps []string
			apps := []App{
				{
					Name: "a",
					Define: func(s *flags.Set) error {
						steps = append(steps, "define a")
						return s.Define(flags.Def{Name: "a.gated", Owner: "test"})
					},
					Register: func(ctx context.Context, _ *Kernel) error {
						if flags.Enabled(ctx, "a.gated") {
							steps = append(steps, "register a on")
						} else {
							steps = append(steps, "register a off")
						}
						return nil
					},
				},
				{
					Name:     "b",
					Define:   func(*flags.Set) error { steps = append(steps, "define b"); return nil },
					Register: func(context.Context, *Kernel) error { steps = append(steps, "register b"); return nil },
				},
			}
			k, err := BuildWith(context.Background(), Deps{}, tt.on, apps...)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("BuildWith() err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && !errors.Is(err, flags.ErrUnknown) {
				t.Fatalf("BuildWith() err = %v, want it to wrap %v too", err, flags.ErrUnknown)
			}
			if !reflect.DeepEqual(steps, tt.wantSteps) {
				t.Fatalf("steps = %v, want %v", steps, tt.wantSteps)
			}
			if err == nil && k.Flags.Enabled("a.gated") != (len(tt.on) > 0) {
				t.Fatalf("kernel flag a.gated = %v, want %v", k.Flags.Enabled("a.gated"), len(tt.on) > 0)
			}
		})
	}
}

func TestBuildWithSeal(t *testing.T) {
	schemas, err := registry.LoadSchemas(os.DirFS("../../spec/kernel"))
	if err != nil {
		t.Fatalf("LoadSchemas: %v", err)
	}
	errDefine := errors.New("define failed")
	tests := []struct {
		name     string
		define   func(*flags.Set) error
		register func(r registry.Registry)
		want     error
	}{
		{name: "valid registrations seal", register: func(r registry.Registry) {
			r.Capability(registry.Capability{ID: "desk.approve", Description: "approve"})
		}},
		{name: "duplicate id", register: func(r registry.Registry) {
			r.Capability(registry.Capability{ID: "desk.approve", Description: "approve"})
			r.Capability(registry.Capability{ID: "desk.approve", Description: "again"})
		}, want: registry.ErrDuplicateID},
		{name: "kernel prefix", register: func(r registry.Registry) {
			r.Capability(registry.Capability{ID: "core.anything", Description: "x"})
		}, want: registry.ErrKernelPrefix},
		{name: "invalid data", register: func(r registry.Registry) {
			r.Capability(registry.Capability{ID: "approve", Description: "x"})
		}, want: registry.ErrInvalid},
		{name: "define error stops boot", define: func(*flags.Set) error { return errDefine },
			register: func(registry.Registry) {}, want: errDefine},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := registry.NewBoot(schemas)
			app := App{Name: "desk", Define: tt.define, Register: func(_ context.Context, k *Kernel) error {
				tt.register(k.Registry)
				return nil
			}}
			k, err := BuildWith(context.Background(), Deps{Registry: b}, nil, app)
			if !errors.Is(err, tt.want) {
				t.Fatalf("BuildWith() err = %v, want %v", err, tt.want)
			}
			if tt.want == nil && k.Registry != registry.Booter(b) {
				t.Fatal("kernel Registry is not the Deps registry")
			}
			if tt.want == nil {
				// The registry is sealed: a later registration is reported as ErrSealed.
				b.Capability(registry.Capability{ID: "desk.late", Description: "late"})
				if err := b.Seal(); !errors.Is(err, registry.ErrSealed) {
					t.Fatalf("Seal after boot = %v, want %v", err, registry.ErrSealed)
				}
			}
		})
	}
}
