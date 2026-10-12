package boot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// riverGrantSQL gives the app and worker roles what River's clients need on River's own
// tables: counseld inserts jobs (counsel_app) and counsel-worker fetches, completes, cleans
// and elects a leader (counsel_jobs). River's migration bookkeeping table is read-only for
// both. It runs after rivermigrate, in River's schema (the migrator's current schema, which
// is the one the clients resolve unqualified River names in), and is safe to run again.
//
// rule: MIG-060
// The step sets lock_timeout (at most 2 s) and statement_timeout like every numbered file.
const riverGrantSQL = `
SET lock_timeout = '2s';
SET statement_timeout = '5min';
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT c.relname, c.relkind FROM pg_class c
           WHERE c.relnamespace = current_schema()::regnamespace
             AND c.relname LIKE 'river\_%' AND c.relkind IN ('r', 'S')
  LOOP
    IF r.relkind = 'S' THEN
      EXECUTE format('GRANT USAGE, SELECT ON SEQUENCE %I TO counsel_app, counsel_jobs', r.relname);
    ELSIF r.relname = 'river_migration' THEN
      EXECUTE format('GRANT SELECT ON TABLE %I TO counsel_app, counsel_jobs', r.relname);
    ELSE
      EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE %I TO counsel_app, counsel_jobs', r.relname);
    END IF;
  END LOOP;
END $$;
RESET lock_timeout;
RESET statement_timeout;
`

// migrateRiver brings River's job tables (river_job, river_queue, river_leader, ...) up to
// the version of the linked River module and grants them to the app and worker roles. The
// expand step of counsel-migrate runs it after the numbered files (which create the roles),
// so a fresh database can queue and work jobs. rivermigrate records its versions in
// river_migration and skips applied ones, and the grants are idempotent, so it can run again.
// pgx errors can name the DSN host and user, so the connect error is not wrapped.
func migrateRiver(ctx context.Context, dsn string) error {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("river schema: parse database url: %w", ErrConfig)
	}
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("river schema: connect: %w", ErrConnect)
	}
	defer pool.Close()
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("river schema: migrator: %w", err)
	}
	res, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return fmt.Errorf("river schema: migrate up: %w", err)
	}
	if _, err := pool.Exec(ctx, riverGrantSQL); err != nil {
		return fmt.Errorf("river schema: grant: %w", err)
	}
	slog.InfoContext(ctx, "river schema migrated", "applied", len(res.Versions))
	return nil
}
