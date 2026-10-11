SET LOCAL lock_timeout TO 2000;
SET LOCAL statement_timeout TO '5min';
ALTER TABLE work.work_item ADD CONSTRAINT work_item_label_fk FOREIGN KEY (label_id) REFERENCES work.work_label (id) NOT VALID;
ALTER TABLE work.work_item ADD CONSTRAINT work_item_rank_ck CHECK (rank >= 0) NOT VALID;
ALTER TABLE work.work_item ADD CONSTRAINT work_item_ref_uq UNIQUE USING INDEX ix_work_item_ref;
