package boot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dagi377/counsel/kernel/flags"
	"github.com/dagi377/counsel/migrations"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{name: "server", cfg: Config{Role: RoleServer}},
		{name: "worker", cfg: Config{Role: RoleWorker}},
		{name: "import", cfg: Config{Role: RoleImport}},
		{name: "migrate expand", cfg: Config{Role: RoleMigrate, MigrateMode: MigrateExpand}},
		{name: "migrate contract", cfg: Config{Role: RoleMigrate, MigrateMode: MigrateContract}},
		{name: "migrate wait-version", cfg: Config{Role: RoleMigrate, MigrateMode: MigrateWaitVersion}},
		{name: "migrate no mode", cfg: Config{Role: RoleMigrate}, want: ErrConfig},
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
	define := App{Name: "a", Register: func(_ context.Context, k *Kernel) error {
		return k.Flags.Define(flags.Def{Name: "a.flag", Owner: "test"})
	}}
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
			err := Run(context.Background(), Config{Role: RoleServer, Flags: tt.flags}, define)
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
