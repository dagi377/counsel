-- expect: MIG-060 DROP TABLE
SET lock_timeout = '2s';
SET statement_timeout = '60s';
DROP TABLE work.old_queue;
