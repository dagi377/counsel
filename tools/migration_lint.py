#!/usr/bin/env python3
"""Migration lint: expand-contract (MIG-060, ARC-U-017) and lock-safe DDL (MIG-061).

Run from the repo root: python3 -I tools/migration_lint.py [--baseline FILE] FILE_OR_DIR...

A directory is linted file by file (numbered NNNN_*.sql, sorted). Prints one line per problem,
"<file>:<line>: <rule>: <message>", and exits 1 when there is any problem.

--baseline FILE names known problems, one problem line per line (lines starting with '#' are comments). A
listed problem is not reported; a listed problem the lint no longer finds is reported as stale,
so the baseline can only shrink. It exists for the frozen contract file 0001_init.sql, which
cannot be edited.

Expand migrations (the default) may only add. These are destructive and are allowed only in a
file marked with a full-line comment "-- contract: <release>" (the release that stopped using
the field):
- DROP TABLE / VIEW / SCHEMA, and ALTER TABLE ... DROP [COLUMN];
- RENAME of a table, column or constraint;
- ALTER COLUMN ... TYPE (the lint cannot see the old type, so every type change counts as a
  possible narrowing or rewrite);
- ALTER COLUMN ... SET NOT NULL on a column of an existing table;
- ADD COLUMN ... NOT NULL without a constant DEFAULT on an existing table.
A table created in the same file is not "existing".

Lock-safe DDL (MIG-061):
- every file sets lock_timeout, at most 2s and not 0 (MIG-061 allows 3s; ARC-U-018 and the
  migration Job use 2s, the strictest), and a non-zero statement_timeout;
- CREATE INDEX on an existing table is CONCURRENTLY, in its own non-transactional file: a file
  marked "-- migrate: no-transaction" holding only SET/RESET and concurrent index statements,
  with no BEGIN or COMMIT;
- a new FOREIGN KEY or CHECK constraint on an existing table is added NOT VALID, and its
  VALIDATE CONSTRAINT runs in a later file; UNIQUE and PRIMARY KEY attach a concurrently built
  index (USING INDEX); ADD COLUMN on an existing table carries no inline REFERENCES or CHECK.
"""
import pathlib
import re
import sys

CONTRACT_MARK = re.compile(r"^--[ \t]*contract:[ \t]*(\S+)[ \t]*$", re.M | re.I)
NO_TX_MARK = re.compile(r"^--[ \t]*migrate:[ \t]*no-transaction[ \t]*$", re.M | re.I)

# The strictest lock wait (ARC-U-018 2s; MIG-061 allows up to 3s).
MAX_LOCK_TIMEOUT_MS = 2000

IDENT = r'(?:"[^"]+"|[A-Za-z_][A-Za-z0-9_$]*)'
QNAME = IDENT + r"(?:\." + IDENT + r")?"

CREATE_TABLE = re.compile(
    r"^CREATE\s+(?:UNLOGGED\s+|TEMP(?:ORARY)?\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(" + QNAME + ")",
    re.I,
)
ALTER_TABLE = re.compile(
    r"^ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?(" + QNAME + r")\s+(.*)$", re.I | re.S
)
DROP_OBJECT = re.compile(r"^DROP\s+(TABLE|(?:MATERIALIZED\s+)?VIEW|SCHEMA)\b", re.I)
SET_TIMEOUT = re.compile(
    r"^SET\s+(?:LOCAL\s+|SESSION\s+)?(lock_timeout|statement_timeout)\s*(?:=|TO)\s*'?\s*(\d+)\s*([a-z]*)\s*'?$",
    re.I,
)
CREATE_INDEX = re.compile(
    r"^CREATE\s+(?:UNIQUE\s+)?INDEX\s+(CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?(?:" + IDENT + r"\s+)?ON\s+(?:ONLY\s+)?(" + QNAME + ")",
    re.I,
)
CONCURRENT_OK = re.compile(r"^(?:SET|RESET)\b|^(?:DROP\s+INDEX|REINDEX\s+\w+)\s+CONCURRENTLY\b", re.I)
TX_CONTROL = re.compile(r"^(?:BEGIN|START\s+TRANSACTION|COMMIT|END|ROLLBACK)\b", re.I)
VALIDATE = re.compile(r"\bVALIDATE\s+CONSTRAINT\s+(" + IDENT + ")", re.I)
UNITS_MS = {"": 1, "ms": 1, "s": 1000, "min": 60000, "h": 3600000}
RENAME_ANY = re.compile(r"^ALTER\s+(?:TABLE|(?:MATERIALIZED\s+)?VIEW|SCHEMA|INDEX|SEQUENCE)\b.*\bRENAME\b", re.I | re.S)

