-- module: app/legal/agent
-- Agent flags (FEAT-CORE-13). Additive only: a new table.
-- A read-only job feature records what it found as structured flag rows: feature, subject,
-- a machine code, a severity and the citations behind it. There is no free-text column
-- (AI-R-005): the digest text goes to the user through kernel/notify and is not stored.
-- A flag is a proposal the user may dismiss. blocks_gate is set only for features whose
-- definition says the flag blocks that gate kind (for example the GA1 duty scan).

-- @module agent @retention RC-OPS @class flag:P4
CREATE TABLE agent.agent_flag (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  feature_id    text NOT NULL CHECK (feature_id ~ '^F-[A-Z]+-[0-9]+[a-z]?$'),
  run_id        text NOT NULL CHECK (run_id <> '' AND length(run_id) <= 64),
  subject_kind  text NOT NULL CHECK (subject_kind ~ '^[a-z][a-z0-9_.]{0,63}$'),
  subject_id    text NOT NULL CHECK (subject_id <> '' AND length(subject_id) <= 64),
  code          text NOT NULL CHECK (code ~ '^[a-z][a-z0-9_.]{0,63}$'),
  severity      text NOT NULL CHECK (severity IN ('info','warning','critical')),
  citations     jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(citations) = 'array'),
  blocks_gate   text CHECK (blocks_gate IS NULL OR blocks_gate ~ '^[a-z][a-z0-9_.]{0,63}$'),
  acting_for    uuid NOT NULL REFERENCES core.app_user(id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  dismissed_at  timestamptz,
  dismissed_by  uuid REFERENCES core.app_user(id),
  CHECK ((dismissed_at IS NULL) = (dismissed_by IS NULL))
);
-- One open flag per feature, subject and code: a rerun does not repeat it.
CREATE UNIQUE INDEX agent_flag_open_uq ON agent.agent_flag (feature_id, subject_kind, subject_id, code)
  WHERE dismissed_at IS NULL;
CREATE INDEX agent_flag_blocking_idx ON agent.agent_flag (blocks_gate, subject_kind, subject_id)
  WHERE dismissed_at IS NULL AND blocks_gate IS NOT NULL;
CREATE TRIGGER agent_flag_no_delete BEFORE DELETE ON agent.agent_flag FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

GRANT SELECT, INSERT, UPDATE ON agent.agent_flag TO counsel_app, counsel_jobs;
GRANT SELECT ON agent.agent_flag TO counsel_integrity;
