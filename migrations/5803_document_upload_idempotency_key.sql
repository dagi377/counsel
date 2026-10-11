-- module: app/legal/docs
-- Upload idempotency key (DOC-UPLOAD). Additive only: a nullable column. A retried
-- POST /documents by the same member with the same Idempotency-Key returns the document it
-- already stored instead of storing a second one. The unique index is built in 5804.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE docs.document ADD COLUMN idempotency_key text;

RESET lock_timeout;
RESET statement_timeout;
