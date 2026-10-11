"""Rule coverage: every rule id defined in spec/rules/*.json is claimed by some task.

Run from the repo root: python3 -I tools/rule_coverage.py [--root DIR]
A rule id is any "id" value shaped like ABC-X-123 in spec/rules/*.json. A task claims it by
listing it in rule_ids in spec/tasks.json (docs/TASKS.md is generated from that file).
Exits 1 and prints each unmapped id. No spec/rules files yet means nothing to check.
"""
import json
import pathlib
import re
import sys

RULE_ID = re.compile(r"^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*-\d+[a-z]?$")


def _root():
    if "--root" in sys.argv:
        return pathlib.Path(sys.argv[sys.argv.index("--root") + 1])
    return pathlib.Path(".")


def rule_ids(node, out):
    if isinstance(node, dict):
        v = node.get("id")
        if isinstance(v, str) and RULE_ID.match(v):
            out.add(v)
        for child in node.values():
            rule_ids(child, out)
    elif isinstance(node, list):
        for child in node:
            rule_ids(child, out)
    return out


def main():
    root = _root()
    defined = set()
    files = sorted((root / "spec" / "rules").glob("*.json"))
    for f in files:
        rule_ids(json.loads(f.read_text()), defined)
    plan = json.loads((root / "spec" / "tasks.json").read_text())
    claimed = {rid for t in plan["tasks"] for rid in t.get("rule_ids", [])}
    missing = sorted(defined - claimed)
    for rid in missing:
        print(f"rule-coverage: {rid} is in spec/rules but in no task's rule_ids")
    print(f"rule-coverage: {len(defined)} rule ids in {len(files)} files, {len(missing)} unmapped")
    return 1 if missing else 0


if __name__ == "__main__":
    sys.exit(main())
