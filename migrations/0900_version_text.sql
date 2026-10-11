-- module: kernel/docs
-- Extracted text of a stored document version (AGENT-PROD-2a). Additive only: a new table.
-- One row per version: its text layer, pages split on form feed and lines on newline, which
-- the agent read and search tools read and cite. It is the firm's document content, held per
-- version like document_version itself; no conversation text is stored here.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- Append-only: a version's text never changes, so a citation keeps pointing at what it quoted.
-- @module docs @retention RC-FILE @class extracted:P4
CREATE TABLE docs.version_text (
  version_id  uuid PRIMARY KEY REFERENCES docs.document_version(id),
  body        text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER version_text_immutable BEFORE UPDATE OR DELETE ON docs.version_text FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

GRANT SELECT, INSERT ON docs.version_text TO counsel_app, counsel_jobs;
GRANT SELECT ON docs.version_text TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
