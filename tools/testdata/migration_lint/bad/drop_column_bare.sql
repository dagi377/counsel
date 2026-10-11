-- expect: MIG-060 DROP COLUMN
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE ONLY work.work_item ADD COLUMN note_ref text, DROP legacy_ref;
