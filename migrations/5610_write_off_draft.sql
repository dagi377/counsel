-- module: app/legal/billing
-- Draft write-offs (docs/QUESTIONS.md Q-PORT-15). Additive only: a new table.
-- billing.write_off holds approved write-offs only: every row reduces the bill balance and is
-- immutable. A write-off is drafted here with its fee, disbursement, tax and interest split
-- (computed at draft time); the G-B5 effect approves it once and only then inserts the
-- billing.write_off row with the same id. Nothing is deleted; the only updates are setting each
-- gate id once and draft -> approved once.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.write_off_draft (
  id               uuid PRIMARY KEY DEFAULT core.uuidv7(),
  bill_id          uuid NOT NULL REFERENCES billing.bill(id),
  amount_cents     bigint NOT NULL CHECK (amount_cents > 0),
  fee_cents        bigint NOT NULL DEFAULT 0 CHECK (fee_cents >= 0),
  dtx_cents        bigint NOT NULL DEFAULT 0 CHECK (dtx_cents >= 0),
  dnt_cents        bigint NOT NULL DEFAULT 0 CHECK (dnt_cents >= 0),
  tax_cents        bigint NOT NULL DEFAULT 0 CHECK (tax_cents >= 0),
  interest_cents   bigint NOT NULL DEFAULT 0 CHECK (interest_cents >= 0),
  reason           text NOT NULL CHECK (reason <> ''),
  status           text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved')),
  prepared_by      uuid NOT NULL REFERENCES core.app_user(id),
  credit_gate_id   uuid REFERENCES gate.approval_request(id),   -- the G-B5 gate (billing.credit_note)
  second_gate_id   uuid REFERENCES gate.approval_request(id),   -- billing.writeoff_second_approval, above the threshold
  approved_by      uuid REFERENCES core.app_user(id),           -- the G-B5 approver
  approved_at      timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  CHECK (fee_cents + dtx_cents + dnt_cents + tax_cents + interest_cents = amount_cents),
  CHECK ((status = 'approved') = (approved_by IS NOT NULL)),
  CHECK ((approved_by IS NULL) = (approved_at IS NULL)),
  CHECK (status = 'draft' OR credit_gate_id IS NOT NULL),
  CHECK (approved_by IS NULL OR approved_by <> prepared_by)
);
CREATE INDEX write_off_draft_bill_idx ON billing.write_off_draft (bill_id);

-- Each gate id is set once, draft -> approved happens once, and nothing else ever changes.
CREATE OR REPLACE FUNCTION billing.write_off_draft_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'billing.write_off_draft rows are never deleted' USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.id, NEW.bill_id, NEW.amount_cents, NEW.fee_cents, NEW.dtx_cents, NEW.dnt_cents, NEW.tax_cents,
      NEW.interest_cents, NEW.reason, NEW.prepared_by, NEW.created_at)
     IS DISTINCT FROM (OLD.id, OLD.bill_id, OLD.amount_cents, OLD.fee_cents, OLD.dtx_cents, OLD.dnt_cents,
      OLD.tax_cents, OLD.interest_cents, OLD.reason, OLD.prepared_by, OLD.created_at) THEN
    RAISE EXCEPTION 'billing.write_off_draft: amount, split, reason and preparer are fixed' USING ERRCODE = 'check_violation';
  END IF;
  IF OLD.credit_gate_id IS NOT NULL AND NEW.credit_gate_id IS DISTINCT FROM OLD.credit_gate_id THEN
    RAISE EXCEPTION 'billing.write_off_draft: credit gate is set once' USING ERRCODE = 'check_violation';
  END IF;
  IF OLD.second_gate_id IS NOT NULL AND NEW.second_gate_id IS DISTINCT FROM OLD.second_gate_id THEN
    RAISE EXCEPTION 'billing.write_off_draft: second gate is set once' USING ERRCODE = 'check_violation';
  END IF;
  IF OLD.status = 'approved' AND (NEW.status, NEW.approved_by, NEW.approved_at)
     IS DISTINCT FROM (OLD.status, OLD.approved_by, OLD.approved_at) THEN
    RAISE EXCEPTION 'billing.write_off_draft: an approved write-off is final' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER write_off_draft_guard BEFORE UPDATE OR DELETE ON billing.write_off_draft
  FOR EACH ROW EXECUTE FUNCTION billing.write_off_draft_guard();

REVOKE DELETE ON billing.write_off_draft FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON billing.write_off_draft TO counsel_app, counsel_jobs;
GRANT SELECT ON billing.write_off_draft TO counsel_integrity;
