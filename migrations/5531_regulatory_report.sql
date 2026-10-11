-- module: app/legal/trust
-- Regulatory reports and releases (G-TREL, docs/QUESTIONS.md Q-GE3-MONEY). Additive only: a
-- new table. A report (the LSO annual report, or another trust record released to the
-- regulator) is prepared 'ready'; only an approved trust.regulatory_release gate marks it
-- 'released' to the approved recipient, once. A released row never changes again.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module trust @retention RC-FIN @class report:P1
CREATE TABLE trust.regulatory_report (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind          text NOT NULL CHECK (kind IN ('lso_annual_report','regulatory_release')),
  year          integer CHECK (year BETWEEN 2000 AND 2100),
  document_id   uuid REFERENCES docs.document(id),
  prepared_by   uuid NOT NULL REFERENCES core.app_user(id),
  status        text NOT NULL DEFAULT 'ready' CHECK (status IN ('ready','released')),
  recipient     text CHECK (recipient IS NULL OR recipient <> ''),
  released_by   uuid REFERENCES core.app_user(id),
  released_at   timestamptz,
  approval_id   uuid UNIQUE,
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'released') = (recipient IS NOT NULL AND released_by IS NOT NULL AND released_at IS NOT NULL
         AND approval_id IS NOT NULL)),
  CHECK (released_by IS NULL OR released_by <> prepared_by)
);

CREATE OR REPLACE FUNCTION trust.regulatory_report_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'trust.regulatory_report rows are never deleted' USING ERRCODE = 'check_violation';
  END IF;
  IF OLD.status = 'released' THEN
    RAISE EXCEPTION 'trust.regulatory_report: a released report is final' USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.id, NEW.kind, NEW.year, NEW.document_id, NEW.prepared_by, NEW.created_at)
     IS DISTINCT FROM (OLD.id, OLD.kind, OLD.year, OLD.document_id, OLD.prepared_by, OLD.created_at) THEN
    RAISE EXCEPTION 'trust.regulatory_report: only the release changes' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER regulatory_report_guard BEFORE UPDATE OR DELETE ON trust.regulatory_report
  FOR EACH ROW EXECUTE FUNCTION trust.regulatory_report_guard();

REVOKE DELETE ON trust.regulatory_report FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON trust.regulatory_report TO counsel_app, counsel_jobs;
GRANT SELECT ON trust.regulatory_report TO counsel_integrity;
