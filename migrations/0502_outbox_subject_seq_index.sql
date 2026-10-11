-- module: kernel/outbox
-- migrate: no-transaction
-- Indexes for 0500_outbox_subject_seq.sql, built CONCURRENTLY so writes go on (MIG-061).
-- The file runs outside a transaction, one statement at a time. IF NOT EXISTS makes a
-- retry a no-op; drop an INVALID index left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS outbox_subject_seq_uq ON platform.outbox (aggregate, aggregate_id, subject_seq)
  WHERE subject_seq IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
