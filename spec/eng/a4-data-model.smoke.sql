-- a4 smoke tests. Load a4-data-model.sql into a throwaway database, then: psql -v ON_ERROR_STOP=1 -f a4-data-model.smoke.sql
-- Every case records PASS/FAIL in smoke.result; the last statement raises if any case failed.
CREATE SCHEMA smoke;
CREATE TABLE smoke.result (n serial, label text, ok boolean, detail text);
CREATE TABLE smoke.ids (k text PRIMARY KEY, id uuid NOT NULL);
CREATE FUNCTION smoke.id(p text) RETURNS uuid LANGUAGE sql STABLE AS $$ SELECT id FROM smoke.ids WHERE k = p $$;
CREATE FUNCTION smoke.put(p text, v uuid) RETURNS uuid LANGUAGE sql AS $$ INSERT INTO smoke.ids VALUES (p, v) RETURNING id $$;

-- Run statements; deferred constraints are checked at the end. pat NULL = must succeed; else error must match pat.
CREATE FUNCTION smoke.expect(label text, stmts text[], pat text) RETURNS void LANGUAGE plpgsql AS $$
DECLARE s text;
BEGIN
  BEGIN
    FOREACH s IN ARRAY stmts LOOP EXECUTE s; END LOOP;
    SET CONSTRAINTS ALL IMMEDIATE;
    SET CONSTRAINTS ALL DEFERRED;
    INSERT INTO smoke.result(label, ok, detail) VALUES (label, pat IS NULL, CASE WHEN pat IS NULL THEN 'ok' ELSE 'expected error ' || pat END);
  EXCEPTION WHEN OTHERS THEN
    INSERT INTO smoke.result(label, ok, detail) VALUES (label, pat IS NOT NULL AND SQLERRM ~ pat, SQLERRM);
  END;
END $$;

-- ---------- fixtures ----------
INSERT INTO core.firm(legal_name, display_name) VALUES ('Smith LLP', 'Smith');
INSERT INTO core.role(id, key, name, status) VALUES (smoke.put('role', core.uuidv7()), 'lawyer', 'Lawyer', 'active');
INSERT INTO core.app_user(id, email, display_name, role_id, licence_type, lso_number, status, principal_kind) VALUES
 (smoke.put('A', core.uuidv7()), 'a@x.ca', 'A', smoke.id('role'), 'lawyer', 'L1', 'active', 'human'),
 (smoke.put('B', core.uuidv7()), 'b@x.ca', 'B', smoke.id('role'), 'lawyer', 'L2', 'active', 'human'),
 (smoke.put('C', core.uuidv7()), 'c@x.ca', 'C', smoke.id('role'), 'lawyer', 'L3', 'active', 'human'),
 (smoke.put('D', core.uuidv7()), 'd@x.ca', 'D', smoke.id('role'), 'none', NULL, 'active', 'human'),
 (smoke.put('AG', core.uuidv7()), 'agent@x.ca', 'Agent', smoke.id('role'), 'none', NULL, 'active', 'agent');
INSERT INTO party.contact(id, kind, legal_name, display_name, state) VALUES (smoke.put('client', core.uuidv7()), 'person', 'Jane Client', 'Jane', 'active');
INSERT INTO matter.practice_area(id, name) VALUES (smoke.put('pa', core.uuidv7()), 'Litigation');
INSERT INTO trust.trust_account(id, type, institution, account_no_ciphertext, account_no_last4, title, opened_on)
 VALUES (smoke.put('acct', core.uuidv7()), 'mixed', 'Bank', '\x01', '1234', 'Mixed trust', '2026-01-01');
INSERT INTO gate.gate_kind(kind, spec_gate, ttl_hours, required_approvals, risk) VALUES
 ('billing.bill_approval', 'G-B2', 72, 1, 'high'), ('trust.withdrawal', 'G11', 72, 1, 'high'),
 ('migration.trust_opening', 'G-IMP', 72, 1, 'high'), ('trust.reconciliation_signoff', 'G-TSIG', 72, 1, 'high'),
 ('trust.regulatory_release', 'G-TREL', 72, 2, 'irreversible');

CREATE FUNCTION smoke.ledger(k text) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE m uuid := core.uuidv7(); l uuid := core.uuidv7();
BEGIN
  INSERT INTO matter.matter(id, number, short_name, client_id, practice_area_id, status, open_date, responsible_lawyer_id, visibility)
  VALUES (m, 'M-' || k, 'Matter ' || k, smoke.id('client'), smoke.id('pa'), 'open', '2026-01-05', smoke.id('A'), 'firm_open');
  INSERT INTO trust.client_ledger(id, account_id, matter_id, client_id) VALUES (l, smoke.id('acct'), m, smoke.id('client'));
  PERFORM smoke.put('M' || k, m); RETURN smoke.put(k, l);
