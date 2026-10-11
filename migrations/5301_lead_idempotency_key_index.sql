-- module: app/legal/intake
-- migrate: no-transaction
-- Index for 5300_lead_idempotency_key.sql, built CONCURRENTLY so writes go on (MIG-061).
-- One lead per form and key. IF NOT EXISTS makes a retry a no-op; drop an INVALID index left
-- by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS lead_form_idempotency_uq
  ON intake.lead (form_id, idempotency_key)
  WHERE form_id IS NOT NULL AND idempotency_key IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
