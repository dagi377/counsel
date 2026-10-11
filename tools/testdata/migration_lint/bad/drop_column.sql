-- expect: MIG-060 DROP COLUMN
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item DROP COLUMN legacy_ref;