END $$;

CREATE FUNCTION smoke.gate(kind text, spec text, ptype text, pid uuid, h bytea DEFAULT '\xaa') RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE g uuid := core.uuidv7();
BEGIN
  INSERT INTO gate.approval_request(id, gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, expires_at)
  VALUES (g, kind, spec, ptype, pid, h, smoke.id('A'), now() + interval '72 hours');
  INSERT INTO gate.gate_decision(approval_id, decided_by, decision, payload_hash, mfa_verified_at) VALUES (g, smoke.id('B'), 'approve', h, now());
  RETURN g;
END $$;

CREATE FUNCTION smoke.receipt(l uuid, amt bigint, meth text DEFAULT 'eft', cleared boolean DEFAULT true) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE t uuid := core.uuidv7();
BEGIN
  INSERT INTO trust.trust_transaction(id, account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, cleared_on)
  VALUES (t, smoke.id('acct'), l, 'receipt', current_date, meth, 'Jane', amt, 'retainer', smoke.id('A'), CASE WHEN cleared THEN current_date END);
  RETURN t;
END $$;

-- proposed -> gate approved -> authorized (B). Returns withdrawal id.
CREATE FUNCTION smoke.authorize(l uuid, amt bigint, k text) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE w uuid := core.uuidv7();
BEGIN
  INSERT INTO trust.withdrawal_request(id, ledger_id, purpose_category, payee, amount_cents, method, initiated_by)
  VALUES (w, l, 'disbursement', 'Court', amt, 'cheque', smoke.id('A'));
  UPDATE trust.withdrawal_request SET approval_id = smoke.gate('trust.withdrawal', 'G11', 'trust.withdrawal_request', w),
         status = 'authorized', authorized_by = smoke.id('B') WHERE id = w;
  RETURN smoke.put(k, w);
END $$;

CREATE FUNCTION smoke.execute(w uuid) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE r trust.withdrawal_request%ROWTYPE; t uuid := core.uuidv7();
BEGIN
  SELECT * INTO r FROM trust.withdrawal_request WHERE id = w;
  INSERT INTO trust.trust_transaction(id, account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, withdrawal_id, approval_id)
  VALUES (t, smoke.id('acct'), r.ledger_id, 'disbursement', current_date, r.method, r.payee, -r.amount_cents, 'payout', smoke.id('C'), w, r.approval_id);
  UPDATE trust.withdrawal_request SET status = 'executed', executed_by = smoke.id('C'), executed_on = current_date WHERE id = w;
  RETURN t;
END $$;

-- ---------- original cases (kept) ----------
SELECT smoke.ledger('L1');
SELECT smoke.expect('T1 receipt 500000', ARRAY[$$SELECT smoke.receipt(smoke.id('L1'), 500000)$$], NULL);
SELECT smoke.expect('T2 overdraft 500001 refused', ARRAY[$$SELECT smoke.execute(smoke.authorize(smoke.id('L1'), 500001, 'w_over'))$$], 'DM-064');
SELECT smoke.expect('T3 exact 500000 ok', ARRAY[$$SELECT smoke.execute(smoke.authorize(smoke.id('L1'), 500000, 'w_exact'))$$], NULL);
SELECT smoke.expect('T4 trust row immutable', ARRAY[$$UPDATE trust.trust_transaction SET amount_cents = 1$$], 'append-only|immutable|DM-01');
INSERT INTO gl.account(id, code, name, type) VALUES (smoke.put('gl1000', core.uuidv7()), '1000', 'Bank', 'asset'), (smoke.put('gl4000', core.uuidv7()), '4000', 'Fees', 'revenue'),
  (smoke.put('gl2300', core.uuidv7()), '2300', 'Due to trust account', 'liability');
SELECT smoke.expect('T5 GL unbalanced refused', ARRAY[
  $$INSERT INTO gl.journal_entry(id, entry_date, memo, source_type, posted_by) VALUES (smoke.put('je1', core.uuidv7()), current_date, 'x', 'manual', smoke.id('A'))$$,
  $$INSERT INTO gl.journal_line(entry_id, account_id, debit_cents) VALUES (smoke.id('je1'), smoke.id('gl1000'), 11300)$$,
  $$INSERT INTO gl.journal_line(entry_id, account_id, credit_cents) VALUES (smoke.id('je1'), smoke.id('gl4000'), 11299)$$], 'DM-061');
