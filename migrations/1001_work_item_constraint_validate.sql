-- module: kernel/work
-- Validates the constraints 1000_work_item_subject.sql added NOT VALID (MIG-061: VALIDATE runs in a later file
-- than its NOT VALID add). VALIDATE takes SHARE UPDATE EXCLUSIVE, so reads and writes go on.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_subject_pair_ck;
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_subject_kind_ck;

RESET lock_timeout;
RESET statement_timeout;
