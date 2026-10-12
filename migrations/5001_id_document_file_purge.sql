-- module: app/legal/party
-- ID document file purge (conflict.id_destruction). Additive only: nullable columns.
-- Destroying ID verification also deletes the stored copy behind file_ref from the document
-- bucket. file_purged_at records that the object was deleted and file_purge_approval_id the
-- gate that approved it; the key itself is redacted with the rest of the artefacts.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE party.id_document
  ADD COLUMN file_purged_at timestamptz,
  ADD COLUMN file_purge_approval_id uuid;

RESET lock_timeout;
RESET statement_timeout;
