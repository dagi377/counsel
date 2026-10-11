-- module: app/legal/billing
-- migrate: no-transaction
-- Index for 5641_bill_idempotency_key.sql, built CONCURRENTLY so writes go on (MIG-061).
-- One draft per key. IF NOT EXISTS makes a retry a no-op; drop an INVALID index left by a
-- failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS bill_idempotency_uq
  ON billing.bill (idempotency_key)
  WHERE idempotency_key IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
