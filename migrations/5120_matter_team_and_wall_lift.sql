-- module: app/legal/matter
-- Matter team of a team-visibility matter (GA4) and the audited record of a wall lift (GA5).
-- Additive only: new tables. Each row carries its gate id, so a re-run changes nothing.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module matter @retention RC-FILE @class membership:P1
CREATE TABLE matter.matter_team (
  matter_id  uuid NOT NULL REFERENCES matter.matter(id),
  user_id    uuid NOT NULL REFERENCES core.app_user(id),
  gate_id    uuid NOT NULL,
  added_at   timestamptz NOT NULL,
  added_by   uuid NOT NULL REFERENCES core.app_user(id),
  PRIMARY KEY (matter_id, user_id)
);

-- @module matter @retention RC-AUDIT @class wall:P1
CREATE TABLE matter.ethical_wall_lift (
  gate_id    uuid PRIMARY KEY,
  wall_id    uuid NOT NULL REFERENCES matter.ethical_wall(id),
  user_id    uuid REFERENCES core.app_user(id),
  lifted_by  uuid NOT NULL REFERENCES core.app_user(id),
  lifted_at  timestamptz NOT NULL,
  CHECK (user_id IS DISTINCT FROM lifted_by)
);

GRANT SELECT, INSERT, UPDATE, DELETE ON matter.matter_team TO counsel_app;
GRANT SELECT, INSERT ON matter.ethical_wall_lift TO counsel_app;
GRANT SELECT ON matter.matter_team, matter.ethical_wall_lift TO counsel_jobs, counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
