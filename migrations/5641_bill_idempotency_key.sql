-- module: app/legal/billing
-- Draft bill idempotency key (AGENT-PROD-2a). Additive only: a nullable column. A batch run or
-- agent call that builds a draft again under the same key (batch.Key, BIL-C-110) gets the
-- first draft back instead of a second one. The unique index is built in 5642.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE billing.bill ADD COLUMN idempotency_key text;

RESET lock_timeout;
RESET statement_timeout;
