-- module: app/legal/agent
-- Designated lawyer per scheduled AI feature (JOB-ACTOR). Additive only: a new table.
-- A scheduled feature run has no event user, so it acts for the lawyer the firm names for that
-- feature here. A feature with no row does not run (fail closed) and the readiness check
-- reports it. The table holds ids only.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module agent @retention RC-CONFIG @class config:P0
CREATE TABLE agent.designated_lawyer (
  feature_id   text PRIMARY KEY CHECK (feature_id ~ '^F-[A-Z]+-[0-9]+[a-z]?$'),
  user_id      uuid NOT NULL REFERENCES core.app_user(id),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  updated_by   uuid NOT NULL REFERENCES core.app_user(id),
  row_version  integer NOT NULL DEFAULT 1
);

GRANT SELECT, INSERT, UPDATE ON agent.designated_lawyer TO counsel_app;
GRANT SELECT ON agent.designated_lawyer TO counsel_jobs, counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
