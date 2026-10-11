package boot

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dagi377/counsel/migrations"
)

// waitPoll is how often wait-version reads the schema version.
const waitPoll = 2 * time.Second

// applier runs one migrate mode's files on the database at dsn and records each in
// schema_meta.applied. Tests replace it with a fake.
type applier func(ctx context.Context, dsn string, mode MigrateMode) error

// pgApply connects to dsn and applies the files of mode through the migrations package:
// expand runs the files without a "-- contract:" mark, contract runs only the marked ones.
// pgx errors can name the DSN host and user, so the connect error is not wrapped.
func pgApply(ctx context.Context, dsn string, mode MigrateMode) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect migration database: %w", ErrConnect)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	switch mode {
	case MigrateExpand:
		return migrations.ApplyExpand(ctx, conn)
	case MigrateContract:
		return migrations.ApplyContract(ctx, conn)
	}
	return fmt.Errorf("migrate mode %q: %w", mode, ErrConfig)
}

// runMigrate runs the migrate role. Expand and contract apply files with the migrator
// credentials over the direct (non-pooler) DSN when one is given (ARC-U-011); wait-version
// applies nothing and waits with the app credentials.
func runMigrate(ctx context.Context, cfg Config, apply applier) error {
	if cfg.MigrateMode == MigrateWaitVersion {
		return waitVersion(ctx, cfg)
	}
	dsn := cfg.DirectDatabaseURL
	if dsn == "" {
		dsn = cfg.DatabaseURL
	}
	if dsn == "" {
		return fmt.Errorf("run migrate %s: database url is empty: %w", cfg.MigrateMode, ErrConfig)
	}
	// rule: ARC-U-017
	// Contract (destructive) files run only on an explicit -mode contract.
	slog.InfoContext(ctx, "applying migrations", "mode", string(cfg.MigrateMode))
	if err := apply(ctx, dsn, cfg.MigrateMode); err != nil {
		return fmt.Errorf("run migrate %s: %w", cfg.MigrateMode, err)
	}
	slog.InfoContext(ctx, "migrations applied", "mode", string(cfg.MigrateMode))
	return nil
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
	dsn := cfg.DatabaseURL
	if dsn == "" {
		dsn = cfg.DirectDatabaseURL
	}
	slog.InfoContext(ctx, "waiting for schema version", "version", want)
	for {
		conn, err := pgx.Connect(ctx, dsn)
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
