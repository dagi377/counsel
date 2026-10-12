-- module: app/legal/billing
-- Held draft line (AGENT-PROD-3). Additive only: a column with a default. A line put on hold
-- (BillLine action "hold", G-B1) stays on the draft with its status changed; bill lines are
-- never deleted. A held line counts toward no total, tax or journal line of its bill.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE billing.bill_line ADD COLUMN on_hold boolean NOT NULL DEFAULT false;

RESET lock_timeout;
RESET statement_timeout;
