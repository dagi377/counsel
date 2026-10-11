-- module: app/legal/conflict
-- migrate: no-transaction
-- Index for 5200_check_run_detail.sql, built CONCURRENTLY so writes go on (MIG-061).
-- The file runs outside a transaction, one statement at a time. IF NOT EXISTS makes a
-- retry a no-op; drop an INVALID index left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS hit_run_ordinal ON conflict.hit (run_id, ordinal)
  WHERE ordinal IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
