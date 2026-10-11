#!/usr/bin/env python3
"""AI fixture lint (spec/ai/mocks.md sec. 4, rules AI-R-002 and AI-R-004).

Run from the repo root: python3 -I tools/ai_fixture_lint.py [--root DIR] [--strict] [--self-test]

Checks:
- every fixture file (testdata/ai/**/*.yaml outside evals/ and inputs/) has `synthetic: true`;
- every fixture names a feature id in spec/ai/features.json and sits in that feature's dir;
- every required feature has a fixture dir with ok, error_429, timeout, injection
  (plus bad_output when model_call includes extraction).

A feature is required when its fixture dir exists or the task that owns that dir is ticked in
docs/TASKS.md; --strict requires every feature (the state once all fixture tasks land).
Exits 1 and prints one line per problem, naming the feature id.
"""
import json
import pathlib
import re
import sys
import tempfile

REQUIRED = ("ok", "error_429", "timeout", "injection")
EXTRACTION_EXTRA = ("bad_output",)
SKIP_DIRS = {"evals", "inputs"}
SYNTHETIC = re.compile(r"^synthetic:[ \t]*true[ \t]*(#.*)?$", re.M)
FEATURE = re.compile(r"^feature:[ \t]*[\"']?([A-Za-z0-9-]+)[\"']?[ \t]*(#.*)?$", re.M)
TICKED = re.compile(r"^\|\s*\[[xX]\]\s*\|\s*([A-Za-z0-9.-]+)\s*\|", re.M)


def _arg(name):
    if name in sys.argv:
        return sys.argv[sys.argv.index(name) + 1]
    return None


def ticked_tasks(root):
    p = root / "docs" / "TASKS.md"
    return set(TICKED.findall(p.read_text())) if p.exists() else set()


def owner_tasks(root):
    """Map fixture dir (no trailing slash) -> id of the task whose files list it."""
    p = root / "spec" / "tasks.json"
    if not p.exists():
        return {}
    out = {}
    for t in json.loads(p.read_text())["tasks"]:
        for f in t.get("files", []):
            if f.startswith("testdata/ai/"):
                out[f.rstrip("/")] = t["id"]
    return out


def fixture_files(root):
    base = root / "testdata" / "ai"
    if not base.exists():
        return []
    return sorted(p for p in base.rglob("*.yaml")
                  if not SKIP_DIRS.intersection(p.relative_to(base).parts[:-1]))


def lint(root, strict=False):
    errs = []
    feats = json.loads((root / "spec" / "ai" / "features.json").read_text())["features"]
    by_id = {f["id"]: f for f in feats}
    ticked = ticked_tasks(root)
    owners = owner_tasks(root)

    # rule: AI-R-002
    for f in feats:
        rel = f["fixture"].rstrip("/")
        d = root / rel
        task = owners.get(rel)
        if not (strict or d.is_dir() or (task and task in ticked)):
            continue
        if not d.is_dir():
            errs.append(f"{f['id']}: fixture dir {rel} missing")
            continue
        want = REQUIRED + (EXTRACTION_EXTRA if "extraction" in f.get("model_call", "") else ())
        for v in want:
            if not (d / f"{v}.yaml").is_file():
                errs.append(f"{f['id']}: {rel}/{v}.yaml missing")

    for p in fixture_files(root):
        rel = p.relative_to(root).as_posix()
        text = p.read_text()
        # rule: AI-R-004
        if not SYNTHETIC.search(text):
            errs.append(f"{rel}: missing top-level 'synthetic: true'")
        m = FEATURE.search(text)
        if not m:
            errs.append(f"{rel}: no feature id")
            continue
        fid = m.group(1)
        if fid not in by_id:
            errs.append(f"{rel}: feature {fid} is not in spec/ai/features.json")
        elif p.parent.relative_to(root).as_posix() != by_id[fid]["fixture"].rstrip("/"):
            errs.append(f"{fid}: {rel} is outside its fixture dir {by_id[fid]['fixture']}")
    return errs


