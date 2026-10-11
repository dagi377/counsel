-- module: kernel/gate
-- Gate decision verdict before the trigger rules. gate.check_decision (0300) is a BEFORE
-- INSERT trigger, and Postgres checks a row's CHECK constraints only after BEFORE triggers,
-- so a decision outside gate_decision_verdict_ck failed on the first DM rule it met (DM-070
-- for the maker) instead of on the verdict CHECK. The function now rejects an unknown verdict
-- first, as check_violation naming gate_decision_verdict_ck. Every other rule is unchanged.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

CREATE OR REPLACE FUNCTION gate.check_decision() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER
SET search_path = pg_catalog, gate, core, agent, audit AS $$
DECLARE r gate.approval_request%ROWTYPE; u core.app_user%ROWTYPE; sole boolean; run_owner uuid; approvals int;
BEGIN
  -- The verdict CHECK runs only after this BEFORE trigger, so an unknown verdict would
  -- otherwise fail on whichever DM rule it hit first. Reject it here, under the CHECK's name.
  IF NEW.decision IS NULL OR NEW.decision NOT IN ('approve','reject','changes_requested','recommend') THEN
    RAISE EXCEPTION 'new row for relation "gate_decision" violates check constraint "gate_decision_verdict_ck"'
      USING ERRCODE = 'check_violation', SCHEMA = 'gate', TABLE = 'gate_decision',
            CONSTRAINT = 'gate_decision_verdict_ck', DETAIL = format('decision %L is not a verdict', NEW.decision);
  END IF;
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
