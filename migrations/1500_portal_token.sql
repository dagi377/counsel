-- module: kernel/portal
-- Portal sign-in tokens and session handles (PORT-27). Additive only: a new table.
-- Only SHA-256 hashes of magic-link tokens and session ids are stored, never the raw values.
-- A session row points at its core.user_session (platform 'portal'), which carries the
-- session's end (logout, expiry, revocation).
SET lock_timeout = '2s';
SET statement_timeout = '5min';

CREATE TABLE platform.portal_token (
  token_hash  text PRIMARY KEY CHECK (token_hash ~ '^[0-9a-f]{64}$'),
  kind        text NOT NULL CHECK (kind IN ('sign_in','session')),
  guest_id    uuid NOT NULL REFERENCES core.app_user(id),
  grants      text[] NOT NULL DEFAULT '{}',
  session_id  uuid REFERENCES core.user_session(id),
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'session') = (session_id IS NOT NULL)),
  CHECK (kind = 'sign_in' OR used_at IS NULL)
);
CREATE INDEX portal_token_expiry_idx ON platform.portal_token (expires_at) WHERE used_at IS NULL;

GRANT SELECT, INSERT, UPDATE ON platform.portal_token TO counsel_app, counsel_jobs;
GRANT SELECT ON platform.portal_token TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