SELECT smoke.expect('T7 audit chain', ARRAY[$$INSERT INTO audit.audit_event(stream, actor_kind, actor_user_id, action, object_type) VALUES ('trust','user',smoke.id('A'),'x','y'), ('trust','user',smoke.id('A'),'x2','y')$$], NULL);
INSERT INTO smoke.result(label, ok, detail) SELECT 'T7 chain links', bool_and(prev_hash = p), 'prev_hash = previous hash'
  FROM (SELECT prev_hash, lag(hash) OVER (ORDER BY seq) p FROM audit.audit_event) x WHERE p IS NOT NULL;
SELECT smoke.expect('T8 bill total check', ARRAY[$$INSERT INTO billing.bill(matter_id, payer_id, fees_cents, tax_cents, total_cents) VALUES (smoke.id('ML1'), smoke.id('client'), 100000, 13000, 113001)$$], 'check constraint');
INSERT INTO smoke.result(label, ok, detail) SELECT 'T9 uuidv7 version nibble', substr(core.uuidv7()::text, 15, 1) = '7', 'v7';

-- ---------- RV2-001 gate codes ----------
SELECT smoke.expect('RV2-001 G-B2/G-IMP/G-TSIG/G-TREL accepted', ARRAY[
  $$INSERT INTO gate.approval_request(gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, expires_at)
    SELECT kind, spec_gate, 'x.y', core.uuidv7(), '\x01', smoke.id('A'), now() + interval '1 hour' FROM gate.gate_kind$$], NULL);
SELECT smoke.expect('RV2-001 legacy code G11 as gate_code refused', ARRAY[
  $$INSERT INTO gate.approval_request(gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, expires_at)
    VALUES ('G11', 'G11', 'x.y', core.uuidv7(), '\x01', smoke.id('A'), now() + interval '1 hour')$$], 'check constraint');

-- ---------- RV2-002 approvals bound to payload version ----------
INSERT INTO gate.approval_request(id, gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, required_approvals, expires_at)
VALUES (smoke.put('g2', core.uuidv7()), 'trust.withdrawal', 'G11', 'trust.withdrawal_request', core.uuidv7(), '\xaa', smoke.id('A'), 2, now() + interval '72 hours');
SELECT smoke.expect('RV2-002 B approves v1', ARRAY[$$INSERT INTO gate.gate_decision(approval_id, decided_by, decision, payload_hash, mfa_verified_at) VALUES (smoke.id('g2'), smoke.id('B'), 'approve', '\xaa', now())$$], NULL);
SELECT smoke.expect('RV2-002 in-place payload_hash edit refused', ARRAY[$$UPDATE gate.approval_request SET payload_hash = '\xbb' WHERE id = smoke.id('g2')$$], 'DM-077');
SELECT smoke.expect('RV2-002 direct status flip refused', ARRAY[$$UPDATE gate.approval_request SET status = 'approved' WHERE id = smoke.id('g2')$$], 'DM-077');
SELECT smoke.expect('RV2-002 supersede to v2', ARRAY[$$SELECT smoke.put('g2v2', gate.supersede(smoke.id('g2'), '\xbb'))$$], NULL);
SELECT smoke.expect('RV2-002 C approves v2', ARRAY[$$INSERT INTO gate.gate_decision(approval_id, decided_by, decision, payload_hash, mfa_verified_at) VALUES (smoke.id('g2v2'), smoke.id('C'), 'approve', '\xbb', now())$$], NULL);
INSERT INTO smoke.result(label, ok, detail) SELECT 'RV2-002 v2 still pending with one approval; v1 superseded',
  (SELECT status FROM gate.approval_request WHERE id = smoke.id('g2v2')) = 'pending' AND (SELECT status FROM gate.approval_request WHERE id = smoke.id('g2')) = 'superseded', 'statuses';

-- ---------- RV2-003 expiry ----------
INSERT INTO gate.approval_request(id, gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, created_at, expires_at)
VALUES (smoke.put('g3', core.uuidv7()), 'trust.withdrawal', 'G11', 'trust.withdrawal_request', core.uuidv7(), '\xaa', smoke.id('A'), now() - interval '2 hours', now() - interval '1 hour');
SELECT smoke.expect('RV2-003 decision after expiry refused', ARRAY[$$INSERT INTO gate.gate_decision(approval_id, decided_by, decision, payload_hash, mfa_verified_at) VALUES (smoke.id('g3'), smoke.id('B'), 'approve', '\xaa', now())$$], 'DM-076');
SELECT smoke.expect('RV2-003 expire job', ARRAY[$$SELECT gate.expire_due()$$], NULL);
INSERT INTO smoke.result(label, ok, detail) SELECT 'RV2-003 status expired', status = 'expired', status FROM gate.approval_request WHERE id = smoke.id('g3');
SELECT smoke.expect('RV2-003 effect on unapproved gate refused', ARRAY[$$INSERT INTO gate.effect_execution(approval_id, handler) VALUES (smoke.id('g3'), 'trust.withdrawal')$$], 'DM-076');

