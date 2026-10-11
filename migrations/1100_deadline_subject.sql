-- module: kernel/deadline
-- Kernel deadlines on registered subjects (docs/QUESTIONS.md Q-PORT-20). Additive only.
-- The kernel stores a deadline against any registered subject through (subject_kind,
-- subject_id), and keeps the pack code, pack version, rule key and trigger date it was
-- computed from, so a new pack version can re-compute it. The app's own link columns stay
-- for the app; the app maps the pair onto them in its own migration
-- (5900_deadline_subject_link.sql). Constraints are added NOT VALID and validated in
-- 1101_deadline_subject_validate.sql.
SET LOCAL lock_timeout TO '2s';
SET LOCAL statement_timeout TO '60s';

ALTER TABLE cal.deadline
  ADD COLUMN subject_kind text,
  ADD COLUMN subject_id   uuid,
  ADD COLUMN pack_code    text,
  ADD COLUMN pack_version text,
  ADD COLUMN rule_key     text,
  ADD COLUMN trigger_on   date;
ALTER TABLE cal.deadline ADD CONSTRAINT deadline_subject_pair_ck
  CHECK ((subject_kind IS NULL) = (subject_id IS NULL)) NOT VALID;
ALTER TABLE cal.deadline ADD CONSTRAINT deadline_subject_kind_ck
  CHECK (subject_kind IS NULL OR subject_kind ~ '^[a-z][a-z0-9_.]*$') NOT VALID;
ALTER TABLE cal.deadline ADD CONSTRAINT deadline_pack_rule_ck
  CHECK ((pack_code IS NULL) = (pack_version IS NULL)
     AND (pack_code IS NULL) = (rule_key IS NULL)
     AND (pack_code IS NULL) = (trigger_on IS NULL)) NOT VALID;
