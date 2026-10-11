-- module: kernel/outbox
-- Gap-free per-subject sequence on the outbox (docs/QUESTIONS.md Q-T-008.4). Additive only.
-- subject_seq numbers the messages of one (aggregate, aggregate_id) from 1 with no gaps, so
-- consumers can order and detect missing messages. Rows written before this migration, and
-- by a release that does not set it yet, keep NULL.

ALTER TABLE platform.outbox
  ADD COLUMN subject_seq bigint;
ALTER TABLE platform.outbox
  ADD CONSTRAINT outbox_subject_seq_ck CHECK (subject_seq IS NULL OR subject_seq >= 1) NOT VALID;
ALTER TABLE platform.outbox VALIDATE CONSTRAINT outbox_subject_seq_ck;
CREATE UNIQUE INDEX outbox_subject_seq_uq ON platform.outbox (aggregate, aggregate_id, subject_seq)
  WHERE subject_seq IS NOT NULL;
