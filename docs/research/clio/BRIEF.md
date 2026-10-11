# Clio deep research brief (accuracy first)

Goal: an almost fully accurate picture of what Clio offers in YOUR scope, with sources and screenshot links,
compared with our spec, so we can close every gap. Read-only research; no code, no git.

Our spec (compare against it): ../legal-spec/*.json (areas: intake, conflicts, documents, matters, deadlines,
work, time, billing, trust, platform, readiness, access). Our claims page: ../versus-clio/index.html.

## Accuracy rules
- Every feature needs at least one source URL you actually opened or saw in a search result. Prefer, in order:
  help.clio.com articles, clio.com product pages, docs.developers.clio.com (API docs reveal real fields), Clio
  release notes / "What's new", Clio YouTube videos, G2 / Capterra / Software Advice reviews, law-society pages.
- help.clio.com often returns 403 to direct fetch. Try: the Wayback Machine (https://web.archive.org/web/2026/<url>
  or /web/2025/<url>), search-result snippets, the API docs, and clio.com pages. Say which you used.
- Confidence per feature: high (official page opened), medium (official snippet or several secondary sources),
  low (one secondary source or memory). Never state low-confidence facts as certain.
- Own words only. Do not copy Clio text or reproduce their UI; screenshots are collected as LINKS only.
- Hard limit: 20 minutes. Write what you have before then.

## Output: ONE file `<your-id>.json` in this folder, exactly:
{
  "id": "<your-id>", "scope": "...", "clio_products": ["Clio Manage", "..."],
  "features": [{"name": "...", "product": "...", "where": "menu path in Clio", "what": "1-2 lines, own words",
    "plan_tier": "which plans, if known", "sources": ["url"], "screenshots": ["url of an image/video/page showing it"],
    "confidence": "high|medium|low"}],
  "gaps": [{"feature": "...", "our_area": "area id", "our_screen": "screen id or null", "status": "missing|partial|covered|deliberately different",
    "proposal": "exactly what to add to our spec (screen/field/action/rule), AI-native where it helps", "priority": "must|should|could"}],
  "corrections": [{"our_claim": "a statement in our spec or versus page that is wrong or unproven", "truth": "...", "sources": ["url"]}],
  "screenshots_index": [{"area": "...", "url": "...", "shows": "..."}],
  "open_questions": ["..."], "sources": ["all urls"]
}
Validate with: python3 -I -c 'import json;json.load(open("<file>"))'. Return a 5-line summary with counts.
