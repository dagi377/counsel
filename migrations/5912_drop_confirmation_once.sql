-- module: app/legal/deadlines
-- migrate: no-transaction
-- cal.change uniqueness per gate (GE3-legal), step 3 of 3. 5910 and 5911 replace the
-- (deadline, user, kind) index, which refused a second change of one deadline by the same
-- approver. Dropped CONCURRENTLY; IF EXISTS makes a retry a no-op.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
DROP INDEX CONCURRENTLY IF EXISTS cal.deadline_confirmation_once;

RESET lock_timeout;
RESET statement_timeout;
