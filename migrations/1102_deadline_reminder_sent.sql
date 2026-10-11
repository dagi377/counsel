-- module: kernel/deadline
-- Durable reminder sent-log (BOOT-REST-3). Additive only: a new table. The reminder worker
-- marks each notice it delivers here, so a restart never sends the same reminder twice in a
-- day. One row per (kind, deadline, user, day): a digest has no deadline, a notice to the
-- firm channel has no user. Rows hold ids and dates only, never deadline content.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '5s';

-- @module cal @retention RC-FILE @class delivery:P1
CREATE TABLE cal.deadline_reminder_sent (
  id           uuid PRIMARY KEY DEFAULT core.uuidv7(),
  kind         text NOT NULL CHECK (kind IN ('deadline.reminder','deadline.digest')),
  deadline_id  uuid REFERENCES cal.deadline(id),
  user_id      uuid REFERENCES core.app_user(id),
  day          date NOT NULL,
  sent_at      timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'deadline.digest') = (deadline_id IS NULL)),
  UNIQUE NULLS NOT DISTINCT (kind, deadline_id, user_id, day)
);
GRANT SELECT, INSERT ON cal.deadline_reminder_sent TO counsel_app, counsel_jobs;
GRANT SELECT ON cal.deadline_reminder_sent TO counsel_integrity;
