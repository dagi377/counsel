-- module: kernel/webhook
-- Endpoint expiry notices claimed in the database (PORT-05). Additive only: one nullable
-- column, no other column changes. expiry_noticed_for holds the instant of the latest
-- expiring notice claimed for the current expires_at (expires_at minus the notice lead).
-- NULL means no notice was sent for it. Renewing an endpoint resets it to NULL.

ALTER TABLE platform.webhook
  ADD COLUMN expiry_noticed_for timestamptz;
