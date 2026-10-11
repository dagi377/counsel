# Brief: AI-native legal practice app (spec research)

Product: a separate SaaS app ("the app"), built on the Paploo agent platform, for small Canadian
(first: Ontario) law firms, tens of firms, one isolated cell per firm, data in Canada.
Covers intake to matter close. Principle: the agent does the work, a human approves at each gate.
Reference: what Clio does (functions only). Never copy Clio wording, UI or branding: design our own.
Background inventory (read first): ../clio-inventory.md

Your job: spec every screen in YOUR area. Research public sources (Clio help center, competitors such
as PracticePanther, MyCase, LEAP, CosmoLex, Smokeball, Jira for the work module, LSO by-laws and
Ontario rules from lso.ca / ontario.ca). Own words only. Mark any regulatory fact not confirmed in an
official source with "(verify)".

Output: write ONE JSON file to this folder, named `<area-id>.json`, exactly this shape:

{
  "area": "<area-id>", "title": "...", "summary": "2-3 lines",
  "screens": [{
    "id": "kebab-id", "name": "...", "route": "/matters/:id",
    "who": ["lawyer","clerk","paralegal","client","admin","agent"],
    "purpose": "1 line",
    "layout": "1-3 lines: regions of the screen",
    "fields": [{"name":"...","type":"text|date|money|enum|ref|file|...","required":true,"note":"..."}],
    "actions": [{"name":"...","by":"human|agent","gate":"none|approve|sign-off","note":"..."}],
    "agent": "what the agent does on this screen automatically",
    "gate": "what exactly a human approves here, or 'none'",
    "states": ["empty","draft","pending approval","..."],
    "edge": ["edge cases / failure modes"],
    "reg": "regulatory notes or ''",
    "mock": {"kind":"table|board|form|detail|calendar|inbox|ledger|dashboard|timeline|doc",
             "columns":["for tables/boards"], "rows":[["fictional sample data rows, 3-5"]]}
  }],
  "entities": [{"name":"Matter","fields":["id","..."],"relations":["Contact many-to-many via Role"]}],
  "gates": [{"id":"G?","name":"...","approver":"role","blocks":"what cannot proceed until approved"}],
  "agent_jobs": [{"name":"...","trigger":"event|schedule","does":"...","output":"draft X for gate Y"}],
  "open_questions": ["for the owner"],
  "sources": ["urls"]
}

Sample data: fictional only (firm "Linda K Law", people Linda Kassim, Ayo Ewebiyi, Yonathan Degu,
Meraf Abay, Kaleab Bereket, Yohannes Sinke; invent client names). No real client data.
Be complete but terse: aim for 5-12 screens. Hard limit 20 minutes; if long, write what you have.
Do not write anywhere else. Do not touch the git repo. Return a 5-line summary.
