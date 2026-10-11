-- module: app/legal/trust
-- Signatory change log (PORT-11). Additive only: a new table. A change adds or removes one
-- signatory on one trust account. It is proposed pending and applies to trust.signatory only
-- when a user other than the proposer approves it; the row is never deleted.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module trust @retention RC-FIN @class authority:P1
CREATE TABLE trust.signatory_change (
  id             uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id     uuid NOT NULL REFERENCES trust.trust_account(id),
  user_id        uuid NOT NULL REFERENCES core.app_user(id),
  op             text NOT NULL CHECK (op IN ('add','remove')),
  kind           text CHECK (kind IN ('licensee','bonded_non_licensee')),
  bond_evidence  text,                                 -- document reference, never the document
  proposed_by    uuid NOT NULL REFERENCES core.app_user(id),
  approved_by    uuid REFERENCES core.app_user(id),
  approval_id    uuid,                                 -- the gate that approved it, when there is one
  approved_at    timestamptz,
  status         text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved')),
  created_at     timestamptz NOT NULL DEFAULT now(),
  CHECK ((op = 'add') = (kind IS NOT NULL)),
  CHECK (kind IS DISTINCT FROM 'bonded_non_licensee' OR bond_evidence IS NOT NULL),
  CHECK ((status = 'approved') = (approved_by IS NOT NULL AND approved_at IS NOT NULL AND approval_id IS NOT NULL)),
  CHECK (approved_by IS DISTINCT FROM proposed_by)
);
CREATE INDEX signatory_change_account_idx ON trust.signatory_change (account_id, created_at);
CREATE TRIGGER signatory_change_no_delete BEFORE DELETE ON trust.signatory_change FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

GRANT SELECT, INSERT, UPDATE ON trust.signatory_change TO counsel_app;
GRANT SELECT ON trust.signatory_change TO counsel_jobs, counsel_integrity;
