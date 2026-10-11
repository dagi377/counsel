package boot

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

// fakeApplier records each call instead of touching a database.
type fakeApplier struct {
	calls []string // "<mode> <dsn>"
	err   error
}

func (f *fakeApplier) apply(_ context.Context, dsn string, mode MigrateMode) error {
	f.calls = append(f.calls, string(mode)+" "+dsn)
	return f.err
}

func TestRunMigrate(t *testing.T) {
	errApply := errors.New("apply migration 0302_x.sql: syntax error")
	tests := []struct {
		name      string
		cfg       Config
		applyErr  error
		wantCalls []string
		wantErr   error
	}{
		{name: "expand on the direct dsn",
			cfg:       Config{Role: RoleMigrate, MigrateMode: MigrateExpand, DatabaseURL: "pooled", DirectDatabaseURL: "direct"},
			wantCalls: []string{"expand direct"}},
		{name: "contract on the only dsn",
			cfg:       Config{Role: RoleMigrate, MigrateMode: MigrateContract, DatabaseURL: "pooled"},
			wantCalls: []string{"contract pooled"}},
		{name: "apply error is returned so the process exits non-zero",
			cfg:      Config{Role: RoleMigrate, MigrateMode: MigrateExpand, DatabaseURL: "pooled"},
			applyErr: errApply, wantCalls: []string{"expand pooled"}, wantErr: errApply},
		{name: "unknown mode applies nothing",
			cfg:     Config{Role: RoleMigrate, MigrateMode: "sideways", DatabaseURL: "pooled"},
			wantErr: ErrConfig},
		{name: "no database applies nothing",
			cfg:     Config{Role: RoleMigrate, MigrateMode: MigrateExpand},
			wantErr: ErrConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rig := &runRig{}
			e := rig.env(t)
			fake := &fakeApplier{err: tt.applyErr}
			e.apply = fake.apply
			registered := false
			app := App{Name: "a", Register: func(context.Context, *Kernel) error { registered = true; return nil }}
			err := run(context.Background(), tt.cfg, e, app)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("run() = %v, want %v", err, tt.wantErr)
			}
			if !slices.Equal(fake.calls, tt.wantCalls) {
				t.Fatalf("apply calls = %v, want %v", fake.calls, tt.wantCalls)
			}
			if registered || slices.Contains(rig.steps, "open") {
				t.Fatalf("migrate built apps (%v) or opened the pool (steps %v)", registered, rig.steps)
			}
		})
	}
}

// The DSN carries the migrator password; it never reaches a log line or the returned error.
func TestRunMigrateNeverLogsDSN(t *testing.T) {
	const secret = "s3cr3t-pw"
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	fake := &fakeApplier{err: errors.New("apply migration 0001_init.sql: boom")}
	cfg := Config{Role: RoleMigrate, MigrateMode: MigrateExpand, DirectDatabaseURL: "postgres://migrator:" + secret + "@db:5432/counsel"}
	err := runMigrate(context.Background(), cfg, fake.apply)
	if err == nil {
		t.Fatal("runMigrate() = nil, want the apply error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(buf.String(), secret) {
		t.Fatalf("dsn leaked: err %q, log %q", err, buf.String())
	}
}

// The production applier fails with ErrConnect, naming no DSN, when the database is down.
func TestPGApplyConnectError(t *testing.T) {
	const secret = "s3cr3t-pw"
	err := pgApply(context.Background(), "postgres://migrator:"+secret+"@127.0.0.1:1/none?connect_timeout=1", MigrateExpand)
	if !errors.Is(err, ErrConnect) || strings.Contains(err.Error(), secret) {
		t.Fatalf("pgApply() = %v, want ErrConnect without the dsn", err)
	}
}
