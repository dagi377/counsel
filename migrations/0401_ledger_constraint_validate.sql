-- module: kernel/ledger
-- Validates the constraints 0400_ledger_book.sql added NOT VALID (MIG-061: VALIDATE runs in a later file
-- than its NOT VALID add). VALIDATE takes SHARE UPDATE EXCLUSIVE, so reads and writes go on.

SET lock_timeout = '2s';
SET statement_timeout = '5min';
ALTER TABLE gl.account VALIDATE CONSTRAINT account_book_ck;
ALTER TABLE gl.journal_entry VALIDATE CONSTRAINT journal_entry_book_ck;
ALTER TABLE gl.journal_entry VALIDATE CONSTRAINT journal_entry_source_kind_ck;

RESET lock_timeout;
RESET statement_timeout;
