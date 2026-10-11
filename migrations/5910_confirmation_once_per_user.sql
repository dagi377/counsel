-- module: app/legal/deadlines
-- migrate: no-transaction
-- cal.change uniqueness per gate (GE3-legal), step 1 of 3. Every confirmation kind except
-- change stays unique per (deadline, user, kind). Built CONCURRENTLY (MIG-061); IF NOT EXISTS
-- makes a retry a no-op. 5912 drops the old index deadline_confirmation_once.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS deadline_confirmation_once_per_user
  ON cal.deadline_confirmation (deadline_id, user_id, kind)
  WHERE kind <> 'change';

RESET lock_timeout;
RESET statement_timeout;
