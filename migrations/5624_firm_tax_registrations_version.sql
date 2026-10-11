-- module: app/legal/billing
-- Version of the firm's provincial sales tax registrations (docs/QUESTIONS.md
-- Q-TAX-API). Additive only: one column with a default. PATCH /firm/tax-registrations
-- needs an integer version for If-Match and ETag (ARC-I-006); core.firm has no row_version.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

ALTER TABLE core.firm
  ADD COLUMN tax_registrations_version integer NOT NULL DEFAULT 1;
