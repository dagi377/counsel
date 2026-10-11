-- module: kernel/outbox
-- Validates the constraints 0500_outbox_subject_seq.sql added NOT VALID (MIG-061: VALIDATE runs in a later file
-- than its NOT VALID add). VALIDATE takes SHARE UPDATE EXCLUSIVE, so reads and writes go on.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE platform.outbox VALIDATE CONSTRAINT outbox_subject_seq_ck;

RESET lock_timeout;
RESET statement_timeout;
