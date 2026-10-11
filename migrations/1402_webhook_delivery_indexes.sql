-- module: kernel/webhook
-- migrate: no-transaction
-- Indexes for 1400_webhook_delivery_subject.sql, built CONCURRENTLY so writes go on (MIG-061).
-- The file runs outside a transaction, one statement at a time. IF NOT EXISTS makes a
-- retry a no-op; drop an INVALID index left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS webhook_delivery_pending_idx ON platform.webhook_delivery (webhook_id, subject, at)
  WHERE acked_at IS NULL AND subject IS NOT NULL;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS webhook_delivery_attempt_uq ON platform.webhook_delivery (webhook_id, message_id, attempt)
  WHERE message_id IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
