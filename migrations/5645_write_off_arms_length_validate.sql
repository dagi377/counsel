-- module: app/legal/billing
-- Validates the arm's-length check 5644_write_off_arms_length.sql added NOT VALID (MIG-061:
-- VALIDATE runs in a later file than its NOT VALID add).
SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE billing.write_off_draft VALIDATE CONSTRAINT write_off_draft_arms_length_ck;

RESET lock_timeout;
RESET statement_timeout;
