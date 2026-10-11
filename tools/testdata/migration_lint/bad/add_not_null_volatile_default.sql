-- expect: MIG-060 ADD COLUMN NOT NULL
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD COLUMN seen_at timestamptz NOT NULL DEFAULT now();
