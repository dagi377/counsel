-- module: app/legal/work
-- Backfill the kernel subject pair of work items from matter_id (docs/QUESTIONS.md Q-T-034.1).
-- Every work item linked to a matter gets ('matter', matter_id). matter_id stays for
-- app/legal. Batches of at most 10000 rows. Runs as the migration role, which must bypass
-- RLS (work.work_item has FORCE ROW LEVEL SECURITY with policies for counsel_app only).
-- Only rows whose pair is still empty are touched, so a rerun changes nothing.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

DO $$
DECLARE n integer;
BEGIN
  LOOP
    UPDATE work.work_item w
       SET subject_kind = 'matter', subject_id = w.matter_id
     WHERE w.id IN (SELECT id FROM work.work_item
                     WHERE matter_id IS NOT NULL AND subject_kind IS NULL
                     LIMIT 10000);
    GET DIAGNOSTICS n = ROW_COUNT;
    EXIT WHEN n = 0;
  END LOOP;
END $$;

RESET lock_timeout;
RESET statement_timeout;
