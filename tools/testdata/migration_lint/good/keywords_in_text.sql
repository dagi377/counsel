-- Keywords inside comments, strings and function bodies are not DDL.
-- DROP TABLE work.work_item;
SET lock_timeout = '2s';
SET statement_timeout = '60s';
/* ALTER TABLE work.work_item DROP COLUMN title; */
COMMENT ON TABLE work.work_label IS 'never DROP TABLE; never RENAME';
CREATE FUNCTION work.noop() RETURNS void LANGUAGE plpgsql AS $$
BEGIN
    EXECUTE 'DROP TABLE work.scratch';
END
$$;
