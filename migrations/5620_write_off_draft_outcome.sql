-- module: app/legal/billing
-- Write-off draft outcomes (docs/QUESTIONS.md Q-INTEG-12, WO-FIX). Additive: two more draft
-- statuses. A submitted draft whose G-B5 gate is rejected becomes 'rejected'; one the preparer
-- takes back becomes 'withdrawn'. Both are final and neither counts toward the cumulative
-- write-off total that triggers the managing-lawyer second approval (BIL-C-085). The status
-- check is added NOT VALID and validated in 5621 (MIG-061).
SET lock_timeout = '2s';
SET statement_timeout = '5s';

ALTER TABLE billing.write_off_draft DROP CONSTRAINT write_off_draft_status_check;
ALTER TABLE billing.write_off_draft
  ADD CONSTRAINT write_off_draft_status_ck CHECK (status IN ('draft','approved','rejected','withdrawn')) NOT VALID;

-- Each gate id is set once, and a draft leaves 'draft' once: to approved, rejected or
-- withdrawn. A row that has left 'draft' never changes again.
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
  IF OLD.status <> 'draft' AND (NEW.status, NEW.approved_by, NEW.approved_at, NEW.credit_gate_id, NEW.second_gate_id)
     IS DISTINCT FROM (OLD.status, OLD.approved_by, OLD.approved_at, OLD.credit_gate_id, OLD.second_gate_id) THEN
    RAISE EXCEPTION 'billing.write_off_draft: a % write-off is final', OLD.status USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;

RESET lock_timeout;
RESET statement_timeout;
