-- module: app/legal/billing
-- Operating payments over Postgres (PG-PAYMENT). Additive only: three new tables.
-- billing.payment_allocation is append-only and holds applied money only. A proposed
-- allocation set (HTTP-15) is kept here until the billing.payment_allocation effect applies
-- it; a newer set supersedes the older one, and nothing is deleted. A manual capture's
-- idempotency key and a reversal's reason have no column on the frozen billing.payment, so
-- each gets its own table keyed by the payment.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module billing @retention RC-FIN @class amounts:P1
CREATE TABLE billing.payment_allocation_proposal (
  id            uuid PRIMARY KEY DEFAULT core.uuidv7(),
  payment_id    uuid NOT NULL REFERENCES billing.payment(id),
  ordinal       integer NOT NULL CHECK (ordinal >= 0),
  bill_id       uuid NOT NULL REFERENCES billing.bill(id),
  amount_cents  bigint NOT NULL CHECK (amount_cents > 0),
  fee_cents     bigint NOT NULL CHECK (fee_cents >= 0),
  tax_cents     bigint NOT NULL CHECK (tax_cents >= 0),
  status        text NOT NULL DEFAULT 'proposed' CHECK (status IN ('proposed','superseded','applied')),
  proposed_by   uuid NOT NULL REFERENCES core.app_user(id),
  created_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (fee_cents + tax_cents = amount_cents)
);
CREATE INDEX payment_allocation_proposal_payment_idx ON billing.payment_allocation_proposal (payment_id, status);

-- A proposed row moves forward once (proposed -> superseded or applied); nothing else changes.
CREATE OR REPLACE FUNCTION billing.payment_allocation_proposal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'billing.payment_allocation_proposal rows are never deleted' USING ERRCODE = 'check_violation';
  END IF;
  IF (NEW.id, NEW.payment_id, NEW.ordinal, NEW.bill_id, NEW.amount_cents, NEW.fee_cents, NEW.tax_cents,
      NEW.proposed_by, NEW.created_at)
     IS DISTINCT FROM (OLD.id, OLD.payment_id, OLD.ordinal, OLD.bill_id, OLD.amount_cents, OLD.fee_cents,
      OLD.tax_cents, OLD.proposed_by, OLD.created_at) OR OLD.status <> 'proposed' THEN
    RAISE EXCEPTION 'billing.payment_allocation_proposal: only a proposed row changes, and only its status' USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER payment_allocation_proposal_guard BEFORE UPDATE OR DELETE ON billing.payment_allocation_proposal
  FOR EACH ROW EXECUTE FUNCTION billing.payment_allocation_proposal_guard();

-- The proposed rows of one payment never exceed the payment (DM-045 for proposals).
CREATE OR REPLACE FUNCTION billing.check_proposal_total() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, billing AS $$
DECLARE prop bigint; amt bigint;
BEGIN
  SELECT amount_cents INTO amt FROM billing.payment WHERE id = NEW.payment_id;
  SELECT coalesce(sum(amount_cents), 0) INTO prop FROM billing.payment_allocation_proposal
    WHERE payment_id = NEW.payment_id AND status = 'proposed';
  IF prop > amt THEN
    RAISE EXCEPTION 'DM-045: proposed allocations % exceed payment % on %', prop, amt, NEW.payment_id USING ERRCODE = 'check_violation';
  END IF;
  RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER payment_allocation_proposal_total AFTER INSERT ON billing.payment_allocation_proposal
  DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION billing.check_proposal_total();

-- @module billing @retention RC-FIN @class key:P1
CREATE TABLE billing.payment_capture_key (
  idempotency_key text PRIMARY KEY CHECK (idempotency_key <> '' AND length(idempotency_key) <= 200),
  payment_id      uuid NOT NULL UNIQUE REFERENCES billing.payment(id),
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER payment_capture_key_immutable BEFORE UPDATE OR DELETE ON billing.payment_capture_key
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

-- @module billing @retention RC-FIN @class reversal:P1
CREATE TABLE billing.payment_reversal (
  payment_id  uuid PRIMARY KEY REFERENCES billing.payment(id),
  reason      text NOT NULL CHECK (reason IN ('nsf','refund','chargeback')),
  -- the human who reversed the payment, never the original allocator (PAYMENT-FIX)
  reversed_by uuid NOT NULL REFERENCES core.app_user(id),
  reversed_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER payment_reversal_immutable BEFORE UPDATE OR DELETE ON billing.payment_reversal
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

REVOKE DELETE ON billing.payment_allocation_proposal, billing.payment_capture_key, billing.payment_reversal FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON billing.payment_allocation_proposal TO counsel_app, counsel_jobs;
GRANT SELECT, INSERT ON billing.payment_capture_key, billing.payment_reversal TO counsel_app, counsel_jobs;
GRANT SELECT ON billing.payment_allocation_proposal, billing.payment_capture_key, billing.payment_reversal TO counsel_integrity;
