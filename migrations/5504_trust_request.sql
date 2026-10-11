-- module: app/legal/trust
-- Trust requests: retainer and funds-into-trust requests to a client (docs/QUESTIONS.md
-- HTTP-19). Additive only: a new table. A request is prepared as a draft; approval is a
-- separate step by a different person and never by the agent. No money moves here: funds
-- arrive as trust receipts.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module trust @retention RC-FIN @class amounts:P1
CREATE TABLE trust.trust_request (
  id                 uuid PRIMARY KEY DEFAULT core.uuidv7(),
  matter_id          uuid NOT NULL REFERENCES matter.matter(id),
  amount_cents       bigint NOT NULL CHECK (amount_cents > 0),
  reason             text,
  due_on             date,
  status             text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','sent','funded','cancelled')),
  prepared_by        uuid NOT NULL REFERENCES core.app_user(id),
  prepared_by_agent  boolean NOT NULL DEFAULT false,   -- the agent drafted it for prepared_by
  approved_by        uuid REFERENCES core.app_user(id),
  approved_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  row_version        integer NOT NULL DEFAULT 1,
  -- The person who prepares a request never approves it.
  CHECK (approved_by IS NULL OR approved_by <> prepared_by),
  CHECK ((approved_by IS NULL) = (approved_at IS NULL)),
  CHECK (status <> 'draft' OR approved_by IS NULL),
  CHECK (status IN ('draft','cancelled') OR approved_by IS NOT NULL)
);
CREATE INDEX trust_request_matter_idx ON trust.trust_request (matter_id, id);
CREATE TRIGGER trust_request_no_delete BEFORE DELETE ON trust.trust_request FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

GRANT SELECT, INSERT, UPDATE ON trust.trust_request TO counsel_app, counsel_jobs;
GRANT SELECT ON trust.trust_request TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