# One string literal (contents already masked), a number, or a boolean, with optional casts.
CONST_DEFAULT = re.compile(
    r"\bDEFAULT\s+(?:'[^']*'|-?\d+(?:\.\d+)?|TRUE|FALSE)(?:\s*::\s*[A-Za-z_][\w ]*(?:\(\s*\d+(?:\s*,\s*\d+)?\s*\))?(?:\[\])?)*\s*(?:$|NOT\b|NULL\b|CHECK\b|REFERENCES\b|CONSTRAINT\b|UNIQUE\b|PRIMARY\b|COLLATE\b)",
    re.I,
)


class Stmt:
    """One SQL statement with comments removed and string bodies masked."""

    def __init__(self, text, raw, line):
        self.text = " ".join(text.split())
        self.raw = " ".join(raw.split())
        self.line = line


def split_statements(src):
    """Split SQL into statements. Comments are dropped; quoted and dollar-quoted bodies are
    masked so that keywords inside them never match."""
    out, buf, raw = [], [], []
    line, start = 1, None
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == "\n":
            line += 1
        if src.startswith("--", i):
            j = src.find("\n", i)
            i = n if j < 0 else j
            continue
        if src.startswith("/*", i):
            j = src.find("*/", i + 2)
            j = n if j < 0 else j + 2
            line += src.count("\n", i, j)
            buf.append(" ")
            raw.append(" ")
            i = j
            continue
        if start is None and not c.isspace() and c != ";":
            start = line
        if c == "'":
            j = i + 1
            while j < n:
                if src[j] == "'" and j + 1 < n and src[j + 1] == "'":
                    j += 2
                    continue
                if src[j] == "'":
                    break
                j += 1
            line += src.count("\n", i, j)
            buf.append("'S'")
            raw.append(src[i : j + 1])
            i = j + 1
            continue
        m = re.match(r"\$([A-Za-z_][A-Za-z0-9_]*)?\$", src[i:])
        if m:
            tag = m.group(0)
            j = src.find(tag, i + len(tag))
            j = n if j < 0 else j + len(tag)
            line += src.count("\n", i, j)
            buf.append("$S$")
            raw.append("$S$")
            i = j
            continue
        if c == ";":
            text = "".join(buf).strip()
            if text:
                out.append(Stmt(text, "".join(raw), start))
            buf, raw, start = [], [], None
            i += 1
            continue
        buf.append(c)
        raw.append(c)
        i += 1
    text = "".join(buf).strip()
    if text:
        out.append(Stmt(text, "".join(raw), start))
    return out


def norm(name):
    return name.replace('"', "").lower()


def split_actions(rest):
    """Split the action list of ALTER TABLE on top-level commas."""
    parts, depth, cur = [], 0, []
    for ch in rest:
        if ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
        if ch == "," and depth == 0:
            parts.append("".join(cur).strip())
            cur = []
            continue
        cur.append(ch)
    parts.append("".join(cur).strip())
    return [p for p in parts if p]


