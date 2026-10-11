-- expect: MIG-061 '-- migrate: no-transaction'
SET lock_timeout = '2s';
SET statement_timeout = '60s';
CREATE INDEX CONCURRENTLY ix_work_item_state ON work.work_item (state);
