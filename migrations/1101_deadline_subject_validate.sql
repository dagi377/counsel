-- module: kernel/deadline
-- Validates the subject and pack constraints added NOT VALID by 1100_deadline_subject.sql.
SET LOCAL lock_timeout TO '2s';
SET LOCAL statement_timeout TO '60s';

ALTER TABLE cal.deadline VALIDATE CONSTRAINT deadline_subject_pair_ck;
ALTER TABLE cal.deadline VALIDATE CONSTRAINT deadline_subject_kind_ck;
ALTER TABLE cal.deadline VALIDATE CONSTRAINT deadline_pack_rule_ck;
