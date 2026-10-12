-- module: app/legal/billing
-- Draft line detail (AGENT-PROD-2a). Additive only: columns with defaults and nullable columns.
-- billing.bill_line.amount_cents stays the line's billed net; these keep how it was reached, so
-- draft.Store reads back the write-down (BIL-C-023), markup (BIL-C-040) and discount shares
-- (BIL-C-022) of a draft line. All amounts are integer cents.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE billing.bill_line ADD COLUMN reduction_cents bigint NOT NULL DEFAULT 0;
ALTER TABLE billing.bill_line ADD COLUMN reduction_reason text;
ALTER TABLE billing.bill_line ADD COLUMN reduction_text text;
ALTER TABLE billing.bill_line ADD COLUMN reduction_shown boolean NOT NULL DEFAULT false;
ALTER TABLE billing.bill_line ADD COLUMN markup_cents bigint NOT NULL DEFAULT 0;
ALTER TABLE billing.bill_line ADD COLUMN line_discount_cents bigint NOT NULL DEFAULT 0;
ALTER TABLE billing.bill_line ADD COLUMN discount_share_cents bigint NOT NULL DEFAULT 0;

RESET lock_timeout;
RESET statement_timeout;
