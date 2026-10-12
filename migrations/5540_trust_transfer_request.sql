-- module: app/legal/trust
-- A ledger-to-ledger transfer request (docs/QUESTIONS.md Q-TRANSFER-STORE). prepare_trust_transfer
-- records the row as pending_authorization in the same tx that opens its trust.transfer gate;
-- the gate's effect authorizes and posts it. Additive only: two nullable columns and a
-- replaced trigger function.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- Who prepared the transfer (a person: for an agent run, the user it acts for) and why. The
-- preparer never authorizes it.
ALTER TABLE trust.trust_transfer
  ADD COLUMN requested_by uuid,
  ADD COLUMN reason       text;

-- The preparer and reason freeze with the other terms once the transfer leaves draft.
CREATE OR REPLACE FUNCTION trust.transfer_terms_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status <> 'draft'
     AND (NEW.from_ledger_id, NEW.to_ledger_id, NEW.amount_cents, NEW.authority_doc_id, NEW.withdrawal_id,
          NEW.requested_by, NEW.reason)
         IS DISTINCT FROM (OLD.from_ledger_id, OLD.to_ledger_id, OLD.amount_cents, OLD.authority_doc_id, OLD.withdrawal_id,
          OLD.requested_by, OLD.reason) THEN
    RAISE EXCEPTION 'DM-067: transfer % is % and its terms are frozen', OLD.id, OLD.status;
  END IF;
  RETURN NEW;
END $$;
