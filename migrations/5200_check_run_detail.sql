-- module: app/legal/conflict
-- Run and hit detail the conflict domain records (docs/QUESTIONS.md Q-PORT-18). Additive only:
-- nullable columns, NOT VALID constraints and a partial unique index (built CONCURRENTLY in
-- 5202_check_run_detail_index.sql, MIG-061). check_run stays append-only
-- (check_run_immutable); a run's hits are inserted with it and keep their order.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

ALTER TABLE conflict.check_run ADD COLUMN prepared_by uuid;  -- for an agent, the user it acts for
ALTER TABLE conflict.check_run ADD COLUMN terms text[];      -- the search terms, as run
ALTER TABLE conflict.check_run
  ADD CONSTRAINT check_run_prepared_by_fk FOREIGN KEY (prepared_by) REFERENCES core.app_user(id) NOT VALID;

ALTER TABLE conflict.hit ADD COLUMN ordinal integer;         -- position in the run's hits
ALTER TABLE conflict.hit ADD COLUMN term text;               -- the term that matched
ALTER TABLE conflict.hit ADD COLUMN party_matter_id uuid;    -- matter the matched party is on, when any
ALTER TABLE conflict.hit ADD COLUMN party_role text;         -- role of the party on that matter, e.g. adverse
ALTER TABLE conflict.hit ADD CONSTRAINT hit_ordinal_ck CHECK (ordinal >= 0) NOT VALID;
