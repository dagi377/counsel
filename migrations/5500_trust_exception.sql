-- module: app/legal/trust
-- Trust exceptions (docs/QUESTIONS.md Q-T-011.4). Additive only: a new table.
-- BIL-C-065: a reversal of a trust deposit (NSF, chargeback, failed PAD) always opens a trust
-- exception, in the same transaction as the reversal. The row stays open until a lawyer records
-- how it was resolved (for example the client repaid the firm's replenishment).
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module trust @retention RC-FIN @class amounts:P1
CREATE TABLE trust.exception (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind             text NOT NULL CHECK (kind IN ('nsf')),
  receipt_id       uuid NOT NULL REFERENCES trust.trust_transaction(id),   -- the receipt that was returned
  ledger_id        uuid NOT NULL REFERENCES trust.client_ledger(id),
  matter_id        uuid NOT NULL REFERENCES matter.matter(id),
  amount_cents     bigint NOT NULL CHECK (amount_cents > 0),                -- the returned amount
  shortfall_cents  bigint NOT NULL DEFAULT 0 CHECK (shortfall_cents >= 0),  -- replenished from firm funds
  opened_at        timestamptz NOT NULL DEFAULT now(),
  opened_by        uuid NOT NULL REFERENCES core.app_user(id),
  resolved_at      timestamptz,
  resolved_by      uuid REFERENCES core.app_user(id),
  resolution       text,
  UNIQUE (kind, receipt_id),
  CHECK (shortfall_cents <= amount_cents),
  CHECK ((resolved_at IS NULL) = (resolution IS NULL)),
  CHECK ((resolved_at IS NULL) = (resolved_by IS NULL)),
  CHECK (resolution IS NULL OR resolution <> '')
);
CREATE INDEX exception_open_idx ON trust.exception (matter_id, opened_at) WHERE resolved_at IS NULL;
CREATE TRIGGER exception_no_delete BEFORE DELETE ON trust.exception FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

GRANT SELECT, INSERT, UPDATE ON trust.exception TO counsel_app, counsel_jobs;
GRANT SELECT ON trust.exception TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