class Linter:
    def __init__(self, path, src):
        self.path = path
        self.src = src
        self.stmts = split_statements(src)
        self.contract = CONTRACT_MARK.search(src)
        self.created = {norm(m.group(1)) for s in self.stmts for m in [CREATE_TABLE.match(s.text)] if m}
        self.problems = []

    def report(self, stmt, rule, msg):
        self.problems.append(f"{self.path}:{stmt.line}: {rule}: {msg}")

    def destructive(self, stmt, what):
        # rule: MIG-060
        if not self.contract:
            self.report(stmt, "MIG-060", f"{what} is destructive; only allowed in a file marked '-- contract: <release>'")

    def lint(self):
        for s in self.stmts:
            self.lint_expand_contract(s)
        self.lint_lock_safe()
        return self.problems

    def file_problem(self, rule, msg):
        self.problems.append(f"{self.path}:1: {rule}: {msg}")

    # rule: MIG-061
    def lint_lock_safe(self):
        timeouts = {}
        concurrent, not_valid = [], set()
        for s in self.stmts:
            m = SET_TIMEOUT.match(s.raw)
            if m:
                unit = m.group(3).lower()
                if unit not in UNITS_MS:
                    self.report(s, "MIG-061", f"{m.group(1)}: unknown unit {unit!r}")
                    continue
                timeouts[m.group(1).lower()] = (int(m.group(2)) * UNITS_MS[unit], s)
                continue
            m = CREATE_INDEX.match(s.text)
            if m:
                if m.group(1):
                    concurrent.append(s)
                elif norm(m.group(2)) not in self.created:
                    self.report(s, "MIG-061", f"CREATE INDEX on existing table {norm(m.group(2))} without CONCURRENTLY")
                continue
            if CONCURRENT_OK.match(s.text) and not re.match(r"^(?:SET|RESET)\b", s.text, re.I):
                concurrent.append(s)
                continue
            m = ALTER_TABLE.match(s.text)
            if m:
                self.lint_constraints(s, norm(m.group(1)), m.group(2), not_valid)
        self.lint_timeouts(timeouts)
        if concurrent:
            self.lint_no_transaction()

    def lint_timeouts(self, timeouts):
        lock = timeouts.get("lock_timeout")
        if lock is None:
            self.file_problem("MIG-061", "missing SET lock_timeout")
        elif lock[0] <= 0 or lock[0] > MAX_LOCK_TIMEOUT_MS:
            self.report(lock[1], "MIG-061", f"lock_timeout must be > 0 and <= {MAX_LOCK_TIMEOUT_MS}ms, got {lock[0]}ms")
        stmt = timeouts.get("statement_timeout")
        if stmt is None:
            self.file_problem("MIG-061", "missing SET statement_timeout")
        elif stmt[0] <= 0:
            self.report(stmt[1], "MIG-061", "statement_timeout must be > 0")

    def lint_no_transaction(self):
        if not NO_TX_MARK.search(self.src):
            self.file_problem("MIG-061", "concurrent index needs its own file marked '-- migrate: no-transaction'")
        for s in self.stmts:
            if TX_CONTROL.match(s.text):
                self.report(s, "MIG-061", "concurrent index file must not open a transaction (BEGIN/COMMIT)")
            elif not (CREATE_INDEX.match(s.text) or CONCURRENT_OK.match(s.text)):
                self.report(s, "MIG-061", "concurrent index must be in its own file; move this statement out")

    def lint_constraints(self, s, table, rest, not_valid):
        existing = table not in self.created
        for act in split_actions(rest):
            up = act.upper()
            v = VALIDATE.search(act)
            if v and re.match(r"VALIDATE\b", up):
                if norm(v.group(1)) in not_valid:
                    self.report(s, "MIG-061", f"VALIDATE CONSTRAINT {norm(v.group(1))} must run in a later migration than its NOT VALID add")
                continue
            if not existing:
                continue
            c = re.match(r"ADD\s+(?:CONSTRAINT\s+(" + IDENT + r")\s+)?(FOREIGN\s+KEY|CHECK|UNIQUE|PRIMARY\s+KEY|EXCLUDE)\b", act, re.I)
            if c:
                kind = " ".join(c.group(2).upper().split())
                if kind in ("FOREIGN KEY", "CHECK"):
                    if re.search(r"\bNOT\s+VALID\s*$", up):
                        if c.group(1):
                            not_valid.add(norm(c.group(1)))
                    else:
                        self.report(s, "MIG-061", f"ADD {kind} on existing table {table} must be NOT VALID, then VALIDATE CONSTRAINT separately")
                elif not re.search(r"\bUSING\s+INDEX\b", up):
                    self.report(s, "MIG-061", f"ADD {kind} on existing table {table} must use an index built CONCURRENTLY (USING INDEX)")
                continue
            if re.match(r"ADD\b(?!\s+(?:CONSTRAINT|PRIMARY|UNIQUE|CHECK|FOREIGN|EXCLUDE)\b)", up):
                if re.search(r"\b(?:REFERENCES|CHECK)\b", up):
                    self.report(s, "MIG-061", f"ADD COLUMN on existing table {table} with inline REFERENCES/CHECK; add the constraint NOT VALID separately")

    # rule: MIG-060
    def lint_expand_contract(self, s):
        m = DROP_OBJECT.match(s.text)
        if m:
            self.destructive(s, "DROP " + " ".join(m.group(1).upper().split()))
            return
        if RENAME_ANY.match(s.text):
            self.destructive(s, "RENAME")
            return
        m = ALTER_TABLE.match(s.text)
        if not m:
            return
        table, existing = norm(m.group(1)), norm(m.group(1)) not in self.created
        for act in split_actions(m.group(2)):
            up = act.upper()
            if re.match(r"DROP\b(?!\s+(?:CONSTRAINT|DEFAULT|NOT\s+NULL|IDENTITY|EXPRESSION)\b)", up):
                self.destructive(s, f"DROP COLUMN on {table}")
            elif re.match(r"ALTER\s+(?:COLUMN\s+)?" + IDENT + r"\s+(?:SET\s+DATA\s+)?TYPE\b", act, re.I):
                self.destructive(s, f"ALTER COLUMN TYPE on {table} (may narrow or rewrite)")
            elif re.match(r"ALTER\s+(?:COLUMN\s+)?" + IDENT + r"\s+SET\s+NOT\s+NULL\b", act, re.I) and existing:
                self.destructive(s, f"SET NOT NULL on an existing column of {table}")
            elif re.match(r"ADD\b(?!\s+(?:CONSTRAINT|PRIMARY|UNIQUE|CHECK|FOREIGN|EXCLUDE)\b)", up) and existing:
                if re.search(r"\bNOT\s+NULL\b", up) and not CONST_DEFAULT.search(act):
                    self.destructive(s, f"ADD COLUMN NOT NULL without a constant DEFAULT on {table}")


