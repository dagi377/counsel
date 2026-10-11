-- module: app/legal/intake
-- Lead idempotency key (docs/QUESTIONS.md Q-EVENTS-WIRE-4). Additive only: a nullable column.
-- A portal submission carries an idempotency key; a retried submission of the same form with
-- the same key returns the lead it already created. The unique index is built in 5301.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE intake.lead ADD COLUMN idempotency_key text;

RESET lock_timeout;
RESET statement_timeout;
