-- module: app/legal/trust
-- Trust receipts journal (Q-HTTP-16). Additive only: a new table.
-- By-law 9 record-keeping: money received into trust is recorded the day it arrives. POST
-- /trust/receipts writes a row here at once in state received_pending_classification and opens
-- trust.receipt_classification in the same transaction. Only that gate's effect posts the
-- receipt to the client ledger (trust.trust_transaction) and moves the row on.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

-- @module trust @retention RC-FIN @class amounts:P1
CREATE TABLE trust.receipt_journal (
  id                    uuid PRIMARY KEY DEFAULT core.uuidv7(),
  account_id            uuid NOT NULL REFERENCES trust.trust_account(id),
  matter_id             uuid NOT NULL REFERENCES matter.matter(id),
  ledger_id             uuid NOT NULL REFERENCES trust.client_ledger(id),
  received_on           date NOT NULL,
  amount_cents          bigint NOT NULL CHECK (amount_cents > 0),
  method                text NOT NULL CHECK (method IN ('cheque','eft','wire','cash','bank_draft','card')),
  payer                 text NOT NULL CHECK (payer <> ''),
  instrument_no         text,
  purpose               text NOT NULL CHECK (purpose <> ''),
  payment_id            uuid,
  proposed              text NOT NULL CHECK (proposed IN ('trust','general')),
  status                text NOT NULL DEFAULT 'received_pending_classification'
                        CHECK (status IN ('received_pending_classification','posted','classified_general')),
  gate_id               uuid,
  trust_transaction_id  uuid REFERENCES trust.trust_transaction(id),
  recorded_by           uuid NOT NULL REFERENCES core.app_user(id),
  recorded_at           timestamptz NOT NULL DEFAULT now(),
  classified_at         timestamptz,
  CHECK ((status = 'posted') = (trust_transaction_id IS NOT NULL)),
  CHECK ((status = 'received_pending_classification') = (classified_at IS NULL))
);
CREATE INDEX receipt_journal_day_idx ON trust.receipt_journal (account_id, received_on);
CREATE INDEX receipt_journal_pending_idx ON trust.receipt_journal (matter_id) WHERE status = 'received_pending_classification';
CREATE TRIGGER receipt_journal_no_delete BEFORE DELETE ON trust.receipt_journal FOR EACH ROW EXECUTE FUNCTION core.forbid_delete();

GRANT SELECT, INSERT, UPDATE ON trust.receipt_journal TO counsel_app, counsel_jobs;
GRANT SELECT ON trust.receipt_journal TO counsel_integrity;

RESET lock_timeout;
RESET statement_timeout;
