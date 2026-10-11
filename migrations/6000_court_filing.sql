-- module: app/legal/comms
-- Court filings (docs/QUESTIONS.md T-035L.2). Additive only: a new schema and table.
-- comms.comm_filing stays "file this message to a matter"; a court filing is its own record.

CREATE SCHEMA IF NOT EXISTS legal;

-- @module comms @retention RC-FILE @class filing:P1
CREATE TABLE legal.court_filing (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id    uuid NOT NULL REFERENCES matter.matter(id),
  document_id  uuid REFERENCES docs.document(id),
  court        text NOT NULL CHECK (court <> ''),
  reference    text,                                   -- court file or e-filing reference
  status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','filed','rejected')),
  filed_by     uuid REFERENCES core.app_user(id),
  filed_on     date,                                   -- business date (America/Toronto)
  created_at   timestamptz NOT NULL DEFAULT now(),
  created_by   uuid REFERENCES core.app_user(id),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  updated_by   uuid REFERENCES core.app_user(id),
  row_version  integer NOT NULL DEFAULT 1,
  archived_at  timestamptz,
  CHECK (status <> 'filed' OR (filed_on IS NOT NULL AND filed_by IS NOT NULL))
);
CREATE INDEX court_filing_matter_idx ON legal.court_filing (matter_id, created_at) WHERE archived_at IS NULL;
CREATE TRIGGER court_filing_no_delete BEFORE DELETE ON legal.court_filing FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();
CREATE TRIGGER court_filing_touch BEFORE UPDATE ON legal.court_filing FOR EACH ROW EXECUTE FUNCTION core.touch();

-- Matter-scoped: RLS on and forced, like every matter table (RV2-014, CF-17).
ALTER TABLE legal.court_filing ENABLE ROW LEVEL SECURITY;
ALTER TABLE legal.court_filing FORCE ROW LEVEL SECURITY;
CREATE POLICY court_filing_visible ON legal.court_filing FOR ALL TO counsel_app USING (matter.can_see(matter_id));

GRANT USAGE ON SCHEMA legal TO counsel_app, counsel_jobs, counsel_integrity;
GRANT SELECT, INSERT, UPDATE ON legal.court_filing TO counsel_app, counsel_jobs;
GRANT SELECT ON legal.court_filing TO counsel_integrity;
