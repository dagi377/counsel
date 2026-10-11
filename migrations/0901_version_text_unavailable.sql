-- module: kernel/docs
-- Text-unavailable versions (AGENT-PROD-3). Additive only: a nullable column. A version whose
-- text could not be obtained (no native text and extraction failed or is not possible) gets a
-- row with an empty body and the reason, so it reads as text-unavailable, never as empty text.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE docs.version_text ADD COLUMN unavailable_reason text;

RESET lock_timeout;
RESET statement_timeout;
