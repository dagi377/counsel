-- module: app/legal/billing
-- Arm's-length determination on a draft write-off (LAW-P5, BIL-T-080). Additive only: a new
-- nullable column. ETA s.231 bad-debt relief is available only when the client deals with the
-- firm at arm's length; the determination is recorded when the write-off is drafted. NULL
-- means not checked, and the G-B5 effect refuses to approve such a write-off. The value check
-- is added NOT VALID and validated in 5645 (MIG-061).
SET lock_timeout = '2s';
SET statement_timeout = '5s';

ALTER TABLE billing.write_off_draft ADD COLUMN arms_length text;
ALTER TABLE billing.write_off_draft
  ADD CONSTRAINT write_off_draft_arms_length_ck CHECK (arms_length IN ('arms_length','not_arms_length')) NOT VALID;

RESET lock_timeout;
RESET statement_timeout;
