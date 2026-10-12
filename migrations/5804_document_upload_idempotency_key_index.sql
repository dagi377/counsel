-- module: app/legal/docs
-- migrate: no-transaction
-- Index for 5803_document_upload_idempotency_key.sql, built CONCURRENTLY so writes go on
-- (MIG-061). One document per creator and key. IF NOT EXISTS makes a retry a no-op; drop an
-- INVALID index left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS document_upload_idempotency_uq
  ON docs.document (created_by, idempotency_key)
  WHERE created_by IS NOT NULL AND idempotency_key IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
