-- contract: 2026.11
-- The release before this one stopped reading legacy_ref in every cell.
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item DROP COLUMN legacy_ref;
ALTER TABLE work.work_item RENAME COLUMN title TO subject;