NUMBERED = re.compile(r"^\d{4}_[a-z0-9_]+\.sql$")


def sql_files(args):
    for a in args:
        p = pathlib.Path(a)
        if p.is_dir():
            yield from sorted(f for f in p.glob("*.sql") if NUMBERED.match(f.name))
        else:
            yield p


def read_baseline(path):
    out = []
    for line in pathlib.Path(path).read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if line and not line.startswith("#"):
            out.append(line)
    return out


def main(argv):
    baseline = None
    if argv[:1] == ["--baseline"]:
        if len(argv) < 2:
            print("usage: migration_lint.py [--baseline FILE] FILE_OR_DIR...", file=sys.stderr)
            return 2
        baseline, argv = argv[1], argv[2:]
    if not argv:
        print("usage: migration_lint.py [--baseline FILE] FILE_OR_DIR...", file=sys.stderr)
        return 2
    problems = []
    for f in sql_files(argv):
        try:
            src = f.read_text(encoding="utf-8")
        except OSError as e:
            problems.append(f"{f}:0: read: {e}")
            continue
        problems.extend(Linter(str(f), src).lint())
    if baseline is not None:
        known = read_baseline(baseline)
        stale = [k for k in known if k not in problems]
        problems = [p for p in problems if p not in known]
        problems.extend(f"{baseline}:0: baseline: stale entry, the lint no longer reports {k!r}" for k in stale)
    for p in problems:
        print(p)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
