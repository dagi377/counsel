-- expect: MIG-061 lock_timeout must be > 0 and <= 2000ms, got 3000ms
SET lock_timeout = '3s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD COLUMN note_ref text;
