-- module: app/legal/trust
-- The gate a withdrawal opened when it was requested (docs/QUESTIONS.md PG-WRITES-FIX).
-- Additive only: one nullable column. The request route stores the gate id in the tx that
-- opens the gate; cancel withdraws that gate. approval_id stays the approved gate (G11).
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

ALTER TABLE trust.withdrawal_request ADD COLUMN gate_id uuid;
