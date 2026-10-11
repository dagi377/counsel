-- module: app/legal/docs
-- Draft document idempotency key (docs/QUESTIONS.md Q-DRAFT-FIX). Additive only: a nullable
-- column. A retried draft_document call in the same run with the same key returns the draft
-- it already stored instead of storing a second one. The unique index is built in 5801.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE docs.draft ADD COLUMN idempotency_key text;

RESET lock_timeout;
RESET statement_timeout;