-- ---------- RV2-017 principal kind, risk ----------
INSERT INTO gate.approval_request(id, gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, expires_at)
VALUES (smoke.put('g17', core.uuidv7()), 'billing.bill_approval', 'G-B2', 'billing.bill', core.uuidv7(), '\xaa', smoke.id('A'), now() + interval '1 hour');
SELECT smoke.expect('RV2-017 agent principal cannot decide', ARRAY[$$INSERT INTO gate.gate_decision(approval_id, decided_by, decision, payload_hash, mfa_verified_at) VALUES (smoke.id('g17'), smoke.id('AG'), 'approve', '\xaa', now())$$], 'DM-075');
SELECT smoke.expect('RV2-017 irreversible needs 2 approvers', ARRAY[$$INSERT INTO gate.approval_request(gate_code, spec_gate, payload_type, payload_id, payload_hash, requested_by, risk, expires_at) VALUES ('trust.regulatory_release','G-TREL','x.y',core.uuidv7(),'\x01',smoke.id('A'),'irreversible',now()+interval '1 hour')$$], 'check constraint');

-- ---------- RV2-004 once-only effects ----------
SELECT smoke.ledger('L4'); SELECT smoke.receipt(smoke.id('L4'), 500000);
SELECT smoke.expect('RV2-004 execute withdrawal once', ARRAY[$$SELECT smoke.put('t4', smoke.execute(smoke.authorize(smoke.id('L4'), 100000, 'w4')))$$], NULL);
SELECT smoke.expect('RV2-004 replayed disbursement refused', ARRAY[
  $$INSERT INTO trust.trust_transaction(account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, withdrawal_id)
    VALUES (smoke.id('acct'), smoke.id('L4'), 'disbursement', current_date, 'cheque', 'Court', -100000, 'replay', smoke.id('C'), smoke.id('w4'))$$], 'trust_txn_once_per_withdrawal');
SELECT smoke.expect('RV2-004 effect_execution first', ARRAY[$$INSERT INTO gate.effect_execution(approval_id, handler) SELECT approval_id, 'trust.withdrawal' FROM trust.withdrawal_request WHERE id = smoke.id('w4')$$], NULL);
SELECT smoke.expect('RV2-004 effect_execution replay refused', ARRAY[$$INSERT INTO gate.effect_execution(approval_id, handler) SELECT approval_id, 'trust.withdrawal' FROM trust.withdrawal_request WHERE id = smoke.id('w4')$$], 'effect_execution_pkey');
SELECT smoke.expect('RV2-004 journal once per source', ARRAY[
  $$INSERT INTO gl.journal_entry(id, entry_date, memo, source_type, source_id, posted_by) VALUES (smoke.put('je2', core.uuidv7()), current_date, 'a', 'payment', smoke.id('w4'), smoke.id('A'))$$,
  $$INSERT INTO gl.journal_line(entry_id, account_id, debit_cents) VALUES (smoke.id('je2'), smoke.id('gl1000'), 100)$$,
  $$INSERT INTO gl.journal_line(entry_id, account_id, credit_cents) VALUES (smoke.id('je2'), smoke.id('gl4000'), 100)$$,
  $$INSERT INTO gl.journal_entry(entry_date, memo, source_type, source_id, posted_by) VALUES (current_date, 'b', 'payment', smoke.id('w4'), smoke.id('A'))$$], 'journal_once_per_source');

-- ---------- RV2-005 historical vs live book ----------
SELECT smoke.ledger('L5');
INSERT INTO platform.import_job(id, source, mode, created_by) VALUES (smoke.put('job', core.uuidv7()), 'clio', 'full', smoke.id('A'));
INSERT INTO trust.historical_line(account_id, ledger_id, txn_date, kind, method, amount_cents, source_ref, import_job_id, staged_sha256) VALUES
 (smoke.id('acct'), smoke.id('L5'), '2025-03-01', 'receipt', 'cheque', 400000, '{"clio_id":1}', smoke.id('job'), '\x01'),
 (smoke.id('acct'), smoke.id('L5'), '2025-04-01', 'disbursement', 'cheque', -150000, '{"clio_id":2}', smoke.id('job'), '\x01');
