/* Deadlines (court rules, limitations, contract dates with confirm + blind second check) and Work (board, agent work behind approval). */
(function () {
  "use strict";
  const A = window.App, h = A.h, esc = A.esc;
  const T = () => A.today;
  const me = () => A.me();

  /* ---------- shared helpers ---------- */
  const HOLIDAYS = {}; // date -> name, rebuilt from the dlHolidays store (holiday editor) by syncHol()
  A.seed("dlHolidays", [["2026-10-12", "Thanksgiving", false], ["2026-11-11", "Remembrance Day", true], ["2026-12-25", "Christmas Day", false], ["2026-12-28", "Boxing Day (observed)", false],
    ["2027-01-01", "New Year's Day", false], ["2027-02-15", "Family Day", false], ["2027-03-26", "Good Friday", false]].map(([date, name, courtOpen]) => ({ id: date, date, name, courtOpen, by: "u-linda" })));
  const syncHol = () => { Object.keys(HOLIDAYS).forEach((k) => delete HOLIDAYS[k]); A.store.all("dlHolidays").forEach((x) => (HOLIDAYS[x.date] = x.name + (x.courtOpen ? " (court open)" : ""))); };
  const MONTHS = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];
  const DOW = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  const dow = (d) => new Date(d + "T12:00:00").getDay();
  const fmt = (d) => (d ? `${DOW[dow(d)]} ${A.date ? A.date(d) : d}` : "—");
  const addYears = (d, n) => `${+d.slice(0, 4) + n}${d.slice(4)}`;
  const parseDate = (s) => { const m = /(January|February|March|April|May|June|July|August|September|October|November|December) (\d{1,2}), (\d{4})/.exec(s); return m ? `${m[3]}-${String(MONTHS.indexOf(m[1]) + 1).padStart(2, "0")}-${m[2].padStart(2, "0")}` : null; };
  const docLine = (c) => { const d = c && A.store.get("docs", c.doc); return d && d.pages[c.page - 1] ? d.pages[c.page - 1][c.line - 1] || "" : ""; };
  const cite = (c) => (c ? { ...c, label: (A.store.get("docs", c.doc) || {}).name || c.doc, quote: docLine(c) } : null);
  const dis = (ok, why) => (ok ? "" : A.raw(` disabled title="${esc(why)}"`));
  const hist = (rec, what) => [...(rec.history || []), { at: A.now(), by: me().id, what }];
  const mName = (id) => { const m = A.store.get("matters", id); return m ? `${m.number} ${m.name}` : id; };
  const weekStart = (d) => { const n = (dow(d) + 6) % 7; return A.addDays(d, -n); };

  /* ======================================================================
     DEADLINES
     ====================================================================== */
  const DL = "deadlines";
  const H0 = (by, what, at = "2026-10-08 09:10") => ({ at, by, what });
  A.seed(DL, [
    { id: "dl-1", matterId: "m1", title: "Statement of defence due (Northgate Storage)", due: "2026-10-13", kind: "court-rule", critical: true, status: "proposed", proposedBy: "agent", owner: "u-linda",
      trigger: "2026-09-23", rule: "Rules of Civil Procedure r.18.01(a): defence within 20 days after service of the statement of claim, defendant served in Ontario (verify)",
      calc: "Served 2026-09-23 (personal service, r.16.02(1)(c)) + 20 days = Tue 2026-10-13. Mon 2026-10-12 is Thanksgiving; the date is not a holiday, so no roll-forward (r.3.01(1)(c)).",
      source: { doc: "d-aos", page: 1, line: 3 }, history: [H0("agent", "Computed from affidavit of service")] },
    { id: "dl-2", matterId: "m1", title: "Answer D. Osei: 30-day extension request", due: "2026-10-13", kind: "internal", critical: false, status: "confirmed", proposedBy: "agent", owner: "u-linda",
      rule: "Firm practice: answer an extension request before the defence date it affects", source: { doc: "d-email-osei", page: 1, line: 5 },
      firstCheck: { by: "u-linda", at: "2026-10-08 11:02" }, history: [H0("agent", "Proposed from email"), H0("u-linda", "Confirmed", "2026-10-08 11:02")] },
    { id: "dl-3", matterId: "m2", title: "Requisition date (title requisitions to seller's lawyer)", due: "2026-10-16", kind: "contract", critical: true, status: "confirmed", proposedBy: "agent", owner: "u-yoni",
      rule: "Agreement of Purchase and Sale: requisitions must be delivered on or before the requisition date", source: { doc: "d-aps", page: 1, line: 6 },
      firstCheck: { by: "u-yoni", at: "2026-09-16 10:15" }, history: [H0("agent", "Extracted from APS", "2026-09-15 16:40"), H0("u-yoni", "Confirmed", "2026-09-16 10:15")] },
    { id: "dl-4", matterId: "m2", title: "Closing (completion date)", due: "2026-10-30", kind: "contract", critical: true, status: "second-checked", proposedBy: "agent", owner: "u-yoni",
      rule: "Agreement of Purchase and Sale: completion date", source: { doc: "d-aps", page: 2, line: 1 },
      firstCheck: { by: "u-yoni", at: "2026-09-16 10:16" }, secondCheck: { by: "u-kaleab", at: "2026-09-17 09:30", entered: "2026-10-30" },
      history: [H0("agent", "Extracted from APS", "2026-09-15 16:40"), H0("u-yoni", "Confirmed", "2026-09-16 10:16"), H0("u-kaleab", "Blind second check matched", "2026-09-17 09:30")] },
    { id: "dl-5", matterId: "m3", title: "Limitation period: claim against Lakeshore Transit", due: "2028-08-14", kind: "limitation", critical: true, status: "confirmed", proposedBy: "agent", owner: "u-ayo",
      trigger: "2026-08-14", rule: "Limitations Act, 2002, s.4: 2 years from the day the claim was discovered; s.5 discoverability presumed day of incident (verify). Check any municipal/transit notice period separately.",
      calc: "Incident 2026-08-14 + 2 years = 2028-08-14.", source: { doc: "d-incident", page: 1, line: 2 },
      firstCheck: { by: "u-ayo", at: "2026-10-06 14:20" }, history: [H0("agent", "Computed from incident report", "2026-10-05 17:05"), H0("u-ayo", "Confirmed", "2026-10-06 14:20")] },
    { id: "dl-6", matterId: "m5", title: "Limitation period: wrongful dismissal claim", due: "2028-08-29", kind: "limitation", critical: true, status: "proposed", proposedBy: "agent", owner: "u-ayo",
      trigger: "2026-08-29", rule: "Limitations Act, 2002, s.4: 2 years from termination (verify)", calc: "Termination 2026-08-29 + 2 years = 2028-08-29.",
      source: { doc: "d-term", page: 1, line: 2 }, history: [H0("agent", "Computed from termination letter", "2026-09-10 12:00")] },
    { id: "dl-7", matterId: "m5", title: "Demand letter response deadline", due: "2026-10-20", kind: "internal", critical: false, status: "confirmed", proposedBy: "u-ayo", owner: "u-ayo",
      rule: "Set in demand letter: 10 business days to respond", source: null, firstCheck: { by: "u-linda", at: "2026-10-02 09:00" }, history: [H0("u-ayo", "Created", "2026-10-01 15:00"), H0("u-linda", "Confirmed", "2026-10-02 09:00")] },
    { id: "dl-8", matterId: "m4", title: "File application for certificate of appointment", due: "2026-10-23", kind: "internal", critical: false, status: "proposed", proposedBy: "u-meraf", owner: "u-linda",
      rule: "Firm target date (Rules of Civil Procedure r.74.04 application package)", source: { doc: "d-will", page: 1, line: 2 }, history: [H0("u-meraf", "Created", "2026-10-07 10:00")] },
    { id: "dl-9", matterId: "m6", title: "Lease renewal: comments before term starts", due: "2026-10-20", kind: "contract", critical: false, status: "confirmed", proposedBy: "agent", owner: "u-yoni",
      rule: "Renewal term starts 2026-11-01; landlord needs comments ten days ahead", source: { doc: "d-lease", page: 1, line: 3 },
      firstCheck: { by: "u-yoni", at: "2026-07-29 10:00" }, history: [H0("agent", "Extracted from lease", "2026-07-28 15:00"), H0("u-yoni", "Confirmed", "2026-07-29 10:00")] },
    { id: "dl-10", matterId: "m7", title: "Answer undertakings from examination for discovery", due: "2026-10-28", kind: "court-rule", critical: false, status: "met", proposedBy: "u-linda", owner: "u-linda",
      rule: "Rules of Civil Procedure r.31.07 (verify)", source: null, firstCheck: { by: "u-yoni", at: "2026-09-30 09:00" }, metNote: "Answers served 2026-10-07",
      history: [H0("u-linda", "Created", "2026-09-29 09:00"), H0("u-yoni", "Confirmed", "2026-09-30 09:00"), H0("u-linda", "Met: answers served 2026-10-07", "2026-10-07 16:00")] },
  ]);

  const KINDS = { "court-rule": "Court rule", limitation: "Limitation", contract: "Contract", internal: "Internal" };
  /** Deadline status -> [label, App.STATUS word]. */
  const STATUS = { proposed: ["Proposed", "to-review"], confirmed: ["Confirmed", "confirmed"], "second-checked": ["Second-checked", "confirmed"], met: ["Met", "done"], released: ["Released", "closed"] };
  const visDl = () => { const ids = new Set(A.matters(null, "deadlines.view").map((m) => m.id)); return A.store.where(DL, (d) => ids.has(d.matterId)); };
  const open = (d) => !["met", "released"].includes(d.status);
  const needsSecond = (d) => d.critical && d.status === "confirmed";
  const unconfirmedCritical = () => (A.can("deadlines.view") ? visDl().filter((d) => d.critical && (d.status === "proposed" || d.status === "confirmed")).length : 0);
  function sev(d) {
    if (!open(d)) return "line";
    const n = A.daysBetween(T(), d.due);
    if (n < 0 || (d.critical && n <= 7)) return "bad";
    if (n <= 7) return "gate";
    if (n <= 30) return "warn";
    return "line";
  }
  const stripe = (d) => A.raw(`style="border-left:4px solid var(--${sev(d)})"`);
  const stChip = (d) => (needsSecond(d) && !d.mismatch ? A.ui.status("pending", "Needs second check") : A.ui.status((STATUS[d.status] || [d.status, d.status])[1], (STATUS[d.status] || [d.status])[0]));
  const stBadge = (d) => h`${stChip(d)}${d.critical ? A.ui.status("critical", "Critical") : ""}${d.mismatch ? A.ui.status("conflict", "Check mismatch") : ""}`;
  const pendingGate = (d) => A.store.where("approvals", (g) => g.effect === "deadlines-confirm" && g.status === "pending" && g.payload && g.payload.id === d.id)[0];

  function canConfirm(d) {
    if (d.status !== "proposed") return { ok: false, why: "Not awaiting confirmation" };
    if (!A.can("deadlines.verify", "approve")) return { ok: false, why: `${A.roleName(me().role)} cannot confirm deadlines (deadlines.verify)` };
    if (d.proposedBy === me().id) return { ok: false, why: "You proposed this; another lawyer must confirm it" };
    return { ok: true };
  }
  function canSecond(d) {
    if (!needsSecond(d)) return { ok: false, why: d.critical ? "Confirm first" : "Not a critical date" };
    if (!A.can("deadlines.edit", "act")) return { ok: false, why: "You cannot check deadlines" };
    if (d.firstCheck && d.firstCheck.by === me().id) return { ok: false, why: "You confirmed it; a different person must second-check" };
    return { ok: true };
  }
  const canEdit = () => A.can("deadlines.edit", "act");
  function confirmDeadline(id) {
    const d = A.store.get(DL, id); if (!d || d.status !== "proposed") return;
    A.store.put(DL, { id, status: "confirmed", firstCheck: { by: me().id, at: A.now() }, history: hist(d, "Confirmed against source") });
    A.audit("deadline.confirmed", { target: id, note: `${d.title} ${d.due}` });
    fire("deadline.confirmed", A.store.get(DL, id));
  }
  A.effect("deadlines-confirm", (p) => confirmDeadline(p.id));

  function proposeGate(d) {
    const c = cite(d.source);
    return A.gate({ kind: "deadline", cap: "deadlines.verify", title: `Confirm deadline: ${d.title}`, summary: `${fmt(d.due)} · ${KINDS[d.kind]}${d.critical ? " · critical (second check required after confirm)" : ""}`,
      matterId: d.matterId, preparedBy: d.proposedBy || "agent", body: h`<p><b>Rule:</b> ${d.rule}</p>${d.calc ? h`<p><b>Computation:</b> ${d.calc}</p>` : ""}`, cites: c ? [c] : [],
      checks: [{ label: "Source line found", ok: !!c }, { label: "Not a weekend or holiday", ok: !HOLIDAYS[d.due] && dow(d.due) % 6 !== 0, blocking: false }], effect: "deadlines-confirm", payload: { id: d.id } });
  }

  /* Rules the extractor applies to dated lines in a matter's documents. */
  const EXTRACT = [
    { test: (l) => /\bserved\b/i.test(l), make: (d) => { const sv = SERVICE[settings().service] || SERVICE.personal; const eff = A.addDays(d, sv.days); return { title: "Statement of defence due", kind: "court-rule", critical: true, due: A.addDays(eff, 20), trigger: d,
      rule: "Rules of Civil Procedure r.18.01(a): 20 days after service in Ontario (verify)", calc: `Served ${d}${sv.days ? ` by ${sv.label.split(" (")[0].toLowerCase()}, effective ${eff}` : ""} + 20 days = ${A.addDays(eff, 20)}. Service method from firm defaults; change it if this matter differs.` }; } },
    { test: (l) => /requisition date/i.test(l), make: (d) => ({ title: "Requisition date", kind: "contract", critical: true, due: d, rule: "Agreement of Purchase and Sale: requisition date" }) },
    { test: (l) => /completion date/i.test(l), make: (d) => ({ title: "Closing (completion date)", kind: "contract", critical: true, due: d, rule: "Agreement of Purchase and Sale: completion date" }) },
    { test: (l) => /date of incident/i.test(l), make: (d) => ({ title: "Limitation period", kind: "limitation", critical: true, due: addYears(d, 2), trigger: d,
      rule: "Limitations Act, 2002, s.4: 2 years from discovery (verify)", calc: `Incident ${d} + 2 years = ${addYears(d, 2)}.` }) },
    { test: (l, doc) => /termination/i.test(doc.name) && /^\s*\w+ \d{1,2}, \d{4}\s*$/.test(l), make: (d) => ({ title: "Limitation period: dismissal claim", kind: "limitation", critical: true, due: addYears(d, 2), trigger: d,
      rule: "Limitations Act, 2002, s.4 (verify)", calc: `Termination ${d} + 2 years = ${addYears(d, 2)}.` }) },
    { test: (l) => /\bterm:.*from/i.test(l), make: (d) => ({ title: "Lease term starts", kind: "contract", critical: false, due: d, rule: "Lease renewal: commencement date" }) },
  ];
  function extract(matterId) {
    const m = A.matter(matterId); if (!m) return;
    if (!canEdit()) return A.toast("You cannot add deadlines on this matter", "bad");
    const docs = A.store.where("docs", (x) => x.matterId === matterId);
    A.agent.run({ title: `Extract deadlines: ${m.number}`, matterId, steps: [`Reading ${docs.length} document${docs.length === 1 ? "" : "s"}`, "Finding dates and triggers", "Applying Ontario rules", "Checking holidays"],
      done() {
        let n = 0;
        docs.forEach((doc) => doc.pages.forEach((pg, pi) => pg.forEach((line, li) => {
          const dt = parseDate(line); if (!dt) return;
          const r = EXTRACT.find((x) => x.test(line, doc)); if (!r) return;
          const src = { doc: doc.id, page: pi + 1, line: li + 1 };
          if (A.store.where(DL, (x) => x.source && x.source.doc === src.doc && x.source.page === src.page && x.source.line === src.line && x.matterId === matterId).length) return;
          const d = A.store.put(DL, { id: A.id("dl"), matterId, status: "proposed", proposedBy: "agent", owner: m.responsible, source: src, history: [{ at: A.now(), by: "agent", what: `Extracted from ${doc.name}` }], ...r.make(dt) });
          proposeGate(d); fire("deadline.proposed", d); n++;
        })));
        return { title: n ? `${n} deadline${n > 1 ? "s" : ""} proposed for confirmation` : "No new deadlines found" };
      } });
  }

  /* ---------- shared list helpers (layout comes from App.ui: header, filters, split, list, table, bulkBar) ---------- */
  const narrow = () => typeof window !== "undefined" && window.innerWidth < 1100;
  const opts = (pairs, v) => pairs.map(([k, l]) => h`<option value="${k}" ${String(k) === String(v) ? A.raw("selected") : ""}>${l}</option>`);
  const SEL_DL = "deadlines", SEL_WK = "work";
  const selIds = (name, el) => (el && el.dataset && el.dataset.ids ? el.dataset.ids.split(",") : A.selection(name).ids()).filter(Boolean);
  const demo = () => h`<span class="muted small" title="Prototype: nothing leaves this browser">demo</span>`;
  /** Download rows as CSV and log the export. cols: [[header, fn(row)]]. */
  function csv(name, cols, rows) {
    const q = (v) => `"${String(v ?? "").replace(/"/g, '""')}"`;
    const text = [cols.map((c) => q(c[0])).join(","), ...rows.map((r) => cols.map((c) => q(c[1](r))).join(","))].join("\r\n");
    try { if (window.URL && URL.createObjectURL && typeof Blob !== "undefined") { const a = document.createElement("a"); a.href = URL.createObjectURL(new Blob([text], { type: "text/csv" })); a.download = `${name}-${T()}.csv`; document.body.appendChild(a); a.click(); a.remove(); } } catch (e) { /* download unavailable; the export is still logged */ }
    A.audit("export.csv", { target: name, note: `${rows.length} rows` }); A.toast(`Exported ${rows.length} row${rows.length === 1 ? "" : "s"} to ${name}-${T()}.csv`, "ok");
  }
  /* Keyboard: page keys live in KEYS and are registered with App.ui.keys; labels show in the ? sheet. */
  const KEYS = { deadlines: {}, work: {} };
  const KEYLABEL = {
    deadlines: { j: "Next deadline", k: "Previous deadline", Enter: "Open full record", x: "Select for bulk action", c: "Confirm (selected, or the one under the cursor)", a: "Confirm (same as c)", e: "Edit date and confirm", s: "Blind second check", m: "Mark met", n: "New deadline (rule wizard)", Escape: "Clear selection / back to list" },
    work: { j: "Next item", k: "Previous item", Enter: "Open item", x: "Select for bulk action", a: "Approve the output under the cursor (or selected)", r: "Hand to the agent", d: "Mark done", e: "Edit item", w: "Watch or stop watching", n: "New work item", Escape: "Clear selection / back to list" },
  };
  const registerKeys = () => {
    const reg = (area, prefix) => A.ui.keys(prefix, Object.fromEntries(Object.entries(KEYS[area]).map(([k, fn]) => [k, { label: KEYLABEL[area][k] || "", fn }])));
    reg("deadlines", /^deadlines(\/(?!calendar|settings|event)[^/]+|)$/); reg("work", /^work(\/(?!automations|settings|reports)[^/]+|)$/);
  };
  const move = (order, cur, dir) => { if (!order.length) return null; const i = order.indexOf(cur); return order[i < 0 ? 0 : Math.max(0, Math.min(order.length - 1, i + dir))]; };
  const scrollSel = () => setTimeout(() => { const s = document.querySelector("#view .srow.on"); if (s && s.scrollIntoView) s.scrollIntoView({ block: "nearest" }); }, 0);

  /* ---------- deadlines list ---------- */
  let dlView = "agenda", dlScope = "all", dlQ = "", dlKind = "", dlOwner = "", dlCur = null, dlOrder = [];
  A.onUser(() => { dlCur = null; });
  A.change("deadlines-q", (el) => { dlQ = el.value; A.refresh(); });
  function dlFilter(list) {
    const q = dlQ.trim().toLowerCase();
    return list.filter((d) => (dlScope !== "mine" || d.owner === me().id || ((A.store.get("matters", d.matterId) || {}).team || []).includes(me().id))
      && (!dlKind || d.kind === dlKind) && (!dlOwner || d.owner === dlOwner)
      && (!q || `${d.title} ${mName(d.matterId)} ${d.rule || ""}`.toLowerCase().includes(q)));
  }
  const needsPerson = (d) => open(d) && (d.status === "proposed" || needsSecond(d) || !!d.mismatch);
  const nextStep = (d) => (d.mismatch ? "Resolve check mismatch" : d.status === "proposed" ? "Lawyer to confirm" : needsSecond(d) ? "Second check by another person" : d.status === "met" ? "" : d.status === "released" ? "" : "Watch the date");
  function dlRow(d, showMatter = true) {
    syncHol();
    return h`<div class="kcard" data-a="deadlines-open" data-id="${d.id}" ${stripe(d)}>
      <div class="row wrap"><b class="grow">${d.title}</b>${stBadge(d)}</div>
      <small>${fmt(d.due)} · ${KINDS[d.kind]}${showMatter ? ` · ${mName(d.matterId)}` : ""}${open(d) ? ` · ${A.daysBetween(T(), d.due)} days` : ""}${HOLIDAYS[A.addDays(d.due, -1)] ? ` · day before is ${HOLIDAYS[A.addDays(d.due, -1)]}` : ""}</small></div>`;
  }
  const dlListRow = (d) => ({ id: d.id, on: d.id === dlCur, title: d.title,
    sub: `${fmt(d.due)} · ${KINDS[d.kind]} · ${mName(d.matterId)} · ${A.user(d.owner).name}${needsPerson(d) ? ` · Next: ${nextStep(d)}` : ""}`,
    right: h`${stBadge(d)}${open(d) ? A.ui.due(d.due, { soon: 7 }) : ""}` });
  function agenda(list, grouped) {
    const act = list.sort((a, b) => a.due.localeCompare(b.due));
    dlOrder = act.map((d) => d.id);
    if (!act.length) return A.ui.empty(grouped ? "No open deadlines match" : "Nothing waiting here", grouped ? " Clear the filters, or extract dates from a matter's documents." : " Every date the agent proposed has been confirmed and checked.");
    const L = (xs) => A.ui.list(xs.map(dlListRow), "deadlines-sel", { select: SEL_DL });
    if (!grouped) return L(act);
    const groups = {}; act.forEach((d) => { const w = A.daysBetween(T(), d.due) < 0 ? "Overdue" : weekStart(d.due); (groups[w] = groups[w] || []).push(d); });
    const tw = weekStart(T());
    return h`${Object.keys(groups).sort().map((w) => h`<h3 class="small muted" style="margin:12px 0 6px;text-transform:uppercase;letter-spacing:.05em">${w === "Overdue" ? "Overdue" : w === tw ? `This week · from ${fmt(w)}` : w === A.addDays(tw, 7) ? `Next week · from ${fmt(w)}` : `Week of ${fmt(w)}`}</h3>${L(groups[w])}`)}`;
  }
  /** Action buttons for one deadline; used in the side pane and the full record. */
  function dlActions(d) {
    if (!open(d)) return "";
    const c1 = canConfirm(d), c2 = canSecond(d), law = A.can("deadlines.verify", "approve");
    const b = (o) => A.ui.btn({ id: d.id, ...o });
    return h`<div class="row wrap" style="margin-top:12px">
      ${d.status === "proposed" ? h`${b({ label: "Confirm", a: "deadlines-confirm", primary: true, disabled: !c1.ok, why: c1.why, title: "Confirm against source (c)" })}${b({ label: "Edit and confirm", a: "deadlines-edit", disabled: !c1.ok, why: c1.why, title: "Edit the date, then confirm (e)" })}${b({ label: "Reject", a: "deadlines-reject", disabled: !law, why: "Only a lawyer can reject a proposed date" })}` : ""}
      ${d.mismatch ? b({ label: "Resolve mismatch", a: "deadlines-resolve", primary: true, disabled: !law, why: "Only a lawyer can resolve a mismatch" }) : ""}
      ${needsSecond(d) ? b({ label: "Blind second check", a: "deadlines-second", primary: !d.mismatch, disabled: !c2.ok, why: c2.why, title: "Blind second check (s)" }) : ""}
      ${["confirmed", "second-checked"].includes(d.status) ? b({ label: "Mark met", a: "deadlines-met", disabled: !(canEdit() && !needsSecond(d)), why: needsSecond(d) ? "Second check first" : "You cannot update deadlines", title: "Mark met with proof (m)" }) : ""}
      ${d.status !== "proposed" ? b({ label: "Release…", a: "deadlines-release", danger: true, disabled: !law, why: "Only a lawyer can release a deadline" }) : ""}</div>`;
  }
  const whoActs = (d) => h`<div class="row wrap" style="gap:8px;margin:0 0 6px">${d.proposedBy === "agent" ? A.ui.actor("agent", "Agent proposed") : ""}${needsPerson(d) ? A.ui.actor("person", nextStep(d)) : ""}</div>`;
  function dlPane(d) {
    if (!d) return null;
    return h`<div class="card" ${stripe(d)}><div class="row wrap" style="align-items:flex-start"><div class="grow"><div class="small muted">${mName(d.matterId)}</div><h3 style="margin:2px 0 6px">${d.title}</h3></div>${stBadge(d)}</div>
      ${whoActs(d)}
      <dl class="kv"><dt>Due</dt><dd><b>${fmt(d.due)}</b> ${open(d) ? A.ui.due(d.due, { soon: 7 }) : ""}</dd><dt>Rule</dt><dd>${d.rule}</dd>${d.calc ? h`<dt>Computation</dt><dd>${d.calc}</dd>` : ""}
        <dt>Responsible</dt><dd>${A.user(d.owner).name}</dd><dt>Proposed by</dt><dd>${d.proposedBy === "agent" ? "✦ Agent" : A.user(d.proposedBy).name}</dd>
        <dt>Confirmed</dt><dd>${d.firstCheck ? `${A.user(d.firstCheck.by).name} · ${A.dateTime(d.firstCheck.at)}` : "Not yet"}</dd>${d.critical ? h`<dt>Second check</dt><dd>${d.secondCheck ? `${A.user(d.secondCheck.by).name} · matched` : d.mismatch ? "Mismatch" : "Not yet"}</dd>` : ""}</dl>
      ${dlActions(d)}
      <div style="margin-top:12px">${sourcePanel(d)}</div>
      <div class="row" style="margin-top:8px"><span class="grow"></span>${A.ui.btn({ label: "Full record and history", a: "deadlines-open", id: d.id, small: true, title: "Open full record (Enter)" })}</div></div>`;
  }

  function deadlinesPage(el) {
    syncHol();
    A.setContext({ kind: "deadlines", label: "Deadlines" });
    const all = visDl(), list = dlFilter(all), o = list.filter(open);
    const n = (f) => o.filter(f).length, review = o.filter(needsPerson), second = o.filter(needsSecond);
    const owners = [...new Set(all.map((d) => d.owner))];
    let body;
    if (dlView === "closed") {
      dlOrder = [];
      body = A.ui.table([{ label: "Due", render: (d) => fmt(d.due) }, { label: "Deadline", key: "title" }, { label: "Matter", render: (d) => mName(d.matterId) }, { label: "Status", render: stBadge }, { label: "Note", render: (d) => d.releaseReason || d.metNote || "" }],
        list.filter((d) => !open(d)), { onRow: "deadlines-open", empty: "Nothing met or released yet", select: SEL_DL });
    } else {
      const listHtml = agenda(dlView === "review" ? review : dlView === "second" ? second : o, dlView === "agenda");
      if (!dlOrder.includes(dlCur)) dlCur = dlOrder[0] || null;
      body = A.ui.split(listHtml, dlPane(dlCur && A.store.get(DL, dlCur)), { emptyTitle: "Pick a deadline" });
    }
    const picked = selIds(SEL_DL).map((id) => A.store.get(DL, id)).filter(Boolean), nConf = picked.filter((d) => canConfirm(d).ok).length;
    const im = pendingImports();
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Deadlines", sub: "No date counts until a lawyer confirms it against its source. Critical dates also need a blind second check by someone else.",
        actions: [{ label: "Calendar", to: "deadlines/calendar" }, { label: "Settings", to: "deadlines/settings" }],
        primary: { label: "New deadline", a: "deadlines-new", key: "n", disabled: !canEdit(), why: "You cannot add deadlines" } })}
      <div class="stats">${A.ui.stat("Overdue", n((d) => d.due < T()), "", n((d) => d.due < T()) ? "bad" : "")}${A.ui.stat("Due in 7 days", n((d) => A.daysBetween(T(), d.due) >= 0 && A.daysBetween(T(), d.due) <= 7))}
        ${A.ui.stat("Awaiting confirm", n((d) => d.status === "proposed"), "", n((d) => d.status === "proposed") ? "gate" : "")}${A.ui.stat("Awaiting second check", second.length, "", second.length ? "gate" : "")}</div>
      ${im.length ? h`<div class="card" style="margin-bottom:12px;border-color:var(--gate)"><div class="row wrap"><span class="av agent">✦</span><div class="grow"><b>${im.length} imported task${im.length === 1 ? " is" : "s are"} marked as limitation periods</b><div class="small muted">From your previous system. They are not deadlines yet: the agent proposes each one for a lawyer to confirm and matches any that already exist.</div></div>
        ${A.ui.btn({ label: "✦ Convert to proposed deadlines", a: "deadlines-import", agent: true, disabled: !canEdit(), why: "You cannot add deadlines" })}</div></div>` : ""}
      ${A.ui.filters({ search: { c: "deadlines-q", value: dlQ, placeholder: "Search deadlines, matters, rules" },
        chips: [{ id: "agenda", label: "Agenda", count: o.length }, { id: "review", label: "To review", count: review.length }, { id: "second", label: "Second check", count: second.length }, { id: "closed", label: "Met and released" }], active: dlView, action: "deadlines-view",
        selects: [{ c: "deadlines-f-scope", value: dlScope, label: "Scope", options: [["all", "Everyone's"], ["mine", "Mine"]] }, { c: "deadlines-f-kind", value: dlKind, label: "Type", options: [["", "All types"], ...Object.entries(KINDS)] },
          { c: "deadlines-f-owner", value: dlOwner, label: "Responsible lawyer", options: [["", "Any responsible"], ...owners.map((u) => [u, A.user(u).name])] }],
        right: h`${dlQ || dlKind || dlOwner || dlScope !== "all" ? A.ui.btn({ label: "Clear filters", a: "deadlines-f-clear", small: true }) : ""}${A.ui.btn({ label: "Export CSV", a: "deadlines-csv", small: true, title: "Export the deadlines in view" })}` })}
      ${A.ui.bulkBar(SEL_DL, [{ label: `Confirm ${nConf}`, a: "deadlines-bulk-confirm", primary: true, disabled: !nConf, why: "None of the selected dates are yours to confirm" },
        { label: "Reassign…", a: "deadlines-bulk-owner", disabled: !canEdit(), why: "You cannot reassign deadlines" }, { label: "Export CSV", a: "deadlines-csv" }])}
      ${body}
    </div>`);
  }
  A.change("deadlines-f-scope", (el) => { dlScope = el.value; A.refresh(); });
  A.change("deadlines-f-kind", (el) => { dlKind = el.value; A.refresh(); });
  A.change("deadlines-f-owner", (el) => { dlOwner = el.value; A.refresh(); });
  A.action("deadlines-f-clear", () => { dlQ = ""; dlKind = ""; dlOwner = ""; dlScope = "all"; A.refresh(); });
  A.action("deadlines-sel", (el) => { if (narrow()) return A.go(`deadlines/${el.dataset.id}`); dlCur = el.dataset.id; A.refresh(); });
  A.action("deadlines-csv", (el) => {
    const ids = el.dataset.ids ? el.dataset.ids.split(",") : null;
    const rows = (ids ? ids.map((id) => A.store.get(DL, id)).filter(Boolean) : dlFilter(visDl())).sort((a, b) => a.due.localeCompare(b.due));
    csv("deadlines", [["Due", (d) => d.due], ["Deadline", (d) => d.title], ["Matter", (d) => mName(d.matterId)], ["Type", (d) => KINDS[d.kind]], ["Critical", (d) => (d.critical ? "yes" : "no")], ["Status", (d) => STATUS[d.status][0]], ["Responsible", (d) => A.user(d.owner).name], ["Rule", (d) => d.rule]], rows);
  });
  function sourcePanel(d) {
    if (!d.source) return A.ui.empty("No source document", " Entered by hand; confirm against the file.");
    const doc = A.store.get("docs", d.source.doc); if (!doc) return A.ui.empty("Source missing");
    const pg = doc.pages[d.source.page - 1] || []; const from = Math.max(0, d.source.line - 3), to = Math.min(pg.length, d.source.line + 2);
    return h`<div class="row" style="margin-bottom:6px"><b class="grow">${doc.name}</b>${A.ui.cite(cite(d.source))}</div>
      <div class="docview">${pg.slice(from, to).map((l, i) => h`<div class="ln ${from + i + 1 === d.source.line ? "hit" : ""}"><i>${from + i + 1}</i><span>${l}</span></div>`)}</div>
      <p class="small muted">Page ${d.source.page}, line ${d.source.line}${A.openCite ? "" : " · quoted from the document"}</p>`;
  }

  function deadlineDetail(el, p) {
    syncHol();
    const d = A.store.get(DL, p.id); const m = d && A.matter(d.matterId);
    if (!d || !m || !A.canMatter(m, "deadlines.view")) return A.render(el, h`<div class="page">${A.ui.header({ title: "Not available", back: { label: "Deadlines", to: "deadlines" } })}<div class="locked"><b>Not available</b>This deadline does not exist or you cannot see its matter.</div></div>`);
    A.setContext({ kind: "deadline", id: d.id, label: d.title });
    const g = pendingGate(d), pol = remPolicy()[d.kind] || {};
    const rem = String(pol.days || "").split(",").map((x) => +x.trim()).filter((x) => x > 0).map((n) => ({ n, at: A.addDays(d.due, -n) }));
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: d.title, status: stBadge(d), back: { label: "Deadlines", to: "deadlines" }, meta: h`<span class="muted small"> · <a href="#" data-a="go" data-to="matters/${m.id}">${m.number} ${m.name}</a></span>` })}
      ${whoActs(d)}
      <div class="grid g2">
        <div class="card" ${stripe(d)}>
          <dl class="kv"><dt>Due</dt><dd><b>${fmt(d.due)}</b> ${open(d) ? A.ui.due(d.due, { soon: 7 }) : ""}</dd>
            <dt>Type</dt><dd>${KINDS[d.kind]}${d.critical ? " · critical" : ""}</dd>
            <dt>Rule</dt><dd>${d.rule}</dd>${d.calc ? h`<dt>Computation</dt><dd>${d.calc}</dd>` : ""}
            ${HOLIDAYS[A.addDays(d.due, -1)] || HOLIDAYS[d.due] ? h`<dt>Holiday</dt><dd>${HOLIDAYS[d.due] ? `${fmt(d.due)} is ${HOLIDAYS[d.due]}: roll to next business day` : `${fmt(A.addDays(d.due, -1))} is ${HOLIDAYS[A.addDays(d.due, -1)]}; office closed the day before`}</dd>` : ""}
            <dt>Responsible</dt><dd>${A.user(d.owner).name}</dd>${d.imported ? h`<dt>Origin</dt><dd>Imported limitation task; recompute from the trigger before confirming</dd>` : ""}
            <dt>Proposed by</dt><dd>${d.proposedBy === "agent" ? "✦ Agent" : A.user(d.proposedBy).name}</dd>
            <dt>Confirmed</dt><dd>${d.firstCheck ? `${A.user(d.firstCheck.by).name} · ${A.dateTime(d.firstCheck.at)}` : "Not yet"}</dd>
            ${d.critical ? h`<dt>Second check</dt><dd>${d.secondCheck ? `${A.user(d.secondCheck.by).name} · ${A.dateTime(d.secondCheck.at)} · matched` : d.mismatch ? "Mismatch: recompute before relying on this date" : "Not yet"}</dd>` : ""}
            ${d.releaseReason ? h`<dt>Released</dt><dd>${d.releaseReason}</dd>` : ""}${d.metNote ? h`<dt>Met</dt><dd>${d.metNote}</dd>` : ""}</dl>
          ${dlActions(d)}
          ${g ? h`<div style="margin-top:12px">${A.ui.gateCard(g, { compact: true })}</div>` : ""}
        </div>
        <div class="card"><h3>Source</h3>${sourcePanel(d)}</div>
      </div>
      <div class="card" style="margin-top:14px"><h3>Reminders</h3><p class="muted small">From the firm's ${KINDS[d.kind].toLowerCase()} policy: ${(pol.channels || []).map((c) => CHANNELS[c]).join(", ") || "no channels"}. Proposed dates send no reminders until confirmed.</p>
        ${A.ui.table([{ label: "When", render: (r) => fmt(r.at) }, { label: "Before due", render: (r) => `${r.n} day${r.n === 1 ? "" : "s"}` }, { label: "To", render: () => A.user(d.owner).name },
          { label: "Status", render: (r) => (!open(d) || d.status === "proposed" ? A.ui.status("inactive", "Not sent") : r.at <= T() ? A.ui.status("sent") : A.ui.status("scheduled")) }], rem, { empty: "No reminders in the policy for this type" })}</div>
      <div class="card" style="margin-top:14px"><h3>History</h3>
        ${A.ui.table([{ label: "When", render: (x) => A.dateTime(x.at) }, { label: "Who", render: (x) => (x.by === "agent" ? "✦ Agent" : A.user(x.by).name) }, { label: "What", key: "what" }], [...(d.history || [])].reverse())}</div>
    </div>`);
  }

  A.action("deadlines-open", (el) => A.go(`deadlines/${el.dataset.id}`));
  A.action("deadlines-view", (el) => { dlView = el.dataset.id; A.refresh(); });
  A.action("deadlines-extract", (el) => extract(el.dataset.id));
  /** Confirm one proposed date through its approval when there is one. Returns true when confirmed. */
  function confirmOne(d, quiet) {
    const c = canConfirm(d); if (!c.ok) { if (!quiet) A.toast(c.why, "bad"); return false; }
    const g = pendingGate(d);
    if (g) { if (!A.canApprove(g).ok) { if (!quiet) A.toast(A.canApprove(g).why, "bad"); return false; } A.approve(g.id); }
    else { confirmDeadline(d.id); if (!quiet) A.toast("Deadline confirmed", "ok"); }
    return true;
  }
  A.action("deadlines-confirm", (el) => { const d = A.store.get(DL, el.dataset.id); if (d) confirmOne(d); });
  function bulkConfirm(el) {
    const xs = selIds(SEL_DL, el).map((id) => A.store.get(DL, id)).filter(Boolean);
    let ok = 0; xs.forEach((d) => { if (confirmOne(d, true)) ok++; });
    const crit = xs.filter((d) => d.critical && A.store.get(DL, d.id).status === "confirmed").length;
    A.selection(SEL_DL).clear();
    A.toast(`${ok} of ${xs.length} confirmed${ok < xs.length ? "; the rest are not yours to confirm" : ""}${crit ? `. ${crit} critical date${crit === 1 ? "" : "s"} now need a second check` : ""}`, ok ? "ok" : "bad");
    A.refresh();
  }
  A.action("deadlines-bulk-confirm", bulkConfirm);
  const lawyers = () => A.store.where("users", (u) => A.grant("deadlines.view", u.role) !== "none" && u.role !== "client");
  A.action("deadlines-bulk-owner", (el) => {
    if (!canEdit()) return A.toast("You cannot reassign deadlines", "bad");
    const xs = selIds(SEL_DL, el).map((id) => A.store.get(DL, id)).filter(Boolean); if (!xs.length) return;
    A.modal(`Reassign ${xs.length} deadline${xs.length === 1 ? "" : "s"}`, h`<p class="small muted">Use this when a lawyer is away. Reminders go to the new responsible person from today.</p>
      <label class="field">Responsible<select id="dlowner" class="input">${opts(lawyers().map((u) => [u.id, `${u.name} · ${A.roleName(u.role)}`]), me().id)}</select></label>`, [{ label: "Cancel" }, { label: "Reassign", primary: true, run: () => {
      const to = document.getElementById("dlowner").value;
      xs.forEach((d) => { A.store.put(DL, { id: d.id, owner: to, history: hist(d, `Responsible changed to ${A.user(to).name}`) }); A.audit("deadline.reassigned", { target: d.id, note: `${d.title} -> ${A.user(to).name}` }); });
      A.selection(SEL_DL).clear(); A.toast(`Reassigned to ${A.user(to).name}`, "ok");
    } }]);
  });
  A.action("deadlines-edit", (el) => {
    const d = A.store.get(DL, el.dataset.id); if (!d) return; const c = canConfirm(d); if (!c.ok) return A.toast(c.why, "bad");
    A.modal("Edit and confirm", h`<p class="small"><b>Proposed:</b> ${fmt(d.due)} · ${d.rule}</p>${d.source ? h`<p class="small"><b>Source:</b> “${docLine(d.source)}”</p>` : ""}
      <label class="field">Correct date<input type="date" id="dledate" class="input" value="${d.due}"></label>
      <label class="field">Why it changed<input id="dlenote" class="input" placeholder="e.g. Served by mail, effective 5th day (r.16.06(2))"></label>`, [{ label: "Cancel" }, { label: "Save and confirm", primary: true, run: () => {
      const v = document.getElementById("dledate").value, why = document.getElementById("dlenote").value.trim();
      if (!v) { A.toast("Enter the date", "bad"); return false; }
      if (v !== d.due && !why) { A.toast("Say why the date changed", "bad"); return false; }
      if (v !== d.due) { A.store.put(DL, { id: d.id, due: v, calc: `${d.calc ? d.calc + " " : ""}Changed from ${d.due} to ${v} by ${me().name}: ${why}`, history: hist(d, `Date changed ${d.due} -> ${v}: ${why}`) }); A.audit("deadline.edited", { target: d.id, note: `${d.due} -> ${v}: ${why}` }); }
      confirmOne(A.store.get(DL, d.id));
    } }]);
  });
  A.action("deadlines-reject", (el) => {
    const d = A.store.get(DL, el.dataset.id); if (!d) return; if (!A.can("deadlines.verify", "approve")) return A.toast("Only a lawyer can reject a proposed date", "bad");
    A.modal("Reject proposed date", h`<p class="small">The record is kept as released with your reason; the agent learns nothing is due here.</p><label class="field">Reason<input id="dlrej" class="input" value="Not a deadline on this matter"></label>`, [{ label: "Cancel" }, { label: "Reject", primary: true, run: () => {
      const v = document.getElementById("dlrej").value.trim(); if (!v) { A.toast("A reason is required", "bad"); return false; }
      A.store.put(DL, { id: d.id, status: "released", releaseReason: `Rejected: ${v}`, history: hist(d, `Rejected: ${v}`) }); A.audit("deadline.rejected", { target: d.id, note: v });
      const g = pendingGate(d); if (g) A.store.put("approvals", { id: g.id, status: "returned", decidedBy: me().id, decidedAt: A.now(), note: `Rejected: ${v}` });
    } }]);
  });
  A.action("deadlines-resolve", (el) => {
    const d = A.store.get(DL, el.dataset.id); if (!d || !d.mismatch) return; if (!A.can("deadlines.verify", "approve")) return A.toast("Only a lawyer can resolve a mismatch", "bad");
    A.modal("Resolve check mismatch", h`<p class="small">Set the correct date. A different person then repeats the blind second check.</p><label class="field">Correct date<input type="date" id="dlrdate" class="input" value="${d.due}"></label><label class="field">Resolution note<input id="dlrnote" class="input" value="Recomputed from the source"></label>`, [{ label: "Cancel" }, { label: "Resolve", primary: true, run: () => {
      const v = document.getElementById("dlrdate").value, why = document.getElementById("dlrnote").value.trim(); if (!v || !why) { A.toast("Date and note are required", "bad"); return false; }
      A.store.put(DL, { id: d.id, due: v, mismatch: false, status: "confirmed", firstCheck: { by: me().id, at: A.now() }, secondCheck: null, history: hist(d, `Mismatch resolved: ${v} (${why})`) }); A.audit("deadline.mismatch-resolved", { target: d.id, note: `${v}: ${why}` });
    } }]);
  });

  /* ---------- rule-based deadline wizard (trigger -> chain of proposed dates) ---------- */
  const CHAINS = {
    claim: { label: "Statement of claim served (we act for the defendant)", steps: [
      { title: "Notice of intent to defend", n: 10, rule: "Rules of Civil Procedure r.18.02(1): 10 days after service (verify)" },
      { title: "Statement of defence", n: 20, rule: "Rules of Civil Procedure r.18.01(a): 20 days after service, defendant served in Ontario (verify)", critical: true },
      { title: "Statement of defence (notice of intent delivered)", n: 30, rule: "Rules of Civil Procedure r.18.02(2): 10 further days after a notice of intent (verify)", critical: true, optional: true }] },
    defence: { label: "Statement of defence served (we act for the plaintiff)", steps: [
      { title: "Reply", n: 10, rule: "Rules of Civil Procedure r.25.04(1): 10 days after service of the defence (verify)" },
      { title: "Agree discovery plan", n: 70, rule: "Rules of Civil Procedure r.29.1.03(2): 60 days after close of pleadings, counted from day 10 (verify)" }] },
    motion: { label: "Motion hearing date (we are the moving party)", before: true, steps: [
      { title: "Serve and file motion record", n: 7, rule: "Rules of Civil Procedure r.37.10(2): at least 7 days before the hearing (verify)", critical: true },
      { title: "Serve and file factum", n: 7, rule: "Rules of Civil Procedure r.37.10.1(1): at least 7 days before the hearing (verify)" }] },
    single: { label: "One date I enter (contract, internal, limitation)", steps: [] },
  };
  const closed = (d) => dow(d) % 6 === 0 || (HOLIDAYS[d] && !/court open/.test(HOLIDAYS[d]));
  const roll = (d, back) => { let x = d, k = 0; while (closed(x) && k++ < 10) x = A.addDays(x, back ? -1 : 1); return x; };
  function chainFor(v) {
    const ch = CHAINS[v.trigger] || CHAINS.claim;
    if (v.trigger === "single") return v.title ? [{ title: v.title, due: v.date, kind: v.kind || "internal", critical: v.kind === "limitation" || v.kind === "contract", rule: "Entered by hand; confirm against the file", calc: "" }] : [];
    const sv = SERVICE[v.service] || SERVICE.personal; const eff = ch.before ? v.date : A.addDays(v.date, sv.days);
    return ch.steps.filter((s) => !s.optional || v.optional).map((s) => {
      const raw = A.addDays(eff, ch.before ? -s.n : s.n), due = roll(raw, ch.before);
      return { title: s.title, due, kind: "court-rule", critical: !!s.critical, rule: s.rule, trigger: v.date,
        calc: `${ch.before ? `Hearing ${v.date} - ${s.n} days` : `Served ${v.date}${sv.days ? ` by ${sv.label.split(" (")[0].toLowerCase()}, effective ${eff}` : ""} + ${s.n} days`} = ${raw}${raw !== due ? `; ${raw} is a ${HOLIDAYS[raw] || "weekend"}, moved ${ch.before ? "back" : "forward"} to ${due} (r.3.01(1), verify)` : ""}.` };
    });
  }
  function wizPreview() {
    const box = document.getElementById("dlwizprev"), f = document.getElementById("dlwiz"); if (!box || !f) return;
    const v = A.formValues(f); f.querySelectorAll("[data-only]").forEach((x) => (x.hidden = !x.dataset.only.split(" ").includes(v.trigger)));
    const rows = v.date ? chainFor(v) : [];
    box.innerHTML = String(rows.length ? A.ui.table([{ label: "Deadline", render: (r) => h`<b>${r.title}</b>${r.critical ? h` ${A.ui.status("critical", "Critical")}` : ""}<br><small class="muted">${r.calc || r.rule}</small>` }, { label: "Due", render: (r) => fmt(r.due) }], rows) : A.ui.empty("Nothing to preview", " Pick a trigger and date."));
  }
  A.change("deadlines-wiz", wizPreview);
  function openWizard(matterId) {
    if (!canEdit()) return A.toast("You cannot add deadlines", "bad");
    const ms = A.matters(null, "deadlines.view"); if (!ms.length) return A.toast("No matters you can add deadlines to", "bad");
    const s = settings();
    A.modal("New deadline", h`<div id="dlwiz" class="form">
      <label class="field">Matter<select name="matter" class="input" data-c="deadlines-wiz">${opts(ms.map((m) => [m.id, `${m.number} ${m.name}`]), matterId || ms[0].id)}</select></label>
      <label class="field">Trigger<select name="trigger" class="input" data-c="deadlines-wiz">${opts(Object.entries(CHAINS).map(([k, c]) => [k, c.label]), "claim")}</select></label>
      <label class="field">Date of the trigger<input type="date" name="date" class="input" value="${T()}" data-c="deadlines-wiz"></label>
      <label class="field" data-only="claim defence">Service method<select name="service" class="input" data-c="deadlines-wiz">${opts(Object.entries(SERVICE).map(([k, x]) => [k, x.label]), s.service)}</select></label>
      <label class="field" data-only="claim"><span><input type="checkbox" name="optional" data-c="deadlines-wiz"> Include the date if a notice of intent is delivered</span></label>
      <label class="field" data-only="single">Title<input name="title" class="input" data-c="deadlines-wiz" placeholder="e.g. Requisition date"></label>
      <label class="field" data-only="single">Type<select name="kind" class="input" data-c="deadlines-wiz">${opts(Object.entries(KINDS), "contract")}</select></label></div>
      <h3 style="margin:12px 0 6px;font-size:13px">Preview · saved as proposed; a lawyer other than you confirms each date</h3><div id="dlwizprev"></div>`,
      [{ label: "Cancel" }, { label: "Save as proposed", primary: true, run: () => {
        const v = A.formValues(document.getElementById("dlwiz")); const m = A.matter(v.matter); if (!m) { A.toast("Pick a matter", "bad"); return false; }
        if (!v.date) { A.toast("Enter the trigger date", "bad"); return false; }
        const rows = chainFor(v); if (!rows.length) { A.toast("Give the date a title", "bad"); return false; }
        let n = 0;
        rows.forEach((r) => {
          if (A.store.where(DL, (x) => x.matterId === m.id && x.title === r.title && x.due === r.due).length) return;
          const d = A.store.put(DL, { id: A.id("dl"), matterId: m.id, status: "proposed", proposedBy: me().id, owner: m.responsible, source: null, history: [{ at: A.now(), by: me().id, what: `Created with the rule wizard (${CHAINS[v.trigger].label})` }], ...r });
          proposeGate(d); fire("deadline.proposed", d); n++;
        });
        A.audit("deadline.wizard", { target: m.id, note: `${n} proposed from ${v.trigger} ${v.date}` });
        A.toast(n ? `${n} date${n === 1 ? "" : "s"} proposed for confirmation` : "Those dates already exist", n ? "ok" : "");
        dlView = "review";
      } }]);
    wizPreview();
  }
  A.action("deadlines-new", (el) => openWizard(el.dataset.id));

  /* ---------- deadline keys ---------- */
  const dlTarget = (r) => (/^deadlines\/./.test(r) ? r.split("/")[1] : dlCur);
  const dlBtn = (name, id) => { const b = document.querySelector(`#view [data-a="${name}"][data-id="${id}"]`); if (b && !b.disabled) { b.click(); return true; } const why = b && b.title; if (why) A.toast(why, "bad"); return !!b; };
  const listView = (r) => r === "deadlines" && dlView !== "closed";
  Object.assign(KEYS.deadlines, {
    j: (e, r) => { if (!listView(r)) return false; dlCur = move(dlOrder, dlCur, 1); A.refresh(); scrollSel(); },
    k: (e, r) => { if (!listView(r)) return false; dlCur = move(dlOrder, dlCur, -1); A.refresh(); scrollSel(); },
    Enter: (e, r) => { if (!listView(r) || !dlCur) return false; A.go(`deadlines/${dlCur}`); },
    x: (e, r) => { if (!listView(r) || !dlCur) return false; A.selection(SEL_DL).toggle(dlCur); A.refresh(); },
    c: (e, r) => { if (r === "deadlines" && A.selection(SEL_DL).size) return bulkConfirm(); const id = dlTarget(r); if (!id) return false; const d = A.store.get(DL, id); if (d) confirmOne(d); },
    a: (e, r) => KEYS.deadlines.c(e, r),
    e: (e, r) => { const id = dlTarget(r); if (!id || !dlBtn("deadlines-edit", id)) return false; },
    s: (e, r) => { const id = dlTarget(r); if (!id || !dlBtn("deadlines-second", id)) return false; },
    m: (e, r) => { const id = dlTarget(r); if (!id || !dlBtn("deadlines-met", id)) return false; },
    n: () => openWizard(),
    Escape: (e, r) => { if (r !== "deadlines") return A.go("deadlines"); if (!A.selection(SEL_DL).size) return false; A.selection(SEL_DL).clear(); A.refresh(); },
  });
  A.action("deadlines-second", (el) => {
    const d = A.store.get(DL, el.dataset.id); if (!d) return; const c = canSecond(d); if (!c.ok) return A.toast(c.why, "bad");
    A.modal("Blind second check", h`<p>Compute the date yourself from the source and rule. The first date is hidden.</p>
      <p class="small"><b>Rule:</b> ${d.rule}</p>${d.source ? h`<p class="small"><b>Source:</b> “${docLine(d.source)}”</p>` : ""}
      <label class="field">Your computed date<input type="date" id="dl2date" class="input"></label>`, [{ label: "Cancel" }, { label: "Submit check", primary: true, run: () => {
      const v = (document.getElementById("dl2date") || {}).value;
      if (!v) { A.toast("Enter the date you computed", "bad"); return false; }
      const cur = A.store.get(DL, d.id);
      if (v === cur.due) {
        A.store.put(DL, { id: d.id, status: "second-checked", mismatch: false, secondCheck: { by: me().id, at: A.now(), entered: v }, history: hist(cur, "Blind second check matched") });
        A.audit("deadline.second-checked", { target: d.id, note: cur.title }); A.toast("Second check matched", "ok");
      } else {
        A.store.put(DL, { id: d.id, mismatch: true, history: hist(cur, `Blind second check mismatch (entered ${v})`) });
        A.audit("deadline.check-mismatch", { target: d.id, note: `${cur.title}: entered ${v}` }); A.toast("Dates differ. Blocked until resolved by the responsible lawyer.", "bad");
      }
    } }]);
  });
  A.action("deadlines-met", (el) => {
    const d = A.store.get(DL, el.dataset.id); if (!d || !canEdit() || needsSecond(d)) return A.toast("Not allowed yet", "bad");
    A.modal("Mark met", h`<label class="field">Proof (filing, service record, sent letter)<input id="dlproof" class="input" placeholder="e.g. Defence served by email 2026-10-12"></label>`, [{ label: "Cancel" }, { label: "Mark met", primary: true, run: () => {
      const v = document.getElementById("dlproof").value.trim(); if (!v) { A.toast("Record the proof", "bad"); return false; }
      A.store.put(DL, { id: d.id, status: "met", metNote: v, history: hist(d, `Met: ${v}`) }); A.audit("deadline.met", { target: d.id, note: v });
    } }]);
  });
  A.action("deadlines-release", (el) => {
    const d = A.store.get(DL, el.dataset.id); if (!d) return; if (!A.can("deadlines.verify", "approve")) return A.toast("Only a lawyer can release a deadline", "bad");
    A.modal("Release deadline", h`<p>Deadlines are never deleted. Releasing keeps the record and stops reminders.</p><label class="field">Reason<textarea id="dlrel" rows="3" class="input"></textarea></label>`, [{ label: "Cancel" }, { label: "Release", primary: true, run: () => {
      const v = document.getElementById("dlrel").value.trim(); if (!v) { A.toast("A reason is required", "bad"); return false; }
      A.store.put(DL, { id: d.id, status: "released", releaseReason: v, history: hist(d, `Released: ${v}`) }); A.audit("deadline.released", { target: d.id, note: v });
      const g = pendingGate(d); if (g) A.store.put("approvals", { id: g.id, status: "returned", decidedBy: me().id, decidedAt: A.now(), note: "Deadline released" });
    } }]);
  });

  A.module({ id: "deadlines", label: "Deadlines", icon: "◷", order: 30, cap: "deadlines.view", badge: unconfirmedCritical,
    smoke: ["deadlines", "deadlines/dl-1", "deadlines/dl-3", "deadlines/dl-5", "deadlines/dl-10", "deadlines/calendar", "deadlines/event/ev-1", "deadlines/event/new", "deadlines/event/missing",
      "deadlines/settings", "deadlines/settings/reminders", "deadlines/settings/packs", "deadlines/settings/holidays", "deadlines/settings/calendars", "deadlines/settings/sync"],
    routes: { deadlines: deadlinesPage, "deadlines/calendar": calendarPage, "deadlines/settings": (el) => dlSettingsPage(el, { tab: "defaults" }), "deadlines/settings/:tab": dlSettingsPage, "deadlines/event/:id": eventPage, "deadlines/:id": deadlineDetail } });

  A.matterTab({ id: "deadlines", label: "Deadlines", cap: "deadlines.view", order: 30, render(el, m) {
    const list = A.store.where(DL, (d) => d.matterId === m.id).sort((a, b) => a.due.localeCompare(b.due));
    A.render(el, h`<div class="row wrap" style="margin-bottom:10px"><span class="grow muted small">${list.filter(open).length} open · ${list.filter((d) => d.status === "proposed").length} awaiting confirmation</span>
      <button class="btn agent" data-a="deadlines-extract" data-id="${m.id}"${dis(canEdit(), "You cannot add deadlines")}>✦ Extract deadlines</button></div>
      ${list.length ? list.map((d) => dlRow(d, false)) : A.ui.empty("No deadlines yet", " Let the agent read this matter's documents.")}`);
  } });

  /* ======================================================================
     WORK
     ====================================================================== */
  const WK = "workItems";
  /** Workflow statuses in board order (editable in Work settings; the five system statuses stay). */
  const COLS = () => ((A.store.get("workflow", "default") || {}).statuses || []).map((s) => s.id);
  const SYS = { "To do": ["todo"], "Agent-ready": ["suggested", "Agent-ready"], "In progress": ["in-progress"], "Needs approval": ["needs-approval"], Done: ["done"] };
  const wkChip = (w) => (w.running ? A.ui.status("running") : SYS[w.status] ? A.ui.status(SYS[w.status][0], SYS[w.status][1]) : A.ui.status("open", w.status));
  const W = (n, matterId, title, status, assignee, due, priority = "Normal", extra = {}) => ({ id: `wk-${n}`, key: `WRK-${n}`, matterId, title, status, assignee, due, priority, comments: [], history: [{ at: "2026-10-08 09:00", by: "u-linda", what: "Created" }], ...extra });
  A.seed(WK, [
    W(101, "m1", "Draft statement of defence reply strategy memo", "In progress", "u-ayo", "2026-10-12", "High"),
    W(102, "m1", "Draft reply to D. Osei on extension and document request", "To do", "u-linda", "2026-10-12", "High"),
    W(103, "m1", "Gather storage agreement and inventory invoices from client", "To do", "u-meraf", "2026-10-14"),
    W(104, "m1", "Summarize the statement of claim for the client", "Needs approval", "agent", "2026-10-10", "Normal", { output: "<p><b>Summary for Hanna Tesfaye.</b> Your claim seeks $86,400 for 140 cartons of textiles destroyed when water entered unit 214 on or about June 3, 2026. Northgate had notice of leaks from April 2026. Northgate was served on September 23, 2026; its defence is due October 13, 2026.</p>", cites: [{ doc: "d-claim", page: 1, line: 4 }, { doc: "d-aos", page: 1, line: 3 }] }),
    W(105, "m2", "Title search review", "In progress", "u-kaleab", "2026-10-14", "High"),
    W(106, "m2", "Draft requisition letter to seller's lawyer", "Agent-ready", "agent", "2026-10-15", "High"),
    W(107, "m2", "Review mortgage instructions", "To do", "u-yoni", "2026-10-20"),
    W(108, "m3", "Request physiotherapy records", "To do", "u-meraf", "2026-10-16"),
    W(109, "m3", "Send notice letter to Lakeshore Transit", "Done", "u-ayo", "2026-10-07"),
    W(110, "m4", "Prepare application for certificate of appointment", "In progress", "u-meraf", "2026-10-23"),
    W(111, "m5", "Compute reasonable notice range (Bardal factors)", "To do", "agent", "2026-10-15"),
    W(112, "m5", "Draft demand letter follow-up", "To do", "u-ayo", "2026-10-21", "Low"),
    W(113, "m6", "Mark up lease renewal: abatement, relocation, guarantee", "In progress", "u-yoni", "2026-10-19", "High"),
    W(114, "m7", "Prepare undertakings chart", "Done", "u-linda", "2026-10-07"),
  ]);
  const visWk = () => { const ids = new Set(A.matters(null, "work.view").map((m) => m.id)); return A.store.where(WK, (w) => ids.has(w.matterId)); };
  const canWork = (w) => A.can("work.edit", "all") || (A.can("work.edit", "act") && (w.assignee === me().id || ((A.store.get("matters", w.matterId) || {}).team || []).includes(me().id)));
  const wGate = (w) => A.store.where("approvals", (g) => g.effect === "work-approve" && g.payload && g.payload.id === w.id).sort((a, b) => (b.created || "").localeCompare(a.created || ""))[0];
  const PRI = { High: "bad", Normal: "", Low: "" };

  function agentOutput(w) {
    const m = A.store.get("matters", w.matterId) || {}; const docs = A.store.where("docs", (d) => d.matterId === w.matterId);
    const cites = docs.slice(0, 2).map((d) => ({ doc: d.id, page: 1, line: Math.min(4, d.pages[0].length) }));
    if (/requisition/i.test(w.title)) return { body: `<p>Re: ${esc(m.name)}. On behalf of the buyers we submit the following requisitions, without prejudice to further requisitions: (1) produce a discharge of every registered mortgage on or before closing; (2) confirm there are no outstanding work orders or deficiency notices, as warranted in the agreement; (3) deliver keys and vacant possession on October 30, 2026.</p>`, cites: [{ doc: "d-aps", page: 1, line: 6 }, { doc: "d-aps", page: 2, line: 3 }] };
    if (/notice|bardal/i.test(w.title)) return { body: "<p>Bardal factors: age (unknown, ask client), length of service 6 years 1 month (start July 12, 2020), supervisory role, re-employment market for warehouse supervisors. Comparable Ontario awards suggest 6 to 9 months' notice, less the 2 weeks paid. Lawyer to verify case range.</p>", cites: [{ doc: "d-term", page: 1, line: 6 }, { doc: "d-term", page: 1, line: 5 }] };
    return { body: `<p>Draft for ${esc(w.title.toLowerCase())} on ${esc(m.number || "")} ${esc(m.name || "")}, prepared from ${docs.length} document${docs.length === 1 ? "" : "s"} on file. Review the cited lines before approving.</p>`, cites };
  }
  function setStatus(w, status, note) {
    const cur = A.store.get(WK, w.id);
    A.store.put(WK, { id: w.id, status, history: hist(cur, note || `Moved to ${status}`) });
    A.audit("work.moved", { target: w.key, note: `${status}` });
  }
  function toAgent(w) {
    if (!canWork(w)) return A.toast("You cannot change this item", "bad");
    const cur = A.store.get(WK, w.id); if (cur.running) return;
    A.store.put(WK, { id: w.id, status: "Agent-ready", assignee: "agent", running: true, history: hist(cur, "Handed to the agent") });
    runAgent(w.id);
  }
  function runAgent(id) { A.agent.run(agentTask(id)); }
  /** The agent task for a work item; fromChat marks the item as handed over when the run starts. */
  function agentTask(id, fromChat) {
    const w = A.store.get(WK, id);
    return { title: `${w.key}: ${w.title}`, matterId: w.matterId, steps: ["Reading the matter file", "Drafting", "Checking citations"],
      done() {
        const cur = A.store.get(WK, id); if (!cur) return;
        const out = agentOutput(cur);
        A.store.put(WK, { id, status: "Needs approval", running: false, output: out.body, cites: out.cites, history: hist(cur, "Agent finished; output sent for approval").map((x, i, a) => (i === a.length - 1 ? { ...x, by: "agent" } : x)) });
        const g = A.gate({ kind: "work", cap: "work.approve", title: `${cur.key}: ${cur.title}`, summary: "Agent output. Approve to mark done; send back to revise.", matterId: cur.matterId, preparedBy: "agent",
          body: out.body, cites: out.cites.map(cite), checks: [{ label: "Every claim cites a source", ok: out.cites.length > 0 }], effect: "work-approve", payload: { id } });
        return { title: `Ready for approval: ${g.title}`, cites: out.cites.map(cite), outputs: [{ label: "Review", go: `approvals/${g.id}` }] };
      },
      onStart: fromChat ? () => { const cur = A.store.get(WK, id); if (cur) A.store.put(WK, { id, status: "Agent-ready", assignee: "agent", running: true, history: hist(cur, "Handed to the agent from chat") }); } : undefined };
  }
  A.effect("work-approve", (p) => { const w = A.store.get(WK, p.id); if (w) { A.store.put(WK, { id: p.id, status: "Done", approvedBy: me().id, history: hist(w, "Output approved; done") }); fire("work.approved", A.store.get(WK, p.id)); } });
  function requestApproval(w) {
    if (!canWork(w)) return A.toast("You cannot change this item", "bad");
    A.gate({ kind: "work", cap: "work.approve", title: `${w.key}: ${w.title}`, summary: `Prepared by ${me().name}.`, matterId: w.matterId, preparedBy: me().id, body: w.output || "", cites: (w.cites || []).map(cite), effect: "work-approve", payload: { id: w.id } });
    setStatus(w, "Needs approval", "Sent for approval");
  }

  function moveButtons(w) {
    const ok = canWork(w), why = "You cannot change this item";
    const b = (label, to, agent) => A.ui.btn({ label, a: "work-move", id: w.id, small: true, agent, disabled: !ok, why, data: { to } });
    if (w.status === "To do") return h`${b("Start", "In progress")}${b("✦ Hand to agent", "Agent-ready", true)}`;
    if (w.status === "Agent-ready") return w.running ? A.ui.actor("agent", "Working…") : b("✦ Run agent", "Agent-ready", true);
    if (w.status === "In progress") return h`${b("Back", "To do")}${b("Request approval", "Needs approval")}${w.assignee === "agent" ? "" : b("Done", "Done")}`;
    if (w.status === "Needs approval") return A.ui.btn({ label: "Review", a: "work-open", id: w.id, small: true });
    if (w.status !== "Done") return h`${b("Back to in progress", "In progress")}`;
    return "";
  }
  const wCard = (w, showMatter = true) => h`<div class="kcard ${w.assignee === "agent" ? "agent" : ""} ${w.status === "Needs approval" ? "gate" : ""}" data-a="work-open" data-id="${w.id}">
    <div class="row"><small class="grow">${w.key}${showMatter ? ` · ${(A.store.get("matters", w.matterId) || {}).number}` : ""}</small>${A.ui.avatar(w.assignee)}</div>
    <div>${w.title}</div>${w.flag ? h`<div>${A.ui.status("blocked", "Assignee lacks access")}</div>` : ""}<small>Due ${fmt(w.due)}${w.priority !== "Normal" ? ` · ${w.priority}` : ""} · ${catName(catOf(w))}</small>
    <div class="row wrap" style="margin-top:6px">${moveButtons(w)}</div></div>`;
  const board = (list, showMatter = true) => { const wf = A.store.get("workflow", "default") || { statuses: [] };
    return h`<div class="board">${COLS().map((c) => { const xs = list.filter((w) => w.status === c); const st = wf.statuses.find((s) => s.id === c) || {}; const over = st.wip && xs.length > st.wip;
      return h`<div class="lane"><h4><span>${c}</span><span>${xs.length}${st.wip ? `/${st.wip}` : ""}</span></h4>${over ? A.ui.status("blocked", `Over WIP limit ${st.wip}`) : ""}${xs.map((w) => wCard(w, showMatter))}${showMatter && (c === "To do" || c === "Agent-ready") ? A.ui.btn({ label: "+ Add", a: "work-new", small: true, disabled: !A.can("work.edit", "act"), why: "You cannot add work", data: { status: c } }) : ""}</div>`; })}</div>`; };
  let wkView = "mine", wkQ = "", wkMatter = "", wkWho = "", wkPri = "", wkCat = "", wkCur = null, wkOrder = [];
  A.onUser(() => { wkCur = null; });
  A.change("work-q", (el) => { wkQ = el.value; A.refresh(); });
  const wkOverdue = (w) => w.status !== "Done" && w.due < T();
  const pendingWGate = (w) => { const g = wGate(w); return g && g.status === "pending" ? g : null; };
  function wkFilter(list) {
    const q = wkQ.trim().toLowerCase();
    return list.filter((w) => (!wkMatter || w.matterId === wkMatter) && (!wkPri || w.priority === wkPri) && (!wkCat || catOf(w) === wkCat)
      && (!wkWho || (wkWho === "me" ? w.assignee === me().id : w.assignee === wkWho))
      && (!q || `${w.key} ${w.title} ${mName(w.matterId)}`.toLowerCase().includes(q)));
  }
  /** Who must act next, in plain words. */
  const wNext = (w) => { const g = pendingWGate(w); if (g) return A.canApprove(g).ok ? "Your approval" : "Waiting for approval"; if (w.running) return "✦ Agent working"; if (w.flag) return "Reassign: no access"; if (w.status === "Agent-ready") return "Ready to run"; return ""; };
  const mentionsMe = (w) => (w.comments || []).some((c) => (c.mentions || []).includes(me().id));
  const watching = (w) => (w.watchers || []).includes(me().id);
  const whyMine = (w) => (w.assignee === me().id ? "" : mentionsMe(w) ? "Mentions you" : watching(w) ? "Watching" : "");
  const dueText = (w) => { const n = A.daysBetween(T(), w.due); return w.status === "Done" ? `Due ${fmt(w.due)}` : n < 0 ? `${-n} days overdue` : n === 0 ? "Due today" : `Due ${fmt(w.due)}`; };
  const wListRow = (w) => { const nx = wNext(w) || whyMine(w);
    return { id: w.id, on: w.id === wkCur, title: w.title, sub: `${w.key} · ${mName(w.matterId)} · ${dueText(w)}${nx ? ` · ${nx}` : ""}`,
      right: h`${wkChip(w)}${w.priority === "High" ? A.ui.status("high", "High") : ""}${A.ui.avatar(w.assignee)}` }; };
  function wPane(w) {
    if (!w) return null;
    const g = pendingWGate(w);
    return h`<div class="card"><div class="row wrap" style="align-items:flex-start"><div class="grow"><div class="small muted">${w.key} · ${mName(w.matterId)}</div><h3 style="margin:2px 0 6px">${w.title}</h3></div>${wkChip(w)}</div>
      <div class="row wrap" style="gap:8px">${w.assignee === "agent" || w.output ? A.ui.actor("agent", w.running ? "Agent working" : "Agent prepared") : ""}${g ? A.ui.actor("person", "Approval by someone other than the preparer") : ""}</div>
      <dl class="kv"><dt>Assignee</dt><dd>${w.assignee === "agent" ? "✦ Agent" : A.user(w.assignee).name}</dd><dt>Due</dt><dd>${fmt(w.due)} ${w.status === "Done" ? "" : A.ui.due(w.due)}</dd><dt>Priority</dt><dd>${w.priority}</dd><dt>Category</dt><dd>${catName(catOf(w))}</dd>${w.flag ? h`<dt>Flag</dt><dd>${w.flag}</dd>` : ""}
        ${(w.watchers || []).length ? h`<dt>Watchers</dt><dd>${w.watchers.map((u) => A.user(u).name).join(", ")}</dd>` : ""}</dl>
      <div class="row wrap" style="margin-top:10px">${moveButtons(w)}</div>
      ${g ? h`<div style="margin-top:10px">${A.ui.gateCard(g)}</div>` : w.output ? h`<div class="draft">${A.raw(w.output)}</div>` : ""}
      <div class="row" style="margin-top:8px"><span class="grow"></span>${A.ui.btn({ label: "Open item", a: "work-open", id: w.id, small: true, title: "Open item (Enter)" })}</div></div>`;
  }
  const split = (rows, empty) => { wkOrder = rows.map((w) => w.id); if (!wkOrder.includes(wkCur)) wkCur = wkOrder[0] || null;
    return A.ui.split(rows.length ? A.ui.list(rows.map(wListRow), "work-sel", { select: SEL_WK }) : A.ui.empty(empty[0], empty[1]), wPane(wkCur && A.store.get(WK, wkCur)), { emptyTitle: "Pick an item" }); };
  const wCols = [{ label: "Key", key: "key" }, { label: "Title", key: "title" }, { label: "Matter", render: (w) => (A.store.get("matters", w.matterId) || {}).number }, { label: "Category", render: (w) => catName(catOf(w)) },
    { label: "Status", render: wkChip }, { label: "Assignee", render: (w) => (w.assignee === "agent" ? "✦ Agent" : A.user(w.assignee).name) }, { label: "Due", render: (w) => h`${fmt(w.due)} ${w.status === "Done" ? "" : A.ui.due(w.due)}` },
    { label: "Priority", key: "priority" }, { label: "Est. h", num: true, render: (w) => estOf(w) }];
  function workPage(el) {
    A.setContext({ kind: "work", label: "Work" });
    const all = visWk(), list = wkFilter(all), openL = list.filter((w) => w.status !== "Done");
    const apprMine = (w) => (pendingWGate(w) && A.canApprove(pendingWGate(w)).ok ? 1 : 0);
    const mine = openL.filter((w) => w.assignee === me().id || apprMine(w) || mentionsMe(w) || watching(w)).sort((a, b) => apprMine(b) - apprMine(a) || a.due.localeCompare(b.due) || (b.priority === "High") - (a.priority === "High"));
    const queue = list.filter((w) => pendingWGate(w)).sort((a, b) => a.due.localeCompare(b.due));
    let body; wkOrder = [];
    if (wkView === "auto") body = automationsView();
    else if (wkView === "board") body = board(list);
    else if (wkView === "load") body = workload(all);
    else if (wkView === "list") body = A.ui.table(wCols, list.slice().sort((a, b) => (a.status === "Done") - (b.status === "Done") || a.due.localeCompare(b.due)), { onRow: "work-open", select: SEL_WK, active: wkCur, empty: "No work items match", emptyHint: " Clear the filters or press n to add one." });
    else if (wkView === "approve") body = split(queue, ["Nothing waiting for approval", " Agent output lands here before it counts as done."]);
    else body = split(mine, ["Nothing needs you", " Items assigned to you, outputs you can approve, mentions and watched items show here, most urgent first."]);
    const picked = pickedW(), canAppr = picked.filter((w) => pendingWGate(w) && A.canApprove(pendingWGate(w)).ok).length;
    const matters = A.matters(null, "work.view"), plain = ["auto", "load"].includes(wkView), sf = visFilters();
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Work", sub: "The agent does the drafting; a person approves its output before it counts as done.",
        actions: [{ label: "Reports", to: "work/reports" }, { label: "Settings", to: "work/settings" }],
        primary: { label: "New item", a: "work-new", key: "n", disabled: !A.can("work.edit", "act"), why: "You cannot add work" } })}
      <div class="stats">${A.ui.stat("Open", openL.length)}${A.ui.stat("Overdue", openL.filter(wkOverdue).length, "", openL.filter(wkOverdue).length ? "bad" : "")}${A.ui.stat("With the agent", openL.filter((w) => w.assignee === "agent").length)}${A.ui.stat("Needs approval", queue.length, "", queue.length ? "gate" : "")}</div>
      ${A.ui.filters({ chips: [{ id: "mine", label: "My work", count: mine.length }, { id: "approve", label: "Needs approval", count: queue.length }, { id: "board", label: "Board" }, { id: "list", label: "List" }, { id: "load", label: "Workload and leave" }, { id: "auto", label: "Automations", count: all.filter((w) => w.flag && w.status !== "Done").length }], active: wkView, action: "work-view",
        search: plain ? null : { c: "work-q", value: wkQ, placeholder: "Search items, keys, matters" },
        selects: plain ? [] : [{ c: "work-f-matter", value: wkMatter, label: "Matter", options: [["", "All matters"], ...matters.map((m) => [m.id, `${m.number} ${m.name}`])] },
          { c: "work-f-who", value: wkWho, label: "Assignee", options: [["", "Anyone"], ["me", "Assigned to me"], ["agent", "The agent"], ...staff().map((u) => [u.id, u.name])] },
          { c: "work-f-pri", value: wkPri, label: "Priority", options: [["", "Any priority"], ["High", "High"], ["Normal", "Normal"], ["Low", "Low"]] },
          { c: "work-f-cat", value: wkCat, label: "Category", options: [["", "Any category"], ...A.store.all("workCats").map((c) => [c.id, c.name])] },
          { c: "work-f-saved", value: "", label: "Saved filter", options: [["", sf.length ? "Saved filters…" : "No saved filters"], ...sf.map((f) => [f.id, `${f.name}${f.shared ? " (shared)" : ""}`])] }],
        right: plain ? "" : h`${wkQ || wkMatter || wkWho || wkPri || wkCat ? h`${A.ui.btn({ label: "Save filter", a: "work-filter-save", small: true })}${A.ui.btn({ label: "Clear", a: "work-f-clear", small: true })}` : ""}${A.ui.btn({ label: "Export CSV", a: "work-csv", small: true, title: "Export the items in view" })}` })}
      ${["board", "auto", "load"].includes(wkView) ? "" : A.ui.bulkBar(SEL_WK, [{ label: `Approve ${canAppr}`, a: "work-bulk-approve", primary: true, disabled: !canAppr, why: "None of the selected outputs are yours to approve" },
        { label: "Assign, priority, due…", a: "work-bulk-edit", disabled: !A.can("work.edit", "act"), why: "You cannot change work" }, { label: "Mark done", a: "work-bulk-done", disabled: !A.can("work.edit", "act"), why: "You cannot change work" },
        { label: "Export CSV", a: "work-csv" }, { label: "Delete…", a: "work-bulk-delete", danger: true, disabled: !A.can("work.edit", "all"), why: "Only lawyers can delete work items" }])}
      ${body}
    </div>`);
  }
  A.change("work-f-matter", (el) => { wkMatter = el.value; A.refresh(); });
  A.change("work-f-who", (el) => { wkWho = el.value; A.refresh(); });
  A.change("work-f-pri", (el) => { wkPri = el.value; A.refresh(); });
  A.change("work-f-cat", (el) => { wkCat = el.value; A.refresh(); });
  A.change("work-f-saved", (el) => { const f = A.store.get("savedFilters", el.value); if (!f) return; ({ q: wkQ = "", matter: wkMatter = "", who: wkWho = "", pri: wkPri = "", cat: wkCat = "" } = f.f || {}); if (f.view) wkView = f.view; A.toast(`Filter: ${f.name}`); A.refresh(); });
  A.action("work-f-clear", () => { wkQ = ""; wkMatter = ""; wkWho = ""; wkPri = ""; wkCat = ""; A.refresh(); });
  A.action("work-sel", (el) => { if (narrow()) return A.go(`work/${el.dataset.id}`); wkCur = el.dataset.id; A.refresh(); });
  A.action("work-csv", (el) => {
    const ids = el.dataset.ids ? el.dataset.ids.split(",") : null;
    const rows = ids ? ids.map((id) => A.store.get(WK, id)).filter(Boolean) : wkFilter(visWk());
    csv("work-items", [["Key", (w) => w.key], ["Title", (w) => w.title], ["Matter", (w) => mName(w.matterId)], ["Category", (w) => catName(catOf(w))], ["Status", (w) => w.status], ["Assignee", (w) => (w.assignee === "agent" ? "Agent" : A.user(w.assignee).name)], ["Due", (w) => w.due], ["Priority", (w) => w.priority], ["Estimate hours", estOf]], rows);
  });
  const pickedW = (el) => selIds(SEL_WK, el).map((id) => A.store.get(WK, id)).filter(Boolean);
  function bulkApprove(el) {
    const xs = pickedW(el).map(pendingWGate).filter((g) => g && A.canApprove(g).ok);
    xs.forEach((g) => A.approve(g.id)); A.selection(SEL_WK).clear(); A.toast(`${xs.length} approved`, xs.length ? "ok" : "bad"); A.refresh();
  }
  A.action("work-bulk-approve", bulkApprove);
  A.action("work-bulk-done", (el) => {
    const xs = pickedW(el); let n = 0;
    xs.forEach((w) => { if (!canWork(w) || w.assignee === "agent" || w.status === "Done" || pendingWGate(w)) return; setStatus(w, "Done", "Marked done (bulk)"); n++; });
    A.selection(SEL_WK).clear(); A.toast(`${n} of ${xs.length} marked done${n < xs.length ? "; agent work and items awaiting approval are done only when approved" : ""}`, n ? "ok" : "bad");
  });
  A.action("work-bulk-delete", (el) => {
    if (!A.can("work.edit", "all")) return A.toast("Only lawyers can delete work items", "bad");
    const xs = pickedW(el); if (!xs.length) return;
    A.modal(`Delete ${xs.length} item${xs.length === 1 ? "" : "s"}`, h`<p>This removes ${xs.map((w) => w.key).join(", ")}. The audit log keeps a record. Pending approvals on them are withdrawn.</p>`, [{ label: "Cancel" }, { label: "Delete", primary: true, run: () => {
      xs.forEach((w) => deleteItem(w)); A.selection(SEL_WK).clear();
    } }]);
  });
  function deleteItem(w) {
    const g = pendingWGate(w); if (g) A.store.put("approvals", { id: g.id, status: "returned", decidedBy: me().id, decidedAt: A.now(), note: "Work item deleted" });
    A.store.remove(WK, w.id); A.audit("work.deleted", { target: w.key, note: w.title });
  }
  const staff = () => A.store.where("users", (u) => u.role !== "client" && A.grant("work.view", u.role) !== "none");
  /** Apply field changes to an item; refuses to assign someone who cannot see the matter. */
  function setFields(w, v, how) {
    const m = A.store.get("matters", w.matterId); const ch = {};
    if (v.assignee && v.assignee !== w.assignee) { if (v.assignee !== "agent" && !userSees(v.assignee, m)) { A.toast(`${A.user(v.assignee).name} cannot see ${m.number}; not assigned`, "bad"); } else { ch.assignee = v.assignee; ch.flag = undefined; } }
    if (v.priority && v.priority !== w.priority) ch.priority = v.priority;
    if (v.due && v.due !== w.due) ch.due = v.due;
    if (v.category && v.category !== catOf(w)) ch.category = v.category;
    if (v.status && v.status !== w.status && freeStatus(v.status) && freeStatus(w.status)) ch.status = v.status;
    if (v.title && v.title.trim() && v.title.trim() !== w.title) ch.title = v.title.trim();
    if (v.description !== undefined && v.description !== (w.description || "")) ch.description = v.description;
    const keys = Object.keys(ch); if (!keys.length) return false;
    const what = keys.map((k) => (k === "assignee" ? `assignee ${ch.assignee === "agent" ? "the agent" : A.user(ch.assignee).name}` : k === "flag" ? "" : k === "category" ? `category ${catName(ch.category)}` : `${k} ${k === "description" ? "edited" : ch[k]}`)).filter(Boolean).join(", ");
    A.store.put(WK, { id: w.id, ...ch, history: hist(A.store.get(WK, w.id), `${how || "Changed"}: ${what}`) }); A.audit("work.edited", { target: w.key, note: what });
    return true;
  }
  const fieldForm = (w, bulk) => h`<div id="wkform" class="form">
    ${bulk ? "" : h`<label class="field" style="grid-column:1/-1">Title<input name="title" class="input" value="${w ? w.title : ""}" placeholder="e.g. Draft requisition letter"></label>`}
    ${bulk || w ? "" : h`<label class="field">Matter<select name="matter" class="input">${opts(A.matters(null, "work.view").map((m) => [m.id, `${m.number} ${m.name}`]), wkMatter || (A.context().kind === "matter" ? A.context().id : ""))}</select></label>`}
    <label class="field">Assignee<select name="assignee" class="input">${bulk ? h`<option value="">Keep</option>` : ""}${opts([["agent", "✦ Agent"], ...staff().map((u) => [u.id, u.name])], bulk ? "" : w ? w.assignee : me().id)}</select></label>
    <label class="field">Priority<select name="priority" class="input">${bulk ? h`<option value="">Keep</option>` : ""}${opts([["High", "High"], ["Normal", "Normal"], ["Low", "Low"]], bulk ? "" : w ? w.priority : "Normal")}</select></label>
    <label class="field">Category<select name="category" class="input">${bulk ? h`<option value="">Keep</option>` : ""}${opts(A.store.all("workCats").map((c) => [c.id, c.name]), bulk ? "" : w ? catOf(w) : "drafting")}</select></label>
    ${w && !bulk && freeStatus(w.status) ? h`<label class="field">Status<select name="status" class="input">${opts(COLS().filter(freeStatus).map((s) => [s, s]), w.status)}</select></label>` : ""}
    <label class="field">Due<input type="date" name="due" class="input" value="${bulk ? "" : w ? w.due : A.addDays(T(), 3)}"></label>
    ${bulk ? "" : h`<label class="field" style="grid-column:1/-1">Details<textarea name="description" rows="3" class="input">${w ? w.description || "" : ""}</textarea></label>`}</div>`;
  A.action("work-bulk-edit", (el) => {
    const xs = pickedW(el).filter(canWork); if (!xs.length) return A.toast("None of the selected items are yours to change", "bad");
    A.modal(`Change ${xs.length} item${xs.length === 1 ? "" : "s"}`, fieldForm(null, true), [{ label: "Cancel" }, { label: "Apply", primary: true, run: () => {
      const v = A.formValues(document.getElementById("wkform")); let n = 0; xs.forEach((w) => { if (setFields(w, v, "Bulk change")) n++; }); A.selection(SEL_WK).clear(); A.toast(`${n} changed`, n ? "ok" : "");
    } }]);
  });
  function newItem(status) {
    if (!A.can("work.edit", "act")) return A.toast("You cannot add work", "bad");
    if (!A.matters(null, "work.view").length) return A.toast("No matters you can add work to", "bad");
    A.modal("New work item", fieldForm(null, false), [{ label: "Cancel" }, { label: "Create", primary: true, run: () => {
      const v = A.formValues(document.getElementById("wkform")); const m = A.matter(v.matter);
      if (!v.title.trim()) { A.toast("Give it a title", "bad"); return false; }
      if (!m) { A.toast("Pick a matter", "bad"); return false; }
      if (v.assignee !== "agent" && !userSees(v.assignee, m)) { A.toast(`${A.user(v.assignee).name} cannot see ${m.number}`, "bad"); return false; }
      const w = A.store.put(WK, { id: A.id("wk"), key: nextKey(), matterId: m.id, title: v.title.trim(), description: v.description, status: status || "To do", assignee: v.assignee, due: v.due || A.addDays(T(), 3), priority: v.priority, category: v.category, watchers: [me().id], comments: [], history: [{ at: A.now(), by: me().id, what: "Created" }] });
      A.audit("work.created", { target: w.key, note: w.title }); wkCur = w.id; A.toast(`${w.key} created`, "ok");
    } }]);
  }
  A.action("work-new", (el) => newItem(el.dataset.status));
  function editItem(w) {
    if (!canWork(w)) return A.toast("You cannot change this item", "bad");
    A.modal(`Edit ${w.key}`, fieldForm(w, false), [{ label: "Cancel" }, { label: "Save", primary: true, run: () => { setFields(w, A.formValues(document.getElementById("wkform")), "Edited"); } }]);
  }
  A.action("work-edit", (el) => { const w = A.store.get(WK, el.dataset.id); if (w) editItem(w); });
  A.action("work-delete", (el) => {
    const w = A.store.get(WK, el.dataset.id); if (!w) return; if (!A.can("work.edit", "all")) return A.toast("Only lawyers can delete work items", "bad");
    A.modal(`Delete ${w.key}`, h`<p>Remove “${w.title}”? The audit log keeps a record.</p>`, [{ label: "Cancel" }, { label: "Delete", primary: true, run: () => { deleteItem(w); A.go("work"); } }]);
  });
  A.action("work-stop", (el) => { const w = A.store.get(WK, el.dataset.id); const r = w && liveRun(w); if (!r) return; A.agent.stop(r.id); A.store.put(WK, { id: w.id, running: false, status: "To do", history: hist(w, "Agent run stopped") }); });
  const liveRun = (w) => A.store.where("runs", (r) => (r.title || "").startsWith(`${w.key}:`) && ["running", "waiting"].includes(r.status))[0];

  /* ---------- work keys ---------- */
  const wkTarget = (r) => (/^work\/./.test(r) ? r.split("/")[1] : wkCur);
  Object.assign(KEYS.work, {
    j: (e, r) => { if (r !== "work" || !wkOrder.length) return false; wkCur = move(wkOrder, wkCur, 1); A.refresh(); scrollSel(); },
    k: (e, r) => { if (r !== "work" || !wkOrder.length) return false; wkCur = move(wkOrder, wkCur, -1); A.refresh(); scrollSel(); },
    Enter: (e, r) => { if (r !== "work" || !wkCur || !wkOrder.length) return false; A.go(`work/${wkCur}`); },
    x: (e, r) => { if (r !== "work" || !wkCur || !wkOrder.length) return false; A.selection(SEL_WK).toggle(wkCur); A.refresh(); },
    a: (e, r) => { if (r === "work" && A.selection(SEL_WK).size) return bulkApprove(); const w = A.store.get(WK, wkTarget(r) || ""); const g = w && pendingWGate(w); if (!g) return false; A.approve(g.id); },
    r: (e, r) => { const w = A.store.get(WK, wkTarget(r) || ""); if (!w || w.status === "Done" || w.status === "Needs approval") return false; toAgent(w); },
    d: (e, r) => { const w = A.store.get(WK, wkTarget(r) || ""); if (!w) return false; if (!canWork(w) || w.assignee === "agent" || pendingWGate(w)) { A.toast("Agent work and items awaiting approval are done only when approved", "bad"); return; } setStatus(w, "Done"); },
    e: (e, r) => { const w = A.store.get(WK, wkTarget(r) || ""); if (!w) return false; editItem(w); },
    w: (e, r) => { const w = A.store.get(WK, wkTarget(r) || ""); if (!w) return false; toggleWatch(w); },
    n: () => newItem(),
    Escape: (e, r) => { if (r !== "work") return A.go("work"); if (!A.selection(SEL_WK).size) return false; A.selection(SEL_WK).clear(); A.refresh(); },
  });
  registerKeys();
  /** Render comment text with @mentions in bold (text is escaped by h). */
  const commentText = (c) => h`${String(c.text).split(/(@[A-Za-z]+)/).map((p) => (/^@[A-Za-z]+$/.test(p) && mentionUser(p.slice(1)) ? h`<b>${p}</b>` : p))}`;
  function workDetail(el, p) {
    const w = A.store.get(WK, p.id); const m = w && A.matter(w.matterId);
    if (!w || !m || !A.canMatter(m, "work.view")) return A.render(el, h`<div class="page">${A.ui.header({ title: "Not available", back: { label: "Work", to: "work" } })}<div class="locked"><b>Not available</b>This item does not exist or you cannot see its matter.</div></div>`);
    A.setContext({ kind: "work-item", id: w.id, label: `${w.key} ${w.title}` });
    const g = wGate(w), watch = watching(w);
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: `${w.key} ${w.title}`, status: wkChip(w), back: { label: "Work", to: "work" }, meta: h`<span class="muted small"> · <a href="#" data-a="go" data-to="matters/${m.id}">${m.number} ${m.name}</a></span>`,
        actions: [{ label: watch ? "Stop watching" : "Watch", a: "work-watch", id: w.id, key: "w" }, { label: "Edit", a: "work-edit", id: w.id, key: "e", disabled: !canWork(w), why: "You cannot change this item" },
          { label: "Delete…", a: "work-delete", id: w.id, danger: true, disabled: !A.can("work.edit", "all"), why: "Only lawyers can delete work items" }] })}
      <div class="grid g2">
        <div class="card"><div class="row wrap" style="gap:8px;margin-bottom:6px">${w.assignee === "agent" || w.output ? A.ui.actor("agent", w.running ? "Agent working" : "Agent prepared") : ""}${pendingWGate(w) ? A.ui.actor("person", "Approval by someone other than the preparer") : ""}</div>
          <dl class="kv"><dt>Assignee</dt><dd>${w.assignee === "agent" ? "✦ Agent" : A.user(w.assignee).name}</dd><dt>Due</dt><dd>${fmt(w.due)} ${w.status === "Done" ? "" : A.ui.due(w.due)}</dd><dt>Priority</dt><dd>${w.priority}</dd>
          <dt>Category</dt><dd>${catName(catOf(w))} · estimate ${estOf(w)} h</dd>
          ${w.flag ? h`<dt>Flag</dt><dd>${w.flag}. Assign someone on the matter team.</dd>` : ""}${w.autoRule ? h`<dt>Created by</dt><dd><a href="#" data-a="go" data-to="work/automations/${w.autoRule}">Automation rule</a></dd>` : ""}
          ${w.approvedBy ? h`<dt>Approved by</dt><dd>${A.user(w.approvedBy).name}</dd>` : ""}<dt>Watchers</dt><dd>${(w.watchers || []).map((u) => A.user(u).name).join(", ") || "Nobody yet"}</dd>${w.external ? h`<dt>Tracker</dt><dd>${w.external} ${demo()}</dd>` : ""}</dl>
          ${w.description ? h`<p style="white-space:pre-wrap;margin:10px 0 0">${w.description}</p>` : ""}
          <div class="row wrap" style="margin-top:10px">${moveButtons(w)}${liveRun(w) ? A.ui.btn({ label: "Stop agent run", a: "work-stop", id: w.id, small: true, danger: true }) : ""}</div>
          ${w.output && !g ? h`<div class="draft">${A.raw(w.output)}</div>${(w.cites || []).length ? h`<div class="cites">${w.cites.map((c) => A.ui.cite(cite(c)))}</div>` : ""}` : ""}</div>
        <div>${g ? A.ui.gateCard(g) : A.ui.empty("No output yet", w.assignee === "agent" ? " Move it to Agent-ready and the agent will do the work." : "")}</div>
      </div>
      <div class="card" style="margin-top:14px"><h3>Comments</h3>
        ${(w.comments || []).map((c) => h`<div class="row" style="align-items:flex-start;margin-bottom:8px">${A.ui.avatar(c.by)}<div class="grow"><small class="muted">${A.user(c.by).name} · ${A.dateTime(c.at)}${(c.mentions || []).length ? ` · mentioned ${c.mentions.map((u) => A.user(u).name).join(", ")}` : ""}</small><div>${commentText(c)}</div></div></div>`)}
        <label class="field">Add a comment. Type @ and a first name to mention someone on the matter; they start watching.<textarea id="wkc" rows="2" class="input" placeholder="@Kaleab can you pull the parcel register?"></textarea></label>
        <div class="row" style="margin-top:6px"><span class="muted small">On this matter: ${(m.team || []).map((u) => `@${A.user(u).name.split(" ")[0]}`).join(" ")}</span><span class="grow"></span>${A.ui.btn({ label: "Comment", a: "work-comment", id: w.id, disabled: !A.can("work.view"), why: "No access" })}</div></div>
      <div class="card" style="margin-top:14px"><h3>History</h3>${A.ui.table([{ label: "When", render: (x) => A.dateTime(x.at) }, { label: "Who", render: (x) => (x.by === "agent" ? "✦ Agent" : A.user(x.by).name) }, { label: "What", key: "what" }], [...(w.history || [])].reverse())}</div>
    </div>`);
  }

  A.action("work-open", (el) => A.go(`work/${el.dataset.id}`));
  A.action("work-view", (el) => { wkView = el.dataset.id; A.refresh(); });
  A.action("work-move", (el) => {
    const w = A.store.get(WK, el.dataset.id); if (!w) return; if (!canWork(w)) return A.toast("You cannot change this item", "bad");
    const to = el.dataset.to;
    if (to === "Agent-ready") return toAgent(w);
    if (to === "Needs approval") return requestApproval(w);
    if (to === "Done" && w.assignee === "agent") return A.toast("Agent work is done only when approved", "bad");
    setStatus(w, to);
  });
  const mentionUser = (first) => A.store.where("users", (u) => u.role !== "client" && u.name.split(" ")[0].toLowerCase() === first.toLowerCase())[0];
  A.action("work-comment", (el) => {
    const t = document.getElementById("wkc"); const v = t ? t.value.trim() : ""; if (!v) return A.toast("Write a comment first");
    const w = A.store.get(WK, el.dataset.id); if (!w) return; const m = A.store.get("matters", w.matterId);
    const named = [...new Set((v.match(/@[A-Za-z]+/g) || []).map((x) => mentionUser(x.slice(1))).filter(Boolean).map((u) => u.id))];
    const blocked = named.filter((u) => !userSees(u, m)), mentions = named.filter((u) => userSees(u, m));
    A.store.put(WK, { id: w.id, comments: [...(w.comments || []), { by: me().id, at: A.now(), text: v, mentions }], watchers: [...new Set([...(w.watchers || []), me().id, ...mentions])] });
    if (mentions.length) A.audit("work.mentioned", { target: w.key, note: mentions.map((u) => A.user(u).name).join(", ") });
    if (blocked.length) A.toast(`${blocked.map((u) => A.user(u).name).join(", ")} cannot see ${m.number}; not notified`, "bad");
    else if (mentions.length) A.toast(`Mentioned ${mentions.map((u) => A.user(u).name).join(", ")}`, "ok");
  });
  function toggleWatch(w) {
    const on = (w.watchers || []).includes(me().id);
    A.store.put(WK, { id: w.id, watchers: on ? w.watchers.filter((u) => u !== me().id) : [...(w.watchers || []), me().id] });
    A.toast(on ? `Stopped watching ${w.key}` : `Watching ${w.key}: it shows in My work`);
  }
  A.action("work-watch", (el) => { const w = A.store.get(WK, el.dataset.id); if (w) toggleWatch(w); });
  /** Apply a practice-area template to a matter: items get due dates relative to the template's anchor date. */
  function applyTemplate(m, tp) {
    if (!m || !tp || !A.can("work.edit", "act")) return A.toast("You cannot add work here", "bad");
    if (A.store.where(WK, (w) => w.matterId === m.id && w.template === tp.id).length) return A.toast("Template already applied to this matter");
    const anchor = tp.anchor === "closing" ? (A.store.where(DL, (d) => d.matterId === m.id && /closing/i.test(d.title))[0] || {}).due || A.addDays(T(), 21) : T();
    const byRole = (role) => (m.team || []).find((u) => (A.store.get("users", u) || {}).role === role) || m.responsible;
    let n = Math.max(...A.store.all(WK).map((w) => +String(w.key).replace(/\D/g, "") || 0), 100);
    tp.items.forEach((it) => { n++; A.store.put(WK, { id: A.id("wk"), key: `WRK-${n}`, matterId: m.id, title: it.title, status: "To do", assignee: it.assignee === "agent" ? "agent" : it.assignee === "responsible" ? m.responsible : byRole(it.assignee),
      due: A.addDays(anchor, +it.offset || 0), priority: it.priority || "Normal", category: it.category, template: tp.id, comments: [], watchers: [me().id], history: [{ at: A.now(), by: me().id, what: `Created from template "${tp.name}" v${tp.version || 1}` }] }); });
    A.audit("work.template", { target: m.id, note: `${tp.name} v${tp.version || 1}` }); A.toast(`${tp.items.length} items added from ${tp.name}`, "ok");
  }
  A.action("work-template", (el) => { const sel = document.getElementById("wktp"); applyTemplate(A.matter(el.dataset.id), A.store.get("workTemplates", (sel && sel.value) || el.dataset.tp)); });

  A.module({ id: "work", label: "Work", icon: "▦", order: 35, cap: "work.view",
    smoke: ["work", "work/wk-104", "work/wk-106", "work/wk-111", "work/automations/au-1", "work/automations/au-2", "work/automations/au-3", "work/automations/missing",
      "work/reports", "work/settings", "work/settings/templates", "work/settings/categories", "work/settings/filters", "work/settings/sync"],
    routes: { work: workPage, "work/automations/:id": ruleDetail, "work/reports": workReports, "work/settings": (el) => wkSettingsPage(el, { tab: "workflow" }), "work/settings/:tab": wkSettingsPage, "work/:id": workDetail } });

  A.matterTab({ id: "work", label: "Work", cap: "work.view", order: 35, render(el, m) {
    const list = A.store.where(WK, (w) => w.matterId === m.id);
    const tps = A.store.where("workTemplates", (t) => t.status !== "agent-prepared").sort((a, b) => (b.area === m.area) - (a.area === m.area));
    A.render(el, h`<div class="row wrap" style="margin-bottom:10px"><span class="grow muted small">${list.filter((w) => w.status !== "Done").length} open</span>
      ${tps.length ? h`<select id="wktp" class="fsel" aria-label="Template">${opts(tps.map((t) => [t.id, `${t.name}${t.area === m.area ? " (this area)" : ""}`]), tps[0].id)}</select>${A.ui.btn({ label: "Apply template", a: "work-template", id: m.id, data: { tp: tps[0].id }, disabled: !A.can("work.edit", "act"), why: "You cannot add work" })}` : ""}</div>${board(list, false)}`);
  } });

  /* ======================================================================
     AUTOMATIONS (event -> action rules, with run history and dry run)
     ====================================================================== */
  const AU = "automations", AR = "autoRuns";
  const EVENTS = { "deadline.confirmed": "Deadline confirmed", "deadline.proposed": "Deadline proposed", "work.approved": "Agent work approved" };
  const WHEN = { any: "Any record", critical: "Critical dates only", limitation: "Limitation periods only" };
  const ACTS = { task: "Create a task", "agent-draft": "Agent drafts, waits for approval, then creates the task" };
  A.seed(AU, [
    { id: "au-1", name: "Prep task before every confirmed deadline", event: "deadline.confirmed", when: "any", action: "task", taskTitle: "Prepare for: {title}", offset: 7, assignee: "responsible", enabled: true, version: 2, by: "u-linda" },
    { id: "au-2", name: "Client reminder before a critical date", event: "deadline.confirmed", when: "critical", action: "agent-draft", taskTitle: "Client reminder: {title}", offset: 14, assignee: "agent", enabled: true, version: 1, by: "u-linda" },
    { id: "au-3", name: "Clerk diary check on limitation periods", event: "deadline.confirmed", when: "limitation", action: "task", taskTitle: "Diary check: {title}", offset: 90, assignee: "u-kaleab", enabled: true, version: 1, by: "u-yoni" },
    { id: "au-4", name: "Close-out task when agent work is approved", event: "work.approved", when: "any", action: "task", taskTitle: "File and close out: {title}", offset: 0, assignee: "responsible", enabled: false, version: 1, by: "u-linda" },
  ]);
  A.seed(AR, [
    { id: "ar-1", ruleId: "au-1", version: 2, at: "2026-09-16 10:15", event: "deadline.confirmed", record: "dl-3", matterId: "m2", status: "created", result: "Created \"Prepare for: Requisition date\" for Yonathan Degu, due Oct 9" },
    { id: "ar-2", ruleId: "au-3", version: 1, at: "2026-10-06 14:20", event: "deadline.confirmed", record: "dl-5", matterId: "m3", status: "flagged", result: "Kaleab Bereket cannot see this matter; task created unassigned in Ayo Ewebiyi's queue" },
    { id: "ar-3", ruleId: "au-1", version: 2, at: "2026-10-06 14:20", event: "deadline.confirmed", record: "dl-5", matterId: "m3", status: "created", result: "Created \"Prepare for: Limitation period\" for Ayo Ewebiyi, due Aug 7, 2028" },
    { id: "ar-4", ruleId: "au-2", version: 1, at: "2026-10-02 09:00", event: "deadline.confirmed", record: "dl-7", matterId: "m5", status: "skipped", result: "Not a critical date" },
  ]);
  const canRules = () => A.can("work.edit", "all");
  /** Can user uid see matter m for work (team, restriction, walls, grant)? Used before a rule assigns someone. */
  function userSees(uid, m) {
    const u = A.store.get("users", uid); if (!u || !m) return false;
    if ((m.wall || []).includes(uid)) return false;
    const onTeam = (m.team || []).includes(uid);
    if (m.restricted && !onTeam) return false;
    const g = A.grant("work.view", u.role); return g === "all" || g === "approve" || g === "view" || (g === "own" && onTeam);
  }
  function evaluate(rule, rec) {
    const m = A.store.get("matters", rec.matterId); if (!m) return { status: "skipped", result: "Record has no matter" };
    if (rule.when === "critical" && !rec.critical) return { status: "skipped", result: "Not a critical date" };
    if (rule.when === "limitation" && rec.kind !== "limitation") return { status: "skipped", result: "Not a limitation period" };
    if (A.store.where(WK, (w) => w.autoRule === rule.id && w.autoRecord === rec.id).length) return { status: "skipped", result: "Already created for this record" };
    const base = rec.due || T(); let due = A.addDays(base, -(+rule.offset || 0)); const late = due < T(); if (late) due = T();
    const title = rule.taskTitle.replace("{title}", rec.title);
    let assignee = rule.assignee === "responsible" ? m.responsible : rule.assignee, flagged = null;
    if (assignee !== "agent" && !userSees(assignee, m)) { flagged = `${A.user(assignee).name} cannot see this matter`; assignee = m.responsible; }
    return { status: flagged ? "flagged" : rule.action === "agent-draft" ? "waiting" : "created", m, title, due, assignee, flagged, late,
      result: flagged ? `${flagged}; task created unassigned in ${A.user(m.responsible).name}'s queue` : `${rule.action === "agent-draft" ? "Agent drafting" : "Created"} "${title}" for ${assignee === "agent" ? "the agent" : A.user(assignee).name}, due ${A.date(due)}${late ? " (date already passed; due today)" : ""}` };
  }
  const logRun = (rule, rec, ev, x) => A.store.put(AR, { id: A.id("ar"), ruleId: rule.id, version: rule.version || 1, at: A.now(), event: ev, record: rec.id, matterId: rec.matterId, status: x.status, result: x.result, runId: x.runId });
  function nextKey() { return `WRK-${Math.max(...A.store.all(WK).map((w) => +String(w.key).replace(/\D/g, "") || 0), 100) + 1}`; }
  function makeTask(rule, rec, x, extra = {}) {
    return A.store.put(WK, { id: A.id("wk"), key: nextKey(), matterId: rec.matterId, title: x.title, status: "To do", assignee: x.assignee, due: x.due, priority: rec.critical ? "High" : "Normal", comments: [], autoRule: rule.id, autoRecord: rec.id,
      flag: x.flagged ? `Rule assignee lacks access: ${x.flagged}` : undefined, history: [{ at: A.now(), by: "agent", what: `Created by automation "${rule.name}"${x.flagged ? ` (flagged: ${x.flagged})` : ""}` }], ...extra });
  }
  function apply(rule, rec, ev) {
    const x = evaluate(rule, rec);
    if (x.status === "skipped") return logRun(rule, rec, ev, x);
    if (rule.action !== "agent-draft") { makeTask(rule, rec, x); return logRun(rule, rec, ev, x); }
    const c = cite(rec.source);
    const r = A.agent.run({ title: `Automation: ${rule.name}`, matterId: rec.matterId, cap: "documents.release", cites: c ? [c] : [],
      steps: ["Read the deadline and its source", "Draft the client reminder",
        { label: "Lawyer approves the reminder before it reaches the client", gate: true, kind: "letter", title: `Step approval: client reminder for ${rec.title}`,
          summary: `Automation "${rule.name}" drafted a reminder to the client. The run creates the task after approval.`,
          body: `<p>Dear client, a key date on your matter falls on ${esc(fmt(rec.due))}: ${esc(rec.title)}. We will be in touch beforehand with anything we need from you.</p>`, cites: c ? [c] : [] },
        "Create the reminder task"],
      done() { const w = makeTask(rule, rec, x, { output: "<p>Client reminder approved and sent.</p>" }); const row = A.store.where(AR, (a) => a.runId === r.id)[0]; if (row) A.store.put(AR, { id: row.id, status: x.flagged ? "flagged" : "created", result: x.result.replace("Agent drafting", "Approved; created") });
        return { title: `Reminder approved; ${w.key} created`, cites: c ? [c] : [], outputs: [{ label: w.key, go: `work/${w.id}` }] }; } });
    if (!r) return logRun(rule, rec, ev, { ...x, status: "flagged", result: "Held: the agent is paused" });
    return logRun(rule, rec, ev, { ...x, status: "waiting", result: "Waiting for step approval: " + x.result, runId: r.id });
  }
  /** Fire every enabled rule for an event on a record. */
  function fire(ev, rec) { if (!rec) return; A.store.where(AU, (r) => r.enabled && r.event === ev).forEach((r) => { try { apply(r, rec, ev); } catch (e) { console.error(e); } }); }
  function candidates(rule) {
    if (rule.event === "work.approved") return A.store.where(WK, (w) => w.status === "Done" && w.approvedBy);
    return A.store.where(DL, (d) => (rule.event === "deadline.proposed" ? d.status === "proposed" : ["confirmed", "second-checked"].includes(d.status)));
  }
  function dryRun(id) {
    const rule = A.store.get(AU, id); if (!rule) return;
    const rows = candidates(rule).map((rec) => ({ rec, x: evaluate(rule, rec) })).filter((r) => A.matter(r.rec.matterId));
    const n = rows.filter((r) => r.x.status !== "skipped").length;
    A.store.put(AR, { id: A.id("ar"), ruleId: rule.id, version: rule.version || 1, at: A.now(), event: "dry run", record: "", matterId: null, status: "dry run", result: `${n} of ${rows.length} existing record${rows.length === 1 ? "" : "s"} would fire; nothing was created` });
    A.modal(`Dry run: ${rule.name}`, h`<p class="muted small">Run against existing records you can see. Nothing is created.</p>${A.ui.table([{ label: "Record", render: (r) => r.rec.title }, { label: "Matter", render: (r) => mName(r.rec.matterId) }, { label: "Would", render: (r) => h`${runChip(r.x.status)} ${r.x.result}` }], rows, { empty: "No existing records match this event" })}`, [{ label: "Close" }]);
  }
  const RUN_ST = { created: ["done", "Created"], flagged: ["blocked", "Flagged"], skipped: ["dismissed", "Skipped"], waiting: ["waiting", "Waiting on approval"], "dry run": ["suggested", "Dry run"] };
  const runChip = (s) => A.ui.status((RUN_ST[s] || [s])[0], (RUN_ST[s] || [s, s])[1]);
  const recLabel = (a) => { const d = A.store.get(DL, a.record) || A.store.get(WK, a.record); if (!a.record) return "—"; if (!d || !A.matter(d.matterId)) return "A matter you cannot see"; return d.title; };
  const recGo = (a) => (A.store.get(DL, a.record) ? `deadlines/${a.record}` : `work/${a.record}`);
  function historyTable(list) {
    return A.ui.table([{ label: "When", key: "at" }, { label: "Rule", render: (a) => (A.store.get(AU, a.ruleId) || {}).name || a.ruleId }, { label: "Matched record", render: (a) => (a.record && recLabel(a) !== "A matter you cannot see" ? h`<a href="#" data-a="go" data-to="${recGo(a)}">${recLabel(a)}</a>` : recLabel(a)) },
      { label: "Result", render: (a) => { const r = a.runId && A.store.get("runs", a.runId); return h`${runChip(a.status)} ${a.result}${r ? h` <a href="#" data-a="go" data-to="activity/run/${r.id}">run</a>` : ""}`; } }], list, { empty: "No runs yet" });
  }
  const visRuns = () => A.store.where(AR, (a) => !a.matterId || A.matter(a.matterId) || a.status === "dry run");
  function automationsView() {
    const rules = A.store.all(AU), ok = canRules();
    return h`<div class="row wrap" style="margin:10px 0"><p class="muted small grow">When something happens on a matter, a rule acts. Rules never assign work to someone who cannot see the matter; they flag it instead. Steps that reach a client wait for approval.</p>
      <button class="btn primary" data-a="work-rule-new"${dis(ok, "Only lawyers and the principal can edit rules")}>New rule</button></div>
      ${A.ui.table([{ label: "Rule", render: (r) => h`<b>${r.name}</b><br><small class="muted">v${r.version || 1}</small>` }, { label: "When", render: (r) => `${EVENTS[r.event]} · ${WHEN[r.when]}` },
        { label: "Then", render: (r) => h`${ACTS[r.action]}${r.action === "agent-draft" ? h` ${A.ui.status("needs-approval", "Approval step")}` : ""}<br><small class="muted">${r.offset ? `${r.offset} days before` : "same day"} · ${r.assignee === "responsible" ? "responsible lawyer" : r.assignee === "agent" ? "the agent" : A.user(r.assignee).name}</small>` },
        { label: "Runs", render: (r) => A.store.where(AR, (a) => a.ruleId === r.id).length },
        { label: "", render: (r) => h`<button class="btn small" data-a="work-rule-dry" data-id="${r.id}">Dry run</button> <button class="btn small" data-a="work-rule-toggle" data-id="${r.id}"${dis(ok, "Only lawyers and the principal can edit rules")}>${r.enabled ? "Turn off" : "Turn on"}</button> <button class="btn small" data-a="work-rule-open" data-id="${r.id}">History</button>` }], rules)}
      <h3 style="margin-top:16px">Recent runs</h3>${historyTable(visRuns().slice(0, 15))}`;
  }
  function ruleDetail(el, p) {
    const r = A.store.get(AU, p.id);
    if (!r) return A.render(el, h`<div class="page"><button class="btn small" data-a="go" data-to="work">Back to Work</button>${A.ui.empty("Rule not found")}</div>`);
    A.setContext({ kind: "work", id: r.id, label: r.name });
    A.render(el, h`<div class="page">${A.ui.header({ title: r.name, status: A.ui.status(r.enabled ? "active" : "inactive", r.enabled ? "On" : "Off"), back: { label: "Automations", to: "work" } })}
      <div class="card"><dl class="kv"><dt>When</dt><dd>${EVENTS[r.event]} · ${WHEN[r.when]}</dd><dt>Then</dt><dd>${ACTS[r.action]}: "${r.taskTitle}"</dd><dt>Due</dt><dd>${r.offset ? `${r.offset} days before the record's date` : "Same day"}</dd>
        <dt>Assign to</dt><dd>${r.assignee === "responsible" ? "Responsible lawyer" : r.assignee === "agent" ? "The agent" : A.user(r.assignee).name} · if they cannot see the matter, the task goes unassigned to the responsible lawyer and is flagged</dd>
        <dt>Approval</dt><dd>${r.action === "agent-draft" ? "The run pauses until a lawyer approves the draft" : "None: internal task only"}</dd><dt>Version</dt><dd>v${r.version || 1} · by ${A.user(r.by).name}</dd></dl>
        <div class="row wrap" style="margin-top:10px"><button class="btn" data-a="work-rule-dry" data-id="${r.id}">Dry run</button><button class="btn" data-a="work-rule-toggle" data-id="${r.id}"${dis(canRules(), "Only lawyers and the principal can edit rules")}>${r.enabled ? "Turn off" : "Turn on"}</button></div></div>
      <div class="card" style="margin-top:14px"><h3>Run history</h3>${historyTable(visRuns().filter((a) => a.ruleId === r.id))}</div></div>`);
  }
  A.action("work-auto-list", () => { wkView = "auto"; A.go("work"); });
  A.action("work-rule-open", (el) => A.go(`work/automations/${el.dataset.id}`));
  A.action("work-rule-dry", (el) => dryRun(el.dataset.id));
  A.action("work-rule-toggle", (el) => { if (!canRules()) return A.toast("Only lawyers and the principal can edit rules", "bad"); const r = A.store.get(AU, el.dataset.id); A.store.put(AU, { id: r.id, enabled: !r.enabled }); A.audit("automation.toggled", { target: r.id, note: `${r.name}: ${r.enabled ? "off" : "on"}` }); });
  A.action("work-rule-new", () => {
    if (!canRules()) return A.toast("Only lawyers and the principal can edit rules", "bad");
    const opt = (o, v) => Object.entries(o).map(([k, l]) => h`<option value="${k}" ${k === v ? A.raw("selected") : ""}>${l}</option>`);
    A.modal("New automation rule", h`<div id="rulef"><label class="field">Name<input name="name" class="input" value="Prep task before a confirmed deadline"></label>
      <label class="field">When<select name="event" class="input">${opt(EVENTS)}</select></label><label class="field">For<select name="when" class="input">${opt(WHEN)}</select></label>
      <label class="field">Then<select name="action" class="input">${opt(ACTS)}</select></label><label class="field">Task title ({title} is the record)<input name="taskTitle" class="input" value="Prepare for: {title}"></label>
      <label class="field">Days before<input name="offset" type="number" min="0" class="input" value="7"></label>
      <label class="field">Assign to<select name="assignee" class="input"><option value="responsible">Responsible lawyer</option><option value="agent">The agent</option>${A.store.where("users", (u) => !["client", "admin", "bookkeeper"].includes(u.role)).map((u) => h`<option value="${u.id}">${u.name}</option>`)}</select></label></div>`,
      [{ label: "Cancel" }, { label: "Save rule", primary: true, run: () => { const v = A.formValues(document.getElementById("rulef")); if (!v.name.trim()) { A.toast("Name the rule", "bad"); return false; }
        const r = A.store.put(AU, { id: A.id("au"), ...v, offset: +v.offset || 0, enabled: true, version: 1, by: me().id }); A.audit("automation.created", { target: r.id, note: r.name }); } }]);
  });

  /* ---------- imported tasks flagged as limitation periods ---------- */
  A.seed("importedTasks", [
    { id: "it-1", matterId: "m3", title: "SOL - Fortier v Lakeshore Transit", due: "2028-08-14", limitation: true, from: "Practice system export, Oct 1, 2026" },
    { id: "it-2", matterId: "m4", title: "Limitation - claim against estate trustee", due: "2028-03-02", limitation: true, from: "Practice system export, Oct 1, 2026" },
    { id: "it-3", matterId: "m6", title: "Limitation: arrears claim under lease", due: "2028-06-30", limitation: true, from: "Practice system export, Oct 1, 2026" },
  ]);
  const pendingImports = () => A.store.where("importedTasks", (t) => t.limitation && !t.converted && A.canMatter(A.store.get("matters", t.matterId), "deadlines.view"));
  function convertImports() {
    if (!canEdit()) return A.toast("You cannot add deadlines", "bad");
    const list = pendingImports(); if (!list.length) return;
    A.agent.run({ title: `Convert ${list.length} imported limitation task${list.length === 1 ? "" : "s"}`, plan: true, steps: ["Read the imported tasks", "Match against existing deadlines", "Propose limitation deadlines for confirmation"],
      done() {
        let made = 0, matched = 0;
        list.forEach((t) => {
          const dup = A.store.where(DL, (d) => d.matterId === t.matterId && d.kind === "limitation" && d.due === t.due)[0];
          if (dup) { matched++; A.store.put("importedTasks", { id: t.id, converted: dup.id, note: "Matched an existing deadline" }); return; }
          const m = A.store.get("matters", t.matterId);
          const d = A.store.put(DL, { id: A.id("dl"), matterId: t.matterId, title: `Limitation period (imported): ${t.title}`, due: t.due, kind: "limitation", critical: true, status: "proposed", proposedBy: "agent", owner: m.responsible, source: null, imported: t.id,
            rule: "Imported task marked as a limitation period. Recompute from the trigger event under the Limitations Act, 2002 before confirming (verify).", history: [{ at: A.now(), by: "agent", what: `Imported from ${t.from}` }] });
          proposeGate(d); made++; A.store.put("importedTasks", { id: t.id, converted: d.id });
        });
        A.audit("deadline.imported", { note: `${made} proposed, ${matched} matched existing` });
        return { title: `${made} proposed for confirmation, ${matched} matched existing`, outputs: [{ label: "Open deadlines", go: "deadlines" }] };
      } });
  }
  A.action("deadlines-import", () => convertImports());

  /* ---------- firm defaults for rule packs ---------- */
  const PACKS = { "on-rcp": "Ontario Rules of Civil Procedure", "on-small": "Ontario Small Claims Court Rules", "on-fam": "Ontario Family Law Rules", "on-lim": "Limitations Act, 2002 and notice statutes" };
  const SERVICE = { personal: { label: "Personal service", days: 0 }, email: { label: "Email (by consent)", days: 0 }, mail: { label: "Mail (effective 5th day, r.16.06(2), verify)", days: 5 }, courier: { label: "Courier (effective 2nd day, r.16.06(3), verify)", days: 2 } };
  A.seed("dlSettings", [{ id: "firm", pack: "on-rcp", service: "personal", calendar: "Firm deadlines calendar", chain: "30, 7, 1", attendees: "responsible", firmRules: [
    { id: "fr-1", name: "Practice direction: motion record served 7 days before the hearing (Toronto Region, verify)", by: "u-ayo", signed: null },
    { id: "fr-2", name: "Firm rule: answer every extension request within 2 business days", by: "u-linda", signed: { by: "u-yoni", at: "2026-09-02 10:00" } }] }]);
  const settings = () => A.store.get("dlSettings", "firm") || { pack: "on-rcp", service: "personal", firmRules: [] };
  const canSettings = () => A.can("deadlines.verify", "approve");
  function settingsView() {
    const s = settings(), ok = canSettings(), why = "Only a lawyer can change firm defaults";
    const sel = (name, o, v) => h`<select class="input" data-c="deadlines-setting" name="${name}"${dis(ok, why)}>${Object.entries(o).map(([k, l]) => h`<option value="${k}" ${k === v ? A.raw("selected") : ""}>${typeof l === "object" ? l.label : l}</option>`)}</select>`;
    return h`<div class="grid g2" style="margin-top:10px"><div class="card"><h3>Firm defaults</h3><p class="muted small">New matters and the deadline extractor start from these.</p>
      <label class="field">Default rule pack${sel("pack", PACKS, s.pack)}</label>
      <label class="field">Default service method${sel("service", SERVICE, s.service)}</label>
      <label class="field">Calendar for confirmed dates<input class="input" data-c="deadlines-setting" name="calendar" value="${s.calendar}"${dis(ok, why)}></label>
      <label class="field">Reminder chain (days before)<input class="input" data-c="deadlines-setting" name="chain" value="${s.chain}"${dis(ok, why)}></label>
      <label class="field">Invite to every deadline${sel("attendees", { responsible: "Responsible lawyer", team: "Whole matter team", "responsible+clerk": "Responsible lawyer and clerk" }, s.attendees)}</label></div>
      <div class="card"><h3>Firm-made rules</h3><p class="muted small">Rules the firm wrote itself. They apply only after a lawyer other than the author signs off, and they are never presented as verified court rules.</p>
        ${(s.firmRules || []).map((r) => h`<div class="kcard"><div class="row wrap"><b class="grow">${r.name}</b>${r.signed ? A.ui.status("approved", "Signed off") : A.ui.status("needs-approval", "Needs sign-off")}${A.ui.status("firm-made", "Firm-made")}</div>
          <small>By ${A.user(r.by).name}${r.signed ? ` · signed off by ${A.user(r.signed.by).name} ${r.signed.at}` : ""}</small>
          ${r.signed ? "" : h`<div class="row" style="margin-top:6px"><button class="btn small" data-a="deadlines-rule-sign" data-id="${r.id}"${dis(ok && r.by !== me().id, r.by === me().id ? "You wrote this rule; another lawyer signs off" : why)}>Sign off</button></div>`}</div>`)}</div></div>`;
  }
  A.change("deadlines-setting", (el) => { if (!canSettings()) return; const s = settings(); A.store.put("dlSettings", { id: "firm", [el.name]: el.value }); A.audit("deadlines.settings", { target: "firm", note: `${el.name}: ${s[el.name]} -> ${el.value}` }); });
  A.action("deadlines-rule-sign", (el) => { const s = settings(); const r = (s.firmRules || []).find((x) => x.id === el.dataset.id); if (!r || !canSettings() || r.by === me().id) return A.toast("You cannot sign off this rule", "bad");
    A.store.put("dlSettings", { id: "firm", firmRules: s.firmRules.map((x) => (x.id === r.id ? { ...x, signed: { by: me().id, at: A.now() } } : x)) }); A.audit("deadlines.rule-signed", { target: r.id, note: r.name }); });

  /* ======================================================================
     CALENDAR: day, week, month and agenda views, event detail
     ====================================================================== */
  const STAFF_IDS = ["u-linda", "u-yoni", "u-ayo", "u-meraf", "u-kaleab"];
  A.seed("calEvents", [
    { id: "ev-1", title: "Case conference", matterId: "m1", date: "2026-10-15", start: "10:00", end: "11:00", category: "hearing", calendar: "firm", attendees: ["u-linda", "u-ayo"], reminders: "1 day, 1 hour", recurrence: "none", location: "Toronto courthouse, 330 University Ave (verify)" },
    { id: "ev-2", title: "Client meeting: closing documents", matterId: "m2", date: "2026-10-27", start: "14:00", end: "15:00", category: "meeting", calendar: "personal-u-yoni", attendees: ["u-yoni", "u-kaleab"], reminders: "1 day", recurrence: "none", location: "Office" },
    { id: "ev-3", title: "Call with the insurer's adjuster", matterId: "m3", date: "2026-10-09", start: "15:30", end: "16:00", category: "call", calendar: "personal-u-ayo", attendees: ["u-ayo"], reminders: "15 minutes", recurrence: "none", location: "Phone" },
    { id: "ev-4", title: "Weekly file review", matterId: null, date: "2026-10-06", start: "09:00", end: "09:30", category: "internal", calendar: "firm", attendees: STAFF_IDS, reminders: "15 minutes", recurrence: "weekly", location: "Boardroom" },
  ]);
  A.seed("calendars", [
    { id: "firm", name: "Firm calendar", kind: "firm", view: "staff", edit: "lawyers" },
    { id: "limitations", name: "Limitation dates", kind: "generated", view: "staff", edit: "none" },
    { id: "workdue", name: "Work due dates", kind: "generated", view: "team", edit: "none" },
    ...STAFF_IDS.map((u) => ({ id: `personal-${u}`, name: "Personal", kind: "personal", owner: u, view: "owner", edit: "owner", share: u === "u-yoni" ? ["u-kaleab"] : [] })),
  ]);
  A.seed("calCategories", [{ id: "hearing", name: "Court attendance" }, { id: "meeting", name: "Client meeting" }, { id: "call", name: "Call" }, { id: "internal", name: "Internal" }]);
  const VIEWS = { owner: "Owner and people shared with", team: "Matter team", staff: "All staff" };
  const EDITS = { owner: "Owner", lawyers: "Lawyers", staff: "All staff", none: "Nobody (generated)" };
  const catLabel = (id) => (A.store.get("calCategories", id) || {}).name || id || "Event";
  const calName = (c) => (!c ? "—" : c.kind === "personal" ? `${A.user(c.owner).name} (personal)` : c.name);
  const calSees = (c) => !c || c.kind !== "personal" || c.owner === me().id || (c.share || []).includes(me().id) || c.view === "staff";
  const evVisible = (e) => (!e.matterId || A.matter(e.matterId)) && calSees(A.store.get("calendars", e.calendar));
  const canDlRules = () => A.can("deadlines.rules", "all");
  const calEdit = (c) => (!c ? false : c.kind === "personal" ? c.owner === me().id : c.edit === "none" ? false : c.edit === "lawyers" ? A.can("deadlines.verify", "approve") : A.can("deadlines.edit", "act"));
  let calMode = "month", calDate = null, calWho = "", calShow = "all";
  const cd = () => calDate || T();
  function occurrences(e, from, to) {
    if (e.recurrence !== "weekly") return e.date >= from && e.date <= to ? [e.date] : [];
    const out = []; let d = e.date, i = 0; while (d <= to && i++ < 300) { if (d >= from) out.push(d); d = A.addDays(d, 7); } return out;
  }
  function calItems(from, to) {
    const who = calWho === "me" ? me().id : calWho, ok = (uids) => !who || uids.includes(who), out = [];
    if (calShow === "all" || calShow === "deadlines") visDl().filter((d) => open(d) && d.due >= from && d.due <= to && ok([d.owner])).forEach((d) => out.push({ date: d.due, time: "", title: d.title, go: `deadlines/${d.id}`, kind: "Deadline", sev: sev(d) }));
    if (calShow === "all" || calShow === "events") A.store.all("calEvents").filter((e) => evVisible(e) && ok(e.attendees || [])).forEach((e) => occurrences(e, from, to).forEach((dt) => out.push({ date: dt, time: e.start, end: e.end, title: e.title, go: `deadlines/event/${e.id}`, kind: catLabel(e.category), sev: "accent" })));
    if (calShow === "all" || calShow === "work") visWk().filter((w) => w.status !== "Done" && w.due >= from && w.due <= to && ok([w.assignee])).forEach((w) => out.push({ date: w.due, time: "", title: `${w.key} ${w.title}`, go: `work/${w.id}`, kind: "Work due", sev: "line" }));
    return out.sort((a, b) => `${a.date}${a.time || "00"}`.localeCompare(`${b.date}${b.time || "00"}`));
  }
  const calChip = (it) => h`<button type="button" class="kb-item" data-a="go" data-to="${it.go}" title="${it.kind}: ${it.title}" style="display:block;width:100%;text-align:left;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;border:0;border-left:3px solid var(--${it.sev});background:var(--soft);border-radius:4px;padding:1px 5px;margin-top:2px;font-size:12px;cursor:pointer">${it.time ? `${it.time} ` : ""}${it.title}</button>`;
  const dayHead = (c) => h`<button type="button" class="small" data-a="deadlines-cal-day" data-id="${c}" style="border:0;background:none;padding:0;cursor:pointer;${c === T() ? "font-weight:700;color:var(--accent)" : ""}" title="Open day view">${DOW[dow(c)]} ${+c.slice(8)}</button>${HOLIDAYS[c] ? h` <span class="muted small">${HOLIDAYS[c]}</span>` : ""}`;
  function monthGrid() {
    const ym = cd().slice(0, 7), [y, mo] = ym.split("-").map(Number), first = `${ym}-01`, days = new Date(y, mo, 0).getDate(), lead = (dow(first) + 6) % 7;
    const cells = []; for (let i = 0; i < lead; i++) cells.push(null); for (let i = 1; i <= days; i++) cells.push(`${ym}-${String(i).padStart(2, "0")}`);
    const by = {}; calItems(first, `${ym}-${days}`).forEach((it) => (by[it.date] = by[it.date] || []).push(it));
    return h`<div style="display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:4px">${["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((x) => h`<div class="small muted" style="padding:2px 4px">${x}</div>`)}
      ${cells.map((c) => (c ? h`<div class="card" style="padding:4px 5px;min-height:78px;overflow:hidden;${c === T() ? "border-color:var(--accent);" : ""}${dow(c) % 6 === 0 || HOLIDAYS[c] ? "background:var(--soft)" : ""}">${dayHead(c)}${(by[c] || []).slice(0, 4).map(calChip)}${(by[c] || []).length > 4 ? h`<div class="small muted">+${by[c].length - 4} more</div>` : ""}</div>` : h`<div></div>`))}</div>`;
  }
  function weekGrid() {
    const ws = weekStart(cd()), days = [0, 1, 2, 3, 4, 5, 6].map((i) => A.addDays(ws, i)), by = {}; calItems(days[0], days[6]).forEach((it) => (by[it.date] = by[it.date] || []).push(it));
    return h`<div style="display:grid;grid-template-columns:repeat(7,minmax(0,1fr));gap:4px">${days.map((c) => h`<div class="card" style="padding:6px;min-height:220px;${c === T() ? "border-color:var(--accent);" : ""}${dow(c) % 6 === 0 || HOLIDAYS[c] ? "background:var(--soft)" : ""}">${dayHead(c)}${(by[c] || []).map(calChip)}</div>`)}</div>`;
  }
  function dayList() {
    const its = calItems(cd(), cd());
    return h`${HOLIDAYS[cd()] ? h`<p class="muted small">${HOLIDAYS[cd()]}${/court open/.test(HOLIDAYS[cd()]) ? "" : ": office and court registry closed"}</p>` : ""}${A.ui.table([{ label: "Time", render: (it) => (it.time ? `${it.time}–${it.end}` : "All day") }, { label: "What", render: (it) => h`<b>${it.title}</b>` }, { label: "Kind", key: "kind" }], its, { onRow: "deadlines-cal-go", rowId: (it) => it.go, empty: "Nothing on this day", emptyHint: " Press n to add an event." })}`;
  }
  function agendaList() {
    const its = calItems(cd(), A.addDays(cd(), 30)), by = {}; its.forEach((it) => (by[it.date] = by[it.date] || []).push(it));
    if (!its.length) return A.ui.empty("Nothing in the next 30 days", " Change the filters or add an event.");
    return h`${Object.keys(by).map((d) => h`<h3 class="small muted" style="margin:12px 0 6px;text-transform:uppercase;letter-spacing:.05em">${fmt(d)}${HOLIDAYS[d] ? ` · ${HOLIDAYS[d]}` : ""}</h3>${A.ui.list(by[d].map((it) => ({ id: it.go, title: it.title, sub: `${it.time ? `${it.time}–${it.end}` : "All day"} · ${it.kind}` })), "deadlines-cal-go")}`)}`;
  }
  function calRange() {
    if (calMode === "day") return fmt(cd());
    if (calMode === "week") { const ws = weekStart(cd()); return `Week of ${fmt(ws)}`; }
    if (calMode === "agenda") return `${fmt(cd())} to ${fmt(A.addDays(cd(), 30))}`;
    const [y, m] = cd().split("-").map(Number); return `${MONTHS[m - 1]} ${y}`;
  }
  function calendarPage(el) {
    syncHol(); A.setContext({ kind: "deadlines", label: "Calendar" });
    const body = calMode === "day" ? dayList() : calMode === "week" ? weekGrid() : calMode === "agenda" ? agendaList() : monthGrid();
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Calendar", sub: `${calRange()} · Proposed dates show here but never sync to outside calendars.`, back: { label: "Deadlines", to: "deadlines" },
        actions: [{ label: "‹", a: "deadlines-cal-shift", id: "-1", key: "[", title: "Previous ([)" }, { label: "Today", a: "deadlines-cal-today", key: "t" }, { label: "›", a: "deadlines-cal-shift", id: "1", key: "]", title: "Next (])" }, { label: "Calendar settings", to: "deadlines/settings/calendars" }],
        primary: { label: "New event", to: "deadlines/event/new", key: "n" } })}
      ${A.ui.filters({ chips: [{ id: "day", label: "Day" }, { id: "week", label: "Week" }, { id: "month", label: "Month" }, { id: "agenda", label: "Agenda" }], active: calMode, action: "deadlines-cal-mode",
        selects: [{ c: "deadlines-cal-who", value: calWho, label: "Whose calendar", options: [["", "Whole firm"], ["me", "Mine"], ...STAFF_IDS.map((u) => [u, A.user(u).name])] },
          { c: "deadlines-cal-show", value: calShow, label: "Show", options: [["all", "Deadlines, events and work"], ["deadlines", "Deadlines only"], ["events", "Events only"], ["work", "Work due dates only"]] }] })}
      ${body}
      ${A.ui.hints([["d", "Day"], ["w", "Week"], ["m", "Month"], ["[", "Previous"], ["]", "Next"], ["t", "Today"], ["n", "New event"]])}
    </div>`);
  }
  const shiftCal = (dir) => {
    if (calMode === "month") { const [y, m] = cd().split("-").map(Number); const x = new Date(y, m - 1 + dir, 1); calDate = `${x.getFullYear()}-${String(x.getMonth() + 1).padStart(2, "0")}-01`; }
    else calDate = A.addDays(cd(), dir * (calMode === "day" ? 1 : calMode === "week" ? 7 : 30));
    A.refresh();
  };
  A.action("deadlines-cal-shift", (el) => shiftCal(+el.dataset.id));
  A.action("deadlines-cal-today", () => { calDate = null; A.refresh(); });
  A.action("deadlines-cal-mode", (el) => { calMode = el.dataset.id; A.refresh(); });
  A.action("deadlines-cal-day", (el) => { calDate = el.dataset.id; calMode = "day"; A.refresh(); });
  A.action("deadlines-cal-go", (el) => A.go(el.dataset.id));
  A.change("deadlines-cal-who", (el) => { calWho = el.value; A.refresh(); });
  A.change("deadlines-cal-show", (el) => { calShow = el.value; A.refresh(); });
  A.ui.keys("deadlines/calendar", { d: { label: "Day view", fn: () => { calMode = "day"; A.refresh(); } }, w: { label: "Week view", fn: () => { calMode = "week"; A.refresh(); } }, m: { label: "Month view", fn: () => { calMode = "month"; A.refresh(); } } });

  /* ---------- event detail (create / edit one ordinary event) ---------- */
  function eventPage(el, p) {
    const isNew = p.id === "new";
    const e = isNew ? { id: "", title: "", matterId: "", date: cd(), start: "09:00", end: "10:00", category: "meeting", calendar: `personal-${me().id}`, attendees: [me().id], reminders: "1 day", recurrence: "none", location: "" } : A.store.get("calEvents", p.id);
    if (!e || (!isNew && !evVisible(e))) return A.render(el, h`<div class="page">${A.ui.header({ title: "Not available", back: { label: "Calendar", to: "deadlines/calendar" } })}<div class="locked"><b>Not available</b>This event does not exist or is on a calendar you cannot see.</div></div>`);
    A.setContext({ kind: "deadlines", id: e.id, label: e.title || "New event" });
    const cals = A.store.all("calendars").filter((c) => calEdit(c) || c.id === e.calendar), can = isNew ? cals.some(calEdit) : calEdit(A.store.get("calendars", e.calendar)), why = "You cannot edit events on this calendar";
    const others = A.store.all("calEvents").filter((x) => x.id !== e.id && occurrences(x, e.date, e.date).length && x.start < e.end && e.start < x.end);
    const clash = (e.attendees || []).map((u) => ({ u, ev: others.filter((x) => (x.attendees || []).includes(u)), away: A.store.where("absences", (a) => a.user === u && e.date >= a.from && e.date <= a.to)[0] })).filter((x) => x.ev.length || x.away);
    const dls = visDl().filter((d) => open(d) && d.due === e.date);
    const dis2 = can ? "" : A.raw(" disabled");
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: isNew ? "New event" : e.title, back: { label: "Calendar", to: "deadlines/calendar" }, meta: e.matterId && A.matter(e.matterId) ? h`<span class="muted small"> · <a href="#" data-a="go" data-to="matters/${e.matterId}">${mName(e.matterId)}</a></span>` : "",
        actions: isNew ? [] : [{ label: "✦ Draft agenda", a: "deadlines-ev-agenda", id: e.id, agent: true, disabled: !can, why }, { label: "Delete…", a: "deadlines-ev-delete", id: e.id, danger: true, disabled: !can, why }],
        primary: { label: isNew ? "Create event" : "Save", a: "deadlines-ev-save", id: e.id || "new", disabled: !can, why } })}
      <div class="grid g2">
        <div class="card"><div id="evform" class="form">
          <label class="field" style="grid-column:1/-1">Title<input name="title" class="input" value="${e.title}" placeholder="e.g. Examination for discovery"${dis2}></label>
          <label class="field">Date<input type="date" name="date" class="input" value="${e.date}"${dis2}></label>
          <label class="field">Calendar<select name="calendar" class="input"${dis2}>${opts(cals.map((c) => [c.id, calName(c)]), e.calendar)}</select></label>
          <label class="field">Starts<input type="time" name="start" class="input" value="${e.start}"${dis2}></label><label class="field">Ends<input type="time" name="end" class="input" value="${e.end}"${dis2}></label>
          <label class="field">Matter<select name="matterId" class="input"${dis2}>${opts([["", "No matter"], ...A.matters(null, "deadlines.view").map((m) => [m.id, `${m.number} ${m.name}`])], e.matterId || "")}</select></label>
          <label class="field">Category<select name="category" class="input"${dis2}>${opts(A.store.all("calCategories").map((c) => [c.id, c.name]), e.category)}</select></label>
          <label class="field">Reminders<input name="reminders" class="input" value="${e.reminders}" placeholder="1 day, 1 hour"${dis2}></label>
          <label class="field">Repeats<select name="recurrence" class="input"${dis2}>${opts([["none", "Does not repeat"], ["weekly", "Every week"]], e.recurrence)}</select></label>
          <label class="field" style="grid-column:1/-1">Where<input name="location" class="input" value="${e.location}"${dis2}></label>
          <fieldset class="field" style="grid-column:1/-1;border:0;padding:0"><legend>Attendees</legend><div class="row wrap">${STAFF_IDS.map((u) => h`<label class="small"><input type="checkbox" name="att-${u}" ${(e.attendees || []).includes(u) ? A.raw("checked") : ""}${dis2}> ${A.user(u).name}</label>`)}</div></fieldset></div></div>
        <div>
          <div class="card"><h3>Conflicts</h3>${clash.length ? clash.map((x) => h`<div class="row wrap" style="margin-bottom:4px">${A.ui.status("conflict", x.away ? x.away.kind : "Double-booked")}<span>${A.user(x.u).name}${x.away ? ` is away (${x.away.kind.toLowerCase()}); delegate ${A.user(x.away.delegate).name}` : `: ${x.ev.map((v) => `${v.title} ${v.start}`).join(", ")}`}</span></div>`) : h`<p class="muted small">No attendee is double-booked or away.</p>`}</div>
          <div class="card" style="margin-top:12px"><h3>Deadlines that day</h3>${dls.length ? dls.map((d) => dlRow(d)) : h`<p class="muted small">None.</p>`}</div>
          ${e.agenda ? h`<div class="card" style="margin-top:12px"><div class="row wrap"><h3 class="grow">Agenda</h3>${e.agendaKept ? A.ui.status("approved", "Kept") : A.ui.actor("agent", "Agent drafted")}</div><div class="draft" style="white-space:pre-wrap">${e.agenda}</div>
            ${e.agendaKept ? "" : h`<div class="row" style="margin-top:6px">${A.ui.btn({ label: "Keep agenda", a: "deadlines-ev-keep", id: e.id, small: true, disabled: !can, why })}${A.ui.btn({ label: "Discard", a: "deadlines-ev-discard", id: e.id, small: true, disabled: !can, why })}</div>`}</div>` : ""}
        </div></div>
    </div>`);
  }
  A.action("deadlines-ev-save", (el) => {
    const f = document.getElementById("evform"); if (!f) return; const v = A.formValues(f); const c = A.store.get("calendars", v.calendar);
    if (!calEdit(c)) return A.toast("You cannot edit events on this calendar", "bad");
    if (!v.title.trim()) return A.toast("Give the event a title", "bad");
    if (!v.date || !v.start || !v.end || v.end <= v.start) return A.toast("Check the date and times: the end must be after the start", "bad");
    let att = STAFF_IDS.filter((u) => v[`att-${u}`]); const m = v.matterId && A.store.get("matters", v.matterId);
    const cut = m ? att.filter((u) => !userSees(u, m)) : []; if (cut.length) { att = att.filter((u) => !cut.includes(u)); A.toast(`${cut.map((u) => A.user(u).name).join(", ")} cannot see ${m.number}; not invited`, "bad"); }
    const id = el.dataset.id === "new" ? A.id("ev") : el.dataset.id;
    A.store.put("calEvents", { id, title: v.title.trim(), date: v.date, start: v.start, end: v.end, calendar: v.calendar, matterId: v.matterId || null, category: v.category, reminders: v.reminders, recurrence: v.recurrence, location: v.location, attendees: att });
    A.audit(el.dataset.id === "new" ? "event.created" : "event.edited", { target: id, note: `${v.title} ${v.date}` }); A.toast("Event saved", "ok");
    if (el.dataset.id === "new") A.go(`deadlines/event/${id}`);
  });
  A.action("deadlines-ev-delete", (el) => { const e = A.store.get("calEvents", el.dataset.id); if (!e || !calEdit(A.store.get("calendars", e.calendar))) return A.toast("You cannot delete this event", "bad");
    A.modal("Delete event", h`<p>Delete “${e.title}” on ${fmt(e.date)}${e.recurrence === "weekly" ? " and every repeat" : ""}?</p>`, [{ label: "Cancel" }, { label: "Delete", primary: true, run: () => { A.store.remove("calEvents", e.id); A.audit("event.deleted", { target: e.id, note: e.title }); A.go("deadlines/calendar"); } }]); });
  A.action("deadlines-ev-agenda", (el) => { const e = A.store.get("calEvents", el.dataset.id); if (!e) return;
    A.agent.run({ title: `Draft agenda: ${e.title}`, matterId: e.matterId || undefined, steps: ["Reading the matter's open deadlines and work", "Drafting the agenda"], done() {
      const ds = e.matterId ? visDl().filter((d) => d.matterId === e.matterId && open(d)).slice(0, 3) : [], ws = e.matterId ? visWk().filter((w) => w.matterId === e.matterId && w.status !== "Done").slice(0, 3) : [];
      A.store.put("calEvents", { id: e.id, agendaKept: false, agenda: [`1. Purpose: ${e.title}`, ...ds.map((d, i) => `${i + 2}. Deadline ${fmt(d.due)}: ${d.title}`), ...ws.map((w, i) => `${i + 2 + ds.length}. ${w.key} ${w.title} (${w.status})`), `${2 + ds.length + ws.length}. Next steps and who does them`].join("\n") });
      return { title: "Agenda drafted; keep or discard it on the event" }; } }); });
  A.action("deadlines-ev-keep", (el) => { A.store.put("calEvents", { id: el.dataset.id, agendaKept: true }); A.audit("event.agenda-kept", { target: el.dataset.id }); });
  A.action("deadlines-ev-discard", (el) => A.store.put("calEvents", { id: el.dataset.id, agenda: "", agendaKept: false }));

  /* ======================================================================
     DEADLINE SETTINGS: defaults, reminders and escalation, rule packs, holidays, calendars, sync
     ====================================================================== */
  const CHANNELS = { app: "In-app", email: "Email", sms: "Text message", calendar: "Calendar invite" };
  const LADDER = { responsible: "Responsible lawyer", principal: "Principal lawyer", team: "Whole matter team", clerk: "Law clerk" };
  A.seed("dlReminderPolicy", [{ id: "firm", version: 3, kinds: {
    "court-rule": { days: "30, 14, 7, 3, 1", channels: ["app", "email", "calendar"], ladder: [{ days: 3, to: "principal" }, { days: 1, to: "team" }] },
    limitation: { days: "180, 90, 60, 30, 14, 7, 1", channels: ["app", "email", "calendar"], ladder: [{ days: 30, to: "principal" }, { days: 7, to: "team" }] },
    contract: { days: "14, 7, 3, 1", channels: ["app", "email"], ladder: [{ days: 2, to: "principal" }] },
    internal: { days: "3, 1", channels: ["app"], ladder: [] } } }]);
  const remPolicy = () => (A.store.get("dlReminderPolicy", "firm") || {}).kinds || {};
  let remDraft = null, remDraftBy = null;
  const clone = (x) => JSON.parse(JSON.stringify(x));
  const remEdit = () => { if (!remDraft) { remDraft = clone(remPolicy()); remDraftBy = me().id; } return remDraft; };
  A.onUser(() => { remDraft = null; });
  function remindersTab() {
    const pol = remDraft || remPolicy(), ok = canDlRules(), why = "Only the principal or the admin can change the reminder policy";
    const v = (A.store.get("dlReminderPolicy", "firm") || {}).version || 1, pend = A.store.where("approvals", (g) => g.effect === "deadlines-rem-apply" && g.status === "pending")[0];
    return h`<div class="row wrap" style="margin:8px 0"><p class="muted small grow">Policy v${v}. Reminders go out only for confirmed dates. If nobody acknowledges a reminder by the ladder's day, it escalates. Changes take effect after someone other than the person who drafted them approves.</p>
      ${A.ui.btn({ label: "✦ Suggest changes from missed reminders", a: "deadlines-rem-suggest", agent: true, disabled: !ok, why })}</div>
      ${pend ? h`<div style="margin-bottom:10px">${A.ui.gateCard(pend, { compact: true })}</div>` : ""}
      ${A.ui.table([{ label: "Deadline type", render: ([k]) => h`<b>${KINDS[k]}</b>` },
        { label: "Remind (days before)", render: ([k, p]) => A.ui.inline({ name: "dl-rem-days", id: k, value: p.days, disabled: !ok, why }) },
        { label: "Channels", render: ([k, p]) => h`${Object.entries(CHANNELS).map(([c, l]) => A.ui.btn({ label: `${(p.channels || []).includes(c) ? "✓ " : ""}${l}`, a: "deadlines-rem-ch", id: k, small: true, primary: (p.channels || []).includes(c), disabled: !ok, why, data: { ch: c } }))}${(p.channels || []).includes("sms") ? h` ${demo()}` : ""}` },
        { label: "Escalation ladder", render: ([k, p]) => h`${(p.ladder || []).map((s, i) => h`<div class="small">Not acknowledged ${s.days} day${s.days === 1 ? "" : "s"} before → ${LADDER[s.to]} ${ok ? A.ui.btn({ label: "Remove", a: "deadlines-rem-ladder-del", id: k, small: true, data: { i } }) : ""}</div>`)}${A.ui.btn({ label: "+ Step", a: "deadlines-rem-ladder-add", id: k, small: true, disabled: !ok, why })}` }],
        Object.entries(pol))}
      ${remDraft ? h`<div class="bulkbar">${A.ui.status("draft", "Unsaved draft")}${remDraftBy === "agent" ? A.ui.actor("agent", "Agent drafted") : ""}<span class="grow"></span>${A.ui.btn({ label: "Submit for approval", a: "deadlines-rem-submit", primary: true, small: true, disabled: !!pend, why: "A change is already waiting for approval" })}${A.ui.btn({ label: "Discard", a: "deadlines-rem-discard", small: true })}</div>` : ""}`;
  }
  A.inlineEdit("dl-rem-days", (k, v) => { if (!canDlRules()) return; const s = String(v).split(",").map((x) => +x.trim()); if (s.some((x) => !(x > 0))) { A.toast("Use whole days separated by commas, e.g. 14, 7, 1", "bad"); return false; } remEdit()[k].days = s.sort((a, b) => b - a).join(", "); });
  A.action("deadlines-rem-ch", (el) => { if (!canDlRules()) return; const p = remEdit()[el.dataset.id]; const c = el.dataset.ch; p.channels = p.channels.includes(c) ? p.channels.filter((x) => x !== c) : [...p.channels, c]; if (!p.channels.includes("app")) { p.channels.push("app"); A.toast("In-app reminders always stay on"); } A.refresh(); });
  A.action("deadlines-rem-ladder-del", (el) => { if (!canDlRules()) return; remEdit()[el.dataset.id].ladder.splice(+el.dataset.i, 1); A.refresh(); });
  A.action("deadlines-rem-ladder-add", (el) => { if (!canDlRules()) return; const k = el.dataset.id;
    A.modal(`Escalation step: ${KINDS[k]}`, h`<div id="ladf" class="form"><label class="field">If not acknowledged this many days before<input name="days" type="number" min="0" class="input" value="2"></label><label class="field">Escalate to<select name="to" class="input">${opts(Object.entries(LADDER), "principal")}</select></label></div>`,
      [{ label: "Cancel" }, { label: "Add step", primary: true, run: () => { const v = A.formValues(document.getElementById("ladf")); remEdit()[k].ladder = [...remEdit()[k].ladder, { days: +v.days || 0, to: v.to }].sort((a, b) => b.days - a.days); } }]); });
  A.action("deadlines-rem-discard", () => { remDraft = null; A.refresh(); });
  A.action("deadlines-rem-submit", () => {
    if (!remDraft) return; const cur = remPolicy(), v = (A.store.get("dlReminderPolicy", "firm") || {}).version || 1;
    const diff = Object.keys(remDraft).flatMap((k) => { const a = cur[k] || {}, b = remDraft[k]; const out = []; if (a.days !== b.days) out.push(`${KINDS[k]}: days ${a.days} → ${b.days}`); if (String(a.channels) !== String(b.channels)) out.push(`${KINDS[k]}: channels ${(b.channels || []).map((c) => CHANNELS[c]).join(", ")}`); if (JSON.stringify(a.ladder) !== JSON.stringify(b.ladder)) out.push(`${KINDS[k]}: ladder ${(b.ladder || []).map((s) => `${s.days}d → ${LADDER[s.to]}`).join("; ") || "none"}`); return out; });
    if (!diff.length) { remDraft = null; return A.toast("Nothing changed"); }
    A.gate({ kind: "policy", cap: "deadlines.rules", title: `Reminder policy v${v + 1}`, summary: `${diff.length} change${diff.length === 1 ? "" : "s"} to reminders and escalation`, preparedBy: remDraftBy || me().id,
      body: h`<ul>${diff.map((x) => h`<li>${x}</li>`)}</ul>`, checks: [{ label: "In-app reminders stay on for every type", ok: Object.values(remDraft).every((p) => p.channels.includes("app")), blocking: true }], effect: "deadlines-rem-apply", payload: { kinds: remDraft, version: v + 1 } });
    A.audit("deadlines.reminder-policy-proposed", { target: "firm", note: diff.join("; ") }); remDraft = null; A.toast("Sent for approval", "ok");
  });
  A.effect("deadlines-rem-apply", (p) => { A.store.put("dlReminderPolicy", { id: "firm", kinds: p.kinds, version: p.version }); A.audit("deadlines.reminder-policy", { target: "firm", note: `v${p.version} in effect` }); });
  A.action("deadlines-rem-suggest", () => { if (!canDlRules()) return A.toast("You cannot change the reminder policy", "bad");
    A.agent.run({ title: "Review reminder policy against missed reminders", steps: ["Reading the last 90 days of reminders", "Finding unacknowledged reminders", "Drafting changes"], done() {
      remDraft = clone(remPolicy()); remDraftBy = "agent";
      if (!remDraft.limitation.ladder.some((s) => s.days === 60)) remDraft.limitation.ladder = [{ days: 60, to: "principal" }, ...remDraft.limitation.ladder];
      if (!remDraft.contract.days.split(",").map((x) => x.trim()).includes("30")) remDraft.contract.days = `30, ${remDraft.contract.days}`;
      return { title: "Draft policy ready: 2 changes (review on Deadline settings · Reminders)", outputs: [{ label: "Review draft", go: "deadlines/settings/reminders" }] }; } }); });

  /* rule packs, versioned */
  A.seed("rulePacks", [
    { id: "rp-rcp-2026-2", pack: "on-rcp", version: "2026.2", effective: "2026-07-01", status: "active", rules: 214, by: "u-yoni", signedBy: "u-linda", changes: ["r.3.01(1): holiday list includes 2027 Ontario holidays", "r.29.1.03: discovery plan timing restated (verify)"] },
    { id: "rp-rcp-2026-1", pack: "on-rcp", version: "2026.1", effective: "2026-01-01", status: "archived", rules: 213, by: "u-yoni", signedBy: "u-linda", changes: ["Initial 2026 pack"] },
    { id: "rp-lim-2024-1", pack: "on-lim", version: "2024.1", effective: "2024-01-01", status: "active", rules: 18, by: "u-linda", signedBy: "u-yoni", changes: ["Limitations Act, 2002: basic (s.4) and ultimate (s.15) periods", "Municipal notice period for road claims (Municipal Act, 2001, verify)"] },
    { id: "rp-small-2025-1", pack: "on-small", version: "2025.1", effective: "2025-01-01", status: "active", rules: 41, by: "u-ayo", signedBy: "u-linda", changes: ["Small Claims Court Rules timelines"] },
  ]);
  const packKind = { "on-rcp": "court-rule", "on-small": "court-rule", "on-fam": "court-rule", "on-lim": "limitation" };
  function packsTab() {
    const ok = canDlRules(), list = A.store.all("rulePacks").sort((a, b) => a.pack.localeCompare(b.pack) || b.version.localeCompare(a.version));
    return h`<div class="row wrap" style="margin:8px 0"><p class="muted small grow">The date engine uses the active version of each pack. A new version takes effect only after a person approves it; open dates computed under the old version are flagged for re-check.</p>
      ${A.ui.btn({ label: "✦ Check for rule changes", a: "deadlines-pack-check", agent: true, disabled: !ok, why: "Only the principal or the admin manages rule packs" })}</div>
      ${A.ui.table([{ label: "Pack", render: (r) => PACKS[r.pack] }, { label: "Version", render: (r) => h`<b>v${r.version}</b>` }, { label: "Effective", render: (r) => fmt(r.effective) },
        { label: "Status", render: (r) => (r.status === "agent-prepared" ? h`${A.ui.status("agent-prepared")} ${A.ui.status("needs-approval")}` : A.ui.status(r.status)) }, { label: "Rules", key: "rules", num: true },
        { label: "Approved by", render: (r) => (r.signedBy ? A.user(r.signedBy).name : "—") }, { label: "", render: (r) => A.ui.btn({ label: "Changes", a: "deadlines-pack-changes", id: r.id, small: true }) }], list)}`;
  }
  A.action("deadlines-pack-changes", (el) => { const r = A.store.get("rulePacks", el.dataset.id); if (!r) return; const n = visDl().filter((d) => open(d) && d.kind === packKind[r.pack]).length;
    A.modal(`${PACKS[r.pack]} v${r.version}`, h`<ul>${r.changes.map((c) => h`<li>${c}</li>`)}</ul><p class="muted small">Prepared by ${r.by === "agent" ? "✦ the agent" : A.user(r.by).name}. ${n} open ${KINDS[packKind[r.pack]].toLowerCase()} date${n === 1 ? "" : "s"} would be flagged for re-check if this version changes.</p>`, [{ label: "Close" }]); });
  A.action("deadlines-pack-check", () => { if (!canDlRules()) return A.toast("You cannot manage rule packs", "bad");
    if (A.store.get("rulePacks", "rp-rcp-2027-1")) return A.toast("Already up to date: v2027.1 is waiting or active");
    A.agent.run({ title: "Check Ontario rule packs for changes", steps: ["Comparing the active packs with published amendments", "Drafting a new version", "Listing affected dates"], done() {
      const r = A.store.put("rulePacks", { id: "rp-rcp-2027-1", pack: "on-rcp", version: "2027.1", effective: "2027-01-01", status: "agent-prepared", rules: 215, by: "agent", changes: ["Holiday list for 2027 added", "Practice direction: motion materials timing (Toronto Region, verify against the published direction)"] });
      A.gate({ kind: "rule-pack", cap: "deadlines.rules", title: `Activate ${PACKS[r.pack]} v${r.version}`, summary: "Agent-prepared rule pack. Approving makes it the active version and flags open court dates for re-check.", preparedBy: "agent",
        body: h`<ul>${r.changes.map((c) => h`<li>${c}</li>`)}</ul>`, checks: [{ label: "Every change cites its published source", ok: false, blocking: false }], effect: "deadlines-pack-activate", payload: { id: r.id } });
      return { title: "Rule pack v2027.1 drafted; waiting for approval", outputs: [{ label: "Rule packs", go: "deadlines/settings/packs" }] }; } }); });
  A.effect("deadlines-pack-activate", (p) => { const r = A.store.get("rulePacks", p.id); if (!r) return;
    A.store.where("rulePacks", (x) => x.pack === r.pack && x.status === "active").forEach((x) => A.store.put("rulePacks", { id: x.id, status: "archived" }));
    A.store.put("rulePacks", { id: r.id, status: "active", signedBy: me().id });
    const hit = A.store.where(DL, (d) => open(d) && d.kind === packKind[r.pack]); hit.forEach((d) => A.store.put(DL, { id: d.id, history: hist(d, `Rule pack ${PACKS[r.pack]} v${r.version} activated: re-check this date`) }));
    A.audit("deadlines.rule-pack", { target: r.id, note: `v${r.version} active; ${hit.length} dates flagged` }); });

  /* holiday editor */
  function holidaysTab() {
    syncHol(); const ok = canDlRules(), why = "Only the principal or the admin edits the holiday calendar";
    const rows = A.store.all("dlHolidays").sort((a, b) => a.date.localeCompare(b.date));
    return h`<div class="row wrap" style="margin:8px 0"><p class="muted small grow">The date engine rolls a deadline that lands on a holiday or weekend to the next business day (r.3.01(1), verify). Mark a day "court open" if registries stay open.</p>
      ${A.ui.btn({ label: "Add holiday", a: "deadlines-hol-add", disabled: !ok, why })}${A.ui.btn({ label: "✦ Propose next year's holidays", a: "deadlines-hol-propose", agent: true, disabled: !ok, why })}</div>
      ${A.ui.table([{ label: "Date", render: (x) => fmt(x.date) }, { label: "Holiday", render: (x) => A.ui.inline({ name: "dl-hol-name", id: x.id, value: x.name, disabled: !ok, why }) },
        { label: "Courts", render: (x) => A.ui.inline({ name: "dl-hol-open", id: x.id, type: "select", value: x.courtOpen ? "1" : "", display: x.courtOpen ? "Court open" : "Closed", options: [["", "Closed"], ["1", "Court open"]], disabled: !ok, why }) },
        { label: "", render: (x) => A.ui.btn({ label: "Remove", a: "deadlines-hol-del", id: x.id, small: true, danger: true, disabled: !ok, why }) }], rows)}`;
  }
  A.inlineEdit("dl-hol-name", (id, v) => { if (!canDlRules() || !String(v).trim()) return false; A.store.put("dlHolidays", { id, name: String(v).trim() }); A.audit("deadlines.holiday", { target: id, note: `renamed ${v}` }); syncHol(); });
  A.inlineEdit("dl-hol-open", (id, v) => { if (!canDlRules()) return false; A.store.put("dlHolidays", { id, courtOpen: !!v }); A.audit("deadlines.holiday", { target: id, note: v ? "court open" : "closed" }); syncHol(); });
  A.action("deadlines-hol-del", (el) => { if (!canDlRules()) return; const x = A.store.get("dlHolidays", el.dataset.id); if (!x) return;
    A.modal("Remove holiday", h`<p>Remove ${x.name} (${fmt(x.date)})? Dates that rolled past it are not recomputed automatically.</p>`, [{ label: "Cancel" }, { label: "Remove", primary: true, run: () => { A.store.remove("dlHolidays", x.id); A.audit("deadlines.holiday-removed", { target: x.id, note: x.name }); syncHol(); } }]); });
  A.action("deadlines-hol-add", () => { if (!canDlRules()) return;
    A.modal("Add holiday", h`<div id="holf" class="form"><label class="field">Date<input type="date" name="date" class="input"></label><label class="field">Name<input name="name" class="input" placeholder="e.g. Civic Holiday"></label><label class="field"><span><input type="checkbox" name="courtOpen"> Court registries open</span></label></div>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: () => { const v = A.formValues(document.getElementById("holf")); if (!v.date || !v.name.trim()) { A.toast("Date and name are required", "bad"); return false; }
        A.store.put("dlHolidays", { id: v.date, date: v.date, name: v.name.trim(), courtOpen: !!v.courtOpen, by: me().id }); A.audit("deadlines.holiday-added", { target: v.date, note: v.name }); syncHol(); } }]); });
  const NEXT_HOL = [["2027-05-24", "Victoria Day"], ["2027-07-01", "Canada Day"], ["2027-09-06", "Labour Day"], ["2027-10-11", "Thanksgiving"], ["2027-12-27", "Christmas Day (observed)"], ["2027-12-28", "Boxing Day (observed)"]];
  A.action("deadlines-hol-propose", () => { if (!canDlRules()) return;
    const add = NEXT_HOL.filter(([d]) => !A.store.get("dlHolidays", d)); if (!add.length) return A.toast("Next year's holidays are already in the calendar");
    if (A.store.where("approvals", (g) => g.effect === "deadlines-hol-apply" && g.status === "pending").length) return A.toast("A holiday proposal is already waiting for approval");
    A.agent.run({ title: "Propose 2027 Ontario holidays", steps: ["Reading the Legislation Act, 2006 holiday list", "Computing observed dates"], done() {
      A.gate({ kind: "rule-pack", cap: "deadlines.rules", title: `Add ${add.length} holidays for 2027`, summary: "Agent-proposed holiday dates for the date engine.", preparedBy: "agent", body: h`<ul>${add.map(([d, n]) => h`<li>${fmt(d)}: ${n}</li>`)}</ul>`, effect: "deadlines-hol-apply", payload: { add } });
      return { title: `${add.length} holidays proposed; waiting for approval` }; } }); });
  A.effect("deadlines-hol-apply", (p) => { (p.add || []).forEach(([date, name]) => A.store.put("dlHolidays", { id: date, date, name, courtOpen: false, by: "agent" })); syncHol(); A.audit("deadlines.holidays-added", { note: `${(p.add || []).length} added` }); });

  /* calendars, sharing and categories */
  function calendarsTab() {
    const list = A.store.all("calendars").filter(calSees);
    return h`<div class="row wrap" style="margin:8px 0"><p class="muted small grow">Generated calendars fill themselves from confirmed deadlines and work due dates; nobody edits them by hand.</p>${A.ui.btn({ label: "New calendar", a: "deadlines-cal-new", disabled: !canDlRules(), why: "Only the principal or the admin adds firm calendars" })}</div>
      ${A.ui.table([{ label: "Calendar", render: (c) => h`<b>${calName(c)}</b>` }, { label: "Kind", render: (c) => ({ firm: "Firm", generated: "Generated", personal: "Personal", custom: "Custom" }[c.kind]) },
        { label: "Who can see", render: (c) => A.ui.inline({ name: "dl-cal-view", id: c.id, type: "select", value: c.view, display: VIEWS[c.view], options: Object.entries(VIEWS), disabled: !(c.kind === "personal" ? c.owner === me().id : canDlRules()), why: "Not yours to change" }) },
        { label: "Who can edit", render: (c) => (c.kind === "personal" || c.kind === "generated" ? EDITS[c.edit] : A.ui.inline({ name: "dl-cal-edit", id: c.id, type: "select", value: c.edit, display: EDITS[c.edit], options: Object.entries(EDITS).filter(([k]) => k !== "owner" && k !== "none"), disabled: !canDlRules(), why: "Only the principal or the admin" })) },
        { label: "Shared with", render: (c) => (c.kind === "personal" ? h`${(c.share || []).map((u) => A.user(u).name).join(", ") || "Nobody"} ${c.owner === me().id ? A.ui.btn({ label: "Share…", a: "deadlines-cal-share", id: c.id, small: true }) : ""}` : "—") }], list)}
      <h3 style="margin-top:16px">Event categories</h3>
      ${A.ui.table([{ label: "Category", render: (c) => A.ui.inline({ name: "dl-cat-name", id: c.id, value: c.name, disabled: !canDlRules(), why: "Only the principal or the admin" }) }, { label: "Events", num: true, render: (c) => A.store.where("calEvents", (e) => e.category === c.id).length },
        { label: "", render: (c) => A.ui.btn({ label: "Remove", a: "deadlines-cat-del", id: c.id, small: true, disabled: !canDlRules() || A.store.where("calEvents", (e) => e.category === c.id).length > 0, why: "In use or not yours to change" }) }], A.store.all("calCategories"))}
      <div class="row" style="margin-top:8px">${A.ui.btn({ label: "Add category", a: "deadlines-cat-add", small: true, disabled: !canDlRules(), why: "Only the principal or the admin" })}</div>`;
  }
  A.inlineEdit("dl-cal-view", (id, v) => { const c = A.store.get("calendars", id); if (!c || !(c.kind === "personal" ? c.owner === me().id : canDlRules())) return false; A.store.put("calendars", { id, view: v }); A.audit("calendar.sharing", { target: id, note: `view: ${v}` }); });
  A.inlineEdit("dl-cal-edit", (id, v) => { if (!canDlRules()) return false; A.store.put("calendars", { id, edit: v }); A.audit("calendar.sharing", { target: id, note: `edit: ${v}` }); });
  A.inlineEdit("dl-cat-name", (id, v) => { if (!canDlRules() || !String(v).trim()) return false; A.store.put("calCategories", { id, name: String(v).trim() }); });
  A.action("deadlines-cat-del", (el) => { if (!canDlRules()) return; A.store.remove("calCategories", el.dataset.id); });
  A.action("deadlines-cat-add", () => { if (!canDlRules()) return; A.modal("Add category", h`<label class="field">Name<input id="catn" class="input" placeholder="e.g. Mediation"></label>`, [{ label: "Cancel" }, { label: "Add", primary: true, run: () => { const v = document.getElementById("catn").value.trim(); if (!v) { A.toast("Name it", "bad"); return false; } A.store.put("calCategories", { id: A.id("cat"), name: v }); } }]); });
  A.action("deadlines-cal-new", () => { if (!canDlRules()) return;
    A.modal("New calendar", h`<div id="calf" class="form"><label class="field">Name<input name="name" class="input" placeholder="e.g. Real estate closings"></label><label class="field">Who can see<select name="view" class="input">${opts(Object.entries(VIEWS), "staff")}</select></label></div>`,
      [{ label: "Cancel" }, { label: "Create", primary: true, run: () => { const v = A.formValues(document.getElementById("calf")); if (!v.name.trim()) { A.toast("Name it", "bad"); return false; } A.store.put("calendars", { id: A.id("cal"), name: v.name.trim(), kind: "custom", view: v.view, edit: "lawyers" }); A.audit("calendar.created", { note: v.name }); } }]); });
  A.action("deadlines-cal-share", (el) => { const c = A.store.get("calendars", el.dataset.id); if (!c || c.owner !== me().id) return A.toast("Only the owner shares a personal calendar", "bad");
    A.modal("Share your calendar", h`<div id="shf">${STAFF_IDS.filter((u) => u !== me().id).map((u) => h`<label class="field"><span><input type="checkbox" name="${u}" ${(c.share || []).includes(u) ? A.raw("checked") : ""}> ${A.user(u).name}</span></label>`)}</div><p class="muted small">They see your events, not private notes. Matter events stay hidden from anyone off the matter.</p>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: () => { const v = A.formValues(document.getElementById("shf")); const share = Object.keys(v).filter((k) => v[k]); A.store.put("calendars", { id: c.id, share }); A.audit("calendar.sharing", { target: c.id, note: share.map((u) => A.user(u).name).join(", ") || "nobody" }); } }]); });

  /* external calendar sync (mock) */
  const PROVIDERS = { google: "Google Calendar", microsoft: "Microsoft 365 calendar" };
  const SYNC_WHAT = { deadlines: "Confirmed deadlines", events: "Events", work: "Work due dates" };
  function syncTab() {
    const rows = Object.keys(PROVIDERS).map((p) => A.store.get("calSync", `${p}-${me().id}`) || { id: `${p}-${me().id}`, provider: p, connected: false });
    return h`<p class="muted small" style="margin:8px 0">Your own connection; each person connects their own account. Proposed or unconfirmed dates never sync, and outside calendars see the matter number, not the client's name. ${demo()}</p>
      <div class="grid g2">${rows.map((s) => h`<div class="card"><div class="row wrap"><h3 class="grow">${PROVIDERS[s.provider]}</h3>${s.connected ? A.ui.status(s.health === "failed" ? "failed" : "active", s.health === "failed" ? "Sync failing" : "Connected") : A.ui.status("inactive", "Not connected")}</div>
        ${s.connected ? h`<dl class="kv"><dt>Direction</dt><dd><select class="fsel" data-c="deadlines-sync-dir" data-id="${s.id}" aria-label="Direction">${opts([["push", "This app → calendar"], ["two-way", "Both ways (events only)"]], s.dir || "push")}</select></dd>
          <dt>What syncs</dt><dd>${Object.entries(SYNC_WHAT).map(([k, l]) => A.ui.btn({ label: `${(s.what || []).includes(k) ? "✓ " : ""}${l}`, a: "deadlines-sync-what", id: s.id, small: true, primary: (s.what || []).includes(k), data: { k } }))}</dd>
          <dt>Last sync</dt><dd>${s.last ? A.dateTime(s.last) : "Never"} · ${s.count || 0} items</dd></dl>
          <div class="row wrap" style="margin-top:8px">${A.ui.btn({ label: "Sync now", a: "deadlines-sync-now", id: s.id, small: true, primary: true })}${A.ui.btn({ label: "Disconnect", a: "deadlines-sync-off", id: s.id, small: true, danger: true })}</div>`
          : A.ui.btn({ label: `Connect ${PROVIDERS[s.provider]}`, a: "deadlines-sync-on", id: s.id, data: { p: s.provider } })}</div>`)}</div>`;
  }
  A.action("deadlines-sync-on", (el) => { A.store.put("calSync", { id: el.dataset.id, provider: el.dataset.p, user: me().id, connected: true, dir: "push", what: ["deadlines", "events"], health: "ok" }); A.audit("calendar.sync-connected", { target: el.dataset.p }); A.toast(`${PROVIDERS[el.dataset.p]} connected (demo)`, "ok"); });
  A.action("deadlines-sync-off", (el) => { A.store.put("calSync", { id: el.dataset.id, connected: false }); A.audit("calendar.sync-disconnected", { target: el.dataset.id }); });
  A.change("deadlines-sync-dir", (el) => A.store.put("calSync", { id: el.dataset.id, dir: el.value }));
  A.action("deadlines-sync-what", (el) => { const s = A.store.get("calSync", el.dataset.id); if (!s) return; const k = el.dataset.k; A.store.put("calSync", { id: s.id, what: (s.what || []).includes(k) ? s.what.filter((x) => x !== k) : [...(s.what || []), k] }); });
  A.action("deadlines-sync-now", (el) => { const s = A.store.get("calSync", el.dataset.id); if (!s || !s.connected) return;
    const n = (s.what.includes("deadlines") ? visDl().filter((d) => ["confirmed", "second-checked"].includes(d.status) && d.owner === me().id).length : 0) + (s.what.includes("events") ? A.store.where("calEvents", (e) => (e.attendees || []).includes(me().id)).length : 0) + (s.what.includes("work") ? visWk().filter((w) => w.assignee === me().id && w.status !== "Done").length : 0);
    A.store.put("calSync", { id: s.id, last: A.now(), count: n, health: "ok" }); A.toast(`Synced ${n} items (demo)`, "ok"); });

  function dlSettingsPage(el, p) {
    syncHol(); A.setContext({ kind: "deadlines", label: "Deadline settings" });
    const tab = p.tab || "defaults", T2 = { defaults: settingsView, reminders: remindersTab, packs: packsTab, holidays: holidaysTab, calendars: calendarsTab, sync: syncTab };
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Deadline settings", back: { label: "Deadlines", to: "deadlines" }, sub: canDlRules() ? "" : "Read only for your role; the principal or the admin changes firm settings." })}
      ${A.ui.filters({ chips: [{ id: "defaults", label: "Defaults and firm rules" }, { id: "reminders", label: "Reminders and escalation" }, { id: "packs", label: "Rule packs" }, { id: "holidays", label: "Holidays" }, { id: "calendars", label: "Calendars and categories" }, { id: "sync", label: "Calendar sync" }], active: tab, action: "deadlines-set-tab" })}
      ${(T2[tab] || settingsView)()}
    </div>`);
  }
  A.action("deadlines-set-tab", (el) => A.go(el.dataset.id === "defaults" ? "deadlines/settings" : `deadlines/settings/${el.dataset.id}`));

  /* ======================================================================
     WORK: categories, workflow, templates, saved filters, tracker sync, reports, workload and leave
     ====================================================================== */
  A.seed("workCats", [{ id: "drafting", name: "Drafting", est: 4 }, { id: "research", name: "Research and analysis", est: 5 }, { id: "filing", name: "Filing and service", est: 2 }, { id: "client", name: "Client communication", est: 1.5 }, { id: "admin", name: "File admin", est: 1 }]);
  A.seed("workPriorities", [{ id: "High", target: "Same or next business day" }, { id: "Normal", target: "Within 3 business days" }, { id: "Low", target: "Within 2 weeks" }]);
  const catOf = (w) => w.category || (/draft|letter|memo|summar|mark up|chart/i.test(w.title) ? "drafting" : /review|search|compute|research/i.test(w.title) ? "research" : /file|serve|application|register|order|notice/i.test(w.title) ? "filing" : /client|request|gather|records|report to/i.test(w.title) ? "client" : "admin");
  const catName = (id) => (A.store.get("workCats", id) || {}).name || id;
  const estOf = (w) => +(w.estimate || (A.store.get("workCats", catOf(w)) || {}).est || 1);
  const canWf = () => A.can("work.workflows", "all");
  const WFWHY = "Only the principal or the admin edits workflows, templates and categories";
  A.seed("workflow", [{ id: "default", version: 3, statuses: [{ id: "To do", group: "Open", system: true }, { id: "Agent-ready", group: "Open", system: true }, { id: "In progress", group: "Active", system: true, wip: 6 }, { id: "Needs approval", group: "Review", system: true }, { id: "Done", group: "Closed", system: true }],
    transitions: [{ id: "t1", from: "To do", to: "In progress", guard: "Assignee can see the matter" }, { id: "t2", from: "To do", to: "Agent-ready", guard: "Agent is not paused on the matter", action: "Agent run starts" },
      { id: "t3", from: "Agent-ready", to: "Needs approval", guard: "Every claim cites a source", action: "Approval created", locked: true }, { id: "t4", from: "In progress", to: "Needs approval", guard: "Output attached", action: "Approval created" },
      { id: "t5", from: "Needs approval", to: "Done", guard: "Approved by someone other than the preparer; the agent never approves", locked: true }, { id: "t6", from: "In progress", to: "Done", guard: "Person-made work only" }] }]);
  const wf = () => A.store.get("workflow", "default") || { statuses: [], transitions: [] };
  const freeStatus = (s) => { const st = wf().statuses.find((x) => x.id === s); return !!st && ["Open", "Active"].includes(st.group) && s !== "Agent-ready"; };
  const wfPut = (ch, note) => { A.store.put("workflow", { id: "default", ...ch, version: (wf().version || 1) + 1 }); A.audit("work.workflow", { target: "default", note }); };
  function workflowTab() {
    const w0 = wf(), ok = canWf();
    return h`<p class="muted small" style="margin:8px 0">Workflow v${w0.version}. The five system statuses and the approval transitions are fixed; add your own statuses for open or active work (for example "Waiting on client"). WIP limits warn on the board.</p>
      ${A.ui.table([{ label: "Status", render: (s) => h`<b>${s.id}</b>${s.system ? h` <span class="muted small">system</span>` : ""}` },
        { label: "Group", render: (s) => (s.system ? s.group : A.ui.inline({ name: "wk-st-group", id: s.id, type: "select", value: s.group, options: [["Open", "Open"], ["Active", "Active"]], disabled: !ok, why: WFWHY })) },
        { label: "WIP limit", render: (s) => A.ui.inline({ name: "wk-st-wip", id: s.id, type: "number", value: s.wip || 0, display: s.wip ? String(s.wip) : "None", disabled: !ok || s.group === "Closed", why: WFWHY }) },
        { label: "Items", num: true, render: (s) => A.store.where(WK, (x) => x.status === s.id).length },
        { label: "", render: (s) => (s.system ? "" : A.ui.btn({ label: "Remove", a: "work-st-del", id: s.id, small: true, danger: true, disabled: !ok || A.store.where(WK, (x) => x.status === s.id).length > 0, why: "Move its items first" })) }], w0.statuses, { rowId: (s) => s.id })}
      <div class="row" style="margin:8px 0 16px">${A.ui.btn({ label: "Add status", a: "work-st-add", small: true, disabled: !ok, why: WFWHY })}</div>
      <h3>Transitions</h3>
      ${A.ui.table([{ label: "From", key: "from" }, { label: "To", key: "to" }, { label: "Guard", render: (t) => (t.locked ? h`${t.guard} ${A.ui.status("locked", "Fixed")}` : A.ui.inline({ name: "wk-tr-guard", id: t.id, value: t.guard, disabled: !ok, why: WFWHY })) }, { label: "Then", render: (t) => t.action || "—" },
        { label: "", render: (t) => (t.locked ? "" : A.ui.btn({ label: "Remove", a: "work-tr-del", id: t.id, small: true, danger: true, disabled: !ok, why: WFWHY })) }], w0.transitions)}
      <div class="row" style="margin-top:8px">${A.ui.btn({ label: "Add transition", a: "work-tr-add", small: true, disabled: !ok, why: WFWHY })}</div>`;
  }
  A.inlineEdit("wk-st-group", (id, v) => { if (!canWf()) return false; wfPut({ statuses: wf().statuses.map((s) => (s.id === id && !s.system ? { ...s, group: v } : s)) }, `${id} group ${v}`); });
  A.inlineEdit("wk-st-wip", (id, v) => { if (!canWf() || v < 0) return false; wfPut({ statuses: wf().statuses.map((s) => (s.id === id ? { ...s, wip: +v || 0 } : s)) }, `${id} WIP ${v}`); });
  A.inlineEdit("wk-tr-guard", (id, v) => { if (!canWf()) return false; wfPut({ transitions: wf().transitions.map((t) => (t.id === id && !t.locked ? { ...t, guard: String(v) } : t)) }, `guard ${id}`); });
  A.action("work-st-del", (el) => { if (!canWf()) return; const id = el.dataset.id; if (A.store.where(WK, (x) => x.status === id).length) return A.toast("Move its items first", "bad"); wfPut({ statuses: wf().statuses.filter((s) => s.id !== id || s.system), transitions: wf().transitions.filter((t) => t.from !== id && t.to !== id) }, `removed ${id}`); });
  A.action("work-tr-del", (el) => { if (!canWf()) return; wfPut({ transitions: wf().transitions.filter((t) => t.id !== el.dataset.id || t.locked) }, `removed transition ${el.dataset.id}`); });
  A.action("work-st-add", () => { if (!canWf()) return;
    A.modal("Add status", h`<div id="stf" class="form"><label class="field">Name<input name="name" class="input" placeholder="e.g. Waiting on client"></label><label class="field">Group<select name="group" class="input">${opts([["Open", "Open"], ["Active", "Active"]], "Active")}</select></label></div>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: () => { const v = A.formValues(document.getElementById("stf")); const n = v.name.trim(); if (!n || wf().statuses.some((s) => s.id.toLowerCase() === n.toLowerCase())) { A.toast("Give it a new, unique name", "bad"); return false; }
        const st = [...wf().statuses]; st.splice(st.findIndex((s) => s.id === "Needs approval"), 0, { id: n, group: v.group }); wfPut({ statuses: st, transitions: [...wf().transitions, { id: A.id("t"), from: "In progress", to: n, guard: "" }, { id: A.id("t"), from: n, to: "In progress", guard: "" }] }, `added ${n}`); } }]); });
  A.action("work-tr-add", () => { if (!canWf()) return; const ss = wf().statuses.map((s) => [s.id, s.id]);
    A.modal("Add transition", h`<div id="trf" class="form"><label class="field">From<select name="from" class="input">${opts(ss, "To do")}</select></label><label class="field">To<select name="to" class="input">${opts(ss, "In progress")}</select></label><label class="field">Guard<input name="guard" class="input" placeholder="e.g. Client documents received"></label></div>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: () => { const v = A.formValues(document.getElementById("trf")); if (v.from === v.to) { A.toast("Pick two different statuses", "bad"); return false; }
        if (v.to === "Done" || v.to === "Needs approval") { A.toast("Approval transitions are fixed", "bad"); return false; } wfPut({ transitions: [...wf().transitions, { id: A.id("t"), ...v }] }, `transition ${v.from} -> ${v.to}`); } }]); });

  /* practice-area templates */
  const RE_CLOSING = ["Title search and execution search", "Requisition letter to seller's lawyer", "Review mortgage instructions and conditions", "Order tax certificate and utility readings", "Statement of adjustments", "Land transfer tax affidavit (Ontario and Toronto)", "Title insurance policy", "Closing documents and undertakings", "Trust funds request to client", "Report to client and lender"];
  A.seed("workTemplates", [
    { id: "tp-re", name: "Real estate closing", area: "Real estate", anchor: "closing", version: 2, status: "active", items: RE_CLOSING.map((t, i) => ({ id: `ti-re-${i}`, title: t, offset: -Math.max(1, 14 - i), assignee: i % 3 === 1 ? "agent" : "clerk", category: i % 3 === 1 ? "drafting" : "filing", priority: i < 2 ? "High" : "Normal" })) },
    { id: "tp-emp", name: "Wrongful dismissal: demand stage", area: "Employment", anchor: "opened", version: 1, status: "active", items: [
      { id: "ti-e1", title: "Collect employment contract, termination letter and pay records", offset: 2, assignee: "paralegal", category: "client" }, { id: "ti-e2", title: "Compute reasonable notice range (Bardal factors)", offset: 5, assignee: "agent", category: "research" },
      { id: "ti-e3", title: "Draft demand letter", offset: 7, assignee: "agent", category: "drafting" }, { id: "ti-e4", title: "Propose limitation deadline (2 years from termination)", offset: 1, assignee: "responsible", category: "filing", priority: "High" }] },
    { id: "tp-est", name: "Estate: certificate of appointment", area: "Estates", anchor: "opened", version: 1, status: "active", items: [
      { id: "ti-s1", title: "Obtain original will and death certificate", offset: 3, assignee: "paralegal", category: "client" }, { id: "ti-s2", title: "Draft asset and liability summary for estate administration tax", offset: 10, assignee: "agent", category: "drafting" },
      { id: "ti-s3", title: "Prepare application package (r.74.04)", offset: 14, assignee: "paralegal", category: "filing" }, { id: "ti-s4", title: "Serve notice of application on beneficiaries", offset: 21, assignee: "clerk", category: "filing" }] },
  ]);
  const ROLES = [["responsible", "Responsible lawyer"], ["paralegal", "Paralegal on the team"], ["clerk", "Law clerk on the team"], ["agent", "✦ Agent"]];
  let tpCur = "tp-re";
  const tpPut = (tp, ch, note) => { A.store.put("workTemplates", { id: tp.id, ...ch, version: (tp.version || 1) + 1 }); A.audit("work.template-edited", { target: tp.id, note }); };
  function templatesTab() {
    const all = A.store.all("workTemplates"), tp = A.store.get("workTemplates", tpCur) || all[0], ok = canWf();
    if (!tp) return A.ui.empty("No templates", " Create one.");
    return h`<div class="row wrap" style="margin:8px 0"><p class="muted small grow">Apply a template from a matter's Work tab. Due dates count from the anchor (closing date, or the day it is applied); assignees resolve to people on the matter team.</p>
      ${A.ui.btn({ label: "New template", a: "work-tp-new", small: true, disabled: !ok, why: WFWHY })}${A.ui.btn({ label: "✦ Draft from closed matters", a: "work-tp-draft", small: true, agent: true, disabled: !ok, why: WFWHY })}</div>
      ${A.ui.filters({ chips: all.map((t) => ({ id: t.id, label: t.name })), active: tp.id, action: "work-tp-pick" })}
      <div class="card"><div class="row wrap"><h3 class="grow">${tp.name} · v${tp.version || 1}</h3>${tp.status === "agent-prepared" ? h`${A.ui.status("agent-prepared")}${A.ui.btn({ label: "Accept template", a: "work-tp-accept", id: tp.id, small: true, primary: true, disabled: !ok, why: WFWHY })}${A.ui.btn({ label: "Discard", a: "work-tp-del", id: tp.id, small: true, disabled: !ok, why: WFWHY })}` : A.ui.status("active")}</div>
        <p class="muted small">${tp.area} · anchor: ${tp.anchor === "closing" ? "closing date (negative days are before it)" : "date applied"}</p>
        ${A.ui.table([{ label: "Item", render: (it) => A.ui.inline({ name: "wk-tp-title", id: `${tp.id}|${it.id}`, value: it.title, disabled: !ok, why: WFWHY }) },
          { label: "Due (days)", render: (it) => A.ui.inline({ name: "wk-tp-offset", id: `${tp.id}|${it.id}`, type: "number", value: it.offset, disabled: !ok, why: WFWHY }) },
          { label: "Assignee", render: (it) => A.ui.inline({ name: "wk-tp-who", id: `${tp.id}|${it.id}`, type: "select", value: it.assignee, display: (ROLES.find((r) => r[0] === it.assignee) || [, it.assignee])[1], options: ROLES, disabled: !ok, why: WFWHY }) },
          { label: "Category", render: (it) => catName(it.category) },
          { label: "", render: (it) => A.ui.btn({ label: "Remove", a: "work-tp-item-del", id: tp.id, small: true, danger: true, disabled: !ok, why: WFWHY, data: { item: it.id } }) }], tp.items)}
        <div class="row" style="margin-top:8px">${A.ui.btn({ label: "Add item", a: "work-tp-item-add", id: tp.id, small: true, disabled: !ok, why: WFWHY })}</div></div>`;
  }
  const tpItem = (key, fn) => { const [tid, iid] = key.split("|"); const tp = A.store.get("workTemplates", tid); if (!tp || !canWf()) return false; tpPut(tp, { items: tp.items.map((it) => (it.id === iid ? fn(it) : it)) }, `item ${iid}`); };
  A.inlineEdit("wk-tp-title", (k, v) => (String(v).trim() ? tpItem(k, (it) => ({ ...it, title: String(v).trim() })) : false));
  A.inlineEdit("wk-tp-offset", (k, v) => tpItem(k, (it) => ({ ...it, offset: Math.round(+v) || 0 })));
  A.inlineEdit("wk-tp-who", (k, v) => tpItem(k, (it) => ({ ...it, assignee: v })));
  A.action("work-tp-pick", (el) => { tpCur = el.dataset.id; A.refresh(); });
  A.action("work-tp-item-del", (el) => { const tp = A.store.get("workTemplates", el.dataset.id); if (!tp || !canWf()) return; tpPut(tp, { items: tp.items.filter((it) => it.id !== el.dataset.item) }, `removed ${el.dataset.item}`); });
  A.action("work-tp-item-add", (el) => { const tp = A.store.get("workTemplates", el.dataset.id); if (!tp || !canWf()) return; tpPut(tp, { items: [...tp.items, { id: A.id("ti"), title: "New item", offset: 7, assignee: "responsible", category: "admin" }] }, "added item"); });
  A.action("work-tp-accept", (el) => { const tp = A.store.get("workTemplates", el.dataset.id); if (!tp || !canWf()) return; A.store.put("workTemplates", { id: tp.id, status: "active", acceptedBy: me().id }); A.audit("work.template-accepted", { target: tp.id, note: tp.name }); A.toast("Template accepted", "ok"); });
  A.action("work-tp-del", (el) => { if (!canWf()) return; A.store.remove("workTemplates", el.dataset.id); tpCur = "tp-re"; });
  A.action("work-tp-new", () => { if (!canWf()) return;
    A.modal("New template", h`<div id="tpf" class="form"><label class="field">Name<input name="name" class="input" placeholder="e.g. Commercial lease review"></label><label class="field">Practice area<input name="area" class="input" value="Commercial"></label><label class="field">Anchor<select name="anchor" class="input">${opts([["opened", "Date applied"], ["closing", "Closing date"]], "opened")}</select></label></div>`,
      [{ label: "Cancel" }, { label: "Create", primary: true, run: () => { const v = A.formValues(document.getElementById("tpf")); if (!v.name.trim()) { A.toast("Name it", "bad"); return false; } const t = A.store.put("workTemplates", { id: A.id("tp"), name: v.name.trim(), area: v.area, anchor: v.anchor, version: 1, status: "active", items: [] }); tpCur = t.id; A.audit("work.template-created", { target: t.id, note: t.name }); } }]); });
  A.action("work-tp-draft", () => { if (!canWf()) return; if (A.store.get("workTemplates", "tp-lease")) { tpCur = "tp-lease"; return A.refresh(); }
    A.agent.run({ title: "Draft a template from closed commercial matters", steps: ["Reading closed commercial lease files", "Finding the steps that repeat", "Drafting the checklist"], done() {
      A.store.put("workTemplates", { id: "tp-lease", name: "Commercial lease review", area: "Commercial", anchor: "opened", version: 1, status: "agent-prepared", by: "agent", items: [
        { id: "ti-l1", title: "Summarize rent, term, renewal and termination clauses", offset: 2, assignee: "agent", category: "drafting" }, { id: "ti-l2", title: "Flag relocation, demolition and guarantee clauses", offset: 3, assignee: "agent", category: "research" },
        { id: "ti-l3", title: "Mark up lease and send comments to landlord's counsel", offset: 7, assignee: "responsible", category: "drafting" }, { id: "ti-l4", title: "Report to client on negotiated terms", offset: 14, assignee: "responsible", category: "client" }] });
      tpCur = "tp-lease"; return { title: "Template drafted; a person accepts it before anyone can apply it", outputs: [{ label: "Review template", go: "work/settings/templates" }] }; } }); });

  /* categories and priorities */
  function categoriesTab() {
    const ok = canWf();
    return h`<p class="muted small" style="margin:8px 0">Estimates feed the workload view and the estimate-versus-actual report.</p>
      ${A.ui.table([{ label: "Category", render: (c) => A.ui.inline({ name: "wk-cat-name", id: c.id, value: c.name, disabled: !ok, why: WFWHY }) }, { label: "Default estimate (hours)", render: (c) => A.ui.inline({ name: "wk-cat-est", id: c.id, type: "number", value: c.est, disabled: !ok, why: WFWHY }) },
        { label: "Open items", num: true, render: (c) => visWk().filter((w) => w.status !== "Done" && catOf(w) === c.id).length }], A.store.all("workCats"))}
      <div class="row" style="margin:8px 0 16px">${A.ui.btn({ label: "Add category", a: "work-cat-add", small: true, disabled: !ok, why: WFWHY })}</div>
      <h3>Priorities</h3>${A.ui.table([{ label: "Priority", key: "id" }, { label: "Response target", render: (p) => A.ui.inline({ name: "wk-pri-target", id: p.id, value: p.target, disabled: !ok, why: WFWHY }) }, { label: "Open items", num: true, render: (p) => visWk().filter((w) => w.status !== "Done" && w.priority === p.id).length }], A.store.all("workPriorities"))}`;
  }
  A.inlineEdit("wk-cat-name", (id, v) => { if (!canWf() || !String(v).trim()) return false; A.store.put("workCats", { id, name: String(v).trim() }); });
  A.inlineEdit("wk-cat-est", (id, v) => { if (!canWf() || !(v > 0)) return false; A.store.put("workCats", { id, est: +v }); A.audit("work.category", { target: id, note: `estimate ${v} h` }); });
  A.inlineEdit("wk-pri-target", (id, v) => { if (!canWf()) return false; A.store.put("workPriorities", { id, target: String(v) }); });
  A.action("work-cat-add", () => { if (!canWf()) return; A.modal("Add category", h`<div id="wcf" class="form"><label class="field">Name<input name="name" class="input"></label><label class="field">Default estimate (hours)<input name="est" type="number" min="0.25" step="0.25" class="input" value="2"></label></div>`,
    [{ label: "Cancel" }, { label: "Add", primary: true, run: () => { const v = A.formValues(document.getElementById("wcf")); if (!v.name.trim()) { A.toast("Name it", "bad"); return false; } A.store.put("workCats", { id: A.id("cat"), name: v.name.trim(), est: +v.est || 1 }); } }]); });

  /* saved filters */
  A.seed("savedFilters", [
    { id: "sf-1", name: "High priority, firm-wide", owner: "u-linda", shared: true, subs: ["u-linda"], view: "list", f: { pri: "High" } },
    { id: "sf-2", name: "14 Birchmount closing", owner: "u-yoni", shared: true, subs: ["u-yoni", "u-kaleab"], view: "board", f: { matter: "m2" } },
    { id: "sf-3", name: "With the agent", owner: "u-linda", shared: false, subs: [], view: "list", f: { who: "agent" } },
  ]);
  const visFilters = () => A.store.where("savedFilters", (f) => f.shared || f.owner === me().id);
  const fDesc = (f) => Object.entries(f.f || {}).filter(([, v]) => v).map(([k, v]) => (k === "matter" ? mName(v) : k === "who" ? (v === "me" ? "assigned to me" : v === "agent" ? "the agent" : A.user(v).name) : k === "cat" ? catName(v) : k === "pri" ? `${v} priority` : `“${v}”`)).join(" · ") || "Everything";
  function filtersTab() {
    return h`<p class="muted small" style="margin:8px 0">Save a filter from the Work page. Shared filters appear for everyone; subscribe to get a daily digest at 8:00 (Toronto). ${demo()}</p>
      ${A.ui.table([{ label: "Filter", render: (f) => h`<b>${f.name}</b>` }, { label: "Shows", render: fDesc }, { label: "Owner", render: (f) => A.user(f.owner).name },
        { label: "Shared", render: (f) => (f.owner === me().id ? A.ui.btn({ label: f.shared ? "Shared" : "Private", a: "work-sf-share", id: f.id, small: true }) : f.shared ? "Shared" : "Private") },
        { label: "Digest", render: (f) => A.ui.btn({ label: (f.subs || []).includes(me().id) ? "✓ Subscribed" : "Subscribe", a: "work-sf-sub", id: f.id, small: true, primary: (f.subs || []).includes(me().id) }) },
        { label: "", render: (f) => h`${A.ui.btn({ label: "Apply", a: "work-sf-apply", id: f.id, small: true })}${f.owner === me().id ? A.ui.btn({ label: "Delete", a: "work-sf-del", id: f.id, small: true, danger: true }) : ""}` }], visFilters(), { empty: "No saved filters", emptyHint: " Filter the Work page, then press Save filter." })}`;
  }
  A.action("work-filter-save", () => { if (!A.can("work.view")) return;
    A.modal("Save filter", h`<div id="sff" class="form"><label class="field">Name<input name="name" class="input" placeholder="e.g. My high-priority drafting"></label><label class="field"><span><input type="checkbox" name="shared"> Share with the firm</span></label></div><p class="muted small">${fDesc({ f: { q: wkQ, matter: wkMatter, who: wkWho, pri: wkPri, cat: wkCat } })}</p>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: () => { const v = A.formValues(document.getElementById("sff")); if (!v.name.trim()) { A.toast("Name it", "bad"); return false; }
        A.store.put("savedFilters", { id: A.id("sf"), name: v.name.trim(), owner: me().id, shared: !!v.shared, subs: [], view: ["list", "board"].includes(wkView) ? wkView : "list", f: { q: wkQ, matter: wkMatter, who: wkWho, pri: wkPri, cat: wkCat } }); A.toast("Filter saved", "ok"); } }]); });
  A.action("work-sf-share", (el) => { const f = A.store.get("savedFilters", el.dataset.id); if (!f || f.owner !== me().id) return; A.store.put("savedFilters", { id: f.id, shared: !f.shared }); });
  A.action("work-sf-sub", (el) => { const f = A.store.get("savedFilters", el.dataset.id); if (!f) return; const on = (f.subs || []).includes(me().id); A.store.put("savedFilters", { id: f.id, subs: on ? f.subs.filter((u) => u !== me().id) : [...(f.subs || []), me().id] }); A.toast(on ? "Unsubscribed" : "Daily digest on (demo)"); });
  A.action("work-sf-del", (el) => { const f = A.store.get("savedFilters", el.dataset.id); if (!f || f.owner !== me().id) return; A.store.remove("savedFilters", f.id); });
  A.action("work-sf-apply", (el) => { const f = A.store.get("savedFilters", el.dataset.id); if (!f) return; ({ q: wkQ = "", matter: wkMatter = "", who: wkWho = "", pri: wkPri = "", cat: wkCat = "" } = f.f || {}); wkView = f.view || "list"; A.go("work"); });

  /* external tracker sync (mock, generic connector) */
  const TRACKER_ST = ["Open", "In Progress", "In Review", "Blocked", "Done"];
  A.seed("trackerSync", [{ id: "default", connected: false, url: "", project: "", twoWay: false, mapping: { "To do": "Open", "Agent-ready": "Open", "In progress": "In Progress", "Needs approval": "In Review", Done: "Done" }, log: [], conflicts: [] }]);
  const tracker = () => A.store.get("trackerSync", "default") || { mapping: {}, log: [], conflicts: [] };
  function trackerTab() {
    const s = tracker(), ok = canWf();
    return h`<p class="muted small" style="margin:8px 0">Optional two-way sync with an issue tracker the firm or a client already uses. Restricted matters never sync. A tracker can never mark an item done while it waits for approval. ${demo()}</p>
      <div class="card"><div class="row wrap"><h3 class="grow">Issue tracker connector</h3>${s.connected ? A.ui.status("active", "Connected") : A.ui.status("inactive", "Not connected")}</div>
        ${s.connected ? h`<dl class="kv"><dt>Tracker</dt><dd>${s.url} · project ${s.project}</dd><dt>Direction</dt><dd>${A.ui.btn({ label: s.twoWay ? "Two-way" : "Push only", a: "work-trk-dir", small: true, disabled: !ok, why: WFWHY })}</dd><dt>Last sync</dt><dd>${(s.log[0] || {}).at ? A.dateTime(s.log[0].at) : "Never"}</dd></dl>
          <div class="row wrap" style="margin-top:8px">${A.ui.btn({ label: "Sync now", a: "work-trk-sync", primary: true, small: true, disabled: !ok, why: WFWHY })}${A.ui.btn({ label: "Disconnect", a: "work-trk-off", small: true, danger: true, disabled: !ok, why: WFWHY })}</div>`
          : A.ui.btn({ label: "Connect a tracker", a: "work-trk-on", disabled: !ok, why: WFWHY })}</div>
      ${s.connected ? h`<h3 style="margin-top:14px">Status mapping</h3>${A.ui.table([{ label: "Status here", render: ([k]) => k }, { label: "Status in tracker", render: ([k, v]) => A.ui.inline({ name: "wk-trk-map", id: k, type: "select", value: v, options: TRACKER_ST.map((x) => [x, x]), disabled: !ok, why: WFWHY }) }], COLS().map((c) => [c, s.mapping[c] || "Open"]))}
        <h3 style="margin-top:14px">Conflicts</h3>${A.ui.table([{ label: "Item", render: (c) => c.key }, { label: "Here", render: (c) => c.ours }, { label: "Tracker", render: (c) => c.theirs }, { label: "", render: (c) => h`${A.ui.btn({ label: "Keep ours", a: "work-trk-keep", id: c.id, small: true, disabled: !ok, why: WFWHY })}` }], s.conflicts || [], { empty: "No conflicts" })}
        <h3 style="margin-top:14px">Sync log</h3>${A.ui.table([{ label: "When", render: (x) => A.dateTime(x.at) }, { label: "Result", key: "note" }], (s.log || []).slice(0, 8), { empty: "No syncs yet" })}` : ""}`;
  }
  A.inlineEdit("wk-trk-map", (k, v) => { if (!canWf()) return false; A.store.put("trackerSync", { id: "default", mapping: { ...tracker().mapping, [k]: v } }); });
  A.action("work-trk-on", () => { if (!canWf()) return;
    A.modal("Connect a tracker", h`<div id="trkf" class="form"><label class="field">Tracker address<input name="url" class="input" value="https://tracker.example.com"></label><label class="field">Project key<input name="project" class="input" value="LKL"></label></div><p class="muted small">Demo: no connection is made. The token would be stored by the admin, never shown here.</p>`,
      [{ label: "Cancel" }, { label: "Connect", primary: true, run: () => { const v = A.formValues(document.getElementById("trkf")); if (!v.url.trim() || !v.project.trim()) { A.toast("Address and project are required", "bad"); return false; } A.store.put("trackerSync", { id: "default", connected: true, url: v.url.trim(), project: v.project.trim().toUpperCase() }); A.audit("work.tracker-connected", { note: v.url }); } }]); });
  A.action("work-trk-off", () => { if (!canWf()) return; A.store.put("trackerSync", { id: "default", connected: false }); A.audit("work.tracker-disconnected", {}); });
  A.action("work-trk-dir", () => { if (!canWf()) return; A.store.put("trackerSync", { id: "default", twoWay: !tracker().twoWay }); });
  A.action("work-trk-sync", () => { const s = tracker(); if (!s.connected || !canWf()) return;
    const items = A.store.where(WK, (w) => w.status !== "Done" && !(A.store.get("matters", w.matterId) || {}).restricted); let n = 0;
    items.forEach((w) => { if (!w.external) { n++; A.store.put(WK, { id: w.id, external: `${s.project}-${String(w.key).replace(/\D/g, "")}` }); } });
    const conflicts = s.twoWay ? items.filter((w) => w.status === "Needs approval").slice(0, 1).map((w) => ({ id: w.id, key: w.key, ours: w.status, theirs: "Done" })) : [];
    const note = `Pushed ${n} new, updated ${items.length - n}; ${conflicts.length} conflict${conflicts.length === 1 ? "" : "s"}${s.twoWay ? "" : " (push only)"}`;
    A.store.put("trackerSync", { id: "default", conflicts, log: [{ at: A.now(), note }, ...(s.log || [])].slice(0, 20) }); A.audit("work.tracker-sync", { note }); A.toast(note, "ok"); });
  A.action("work-trk-keep", (el) => { const s = tracker(); A.store.put("trackerSync", { id: "default", conflicts: (s.conflicts || []).filter((c) => c.id !== el.dataset.id) }); A.toast("Kept this app's status; the tracker is updated on the next sync"); });

  function wkSettingsPage(el, p) {
    A.setContext({ kind: "work", label: "Work settings" });
    const tab = p.tab || "workflow", T2 = { workflow: workflowTab, templates: templatesTab, categories: categoriesTab, filters: filtersTab, sync: trackerTab };
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Work settings", back: { label: "Work", to: "work" }, sub: canWf() ? "" : "Read only for your role, except your own saved filters." })}
      ${A.ui.filters({ chips: [{ id: "workflow", label: "Workflow" }, { id: "templates", label: "Templates" }, { id: "categories", label: "Categories and priorities" }, { id: "filters", label: "Saved filters", count: visFilters().length }, { id: "sync", label: "Tracker sync" }], active: tab, action: "work-set-tab" })}
      ${(T2[tab] || workflowTab)()}
    </div>`);
  }
  A.action("work-set-tab", (el) => A.go(el.dataset.id === "workflow" ? "work/settings" : `work/settings/${el.dataset.id}`));

  /* ---------- work reports ---------- */
  let repDays = 30;
  const ACTUAL = { "wk-109": 1.5, "wk-114": 5 }; // hours recorded against seeded items
  const actualOf = (w) => (w.actual != null ? w.actual : ACTUAL[w.id]);
  const doneAt = (w) => { const x = [...(w.history || [])].reverse().find((e) => /done/i.test(e.what)); return x ? x.at.slice(0, 10) : w.status === "Done" ? w.due : null; };
  function workReports(el) {
    A.setContext({ kind: "work", label: "Work reports" });
    const all = visWk(), from = A.addDays(T(), -repDays), done = all.filter((w) => w.status === "Done" && (doneAt(w) || "") >= from), openL = all.filter((w) => w.status !== "Done");
    const gates = A.store.where("approvals", (g) => g.effect === "work-approve" && g.status === "approved" && g.created && g.decidedAt && A.matter(g.matterId));
    const hrs = (g) => Math.max(0, (new Date(g.decidedAt.replace(" ", "T")) - new Date(g.created.replace(" ", "T"))) / 36e5);
    const turn = gates.map(hrs).sort((a, b) => a - b), median = turn.length ? turn[Math.floor(turn.length / 2)] : null;
    const agentDone = done.filter((w) => w.approvedBy || (w.history || []).some((x) => x.by === "agent")).length;
    const people = [{ id: "agent", name: "✦ Agent" }, ...staff()].map((u) => { const mine = all.filter((w) => w.assignee === u.id); return { id: u.id, name: u.name, open: mine.filter((w) => w.status !== "Done").length, done: done.filter((w) => w.assignee === u.id).length, overdue: mine.filter(wkOverdue).length, est: mine.filter((w) => w.status !== "Done").reduce((s, w) => s + estOf(w), 0) }; });
    const cats = A.store.all("workCats").map((c) => { const xs = done.filter((w) => catOf(w) === c.id); const act = xs.filter((w) => actualOf(w) != null); return { id: c.id, name: c.name, done: xs.length, open: openL.filter((w) => catOf(w) === c.id).length, est: xs.reduce((s, w) => s + estOf(w), 0), actual: act.length ? act.reduce((s, w) => s + actualOf(w), 0) : null }; });
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Work reports", back: { label: "Work", to: "work" }, sub: `Completed since ${fmt(from)} on matters you can see.`, actions: [{ label: "Export by person", a: "work-rep-csv", id: "people" }, { label: "Export by category", a: "work-rep-csv", id: "cats" }] })}
      ${A.ui.filters({ chips: [{ id: "30", label: "30 days" }, { id: "90", label: "90 days" }, { id: "365", label: "12 months" }], active: String(repDays), action: "work-rep-period" })}
      <div class="stats">${A.ui.stat("Completed", done.length)}${A.ui.stat("Overdue now", openL.filter(wkOverdue).length, "", openL.filter(wkOverdue).length ? "bad" : "")}${A.ui.stat("Agent share of completed", done.length ? `${Math.round((agentDone / done.length) * 100)}%` : "—")}${A.ui.stat("Median approval turnaround", median == null ? "—" : `${median.toFixed(1)} h`)}</div>
      <div class="card"><h3>By person</h3>${A.ui.table([{ label: "Person", key: "name" }, { label: "Open", key: "open", num: true }, { label: "Completed", key: "done", num: true }, { label: "Overdue", num: true, render: (r) => (r.overdue ? A.ui.status("overdue", String(r.overdue)) : "0") }, { label: "Open estimate (h)", num: true, render: (r) => r.est.toFixed(1) }], people)}</div>
      <div class="card" style="margin-top:14px"><h3>By category: estimate versus actual</h3>${A.ui.table([{ label: "Category", key: "name" }, { label: "Completed", key: "done", num: true }, { label: "Open", key: "open", num: true }, { label: "Estimate (h)", num: true, render: (r) => r.est.toFixed(1) }, { label: "Actual (h)", num: true, render: (r) => (r.actual == null ? "—" : r.actual.toFixed(1)) }], cats)}
        <p class="muted small">Actual hours come from time recorded against the item; "—" means none recorded.</p></div>
    </div>`);
    repCache = { people, cats };
  }
  let repCache = { people: [], cats: [] };
  A.action("work-rep-period", (el) => { repDays = +el.dataset.id || 30; A.refresh(); });
  A.action("work-rep-csv", (el) => (el.dataset.id === "cats" ? csv("work-by-category", [["Category", (r) => r.name], ["Completed", (r) => r.done], ["Open", (r) => r.open], ["Estimate hours", (r) => r.est], ["Actual hours", (r) => r.actual ?? ""]], repCache.cats)
    : csv("work-by-person", [["Person", (r) => r.name.replace("✦ ", "")], ["Open", (r) => r.open], ["Completed", (r) => r.done], ["Overdue", (r) => r.overdue], ["Open estimate hours", (r) => r.est]], repCache.people)));

  /* ---------- workload, team availability and rebalancing ---------- */
  A.seed("absences", [
    { id: "ab-1", user: "u-yoni", kind: "Leave", from: "2026-10-19", to: "2026-10-23", delegate: "u-linda", note: "Vacation" },
    { id: "ab-2", user: "u-ayo", kind: "Court day", from: "2026-10-14", to: "2026-10-14", delegate: "u-linda", note: "Motion, Toronto" },
    { id: "ab-3", user: "u-meraf", kind: "Part day", from: "2026-10-16", to: "2026-10-16", delegate: "u-kaleab", note: "Afternoon" },
  ]);
  const HOURS = 6; // working hours a person has for matter work in a full day
  const awayOn = (uid, d) => A.store.where("absences", (a) => a.user === uid && d >= a.from && d <= a.to)[0];
  function capRows(list) {
    const from = T(), to = A.addDays(T(), 13); let biz = 0; for (let d = from; d <= to; d = A.addDays(d, 1)) if (!closed(d)) biz++;
    const away = (uid) => { let n = 0; for (let d = from; d <= to; d = A.addDays(d, 1)) { const a = !closed(d) && awayOn(uid, d); if (a) n += a.kind === "Part day" ? 0.5 : 1; } return n; };
    const openL = list.filter((w) => w.status !== "Done" && w.due <= to);
    return staff().map((u) => { const xs = openL.filter((w) => w.assignee === u.id), cap = Math.max(0, (biz - away(u.id)) * HOURS), load = xs.reduce((s, w) => s + estOf(w), 0);
      return { id: u.id, name: u.name, items: xs.length, overdue: xs.filter(wkOverdue).length, away: away(u.id), cap, load, pct: cap ? Math.round((load / cap) * 100) : load ? 999 : 0 }; });
  }
  let rebal = [];
  A.onUser(() => { rebal = []; });
  const canAbs = (a) => A.can("work.edit", "all") || (a && a.user === me().id);
  function workload(all) {
    const rows = capRows(all), agentQ = all.filter((w) => w.assignee === "agent" && w.status !== "Done").length;
    const abs = A.store.where("absences", (a) => a.to >= T()).sort((a, b) => a.from.localeCompare(b.from));
    const loadChip = (r) => (r.pct > 100 ? A.ui.status("overdue", "Over capacity") : r.pct < 40 ? A.ui.status("open", "Has room") : A.ui.status("active", "On track"));
    return h`<p class="muted small" style="margin:8px 0">Next two weeks, net of holidays, leave, court days and part days (${HOURS} h of matter work per full day). The agent's queue has no capacity limit but each output still waits for approval.</p>
      ${A.ui.table([{ label: "Person", key: "name" }, { label: "Items due", key: "items", num: true }, { label: "Overdue", num: true, render: (r) => (r.overdue ? A.ui.status("overdue", String(r.overdue)) : "0") }, { label: "Away (days)", num: true, render: (r) => r.away || "—" },
        { label: "Load / capacity (h)", num: true, render: (r) => `${r.load.toFixed(1)} / ${r.cap}` }, { label: "", render: loadChip }], rows)}
      <p class="small">✦ Agent queue: <b>${agentQ}</b> open item${agentQ === 1 ? "" : "s"}.</p>
      <div class="card" style="margin-top:14px"><div class="row wrap"><h3 class="grow">Rebalance</h3>${A.ui.btn({ label: "✦ Suggest moves", a: "work-rebal", agent: true, disabled: !A.can("work.edit", "all"), why: "Only lawyers and the admin reassign other people's work" })}${rebal.length ? A.ui.btn({ label: `Apply all ${rebal.length}`, a: "work-rebal-all", primary: true }) : ""}</div>
        ${rebal.length ? h`${A.ui.actor("agent", "Agent suggested; a person applies each move")}${A.ui.table([{ label: "Item", render: (m) => { const w = A.store.get(WK, m.wid); return w ? `${w.key} ${w.title}` : m.wid; } }, { label: "From", render: (m) => A.user(m.from).name }, { label: "To", render: (m) => A.user(m.to).name }, { label: "Why", key: "why" },
          { label: "", render: (m) => h`${A.ui.btn({ label: "Apply", a: "work-rebal-apply", id: m.wid, small: true, primary: true })}${A.ui.btn({ label: "Dismiss", a: "work-rebal-skip", id: m.wid, small: true })}` }], rebal)}` : h`<p class="muted small">The agent looks for people over capacity and items due while their assignee is away, and suggests someone on the matter team with room.</p>`}</div>
      <div class="card" style="margin-top:14px"><div class="row wrap"><h3 class="grow">Team availability</h3>${A.ui.btn({ label: "Record absence", a: "work-abs-new", small: true })}</div>
        ${A.ui.table([{ label: "Person", render: (a) => A.user(a.user).name }, { label: "Kind", key: "kind" }, { label: "When", render: (a) => (a.from === a.to ? fmt(a.from) : `${fmt(a.from)} to ${fmt(a.to)}`) }, { label: "Delegate", render: (a) => A.user(a.delegate).name }, { label: "Note", key: "note" },
          { label: "Coverage", render: (a) => { const d = visDl().filter((x) => open(x) && x.owner === a.user && x.due >= a.from && x.due <= a.to).length, w = all.filter((x) => x.status !== "Done" && x.assignee === a.user && x.due >= a.from && x.due <= a.to).length; return `${d} deadline${d === 1 ? "" : "s"}, ${w} item${w === 1 ? "" : "s"} due while away`; } },
          { label: "", render: (a) => h`${A.ui.btn({ label: "Hand over to delegate", a: "work-abs-handover", id: a.id, small: true, disabled: !canAbs(a), why: "Not yours to change" })}${A.ui.btn({ label: "Remove", a: "work-abs-del", id: a.id, small: true, danger: true, disabled: !canAbs(a), why: "Not yours to change" })}` }], abs, { empty: "Nobody is away in the coming weeks" })}</div>`;
  }
  A.action("work-rebal", () => { if (!A.can("work.edit", "all")) return A.toast("You cannot reassign other people's work", "bad");
    A.agent.run({ title: "Suggest workload rebalancing", steps: ["Reading capacity, leave and court days", "Finding items at risk", "Matching people who can see each matter"], done() {
      const all = visWk(), rows = capRows(all), by = Object.fromEntries(rows.map((r) => [r.id, r])), moves = [];
      const target = (w, not) => { const m = A.store.get("matters", w.matterId); return rows.filter((r) => r.id !== not && r.pct < 80 && userSees(r.id, m) && !awayOn(r.id, w.due)).sort((a, b) => a.pct - b.pct)[0]; };
      all.filter((w) => w.status !== "Done" && w.assignee !== "agent" && !pendingWGate(w)).forEach((w) => {
        const a = awayOn(w.assignee, w.due), r = by[w.assignee];
        if (a) { const m = A.store.get("matters", w.matterId); const to = a.delegate && userSees(a.delegate, m) && !awayOn(a.delegate, w.due) ? { id: a.delegate } : target(w, w.assignee); if (to) moves.push({ wid: w.id, from: w.assignee, to: to.id, why: `${A.user(w.assignee).name} is away (${a.kind.toLowerCase()}) on ${fmt(w.due)}` }); }
        else if (r && r.pct > 100 && w.priority !== "High") { const to = target(w, w.assignee); if (to) { moves.push({ wid: w.id, from: w.assignee, to: to.id, why: `${r.name} is at ${r.pct}% of capacity` }); r.pct -= Math.round((estOf(w) / (r.cap || 1)) * 100); } }
      });
      rebal = moves; return { title: moves.length ? `${moves.length} move${moves.length === 1 ? "" : "s"} suggested; apply them on Work · Workload` : "No moves needed", outputs: [{ label: "Review", go: "work" }] }; } }); });
  const applyMove = (m) => { const w = A.store.get(WK, m.wid); if (!w || !canWork(w)) return false; return setFields(w, { assignee: m.to }, "Rebalanced"); };
  A.action("work-rebal-apply", (el) => { const m = rebal.find((x) => x.wid === el.dataset.id); if (!m) return; if (applyMove(m)) A.toast("Reassigned", "ok"); rebal = rebal.filter((x) => x !== m); A.refresh(); });
  A.action("work-rebal-skip", (el) => { rebal = rebal.filter((x) => x.wid !== el.dataset.id); A.refresh(); });
  A.action("work-rebal-all", () => { const n = rebal.filter(applyMove).length; rebal = []; A.toast(`${n} reassigned`, n ? "ok" : ""); A.refresh(); });
  A.action("work-abs-new", () => {
    const who = A.can("work.edit", "all") ? staff() : staff().filter((u) => u.id === me().id); if (!who.length) return A.toast("You cannot record absences", "bad");
    A.modal("Record absence", h`<div id="absf" class="form"><label class="field">Person<select name="user" class="input">${opts(who.map((u) => [u.id, u.name]), me().id)}</select></label><label class="field">Kind<select name="kind" class="input">${opts([["Leave", "Leave"], ["Court day", "Court day"], ["Part day", "Part day"], ["Other", "Other"]], "Leave")}</select></label>
      <label class="field">From<input type="date" name="from" class="input" value="${T()}"></label><label class="field">To<input type="date" name="to" class="input" value="${T()}"></label>
      <label class="field">Delegate<select name="delegate" class="input">${opts(staff().map((u) => [u.id, u.name]), "u-linda")}</select></label><label class="field">Note<input name="note" class="input"></label></div>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: () => { const v = A.formValues(document.getElementById("absf")); if (!v.from || !v.to || v.to < v.from) { A.toast("Check the dates", "bad"); return false; } if (v.delegate === v.user) { A.toast("Pick someone else as delegate", "bad"); return false; }
        A.store.put("absences", { id: A.id("ab"), ...v }); A.audit("work.absence", { target: v.user, note: `${v.kind} ${v.from}–${v.to}` }); } }]); });
  A.action("work-abs-del", (el) => { const a = A.store.get("absences", el.dataset.id); if (!a || !canAbs(a)) return; A.store.remove("absences", a.id); A.audit("work.absence-removed", { target: a.user }); });
  A.action("work-abs-handover", (el) => { const a = A.store.get("absences", el.dataset.id); if (!a || !canAbs(a)) return;
    const xs = visWk().filter((w) => w.status !== "Done" && w.assignee === a.user && w.due >= a.from && w.due <= a.to && !pendingWGate(w)); let n = 0, skip = 0;
    xs.forEach((w) => { if (canWork(w) && userSees(a.delegate, A.store.get("matters", w.matterId))) { if (setFields(w, { assignee: a.delegate }, `Handed over during ${a.kind.toLowerCase()}`)) n++; } else skip++; });
    A.toast(`${n} item${n === 1 ? "" : "s"} handed to ${A.user(a.delegate).name}${skip ? `; ${skip} kept (delegate cannot see the matter or not yours to move)` : ""}`, n ? "ok" : ""); });
  /* ---------- agent intents ---------- */
  A.intent({ match: /due (this|next) week|what('?s| is) due/i, label: "What is due this week", example: "what is due this week", run() {
    if (!A.can("deadlines.view")) return { text: "You do not have access to deadlines." };
    const xs = visDl().filter((d) => open(d) && A.daysBetween(T(), d.due) <= 7).sort((a, b) => a.due.localeCompare(b.due));
    const ws = visWk().filter((w) => w.status !== "Done" && A.daysBetween(T(), w.due) <= 7);
    if (!xs.length && !ws.length) return { text: "Nothing is due in the next 7 days." };
    return { text: `${xs.length} deadline${xs.length === 1 ? "" : "s"} in the next 7 days: ${xs.map((d) => `${d.title} (${fmt(d.due)}, ${STATUS[d.status][0].toLowerCase()})`).join("; ") || "none"}. ${ws.length} work item${ws.length === 1 ? "" : "s"} due.`,
      cites: xs.filter((d) => d.source).map((d) => cite(d.source)), actions: [...xs.slice(0, 3).map((d) => ({ label: d.title, go: `deadlines/${d.id}` })), { label: "Open deadlines", go: "deadlines" }] };
  } });
  A.intent({ match: /defen[cs]e deadline/i, label: "Compute the defence deadline", example: "compute the defence deadline", run() {
    const m = A.matter("m1"); if (!m || !A.canMatter(m, "deadlines.view")) return { text: "You do not have access to that matter's deadlines." };
    const d = A.store.get(DL, "dl-1");
    return { text: `Northgate Storage was served personally on Sep 23, 2026. Under r.18.01(a) a defendant served in Ontario has 20 days: due Tue Oct 13, 2026. Mon Oct 12 is Thanksgiving, so the office is closed the day before. Status: ${d ? STATUS[d.status][0] : "not recorded"}. Verify the rule, then confirm.`,
      cites: [cite({ doc: "d-aos", page: 1, line: 3 }), cite({ doc: "d-aos", page: 1, line: 5 }), cite({ doc: "d-email-osei", page: 1, line: 5 })], actions: d ? [{ label: "Open deadline", go: `deadlines/${d.id}` }] : [] };
  } });
  A.intent({ match: /assign (.+?) to (the )?agent/i, label: "Assign a task to the agent", example: "assign the requisition letter to the agent", run(text, ctx) {
    if (!A.can("work.edit", "act")) return { text: "You cannot assign work." };
    const q = /assign (.+?) to (the )?agent/i.exec(text)[1].replace(/^(the|a)\s+/i, "").toLowerCase();
    const hit = visWk().find((w) => w.status !== "Done" && w.title.toLowerCase().includes(q));
    if (hit) { if (!canWork(hit)) return { text: `${hit.key} is not yours to reassign.` }; return { text: `${hit.key} takes several steps. Here is the plan; the output waits for approval before it counts as done.`, preview: agentTask(hit.id, true) }; }
    const mid = ctx && (ctx.kind === "matter" ? ctx.id : null); const m = mid && A.matter(mid);
    if (!m) return { text: "Open the matter first, or name an existing work item.", actions: [{ label: "Open work", go: "work" }] };
    const n = Math.max(...A.store.all(WK).map((w) => +String(w.key).replace(/\D/g, "") || 0), 100) + 1;
    const w = A.store.put(WK, { id: A.id("wk"), key: `WRK-${n}`, matterId: m.id, title: q.charAt(0).toUpperCase() + q.slice(1), status: "To do", assignee: "agent", due: A.addDays(T(), 3), priority: "Normal", comments: [], history: [{ at: A.now(), by: me().id, what: "Created from chat" }] });
    return { text: `Created ${w.key} on ${m.number}. Here is the plan; press Run to hand it to the agent.`, preview: agentTask(w.id, true), actions: [{ label: w.key, go: `work/${w.id}` }] };
  } });
  A.suggest("matter", () => ["What is due this week", "Assign the next task to the agent"]);
  A.suggest("deadlines", () => ["What is due this week", "Compute the defence deadline"]);
  A.suggest("deadline", () => ["Compute the defence deadline", "What is due this week"]);
  A.suggest("work", () => ["Assign the requisition letter to the agent", "What is due this week"]);
  A.suggest("home", () => (A.can("deadlines.view") ? ["What is due this week"] : []));
})();
