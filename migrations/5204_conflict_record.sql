-- module: app/legal/conflict
-- Conflict record (docs/QUESTIONS.md Q-LAW-P2). Additive only: three new tables.
-- RPC 3.4 needs a conflict record the firm can rely on, so a lawyer's disposition of each hit,
-- each client consent and the G2 decision on a run are stored, not kept in process memory.
-- Every table is append-only (core.forbid_mutation): a re-disposition or a withdrawn consent is
-- a new row, and the latest row per hit counts. Nothing is ever deleted, so the record is kept
-- for the whole RC-CONFLICT retention class. A consent row points at the signed consent
-- document; it never holds the document's text.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module conflict @retention RC-CONFLICT @class disposition:P2
CREATE TABLE conflict.hit_disposition (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  run_id       uuid NOT NULL REFERENCES conflict.check_run(id),
  hit_id       uuid NOT NULL REFERENCES conflict.hit(id),
  ordinal      integer NOT NULL CHECK (ordinal >= 0),        -- the hit's position in the run
  disposition  text NOT NULL CHECK (disposition IN ('not_a_conflict','conflict','needs_consent','needs_wall')),
  note         text NOT NULL CHECK (btrim(note) <> ''),
  disposed_by  uuid NOT NULL REFERENCES core.app_user(id),
  disposed_at  timestamptz NOT NULL
);
CREATE INDEX hit_disposition_run_idx ON conflict.hit_disposition (run_id, ordinal, disposed_at, id);
CREATE TRIGGER hit_disposition_immutable BEFORE UPDATE OR DELETE ON conflict.hit_disposition
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- @module conflict @retention RC-CONFLICT @class consent:P4
CREATE TABLE conflict.hit_consent (
  id                  uuid PRIMARY KEY DEFAULT core.uuidv7(),
  run_id              uuid NOT NULL REFERENCES conflict.check_run(id),
  hit_id              uuid NOT NULL REFERENCES conflict.hit(id),
  ordinal             integer NOT NULL CHECK (ordinal >= 0),
  type                text NOT NULL CHECK (type IN ('joint_retainer','acting_against_former','other')),
  status              text NOT NULL CHECK (status IN ('requested','signed','refused','withdrawn')),
  disclosure_summary  text NOT NULL CHECK (btrim(disclosure_summary) <> ''),
  contact_id          uuid REFERENCES party.contact(id),
  document_id         uuid REFERENCES docs.document(id),     -- the signed written consent
  signed_at           timestamptz,
  recorded_by         uuid NOT NULL REFERENCES core.app_user(id),
  recorded_at         timestamptz NOT NULL,
  -- RPC 3.4-2 / r.1.1: consent is in writing, so a signed consent names its document and date.
  CHECK (status <> 'signed' OR (document_id IS NOT NULL AND signed_at IS NOT NULL))
);
CREATE INDEX hit_consent_run_idx ON conflict.hit_consent (run_id, ordinal, recorded_at, id);
CREATE TRIGGER hit_consent_immutable BEFORE UPDATE OR DELETE ON conflict.hit_consent
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- One G2 decision per run: clear or reject.
-- @module conflict @retention RC-CONFLICT @class decision:P2
CREATE TABLE conflict.run_decision (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  run_id      uuid NOT NULL UNIQUE REFERENCES conflict.check_run(id),
  decision    text NOT NULL CHECK (decision IN ('clear','reject')),
  reason      text NOT NULL DEFAULT '',
  decided_by  uuid NOT NULL REFERENCES core.app_user(id),
  decided_at  timestamptz NOT NULL
);
CREATE TRIGGER run_decision_immutable BEFORE UPDATE OR DELETE ON conflict.run_decision
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

GRANT SELECT, INSERT ON conflict.hit_disposition, conflict.hit_consent, conflict.run_decision TO counsel_app, counsel_jobs;
GRANT SELECT ON conflict.hit_disposition, conflict.hit_consent, conflict.run_decision TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