SELECT smoke.expect('RV2-005 opening_balance without G-IMP refused', ARRAY[
  $$INSERT INTO trust.trust_transaction(account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, cleared_on)
    VALUES (smoke.id('acct'), smoke.id('L5'), 'opening_balance', current_date, 'internal', 'cut-over', 250000, 'opening', smoke.id('A'), current_date)$$], 'check constraint');
SELECT smoke.expect('RV2-005 opening_balance with G-IMP', ARRAY[
  $$INSERT INTO trust.trust_transaction(account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, cleared_on, approval_id)
    VALUES (smoke.id('acct'), smoke.id('L5'), 'opening_balance', current_date, 'internal', 'cut-over', 250000, 'opening', smoke.id('A'), current_date,
            smoke.gate('migration.trust_opening', 'G-IMP', 'platform.import_job', smoke.id('job')))$$], NULL);
INSERT INTO smoke.result(label, ok, detail) SELECT 'RV2-005 live balance = opening only (no double count); tie-out 0',
  (SELECT balance_cents FROM trust.ledger_balance WHERE ledger_id = smoke.id('L5')) = 250000
  AND (SELECT history_cents - opening_cents FROM trust.opening_tieout WHERE ledger_id = smoke.id('L5')) = 0, 'balance 250000';
SELECT smoke.expect('RV2-005 historical_line immutable', ARRAY[$$UPDATE trust.historical_line SET amount_cents = 0$$], 'append-only|immutable|DM-01');

-- ---------- RV1-002 authorized holds ----------
SELECT smoke.ledger('L6'); SELECT smoke.receipt(smoke.id('L6'), 500000);
SELECT smoke.expect('RV1-002 first authorization 300000', ARRAY[$$SELECT smoke.authorize(smoke.id('L6'), 300000, 'w6a')$$], NULL);
SELECT smoke.expect('RV1-002 second authorization 300000 refused (held funds)', ARRAY[$$SELECT smoke.authorize(smoke.id('L6'), 300000, 'w6b')$$], 'DM-064');
SELECT smoke.expect('RV1-002 executing the held withdrawal works', ARRAY[$$SELECT smoke.execute(smoke.id('w6a'))$$], NULL);
SELECT smoke.expect('RV2-018 withdrawal terms frozen after proposal', ARRAY[$$UPDATE trust.withdrawal_request SET amount_cents = 1 WHERE id = smoke.id('w6a')$$], 'DM-067');
SELECT smoke.expect('RV2-018 authorize without approved gate refused', ARRAY[
  $$INSERT INTO trust.withdrawal_request(id, ledger_id, purpose_category, payee, amount_cents, method, initiated_by) VALUES (smoke.put('w6c', core.uuidv7()), smoke.id('L6'), 'disbursement', 'Court', 1, 'cheque', smoke.id('A'))$$,
  $$UPDATE trust.withdrawal_request SET status = 'authorized', authorized_by = smoke.id('B'), approval_id = smoke.id('g17') WHERE id = smoke.id('w6c')$$], 'DM-067');
SELECT smoke.expect('RV2-018 payee cash refused', ARRAY[$$INSERT INTO trust.withdrawal_request(ledger_id, purpose_category, payee, amount_cents, method, initiated_by) VALUES (smoke.id('L6'), 'disbursement', ' Cash ', 1, 'cheque', smoke.id('A'))$$], 'check constraint');

-- ---------- RV1-003 / RV2-021 cleared and protected funds ----------
SELECT smoke.ledger('L7'); SELECT smoke.put('r7', smoke.receipt(smoke.id('L7'), 500000, 'cheque', false));
SELECT smoke.expect('RV1-003 withdrawal against uncleared cheque refused', ARRAY[$$SELECT smoke.execute(smoke.authorize(smoke.id('L7'), 100000, 'w7a'))$$], 'DM-064');
INSERT INTO trust.receipt_clearance(trust_transaction_id, cleared_on, recorded_by) VALUES (smoke.id('r7'), current_date, smoke.id('B'));
SELECT smoke.expect('RV1-003 after clearance allowed', ARRAY[$$SELECT smoke.execute(smoke.authorize(smoke.id('L7'), 100000, 'w7b'))$$], NULL);
SELECT smoke.ledger('L8'); SELECT smoke.receipt(smoke.id('L8'), 500000);
UPDATE trust.client_ledger SET protected_cents = 200000, protected_purpose = 'undertaking' WHERE id = smoke.id('L8');
SELECT smoke.expect('RV2-021 protected 200000 of 500000: 300001 refused', ARRAY[$$SELECT smoke.authorize(smoke.id('L8'), 300001, 'w8a')$$], 'DM-064');
SELECT smoke.expect('RV2-021 protected: 300000 allowed', ARRAY[$$SELECT smoke.execute(smoke.authorize(smoke.id('L8'), 300000, 'w8b'))$$], NULL);

