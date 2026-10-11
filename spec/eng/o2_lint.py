"""Lint the Counsel product spec. Run: python3 -I o2_lint.py [--spec DIR] [--json out.json] [--fix]
       [--strict [--baseline FILE]] [--write-baseline [FILE]]

Spec dir: --spec DIR, else $COUNSEL_SPEC_DIR, else ../spec (Counsel repo layout), else ../legal-spec.
--strict exits 1 when any mechanical finding exists or any judgment check count exceeds the checked-in
baseline (default: o2_lint.baseline.json next to this file; missing baseline = every count must be 0).
--write-baseline records the current judgment counts as the baseline. Without --strict the exit code is 0.

Checks area JSON files, access-map*.json and access.json. --fix applies only mechanical, safe edits
(field types for money/date fields, agent/admin removed from approve lists, missing gate stubs that
are referenced but defined nowhere). It never deletes content and never touches access.json matrix grants.
"""
import json, pathlib, re, sys, collections

HERE = pathlib.Path(__file__).resolve().parent
import os


def _arg(flag, default=None):
    if flag in sys.argv:
        i = sys.argv.index(flag)
        if i + 1 < len(sys.argv) and not sys.argv[i + 1].startswith("--"):
            return sys.argv[i + 1]
    return default


def _spec_dir():
    if _arg("--spec"):
        return pathlib.Path(_arg("--spec"))
    if os.environ.get("COUNSEL_SPEC_DIR"):
        return pathlib.Path(os.environ["COUNSEL_SPEC_DIR"])
    for cand in (HERE.parent / "spec", HERE.parent / "legal-spec"):
        if (cand / "billing.json").exists():
            return cand
    return HERE.parent / "legal-spec"


SPEC = _spec_dir()
AREAS = ["intake", "conflicts", "documents", "matters", "deadlines", "work", "time", "billing",
         "trust", "platform", "readiness", "access"]
REQUIRED_SCREEN = ["id", "name", "route", "who", "purpose", "layout", "fields", "actions", "gate", "states", "mock"]
REQUIRED_AREA = ["screens", "entities", "gates", "summary"]

GATE_RE = re.compile(r"\b(G(?:-[A-Z]{1,6}\d*|A\d+|\d+[a-z]?))\b")
EXTERNAL = re.compile(r"\b(send|sends|sent|file with|e-?file|pay|pays|payment|wire|eft|"
                      r"withdraw|disburse|delete|deletes|destroy|purge|submit|publish|release|share|transfer)\b", re.I)
MONEY_NAME = re.compile(r"(^|_| )(amount|amt|balance|total|subtotal|fee|fees|price|cost|retainer_amount|"
                        r"disbursement|hst|tax_amount|paid|owing|outstanding|due_amount|deposit|payout|refund_amount|"
                        r"write_?off|writedown|write_down|limit_amount|value_cad|cents)($|_| )", re.I)
MONEY_NOT = re.compile(r"(type|kind|status|method|mode|policy|basis|terms|schedule|rule|reason|count|ids?|"
                       r"currency|percent|pct|rate_table|rate_type|plan|frequency|account|ref|note|split|code|"
                       r"number|scope|structure|arrangement|tier|band|_at$|date|day|days|flag|enabled|allowed|by$)", re.I)
MONEY_OK_TYPES = {"money", "money[]", "list<money>"}
DATE_NAME = re.compile(r"(^|_| )(date|dob|birth_date|due|due_on|deadline|expires|expiry|expires_on|"
                       r"[a-z]+_on|[a-z]+_date|date_[a-z]+|[a-z]+_at)$", re.I)
# Mechanical gate-id fixes. G10 is cited as "bill approval in billing area"; billing defines it as G-B2.
GATE_ALIASES = {"G10": "G-B2"}
# Area-local gate ids that collide with access.json ids of a different meaning; renamed inside the owning file.
GATE_RENAMES = {"trust": {"GA1": "G-TSIG", "GA2": "G-TREL"}}
DATE_TYPES = {"date", "datetime", "date[]", "list<datetime>", "daterange", "date range", "time"}


