-- expect: MIG-061 got 0ms
SET lock_timeout TO 0;
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD COLUMN note_ref text;
