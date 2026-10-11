-- module: app/legal/trust
-- Withdrawal authorisation expiry, frozen terms and Form 9A signatures (docs/QUESTIONS.md
-- Q-W3-TRUST). Additive only: nullable columns, two new triggers and a new table.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- An authorisation lasts until expires_at. PGStore.Lock fails closed once it has passed, and
-- PGStore.ReleaseExpired moves expired authorisations to 'cancelled', which drops their hold
-- from trust.available_cents. Rows authorised before this migration have no expiry and are
-- treated as expired.
ALTER TABLE trust.withdrawal_request
  ADD COLUMN authorized_at timestamptz,
  ADD COLUMN expires_at    timestamptz;
ALTER TABLE trust.withdrawal_request
  ADD CONSTRAINT withdrawal_request_expiry_ck CHECK (expires_at IS NULL OR expires_at > authorized_at) NOT VALID;
-- The expiry index is built CONCURRENTLY in 5503_withdrawal_authority_index.sql (MIG-061).

-- The approved gate binds the terms: once a withdrawal is proposed its terms never change, in
-- any status. A change is a new request (and a new gate). The Form 9A record may be attached
-- once while the request awaits authorisation and is frozen after that.
CREATE FUNCTION trust.withdrawal_terms_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (NEW.amount_cents, NEW.payee, NEW.ledger_id, NEW.method, NEW.bill_id, NEW.purpose_category,
      NEW.initiated_by, NEW.receiving_account)
     IS DISTINCT FROM (OLD.amount_cents, OLD.payee, OLD.ledger_id, OLD.method, OLD.bill_id, OLD.purpose_category,
      OLD.initiated_by, OLD.receiving_account) THEN
    RAISE EXCEPTION 'DM-067: withdrawal % terms are fixed once proposed; open a new request', OLD.id;
  END IF;
  IF NEW.form_9a_doc_id IS DISTINCT FROM OLD.form_9a_doc_id
     AND (OLD.form_9a_doc_id IS NOT NULL OR OLD.status NOT IN ('proposed','pending_authorization')) THEN
    RAISE EXCEPTION 'DM-067: withdrawal % Form 9A record is frozen', OLD.id;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER withdrawal_terms_frozen BEFORE UPDATE ON trust.withdrawal_request
  FOR EACH ROW EXECUTE FUNCTION trust.withdrawal_terms_frozen();

-- A ledger-to-ledger transfer is frozen once it leaves draft (the trust.transfer gate is open).
CREATE FUNCTION trust.transfer_terms_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.status <> 'draft'
     AND (NEW.from_ledger_id, NEW.to_ledger_id, NEW.amount_cents, NEW.authority_doc_id, NEW.withdrawal_id)
         IS DISTINCT FROM (OLD.from_ledger_id, OLD.to_ledger_id, OLD.amount_cents, OLD.authority_doc_id, OLD.withdrawal_id) THEN
    RAISE EXCEPTION 'DM-067: transfer % is % and its terms are frozen', OLD.id, OLD.status;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER transfer_terms_frozen BEFORE UPDATE ON trust.trust_transfer
  FOR EACH ROW EXECUTE FUNCTION trust.transfer_terms_frozen();

-- Form 9A signature: who signed the EFT authorisation and when. The signer is this record,
-- never the user who uploaded the document. Append-only; one signature per record.
-- @module trust @retention RC-FIN @class authority:P1
CREATE TABLE trust.form_9a_signature (
  id              uuid PRIMARY KEY DEFAULT core.uuidv7(),
  form_9a_doc_id  uuid NOT NULL UNIQUE REFERENCES docs.document(id),
  signed_by       uuid NOT NULL REFERENCES core.app_user(id),
  signed_at       timestamptz NOT NULL,
  recorded_at     timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER form_9a_signature_immutable BEFORE UPDATE OR DELETE ON trust.form_9a_signature
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

GRANT SELECT, INSERT ON trust.form_9a_signature TO counsel_app;
GRANT SELECT ON trust.form_9a_signature TO counsel_jobs, counsel_integrity;
