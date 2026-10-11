-- expect: MIG-061 must not open a transaction
-- migrate: no-transaction
SET lock_timeout = '2s';
SET statement_timeout = '60s';
BEGIN;
CREATE INDEX CONCURRENTLY ix_work_item_state ON work.work_item (state);
COMMIT;