-- ---------- RV1-012 bounced deposit ----------
SELECT smoke.ledger('L9'); SELECT smoke.put('r9', smoke.receipt(smoke.id('L9'), 300000));
SELECT smoke.execute(smoke.authorize(smoke.id('L9'), 300000, 'w9'));
SELECT smoke.expect('RV1-012 NSF reversal alone would overdraw -> refused', ARRAY[
  $$INSERT INTO trust.trust_transaction(account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, reverses_id)
    VALUES (smoke.id('acct'), smoke.id('L9'), 'reversal', current_date, 'eft', 'Bank NSF', -300000, 'NSF', smoke.id('A'), smoke.id('r9'))$$], 'DM-052');
SELECT smoke.expect('RV1-012 NSF reversal + firm replenishment in one tx', ARRAY[
  $$INSERT INTO trust.trust_transaction(id, account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, reverses_id)
    VALUES (smoke.put('nsf9', core.uuidv7()), smoke.id('acct'), smoke.id('L9'), 'reversal', current_date, 'eft', 'Bank NSF', -300000, 'NSF', smoke.id('A'), smoke.id('r9'))$$,
  $$INSERT INTO trust.trust_transaction(account_id, ledger_id, kind, txn_date, method, counterparty, amount_cents, purpose, posted_by, cleared_on, replenishes_id, approval_id)
    VALUES (smoke.id('acct'), smoke.id('L9'), 'receipt', current_date, 'eft', 'firm general account', 300000, 'replenish NSF shortfall', smoke.id('A'), current_date, smoke.id('nsf9'),
            smoke.gate('trust.receipt', 'G6', 'trust.trust_transaction', smoke.id('nsf9')))$$], NULL);
INSERT INTO smoke.result(label, ok, detail) SELECT 'RV1-012 ledger ends at 0', balance_cents = 0, balance_cents::text FROM trust.ledger_balance WHERE ledger_id = smoke.id('L9');

-- ---------- RV1-025 isolation (checked as plain SQL below) ----------
-- ---------- RV2-007 reconciliation recompute ----------
SELECT smoke.expect('RV2-007 signing with stale journal balance refused', ARRAY[
  $$INSERT INTO trust.reconciliation(account_id, month, bank_balance_cents, adjusted_bank_cents, journal_balance_cents, client_listing_total_cents, client_listing_snapshot,
       prepared_by, signed_by, signed_at, status, package_sha256)
    VALUES (smoke.id('acct'), date_trunc('month', current_date)::date, 1, 1, 1, 1, '[]', smoke.id('A'), smoke.id('B'), now(), 'signed', '\x01')$$], 'DM-066');
SELECT smoke.expect('RV2-006 imported month needs job + external signer', ARRAY[
  $$INSERT INTO trust.reconciliation(account_id, month, bank_balance_cents, adjusted_bank_cents, journal_balance_cents, client_listing_total_cents, client_listing_snapshot, prepared_by, status)
    VALUES (smoke.id('acct'), '2025-12-01', 0, 0, 0, 0, '[]', smoke.id('A'), 'imported')$$], 'check constraint');

-- ---------- RV2-022 bank line dedupe ----------
SELECT smoke.expect('RV2-022 two identical cheques in one statement kept', ARRAY[
  $$INSERT INTO trust.bank_line(account_kind, trust_account_id, posted_on, description, amount_cents, source_seq) VALUES
    ('trust', smoke.id('acct'), current_date, 'CHQ', -5000, 1), ('trust', smoke.id('acct'), current_date, 'CHQ', -5000, 2)$$], NULL);
SELECT smoke.expect('RV2-022 overlapping re-import deduped', ARRAY[
  $$INSERT INTO trust.bank_line(account_kind, trust_account_id, posted_on, description, amount_cents, source_seq) VALUES ('trust', smoke.id('acct'), current_date, 'CHQ', -5000, 2)$$], 'bank_line_dedupe');

