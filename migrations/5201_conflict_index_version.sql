-- module: app/legal/conflict
-- Conflict index version (docs/QUESTIONS.md Q-PORT-19). Additive only.
-- conflict.check_run.index_version records which state of the name index a check ran
-- against. Every write that can change a conflict search result (party.contact names and
-- aliases, matter.party_role rows, conflict.sealed_prospect rows) bumps the version from a
-- sequence, once per statement. The current value lives in a one-row table updated in the
-- writer's transaction, so a search only ever sees committed versions: a search reads the
-- version first and its hits can then only be newer, never older, than the version it
-- reports. A sequence value is never reused, so versions only grow, even across rollbacks.
-- The trigger function is SECURITY DEFINER: app roles read the version but never write it.
SET LOCAL lock_timeout TO '2s';
SET LOCAL statement_timeout TO '60s';

CREATE SEQUENCE conflict.index_version_seq AS bigint MINVALUE 1;

CREATE TABLE conflict.index_version (
  id       boolean PRIMARY KEY DEFAULT true CHECK (id),
  version  bigint NOT NULL CHECK (version > 0)
);
INSERT INTO conflict.index_version (id, version) VALUES (true, nextval('conflict.index_version_seq'));

CREATE FUNCTION conflict.bump_index_version() RETURNS trigger
LANGUAGE plpgsql SECURITY DEFINER SET search_path = pg_catalog, conflict AS $$
BEGIN
  UPDATE conflict.index_version SET version = nextval('conflict.index_version_seq') WHERE id;
  RETURN NULL;
END $$;

CREATE TRIGGER contact_conflict_index_version
  AFTER INSERT OR DELETE OR UPDATE OF legal_name, display_name, aliases ON party.contact
  FOR EACH STATEMENT EXECUTE FUNCTION conflict.bump_index_version();
CREATE TRIGGER party_role_conflict_index_version
  AFTER INSERT OR DELETE OR UPDATE ON matter.party_role
  FOR EACH STATEMENT EXECUTE FUNCTION conflict.bump_index_version();
CREATE TRIGGER sealed_prospect_conflict_index_version
  AFTER INSERT OR DELETE OR UPDATE ON conflict.sealed_prospect
  FOR EACH STATEMENT EXECUTE FUNCTION conflict.bump_index_version();

GRANT SELECT ON conflict.index_version TO counsel_app, counsel_jobs, counsel_integrity;
