-- module: app/legal/billing
-- The firm's provincial sales tax registrations (docs/QUESTIONS.md Q-TAX-FIELDS). Additive only:
-- one column with a default on the core.firm settings row. Each entry is a province code whose
-- provincial sales tax the firm is registered to collect (BIL-T-020); empty means none.
-- core.firm has no migration block of its own and billing is its only reader of this column.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

ALTER TABLE core.firm
  ADD COLUMN tax_registration_provinces char(2)[] NOT NULL DEFAULT '{}';
