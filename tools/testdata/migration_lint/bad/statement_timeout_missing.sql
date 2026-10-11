-- expect: MIG-061 missing SET statement_timeout
SET lock_timeout = '2s';
ALTER TABLE work.work_item ADD COLUMN note_ref text;
