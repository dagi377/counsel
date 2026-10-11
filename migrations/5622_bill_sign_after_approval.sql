-- module: app/legal/billing
-- Signing an approved bill (docs/QUESTIONS.md Q-PG-BILL-FIX). billing.bill_guard (0001) froze
-- signed_by, signed_at and signature_hash with every other amount, date and term once a bill
-- left draft, but the licensee signs a bill after approval (BIL-L-001, CF-09), so an approved
-- bill could never be signed and therefore never delivered. The function is replaced with one
-- change: on an approved, undelivered, unsigned bill the three signature columns may go from
-- NULL to a value once, all together. Every other frozen column (signature_ref included) stays
-- frozen, and every other rule is unchanged. The trigger itself is not touched.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

CREATE OR REPLACE FUNCTION billing.bill_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.status <> 'draft' THEN RAISE EXCEPTION 'DM-010: only draft bills may be deleted'; END IF;
    RETURN OLD;
  END IF;
  -- RV1-008: delivered_at is a legal clock. NULL -> value (<= now) once; later changes only via an approved delivery_correction.
  IF NEW.delivered_at IS DISTINCT FROM OLD.delivered_at THEN
    IF OLD.delivered_at IS NULL AND NEW.delivered_at IS NOT NULL THEN
      IF NEW.delivered_at > now() THEN RAISE EXCEPTION 'DM-041: delivered_at % is in the future', NEW.delivered_at; END IF;
    ELSIF NOT EXISTS (SELECT 1 FROM billing.delivery_correction dc JOIN gate.approval_request ar ON ar.id = dc.approval_id AND ar.status = 'approved'
                       WHERE dc.bill_id = OLD.id AND dc.old_delivered_at = OLD.delivered_at
                         AND dc.new_delivered_at IS NOT DISTINCT FROM NEW.delivered_at AND dc.applied_at IS NULL) THEN
      RAISE EXCEPTION 'DM-041: delivered_at of bill % cannot be rewritten without an approved delivery_correction', OLD.id;
    END IF;
  END IF;
  IF NEW.status = 'void' AND OLD.status <> 'void' AND OLD.delivered_at IS NOT NULL THEN
    RAISE EXCEPTION 'DM-041: bill % was delivered; issue a credit note instead of voiding', OLD.id;
  END IF;
  IF OLD.status = 'void' AND NEW.status <> 'void' THEN RAISE EXCEPTION 'DM-041: void is terminal'; END IF;
  IF OLD.status NOT IN ('draft','preflight_failed','pending_approval') THEN
    -- DM-041: every frozen column except the three signature columns.
    IF (NEW.fees_cents, NEW.taxable_disb_cents, NEW.nontaxable_disb_cents, NEW.discount_cents, NEW.tax_cents,
        NEW.total_cents, NEW.number, NEW.matter_id, NEW.payer_id, NEW.tax_breakdown, NEW.issue_date,
        NEW.due_date, NEW.approved_by, NEW.approved_at, NEW.approval_id, NEW.signature_ref,
        NEW.interest_rate_ppm, NEW.interest_terms_doc_id, NEW.place_of_supply, NEW.place_of_supply_province)
       IS DISTINCT FROM (
        OLD.fees_cents, OLD.taxable_disb_cents, OLD.nontaxable_disb_cents, OLD.discount_cents, OLD.tax_cents,
        OLD.total_cents, OLD.number, OLD.matter_id, OLD.payer_id, OLD.tax_breakdown, OLD.issue_date,
        OLD.due_date, OLD.approved_by, OLD.approved_at, OLD.approval_id, OLD.signature_ref,
        OLD.interest_rate_ppm, OLD.interest_terms_doc_id, OLD.place_of_supply, OLD.place_of_supply_province) THEN
      RAISE EXCEPTION 'DM-041: bill % is % and its amounts, dates, signer and terms are frozen; issue a credit note', OLD.id, OLD.status;
    END IF;
    -- BIL-L-001: the signature is set once, all three columns together, on an approved bill
    -- that is not yet delivered. It is never changed or cleared afterwards.
    IF (NEW.signed_by, NEW.signed_at, NEW.signature_hash) IS DISTINCT FROM (OLD.signed_by, OLD.signed_at, OLD.signature_hash)
       AND NOT (OLD.status = 'approved' AND NEW.status = 'approved'
                AND OLD.delivered_at IS NULL AND NEW.delivered_at IS NULL
                AND OLD.signed_by IS NULL AND OLD.signed_at IS NULL AND OLD.signature_hash IS NULL
                AND NEW.signed_by IS NOT NULL AND NEW.signed_at IS NOT NULL AND NEW.signature_hash IS NOT NULL) THEN
      RAISE EXCEPTION 'DM-041: bill % is %; its signature is set once, together, on an approved undelivered bill', OLD.id, OLD.status;
    END IF;
  END IF;
  -- RV2-018: approval needs an approved billing.bill_approval gate on this bill (G-B2).
  IF NEW.status = 'approved' AND OLD.status <> 'approved' AND NOT EXISTS (
       SELECT 1 FROM gate.approval_request ar WHERE ar.id = NEW.approval_id AND ar.status = 'approved'
          AND ar.gate_code = 'billing.bill_approval' AND ar.payload_type = 'billing.bill' AND ar.payload_id = NEW.id) THEN
    RAISE EXCEPTION 'DM-067: bill % needs an approved billing.bill_approval gate', NEW.id;
  END IF;
  RETURN NEW;
END $$;
