-- module: app/legal/comms
-- Email draft run and idempotency key (docs/QUESTIONS.md Q-DRAFT-FIX). Additive only:
-- nullable columns.
-- agent_run_id is the run that drafted an unsent email. drafted_by_agent_run_id stays the
-- run of an approved outbound message (its CHECK needs an approval id), so a draft no
-- longer keeps its run id in participants. idempotency_key makes a retried draft_email call
-- in the same run return the draft it already stored. The unique index is built in 6002.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE comms.comm_item ADD COLUMN agent_run_id uuid;
ALTER TABLE comms.comm_item ADD COLUMN idempotency_key text;

RESET lock_timeout;
RESET statement_timeout;
