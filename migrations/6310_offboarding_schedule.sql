-- module: app/legal/access
-- An approved offboarding (GA12) waits here until its cut-off; job core.offboarding_cutoff
-- applies it. Additive only: a new table keyed by gate id.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module core @retention RC-PERSONNEL @class offboarding:P1
CREATE TABLE core.offboarding_schedule (
  gate_id           uuid PRIMARY KEY,
  user_id           uuid NOT NULL REFERENCES core.app_user(id),
  handover_user_id  uuid NOT NULL REFERENCES core.app_user(id),
  cutoff_at         timestamptz NOT NULL,
  approved_by       uuid NOT NULL REFERENCES core.app_user(id),
  applied_at        timestamptz,
  CHECK (handover_user_id <> user_id),
  CHECK (approved_by <> user_id)
);
CREATE INDEX offboarding_schedule_due_idx ON core.offboarding_schedule (cutoff_at) WHERE applied_at IS NULL;

GRANT SELECT, INSERT, UPDATE ON core.offboarding_schedule TO counsel_app, counsel_jobs;
GRANT SELECT ON core.offboarding_schedule TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
