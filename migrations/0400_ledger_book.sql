-- module: kernel/ledger
-- Books in the journal, per-book period close (docs/QUESTIONS.md Q-GL-BOOK). Additive only.
-- Supersedes the 0001 header line "gl.* holds the operating book only" (CF-02): the app
-- registers its books (operating and trust for Counsel) and every entry names one. Existing
-- rows and callers that send no book stay in 'operating'. The engine knows no book name, so
-- the column CHECK is a format check; the app's chart decides which books exist.

ALTER TABLE gl.account
  ADD COLUMN book text NOT NULL DEFAULT 'operating';
ALTER TABLE gl.account
  ADD CONSTRAINT account_book_ck CHECK (book ~ '^[a-z][a-z0-9_]*$') NOT VALID;
ALTER TABLE gl.account VALIDATE CONSTRAINT account_book_ck;

ALTER TABLE gl.journal_entry
  ADD COLUMN book text NOT NULL DEFAULT 'operating';
ALTER TABLE gl.journal_entry
  ADD CONSTRAINT journal_entry_book_ck CHECK (book ~ '^[a-z][a-z0-9_]*$') NOT VALID;
ALTER TABLE gl.journal_entry VALIDATE CONSTRAINT journal_entry_book_ck;
CREATE INDEX journal_entry_book_date_idx ON gl.journal_entry (book, entry_date);

-- Append the 'trust' source kind (widening only; every stored row still passes).
-- 0001 left the column CHECK unnamed, so it is found by its definition.
DO $$
DECLARE c text;
BEGIN
  SELECT conname INTO STRICT c FROM pg_constraint
   WHERE conrelid = 'gl.journal_entry'::regclass AND contype = 'c'
     AND pg_get_constraintdef(oid) LIKE '%''interest_charge''%';
  EXECUTE format('ALTER TABLE gl.journal_entry DROP CONSTRAINT %I', c);
END $$;
ALTER TABLE gl.journal_entry
  ADD CONSTRAINT journal_entry_source_kind_ck
    CHECK (source_type IN ('bill','payment','payment_allocation','credit_note','expense','vendor_bill','write_off',
                           'interest_charge','trust_fee_transfer','nsf_replenishment','refund','manual','reversal',
                           'import','trust')) NOT VALID;
ALTER TABLE gl.journal_entry VALIDATE CONSTRAINT journal_entry_source_kind_ck;

-- Per-book period close. gl.period (keyed by month alone) stays and keeps meaning the
-- 'operating' book, so a release that still writes it closes exactly what it closed before.
-- @module gl @retention RC-FIN @class period:P0
CREATE TABLE gl.book_period (
  book         text NOT NULL CHECK (book ~ '^[a-z][a-z0-9_]*$'),
  month        date NOT NULL CHECK (extract(day FROM month) = 1),
  status       text NOT NULL DEFAULT 'open' CHECK (status IN ('open','closed')),
  closed_by    uuid REFERENCES core.app_user(id),
  closed_at    timestamptz,
  approval_id  uuid,
  PRIMARY KEY (book, month),
  CHECK ((status = 'closed') = (closed_at IS NOT NULL))
);

-- gl.period_closed reports whether month is closed for book: a gl.book_period row, or a
-- legacy gl.period row for the 'operating' book.
CREATE OR REPLACE FUNCTION gl.period_closed(p_book text, p_month date) RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT EXISTS (SELECT 1 FROM gl.book_period b
                  WHERE b.book = p_book AND b.month = date_trunc('month', p_month)::date AND b.status = 'closed')
      OR (p_book = 'operating' AND EXISTS (SELECT 1 FROM gl.period p
                  WHERE p.month = date_trunc('month', p_month)::date AND p.status = 'closed'))
$$;

-- Same DM-062 refusal as 0001, now per book of the entry.
CREATE OR REPLACE FUNCTION gl.before_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF gl.period_closed(NEW.book, NEW.entry_date) THEN
    RAISE EXCEPTION 'DM-062: GL period % of book % is closed', date_trunc('month', NEW.entry_date)::date, NEW.book;
  END IF;
  RETURN NEW;
END $$;

-- CF-02 reread: operating views show the operating book only.
CREATE VIEW gl.operating_journal_entry AS
  SELECT e.* FROM gl.journal_entry e WHERE e.book = 'operating';
CREATE VIEW gl.operating_journal_line AS
  SELECT l.* FROM gl.journal_line l JOIN gl.journal_entry e ON e.id = l.entry_id WHERE e.book = 'operating';

GRANT SELECT, INSERT, UPDATE ON gl.book_period TO counsel_app, counsel_jobs;
GRANT SELECT ON gl.book_period, gl.operating_journal_entry, gl.operating_journal_line TO counsel_integrity;
GRANT SELECT ON gl.operating_journal_entry, gl.operating_journal_line TO counsel_app, counsel_jobs;
