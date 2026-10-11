-- expect: MIG-061 missing SET lock_timeout
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD COLUMN note_ref text;
