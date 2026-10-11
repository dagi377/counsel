-- expect: MIG-061 ADD FOREIGN KEY on existing table work.work_item must be NOT VALID
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ADD CONSTRAINT work_item_label_fk FOREIGN KEY (label_id) REFERENCES work.work_label (id);
