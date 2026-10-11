-- module: kernel/webhook
-- Validates the constraints 1400_webhook_delivery_subject.sql added NOT VALID (MIG-061: VALIDATE runs in a later file
-- than its NOT VALID add). VALIDATE takes SHARE UPDATE EXCLUSIVE, so reads and writes go on.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE platform.webhook_delivery VALIDATE CONSTRAINT webhook_delivery_payload_ref_fk;
ALTER TABLE platform.webhook_delivery VALIDATE CONSTRAINT webhook_delivery_ack_ck;

RESET lock_timeout;
RESET statement_timeout;
