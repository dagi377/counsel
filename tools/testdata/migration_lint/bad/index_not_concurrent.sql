-- expect: MIG-061 without CONCURRENTLY
SET lock_timeout = '2s';
SET statement_timeout = '60s';
CREATE INDEX ix_work_item_state ON work.work_item (state);
