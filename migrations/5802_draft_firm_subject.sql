-- module: app/legal/docs
-- Firm-level drafts (docs/QUESTIONS.md Q-FORM-SUBJECT). A draft on the firm subject, such as
-- the public wording of an intake form (F-INT-12), belongs to no matter, so matter_id may be
-- null. draft_document stores a null matter only with subject_ref "firm" and never borrows a
-- matter for it. Relaxing NOT NULL rewrites no rows.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE docs.draft ALTER COLUMN matter_id DROP NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
