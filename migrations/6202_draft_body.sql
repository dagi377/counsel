-- module: app/legal/agent
-- Agent draft bodies (AGENT-PROD-2b). Additive only: a new table. A draft the agent stores
-- (docs.draft, comms.comm_item, matter.note) holds only a body_ref; the text lives here, sealed
-- with AES-256-GCM under the boot seal key, so the plain text never reaches a row, a log or an
-- event (DM-020). Retried drafts reuse their row through the draft's idempotency key.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module agent @retention RC-FILE @class content_ref:P4
CREATE TABLE agent.draft_body (
  id          uuid PRIMARY KEY DEFAULT core.uuidv7(),
  sealed      bytea NOT NULL CHECK (octet_length(sealed) > 0),
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER draft_body_no_delete BEFORE DELETE ON agent.draft_body FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

GRANT SELECT, INSERT ON agent.draft_body TO counsel_app, counsel_jobs;

RESET lock_timeout;
RESET statement_timeout;
