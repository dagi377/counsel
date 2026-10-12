-- module: app/legal/billing
-- Pre-bill review (G-B1, docs/QUESTIONS.md Q-GE3-MONEY). Additive only: a new table.
-- An approved billing.prebill_review gate moves a draft bill to 'prebill_approved' by writing
-- one row here; the bill itself stays 'draft' so the frozen bill status check and bill_guard
-- are unchanged and the bill goes on to final approval (billing.bill_approval). One row per
-- bill and per gate; rows never change.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module billing @retention RC-FIN @class review:P2
CREATE TABLE billing.prebill_review (
  bill_id      uuid PRIMARY KEY REFERENCES billing.bill(id),
  status       text NOT NULL DEFAULT 'prebill_approved' CHECK (status = 'prebill_approved'),
  matter_id    uuid NOT NULL REFERENCES matter.matter(id),
  period_from  date,
  period_to    date NOT NULL,
  approval_id  uuid NOT NULL UNIQUE,
  approved_by  uuid NOT NULL REFERENCES core.app_user(id),
  approved_at  timestamptz NOT NULL DEFAULT now(),
  CHECK (period_from IS NULL OR period_from <= period_to)
);
CREATE TRIGGER prebill_review_immutable BEFORE UPDATE OR DELETE ON billing.prebill_review
  FOR EACH ROW EXECUTE FUNCTION core.forbid_mutation();

REVOKE UPDATE, DELETE ON billing.prebill_review FROM PUBLIC;
GRANT SELECT, INSERT ON billing.prebill_review TO counsel_app, counsel_jobs;
GRANT SELECT ON billing.prebill_review TO counsel_integrity;
