-- module: app/legal/trust
-- Narrow the UPDATE grant on trust.exception (5500 granted it table-wide). A trust exception's
-- kind, receipt, ledger, matter and amounts are fixed when it opens; the app only records how
-- it was resolved, so UPDATE is granted on the resolution columns alone.
SET lock_timeout = '2s';
SET statement_timeout = '5min';

REVOKE UPDATE ON trust.exception FROM counsel_app, counsel_jobs;
GRANT UPDATE (resolved_at, resolved_by, resolution) ON trust.exception TO counsel_app, counsel_jobs;

RESET lock_timeout;
RESET statement_timeout;
