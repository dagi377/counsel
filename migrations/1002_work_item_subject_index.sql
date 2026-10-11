-- module: kernel/work
-- migrate: no-transaction
-- Indexes for 1000_work_item_subject.sql, built CONCURRENTLY so writes go on (MIG-061).
-- The file runs outside a transaction, one statement at a time. IF NOT EXISTS makes a
-- retry a no-op; drop an INVALID index left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS work_item_subject_idx ON work.work_item (subject_kind, subject_id) WHERE archived_at IS NULL;

RESET lock_timeout;
RESET statement_timeout;
