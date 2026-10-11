# Engineering spec brief (design only, no code)

Product: "Counsel", an AI-native legal practice app for small Ontario firms, separate repo, running on the Paploo
agent runtime (see Paploo ADR 0014 proposal at
/Users/dagmfekaduyenealem/Documents/ai/paploo/.claude/worktrees/agent-a3a554c8ad07d8ef4/docs/adr/0014-custom-apps.md).
One isolated cell per firm (namespace + Postgres + keys), Canada region. Tens of firms.
Product spec (screens, fields, gates, entities): ../legal-spec/*.json (12 areas, BRIEF shape) and access matrix in
../legal-spec/access.json. Clio research: ../clio-research/r*.json.

Goal of this phase: make the spec correct and implementable so coding agents cannot make costly mistakes.

## Output rules
- Write into THIS folder only, files named `<your-id>.*`.
- Human-readable part: `<your-id>.html` — an HTML FRAGMENT (no <html>/<head>/<style>), using only h2, h3, p, ul/ol/li,
  table (wrap tables in <div class="scroll"><table class="ftable">…</table></div>), pre/code, b, i, a. Start with one
  <h2>. Be precise and terse; no filler.
- Machine-readable part where your brief says so (json/sql/yaml), valid and parseable.
- Every legal or tax rule: cite the official source (ontario.ca/laws e-Laws, lso.ca, canada.ca/CRA) with section number.
  If you could not read the official text, mark the rule "(verify)" and say what to check. Never invent section numbers.
- Every rule that code must enforce gets an id (e.g. BIL-R-012) and at least one concrete test case with numbers.
- Money: integer cents, CAD; never floats. Dates: ISO; time zone America/Toronto.
- Hard limit 20 minutes. Write what you have before then.
- Return a 6-line summary with counts.
