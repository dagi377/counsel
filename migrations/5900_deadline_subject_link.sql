-- module: app/legal/deadlines
-- Maps the kernel subject pair of cal.deadline onto the app's link columns
-- (docs/QUESTIONS.md Q-PORT-20). kernel/deadline writes only (subject_kind, subject_id) and
-- the pack columns; this BEFORE trigger sets matter_id or lead_id from the pair, so the
-- 0001 "exactly one of matter_id, lead_id" CHECK and the row-level policy see the app link.
-- A row the app writes with only matter_id or lead_id gets the pair the other way. title
-- falls back to the rule key and kind to 'other' when the kernel leaves them empty.
-- Then existing rows get their pair, in batches of at most 10000; only rows whose pair is
-- still empty are touched, so a rerun changes nothing. Runs as the migration role, which
-- must bypass RLS.
SET LOCAL lock_timeout TO '2s';
SET LOCAL statement_timeout TO '5min';

CREATE FUNCTION cal.deadline_subject_link() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.subject_kind = 'matter' AND NEW.matter_id IS NULL AND NEW.lead_id IS NULL THEN
    NEW.matter_id := NEW.subject_id;
  ELSIF NEW.subject_kind = 'lead' AND NEW.matter_id IS NULL AND NEW.lead_id IS NULL THEN
    NEW.lead_id := NEW.subject_id;
  ELSIF NEW.subject_kind IS NULL AND NEW.matter_id IS NOT NULL THEN
    NEW.subject_kind := 'matter';
    NEW.subject_id := NEW.matter_id;
  ELSIF NEW.subject_kind IS NULL AND NEW.lead_id IS NOT NULL THEN
    NEW.subject_kind := 'lead';
    NEW.subject_id := NEW.lead_id;
  END IF;
  NEW.title := COALESCE(NEW.title, NEW.rule_key);
  NEW.kind := COALESCE(NEW.kind, 'other');
  RETURN NEW;
END $$;

CREATE TRIGGER deadline_subject_link
  BEFORE INSERT ON cal.deadline
  FOR EACH ROW EXECUTE FUNCTION cal.deadline_subject_link();

DO $$
DECLARE n integer;
BEGIN
  LOOP
    UPDATE cal.deadline d
       SET subject_kind = CASE WHEN d.matter_id IS NOT NULL THEN 'matter' ELSE 'lead' END,
           subject_id   = COALESCE(d.matter_id, d.lead_id)
     WHERE d.id IN (SELECT id FROM cal.deadline
                     WHERE subject_kind IS NULL
                       AND (matter_id IS NOT NULL OR lead_id IS NOT NULL)
                     LIMIT 10000);
    GET DIAGNOSTICS n = ROW_COUNT;
    EXIT WHEN n = 0;
  END LOOP;
END $$;