def load(name):
    return json.loads((SPEC / name).read_text())


def ent_tokens(rel):
    """CamelCase identifiers in a relation string (entity references)."""
    return [t for t in re.findall(r"\b([A-Z][a-z]+(?:[A-Z][a-z]*)+|[A-Z][a-z]{2,})\b", rel)]


# Words in relation text that are roles, gates or prose, not entity names.
REL_STOP = {"Many", "One", "Both", "Optional", "Firm", "Client", "Email", "Call", "Bill", "Lead", "User",
            "Lawyer", "Gate", "Intake", "File", "Party", "Workload"}
# Relation names that refer to an entity another area already defines under a different name.
ENTITY_ALIASES = {"MatterMember": "TeamMember", "TrustTransaction": "TrustJournalLine"}


def lint(fix=False):
    F = []  # findings: dict(check, area, item, msg, kind) kind=mech|judg

    def add(check, area, item, msg, kind="judg"):
        F.append({"check": check, "area": area, "item": item, "msg": msg, "kind": kind})

    access = load("access.json")
    caps = {c["id"] for c in access["matrix"]["capabilities"]}
    cap_pfx = {c.split(".")[0] for c in caps}
    roles = {r["id"] for r in access["roles"]}
    amap, amap_src = {}, {}
    for p in [SPEC / "access-map.json"] + sorted(SPEC.glob("access-map-*.json")):
        for k, v in json.loads(p.read_text()).items():
            amap[k] = v
            amap_src[k] = p.name

    if fix:
        for a in AREAS:
            p = SPEC / f"{a}.json"
            if not p.exists():
                continue
            t0 = p.read_text()
            t = t0
            for old, new in GATE_RENAMES.get(a, {}).items():
                t = re.sub(r"\b" + re.escape(old) + r"\b", new, t)
            for old, new in GATE_ALIASES.items():
                t = re.sub(r"\b" + re.escape(old) + r"\b", new, t)
            if t != t0:
                p.write_text(t)
    data = {}
    for a in AREAS:
        p = SPEC / f"{a}.json"
        if not p.exists():
            add("area-missing", a, a, "area file missing")
            continue
        data[a] = json.loads(p.read_text())
    dirty = set()

    # gates defined anywhere
    gate_def = collections.defaultdict(list)  # id -> [(area, gate)]
    for a, d in data.items():
        for g in d.get("gates", []):
            gate_def[g["id"]].append((a, g))
    for gid, defs in gate_def.items():
        if len(defs) > 1:
            names = {re.sub(r"\(.*?\)", "", g["name"]).strip().lower() for _, g in defs}
            add("gate-dup", ",".join(a for a, _ in defs), gid,
                f"{gid} defined in {len(defs)} areas: " + " | ".join(f"{a}: {g['name']}" for a, g in defs),
                "judg" if len(names) > 1 else "info")

    # entities
    ent_def = collections.defaultdict(list)
    for a, d in data.items():
        for e in d.get("entities", []):
            ent_def[e["name"]].append((a, e))
    all_ents = set(ent_def)
    for n, defs in ent_def.items():
        if len(defs) > 1:
            fsets = [frozenset(e.get("fields", [])) for _, e in defs]
            if len(set(fsets)) > 1:
                core = set.intersection(*map(set, fsets))
                add("entity-conflict", ",".join(a for a, _ in defs), n,
                    f"{n} defined in {[a for a, _ in defs]} with different field lists (shared: {len(core)}; "
                    + "; ".join(f"{a}: {len(e.get('fields', []))} fields" for a, e in defs) + ")")
    for a, d in data.items():
        for e in d.get("entities", []):
            for rel in e.get("relations", []):
                for t in ent_tokens(rel):
                    if t not in all_ents and t not in REL_STOP:
                        add("entity-ref", a, f"{e['name']} -> {t}", f"relation '{rel}' names undefined entity {t}")

    for a, d in data.items():
        for k in REQUIRED_AREA:
            if not d.get(k):
                add("empty-section", a, k, f"area section '{k}' empty")
        ids = collections.Counter(s.get("id") for s in d.get("screens", []))
        for i, n in ids.items():
            if n > 1:
                add("screen-dup", a, i, f"screen id {i} used {n} times")
        area_text = json.dumps(d)
        add("verify-count", a, "", str(area_text.count("(verify)")), "info")
        for s in d.get("screens", []):
            sid = f"{a}/{s.get('id')}"
            for k in REQUIRED_SCREEN:
                v = s.get(k)
                if v in (None, "", [], {}):
                    add("empty-section", a, sid, f"screen field '{k}' empty")
            m = amap.get(sid)
            if not m:
                add("access-missing", a, sid, "no access-map entry")
            else:
                for lvl in ("view", "act", "approve"):
                    for r in m.get(lvl, []):
                        if r not in roles:
                            add("access-role", a, sid, f"{lvl} names unknown role {r}")
                bad = [r for r in m.get("approve", []) if r in ("agent", "admin")]
                if bad:
                    add("approve-forbidden", a, sid, f"approve lists {bad} ({amap_src[sid]})", "mech")
                    if fix:
                        f = SPEC / amap_src[sid]
                        doc = json.loads(f.read_text())
                        doc[sid]["approve"] = [r for r in doc[sid]["approve"] if r not in ("agent", "admin")]
                        f.write_text(json.dumps(doc, indent=1, ensure_ascii=False) + "\n")
                gtxt = str(s.get("gate", "")).strip().lower()
                if not re.match(r"^(none|n/a|no gate)\b", gtxt) and gtxt and not [r for r in m.get("approve", []) if r not in ("agent", "admin")]:
                    add("gate-no-approver", a, sid, "screen has a gate but no approve-capable role mapped")
            stext = json.dumps(s)
            for t in re.findall(r"\b([a-z]+\.[a-z_]+)\b", stext):
                if t.split(".")[0] in cap_pfx and t not in caps and not t.endswith(("json", "html")):
                    add("cap-unknown", a, sid, f"capability {t} not in access.json matrix")
            for gid in set(GATE_RE.findall(stext)):
                if gid not in gate_def:
                    add("gate-undefined", a, sid, f"gate {gid} referenced but defined in no area")
            for act in s.get("actions", []):
                if act.get("by") == "agent" and str(act.get("gate", "")).lower() == "none":
                    blob = f"{act.get('name', '')} {act.get('note', '')}"
                    hit = EXTERNAL.search(act.get("name", ""))
                    if hit and not re.search(r"\b(draft|drafts|prepare|suggest|propose|queue|internal|preview|approved|after approval|after G[\w-]+|template)\b", blob, re.I):
                        add("agent-ungated-external", a, sid,
                            f"agent action '{act['name']}' gate=none with external-effect word '{hit.group(0)}'")
            for fld in s.get("fields", []):
                n, t = str(fld.get("name", "")), str(fld.get("type", ""))
                nn = n.strip().lower().replace(" ", "_")
                if MONEY_NAME.search(nn) and not MONEY_NOT.search(nn) and t not in MONEY_OK_TYPES:
                    if t in ("number", "int", "text", "decimal", "float"):
                        add("money-type", a, sid, f"field {n} typed {t}, should be money", "mech")
                        if fix:
                            fld["type"] = "money"
                            dirty.add(a)
                    elif t not in ("list", "table", "object", "map", "enum", "bool", "boolean", "ref", "percent"):
                        add("money-type", a, sid, f"field {n} typed {t}; money-like name")
                if DATE_NAME.search(nn) and t == "text":
                    add("date-type", a, sid, f"field {n} typed text, should be date/datetime", "mech")
                    if fix:
                        fld["type"] = "datetime" if nn.endswith("_at") else "date"
                        dirty.add(a)
    # entity fixes: aliases rewritten in relation text; still-undefined names get a stub in the referencing area
    if fix:
        stubs = collections.defaultdict(set)
        for f in F:
            if f["check"] == "entity-ref":
                stubs[f["item"].split(" -> ")[1]].add(f["area"])
        for a, d in data.items():
            for e in d.get("entities", []):
                rels = e.get("relations", [])
                new = [re.sub(r"\b(" + "|".join(ENTITY_ALIASES) + r")\b", lambda m: ENTITY_ALIASES[m.group(1)], r) for r in rels]
                if new != rels:
                    e["relations"] = new
                    dirty.add(a)
        for name, areas in sorted(stubs.items()):
            if name in ENTITY_ALIASES:
                continue
            owner = sorted(areas)[0]
            data[owner]["entities"].append({
                "name": name, "fields": ["id"],
                "relations": [f"referenced by {', '.join(sorted(areas))} relations"],
                "note": "stub added by spec lint: fields to be defined by the area owner"})
            dirty.add(owner)
    # gate stubs: undefined gates referenced -> add stub to owning area (most references)
    if fix and "--stub-gates" in sys.argv:
        und = collections.defaultdict(collections.Counter)
        for f in F:
            if f["check"] == "gate-undefined":
                und[f["msg"].split()[1]][f["area"]] += 1
        for gid, cnt in und.items():
            owner = cnt.most_common(1)[0][0]
            data[owner].setdefault("gates", []).append({
                "id": gid, "name": f"{gid} (stub: referenced by {', '.join(sorted(cnt))}; define)",
                "approver": "lawyer (verify)", "blocks": "TODO owner: define what this gate blocks"})
            dirty.add(owner)
    for a in dirty:
        p = SPEC / f"{a}.json"
        lines = p.read_text().splitlines()
        indent = (len(lines[1]) - len(lines[1].lstrip(" "))) if len(lines) > 1 else 1
        p.write_text(json.dumps(data[a], indent=indent or 1, ensure_ascii=False) + "\n")
    return F


