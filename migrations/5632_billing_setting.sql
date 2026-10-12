-- module: app/legal/billing
-- Billing settings (G-B8, docs/QUESTIONS.md Q-GE3-MONEY). Additive only: two new tables.
-- billing.setting holds the current value of each firm billing setting; it changes only
-- through an approved billing.settings_change gate, which also appends one
-- billing.setting_change row keyed by the gate (the history and the idempotency key).
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module billing @retention RC-FIN @class settings:P2
CREATE TABLE billing.setting (
  key          text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]{0,63}$'),
  value        jsonb NOT NULL,
  approval_id  uuid NOT NULL,
  updated_by   uuid NOT NULL REFERENCES core.app_user(id),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

-- @module billing @retention RC-FIN @class settings:P2
CREATE TABLE billing.setting_change (
  approval_id  uuid PRIMARY KEY,
  key          text NOT NULL,
  old_value    jsonb,
  new_value    jsonb NOT NULL,
  approved_by  uuid NOT NULL REFERENCES core.app_user(id),
  at           timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER setting_change_immutable BEFORE UPDATE OR DELETE ON billing.setting_change
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

REVOKE DELETE ON billing.setting, billing.setting_change FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON billing.setting TO counsel_app, counsel_jobs;
GRANT SELECT, INSERT ON billing.setting_change TO counsel_app, counsel_jobs;
GRANT SELECT ON billing.setting, billing.setting_change TO counsel_integrity;
