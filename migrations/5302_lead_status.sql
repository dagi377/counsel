-- module: app/legal/intake
-- Lead status (docs/QUESTIONS.md Q-EVENTS-WIRE-6). Additive only: a column with a constant
-- default. A lead that is open or awaiting info; converted stays derived from
-- converted_matter_id. Moving into awaiting_info publishes intake.lead_awaiting_info.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE intake.lead ADD COLUMN status text NOT NULL DEFAULT 'open';
ALTER TABLE intake.lead ADD CONSTRAINT lead_status_ck
  CHECK (status IN ('open', 'awaiting_info', 'declined', 'sealed_prospect')) NOT VALID;

RESET lock_timeout;
RESET statement_timeout;
