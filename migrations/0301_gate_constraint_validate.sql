-- module: kernel/gate
-- Validates the constraints 0300_gate_payload_recommend.sql added NOT VALID (MIG-061: VALIDATE runs in a later file
-- than its NOT VALID add). VALIDATE takes SHARE UPDATE EXCLUSIVE, so reads and writes go on.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE gate.approval_request VALIDATE CONSTRAINT approval_request_payload_pair_ck;
ALTER TABLE gate.approval_request VALIDATE CONSTRAINT approval_request_payload_sha_ck;
ALTER TABLE gate.gate_decision VALIDATE CONSTRAINT gate_decision_verdict_ck;

RESET lock_timeout;
RESET statement_timeout;
