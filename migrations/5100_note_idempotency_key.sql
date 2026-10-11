-- module: app/legal/matter
-- Draft note idempotency key (docs/QUESTIONS.md Q-DRAFT-FIX). Additive only: a nullable column.
-- An agent draft_note call carries an idempotency key; a retried call in the same run with
-- the same key returns the note it already stored instead of storing a second one. The
-- unique index is built in 5101.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE matter.note ADD COLUMN idempotency_key text;

RESET lock_timeout;
RESET statement_timeout;