def summarize(F):
    c = collections.Counter((f["check"], f["kind"]) for f in F if f["check"] != "verify-count")
    return c


if __name__ == "__main__":
    fix = "--fix" in sys.argv
    F = lint(fix=fix)
    out = None
    if "--json" in sys.argv:
        out = sys.argv[sys.argv.index("--json") + 1]
        pathlib.Path(out).write_text(json.dumps(F, indent=1))
    vc = {f["area"]: int(f["msg"]) for f in F if f["check"] == "verify-count"}
    for (chk, kind), n in sorted(summarize(F).items()):
        print(f"{chk:26} {kind:5} {n}")
    print("verify:", vc, "total", sum(vc.values()))
    base_path = pathlib.Path(_arg("--baseline", str(HERE / "o2_lint.baseline.json")))
    judg = collections.Counter(chk for (chk, kind), n in summarize(F).items() if kind != "mech" for _ in range(n))
    if "--write-baseline" in sys.argv:
        wp = pathlib.Path(_arg("--write-baseline", str(base_path)))
        wp.write_text(json.dumps(dict(sorted(judg.items())), indent=1) + "\n")
        print("baseline written:", wp)
    if "--strict" in sys.argv:
        base = json.loads(base_path.read_text()) if base_path.exists() else {}
        fails = [f"{chk} mech {n}" for (chk, kind), n in summarize(F).items() if kind == "mech" and n > 0]
        fails += [f"{chk} {n} > baseline {base.get(chk, 0)}" for chk, n in judg.items() if n > base.get(chk, 0)]
        if not SPEC.is_dir():
            fails.append(f"spec dir not found: {SPEC}")
        if fails:
            print("STRICT FAIL:", "; ".join(sorted(fails)))
            sys.exit(1)
        print("STRICT OK (spec:", SPEC, ")")
