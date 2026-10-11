-- Expand: new table, nullable column, NOT NULL with a constant default.
SET lock_timeout = '2s';
SET statement_timeout = '60s';
CREATE TABLE work.work_label (
    id bigint PRIMARY KEY,
    name text
);
ALTER TABLE work.work_label ALTER COLUMN name SET NOT NULL;
ALTER TABLE work.work_label ADD COLUMN weight int NOT NULL;
ALTER TABLE work.work_item ADD COLUMN label_id bigint;
ALTER TABLE work.work_item ADD COLUMN state text NOT NULL DEFAULT 'open';
ALTER TABLE work.work_item ADD COLUMN rank int NOT NULL DEFAULT 0;
ALTER TABLE work.work_item ADD COLUMN flagged boolean DEFAULT false NOT NULL;
ALTER TABLE work.work_item ADD COLUMN kind text DEFAULT 'task'::text NOT NULL;
ALTER TABLE work.work_item ALTER COLUMN state DROP DEFAULT;
