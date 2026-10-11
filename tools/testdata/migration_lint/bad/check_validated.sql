-- expect: MIG-061 ADD CHECK on existing table work.work_item must be NOT VALID
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD CONSTRAINT work_item_rank_ck CHECK (rank >= 0);
