-- expect: MIG-061 must be in its own file
-- migrate: no-transaction
SET lock_timeout = '2s';
SET statement_timeout = '60s';
CREATE INDEX CONCURRENTLY ix_work_item_state ON work.work_item (state);
ALTER TABLE work.work_item ADD COLUMN note_ref text;
