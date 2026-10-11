-- module: app/legal/trust
-- migrate: no-transaction
-- Index for 5502_withdrawal_authority.sql, built CONCURRENTLY so writes go on (MIG-061).
-- The file runs outside a transaction, one statement at a time. IF NOT EXISTS makes a
-- retry a no-op; drop an INVALID index left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS withdrawal_request_expiry_idx ON trust.withdrawal_request (expires_at)
  WHERE status = 'authorized';

RESET lock_timeout;
RESET statement_timeout;
