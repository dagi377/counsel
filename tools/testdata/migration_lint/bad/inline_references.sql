-- expect: MIG-061 inline REFERENCES/CHECK
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD COLUMN owner_id bigint REFERENCES work.work_owner (id);
