-- module: app/legal/conflict
-- Proposed party source citation (docs/QUESTIONS.md Q-DRAFT-FIX). Additive only: a nullable
-- column. A party the agent proposes from a document keeps where it was read: the cited
-- document version, page and lines as a JSON object ({version_id, page, from_line,
-- to_line}). It holds a pointer only, never document text. NULL for a party entered by hand.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE conflict.check_party ADD COLUMN source jsonb;

RESET lock_timeout;
RESET statement_timeout;