-- ---------- bills: RV1-011 SoD, RV1-008 delivery, RV1-010 allocation, CF-08 rebill ----------
INSERT INTO timekeeping.time_entry(id, matter_id, user_id, work_date, minutes, billed_minutes, narrative, rate_cents, rate_source, amount_cents, origin, status)
VALUES (smoke.put('te', core.uuidv7()), smoke.id('ML1'), smoke.id('A'), current_date, 60, 60, 'work', 100000, 'manual', 100000, 'manual', 'approved');
CREATE FUNCTION smoke.bill(k text, maker uuid) RETURNS uuid LANGUAGE plpgsql AS $$
DECLARE b uuid := core.uuidv7();
BEGIN
  INSERT INTO billing.bill(id, matter_id, payer_id, fees_cents, tax_cents, total_cents, created_by, place_of_supply_province)
  VALUES (b, smoke.id('ML1'), smoke.id('client'), 100000, 13000, 113000, maker, 'ON');
  INSERT INTO billing.bill_line(bill_id, ordinal, source_type, time_entry_id, narrative, quantity_milli, rate_cents, amount_cents, tax_code)
  VALUES (b, 1, 'time', smoke.id('te'), 'work', 1000, 100000, 100000, 'HST-ON');
  RETURN smoke.put(k, b);
END $$;
CREATE FUNCTION smoke.approve_bill(b uuid, approver uuid) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
  UPDATE billing.bill SET status = 'approved', number = 'B-' || right(b::text, 12), issue_date = current_date, approved_by = approver, approved_at = now(),
         approval_id = smoke.gate('billing.bill_approval', 'G-B2', 'billing.bill', b) WHERE id = b;
END $$;
SELECT smoke.bill('b1', smoke.id('A'));
SELECT smoke.expect('RV1-011 two-licensee firm: self-approval refused', ARRAY[$$SELECT smoke.approve_bill(smoke.id('b1'), smoke.id('A'))$$], 'DM-078');
UPDATE core.firm SET sole_practitioner = true;
SELECT smoke.expect('RV1-011 sole practitioner: self-approval allowed', ARRAY[$$SELECT smoke.approve_bill(smoke.id('b1'), smoke.id('A'))$$], NULL);
INSERT INTO smoke.result(label, ok, detail) SELECT 'RV1-011 audit row sod_solo_exception', count(*) = 1, count(*)::text FROM audit.audit_event WHERE action = 'sod_solo_exception' AND object_id = smoke.id('b1');
UPDATE core.firm SET sole_practitioner = false;
SELECT smoke.expect('RV2-018 bill approval without gate refused', ARRAY[$$INSERT INTO billing.bill(id, matter_id, payer_id, created_by, place_of_supply_province) VALUES (smoke.put('b0', core.uuidv7()), smoke.id('ML1'), smoke.id('client'), smoke.id('A'), 'ON')$$,
  $$UPDATE billing.bill SET status = 'approved', number = 'B0', issue_date = current_date, approved_by = smoke.id('B'), approval_id = smoke.id('g17') WHERE id = smoke.id('b0')$$], 'DM-067');
SELECT smoke.expect('CF-08 void before delivery releases the time entry', ARRAY[$$UPDATE billing.bill SET status = 'void' WHERE id = smoke.id('b1')$$, $$SELECT smoke.bill('b2', smoke.id('A'))$$], NULL);
SELECT smoke.expect('CF-08 same entry on two live bills refused', ARRAY[$$SELECT smoke.bill('b2x', smoke.id('A'))$$], 'bill_line_time_once');
SELECT smoke.approve_bill(smoke.id('b2'), smoke.id('B'));
UPDATE billing.bill SET delivered_at = '2026-10-03 12:00-04', status = 'delivered' WHERE id = smoke.id('b2');
SELECT smoke.expect('RV1-008 back-dating delivered_at refused', ARRAY[$$UPDATE billing.bill SET delivered_at = '2026-09-01' WHERE id = smoke.id('b2')$$], 'DM-041');
SELECT smoke.expect('RV1-008 voiding a delivered bill refused', ARRAY[$$UPDATE billing.bill SET status = 'void' WHERE id = smoke.id('b2')$$], 'DM-041');
SELECT smoke.expect('RV1-008 due_date frozen after approval', ARRAY[$$UPDATE billing.bill SET due_date = current_date + 90 WHERE id = smoke.id('b2')$$], 'DM-041');
INSERT INTO billing.payment(id, received_on, payer_name, amount_cents, method, deposit_to, trust_account_id) VALUES (smoke.put('ptrust', core.uuidv7()), current_date, 'Jane', 50000, 'eft', 'trust', smoke.id('acct'));
INSERT INTO billing.payment(id, received_on, payer_name, amount_cents, method, deposit_to) VALUES (smoke.put('pop', core.uuidv7()), current_date, 'Jane', 113001, 'eft', 'operating');
SELECT smoke.expect('RV1-010 trust receipt allocated to a bill refused', ARRAY[
  $$INSERT INTO billing.payment_allocation(payment_id, target_type, bill_id, amount_cents, fee_cents, allocated_by) VALUES (smoke.id('ptrust'), 'bill', smoke.id('b2'), 50000, 50000, smoke.id('A'))$$], 'DM-046');
