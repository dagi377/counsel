-- migrate: no-transaction
SET lock_timeout = '1500ms';
SET statement_timeout = '30min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS ix_work_item_ref ON work.work_item (external_ref);
