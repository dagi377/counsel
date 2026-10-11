#!/usr/bin/env python3
"""Migration lint: expand-contract schema changes (MIG-060, ARC-U-017).

Run from the repo root: python3 -I tools/migration_lint.py FILE_OR_DIR...

A directory is linted file by file (*.sql, sorted). Prints one line per problem,
"<file>:<line>: <rule>: <message>", and exits 1 when there is any problem.

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
"""
import pathlib
import re
import sys

CONTRACT_MARK = re.compile(r"^--[ \t]*contract:[ \t]*(\S+)[ \t]*$", re.M | re.I)

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
RENAME_ANY = re.compile(r"^ALTER\s+(?:TABLE|(?:MATERIALIZED\s+)?VIEW|SCHEMA|INDEX|SEQUENCE)\b.*\bRENAME\b", re.I | re.S)

# One string literal (contents already masked), a number, or a boolean, with optional casts.
CONST_DEFAULT = re.compile(
    r"\bDEFAULT\s+(?:'[^']*'|-?\d+(?:\.\d+)?|TRUE|FALSE)(?:\s*::\s*[A-Za-z_][\w ]*(?:\(\s*\d+(?:\s*,\s*\d+)?\s*\))?(?:\[\])?)*\s*(?:$|NOT\b|NULL\b|CHECK\b|REFERENCES\b|CONSTRAINT\b|UNIQUE\b|PRIMARY\b|COLLATE\b)",
    re.I,
)


class Stmt:
    """One SQL statement with comments removed and string bodies masked."""

    def __init__(self, text, line):
        self.text = " ".join(text.split())
        self.line = line


def split_statements(src):
    """Split SQL into statements. Comments are dropped; quoted and dollar-quoted bodies are
    masked so that keywords inside them never match."""
    out, buf = [], []
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
            i = j + 1
            continue
        m = re.match(r"\$([A-Za-z_][A-Za-z0-9_]*)?\$", src[i:])
        if m:
            tag = m.group(0)
            j = src.find(tag, i + len(tag))
            j = n if j < 0 else j + len(tag)
            line += src.count("\n", i, j)
            buf.append("$S$")
            i = j
            continue
        if c == ";":
            text = "".join(buf).strip()
            if text:
                out.append(Stmt(text, start))
            buf, start = [], None
            i += 1
            continue
        buf.append(c)
        i += 1
    text = "".join(buf).strip()
    if text:
        out.append(Stmt(text, start))
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
        return self.problems

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


def sql_files(args):
    for a in args:
        p = pathlib.Path(a)
        if p.is_dir():
            yield from sorted(p.glob("*.sql"))
        else:
            yield p


def main(argv):
    if not argv:
        print("usage: migration_lint.py FILE_OR_DIR...", file=sys.stderr)
        return 2
    problems = []
    for f in sql_files(argv):
        try:
            src = f.read_text(encoding="utf-8")
        except OSError as e:
            problems.append(f"{f}:0: read: {e}")
            continue
        problems.extend(Linter(str(f), src).lint())
    for p in problems:
        print(p)
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
