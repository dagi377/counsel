-- module: kernel/agenttools
-- Agent run records (PG-RUNS-WEBHOOK). Additive only: a new table in the agent schema.
-- One row per run started through POST /agent/runs: ids, state, feature, the run principal
-- (acting-for user and narrowed grant), the subject, timestamps and model cost. There is no prompt, input,
-- answer or other conversation column (AI-R-005); error_code is a machine code.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module agent @retention RC-AUDIT @class run:P1
CREATE TABLE agent.run_record (
  id                uuid PRIMARY KEY,
  feature_id        text NOT NULL CHECK (feature_id ~ '^F-[A-Z]+-[0-9]+[a-z]?$'),
  skill_id          text NOT NULL CHECK (skill_id ~ '^[a-z][a-z0-9_.-]{0,63}$'),
  acting_for        uuid NOT NULL REFERENCES core.app_user(id),
  grant_caps        text[] NOT NULL DEFAULT '{}',
  subject_id        uuid,                                  -- the app subject the run is about (a matter)
  state             text NOT NULL DEFAULT 'queued'
                    CHECK (state IN ('queued','running','waiting','succeeded','failed','cancelled')),
  error_code        text CHECK (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_.]{0,63}$'),
  created_at        timestamptz NOT NULL DEFAULT now(),
  started_at        timestamptz,
  ended_at          timestamptz,
  input_tokens      bigint NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
  output_tokens     bigint NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
  cost_micro_cents  bigint NOT NULL DEFAULT 0 CHECK (cost_micro_cents >= 0),   -- model cost; not client money
  CHECK (ended_at IS NULL OR started_at IS NULL OR ended_at >= started_at)
);
CREATE INDEX run_record_acting_for_idx ON agent.run_record (acting_for, created_at);
CREATE TRIGGER run_record_no_delete BEFORE DELETE ON agent.run_record FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

GRANT SELECT, INSERT, UPDATE ON agent.run_record TO counsel_app, counsel_jobs;
GRANT SELECT ON agent.run_record TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
