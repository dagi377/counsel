-- expect: MIG-060 DROP COLUMN
-- expect: MIG-061 without CONCURRENTLY
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item DROP COLUMN legacy_ref;
CREATE INDEX ix_work_item_rank ON work.work_item (rank);
