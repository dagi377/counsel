# Migrations

Postgres 16 schema changes, applied in file-name order by `cmd/counsel-migrate`.

## Numbering

Every file is `NNNN_<subject>.sql` (four digits, lower-case snake-case subject).

- `0001_init.sql` is the frozen contract DDL. It is the only file with a `CONTRACT`
  header and the only file in the `0001-0099` block.
- Every other module owns one 100-number block, listed in `tools/migration_blocks.txt`
  (for example `0100-0199 kernel/audit`). Kernel modules use `0100-4999`, app modules
  `5000-9999`.
- The first line of every later file names its owner: `-- module: kernel/audit`. The file
  number must fall inside that owner's block. A module writes migrations only for its own
  tables.
- Numbers are unique. Inside a block, take the next free number.

`tools/migration_blocks_test.go` enforces all of this in `make check`. A file outside its
module's block, a missing module line, an unknown module or a second `CONTRACT` header
fails the build.

## Expand-contract only

Files after `0001_init.sql` are additive. Release N may add schemas, tables, nullable
columns, columns with a default, indexes and constraints added `NOT VALID`; it never
breaks the previous release's code. A rename is an add, a backfill and a later drop, never
`RENAME`. Migrations never `DELETE FROM` or `TRUNCATE`: financial, trust, journal, gate
and audit rows are corrected with reversing rows. The check rejects `RENAME`, `TRUNCATE`
and `DELETE FROM` outside comments; lock and backfill limits (lock_timeout 2 s, batches of
at most 10000 rows) are checked by the migration job and its lint.

## Adding a module block

Append a line to `tools/migration_blocks.txt` in the module's layer range. Never move or
shrink a block once a migration uses it.

Non-migration files here (this README, `smoke.sql`) do not start with a digit and are not
numbered.

`migrations.go` embeds the numbered files: `migrations.Apply` runs them in file-name order,
for `counsel-migrate` and for integration tests that need the current schema.
