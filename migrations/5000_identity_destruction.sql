-- module: app/legal/party
-- ID verification destruction (G14, conflict.id_destruction). Additive only: nullable columns.
-- Destroying ID verification redacts the artefacts (the document copy and the verification
-- data) and stamps destroyed_at. The identity_record row itself is never deleted (it has a
-- no-delete trigger), so the conflict-check history stays. destruction_approval_id is the
-- gate that approved the destruction.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE party.identity_record
  ADD COLUMN destroyed_at timestamptz,
  ADD COLUMN destroyed_by uuid,
  ADD COLUMN destruction_approval_id uuid;
ALTER TABLE party.id_document ADD COLUMN destroyed_at timestamptz;

RESET lock_timeout;
RESET statement_timeout;
