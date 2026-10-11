-- module: kernel/ledger
-- migrate: no-transaction
-- Indexes for 0400_ledger_book.sql, built CONCURRENTLY so writes go on (MIG-061).
-- The file runs outside a transaction, one statement at a time. IF NOT EXISTS makes a
-- retry a no-op; drop an INVALID index left by a failed build before the retry.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
CREATE INDEX CONCURRENTLY IF NOT EXISTS journal_entry_book_date_idx ON gl.journal_entry (book, entry_date);

RESET lock_timeout;
RESET statement_timeout;
