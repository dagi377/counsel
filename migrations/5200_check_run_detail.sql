-- module: app/legal/conflict
-- Run and hit detail the conflict domain records (docs/QUESTIONS.md Q-PORT-18). Additive only:
-- nullable columns and a partial unique index. check_run stays append-only
-- (check_run_immutable); a run's hits are inserted with it and keep their order.

ALTER TABLE conflict.check_run ADD COLUMN prepared_by uuid REFERENCES core.app_user(id);  -- for an agent, the user it acts for
ALTER TABLE conflict.check_run ADD COLUMN terms text[];                                  -- the search terms, as run

ALTER TABLE conflict.hit ADD COLUMN ordinal integer CHECK (ordinal >= 0);  -- position in the run's hits
ALTER TABLE conflict.hit ADD COLUMN term text;                             -- the term that matched
ALTER TABLE conflict.hit ADD COLUMN party_matter_id uuid;                  -- matter the matched party is on, when any
ALTER TABLE conflict.hit ADD COLUMN party_role text;                       -- role of the party on that matter, e.g. adverse
CREATE UNIQUE INDEX hit_run_ordinal ON conflict.hit (run_id, ordinal) WHERE ordinal IS NOT NULL;
