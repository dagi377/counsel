-- Validates constraints added NOT VALID by an earlier migration.
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_label_fk;
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_rank_ck;