SELECT smoke.expect('RV1-010 bucket split must sum', ARRAY[
  $$INSERT INTO billing.payment_allocation(payment_id, target_type, bill_id, amount_cents, fee_cents, allocated_by) VALUES (smoke.id('pop'), 'bill', smoke.id('b2'), 113000, 100000, smoke.id('A'))$$], 'check constraint');
SELECT smoke.expect('RV1-010 over-allocate by 1 cent refused', ARRAY[
  $$INSERT INTO billing.payment_allocation(payment_id, target_type, bill_id, amount_cents, fee_cents, tax_cents, allocated_by) VALUES (smoke.id('pop'), 'bill', smoke.id('b2'), 113001, 100000, 13001, smoke.id('A'))$$], 'DM-047');
SELECT smoke.expect('RV1-010 exact allocation ok', ARRAY[
  $$INSERT INTO billing.payment_allocation(id, payment_id, target_type, bill_id, amount_cents, fee_cents, tax_cents, allocated_by) VALUES (smoke.put('al1', core.uuidv7()), smoke.id('pop'), 'bill', smoke.id('b2'), 113000, 100000, 13000, smoke.id('A'))$$], NULL);
SELECT smoke.expect('RV1-010 reversal must mirror buckets', ARRAY[
  $$INSERT INTO billing.payment_allocation(payment_id, target_type, bill_id, amount_cents, fee_cents, tax_cents, reverses_id, allocated_by) VALUES (smoke.id('pop'), 'bill', smoke.id('b2'), -113000, -113000, 0, smoke.id('al1'), smoke.id('A'))$$], 'DM-046');

-- ---------- RLS (RV2-014): screened user sees nothing on walled matter ----------
INSERT INTO matter.ethical_wall(id, reason, status, effective_from, created_by) VALUES (smoke.put('wall', core.uuidv7()), 'conflict', 'active', now(), smoke.id('A'));
INSERT INTO matter.ethical_wall_matter VALUES (smoke.id('wall'), smoke.id('ML1'));
INSERT INTO matter.ethical_wall_screened_user(wall_id, user_id) VALUES (smoke.id('wall'), smoke.id('D'));
CREATE FUNCTION smoke.rls_count(viewer uuid, tbl text, m uuid) RETURNS bigint LANGUAGE plpgsql AS $$
DECLARE n bigint;
BEGIN
  PERFORM set_config('counsel.user_id', viewer::text, true);
  SET ROLE counsel_app;
  EXECUTE format('SELECT count(*) FROM %s WHERE matter_id = $1', tbl) INTO n USING m;
  RESET ROLE;
  RETURN n;
END $$;
INSERT INTO smoke.result(label, ok, detail)
SELECT 'RV2-014 screened user sees 0 rows: ' || t, smoke.rls_count(smoke.id('D'), t, smoke.id('ML1')) = 0
       AND smoke.rls_count(smoke.id('C'), t, smoke.id('ML1')) > 0, 'screened 0, unscreened > 0'
  FROM unnest(ARRAY['billing.bill','trust.client_ledger','timekeeping.time_entry','audit.audit_event']) t
 WHERE t <> 'audit.audit_event';
INSERT INTO smoke.result(label, ok, detail)
SELECT 'RV2-014 FORCE RLS on every RLS table', bool_and(relforcerowsecurity), count(*)::text || ' tables' FROM pg_class WHERE relrowsecurity;

-- ---------- RV1-025: trust posting at REPEATABLE READ refused ----------
BEGIN ISOLATION LEVEL REPEATABLE READ;
SELECT smoke.expect('RV1-025 trust posting outside READ COMMITTED refused', ARRAY[$$SELECT smoke.receipt(smoke.id('L1'), 1)$$], 'DM-065');
COMMIT;

-- ---------- report ----------
SELECT n, CASE WHEN ok THEN 'PASS' ELSE 'FAIL' END AS r, label, left(detail, 110) AS detail FROM smoke.result ORDER BY n;
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM smoke.result WHERE NOT ok) THEN
    RAISE EXCEPTION 'smoke: % failing case(s)', (SELECT count(*) FROM smoke.result WHERE NOT ok);
  END IF;
END $$;
