-- module: kernel/gate
-- Gate payload bytes and recommendations (docs/QUESTIONS.md Q-T-007.3). Additive only.
-- A gate version stores the RFC 8785 (JCS) bytes of its payload and their SHA-256, so Decide
-- reads the stored payload instead of trusting an app hook. An approval above the decider's
-- authority limit is recorded as decision 'recommend': it never counts as an approval and
-- never closes the request.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE gate.approval_request
  ADD COLUMN payload_jcs    bytea,                  -- JCS bytes of the payload; NULL on rows opened before this migration
  ADD COLUMN payload_sha256 bytea;                  -- sha256(payload_jcs); equals payload_hash when set

ALTER TABLE gate.approval_request
  ADD CONSTRAINT approval_request_payload_pair_ck CHECK ((payload_jcs IS NULL) = (payload_sha256 IS NULL)) NOT VALID,
  ADD CONSTRAINT approval_request_payload_sha_ck CHECK (
    payload_jcs IS NULL OR (payload_sha256 = sha256(payload_jcs) AND payload_sha256 = payload_hash)) NOT VALID;

-- Append 'recommend' to the decision values (widening only; every stored row still passes).
-- 0001 left the column CHECK unnamed, so it is found by its definition.
DO $$
DECLARE c text;
BEGIN
  SELECT conname INTO STRICT c FROM pg_constraint
   WHERE conrelid = 'gate.gate_decision'::regclass AND contype = 'c'
     AND pg_get_constraintdef(oid) LIKE '%''changes_requested''%';
  EXECUTE format('ALTER TABLE gate.gate_decision DROP CONSTRAINT %I', c);
END $$;
ALTER TABLE gate.gate_decision
  ADD CONSTRAINT gate_decision_verdict_ck
    CHECK (decision IN ('approve','reject','changes_requested','recommend')) NOT VALID;

-- Same checks as 0001; only the closing branch changes: a 'recommend' leaves the request
-- pending, so another decider with a high enough limit still has to approve.
CREATE OR REPLACE FUNCTION gate.check_decision() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, gate, core, agent, audit AS $$
DECLARE r gate.approval_request%ROWTYPE; u core.app_user%ROWTYPE; sole boolean; run_owner uuid; approvals int;
BEGIN
  SELECT * INTO r FROM gate.approval_request WHERE id = NEW.approval_id FOR UPDATE;
  IF r.status <> 'pending' THEN
    RAISE EXCEPTION 'DM-071: approval % is %, not pending', r.id, r.status;
  END IF;
  IF now() >= r.expires_at THEN                                  -- RV2-003 (the gate.expire job persists 'expired')
    RAISE EXCEPTION 'DM-076: approval % expired at %', r.id, r.expires_at USING ERRCODE = 'P0001';
  END IF;
  IF NEW.payload_hash <> r.payload_hash THEN
    RAISE EXCEPTION 'DM-072: decision payload hash does not match the request (payload changed)';
  END IF;
  SELECT * INTO u FROM core.app_user WHERE id = NEW.decided_by;   -- RV2-017
  IF u.principal_kind <> 'human' OR u.is_system_admin OR u.status <> 'active' THEN
    RAISE EXCEPTION 'DM-075: agent_cannot_decide: % (kind %, admin %) may not decide gates', NEW.decided_by, u.principal_kind, u.is_system_admin;
  END IF;
  SELECT on_behalf_of INTO run_owner FROM agent.agent_run WHERE id = r.agent_run_id;
  IF NEW.decided_by = r.requested_by OR NEW.decided_by = run_owner THEN
    SELECT sole_practitioner INTO sole FROM core.firm;
    IF NOT (coalesce(sole, false) AND NEW.self_approval_exception) THEN
      RAISE EXCEPTION 'DM-070: maker cannot approve own item (approval %)', r.id;
    END IF;
    IF r.gate_code IN ('trust.withdrawal','trust.transfer','trust.reconciliation_signoff','trust.regulatory_release')
       AND NEW.effective_at < NEW.decided_at + interval '24 hours' THEN
      RAISE EXCEPTION 'DM-073: self-approval on % requires a 24h cooling-off (verify policy)', r.gate_code;
    END IF;
    INSERT INTO audit.audit_event(stream, actor_kind, actor_user_id, action, object_type, object_id, matter_id, approval_id, summary)
    VALUES ('gate', 'user', NEW.decided_by, 'sod_solo_exception', r.payload_type, r.payload_id, r.matter_id, r.id,
            'sole-practitioner self-approval on ' || r.gate_code);
  ELSIF NEW.self_approval_exception THEN
    RAISE EXCEPTION 'DM-070: self_approval_exception set but decider is not the maker';
  END IF;
  PERFORM set_config('counsel.gate_tx', 'on', true);
  IF NEW.decision = 'recommend' THEN
    NULL;                                                        -- recorded only; the request stays pending
  ELSIF NEW.decision <> 'approve' THEN
    UPDATE gate.approval_request SET status = CASE NEW.decision WHEN 'reject' THEN 'rejected' ELSE 'changes_requested' END,
                                     closed_at = now() WHERE id = r.id;
  ELSE
    -- RV2-002: count only approvals bound to this exact payload version.
    SELECT count(*) + 1 INTO approvals FROM gate.gate_decision
     WHERE approval_id = r.id AND decision = 'approve' AND payload_hash = r.payload_hash;
    IF approvals >= r.required_approvals THEN
      UPDATE gate.approval_request SET status = 'approved', closed_at = now() WHERE id = r.id;
    END IF;
  END IF;
  PERFORM set_config('counsel.gate_tx', 'off', true);
  RETURN NEW;
END $$;

RESET lock_timeout;
RESET statement_timeout;
