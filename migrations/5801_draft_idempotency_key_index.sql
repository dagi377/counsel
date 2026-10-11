-- module: app/legal/docs
-- migrate: no-transaction
-- Index for 5800_draft_idempotency_key.sql, built CONCURRENTLY so writes go on (MIG-061).
-- One draft per run and key. IF NOT EXISTS makes a retry a no-op; drop an INVALID index
-- left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS draft_run_idempotency_uq
  ON docs.draft (agent_run_id, idempotency_key)
  WHERE agent_run_id IS NOT NULL AND idempotency_key IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
