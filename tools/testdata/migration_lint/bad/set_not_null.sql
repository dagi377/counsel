-- expect: MIG-060 SET NOT NULL
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ALTER COLUMN subject_id SET NOT NULL;