def self_test():
    """Table-driven tests of lint() over throwaway repos."""
    feats = {"features": [
        {"id": "F-TST-01", "fixture": "testdata/ai/f-tst-01/day", "model_call": "chat"},
        {"id": "F-TST-02", "fixture": "testdata/ai/f-tst-02/pull", "model_call": "embedding+extraction"},
    ]}
    tasks = {"tasks": [
        {"id": "X-1", "files": ["testdata/ai/f-tst-01/day/"]},
        {"id": "X-2", "files": ["testdata/ai/f-tst-02/pull/"]},
    ]}
    ok = "synthetic: true\nfeature: F-TST-01\nfault: timeout\n"
    full1 = {f"testdata/ai/f-tst-01/day/{v}.yaml": ok for v in REQUIRED}
    cases = [
        # (name, files, ticked, strict, expected error substrings; [] = clean)
        ("empty tree, nothing ticked", {}, [], False, []),
        ("complete feature", full1, ["X-1"], False, []),
        ("AI-R-002 ticked task without dir names feature", {}, ["X-1"], False, ["F-TST-01: fixture dir"]),
        ("AI-R-002 strict requires every feature", full1, [], True, ["F-TST-02: fixture dir"]),
        ("AI-R-002 missing variant", {k: v for k, v in full1.items() if "timeout" not in k}, [], False,
         ["F-TST-01: testdata/ai/f-tst-01/day/timeout.yaml missing"]),
        ("AI-R-002 extraction needs bad_output",
         {f"testdata/ai/f-tst-02/pull/{v}.yaml": ok.replace("01", "02") for v in REQUIRED}, [], False,
         ["F-TST-02: testdata/ai/f-tst-02/pull/bad_output.yaml missing"]),
        ("AI-R-004 rejects missing synthetic",
         {**full1, "testdata/ai/f-tst-01/day/ok.yaml": "feature: F-TST-01\n"}, [], False,
         ["day/ok.yaml: missing top-level 'synthetic: true'"]),
        ("AI-R-004 rejects synthetic false",
         {**full1, "testdata/ai/f-tst-01/day/ok.yaml": "synthetic: false\nfeature: F-TST-01\n"}, [], False,
         ["missing top-level 'synthetic: true'"]),
        ("AI-R-004 nested synthetic does not count",
         {**full1, "testdata/ai/f-tst-01/day/ok.yaml": "feature: F-TST-01\nmatch:\n  synthetic: true\n"}, [], False,
         ["missing top-level 'synthetic: true'"]),
        ("unknown feature", {**full1, "testdata/ai/f-tst-01/day/extra.yaml": ok.replace("01", "09")}, [], False,
         ["feature F-TST-09 is not in"]),
        ("fixture outside its dir", {**full1, "testdata/ai/f-tst-01/other/ok.yaml": ok}, [], False,
         ["outside its fixture dir"]),
        ("evals and inputs skipped", {**full1, "testdata/ai/evals/x.yaml": "cases: []\n",
                                      "testdata/ai/f-tst-01/day/inputs/seed.yaml": "rows: []\n"}, [], False, []),
    ]
    failed = 0
    for name, files, ticked, strict, want in cases:
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "spec" / "ai").mkdir(parents=True)
            (root / "spec" / "ai" / "features.json").write_text(json.dumps(feats))
            (root / "spec" / "tasks.json").write_text(json.dumps(tasks))
            (root / "docs").mkdir()
            (root / "docs" / "TASKS.md").write_text(
                "".join(f"| [x] | {t} | {t} | kernel | t |\n" for t in ticked) + "| [ ] | X-9 | X-9 | k | t |\n")
            for rel, body in files.items():
                (root / rel).parent.mkdir(parents=True, exist_ok=True)
                (root / rel).write_text(body)
            errs = lint(root, strict)
            bad = [w for w in want if not any(w in e for e in errs)]
            if (not want and errs) or bad:
                failed += 1
                print(f"FAIL {name}: errors={errs} want={want}")
            else:
                print(f"ok   {name}")
    return 1 if failed else 0


def main():
    if "--self-test" in sys.argv:
        return self_test()
    root = pathlib.Path(_arg("--root") or ".")
    errs = lint(root, "--strict" in sys.argv)
    for e in errs:
        print(f"ai-fixture-lint: {e}")
    n = len(fixture_files(root))
    print(f"ai-fixture-lint: {n} fixture files, {len(errs)} problems")
    return 1 if errs else 0


if __name__ == "__main__":
    sys.exit(main())
