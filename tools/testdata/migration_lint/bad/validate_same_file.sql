-- expect: MIG-061 VALIDATE CONSTRAINT work_item_label_fk must run in a later migration
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD CONSTRAINT work_item_label_fk FOREIGN KEY (label_id) REFERENCES work.work_label (id) NOT VALID;
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_label_fk;
