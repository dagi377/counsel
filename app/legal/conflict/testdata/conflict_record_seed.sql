-- Seed for TestPGConflictRecordRoundTrip: two lawyers, a matter, one conflict check and
-- one check party. Returns matter, check, check party, preparer, lawyer and client ids.
WITH r AS (INSERT INTO core.role (key, name) VALUES ('lawyer', 'Lawyer') RETURNING id),
  u1 AS (INSERT INTO core.app_user (email, display_name, role_id) SELECT 'a@x.test', 'A', id FROM r RETURNING id),
  u2 AS (INSERT INTO core.app_user (email, display_name, role_id) SELECT 'b@x.test', 'B', id FROM r RETURNING id),
  c AS (INSERT INTO party.contact (kind, legal_name, display_name) VALUES ('organization', 'Acme', 'Acme') RETURNING id),
  pa AS (INSERT INTO matter.practice_area (name) VALUES ('Civil') RETURNING id),
  m AS (INSERT INTO matter.matter (number, short_name, client_id, practice_area_id, responsible_lawyer_id)
    SELECT 'M-1', 'Acme v Beta', c.id, pa.id, u1.id FROM c, pa, u1 RETURNING id),
  s AS (INSERT INTO conflict.conflict_settings (version, sources_searched, default_match) VALUES (1, '{contacts}', 'exact') RETURNING version),
  k AS (INSERT INTO conflict.conflict_check (matter_id, trigger, settings_version, requested_by)
    SELECT m.id, 'new_matter', s.version, u1.id FROM m, s, u1 RETURNING id),
  p AS (INSERT INTO conflict.check_party (check_id, name, kind, role) SELECT k.id, 'Acme', 'organization', 'client' FROM k RETURNING id)
SELECT m.id::text, k.id::text, p.id::text, u1.id::text, u2.id::text, c.id::text FROM m, k, p, u1, u2, c;
