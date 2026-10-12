-- module: app/legal/work
-- Approved state of G-W3 (workflow release), G-W4 (external sync), G-W5 (absence) and G-W2
-- (plan acceptance). Additive only: new tables, each row keyed or unique by its gate id.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.workflow_release (
  workflow_id   uuid PRIMARY KEY REFERENCES work.workflow(id),
  version       integer NOT NULL CHECK (version > 0),
  gate_id       uuid NOT NULL UNIQUE,
  published_by  uuid NOT NULL REFERENCES core.app_user(id),
  published_at  timestamptz NOT NULL
);

-- @module work @retention RC-CONFIG @class config:P0
CREATE TABLE work.external_sync (
  matter_id       uuid NOT NULL,
  integration_id  uuid NOT NULL,
  gate_id         uuid NOT NULL UNIQUE,
  enabled_by      uuid NOT NULL REFERENCES core.app_user(id),
  enabled_at      timestamptz NOT NULL,
  PRIMARY KEY (matter_id, integration_id)
);

-- @module work @retention RC-PERSONNEL @class absence:P1
CREATE TABLE work.absence (
  gate_id      uuid PRIMARY KEY,
  user_id      uuid NOT NULL REFERENCES core.app_user(id),
  start_on     date NOT NULL,
  end_on       date NOT NULL,
  approved_by  uuid NOT NULL REFERENCES core.app_user(id),
  approved_at  timestamptz NOT NULL,
  CHECK (end_on >= start_on)
);

-- @module work @retention RC-FILE @class plan:P1
CREATE TABLE work.plan_acceptance (
  gate_id      uuid PRIMARY KEY,
  matter_id    uuid NOT NULL,
  template_id  uuid NOT NULL REFERENCES work.work_template(id),
  accepted_by  uuid NOT NULL REFERENCES core.app_user(id),
  accepted_at  timestamptz NOT NULL
);

GRANT SELECT, INSERT, UPDATE ON work.workflow_release, work.external_sync, work.absence, work.plan_acceptance TO counsel_app;
GRANT SELECT ON work.workflow_release, work.external_sync, work.absence, work.plan_acceptance TO counsel_jobs, counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
