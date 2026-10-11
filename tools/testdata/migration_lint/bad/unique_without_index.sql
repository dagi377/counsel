-- expect: MIG-061 USING INDEX
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD CONSTRAINT work_item_ref_uq UNIQUE (external_ref);
