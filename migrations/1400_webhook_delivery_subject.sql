-- module: kernel/webhook
-- Ordered delivery per endpoint and subject (docs/QUESTIONS.md Q-T-036.2). Additive only.
-- A delivery names its subject, the message it carries and the outbox row that holds the
-- payload. The delivery row never stores the payload itself. acked_at is set when the
-- endpoint acknowledged the message; the next message of the same subject waits for it.

ALTER TABLE platform.webhook_delivery
  ADD COLUMN subject     text,                                     -- "<aggregate>:<aggregate_id>"
  ADD COLUMN message_id  uuid,                                     -- stable id sent to the endpoint; retries reuse it
  ADD COLUMN payload_ref uuid REFERENCES platform.outbox(id),      -- pointer to the outbox row, never the raw payload
  ADD COLUMN acked_at    timestamptz;
ALTER TABLE platform.webhook_delivery
  ADD CONSTRAINT webhook_delivery_ack_ck CHECK (acked_at IS NULL OR message_id IS NOT NULL) NOT VALID;
ALTER TABLE platform.webhook_delivery VALIDATE CONSTRAINT webhook_delivery_ack_ck;
CREATE INDEX webhook_delivery_pending_idx ON platform.webhook_delivery (webhook_id, subject, at)
  WHERE acked_at IS NULL AND subject IS NOT NULL;
CREATE UNIQUE INDEX webhook_delivery_attempt_uq ON platform.webhook_delivery (webhook_id, message_id, attempt)
  WHERE message_id IS NOT NULL;
