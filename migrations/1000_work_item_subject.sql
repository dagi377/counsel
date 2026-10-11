-- module: kernel/work
-- Work items on registered subjects (docs/QUESTIONS.md Q-T-034.1). Additive only.
-- The kernel links a work item to any registered subject through (subject_kind, subject_id).
-- The app's own link column stays for the app; the app backfills the pair from it in its
-- own migration (6100_work_item_subject_backfill.sql).

ALTER TABLE work.work_item
  ADD COLUMN subject_kind text,
  ADD COLUMN subject_id   uuid;
ALTER TABLE work.work_item
  ADD CONSTRAINT work_item_subject_pair_ck CHECK ((subject_kind IS NULL) = (subject_id IS NULL)) NOT VALID,
  ADD CONSTRAINT work_item_subject_kind_ck CHECK (subject_kind IS NULL OR subject_kind ~ '^[a-z][a-z0-9_.]*$') NOT VALID;
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_subject_pair_ck;
ALTER TABLE work.work_item VALIDATE CONSTRAINT work_item_subject_kind_ck;
CREATE INDEX work_item_subject_idx ON work.work_item (subject_kind, subject_id) WHERE archived_at IS NULL;
