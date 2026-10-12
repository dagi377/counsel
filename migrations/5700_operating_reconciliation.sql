-- module: app/legal/books
-- Operating bank reconciliation (G12g, docs/QUESTIONS.md Q-GE3-MONEY). Additive only: a new
-- table. Mirrors trust.reconciliation for an operating bank account: a row is prepared, and
-- only an approved gl.operating_reconciliation gate signs it, by someone other than its
-- preparer, when the adjusted bank balance equals the book balance. A signed row is final.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module gl @retention RC-FIN @class reconciliation:P1
CREATE TABLE gl.operating_reconciliation (
  id                   uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id           uuid NOT NULL REFERENCES gl.account(id),
  period_end           date NOT NULL,
  bank_balance_cents   bigint NOT NULL,
  outstanding_items    jsonb NOT NULL DEFAULT '[]'::jsonb,
  adjusted_bank_cents  bigint NOT NULL,
  book_balance_cents   bigint NOT NULL,
  prepared_by          uuid NOT NULL REFERENCES core.app_user(id),
  status               text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','ready','signed')),
  signed_by            uuid REFERENCES core.app_user(id),
  signed_at            timestamptz,
  approval_id          uuid UNIQUE,
  created_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (account_id, period_end),
  CHECK (status <> 'signed' OR (signed_by IS NOT NULL AND signed_at IS NOT NULL AND approval_id IS NOT NULL
         AND adjusted_bank_cents = book_balance_cents AND signed_by <> prepared_by))
);

CREATE OR REPLACE FUNCTION gl.operating_reconciliation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'gl.operating_reconciliation rows are never deleted' USING ERRCODE = 'check_violation';
  END IF;
  IF OLD.status = 'signed' THEN
    RAISE EXCEPTION 'gl.operating_reconciliation: a signed reconciliation is final' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER operating_reconciliation_guard BEFORE UPDATE OR DELETE ON gl.operating_reconciliation
  FOR EACH ROW EXECUTE FUNCTION gl.operating_reconciliation_guard();

REVOKE DELETE ON gl.operating_reconciliation FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON gl.operating_reconciliation TO counsel_app, counsel_jobs;
GRANT SELECT ON gl.operating_reconciliation TO counsel_integrity;
