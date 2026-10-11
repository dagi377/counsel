-- module: app/legal/billing
-- Validates the status check 5620_write_off_draft_outcome.sql added NOT VALID (MIG-061: VALIDATE
-- runs in a later file than its NOT VALID add).
SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE billing.write_off_draft VALIDATE CONSTRAINT write_off_draft_status_ck;

RESET lock_timeout;
RESET statement_timeout;
