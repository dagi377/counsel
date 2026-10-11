-- module: kernel/work
-- Validates the constraint 1003_work_item_description.sql added NOT VALID (MIG-061: VALIDATE
-- runs in a later file than its NOT VALID add).
SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_description_ck;

RESET lock_timeout;
RESET statement_timeout;
