-- expect: MIG-060 ALTER COLUMN TYPE
SET lock_timeout = '2s';
SET statement_timeout = '60s';
ALTER TABLE work.work_item ALTER COLUMN title TYPE varchar(40);
