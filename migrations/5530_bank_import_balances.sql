-- module: app/legal/trust
-- Statement balances on trust.bank_import (PORT-STMT, docs/QUESTIONS.md). Additive only:
-- two nullable columns. The imported statement's closing balance is the bank balance of the
-- month's three-way reconciliation (HTTP-18b), so the import stores it, in cents, beside the
-- opening balance. Imports made before this file have NULL balances and read as not found.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE trust.bank_import
  ADD COLUMN opening_cents bigint,
  ADD COLUMN closing_cents bigint;

RESET lock_timeout;
RESET statement_timeout;
