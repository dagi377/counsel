# Fix phase (spec only — nothing is implemented)

Findings: r-review-1.json (money & law, RV1-*) and r-review-2.json (architecture, RV2-*), plus the conflict register
in o1-tasks.json (CF-*). Each finding lists the files that must change and an exact fix.

Rules:
- Fix ONLY the files you own (your prompt lists them). If a finding spans files, apply your part and say in your log
  which other owner applies the rest (by file name). Never edit another owner's file.
- Several outputs are GENERATED. Edit the generator source, then regenerate; never hand-edit generated output only:
  b1 → ../gen-b1/ (rules.py, build.py); b2 → ../b2work/gen.py; b3 → ../b3work/ (engine.py, examples.py, rules.py, gen.py);
  a2 → ../a2work/gen.py (openapi yaml may be hand-edited if not generated — check); a3 → ../a3gen/ (data.py, gen.py);
  o1 → ../o1/ (tasks.py, html.py). a4 SQL/JSON/HTML: edit directly; the JSON must still match the DDL.
- Keep rule ids stable. New rules get new ids. If you reject a finding, say why with evidence.
- Legal/tax points the reviewer marked (verify) stay (verify) unless you read the official text.
- Keep money in integer cents, rates in ONE unit across all docs: parts per million (ppm) — this resolves CF-01.
- After changes: validate JSON/YAML, re-run generators, re-run any reference calculations, and (a4) reload the SQL
  into a throwaway local Postgres (Homebrew binaries, data dir under /tmp, never Docker, never existing DBs).
- Write `f-<your-id>.json`: {"owner":"<your-id>","items":[{"finding":"RV1-002","status":"fixed|partial|rejected|n/a","change":"what you changed","left_for":"other owner file if any"}]}.
- Hard limit 20 minutes. Return a 6-line summary.
