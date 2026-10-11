-- module: kernel/outbox
-- Gap-free per-subject sequence on the outbox (docs/QUESTIONS.md Q-T-008.4). Additive only.
-- subject_seq numbers the messages of one (aggregate, aggregate_id) from 1 with no gaps, so
-- consumers can order and detect missing messages. Rows written before this migration, and
-- by a release that does not set it yet, keep NULL.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE platform.outbox
  ADD COLUMN subject_seq bigint;
ALTER TABLE platform.outbox
  ADD CONSTRAINT outbox_subject_seq_ck CHECK (subject_seq IS NULL OR subject_seq >= 1) NOT VALID;

RESET lock_timeout;
RESET statement_timeout;
