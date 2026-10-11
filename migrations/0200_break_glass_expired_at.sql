-- module: kernel/identity
-- When a break-glass grant was expired (docs/QUESTIONS.md PORT-26). Additive only.
-- PGGrants.ExpireDue sets expired_at once, in the same transaction as the grant's expiry
-- audit event, and skips grants that have it, so a grant is audited as expired exactly once.
-- Grants written before this migration, and by a release that does not set it, keep NULL.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

ALTER TABLE audit.break_glass
  ADD COLUMN expired_at timestamptz;

RESET lock_timeout;
RESET statement_timeout;
