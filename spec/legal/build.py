"""Inline the area JSON files and meta.html into template.html -> counsel-spec.html."""
import json, pathlib, re, sys

D = pathlib.Path(__file__).parent
ORDER = ["intake", "conflicts", "documents", "matters", "deadlines", "work", "time", "billing", "trust", "platform", "readiness", "access"]

areas, missing = [], []
for a in ORDER:
    p = D / f"{a}.json"
    if not p.exists():
        missing.append(a)
        continue
    try:
        areas.append(json.loads(p.read_text()))
    except json.JSONDecodeError as e:
        sys.exit(f"{p.name}: {e}")

meta_src = (D / "meta.html").read_text()
parts = dict(re.findall(r"<!--(\w+)-->\n(.*?)(?=\n<!--\w+-->|\Z)", meta_src, re.S))
amap = D / "access-map.json"
amap_all = json.loads(amap.read_text()) if amap.exists() else {}
for extra in sorted(D.glob("access-map-*.json")):
    amap_all.update(json.loads(extra.read_text()))
research = []
for rp in sorted((D.parent / "clio-research").glob("r*.json")):
    try:
        research.append(json.loads(rp.read_text()))
    except json.JSONDecodeError as e:
        print(f"skip {rp.name}: {e}")
ENG = [("k1-kernel","App kernel"),("b1-ontario-billing-law","Billing: Ontario law"),("b2-sales-tax","Billing: sales tax"),("b3-billing-engine","Billing: engine & ledger"),
       ("a1-uptime","Uptime & reliability"),("a2-interfaces","Interfaces & contracts"),("a3-migration","Migration"),
       ("a4-data-model","Data model"),("o1-build-guide","Build guide & tasks"),("o2-spec-lint","Spec consistency"),("r-review-1","Review: money & law"),("r-review-2","Review: architecture"),("f-fixlog","Fix log")]
eng = []
for eid, title in ENG:
    fp = D.parent / "eng-spec" / f"{eid}.html"
    if fp.exists():
        eng.append({"id": eid, "title": title, "html": fp.read_text()})
spec = {"areas": areas, "meta": parts, "accessMap": amap_all, "research": research, "eng": eng}
data = json.dumps(spec, ensure_ascii=False).replace("</", "<\\/")
out = (D / "template.html").read_text().replace("/*DATA*/null", data)
(D / "counsel-spec.html").write_text(out)
n = sum(len(a.get("screens", [])) for a in areas)
print(f"areas={len(areas)} screens={n} missing={missing} bytes={len(out)}")
