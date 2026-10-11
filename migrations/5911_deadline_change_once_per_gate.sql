-- module: app/legal/deadlines
-- migrate: no-transaction
-- cal.change uniqueness per gate (GE3-legal), step 2 of 3. A change is logged once per
-- approving gate, so one deadline can change more than once, also by the same approver.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS deadline_change_once_per_gate
  ON cal.deadline_confirmation (deadline_id, gate_decision_id)
  WHERE kind = 'change';

RESET lock_timeout;
RESET statement_timeout;
