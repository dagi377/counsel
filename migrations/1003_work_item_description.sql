-- module: kernel/work
-- Work item description (TOOL16-DESC). Additive: work.work_item.description already exists as
-- a nullable text column (0001_init.sql); kernel/work Item.Description now writes it. This adds
-- the length limit Item.Description is checked against (MaxDescriptionLen, the create_task
-- description_markdown maximum) and keeps a blank description out (it is NULL instead). Added
-- NOT VALID and validated in 1004 (MIG-061).
SET lock_timeout = '2s';
SET statement_timeout = '5s';

COMMENT ON COLUMN work.work_item.description IS 'Markdown body of the item; NULL when none.';
ALTER TABLE work.work_item
  ADD CONSTRAINT work_item_description_ck
  CHECK (description IS NULL OR (char_length(description) <= 10000 AND btrim(description) <> '')) NOT VALID;

RESET lock_timeout;
RESET statement_timeout;
