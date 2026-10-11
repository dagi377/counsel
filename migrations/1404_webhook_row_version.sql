-- module: kernel/webhook
-- Endpoint row version for If-Match (HTTP-26, BOOT-REST-3). Additive only: one column with a
-- default. Every write to an endpoint bumps it; PATCH /webhook-endpoints/{id} compares it.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

ALTER TABLE platform.webhook
  ADD COLUMN row_version integer NOT NULL DEFAULT 1;
