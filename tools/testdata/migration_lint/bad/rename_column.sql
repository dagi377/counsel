-- expect: MIG-060 RENAME
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item RENAME COLUMN title TO subject;
