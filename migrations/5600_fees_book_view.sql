-- module: app/legal/billing
-- Fees book (By-Law 9 lets it be a chronological file of the bills; docs/QUESTIONS.md).
-- Numbered billing.bill rows are the register; this view is the fees book. Additive only.
-- A void bill keeps its row and number and shows as 'void'.
SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE VIEW billing.fees_book AS
SELECT b.number,
       b.issue_date AS bill_date,
       b.payer_id   AS client_id,
       b.matter_id,
       b.fees_cents,
       b.taxable_disb_cents + b.nontaxable_disb_cents AS disbursements_cents,
       b.discount_cents,
       b.tax_cents,
       b.total_cents,
       CASE WHEN b.status = 'void' THEN 'void' ELSE 'issued' END AS status
  FROM billing.bill b
 WHERE b.number IS NOT NULL AND b.issue_date IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
