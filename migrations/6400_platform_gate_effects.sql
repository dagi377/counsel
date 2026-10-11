-- module: app/legal/platform
-- Tables the platform.* gate effects write (GE3-ops-a, docs/QUESTIONS.md Q-GE3-OPS-A). Additive
-- only: new tables. Each row is keyed or tagged by the approving gate, so a re-run of the same
-- effect finds its row and changes nothing. Message, memo and answer text stays in the gate
-- payload; a release row holds ids only and the sender reads the payload by gate id.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- An approved outbound item (portal message, email, research memo, security answer) queued for
-- the sender.
-- @module platform @retention RC-AUDIT @class release:P1
CREATE TABLE platform.release (
  gate_id      uuid PRIMARY KEY REFERENCES gate.approval_request(id),
  kind         text NOT NULL CHECK (kind IN ('portal_message','outgoing_email','research_memo','security_answer')),
  matter_id    uuid,
  ref_id       uuid,
  recipient    text,
  status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sent','failed')),
  approved_by  uuid NOT NULL REFERENCES core.app_user(id),
  approved_at  timestamptz NOT NULL
);

-- The confirmed matter summary; the latest approved gate wins.
-- @module platform @retention RC-FILE @class summary:P2
CREATE TABLE platform.matter_summary (
  matter_id     uuid PRIMARY KEY,
  body_markdown text NOT NULL,
  gate_id       uuid NOT NULL REFERENCES gate.approval_request(id),
  confirmed_by  uuid NOT NULL REFERENCES core.app_user(id),
  confirmed_at  timestamptz NOT NULL
);

-- An approved file destruction, executed by the retention job (which re-checks legal hold).
-- @module platform @retention RC-PERM @class destruction:P1
CREATE TABLE platform.destruction_order (
  gate_id          uuid PRIMARY KEY REFERENCES gate.approval_request(id),
  matter_id        uuid NOT NULL,
  retention_class  text NOT NULL,
  status           text NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled','done','blocked')),
  approved_by      uuid NOT NULL REFERENCES core.app_user(id),
  approved_at      timestamptz NOT NULL
);

-- Time-boxed vendor support access.
-- @module platform @retention RC-AUDIT @class support:P1
CREATE TABLE platform.support_access (
  gate_id       uuid PRIMARY KEY REFERENCES gate.approval_request(id),
  support_user  text NOT NULL,
  scope         text NOT NULL,
  expires_at    timestamptz NOT NULL,
  approved_by   uuid NOT NULL REFERENCES core.app_user(id),
  approved_at   timestamptz NOT NULL,
  revoked_at    timestamptz,
  CHECK (expires_at > approved_at)
);

-- The firm's escrow destination (one row).
-- @module platform @retention RC-CONFIG @class config:P1
CREATE TABLE platform.escrow_destination (
  singleton    boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  destination  text NOT NULL,
  gate_id      uuid NOT NULL REFERENCES gate.approval_request(id),
  approved_by  uuid NOT NULL REFERENCES core.app_user(id),
  approved_at  timestamptz NOT NULL
);

-- Published plan prices, in cents.
-- @module platform @retention RC-CONFIG @class plan:P0
CREATE TABLE platform.published_price (
  plan         text PRIMARY KEY,
  price_cents  bigint NOT NULL CHECK (price_cents >= 0),
  gate_id      uuid NOT NULL REFERENCES gate.approval_request(id),
  approved_by  uuid NOT NULL REFERENCES core.app_user(id),
  approved_at  timestamptz NOT NULL
);

GRANT SELECT, INSERT, UPDATE ON platform.release, platform.matter_summary, platform.destruction_order,
  platform.support_access, platform.escrow_destination, platform.published_price TO counsel_app;
GRANT SELECT ON platform.release, platform.matter_summary, platform.destruction_order,
  platform.support_access, platform.escrow_destination, platform.published_price
  TO counsel_jobs, counsel_integrity;
GRANT UPDATE ON platform.release, platform.destruction_order TO counsel_jobs;

RESET lock_timeout;
RESET statement_timeout;
