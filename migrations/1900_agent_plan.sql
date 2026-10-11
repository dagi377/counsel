-- module: kernel/agenttools
-- Agent plans (PORT-25). Additive only: a new table in the agent schema.
-- body holds the tool steps only (index, title, status, call ids). Prompt and completion text
-- are never stored here. One plan per run: a run that reports new step titles replaces its plan.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

CREATE TABLE agent.plan (
  id          text PRIMARY KEY CHECK (id <> ''),
  run_id      text NOT NULL UNIQUE CHECK (run_id <> ''),
  body        jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(body) = 'array'),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

GRANT SELECT, INSERT, UPDATE ON agent.plan TO counsel_app, counsel_jobs;
GRANT SELECT ON agent.plan TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
