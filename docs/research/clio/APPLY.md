# Phase 2: apply the Clio research

Research files: this folder, r01..r14 *.json (features, gaps, corrections, sources). Spec: ../legal-spec/*.json
(BRIEF shape in ../legal-spec/BRIEF.md). Prototype: ../counsel-app (read its README.md).

## Spec updaters
- Edit ONLY the files you own. Keep the BRIEF shape, keep existing content unless a correction says it is wrong.
- Apply every gap with priority must and should that maps to your files: new screens (unique kebab ids), fields,
  actions, edge cases, gates, entities, agent jobs. "could" items go into open_questions as one line each.
- Apply every correction that touches your files: fix wrong menu paths, claims, statuses in clio_gaps, and remove
  "(verify)" only when a research file gives a high-confidence source.
- Update each file's clio_gaps list: add rows for every Clio feature in your research files that maps to your area
  (status added | already covered | deliberately different), with the research source in note.
- New screens need access rules: write `../legal-spec/access-map-<your-id>.json` mapping "<area>/<screen id>" to
  {"view":[...],"act":[...],"approve":[...]} using role ids owner, admin, lawyer, associate, paralegal, clerk,
  bookkeeper, auditor, client, agent. The agent and admin never approve; admin never sees matter content or money.
- Validate every file with python3 -I -c 'import json;json.load(open(...))' and run
  `python3 -I ../legal-spec/build.py` (must print missing=[]).

## Rules for everyone
- Own words only; never copy Clio text or UI. Fictional data only.
- Accuracy: do not upgrade a claim beyond what the research sources support.
- Hard limit 20 minutes. No git. Do not edit files you do not own.
- Return a 6-line summary: counts of screens/fields added, corrections applied, anything you could not apply.
