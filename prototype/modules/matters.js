/* Matters and contacts: directory, board, matter workspace (ask box, brief, overview, documents with status/tags/lock/history,
   chronology behind a confirm gate, filed email, notes), unfiled email queue, cited doc viewer, agent drafting and
   tracked-change revisions behind release gates, contacts with conflict aliases and gated merges. */
(function () {
  "use strict";
  const A = window.App, h = A.h, ui = A.ui;

  /* ---------- module state (UI only) ---------- */
  const st = { view: "list", area: "", resp: "", mine: false, q: "", tab: {}, doc: {}, cq: "", ask: {}, mail: {}, an: {}, bundle: {}, lib: "firm", sigSel: "", tplSel: "", asm: {} };
  A.onUser(() => { st.sel = null; st.csel = null; st.doc = {}; st.an = {}; });

  /* ---------- seed: notes and contact aliases ---------- */
  A.seed("notes", [
    { id: "n-1", matterId: "m1", by: "u-ayo", at: "2026-10-08 16:20", text: "Osei called ahead of his email. He sounded open to early mediation if the inventory valuation holds up." },
    { id: "n-2", matterId: "m1", by: "u-meraf", at: "2026-09-24 11:05", text: "Affidavit of service sworn and filed. Defence due 20 days from service." },
    { id: "n-3", matterId: "m2", by: "u-kaleab", at: "2026-10-02 09:40", text: "Title search ordered. Waiting on the bank's mortgage instructions." },
    { id: "n-4", matterId: "m5", by: "u-ayo", at: "2026-09-12 14:30", text: "Client has the termination letter and six years of pay stubs. Mitigation log started." },
    { id: "n-5", matterId: "m7", by: "u-linda", at: "2026-09-30 10:15", text: "Examination for discovery of Orion's installer set for November." },
  ]);
  A.seed("aliases", [
    { id: "al-1", contactId: "c-hanna", alias: "Hanna T. Gebre", note: "Former name (before 2022)" },
    { id: "al-2", contactId: "c-northgate", alias: "Northgate Self Storage", note: "Operating name on the storage agreement" },
    { id: "al-3", contactId: "c-northgate", alias: "Northgate Holdings Group", note: "Parent company; also parent of Northgate Logistics Inc." },
    { id: "al-4", contactId: "c-nglog", alias: "Northgate Holdings Group", note: "Parent company; also parent of Northgate Storage Inc." },
    { id: "al-5", contactId: "c-orion", alias: "Orion Signage & Display", note: "Trade name used on invoices" },
    { id: "al-6", contactId: "c-okafor", alias: "Ruth Adeyemi-Okafor", note: "Name on the mortgage commitment" },
  ]);

  /* Document metadata kept beside the shared docs: status, tags, received date, lock. */
  A.seed("docmeta", [
    { id: "d-claim", status: "filed", tags: ["pleading", "damages"], received: "" },
    { id: "d-aos", status: "filed", tags: ["service"], received: "" },
    { id: "d-email-osei", status: "final", tags: ["correspondence", "extension"], received: "2026-10-08" },
    { id: "d-aps", status: "final", tags: ["closing"], received: "2026-09-15" },
    { id: "d-will", status: "final", tags: ["original will"], received: "2026-08-20", locked: true, lockedBy: "u-linda", lockedAt: "2026-08-20 15:10" },
    { id: "d-term", status: "final", tags: ["termination"], received: "2026-09-10" },
    { id: "d-incident", status: "final", tags: ["liability"], received: "2026-10-05" },
    { id: "d-lease", status: "draft", tags: ["lease", "negotiation"], received: "2026-07-28" },
  ]);
  /* Responsible staff: the non-lawyer who runs the file day to day (separate from the responsible lawyer). */
  A.seed("matterstaff", [
    { id: "m1", staff: "u-meraf" }, { id: "m2", staff: "u-kaleab" }, { id: "m3", staff: "u-meraf" }, { id: "m4", staff: "u-meraf" },
    { id: "m6", staff: "u-kaleab" }, { id: "m8", staff: "u-kaleab" },
  ]);
  /* Email: filed to a matter, or waiting in the firm's unfiled queue. Removed messages leave a tombstone so a re-sync skips them. */
  const INBOX = [
    { id: "em-u1", msgId: "<ngh-4471@northgateholdings.example.ca>", from: "Records Office, Northgate Holdings Group", fromEmail: "records@northgateholdings.example.ca", to: "u-linda", date: "2026-10-09 08:12",
      subject: "Records request for our subsidiary", lines: ["We received your request for records concerning our subsidiary.", "Please confirm which Northgate company and which file number the request relates to.", "We will respond within 30 days of that confirmation."] },
    { id: "em-u2", msgId: "<okf-2210@example.ca>", from: "Samuel and Ruth Okafor", fromEmail: "okafor.family@example.ca", to: "u-yoni", date: "2026-10-08 19:44",
      subject: "Mortgage commitment for Birchmount", lines: ["The bank sent the final mortgage commitment today. It is attached.", "The appraisal came in at the purchase price, so no change to the down payment."] },
    { id: "em-u3", msgId: "<tsh-0918@example.ca>", from: "Tsion Haile", fromEmail: "tsion.h@example.ca", to: "u-ayo", date: "2026-10-09 07:30",
      subject: "Interview next week", lines: ["I have a second interview with a distribution company on October 15.", "I added it to my job search log. Should I mention the claim if they ask?"] },
    { id: "em-u4", msgId: "<cpd-88120@lawsociety-notices.example.ca>", from: "CPD Notices", fromEmail: "notices@lawsociety-notices.example.ca", to: "u-linda", date: "2026-10-07 06:00",
      subject: "Reminder: professionalism hours due December 31", lines: ["Licensees must complete three professionalism hours by December 31.", "This is an automated reminder."] },
    { id: "em-u5", msgId: "<dft-3301@example.ca>", from: "Daniel Fortier", fromEmail: "d.fortier@example.ca", to: "u-ayo", date: "2026-10-09 09:05",
      subject: "Physio report from last week", lines: ["My physiotherapist wrote up her assessment after six sessions.", "She says the shoulder will need another eight weeks."] },
  ];
  A.seed("emails", [
    { id: "em-1", msgId: "<bv-1201@bramptonvale.example.ca>", matterId: "m1", from: "Daniel Osei, Brampton & Vale LLP", fromEmail: "dosei@bramptonvale.example.ca", to: "u-linda", date: "2026-10-08 14:02",
      subject: "Tesfaye v. Northgate Storage: extension", lines: ["We have just been retained by Northgate Storage.", "We ask for your consent to a 30-day extension to deliver the statement of defence.", "We also request copies of the storage agreement and the inventory invoices."], filedBy: "u-meraf" },
    { id: "em-2", msgId: "<ht-7781@example.ca>", matterId: "m1", from: "Hanna Tesfaye", fromEmail: "hanna.tesfaye@example.ca", to: "u-ayo", date: "2026-10-03 10:15",
      subject: "Remaining supplier invoices", lines: ["I found the last 22 invoices from the textile supplier in my office.", "Together they cover all 140 cartons. Scans attached."], filedBy: "agent" },
    { id: "em-3", msgId: "<tsh-0901@example.ca>", matterId: "m5", from: "Tsion Haile", fromEmail: "tsion.h@example.ca", to: "u-ayo", date: "2026-09-20 18:40",
      subject: "Job search log, first three weeks", lines: ["Attached is my log: 14 applications and two interviews so far.", "Northgate has not sent my record of employment yet."], filedBy: "agent" },
    { id: "em-4", msgId: "<lk-5502@lindaklaw.example.ca>", matterId: "m5", from: "Ayo Ewebiyi, Linda K Law", fromEmail: "ayo@lindaklaw.example.ca", to: "u-ayo", date: "2026-09-25 11:00",
      subject: "Request for personnel file and record of employment", lines: ["We act for Tsion Haile.", "Please send her personnel file, her record of employment and the bonus plan in effect in 2026."], filedBy: "u-ayo" },
  ].concat(INBOX.slice(0, 4).map((e) => ({ ...e, matterId: null }))));
  A.seed("mailtombs", []);

  /* ---------- helpers ---------- */
  const STAGES = ["Intake complete", "Demand", "Pleadings", "Discoveries", "Review", "Requisitions", "Probate application", "Closed"];
  const canSee = (m) => A.canMatter(m, "matters.view");
  const canDocs = (m) => A.canMatter(m, "documents.view");
  const dirMatters = () => A.matters(null, "matters.directory");
  const docsOf = (m) => A.store.where("docs", (d) => d.matterId === m.id);
  const words = (s) => String(s).toLowerCase().split(/[^a-z0-9$]+/).filter((w) => w.length > 3).map((w) => w.replace(/s$/, ""));
  const contactName = (id) => A.contact(id).name;
  const sentences = (s) => String(s || "").split(/(?<=\.)\s+(?=[A-Z])/).filter(Boolean);
  const lines = (d) => d.pages.flatMap((p, pi) => p.map((t, li) => ({ doc: d.id, page: pi + 1, line: li + 1, t })));
  const cite = (l, d) => ({ doc: l.doc, page: l.page, line: l.line, quote: l.t, label: d ? d.name : l.doc });
  /** Best supporting line in the matter's docs for a sentence (word overlap). */
  function support(m, text) {
    const q = new Set(words(text)); let best = null, score = 0;
    docsOf(m).forEach((d) => lines(d).forEach((l) => { const s = words(l.t).filter((w) => q.has(w)).length; if (s > score) { score = s; best = cite(l, d); } }));
    return score >= 2 || (score >= 1 && q.size <= 4) ? best : null;
  }
  const findLine = (m, re) => { for (const d of docsOf(m)) for (const l of lines(d)) if (re.test(l.t)) return cite(l, d); return null; };
  const roleOn = (c, m) => (m.clientId === c.id ? "Client" : (m.opposing || []).includes(c.id) ? A.contact(c.id).role || "Opposing party" : null);
  const cap1 = (s) => s.charAt(0).toUpperCase() + s.slice(1);
  const no = (why) => A.raw(`disabled title="${A.esc(why)}"`);
  const roleWhy = (what) => `${A.roleName(A.me().role)} cannot ${what} on this matter`;
  const canEditDocs = (m) => A.canMatter(m, "documents.edit") && A.can("documents.edit", "act");
  const canEditMatter = (m) => A.canMatter(m, "matters.edit") && A.can("matters.edit", "act");
  const staffOf = (m) => (A.store.get("matterstaff", m.id) || {}).staff || "";
  const STAFF_ROLES = ["paralegal", "clerk"];
  /** Document metadata with defaults for documents created later (agent releases are final). */
  const meta = (d) => ({ status: d.approvedBy ? "final" : "draft", tags: [], received: "", locked: false, ...(A.store.get("docmeta", d.id) || {}) });
  const lockedByOther = (d) => { const x = meta(d); return x.locked && x.lockedBy !== A.me().id; };

  /* Dates in document text, for the chronology. */
  const MONTHS = ["january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"];
  const DATE_RE = /\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+(\d{1,2}),\s+(\d{4})\b|\b(\d{4})-(\d{2})-(\d{2})\b/g;
  function datesIn(t) {
    const out = []; let x; DATE_RE.lastIndex = 0;
    while ((x = DATE_RE.exec(t))) out.push(x[1] ? `${x[3]}-${String(MONTHS.indexOf(x[1].toLowerCase()) + 1).padStart(2, "0")}-${x[2].padStart(2, "0")}` : `${x[4]}-${x[5]}-${x[6]}`);
    return out;
  }
  /** Agent-built chronology: every dated line in the matter's documents becomes a cited event. */
  function extractChronology(m) {
    const rows = [];
    docsOf(m).forEach((d) => lines(d).forEach((l) => datesIn(l.t).forEach((iso) => {
      let text = l.t.replace(/^\d+\.\s*/, "").replace(/^(Date|Date of incident):\s*/i, "");
      if (text.replace(DATE_RE, "").replace(/[\s,.]|a\.m|p\.m|approx|\d|:/gi, "").length < 4) text = `${d.name} dated`;
      if (!rows.some((r) => r.date === iso && r.cite.doc === d.id)) rows.push({ id: A.id("ev"), date: iso, text, cite: cite(l, d) });
    })));
    return rows.sort((a, b) => a.date.localeCompare(b.date));
  }

  /* Question answering over the matter's documents. */
  const STOP = new Set(["what", "when", "where", "which", "does", "have", "with", "that", "this", "there", "about", "from", "they", "their", "should", "would", "could", "will", "were", "much", "many", "tell"]);
  function answerFor(m, q) {
    const qw = new Set(words(q).filter((w) => !STOP.has(w))); const when = /\bwhen|date|deadline\b/i.test(q), amount = /how much|amount|\$|price|value|damages/i.test(q);
    if (!qw.size) return { text: "Ask about something in this matter's documents, such as a date, an amount or a party.", cites: [] };
    const scored = [];
    docsOf(m).forEach((d) => lines(d).forEach((l) => { let s = words(l.t).filter((w) => qw.has(w)).length; if (!s) return;
      if (when && datesIn(l.t).length) s += 1.5; if (amount && /\$/.test(l.t)) s += 1.5; scored.push({ s, c: cite(l, d) }); }));
    scored.sort((a, b) => b.s - a.s);
    const top = scored.filter((x) => x.s >= Math.min(2, qw.size)).slice(0, 3);
    if (!top.length) return { text: "The documents on this matter do not answer that. I will not guess without a source.", cites: [] };
    const clean = (t) => t.replace(/^\d+\.\s*/, "");
    return { text: `${A.store.get("docs", top[0].c.doc).name} says: “${clean(top[0].c.quote)}”${top.length > 1 ? ` Related: ${top.slice(1).map((x) => `“${clean(x.c.quote)}”`).join(" ")}` : ""}`, cites: top.map((x) => x.c) };
  }

  /* Word-level tracked changes between an original line and the agent's revision. */
  function trackedLine(a, b) {
    const x = a.split(" "), y = b.split(" "), n = x.length, k = y.length;
    const L = Array.from({ length: n + 1 }, () => new Array(k + 1).fill(0));
    for (let i = n - 1; i >= 0; i--) for (let j = k - 1; j >= 0; j--) L[i][j] = x[i] === y[j] ? L[i + 1][j + 1] + 1 : Math.max(L[i + 1][j], L[i][j + 1]);
    const ops = []; let i = 0, j = 0;
    const push = (kind, t) => { const last = ops[ops.length - 1]; if (last && last.kind === kind) last.t.push(t); else ops.push({ kind, t: [t] }); };
    while (i < n && j < k) { if (x[i] === y[j]) { push("eq", x[i]); i++; j++; } else if (L[i + 1][j] >= L[i][j + 1]) push("del", x[i++]); else push("ins", y[j++]); }
    while (i < n) push("del", x[i++]); while (j < k) push("ins", y[j++]);
    return h`${ops.map((o, n2) => { const t = o.t.join(" "); const sp = n2 < ops.length - 1 ? " " : "";
      return o.kind === "eq" ? h`${t}${sp}` : o.kind === "del" ? h`<del style="background:var(--bad-soft);color:var(--bad)">${t}</del>${sp}` : h`<ins style="background:var(--ok-soft);color:var(--ok);text-decoration:underline">${t}</ins>${sp}`; })}`;
  }
  /** Firm playbook positions the agent applies when revising a draft (tenant-side commercial lease points). */
  const PLAYBOOK = [
    [/^Rent abatement: none for the period of roof repairs in 2025\.$/, "Rent abatement: full base rent abated for the period of roof repairs in 2025."],
    [/^Tenant is responsible for HVAC repairs regardless of cause\.$/, "Tenant is responsible for HVAC repairs, except repairs caused by the landlord or by a structural defect."],
    [/^Landlord may relocate tenant on 60 days' notice\.$/, "Landlord may relocate tenant only on 180 days' notice, at the landlord's cost, to comparable premises in the plaza."],
    [/^Personal guarantee required from the principal of the tenant\.$/, "Personal guarantee from the principal of the tenant, limited to 12 months' base rent."],
    [/\bapprox\.\s/, (t) => t.replace(/\bapprox\.\s/, "approximately ")],
  ];
  function proposeRevision(d) {
    const changes = []; const pages = d.pages.map((p, pi) => p.map((t, li) => {
      for (const [re, to] of PLAYBOOK) if (re.test(t)) { const nt = typeof to === "function" ? to(t) : to; if (nt !== t) { changes.push({ page: pi + 1, line: li + 1, from: t, to: nt }); return nt; } }
      return t; }));
    return { pages, changes };
  }

  /* Email filing: the agent scores each message against matters the user can see; it suggests, a person files. */
  function mailSuggest(e) {
    const scores = A.matters((m) => m.status === "open").map((m) => {
      let s = 0; const why = [];
      const people = [m.clientId].concat(m.opposing || []).map(A.contact);
      if (people.some((c) => c.email && c.email === e.fromEmail)) { s += 5; why.push("sender is a party"); }
      const text = `${e.subject} ${e.lines.join(" ")}`.toLowerCase();
      if (text.includes(m.number.toLowerCase())) { s += 5; why.push("file number"); }
      const tw = new Set(words(text));
      const nameHits = words(m.name).filter((w) => !["purchase", "estate", "review"].includes(w) && tw.has(w));
      if (nameHits.length) { s += nameHits.length * 1.5; why.push(`mentions ${nameHits.join(", ")}`); }
      const orgHits = people.filter((c) => c.kind === "org" && tw.has(words(c.name)[0]));
      if (orgHits.length) { s += 1; if (!nameHits.length) why.push(`mentions ${orgHits[0].name.split(" ")[0]}`); }
      return { m, s, why };
    }).filter((x) => x.s > 0).sort((a, b) => b.s - a.s);
    const top = scores[0], next = scores[1];
    const clear = !!top && top.s >= 4 && (!next || top.s - next.s >= 3);
    return { clear, top, candidates: scores.slice(0, 4) };
  }
  const canQueue = () => A.me().role !== "client" && A.can("documents.edit", "act");

  /** The agent's matter brief: every sentence carries a citation into the matter's documents. */
  function brief(m) {
    const out = [];
    if (!canDocs(m)) return out;
    sentences(m.summary).forEach((s) => { const c = support(m, s); if (c) out.push({ text: s, cites: [c] }); });
    docsOf(m).forEach((d) => {
      if (out.some((o) => o.cites.some((c) => c.doc === d.id))) return;
      const ls = lines(d); const key = ls.find((l) => /\$|ask|request|terminat|appoint|slipped|price|rent|serv/i.test(l.t)) || ls[Math.min(3, ls.length - 1)];
      out.push({ text: `${d.name} (${A.date(d.added)}): ${key.t.replace(/^\d+\.\s*/, "")}`, cites: [cite(key, d)] });
    });
    return out;
  }
  const asks = (m) => docsOf(m).filter((d) => d.kind === "email").flatMap((d) => lines(d).filter((l) => /\b(ask|request)/i.test(l.t)).map((l) => cite(l, d)));

  function nextSteps(m) {
    const s = [];
    asks(m).forEach((c) => s.push({ text: `Respond to opposing counsel: ${c.quote.replace(/^We (also )?(ask|request) (for )?/i, "")}`, cite: c }));
    const by = {
      Pleadings: "Diarize the defence deadline and confirm whether to consent to any extension.",
      Requisitions: "Send requisitions to the seller's lawyer before the requisition date.",
      "Intake complete": "Request the cleaning logs and platform video before they are overwritten.",
      Demand: "Send a demand letter setting out common-law reasonable notice.",
      "Probate application": "Complete the application for a certificate of appointment and estate information return.",
      Review: "Report to the client on the abatement, HVAC and relocation clauses.",
      Discoveries: "Prepare the affidavit of documents and examination outline.",
      Closed: "Nothing open. File is in retention.",
    };
    const t = by[m.stage]; if (t) s.push({ text: t, cite: support(m, t) });
    return s;
  }

  /* ---------- shared list helpers: status chips (App.STATUS), split view ---------- */
  /** Map this module's record states onto the shared chip vocabulary (ui.status); labels stay plain. */
  const CHIP = { open: ["open"], pending: ["needs-approval"], closing: ["in-progress", "Closing"], closed: ["closed"], archived: ["archived"],
    draft: ["draft"], final: ["complete", "Final"], filed: ["filed"], verified: ["confirmed", "ID verified"], identified: ["unconfirmed", "ID pending"], restricted: ["blocked", "Restricted"],
    deleted: ["archived", "Deleted"], signed: ["complete", "Signed"], sent: ["sent"], viewed: ["in-progress", "Viewed"], voided: ["declined", "Voided"], approved: ["approved"], private: ["draft", "Private"] };
  const chip = (s) => { const c = CHIP[s]; return c ? ui.status(c[0], c[1]) : ui.status(s || "", cap1(String(s || ""))); };
  /** Per-area stage lists, editable under Practice areas (a matter keeps its current stage even if the list changes). */
  const AREA_DEFAULTS = {
    "Civil litigation": ["Intake complete", "Demand", "Pleadings", "Discoveries", "Mediation", "Pre-trial", "Trial", "Closed"],
    "Personal injury": ["Intake complete", "Demand", "Pleadings", "Discoveries", "Mediation", "Settlement", "Closed"],
    "Real estate": ["Intake complete", "Agreement review", "Requisitions", "Closing", "Reporting letter", "Closed"],
    Estates: ["Intake complete", "Probate application", "Administration", "Accounts", "Distribution", "Closed"],
    Employment: ["Intake complete", "Demand", "Negotiation", "Pleadings", "Closed"],
    Commercial: ["Intake complete", "Review", "Negotiation", "Signing", "Closed"],
  };
  const AREA_FIELDS = { "Civil litigation": ["Court file number", "Court location", "Limitation date"], "Personal injury": ["Date of incident", "Insurer", "Claim number", "Limitation date"],
    "Real estate": ["Property address", "Closing date", "Purchase price", "Lender"], Estates: ["Date of death", "Estate trustee", "Estimated estate value"],
    Employment: ["Employer", "Termination date", "Years of service"], Commercial: ["Counterparty", "Agreement type", "Term"] };
  const AREA_PLANS = { "Civil litigation": ["Diarize limitation date", "Draft statement of claim", "Serve and file"], "Personal injury": ["Request accident benefits file", "Preserve video and logs", "Notice to defendant within 10 days"],
    "Real estate": ["Title search", "Requisition letter", "Closing funds request"], Estates: ["Locate original will", "Certificate of appointment application", "Estate information return"],
    Employment: ["Collect termination letter", "Mitigation log", "Demand letter"], Commercial: ["Issues list", "Client report on key clauses", "Execution version"] };
  A.seed("practiceareas", Object.keys(AREA_DEFAULTS).map((n, i) => ({ id: `pa-${i + 1}`, name: n, stages: AREA_DEFAULTS[n], fields: AREA_FIELDS[n], plan: AREA_PLANS[n], conflictRoles: ["Client", "Opposing party", "Opposing counsel"].concat(n === "Estates" ? ["Beneficiary"] : n === "Real estate" ? ["Lender"] : []) })));
  const areaRec = (name) => A.store.all("practiceareas").find((a) => a.name === name);
  const stagesFor = (m) => { const a = areaRec(m.area); const l = (a && a.stages) || STAGES; return l.includes(m.stage) ? l : l.slice(0, -1).concat(m.stage, l.slice(-1)); };
  const pendingOn = (m) => A.store.where("approvals", (g) => g.status === "pending" && g.matterId === m.id);
  const lastActivity = (m) => { const a = A.store.all("audit").find((x) => x.matterId === m.id || x.target === m.id); const n = A.store.where("notes", (x) => x.matterId === m.id).map((x) => x.at).sort().pop();
    const t = [a && a.at, n].filter(Boolean).sort().pop(); return t || m.opened; };
  const ago = (t) => { const d = A.daysBetween(String(t).slice(0, 10), A.today); return d <= 0 ? "today" : d === 1 ? "yesterday" : `${d} days ago`; };
  const demo = h`<span class="muted small" title="Prototype: no backend behind this screen">demo</span>`;
  const sid = (name) => A.selection(name).ids();

  /* ---------- matter list ---------- */
  st.saved = "open"; st.sel = "";
  const SAVED = [{ id: "mine", label: "Mine" }, { id: "open", label: "Open" }, { id: "waiting", label: "Needs approval" }, { id: "closed", label: "Closed" }, { id: "all", label: "All" }];
  const savedFn = { mine: (m) => (m.team || []).includes(A.me().id) && m.status !== "closed", open: (m) => m.status === "open", waiting: (m) => canSee(m) && pendingOn(m).length > 0, closed: (m) => m.status === "closed", all: () => true };
  function filtered() {
    const q = st.q.toLowerCase(), f = savedFn[st.saved] || savedFn.all;
    return dirMatters().filter((m) => f(m) && (!st.area || m.area === st.area) && (!st.resp || m.responsible === st.resp)
      && (!q || `${m.number} ${m.name} ${canSee(m) ? contactName(m.clientId) : ""} ${m.area}`.toLowerCase().includes(q)));
  }
  function matterPreview(m) {
    if (!m) return null;
    if (!canSee(m)) return h`<div class="card"><div class="muted small" style="font-family:var(--mono)">${m.number}</div><h3>${m.name}</h3><p class="muted">Directory only: your role sees the name and number, not the contents.</p></div>`;
    const b = brief(m).slice(0, 2), waits = pendingOn(m), can = canEditMatter(m), runs = A.store.where("runs", (r) => r.matterId === m.id && (r.status === "running" || r.status === "waiting"));
    return h`<div class="card"><div class="row"><span class="muted small grow" style="font-family:var(--mono)">${m.number} · ${m.area}</span>${waits.length ? chip("pending") : chip(m.status)}</div>
      <h3 style="margin:4px 0 2px">${m.name}</h3>
      <div class="muted small">${contactName(m.clientId)} · ${A.user(m.responsible).name}${staffOf(m) ? ` · ${A.user(staffOf(m)).name}` : ""}</div>
      <div class="row wrap" style="margin:8px 0"><span class="small">Stage</span>
        <select class="input" style="width:auto" data-c="matters-stage" data-id="${m.id}" aria-label="Stage" ${can ? "" : no(roleWhy("move the stage"))}>${stagesFor(m).map((s) => h`<option ${s === m.stage ? A.raw("selected") : ""}>${s}</option>`)}</select></div>
      ${waits.length ? h`<div class="mx-todo small">${ui.actor("person", `${waits.length} approval${waits.length > 1 ? "s" : ""} waiting`)}
        ${waits.slice(0, 3).map((g) => h`<div><a href="#" data-a="go" data-to="approvals/${g.id}">${g.title}</a></div>`)}</div>` : ""}
      ${runs.length ? h`<div class="small" style="margin-bottom:8px">${ui.actor("agent", `Agent working: ${runs.map((r) => r.title).join("; ")}`)}</div>` : ""}
      ${b.length ? h`<div class="small">${b.map((s) => h`<p style="margin:0 0 6px">✦ ${s.text} ${s.cites.map(ui.cite)}</p>`)}</div>` : ""}
      <div class="muted small" style="margin-bottom:8px">Last activity ${ago(lastActivity(m))}</div>
      <div class="row"><span class="grow"></span>${ui.btn({ label: "Open", a: "matters-open", id: m.id, primary: true, small: true })}</div></div>`;
  }
  function boardHtml(rows) {
    if (!rows.length) return ui.empty("No matters match", " Clear a filter to see more.");
    const order = st.area && areaRec(st.area) ? areaRec(st.area).stages : STAGES;
    const lanes = order.filter((s) => st.area || rows.some((m) => m.stage === s)).concat([...new Set(rows.map((m) => m.stage))].filter((s) => !order.includes(s)));
    return h`<p class="muted small" style="margin:0 0 8px">Drag a card to another stage, or press Move on it. Moves to Closed go to a lawyer for approval.${st.area ? "" : " Pick a practice area to see its full stage list."}</p>
      <div class="board" id="mx-board">${lanes.map((s) => { const ms = rows.filter((m) => m.stage === s);
      return h`<div class="lane" data-stage="${s}"><h4>${s}<span>${ms.length}</span></h4>${ms.map((m) => canSee(m)
        ? h`<div class="kcard" draggable="${canEditMatter(m) ? "true" : "false"}" data-mid="${m.id}" data-a="matters-open" data-id="${m.id}"><b>${m.name}</b><small>${m.number} · ${contactName(m.clientId)}</small><div class="row" style="margin-top:6px">${(m.team || []).map(ui.avatar)}${m.restricted ? chip("restricted") : ""}${pendingOn(m).length ? chip("pending") : ""}<span class="grow"></span>${canEditMatter(m) ? ui.btn({ label: "Move", a: "matters-move", id: m.id, small: true, title: "Move to another stage (m)" }) : ""}</div></div>`
        : h`<div class="kcard" title="Your role sees the directory only"><b>${m.name}</b><small>${m.number} · Directory only</small></div>`)}</div>`; })}</div>`;
  }
  function listPage(el) {
    A.setContext({ kind: "matters", label: "Matters" });
    const all = dirMatters(); const dirOnly = !A.can("matters.view");
    const areas = [...new Set(all.map((m) => m.area))].sort(); const resps = [...new Set(all.map((m) => m.responsible))];
    const canOpen = A.can("matters.open", "act") && A.can("intake.leads");
    const rows = filtered();
    if (!rows.some((m) => m.id === st.sel)) st.sel = rows[0] ? rows[0].id : "";
    const S = A.selection("matters"); S.ids().forEach((id) => { if (!all.some((m) => m.id === id)) S.toggle(id); });
    const setup = A.can("platform.firm_profile", "act") || A.can("matters.open", "act");
    const body = st.view === "board" ? boardHtml(rows) : ui.split(ui.table([
      { label: "Number", render: (m) => h`<span style="font-family:var(--mono)">${m.number}</span>` },
      { label: "Matter", render: (m) => h`<b>${m.name}</b>${m.restricted ? h` ${chip("restricted")}` : ""}${canSee(m) ? "" : h` ${ui.status("directory", "Directory only")}`}` },
      { label: "Client", render: (m) => (canSee(m) ? contactName(m.clientId) : "") },
      { label: "Stage", render: (m) => (canSee(m) ? h`<span class="small">${m.stage}</span>` : "") },
      { label: "Status", render: (m) => (canSee(m) && pendingOn(m).length ? chip("pending") : chip(m.status)) },
      { label: "Responsible", render: (m) => A.user(m.responsible).name },
      { label: "Activity", render: (m) => (canSee(m) ? h`<span class="muted small">${ago(lastActivity(m))}</span>` : "") },
    ], rows, { onRow: "matters-sel", select: "matters", active: st.sel, empty: "No matters match these filters", emptyHint: " Clear a filter to see more." }), matterPreview(rows.find((m) => m.id === st.sel)), { emptyTitle: "Select a matter" });
    A.render(el, h`<div class="page">
      ${ui.header({ title: "Matters", sub: dirOnly ? "Directory only: your role sees matter names and numbers, not their contents." : `${all.filter((m) => m.status === "open").length} open matters you can see.`,
        actions: [ui.tabs([{ id: "list", label: "List" }, { id: "board", label: "Board" }], st.view, "matters-view"),
          ...(A.can("admin.access_review") ? [{ label: "Access report", to: "matters/access" }] : []), ...(setup ? [{ label: "Setup", to: "matters/setup" }] : [])],
        primary: { label: "New matter", a: "matters-new", key: "n", disabled: !canOpen, why: `${A.roleName(A.me().role)} cannot open matters` } })}
      ${ui.filters({ search: { c: "matters-q", value: st.q, placeholder: "Search number, name, client" }, chips: SAVED.map((s) => ({ ...s, count: all.filter(savedFn[s.id]).length })), active: st.saved, action: "matters-saved",
        selects: [{ c: "matters-area", value: st.area, label: "Practice area", options: [["", "All areas"]].concat(areas.map((a) => [a, a])) }, { c: "matters-resp", value: st.resp, label: "Responsible lawyer", options: [["", "Any lawyer"]].concat(resps.map((u) => [u, A.user(u).name])) }] })}
      ${st.view === "list" ? ui.bulkBar("matters", [{ label: "Set responsible staff", a: "matters-bulk-staff" }, { label: "Copy as CSV", a: "matters-bulk-csv" }]) : ""}
      ${body}
      ${ui.hints([["j", "next"], ["k", "previous"], ["Enter", "preview, again to open"], ["x", "select"], ["/", "search"], ["n", "new matter"]])}</div>`);
    if (st.view === "board") wireBoard(el);
  }
  /** HTML5 drag and drop on the board; drop runs the same stage move as the select (Closed goes to a gate). */
  function wireBoard(el) {
    const b = el.querySelector("#mx-board"); if (!b) return;
    b.querySelectorAll('.kcard[draggable="true"]').forEach((c) => { c.ondragstart = (e) => { if (e.dataTransfer) e.dataTransfer.setData("text/plain", c.dataset.mid); c.style.opacity = ".5"; }; c.ondragend = () => { c.style.opacity = ""; }; });
    b.querySelectorAll(".lane").forEach((ln) => { ln.ondragover = (e) => { e.preventDefault(); ln.style.outline = "2px dashed var(--accent)"; }; ln.ondragleave = () => { ln.style.outline = ""; };
      ln.ondrop = (e) => { e.preventDefault(); ln.style.outline = ""; const id = e.dataTransfer && e.dataTransfer.getData("text/plain"); const m = id && A.matter(id); if (m) moveStage(m, ln.dataset.stage); }; });
  }

  /* ---------- matter workspace ---------- */
  function workspace(el, { id }) {
    const raw = A.store.get("matters", id); const m = A.matter(id);
    if (!m) {
      const why = raw && A.canMatter(raw, "matters.directory") ? "Your role sees this matter in the directory only." : "This matter does not exist or is not available to you.";
      A.render(el, h`<div class="page"><div class="locked"><b>No access</b>${why} The attempt is logged.</div></div>`); return;
    }
    A.setContext({ kind: "matter", id: m.id, label: `${m.number} ${m.name}` });
    const ext = A.matterTabs(m);
    const mails = canDocs(m) ? A.store.where("emails", (e) => e.matterId === m.id) : [];
    const chr = A.store.get("chronos", `chr-${m.id}`);
    const tabs = [{ id: "overview", label: "Overview" }, { id: "documents", label: "Documents", count: canDocs(m) ? docsOf(m).length : 0 },
      { id: "chronology", label: "Chronology", count: chr && canDocs(m) ? chr.rows.length : 0 }, { id: "communications", label: "Communications", count: mails.length },
      { id: "analyze", label: "Analyze" }, { id: "sharing", label: "Sharing", count: canDocs(m) ? docsOf(m).filter(shareOf).length : 0 },
      { id: "notes", label: "Notes & activity" }].concat(ext.map((t) => ({ id: t.id, label: t.label })));
    let tab = st.tab[m.id] || "overview"; if (!tabs.some((t) => t.id === tab)) tab = "overview";
    const b = brief(m);
    A.render(el, h`<div class="page">
      ${ui.header({ title: m.name, status: chip(m.status), back: { label: "Matters", to: "matters" }, meta: h`${m.restricted ? chip("restricted") : ""}<span class="muted small" style="font-family:var(--mono);margin-left:8px">${m.number} · ${m.area}</span>`,
        actions: [h`<span class="row">${(m.team || []).map(ui.avatar)}</span>`, { label: "Note", a: "matters-tab", id: "notes" }, { label: "Share", a: "matters-tab", id: "sharing", title: "Client sharing and secure links" }],
        primary: { label: "✦ Draft", a: "matters-draft", id: m.id, agent: true, key: "d", disabled: !canEditDocs(m), why: roleWhy("draft documents") },
        sub: h`Client <a href="#" data-a="go" data-to="contacts/${m.clientId}">${contactName(m.clientId)}</a> · Stage ${ui.badge(m.stage)} · Responsible lawyer ${A.user(m.responsible).name}
          · Responsible staff ${staffOf(m) ? A.user(staffOf(m)).name : h`<span class="muted">none</span>`}
          <button class="btn small" data-a="matters-staff" data-id="${m.id}" ${canEditMatter(m) ? "" : no(roleWhy("change responsible staff"))}>Change</button>` })}
      ${todoStrip(m)}
      ${canDocs(m) ? askBox(m) : ""}
      <div class="card" style="border-left:3px solid var(--accent);margin-bottom:14px"><h2>✦ Agent brief</h2>
        ${b.length ? h`${b.map((s) => h`<p style="margin:0 0 6px">${s.text} ${s.cites.map(ui.cite)}</p>`)}<div class="muted small">Built from ${docsOf(m).length} documents. Every sentence links to its source line.</div>`
          : canDocs(m) ? h`<p class="muted" style="margin:0">No documents on file yet. Upload or draft one and the brief will cite it.</p>` : h`<p class="muted" style="margin:0">The brief draws on documents you cannot open.</p>`}</div>
      ${ui.tabs(tabs, tab, "matters-tab")}
      <div id="mtab" data-matter="${m.id}"></div></div>`);
    const ai = el.querySelector("#mask"); if (ai) ai.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); askMatter(m, ai.value); } };
    const body = el.querySelector("#mtab");
    const t = ext.find((x) => x.id === tab);
    if (t) { try { t.render(body, m); } catch (e) { console.error(e); A.render(body, h`<div class="locked"><b>This tab failed to load</b>${e.message}</div>`); } }
    else if (tab === "documents") A.render(body, docsTab(m));
    else if (tab === "chronology") A.render(body, chronologyTab(m));
    else if (tab === "communications") A.render(body, commsTab(m));
    else if (tab === "notes") A.render(body, notesTab(m));
    else if (tab === "analyze") A.render(body, analyzeTab(m));
    else if (tab === "sharing") A.render(body, sharingTab(m));
    else A.render(body, overviewTab(m));
  }

  /** What a person must do on this matter vs what the agent is doing, always above the tabs. */
  function todoStrip(m) {
    const waits = pendingOn(m), mine = waits.filter((g) => A.canApprove(g).ok);
    const runs = A.store.where("runs", (r) => r.matterId === m.id && (r.status === "running" || r.status === "waiting"));
    if (!waits.length && !runs.length) return "";
    return h`<div class="mx-todo small"><div class="row wrap">
      ${waits.length ? h`<b>${mine.length ? `${mine.length} for you to approve` : `${waits.length} waiting for approval`}</b>` : ""}
      ${waits.slice(0, 4).map((g) => h`<a href="#" data-a="go" data-to="approvals/${g.id}">${g.title}</a>`)}
      ${runs.length ? h`<span class="grow"></span><span>✦ Agent: ${runs.map((r) => (r.status === "waiting" ? `${r.title} (paused for approval)` : r.title)).join("; ")}</span>` : ""}</div></div>`;
  }
  function overviewTab(m) {
    const parties = [{ id: m.clientId, role: "Client" }].concat((m.opposing || []).map((c) => ({ id: c, role: A.contact(c).role || "Opposing party" })));
    return h`<div class="grid g2">
      <div class="card"><h3>Key facts</h3><dl class="kv">
        <dt>Number</dt><dd>${m.number}</dd><dt>Area</dt><dd>${m.area}</dd><dt>Stage</dt><dd>${m.stage}</dd><dt>Status</dt><dd>${cap1(m.status)}</dd>
        <dt>Opened</dt><dd>${A.date(m.opened)}</dd>${m.closed ? h`<dt>Closed</dt><dd>${A.date(m.closed)}</dd>` : ""}
        <dt>Fee basis</dt><dd>${cap1(m.fee || "")}</dd><dt>Responsible lawyer</dt><dd>${A.user(m.responsible).name}</dd><dt>Responsible staff</dt><dd>${staffOf(m) ? A.user(staffOf(m)).name : "None assigned"}</dd>
        <dt>Team</dt><dd>${(m.team || []).map((u) => A.user(u).name).join(", ")}</dd>
        ${m.restricted ? h`<dt>Screened</dt><dd>${(m.wall || []).map((u) => A.user(u).name).join(", ") || "No one"}</dd>` : ""}</dl></div>
      <div class="card"><h3>Parties</h3>${parties.map((p) => { const c = A.contact(p.id);
        return h`<div class="row" style="padding:5px 0;border-bottom:1px solid var(--line)"><a href="#" class="grow" data-a="go" data-to="contacts/${p.id}">${c.name}</a>${c.org ? h`<span class="muted small">${c.org}</span>` : ""}${ui.badge(p.role)}</div>`; })}</div>
      <div class="card"><h3>✦ Next steps</h3>${nextSteps(m).map((s) => h`<p style="margin:0 0 6px">${s.text} ${s.cite ? ui.cite(s.cite) : ""}</p>`)}</div></div>`;
  }

  function viewer(d, hitPage, hitLine) {
    return h`<div class="docview">${d.pages.map((p, pi) => h`<div class="muted small" style="margin:${pi ? "12px" : "0"} 0 4px;font-family:var(--f)">Page ${pi + 1} of ${d.pages.length}</div>${p.map((t, li) =>
      h`<div class="ln ${pi + 1 === hitPage && li + 1 === hitLine ? "hit" : ""}" id="ln-${d.id}-${pi + 1}-${li + 1}"><i>${li + 1}</i><span>${t}</span></div>`)}`)}</div>`;
  }

  /* ---------- ask box ---------- */
  function askBox(m) {
    const a = st.ask[m.id];
    return h`<div class="card" style="margin-bottom:14px"><div class="row">
        <input id="mask" class="input grow" placeholder="Ask about this matter: dates, amounts, who said what" value="${a ? a.q : ""}" aria-label="Ask about this matter">
        <button class="btn agent" data-a="matters-ask" data-id="${m.id}">✦ Ask</button>${a ? h`<button class="btn small" data-a="matters-ask-clear" data-id="${m.id}">Clear</button>` : ""}</div>
      ${a ? h`<p style="margin:10px 0 4px">${a.text}</p>${a.cites.length ? h`<div class="cites"><span class="muted">Sources</span> ${a.cites.map(ui.cite)}</div>` : ""}
        <div class="muted small">Answered only from ${docsOf(m).length} documents on this matter. Click a source to see the line.</div>` : ""}</div>`;
  }
  function askMatter(m, q) {
    q = String(q || "").trim(); if (!q) return A.toast("Type a question first");
    if (!canDocs(m)) return A.toast("You cannot open this matter's documents", "bad");
    st.ask[m.id] = { q, ...answerFor(m, q) };
    A.audit("matter.asked", { target: m.id, matterId: m.id, note: q.slice(0, 80) });
  }

  /* ---------- documents ---------- */
  function docHistory(d) {
    const ev = A.store.where("audit", (a) => a.target === d.id).slice(0, 12);
    const made = { at: d.added, actor: d.by, action: d.approvedBy ? `created by the agent, released by ${A.user(d.approvedBy).name}` : "added to the matter" };
    return h`${ev.map((a) => h`<div class="run">${ui.avatar(a.actor)}<div class="grow"><div class="muted small">${A.user(a.actor).name} · ${a.at}</div>${a.action.replace(/^document\./, "").replace(/\./g, " ")}${a.note ? ` · ${a.note}` : ""}</div></div>`)}
      <div class="run">${ui.avatar(made.actor)}<div class="grow"><div class="muted small">${made.actor === "agent" ? "Agent" : A.user(made.actor).name} · ${A.date(made.at)}</div>${made.action}</div></div>`;
  }
  function docPanel(m, d) {
    const x = meta(d); const can = canEditDocs(m); const other = lockedByOther(d);
    const editWhy = !can ? roleWhy("edit documents") : other ? `Locked by ${A.user(x.lockedBy).name}` : "";
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "matters-doc-revise" && g.payload.docId === d.id);
    return h`<div class="card" style="margin-top:14px"><div class="row wrap"><h3 class="grow">${d.name}</h3>
        ${chip(x.status)}${x.locked ? ui.status("blocked", `Locked by ${A.user(x.lockedBy).name}`) : ""}
        <button class="btn small" data-a="matters-doc-meta" data-id="${d.id}" ${editWhy ? no(editWhy) : ""}>Edit details</button>
        <button class="btn small" data-a="matters-doc-lock" data-id="${d.id}" ${!can ? no(roleWhy("lock documents")) : other ? no(`Only ${A.user(x.lockedBy).name} can unlock`) : ""}>${x.locked ? "Unlock" : "Lock"}</button>
        <button class="btn small agent" data-a="matters-doc-revise" data-id="${d.id}" ${editWhy ? no(editWhy) : x.status !== "draft" ? no("Only draft documents can be revised") : pend.length ? no("A revision is already waiting for approval") : ""}>✦ Revise</button>
        <button class="btn small danger" data-a="matters-doc-trash" data-id="${d.id}" ${editWhy ? no(editWhy) : x.locked ? no("Unlock before deleting") : ""}>Delete</button>
        <button class="btn small" data-a="matters-doc-close" data-id="${m.id}">Close</button></div>
      <div class="muted small" style="margin-bottom:8px">Tags: ${x.tags.length ? x.tags.join(", ") : "none"} · Received ${x.received ? A.date(x.received) : "not recorded"}${x.locked ? ` · locked ${x.lockedAt}: others can read but not change it` : ""}</div>
      ${pend.map((g) => ui.gateCard(g))}
      <div class="grid g2"><div>${viewer(d, 0, 0)}</div><div><h3>History</h3>${docHistory(d)}</div></div></div>`;
  }
  st.dstat = "all"; st.dq = "";
  const docFilter = (m) => { const q = st.dq.toLowerCase(); return docsOf(m).filter((d) => (st.dstat === "all" || meta(d).status === st.dstat) && (!q || `${d.name} ${d.kind} ${meta(d).tags.join(" ")}`.toLowerCase().includes(q))); };
  function docsTab(m) {
    if (!canDocs(m)) return h`<div class="locked"><b>No access</b>${A.roleName(A.me().role)} cannot open documents on this matter.</div>`;
    const all = docsOf(m), ds = docFilter(m);
    const open = all.find((d) => d.id === st.doc[m.id]);
    const canDraft = canEditDocs(m), why = roleWhy("edit documents");
    const S = A.selection("mdocs"); S.ids().forEach((id) => { if (!all.some((d) => d.id === id)) S.toggle(id); });
    const counts = (s) => all.filter((d) => s === "all" || meta(d).status === s).length;
    return h`${ui.filters({ search: { c: "matters-dq", value: st.dq, placeholder: "Filter by name, kind, tag" }, chips: ["all", "draft", "final", "filed"].map((s) => ({ id: s, label: s === "all" ? "All" : cap1(s), count: counts(s) })), active: st.dstat, action: "matters-dstat",
        right: h`${ui.btn({ label: "Bundle", to: `documents/bundle/${m.id}`, small: true })}${ui.btn({ label: "✦ Analyze several", a: "matters-tab", id: "analyze", small: true })}${ui.btn({ label: "✦ Draft with agent", a: "matters-draft", id: m.id, agent: true, key: "n", disabled: !canDraft, why: roleWhy("draft documents") })}` })}
      ${ui.bulkBar("mdocs", [{ label: "Mark final", a: "matters-dbulk-status", data: { st: "final" }, disabled: !canDraft, why }, { label: "Mark filed", a: "matters-dbulk-status", data: { st: "filed" }, disabled: !canDraft, why },
        { label: "Add tag", a: "matters-dbulk-tag", disabled: !canDraft, why }, { label: "Move to trash", a: "matters-dbulk-trash", danger: true, disabled: !canDraft, why }])}
      ${ui.table([
        { label: "Document", render: (d) => h`<b>${d.name}</b>${meta(d).locked ? h` <span class="muted small" title="Locked by ${A.user(meta(d).lockedBy).name}">locked</span>` : ""}${shareOf(d) ? h` ${ui.status("sent", "Shared")}` : ""}` },
        { label: "Status", render: (d) => chip(meta(d).status) },
        { label: "Kind", render: (d) => h`<span class="small">${d.kind}</span>` },
        { label: "Tags", render: (d) => h`<span class="muted small">${meta(d).tags.join(", ")}</span>` },
        { label: "Received", render: (d) => (meta(d).received ? A.date(meta(d).received) : "") },
        { label: "Pages", num: true, render: (d) => d.pages.length },
        { label: "Added", render: (d) => h`${A.date(d.added)} <span class="muted small">${d.by === "agent" ? "✦ agent" : A.user(d.by).initials}</span>` },
      ], ds, { onRow: "matters-doc", select: canDraft ? "mdocs" : "", active: open ? open.id : "", empty: all.length ? "No documents match this filter" : "No documents on this matter yet", emptyHint: all.length ? " Clear the filter." : " Draft one with the agent or file an email." })}
      ${ui.hints([["j", "next"], ["k", "previous"], ["Enter", "open"], ["x", "select"], ["n", "draft"]])}
      ${open ? docPanel(m, open) : ""}`;
  }
  function startRevision(m, d) {
    if (!canEditDocs(m) || lockedByOther(d)) { A.toast("You cannot revise this document", "bad"); return; }
    const r = proposeRevision(d);
    if (!r.changes.length) { A.toast("The agent found nothing to change against the firm playbook"); return; }
    A.agent.run({ title: `Revise ${d.name}: ${m.number}`, matterId: m.id, steps: ["Reading the draft line by line", "Applying the firm's negotiating positions", "Marking every change as tracked"],
      done() {
        const body = h`<p class="muted small">Tracked changes. Insertions are underlined; deletions are struck through.</p>${r.changes.map((c) => h`<p><span class="muted small">p.${c.page} l.${c.line}</span> ${trackedLine(c.from, c.to)}</p>`)}`;
        return A.gate({ kind: "document.revise", cap: "documents.release", title: `Revisions to ${d.name}: ${m.name}`, matterId: m.id, preparedBy: "agent",
          summary: `${r.changes.length} tracked change${r.changes.length > 1 ? "s" : ""}. Approve to apply them to the draft.`, body: String(body),
          cites: r.changes.map((c) => ({ doc: d.id, page: c.page, line: c.line, quote: c.from, label: d.name })),
          checks: [{ label: "Document is still a draft", ok: meta(d).status === "draft", blocking: true }, { label: "Not locked by someone else", ok: !lockedByOther(d), blocking: true }],
          effect: "matters-doc-revise", payload: { docId: d.id, pages: r.pages, count: r.changes.length } });
      } });
    A.toast("The agent is revising. Changes appear as tracked edits for approval.");
  }
  A.effect("matters-doc-revise", (p, g) => {
    const d = A.store.get("docs", p.docId); if (!d) return;
    const x = meta(d); if (x.locked && x.lockedBy !== g.decidedBy) return A.toast("Document is locked; revisions not applied", "bad");
    A.store.put("docs", { id: d.id, pages: p.pages });
    A.audit("document.changed", { target: d.id, matterId: d.matterId, note: `${p.count} tracked changes accepted` });
  });

  /* ---------- chronology ---------- */
  function chronologyTab(m) {
    if (!canDocs(m)) return h`<div class="locked"><b>No access</b>${A.roleName(A.me().role)} cannot open documents on this matter.</div>`;
    const c = A.store.get("chronos", `chr-${m.id}`); const can = canEditDocs(m);
    const g = c && c.gateId && A.store.get("approvals", c.gateId);
    const status = !c ? "" : c.status === "confirmed" ? ui.status("confirmed", `Confirmed by ${A.user(c.confirmedBy).name}`) : g && g.status === "pending" ? ui.status("needs-approval", "Waiting for a lawyer") : ui.status("draft", "Draft: not confirmed");
    const editable = c && c.status === "draft" && can;
    return h`<div class="row wrap" style="margin-bottom:10px"><span class="muted small grow">${c ? h`Built ${c.built} from ${new Set(c.rows.map((r) => r.cite.doc)).size} documents. ${status}` : "The agent reads every document on the matter and lists each dated event with its source line."}</span>
        <button class="btn agent" data-a="matters-chron-build" data-id="${m.id}" ${can ? "" : no(roleWhy("build a chronology"))}>✦ ${c ? "Rebuild" : "Build chronology"}</button>
        ${c ? h`<button class="btn" data-a="matters-chron-confirm" data-id="${m.id}" ${!can ? no(roleWhy("send for confirmation")) : c.status !== "draft" ? no(c.status === "confirmed" ? "Already confirmed" : "Already waiting for a lawyer") : !c.rows.length ? no("Nothing to confirm") : ""}>Send to confirm</button>
          <button class="btn primary" data-a="matters-chron-share" data-id="${m.id}" ${c.status !== "confirmed" ? no("A lawyer must confirm the chronology before it can be shared") : !can ? no(roleWhy("share documents")) : ""}>Share as document</button>` : ""}</div>
      ${g && g.status === "pending" ? ui.gateCard(g) : ""}
      ${c ? ui.table([
        { label: "Date", render: (r) => h`<span style="font-family:var(--mono)">${A.date(r.date)}</span>` },
        { label: "Event", render: (r) => r.text },
        { label: "Source", render: (r) => ui.cite(r.cite) },
        { label: "", render: (r) => (editable ? h`<button class="btn small" data-a="matters-chron-drop" data-id="${m.id}" data-row="${r.id}" title="Remove this event before confirming">Remove</button>` : "") },
      ], c.rows, { empty: "No dated events found in this matter's documents" }) : ui.empty("No chronology yet", " Build one and the agent will cite every row.")}`;
  }
  function chronBody(c) {
    return String(h`<table class="tbl"><tbody>${c.rows.map((r) => h`<tr><td>${A.date(r.date)}</td><td>${r.text}</td><td>${ui.cite(r.cite)}</td></tr>`)}</tbody></table>`);
  }
  A.effect("matters-chron-confirm", (p, g) => {
    const c = A.store.get("chronos", `chr-${p.matterId}`); if (!c) return;
    A.store.put("chronos", { id: c.id, status: "confirmed", confirmedBy: g.decidedBy, confirmedAt: g.decidedAt });
    A.audit("chronology.confirmed", { target: c.id, matterId: p.matterId, note: `${c.rows.length} events` });
  });

  /* ---------- communications ---------- */
  function mailCard(e, actions) {
    const open = st.mail[e.id];
    return h`<div class="card" style="margin-bottom:8px;padding:10px 12px"><div class="row wrap click" data-a="matters-mail-open" data-id="${e.id}">
        <b class="grow">${e.subject}</b><span class="muted small">${e.date}</span></div>
      <div class="muted small">${e.from} &lt;${e.fromEmail}&gt; · to ${A.user(e.to).name}${e.filedBy ? ` · filed by ${e.filedBy === "agent" ? "the agent (sender is a party)" : A.user(e.filedBy).name}` : ""}</div>
      ${open ? h`<div class="docview" style="margin-top:6px">${e.lines.map((t, i) => h`<div class="ln"><i>${i + 1}</i><span>${t}</span></div>`)}</div>` : ""}
      ${actions || ""}</div>`;
  }
  function commsTab(m) {
    if (!canDocs(m)) return h`<div class="locked"><b>No access</b>${A.roleName(A.me().role)} cannot open correspondence on this matter.</div>`;
    const ms = A.store.where("emails", (e) => e.matterId === m.id).sort((a, b) => b.date.localeCompare(a.date));
    const q = canQueue() ? A.store.where("emails", (e) => !e.matterId).length : 0;
    return h`<div class="row" style="margin-bottom:10px"><span class="muted small grow">${ms.length} emails filed to this matter. Filing keeps a copy here; the mailbox is not changed.</span>
        ${canQueue() ? h`<button class="btn" data-a="go" data-to="matters/unfiled">Unfiled email${q ? ` (${q})` : ""}</button>` : ""}</div>
      ${ms.length ? ms.map((e) => mailCard(e, canEditDocs(m) ? h`<div class="row" style="margin-top:6px"><span class="grow"></span><button class="btn small" data-a="matters-mail-unfile" data-id="${e.id}">Move to unfiled</button></div>` : "")) : ui.empty("No email filed yet", " Messages from parties on this matter are suggested here from the unfiled queue.")}`;
  }
  function unfiledPage(el) {
    A.setContext({ kind: "matters", label: "Unfiled email" });
    if (!canQueue()) { A.render(el, h`<div class="page"><div class="locked"><b>No access</b>${A.roleName(A.me().role)} cannot file firm email.</div></div>`); return; }
    const q = A.store.where("emails", (e) => !e.matterId).sort((a, b) => b.date.localeCompare(a.date));
    const tombs = A.store.all("mailtombs").length;
    A.render(el, h`<div class="page"><div class="page-h"><h1>Unfiled email</h1><span class="grow"></span>
        <button class="btn" data-a="matters-mail-sync">Check inbox</button>
        <div class="sub">The agent suggests a matter for each message. It files nothing on its own; when more than one matter fits, you choose. ${tombs ? `${tombs} removed message${tombs > 1 ? "s" : ""} will not be imported again.` : ""}</div></div>
      ${q.length ? q.map((e) => { const s = mailSuggest(e); const cands = s.candidates.filter((c) => canEditDocs(c.m));
        const sug = !s.top ? h`<span class="muted small">✦ No matter fits. Likely not client work.</span>`
          : s.clear ? h`<span class="small">✦ Suggested: <b>${s.top.m.number} ${s.top.m.name}</b> <span class="muted">(${s.top.why.join("; ")})</span></span>`
          : h`<span class="small" style="color:var(--warn)">✦ Ambiguous: ${s.candidates.map((c) => `${c.m.number} ${c.m.name}`).join(" or ")}. Choose one.</span>`;
        return mailCard(e, h`<div class="row wrap" style="margin-top:8px">${sug}<span class="grow"></span>
          ${cands.length ? h`<select class="input" style="width:auto" id="mf-${e.id}" aria-label="File to matter">${s.clear ? "" : h`<option value="">Choose a matter…</option>`}${cands.map((c) => h`<option value="${c.m.id}">${c.m.number} ${c.m.name}</option>`)}</select>
            <button class="btn small primary" data-a="matters-mail-file" data-id="${e.id}">File</button>` : ""}
          <button class="btn small danger" data-a="matters-mail-remove" data-id="${e.id}" title="Removes it from this app only; the mailbox is not touched">Do not file and remove</button></div>`); })
        : ui.empty("Nothing to file", " New messages from your mailbox appear here.")}</div>`);
  }

  function notesTab(m) {
    const notes = A.store.where("notes", (n) => n.matterId === m.id);
    const docIds = new Set(docsOf(m).map((d) => d.id));
    const runs = A.store.where("runs", (r) => r.matterId === m.id).slice(0, 6);
    const acts = A.store.where("audit", (a) => a.matterId === m.id || a.target === m.id || docIds.has(a.target)).slice(0, 10);
    const canNote = A.canMatter(m, "matters.edit") && A.can("matters.edit", "act");
    return h`<div class="grid g2"><div class="card"><h3>Notes</h3>
      ${canNote ? h`<div class="field" style="margin-bottom:10px"><textarea id="mnote" rows="2" class="input" placeholder="Add a note to the file"></textarea><div class="row"><span class="grow"></span><button class="btn small primary" data-a="matters-note" data-id="${m.id}">Add note</button></div></div>` : ""}
      ${notes.length ? notes.map((n) => h`<div class="run">${ui.avatar(n.by)}<div class="grow"><div class="muted small">${A.user(n.by).name} · ${n.at}</div>${n.text}</div></div>`) : ui.empty("No notes yet")}</div>
      <div class="card"><h3>Activity</h3>${runs.map(ui.runCard)}
      ${acts.map((a) => h`<div class="run">${ui.avatar(a.actor)}<div class="grow"><div class="muted small">${a.at}</div>${a.action}${a.note ? ` · ${a.note}` : ""}</div></div>`)}
      ${!runs.length && !acts.length ? ui.empty("No activity recorded yet") : ""}</div></div>`;
  }

  /* ---------- cited document viewer (shared by every module) ---------- */
  A.openCite = (docId, page, line) => {
    const d = A.store.get("docs", docId); const m = d && A.store.get("matters", d.matterId);
    const leadDoc = d && !d.matterId;
    if (!d || (leadDoc ? !A.can("intake.leads") : (!m || !canDocs(m)))) { A.audit("document.view.denied", { target: docId }); return A.modal("No access", h`<div class="locked"><b>No access</b>You cannot open this source document. The attempt is logged.</div>`); }
    A.audit("document.viewed", { target: docId, matterId: m ? m.id : null, note: `${d.name}${page ? ` p.${page} l.${line}` : ""}` });
    A.modal(`${d.name}${page ? ` · p.${page} l.${line}` : ""}`, h`<div class="muted small" style="margin-bottom:6px">${m ? `${m.number} ${m.name}` : "Intake record"} · added ${A.date(d.added)}</div>${viewer(d, page, line)}`,
      m ? [{ label: "Open matter", run: () => { st.tab[m.id] = "documents"; st.doc[m.id] = d.id; A.go(`matters/${m.id}`); } }, { label: "Close", primary: true }] : [{ label: "Close", primary: true }]);
    const hit = document.getElementById(`ln-${d.id}-${page}-${line}`);
    if (hit && hit.scrollIntoView) hit.scrollIntoView({ block: "center" });
  };

  /* ---------- agent drafting ---------- */
  const TYPES = { opposing: "Letter to opposing counsel", client: "Client update", demand: "Demand letter" };
  function compose(m, type) {
    const client = A.contact(m.clientId); const cites = [];
    const oc = (m.opposing || []).map(A.contact).find((c) => /counsel/i.test(c.role || ""));
    const op = (m.opposing || []).map(A.contact).find((c) => !/counsel/i.test(c.role || ""));
    const para = [];
    const add = (text, c) => { if (c) cites.push(c); para.push(h`<p>${text}${c ? h` ${ui.cite(c)}` : ""}</p>`); };
    const facts = brief(m).slice(0, 3);
    if (type === "opposing") {
      const to = oc || op; add(`To: ${to ? `${to.name}${to.org ? `, ${to.org}` : ""}` : "Opposing counsel (none on file)"}`);
      add(`Re: ${m.name}, our file ${m.number}. We act for ${client.name}.`);
      asks(m).forEach((c, i) => add(i === 0 ? `Thank you for your letter. You asked: "${c.quote}" Our client consents to a 15-day extension, not 30, given that the claim was served in September.` : `You also requested: "${c.quote}" We will deliver these with our affidavit of documents.`, c));
      if (!asks(m).length) facts.forEach((f) => add(f.text, f.cites[0]));
      add("Please confirm by reply. Yours truly, Linda K Law");
    } else if (type === "client") {
      add(`Dear ${client.name},`); add(`Here is where your matter stands. It is at the ${m.stage.toLowerCase()} stage.`);
      facts.forEach((f) => add(f.text, f.cites[0]));
      nextSteps(m).slice(0, 2).forEach((s) => add(`Next: ${s.text}`, s.cite));
      add("Call us with any questions. This update is not a bill.");
    } else {
      add(`To: ${op ? op.name : "the responding party"}${oc ? ` (c/o ${oc.name}, ${oc.org || ""})` : ""}`); add(`Re: ${m.name}. We act for ${client.name}.`);
      const amt = findLine(m, /\$\s?[\d,]+/); facts.forEach((f) => add(f.text, f.cites[0]));
      add(`Our client demands payment${amt ? ` of ${(amt.quote.match(/\$\s?[\d,]+/) || [""])[0]}` : ""} within 14 days, failing which we have instructions to proceed without further notice.`, amt);
      add("This letter is written without prejudice to our client's rights. Linda K Law");
    }
    const uniq = cites.filter((c, i) => cites.findIndex((x) => x.doc === c.doc && x.page === c.page && x.line === c.line) === i);
    const text = para.map((p) => String(p).replace(/<button[\s\S]*?<\/button>/g, "").replace(/<[^>]+>/g, "").replace(/&amp;/g, "&").replace(/&quot;/g, '"').replace(/&#39;/g, "'").replace(/&lt;/g, "<").replace(/&gt;/g, ">").trim());
    return { body: String(h`${para}`), cites: uniq, text, addressee: type === "client" ? client : oc || op };
  }
  function startDraft(m, type) {
    const label = TYPES[type] || TYPES.opposing;
    if (!(A.canMatter(m, "documents.edit") && A.can("documents.edit", "act"))) { A.toast("Your role cannot draft documents on this matter", "bad"); return null; }
    return A.agent.run({ title: `Draft ${label.toLowerCase()}: ${m.number}`, matterId: m.id,
      steps: ["Reading the matter's documents", "Pulling facts with page and line references", `Drafting the ${label.toLowerCase()}`, "Checking every statement has a source"],
      done() {
        const d = compose(m, type);
        return A.gate({ kind: "document.release", cap: "documents.release", title: `${label}: ${m.name}`, matterId: m.id, preparedBy: "agent",
          summary: `Drafted from ${new Set(d.cites.map((c) => c.doc)).size} matter documents. Approve to file it as a released document.`,
          body: d.body, cites: d.cites,
          checks: [{ label: "Every factual statement cites a source", ok: d.cites.length > 0 }, { label: "Addressee is a party on this matter", ok: !!d.addressee }, { label: "No privileged notes quoted", ok: true }],
          effect: "matters-doc-release", payload: { matterId: m.id, name: `${label} (${A.date(A.today)})`, text: d.text } });
      } });
  }
  A.effect("matters-doc-release", (p, g) => {
    const pages = []; for (let i = 0; i < p.text.length; i += 12) pages.push(p.text.slice(i, i + 12));
    const d = A.store.put("docs", { id: A.id("d"), matterId: p.matterId, name: p.name, kind: "letter", added: A.today, by: "agent", approvedBy: g.decidedBy, pages: pages.length ? pages : [[""]] });
    A.audit("document.released", { target: d.id, matterId: p.matterId, note: `${p.name} approved` });
  });
  function draftModal(m) {
    A.modal(`Draft with agent · ${m.number}`, h`<label class="field">What should the agent draft?<select id="mdtype" class="input">${Object.entries(TYPES).map(([k, v]) => h`<option value="${k}">${v}</option>`)}</select></label>
      <p class="muted small">The agent drafts from this matter's documents and cites each fact. A lawyer approves before anything leaves the firm.</p>`,
      [{ label: "Cancel" }, { label: "Start draft", primary: true, run: (d) => { const s = d.querySelector("#mdtype"); if (startDraft(m, s ? s.value : "opposing")) A.toast("Drafting. It will appear in Approvals."); } }]);
  }

  /* ---------- contacts ---------- */
  const contactMatters = (c) => dirMatters().filter((m) => roleOn(c, m));
  const visibleContacts = () => A.store.all("contacts").filter((c) => {
    const all = A.store.all("matters").filter((m) => roleOn(c, m)); return !all.length || contactMatters(c).length > 0;
  });
  st.ctab = "all"; st.csel = "";
  const aliasesOf = (c) => A.store.where("aliases", (a) => a.contactId === c.id);
  const isClient = (c) => contactMatters(c).some((m) => m.clientId === c.id);
  /** Possible duplicates: same email, shared alias, or same first two name words. Each pair once. */
  function dupPairs() {
    const cs = visibleContacts(), out = [];
    const key = (c) => words(c.name).slice(0, 2).join(" ");
    for (let i = 0; i < cs.length; i++) for (let j = i + 1; j < cs.length; j++) {
      const a = cs[i], b = cs[j]; let why = "";
      if (a.email && a.email === b.email) why = "Same email";
      else if (aliasesOf(a).some((x) => aliasesOf(b).some((y) => y.alias === x.alias))) why = `Shared name: ${aliasesOf(a).find((x) => aliasesOf(b).some((y) => y.alias === x.alias)).alias}`;
      else if (a.kind === b.kind && key(a) && key(a) === key(b)) why = "Similar name";
      if (why) out.push({ id: `${a.id}~${b.id}`, a, b, why });
    }
    return out;
  }
  const CTABS = [{ id: "all", label: "All" }, { id: "clients", label: "Clients" }, { id: "people", label: "People" }, { id: "orgs", label: "Organizations" }, { id: "dups", label: "Possible duplicates" }];
  const ctabFn = { all: () => true, clients: isClient, people: (c) => c.kind !== "org", orgs: (c) => c.kind === "org" };
  function contactRows() {
    const q = st.cq.toLowerCase(), f = ctabFn[st.ctab] || ctabFn.all;
    return visibleContacts().filter((c) => f(c) && (!q || `${c.name} ${c.org || ""} ${c.email || ""} ${aliasesOf(c).map((a) => a.alias).join(" ")}`.toLowerCase().includes(q)));
  }
  function contactPreview(c) {
    if (!c) return null;
    const ms = contactMatters(c), al = aliasesOf(c);
    return h`<div class="card"><div class="row"><span class="muted small grow">${c.kind === "org" ? "Organization" : "Person"}${c.org ? ` · ${c.org}` : ""}</span>${c.idStatus ? chip(c.idStatus) : ""}</div>
      <h3 style="margin:4px 0 6px">${c.name}</h3>
      <dl class="kv"><dt>Email</dt><dd>${c.email || "—"}</dd><dt>Phone</dt><dd>${c.phone || "—"}</dd>${al.length ? h`<dt>Also known as</dt><dd>${al.map((a) => a.alias).join("; ")}</dd>` : ""}</dl>
      <div class="small" style="margin:8px 0">${ms.length ? ms.map((m) => h`<div class="row" style="padding:3px 0"><a href="#" class="grow" data-a="matters-open" data-id="${m.id}">${m.number} ${m.name}</a>${ui.badge(roleOn(c, m))}</div>`) : h`<span class="muted">Not on any matter you can see.</span>`}</div>
      ${fieldVals(c).length ? h`<dl class="kv">${fieldVals(c).map(([k, v]) => h`<dt>${k}</dt><dd>${v}</dd>`)}</dl>` : ""}${(c.tags || []).length ? h`<div class="small">${c.tags.map((t) => ui.badge(t))}</div>` : ""}
      <div class="row"><span class="grow"></span>${ui.btn({ label: "Open", a: "contacts-open", id: c.id, primary: true, small: true })}</div></div>`;
  }
  function contactsBody() {
    if (st.ctab === "dups") {
      const ps = dupPairs();
      return ps.length ? h`${ps.map((p) => h`<div class="card row wrap" style="margin-bottom:8px;padding:10px 12px"><b>${p.a.name}</b><span class="muted">and</span><b>${p.b.name}</b><span class="muted small grow">${p.why}</span>
          <button class="btn small" data-a="contacts-open" data-id="${p.a.id}">Review</button></div>`)}
        <p class="muted small">A merge rewrites conflict data, so a lawyer approves it. Related companies that share a parent stay separate records.</p>`
        : ui.empty("No likely duplicates", " The check compares emails, aliases and names.");
    }
    const rows = contactRows(); if (!rows.some((c) => c.id === st.csel)) st.csel = rows[0] ? rows[0].id : "";
    return ui.split(ui.table([
      { label: "Name", render: (c) => h`<b>${c.name}</b>${c.org ? h` <span class="muted small">${c.org}</span>` : ""}${c.proposed ? h` ${ui.status("new")}` : ""}` },
      { label: "Kind", render: (c) => h`<span class="small">${c.kind === "org" ? "Organization" : "Person"}</span>` },
      { label: "Roles", render: (c) => { const r = [...new Set(contactMatters(c).map((m) => roleOn(c, m)))]; return r.map((x) => ui.badge(x)); } },
      { label: "Matters", num: true, render: (c) => contactMatters(c).length },
      { label: "ID", render: (c) => (c.idStatus ? chip(c.idStatus) : "") },
    ], rows, { onRow: "contacts-sel", select: A.can("contacts.edit", "act") ? "contacts" : "", active: st.csel, empty: "No contacts match", emptyHint: " Clear the search." }), contactPreview(rows.find((c) => c.id === st.csel)), { emptyTitle: "Select a contact" });
  }
  function contactsList(el) {
    A.setContext({ kind: "contacts", label: "Contacts" });
    const canEdit = A.can("contacts.edit", "act"), why = `${A.roleName(A.me().role)} cannot edit contacts`;
    const counts = { dups: dupPairs().length };
    A.render(el, h`<div class="page">${ui.header({ title: "Contacts", sub: "Names, aliases and roles here feed every conflict check.",
        actions: [{ label: "Import", to: "contacts/import", disabled: !canEdit, why }, { label: "Fields and tags", to: "contacts/fields" }],
        primary: { label: "New contact", a: "contacts-new", key: "n", disabled: !canEdit, why } })}
      ${ui.filters({ search: { c: "contacts-q", value: st.cq, placeholder: "Search names, aliases, email" }, chips: CTABS.map((t) => ({ ...t, count: t.id === "dups" ? counts.dups : visibleContacts().filter(ctabFn[t.id]).length })), active: st.ctab, action: "contacts-tab" })}
      ${st.ctab === "dups" ? "" : ui.bulkBar("contacts", [{ label: "Add tag", a: "contacts-bulk-tag" }, { label: "Copy as CSV", a: "contacts-bulk-csv" }])}
      ${contactsBody()}${ui.hints([["j", "next"], ["k", "previous"], ["Enter", "preview, again to open"], ["x", "select"], ["/", "search"], ["n", "new contact"]])}</div>`);
  }
  function contactDetail(el, { id }) {
    const c = A.store.get("contacts", id);
    if (!c || !visibleContacts().includes(c)) { A.render(el, h`<div class="page"><div class="locked"><b>No access</b>This contact is not available to you.</div></div>`); return; }
    A.setContext({ kind: "contact", id: c.id, label: c.name });
    const ms = contactMatters(c); const al = A.store.where("aliases", (a) => a.contactId === c.id);
    const canEdit = A.can("contacts.edit", "act"); const no = A.raw(`disabled title="${A.esc(A.roleName(A.me().role))} cannot edit contacts"`);
    const shared = A.store.where("aliases", (a) => a.contactId !== c.id && al.some((x) => x.alias === a.alias));
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "contacts-merge" && (g.payload.keep === c.id || g.payload.drop === c.id));
    A.render(el, h`<div class="page">${ui.header({ title: c.name, status: c.idStatus ? chip(c.idStatus) : "", back: { label: "Contacts", to: "contacts" }, meta: h`<span class="muted small" style="margin-left:8px">${c.kind === "org" ? "Organization" : "Person"}</span>`,
        actions: [{ label: "Merge duplicate…", a: "contacts-merge", id: c.id, disabled: !canEdit, why: "Your role cannot edit contacts" }], primary: { label: "Edit details", a: "contacts-edit", id: c.id, key: "e", disabled: !canEdit, why: "Your role cannot edit contacts" } })}
      <div class="grid g2"><div class="card"><h3>Details</h3><dl class="kv">
        ${c.org ? h`<dt>Organization</dt><dd>${c.org}</dd>` : ""}${c.role ? h`<dt>Role</dt><dd>${c.role}</dd>` : ""}
        <dt>Email</dt><dd>${c.email || "—"}</dd><dt>Phone</dt><dd>${c.phone || "—"}</dd><dt>ID status</dt><dd>${c.idStatus ? chip(c.idStatus) : "Not identified"}</dd>${fieldVals(c).map(([k, v]) => h`<dt>${k}</dt><dd>${v}</dd>`)}<dt>Tags</dt><dd>${(c.tags || []).map((t) => ui.badge(t))}${(c.tags || []).length ? "" : "none"}</dd></dl></div>
      <div class="card"><h3>Conflict-relevant names</h3>${al.length ? al.map((a) => h`<div class="row" style="padding:4px 0"><b class="grow">${a.alias}</b><span class="muted small">${a.note}</span></div>`) : h`<p class="muted">No aliases recorded.</p>`}
        ${shared.length ? h`<p class="small" style="color:var(--warn)">Shares a name with ${shared.map((a) => A.contact(a.contactId).name).join(", ")}. Conflict searches match both.</p>` : ""}
        <button class="btn small" data-a="contacts-alias" data-id="${c.id}" ${canEdit ? "" : no}>Add alias</button></div></div>
      <h3 style="margin:18px 0 8px">Matters and roles</h3>
      ${ui.table([{ label: "Number", key: "number" }, { label: "Matter", render: (m) => h`${m.name}${canSee(m) ? "" : h` ${ui.status("directory", "Directory only")}`}` }, { label: "Role", render: (m) => ui.badge(roleOn(c, m)) }, { label: "Stage", key: "stage" }], ms, { onRow: "matters-open", empty: "Not on any matter you can see" })}
      ${pend.length ? h`<h3 style="margin:18px 0 8px">Pending merges</h3>${pend.map((g) => ui.gateCard(g))}` : ""}</div>`);
  }
  A.effect("contacts-merge", (p) => {
    const keep = A.store.get("contacts", p.keep), drop = A.store.get("contacts", p.drop); if (!keep || !drop) return;
    A.store.put("aliases", { id: A.id("al"), contactId: keep.id, alias: drop.name, note: `Merged from duplicate record on ${A.today}` });
    A.store.where("aliases", (a) => a.contactId === drop.id).forEach((a) => A.store.put("aliases", { id: a.id, contactId: keep.id }));
    A.store.all("matters").forEach((m) => { if (m.clientId === drop.id) A.store.put("matters", { id: m.id, clientId: keep.id }); if ((m.opposing || []).includes(drop.id)) A.store.put("matters", { id: m.id, opposing: [...new Set(m.opposing.map((x) => (x === drop.id ? keep.id : x)))] }); });
    A.store.remove("contacts", drop.id);
    A.audit("contact.merged", { target: keep.id, note: `${drop.name} merged into ${keep.name}` });
  });

  /* ---------- actions ---------- */
  A.action("matters-view", (el) => { st.view = el.dataset.id; A.refresh(); });
  A.action("matters-open", (el) => { const m = A.store.get("matters", el.dataset.id); if (m && canSee(m)) A.go(`matters/${m.id}`); else A.toast("Directory only: your role cannot open this matter", "bad"); });
  const base = () => `matters/${A.route().split("/")[1]}`;
  A.action("matters-tab", (el) => { const mid = A.route().split("/")[1]; st.tab[mid] = el.dataset.id; A.go(base()); });
  A.action("matters-doc", (el) => { const d = A.store.get("docs", el.dataset.id); if (!d) return; st.doc[d.matterId] = d.id; st.tab[d.matterId] = "documents"; A.audit("document.viewed", { target: d.id, matterId: d.matterId, note: d.name }); A.go(`matters/${d.matterId}`); });
  A.action("matters-doc-close", (el) => { delete st.doc[el.dataset.id]; A.go(`matters/${el.dataset.id}`); });
  A.action("matters-ask", (el) => { const m = A.matter(el.dataset.id); const i = document.getElementById("mask"); if (m) askMatter(m, i ? i.value : ""); });
  A.action("matters-ask-clear", (el) => { delete st.ask[el.dataset.id]; A.refresh(); });
  A.action("matters-staff", (el) => {
    const m = A.matter(el.dataset.id); if (!m || !canEditMatter(m)) return;
    const opts = A.store.all("users").filter((u) => STAFF_ROLES.includes(u.role) && !(m.wall || []).includes(u.id));
    A.modal(`Responsible staff · ${m.number}`, h`<label class="field">Staff member who runs this file day to day<select name="staff"><option value="">None</option>${opts.map((u) => h`<option value="${u.id}" ${u.id === staffOf(m) ? A.raw("selected") : ""}>${u.name} · ${A.roleName(u.role)}</option>`)}</select></label>
      <p class="muted small">The responsible lawyer (${A.user(m.responsible).name}) stays accountable. Staff are added to the team if needed.</p>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => { const v = A.formValues(d).staff || "";
        A.store.put("matterstaff", { id: m.id, staff: v });
        if (v && !(m.team || []).includes(v)) A.store.put("matters", { id: m.id, team: (m.team || []).concat(v) });
        A.audit("matter.staff", { target: m.id, matterId: m.id, note: v ? A.user(v).name : "cleared" }); } }]);
  });
  A.action("matters-doc-meta", (el) => {
    const d = A.store.get("docs", el.dataset.id); const m = d && A.matter(d.matterId); if (!m || !canEditDocs(m) || lockedByOther(d)) return;
    const x = meta(d);
    A.modal(`Details · ${d.name}`, h`<div class="form"><label class="field">Status<select name="status">${["draft", "final", "filed"].map((s) => h`<option value="${s}" ${s === x.status ? A.raw("selected") : ""}>${cap1(s)}</option>`)}</select></label>
      <label class="field">Tags (comma separated)<input name="tags" value="${x.tags.join(", ")}"></label>
      <label class="field">Received<input name="received" type="date" value="${x.received}"></label></div>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (dlg) => { const v = A.formValues(dlg);
        const tags = String(v.tags || "").split(",").map((t) => t.trim().toLowerCase()).filter(Boolean);
        A.store.put("docmeta", { id: d.id, status: v.status || x.status, tags, received: v.received || "" });
        A.audit("document.details", { target: d.id, matterId: d.matterId, note: `${v.status || x.status}${v.status !== x.status ? ` (was ${x.status})` : ""}; tags ${tags.join(", ") || "none"}` }); } }]);
  });
  A.action("matters-doc-lock", (el) => {
    const d = A.store.get("docs", el.dataset.id); const m = d && A.matter(d.matterId); if (!m || !canEditDocs(m) || lockedByOther(d)) return;
    const x = meta(d); const me = A.me().id;
    A.store.put("docmeta", { id: d.id, locked: !x.locked, lockedBy: x.locked ? "" : me, lockedAt: x.locked ? "" : A.now() });
    A.audit(x.locked ? "document.unlocked" : "document.locked", { target: d.id, matterId: d.matterId, note: d.name });
  });
  A.action("matters-doc-revise", (el) => { const d = A.store.get("docs", el.dataset.id); const m = d && A.matter(d.matterId); if (m) startRevision(m, d); });
  A.action("matters-chron-build", (el) => {
    const m = A.matter(el.dataset.id); if (!m || !canEditDocs(m)) return;
    A.agent.run({ title: `Chronology: ${m.number}`, matterId: m.id, steps: ["Reading every document on the matter", "Finding dated events", "Linking each event to its line"],
      done() { const rows = extractChronology(m);
        A.store.put("chronos", { id: `chr-${m.id}`, matterId: m.id, rows, status: "draft", built: A.now(), gateId: "", confirmedBy: "" });
        return { title: `${rows.length} dated events` }; } });
    A.toast("Building the chronology");
  });
  A.action("matters-chron-drop", (el) => {
    const c = A.store.get("chronos", `chr-${el.dataset.id}`); const m = A.matter(el.dataset.id); if (!c || !m || !canEditDocs(m) || c.status !== "draft") return;
    A.store.put("chronos", { id: c.id, rows: c.rows.filter((r) => r.id !== el.dataset.row) });
    A.audit("chronology.edited", { target: c.id, matterId: m.id, note: "Event removed" });
  });
  A.action("matters-chron-confirm", (el) => {
    const c = A.store.get("chronos", `chr-${el.dataset.id}`); const m = A.matter(el.dataset.id); if (!c || !m || !canEditDocs(m) || c.status !== "draft" || !c.rows.length) return;
    const g = A.gate({ kind: "chronology.confirm", cap: "documents.release", title: `Chronology: ${m.name}`, matterId: m.id, preparedBy: "agent",
      summary: `${c.rows.length} dated events, each tied to a source line. Confirm before it is shared with anyone.`, body: chronBody(c),
      cites: c.rows.map((r) => r.cite), checks: [{ label: "Every event cites a source line", ok: c.rows.every((r) => r.cite) }, { label: "Events in date order", ok: true }],
      effect: "matters-chron-confirm", payload: { matterId: m.id } });
    A.store.put("chronos", { id: c.id, status: "pending", gateId: g.id });
  });
  A.action("matters-chron-share", (el) => {
    const c = A.store.get("chronos", `chr-${el.dataset.id}`); const m = A.matter(el.dataset.id); if (!c || !m || c.status !== "confirmed" || !canEditDocs(m)) return;
    const text = c.rows.map((r) => `${r.date}  ${r.text}`); const pages = []; for (let i = 0; i < text.length; i += 12) pages.push(text.slice(i, i + 12));
    const d = A.store.put("docs", { id: A.id("d"), matterId: m.id, name: `Chronology (${A.date(A.today)})`, kind: "chronology", added: A.today, by: "agent", approvedBy: c.confirmedBy, pages: pages.length ? pages : [[""]] });
    A.store.put("docmeta", { id: d.id, status: "final", tags: ["chronology"], received: "" });
    A.audit("chronology.shared", { target: d.id, matterId: m.id, note: `Confirmed by ${A.user(c.confirmedBy).name}` });
    A.toast("Chronology filed as a final document, ready to share", "ok");
  });
  A.action("matters-mail-open", (el) => { st.mail[el.dataset.id] = !st.mail[el.dataset.id]; A.refresh(); });
  A.action("matters-mail-file", (el) => {
    const e = A.store.get("emails", el.dataset.id); const sel = document.getElementById(`mf-${el.dataset.id}`); const m = sel && sel.value && A.matter(sel.value);
    if (!e) return; if (!m) return A.toast("Choose a matter first");
    if (!canEditDocs(m)) return A.toast("You cannot file to this matter", "bad");
    A.store.put("emails", { id: e.id, matterId: m.id, filedBy: A.me().id });
    A.audit("email.filed", { target: e.id, matterId: m.id, note: `${e.subject} → ${m.number}` }); A.toast(`Filed to ${m.number}`, "ok");
  });
  A.action("matters-mail-unfile", (el) => {
    const e = A.store.get("emails", el.dataset.id); const m = e && A.matter(e.matterId); if (!m || !canEditDocs(m)) return;
    A.store.put("emails", { id: e.id, matterId: null, filedBy: "" }); A.audit("email.unfiled", { target: e.id, matterId: m.id, note: e.subject });
  });
  A.action("matters-mail-remove", (el) => {
    const e = A.store.get("emails", el.dataset.id); if (!e || !canQueue()) return;
    A.modal("Do not file and remove", h`<p>Remove “${e.subject}” from this app? It stays in the mailbox. A tombstone keeps it from being imported again.</p>`,
      [{ label: "Cancel" }, { label: "Remove", primary: true, run: () => {
        A.store.put("mailtombs", { id: A.id("tomb"), msgId: e.msgId, by: A.me().id, at: A.now() });
        A.store.remove("emails", e.id); A.audit("email.removed", { target: e.id, note: `${e.subject} (tombstoned)` }); } }]);
  });
  A.action("matters-mail-sync", () => {
    if (!canQueue()) return;
    const have = new Set(A.store.all("emails").map((e) => e.msgId)), dead = new Set(A.store.all("mailtombs").map((t) => t.msgId));
    const fresh = INBOX.filter((e) => !have.has(e.msgId) && !dead.has(e.msgId));
    fresh.forEach((e) => A.store.put("emails", { ...e, id: A.id("em"), matterId: null }));
    const skipped = INBOX.filter((e) => dead.has(e.msgId)).length;
    A.audit("email.synced", { note: `${fresh.length} new, ${skipped} skipped (removed)` });
    A.toast(`${fresh.length} new message${fresh.length === 1 ? "" : "s"}${skipped ? `; ${skipped} removed message${skipped > 1 ? "s" : ""} skipped` : ""}`);
  });
  A.action("matters-draft", (el) => { const m = A.matter(el.dataset.id); if (m) draftModal(m); });
  A.action("matters-note", (el) => {
    const m = A.matter(el.dataset.id); const t = document.getElementById("mnote"); const text = t ? t.value.trim() : "";
    if (!m || !text) return A.toast("Write a note first");
    A.store.put("notes", { id: A.id("n"), matterId: m.id, by: A.me().id, at: A.now(), text }); A.audit("matter.note", { target: m.id, matterId: m.id });
  });
  A.action("contacts-open", (el) => A.go(`contacts/${el.dataset.id}`));
  A.action("contacts-edit", (el) => {
    const c = A.store.get("contacts", el.dataset.id); if (!c || !A.can("contacts.edit", "act")) return;
    A.modal(`Edit ${c.name}`, h`<div class="form"><label class="field">Email<input name="email" value="${c.email || ""}"></label><label class="field">Phone<input name="phone" value="${c.phone || ""}"></label></div><p class="muted small">Name changes and merges go through a conflict review instead.</p>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => { const v = A.formValues(d); A.store.put("contacts", { id: c.id, email: v.email, phone: v.phone }); A.audit("contact.edited", { target: c.id, note: "Contact details" }); } }]);
  });
  A.action("contacts-alias", (el) => {
    const c = A.store.get("contacts", el.dataset.id); if (!c || !A.can("contacts.edit", "act")) return;
    A.modal(`Add alias for ${c.name}`, h`<div class="form"><label class="field">Alias<input name="alias"></label><label class="field">Why it matters<input name="note" placeholder="Former name, trade name, parent…"></label></div>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d); if (!v.alias) return false; A.store.put("aliases", { id: A.id("al"), contactId: c.id, alias: v.alias, note: v.note }); A.audit("contact.alias.added", { target: c.id, note: v.alias }); } }]);
  });
  A.action("contacts-merge", (el) => {
    const c = A.store.get("contacts", el.dataset.id); if (!c || !A.can("contacts.edit", "act")) return;
    const others = visibleContacts().filter((x) => x.id !== c.id && x.kind === c.kind);
    A.modal(`Merge into ${c.name}`, h`<label class="field">Duplicate record to merge in<select name="drop">${others.map((o) => h`<option value="${o.id}">${o.name}</option>`)}</select></label>
      <p class="muted small">A merge changes conflict data. It goes to a lawyer who can clear conflicts; the duplicate's name is kept as an alias.</p>`,
      [{ label: "Cancel" }, { label: "Request merge", primary: true, run: (d) => { const v = A.formValues(d); const o = A.store.get("contacts", v.drop); if (!o) return;
        A.gate({ kind: "contact.merge", cap: "conflicts.clear", title: `Merge ${o.name} into ${c.name}`, preparedBy: A.me().id,
          summary: `Moves ${contactMatters(o).length} matter roles and ${A.store.where("aliases", (a) => a.contactId === o.id).length} aliases to ${c.name}. ${o.name} becomes an alias.`,
          checks: [{ label: "Same kind of contact", ok: o.kind === c.kind }, { label: "Conflict search re-run on merged names", ok: true }], effect: "contacts-merge", payload: { keep: c.id, drop: o.id } });
        A.toast("Merge sent for conflict review"); } }]);
  });

  /* ---------- list, split view and bulk actions ---------- */
  A.action("matters-saved", (el) => { st.saved = el.dataset.id; A.selection("matters").clear(); A.refresh(); });
  A.action("matters-sel", (el) => { if (st.sel === el.dataset.id) { const m = A.store.get("matters", st.sel); if (m && canSee(m)) return A.go(`matters/${m.id}`); } st.sel = el.dataset.id; A.refresh(); });
  A.change("matters-q", (el) => { st.q = el.value; A.refresh(); });
  A.change("matters-area", (el) => { st.area = el.value; A.refresh(); });
  A.change("matters-resp", (el) => { st.resp = el.value; A.refresh(); });
  A.action("matters-bulk-staff", () => {
    const ms = sid("matters").map((id) => A.matter(id)).filter(Boolean); const ok = ms.filter(canEditMatter);
    if (!ok.length) return A.toast(roleWhy("change responsible staff").replace("this matter", "the selected matters"), "bad");
    const opts = A.store.all("users").filter((u) => STAFF_ROLES.includes(u.role));
    A.modal(`Responsible staff · ${ok.length} matter${ok.length > 1 ? "s" : ""}`, h`<label class="field">Staff member<select name="staff"><option value="">None</option>${opts.map((u) => h`<option value="${u.id}">${u.name} · ${A.roleName(u.role)}</option>`)}</select></label>
      ${ok.length < ms.length ? h`<p class="muted small">${ms.length - ok.length} selected matter${ms.length - ok.length > 1 ? "s are" : " is"} skipped: you cannot edit ${ms.length - ok.length > 1 ? "them" : "it"}.</p>` : ""}
      <p class="muted small">Screened staff are skipped on matters they are walled from.</p>`,
      [{ label: "Cancel" }, { label: "Apply", primary: true, run: (d) => { const v = A.formValues(d).staff || ""; let n = 0;
        ok.forEach((m) => { if (v && (m.wall || []).includes(v)) return; n++; A.store.put("matterstaff", { id: m.id, staff: v });
          if (v && !(m.team || []).includes(v)) A.store.put("matters", { id: m.id, team: (m.team || []).concat(v) });
          A.audit("matter.staff", { target: m.id, matterId: m.id, note: v ? A.user(v).name : "cleared" }); });
        A.selection("matters").clear(); A.toast(`Responsible staff set on ${n} matter${n === 1 ? "" : "s"}`, "ok"); } }]);
  });
  A.action("matters-bulk-csv", () => {
    const ms = sid("matters").map((id) => A.store.get("matters", id)).filter((m) => m && A.canMatter(m, "matters.directory"));
    const q = (v) => `"${String(v || "").replace(/"/g, '""')}"`;
    const csv = [["Number", "Matter", "Client", "Area", "Stage", "Status", "Responsible lawyer"].join(",")].concat(ms.map((m) => [m.number, m.name, canSee(m) ? contactName(m.clientId) : "", m.area, canSee(m) ? m.stage : "", m.status, A.user(m.responsible).name].map(q).join(","))).join("\n");
    try { if (navigator.clipboard) navigator.clipboard.writeText(csv); } catch (e) { /* clipboard blocked */ }
    A.audit("matters.exported", { note: `${ms.length} matters copied as CSV` }); A.toast(`${ms.length} rows copied as CSV`);
  });
  A.action("matters-new", () => {
    if (!(A.can("matters.open", "act") && A.can("intake.leads"))) return A.toast(`${A.roleName(A.me().role)} cannot open matters`, "bad");
    A.toast("Matters open from an accepted, conflict-cleared intake. Pick the lead to open."); A.go("intake");
  });
  /** Stage moves: ordinary moves are logged; a move to Closed starts the closing workflow, so a lawyer approves it (G-ST). */
  function moveStage(m, to) {
    if (!m || !to || to === m.stage) return;
    if (!canEditMatter(m)) { A.toast(roleWhy("move the stage"), "bad"); return A.refresh(); }
    if (to === "Closed") {
      if (pendingOn(m).some((g) => g.effect === "matters-stage")) { A.toast("A stage move is already waiting for approval"); return A.refresh(); }
      A.gate({ kind: "matter.stage", cap: "matters.close", title: `Close ${m.name}`, matterId: m.id, preparedBy: A.me().id,
        summary: `Move ${m.number} from ${m.stage} to Closed. Closing starts the retention clock and the final reporting letter.`,
        checks: [{ label: "No other approval pending on this matter", ok: !pendingOn(m).length }, { label: "Moved by a person, not the agent", ok: true }],
        effect: "matters-stage", payload: { matterId: m.id, from: m.stage, to } });
      A.toast("Closing needs a lawyer's approval. Sent to Approvals."); return A.refresh();
    }
    A.store.put("matters", { id: m.id, stage: to });
    A.audit("matter.stage", { target: m.id, matterId: m.id, note: `${m.stage} → ${to}` }); A.toast(`${m.number} moved to ${to}`, "ok");
  }
  A.change("matters-stage", (el) => moveStage(A.matter(el.dataset.id), el.value));
  A.action("matters-move", (el) => {
    const m = A.matter(el.dataset.id); if (!m || !canEditMatter(m)) return;
    A.modal(`Move ${m.number}`, h`<label class="field">Stage<select name="stage">${stagesFor(m).map((s) => h`<option ${s === m.stage ? A.raw("selected") : ""}>${s}</option>`)}</select></label>`,
      [{ label: "Cancel" }, { label: "Move", primary: true, run: (d) => moveStage(m, A.formValues(d).stage) }]);
  });
  A.effect("matters-stage", (p) => {
    const m = A.store.get("matters", p.matterId); if (!m) return;
    A.store.put("matters", { id: m.id, stage: p.to, ...(p.to === "Closed" ? { status: "closed", closed: A.today } : {}) });
    A.audit("matter.stage", { target: m.id, matterId: m.id, note: `${p.from} → ${p.to} (approved)` });
  });
  A.action("matters-dstat", (el) => { st.dstat = el.dataset.id; A.refresh(); });
  A.change("matters-dq", (el) => { st.dq = el.value; A.refresh(); });
  const bulkDocs = () => sid("mdocs").map((id) => A.store.get("docs", id)).filter((d) => { const m = d && A.matter(d.matterId); return m && canEditDocs(m) && !lockedByOther(d); });
  A.action("matters-dbulk-status", (el) => {
    const ds = bulkDocs(), to = el.dataset.st; if (!ds.length) return A.toast("Nothing you can change is selected", "bad");
    ds.forEach((d) => { const was = meta(d).status; A.store.put("docmeta", { id: d.id, status: to }); A.audit("document.details", { target: d.id, matterId: d.matterId, note: `${to} (was ${was})` }); });
    A.selection("mdocs").clear(); A.toast(`${ds.length} document${ds.length > 1 ? "s" : ""} marked ${to}`, "ok");
  });
  A.action("matters-dbulk-tag", () => {
    const ds = bulkDocs(); if (!ds.length) return A.toast("Nothing you can change is selected", "bad");
    A.modal(`Add tag · ${ds.length} document${ds.length > 1 ? "s" : ""}`, h`<label class="field">Tag<select name="tag">${tagList().map((t) => h`<option>${t}</option>`)}</select></label>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const t = String(A.formValues(d).tag || "").trim().toLowerCase(); if (!t) return false;
        ds.forEach((x) => { const tags = meta(x).tags; if (!tags.includes(t)) { A.store.put("docmeta", { id: x.id, tags: tags.concat(t) }); A.audit("document.details", { target: x.id, matterId: x.matterId, note: `tag ${t} added` }); } });
        A.selection("mdocs").clear(); } }]);
  });
  A.action("matters-dbulk-trash", () => { const ds = bulkDocs().filter((d) => !meta(d).locked); if (!ds.length) return A.toast("Nothing you can delete is selected", "bad"); ds.forEach(trashDoc); A.selection("mdocs").clear(); A.toast(`${ds.length} moved to Deleted documents. Restore within ${docSettings().trashDays} days.`); });
  A.action("matters-doc-trash", (el) => { const d = A.store.get("docs", el.dataset.id); const m = d && A.matter(d.matterId); if (!m || !canEditDocs(m) || meta(d).locked) return; delete st.doc[m.id]; trashDoc(d); A.toast(`${d.name} moved to Deleted documents`); });
  A.action("contacts-tab", (el) => { st.ctab = el.dataset.id; A.refresh(); });
  A.action("contacts-sel", (el) => { if (st.csel === el.dataset.id) return A.go(`contacts/${st.csel}`); st.csel = el.dataset.id; A.refresh(); });
  A.change("contacts-q", (el) => { st.cq = el.value; A.refresh(); });
  A.action("contacts-bulk-tag", () => {
    const cs = sid("contacts").map((id) => A.store.get("contacts", id)).filter(Boolean); if (!cs.length || !A.can("contacts.edit", "act")) return;
    A.modal(`Tag ${cs.length} contact${cs.length > 1 ? "s" : ""}`, h`<label class="field">Tag<select name="tag">${tagList().map((t) => h`<option>${t}</option>`)}</select></label>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const t = A.formValues(d).tag; cs.forEach((c) => { if (!(c.tags || []).includes(t)) A.store.put("contacts", { id: c.id, tags: (c.tags || []).concat(t) }); });
        A.audit("contact.tagged", { note: `${t} on ${cs.length} contacts` }); A.selection("contacts").clear(); } }]);
  });
  A.action("contacts-bulk-csv", () => {
    const cs = sid("contacts").map((id) => A.store.get("contacts", id)).filter(Boolean); const q = (v) => `"${String(v || "").replace(/"/g, '""')}"`;
    const csv = [["Name", "Kind", "Email", "Phone"].join(",")].concat(cs.map((c) => [c.name, c.kind, c.email, c.phone].map(q).join(","))).join("\n");
    try { if (navigator.clipboard) navigator.clipboard.writeText(csv); } catch (e) { /* clipboard blocked */ }
    A.audit("contacts.exported", { note: `${cs.length} contacts copied as CSV` }); A.toast(`${cs.length} rows copied as CSV`);
  });
  A.action("contacts-new", () => {
    if (!A.can("contacts.edit", "act")) return A.toast(`${A.roleName(A.me().role)} cannot add contacts`, "bad");
    A.modal("New contact", h`<div class="form"><label class="field">Kind<select name="kind"><option value="person">Person</option><option value="org">Organization</option></select></label>
      <label class="field">Name<input name="name" placeholder="Full legal name"></label><label class="field">Email<input name="email" type="email"></label><label class="field">Phone<input name="phone" placeholder="416-555-0100"></label></div>
      <p class="muted small">A duplicate check runs on save. The name enters the conflict index once it is used on a matter.</p>`,
      [{ label: "Cancel" }, { label: "Create", primary: true, run: (d) => { const v = A.formValues(d); const name = String(v.name || "").trim(); if (!name) { A.toast("Enter a name"); return false; }
        const k = words(name).slice(0, 2).join(" ");
        const dup = A.store.all("contacts").find((c) => (v.email && c.email === v.email) || (k && words(c.name).slice(0, 2).join(" ") === k) || aliasesOf(c).some((a) => a.alias.toLowerCase() === name.toLowerCase()));
        const c = A.store.put("contacts", { kind: v.kind === "org" ? "org" : "person", name, email: v.email || "", phone: v.phone || "", proposed: true });
        A.audit("contact.created", { target: c.id, note: name });
        st.csel = c.id; if (dup) A.toast(`Possible duplicate of ${dup.name}. Review under Possible duplicates.`, "bad"); else A.toast(`${name} added`, "ok"); } }]);
  });

  /* ---------- keyboard: the core layer handles j/k, Enter, x, / and n; page keys below ---------- */
  A.ui.keys("matters", { m: { label: "Move the focused board card to another stage", fn: () => { if (st.view !== "board" || A.route() !== "matters") return false; const c = document.activeElement && document.activeElement.closest && document.activeElement.closest(".kcard[data-mid]"); const b = c && c.querySelector('[data-a="matters-move"]'); if (!b) return false; b.click(); return true; } } });

  /* =====================================================================================================
     Setup and document screens (round 2): practice areas, matter templates, numbering, access report,
     contact import and fields, engagement letters, signatures, client signing, templates and assembly,
     precedents, client sharing, deleted documents, document settings, analysis, bundles, firm library.
     Screens without a backend are working mocks marked "demo".
     ===================================================================================================== */
  const canSetup = () => A.can("platform.firm_profile", "act");
  const setupWhy = () => `${A.roleName(A.me().role)} cannot change firm setup`;
  const canTpl = () => A.can("documents.templates", "act");
  const hash = (s) => { let x = 5381; for (const ch of String(s)) x = ((x * 33) ^ ch.charCodeAt(0)) >>> 0; return x.toString(16).padStart(8, "0"); };
  const toPages = (lines) => { const p = []; for (let i = 0; i < lines.length; i += 12) p.push(lines.slice(i, i + 12)); return p.length ? p : [[""]]; };
  const locked = (msg) => h`<div class="page"><div class="locked"><b>No access</b>${msg}</div></div>`;
  const setupTabs = (on) => ui.tabs([{ id: "matters/setup", label: "Practice areas" }, { id: "matters/setup/templates", label: "Matter templates" }, { id: "matters/setup/numbering", label: "Numbering" }], on, "matters-go");
  A.action("matters-go", (el) => A.go(el.dataset.id));

  /* ---------- practice areas, custom fields and stages ---------- */
  function areasPage(el) {
    A.setContext({ kind: "matters", label: "Practice areas" });
    const rows = A.store.all("practiceareas"), sel = rows.find((a) => a.id === st.paSel) || rows[0]; st.paSel = sel && sel.id;
    const inUse = (a) => A.store.all("matters").filter((m) => m.area === a.name).length;
    const detail = sel && h`<div class="card"><div class="row"><h3 class="grow" style="margin:0">${sel.name}</h3>${ui.btn({ label: "Edit", a: "matters-pa-edit", id: sel.id, key: "e", small: true, disabled: !canSetup(), why: setupWhy() })}</div>
      <dl class="kv"><dt>Stages</dt><dd>${sel.stages.join(" → ")}</dd><dt>Custom fields</dt><dd>${sel.fields.join(", ") || "none"}</dd>
        <dt>Default task plan</dt><dd>${sel.plan.join("; ") || "none"}</dd><dt>Conflict roles</dt><dd>${sel.conflictRoles.join(", ")}</dd><dt>Matters</dt><dd>${inUse(sel)}</dd></dl>
      <p class="muted small">A matter keeps its current stage if you remove that stage here; it shows at the end of its list until moved.</p></div>`;
    A.render(el, h`<div class="page">${ui.header({ title: "Matter setup", back: { label: "Matters", to: "matters" }, sub: "Each practice area defines its stages, custom fields, default task plan and the roles that enter the conflict index.",
        primary: { label: "New practice area", a: "matters-pa-new", key: "n", disabled: !canSetup(), why: setupWhy() } })}
      ${setupTabs("matters/setup")}
      ${ui.split(ui.list(rows.map((a) => ({ id: a.id, title: a.name, sub: `${a.stages.length} stages · ${a.fields.length} fields`, right: h`<span class="muted small">${inUse(a)}</span>`, on: sel && a.id === sel.id })), "matters-pa-sel"), detail)}</div>`);
  }
  A.action("matters-pa-sel", (el) => { st.paSel = el.dataset.id; A.refresh(); });
  function paModal(a) {
    const list = (v) => String(v || "").split(/\n|;/).map((x) => x.trim()).filter(Boolean);
    A.modal(a ? `Edit ${a.name}` : "New practice area", h`<div class="form"><label class="field">Name<input name="name" value="${a ? a.name : ""}"></label>
      <label class="field">Stages (one per line, last is the closed stage)<textarea name="stages" rows="6">${(a ? a.stages : ["Intake complete", "Closed"]).join("\n")}</textarea></label>
      <label class="field">Custom fields (one per line)<textarea name="fields" rows="3">${(a ? a.fields : []).join("\n")}</textarea></label>
      <label class="field">Default task plan (one per line)<textarea name="plan" rows="3">${(a ? a.plan : []).join("\n")}</textarea></label>
      <label class="field">Conflict-relevant roles (one per line)<textarea name="roles" rows="3">${(a ? a.conflictRoles : ["Client", "Opposing party", "Opposing counsel"]).join("\n")}</textarea></label></div>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => { const v = A.formValues(d); const name = String(v.name || "").trim(); const stages = list(v.stages);
        if (!name || stages.length < 2) { A.toast("Give a name and at least two stages"); return false; }
        if (!stages.includes("Closed")) stages.push("Closed");
        const r = A.store.put("practiceareas", { ...(a ? { id: a.id } : {}), name, stages, fields: list(v.fields), plan: list(v.plan), conflictRoles: list(v.roles) });
        st.paSel = r.id; A.audit("setup.practice_area", { target: r.id, note: `${name}: ${stages.length} stages` }); } }]);
  }
  A.action("matters-pa-new", () => { if (canSetup()) paModal(null); });
  A.action("matters-pa-edit", (el) => { const a = A.store.get("practiceareas", el.dataset.id); if (a && canSetup()) paModal(a); });

  /* ---------- matter templates ---------- */
  A.seed("mtemplates", [
    { id: "mt-1", name: "Civil claim, plaintiff", area: "Civil litigation", fee: "hourly", folders: ["Pleadings", "Correspondence", "Productions", "Court"], roles: ["Responsible lawyer", "Paralegal"], work: "Plaintiff claim plan", def: true },
    { id: "mt-2", name: "Residential purchase", area: "Real estate", fee: "flat", folders: ["Agreement", "Title", "Closing"], roles: ["Responsible lawyer", "Law clerk"], work: "Purchase closing plan", def: false },
    { id: "mt-3", name: "Estate administration", area: "Estates", fee: "hourly", folders: ["Will", "Probate", "Accounts"], roles: ["Responsible lawyer", "Paralegal"], work: "Probate plan", def: false },
    { id: "mt-4", name: "Wrongful dismissal", area: "Employment", fee: "contingency", folders: ["Employment records", "Mitigation", "Correspondence"], roles: ["Responsible lawyer"], work: "Dismissal demand plan", def: false },
  ]);
  function templatesPage(el) {
    A.setContext({ kind: "matters", label: "Matter templates" });
    const rows = A.store.all("mtemplates");
    A.render(el, h`<div class="page">${ui.header({ title: "Matter setup", back: { label: "Matters", to: "matters" }, sub: "Presets that fill a new matter. The firm default applies when intake picks no template.",
        primary: { label: "New template", a: "matters-mt-edit", key: "n", disabled: !canSetup(), why: setupWhy() } })}
      ${setupTabs("matters/setup/templates")}
      ${ui.table([{ label: "Template", render: (t) => h`<b>${t.name}</b>${t.def ? h` ${ui.status("active", "Firm default")}` : ""}` }, { label: "Area", key: "area" }, { label: "Fee basis", render: (t) => cap1(t.fee) },
        { label: "Folders", render: (t) => h`<span class="small">${t.folders.join(", ")}</span>` }, { label: "Team roles", render: (t) => h`<span class="small">${t.roles.join(", ")}</span>` }, { label: "Work plan", key: "work" },
        { label: "", render: (t) => h`${t.def ? "" : ui.btn({ label: "Make default", a: "matters-mt-default", id: t.id, small: true, disabled: !canSetup(), why: setupWhy() })}${ui.btn({ label: "Edit", a: "matters-mt-edit", id: t.id, small: true, disabled: !canSetup(), why: setupWhy() })}` }],
        rows, { empty: "No matter templates yet" })}</div>`);
  }
  A.action("matters-mt-default", (el) => { if (!canSetup()) return; A.store.all("mtemplates").forEach((t) => A.store.put("mtemplates", { id: t.id, def: t.id === el.dataset.id })); A.audit("setup.matter_template.default", { target: el.dataset.id }); });
  A.action("matters-mt-edit", (el) => {
    if (!canSetup()) return; const t = A.store.get("mtemplates", el.dataset.id || "") || null;
    A.modal(t ? `Edit ${t.name}` : "New matter template", h`<div class="form"><label class="field">Name<input name="name" value="${t ? t.name : ""}"></label>
      <label class="field">Practice area<select name="area">${A.store.all("practiceareas").map((a) => h`<option ${t && t.area === a.name ? A.raw("selected") : ""}>${a.name}</option>`)}</select></label>
      <label class="field">Fee basis<select name="fee">${["hourly", "flat", "contingency"].map((f) => h`<option ${t && t.fee === f ? A.raw("selected") : ""}>${f}</option>`)}</select></label>
      <label class="field">Folders (comma separated)<input name="folders" value="${t ? t.folders.join(", ") : "Correspondence"}"></label>
      <label class="field">Team roles (comma separated)<input name="roles" value="${t ? t.roles.join(", ") : "Responsible lawyer"}"></label>
      <label class="field">Work plan<input name="work" value="${t ? t.work : ""}"></label></div>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => { const v = A.formValues(d); if (!String(v.name || "").trim()) { A.toast("Name the template"); return false; }
        const sp = (x) => String(x || "").split(",").map((y) => y.trim()).filter(Boolean);
        const r = A.store.put("mtemplates", { ...(t ? { id: t.id } : { def: false }), name: v.name.trim(), area: v.area, fee: v.fee, folders: sp(v.folders), roles: sp(v.roles), work: v.work });
        A.audit("setup.matter_template", { target: r.id, note: r.name }); } }]);
  });

  /* ---------- matter numbering ---------- */
  A.seed("mxsettings", [{ id: "numbering", pattern: "{YYYY}-{SEQ:4}", next: 161, display: "number-name", perArea: false },
    { id: "docs", types: ["letter", "pleading", "affidavit", "agreement", "email", "memo", "chronology", "bundle"], naming: "{YYYY}-{MM}-{DD} {MATTER} {NAME}", trashDays: 30, signOrder: "sequential", reminderDays: 3, linkDays: 7, maxMb: 100 }]);
  const numbering = () => A.store.get("mxsettings", "numbering");
  const docSettings = () => A.store.get("mxsettings", "docs") || { trashDays: 30, linkDays: 7, reminderDays: 3, types: [], naming: "" };
  function formatNumber(pattern, seq, area) {
    const [y, mo] = A.today.split("-"), code = String(area || "GEN").split(/\s+/).map((w) => w[0]).join("").toUpperCase();
    return String(pattern).replace(/\{YYYY\}/g, y).replace(/\{YY\}/g, y.slice(2)).replace(/\{MM\}/g, mo).replace(/\{AREA\}/g, code).replace(/\{SEQ(?::(\d))?\}/g, (_, n) => String(seq).padStart(+n || 1, "0"));
  }
  function numberingPage(el) {
    A.setContext({ kind: "matters", label: "Matter numbering" });
    const n = numbering(), draft = st.numDraft || { pattern: n.pattern, next: n.next, display: n.display };
    const used = new Set(A.store.all("matters").map((m) => m.number));
    const sample = [0, 1, 2].map((i) => formatNumber(draft.pattern, +draft.next + i, ["Civil litigation", "Real estate", "Estates"][i]));
    const clash = sample.find((x) => used.has(x)), changed = draft.pattern !== n.pattern || +draft.next !== n.next || draft.display !== n.display;
    const show = (num, name) => (draft.display === "name-number" ? `${name} (${num})` : draft.display === "number" ? num : `${num} ${name}`);
    A.render(el, h`<div class="page">${ui.header({ title: "Matter setup", back: { label: "Matters", to: "matters" }, sub: "Numbers apply to matters opened after you save. Existing numbers never change." })}
      ${setupTabs("matters/setup/numbering")}
      <div class="grid g2"><div class="card"><h3>Pattern</h3><div class="form">
        <label class="field">Pattern <span class="muted small">{YYYY} {YY} {MM} {AREA} {SEQ:4}</span><input data-c="matters-num" data-f="pattern" value="${draft.pattern}" ${canSetup() ? "" : no(setupWhy())}></label>
        <label class="field">Next sequence<input type="number" min="1" data-c="matters-num" data-f="next" value="${draft.next}" ${canSetup() ? "" : no(setupWhy())}></label>
        <label class="field">Display<select data-c="matters-num" data-f="display" ${canSetup() ? "" : no(setupWhy())}>${[["number-name", "Number then name"], ["name-number", "Name then number"], ["number", "Number only"]].map(([v, l]) => h`<option value="${v}" ${v === draft.display ? A.raw("selected") : ""}>${l}</option>`)}</select></label></div>
        <div class="row">${changed ? ui.status("draft", "Unsaved") : ui.status("active", "In use")}<span class="grow"></span>${ui.btn({ label: "Discard", a: "matters-num-reset", small: true, disabled: !changed })}${ui.btn({ label: "Save", a: "matters-num-save", primary: true, disabled: !canSetup() || !changed || !!clash, why: clash ? `${clash} is already used` : setupWhy() })}</div></div>
      <div class="card"><h3>Preview</h3>${sample.map((x, i) => h`<div class="row" style="padding:4px 0"><span style="font-family:var(--mono)" class="grow">${show(x, ["Smith v. Jones", "Purchase of 9 Oak Ave", "Estate of R. Brown"][i])}</span>${used.has(x) ? ui.status("conflict", "Already used") : ""}</div>`)}
        <p class="muted small">Current: ${show(formatNumber(n.pattern, n.next, "Civil litigation"), "Next matter")}. The highest existing number is ${[...used].sort().pop()}.</p></div></div></div>`);
  }
  A.change("matters-num", (el) => { const n = numbering(); st.numDraft = { ...(st.numDraft || { pattern: n.pattern, next: n.next, display: n.display }), [el.dataset.f]: el.dataset.f === "next" ? Math.max(1, +el.value || 1) : el.value }; A.refresh(); });
  A.action("matters-num-reset", () => { st.numDraft = null; A.refresh(); });
  A.action("matters-num-save", () => { const d = st.numDraft; if (!d || !canSetup()) return; const n = numbering();
    A.store.put("mxsettings", { id: "numbering", pattern: d.pattern, next: +d.next, display: d.display }); A.audit("setup.numbering", { note: `${n.pattern} #${n.next} → ${d.pattern} #${d.next}` }); st.numDraft = null; A.toast("Numbering saved", "ok"); });

  /* ---------- matter access report (admin: names and numbers only) ---------- */
  const sharing = (m) => (m.restricted ? "Restricted" : "Firm");
  function accessPage(el) {
    A.setContext({ kind: "matters", label: "Access report" });
    if (!A.can("admin.access_review")) return A.render(el, locked(`${A.roleName(A.me().role)} cannot review matter access.`));
    const rows = A.store.all("matters").filter((m) => !st.arF || sharing(m) === st.arF);
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "matters-access");
    A.render(el, h`<div class="page">${ui.header({ title: "Matter access report", back: { label: "Matters", to: "matters" }, sub: "How every matter is shared. You see numbers, names and people, never contents. Changes go to a lawyer who can restrict matters." })}
      ${ui.filters({ chips: [{ id: "", label: "All", count: A.store.all("matters").length }, { id: "Firm", label: "Firm-wide", count: A.store.all("matters").filter((m) => !m.restricted).length }, { id: "Restricted", label: "Restricted", count: A.store.all("matters").filter((m) => m.restricted).length }], active: st.arF || "", action: "matters-ar-f" })}
      ${ui.bulkBar("maccess", [{ label: "Restrict to team", a: "matters-access-req", data: { to: "team" } }, { label: "Open to firm", a: "matters-access-req", data: { to: "firm" } }])}
      ${ui.table([{ label: "Number", render: (m) => h`<span style="font-family:var(--mono)">${m.number}</span>` }, { label: "Matter", key: "name" }, { label: "Sharing", render: (m) => (m.restricted ? chip("restricted") : ui.status("open", "Firm")) },
        { label: "Team", render: (m) => h`<span class="small">${(m.team || []).map((u) => A.user(u).name).join(", ")}</span>` }, { label: "Screened", render: (m) => h`<span class="small">${(m.wall || []).map((u) => A.user(u).name).join(", ") || "—"}</span>` },
        { label: "Responsible", render: (m) => A.user(m.responsible).name }], rows, { select: "maccess", empty: "No matters" })}
      ${pend.length ? h`<h3 style="margin:16px 0 8px">Waiting for a lawyer</h3>${pend.map((g) => ui.gateCard(g, { compact: true }))}` : ""}</div>`);
  }
  A.action("matters-ar-f", (el) => { st.arF = el.dataset.id; A.refresh(); });
  A.action("matters-access-req", (el) => {
    if (!A.can("admin.access_review")) return; const ids = sid("maccess"); const to = el.dataset.to; if (!ids.length) return;
    const ms = ids.map((id) => A.store.get("matters", id)).filter(Boolean);
    A.gate({ kind: "matter.access", cap: "matters.restrict", title: `${to === "team" ? "Restrict" : "Open"} ${ms.length} matter${ms.length > 1 ? "s" : ""}`, preparedBy: A.me().id,
      summary: `${to === "team" ? "Limit to the matter team" : "Make visible to the whole firm"}: ${ms.map((m) => m.number).join(", ")}. Walls stay in force either way.`,
      checks: [{ label: "Walls are not changed", ok: true }, { label: "Requested by a person, not the agent", ok: true }], effect: "matters-access", payload: { ids, to } });
    A.selection("maccess").clear(); A.toast("Sent to a lawyer for approval");
  });
  A.effect("matters-access", (p) => p.ids.forEach((id) => { A.store.put("matters", { id, restricted: p.to === "team" }); A.audit("matter.access", { target: id, matterId: id, note: p.to === "team" ? "restricted to team" : "opened to firm" }); }));

  /* ---------- contact fields and tags ---------- */
  A.seed("contactfields", [{ id: "cf-1", label: "Preferred language", type: "select", options: ["English", "French", "Amharic", "Tigrinya"], applies: "person" },
    { id: "cf-2", label: "Date of birth", type: "date", applies: "person" }, { id: "cf-3", label: "Ontario corporation number", type: "text", applies: "org" }]);
  A.seed("mxtags", ["vip", "referral source", "former client", "do not contact", "disclosure", "privileged", "court record"].map((t, i) => ({ id: `tg-${i + 1}`, name: t })));
  const tagList = () => A.store.all("mxtags").map((t) => t.name);
  const fieldVals = (c) => A.store.all("contactfields").filter((f) => (c.fields || {})[f.id]).map((f) => [f.label, f.type === "date" ? A.date(c.fields[f.id]) : c.fields[f.id]]);
  function fieldsPage(el) {
    A.setContext({ kind: "contacts", label: "Contact fields" });
    const fs = A.store.all("contactfields"), ts = A.store.all("mxtags"), can = canSetup() || A.can("contacts.edit", "act");
    const usage = (t) => A.store.all("contacts").filter((c) => (c.tags || []).includes(t)).length + A.store.all("docmeta").filter((d) => (d.tags || []).includes(t)).length;
    A.render(el, h`<div class="page">${ui.header({ title: "Contact fields and tags", back: { label: "Contacts", to: "contacts" }, sub: "Firm-wide custom fields for contacts, and one tag list shared by contacts and documents.",
        primary: { label: "New field", a: "contacts-field-new", key: "n", disabled: !can, why: setupWhy() } })}
      <div class="grid g2"><div class="card"><h3>Custom fields</h3>${ui.table([{ label: "Field", key: "label" }, { label: "Type", render: (f) => (f.type === "select" ? `Choice: ${f.options.join(", ")}` : cap1(f.type)) }, { label: "For", render: (f) => (f.applies === "org" ? "Organizations" : f.applies === "person" ? "People" : "Everyone") },
        { label: "", render: (f) => ui.btn({ label: "Remove", a: "contacts-field-del", id: f.id, small: true, danger: true, disabled: !can, why: setupWhy() }) }], fs, { empty: "No custom fields" })}</div>
      <div class="card"><div class="row"><h3 class="grow">Tags</h3>${ui.btn({ label: "Add tag", a: "contacts-tag-new", small: true, disabled: !can, why: setupWhy() })}</div>
        ${ts.map((t) => h`<div class="row" style="padding:4px 0;border-bottom:1px solid var(--line)"><span class="grow">${ui.badge(t.name)}</span><span class="muted small">${usage(t.name)} in use</span>${ui.btn({ label: "Remove", a: "contacts-tag-del", id: t.id, small: true, disabled: !can || usage(t.name) > 0, why: usage(t.name) ? "In use: remove it from records first" : setupWhy() })}</div>`)}</div></div></div>`);
  }
  A.action("contacts-field-new", () => A.modal("New contact field", h`<div class="form"><label class="field">Label<input name="label"></label><label class="field">Type<select name="type"><option>text</option><option>date</option><option>select</option></select></label>
      <label class="field">Choices (for select, comma separated)<input name="options"></label><label class="field">Applies to<select name="applies"><option value="all">Everyone</option><option value="person">People</option><option value="org">Organizations</option></select></label></div>`,
    [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d); if (!String(v.label || "").trim()) { A.toast("Enter a label"); return false; }
      A.store.put("contactfields", { label: v.label.trim(), type: v.type, options: String(v.options || "").split(",").map((x) => x.trim()).filter(Boolean), applies: v.applies }); A.audit("setup.contact_field", { note: v.label }); } }]));
  A.action("contacts-field-del", (el) => { A.store.remove("contactfields", el.dataset.id); A.audit("setup.contact_field.removed", { target: el.dataset.id }); });
  A.action("contacts-tag-new", () => A.modal("Add tag", h`<label class="field">Tag<input name="tag"></label>`, [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const t = String(A.formValues(d).tag || "").trim().toLowerCase(); if (!t) return false; if (!tagList().includes(t)) A.store.put("mxtags", { name: t }); A.audit("setup.tag", { note: t }); } }]));
  A.action("contacts-tag-del", (el) => { A.store.remove("mxtags", el.dataset.id); A.audit("setup.tag.removed", { target: el.dataset.id }); });

  /* ---------- contact import: the agent maps and de-duplicates; a lawyer approves before names reach the conflict index ---------- */
  const SAMPLE_CSV = "Full name,Email,Phone,Type\nHanna Tesfaye,hanna.tesfaye@example.ca,416-555-0142,person\nAbebe Worku,abebe.w@example.ca,647-555-0181,person\nNorthgate Self Storage,,905-555-0110,org\nLakeview Dental Professional Corp.,office@lakeviewdental.example.ca,416-555-0133,org\nMiriam Haddad,m.haddad@example.ca,,person";
  function parseCsv(t) {
    const rows = String(t || "").trim().split(/\r?\n/).map((l) => l.split(",").map((x) => x.trim())); const head = (rows.shift() || []).map((x) => x.toLowerCase());
    const col = (re) => head.findIndex((x) => re.test(x));
    const ix = { name: col(/name/), email: col(/mail/), phone: col(/phone|tel/), kind: col(/type|kind/) };
    return { ix, head, rows: rows.filter((r) => r.some(Boolean)).map((r) => ({ name: r[ix.name] || "", email: ix.email >= 0 ? r[ix.email] || "" : "", phone: ix.phone >= 0 ? r[ix.phone] || "" : "", kind: /org/i.test(r[ix.kind] || "") ? "org" : "person" })) };
  }
  function dupOf(r) {
    const k = words(r.name).slice(0, 2).join(" ");
    const c = A.store.all("contacts").find((c) => (r.email && c.email === r.email) || (k && words(c.name).slice(0, 2).join(" ") === k)); if (c) return { c, why: r.email && c.email === r.email ? "same email" : "same name" };
    const a = A.store.all("aliases").find((a) => a.alias.toLowerCase() === r.name.toLowerCase()); return a ? { c: A.contact(a.contactId), why: `alias of ${A.contact(a.contactId).name}` } : null;
  }
  function importPage(el) {
    A.setContext({ kind: "contacts", label: "Contact import" });
    if (!A.can("contacts.edit", "act")) return A.render(el, locked(`${A.roleName(A.me().role)} cannot import contacts.`));
    const imp = st.imp || { text: SAMPLE_CSV, src: "csv" }; st.imp = imp;
    const p = imp.mapped ? parseCsv(imp.text) : null;
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "contacts-import");
    A.render(el, h`<div class="page">${ui.header({ title: "Import contacts", back: { label: "Contacts", to: "contacts" }, meta: demo, sub: "Paste a CSV export or a vCard list. The agent maps columns and finds duplicates; nothing reaches the conflict index until a lawyer approves.",
        primary: { label: "✦ Map and check", a: "contacts-import-map", agent: true, key: "n" } })}
      <div class="grid g2"><div class="card"><label class="field">Source<select data-c="contacts-import-src">${[["csv", "CSV file"], ["vcard", "vCard"], ["prior", "Prior system export"]].map(([v, l]) => h`<option value="${v}" ${v === imp.src ? A.raw("selected") : ""}>${l}</option>`)}</select></label>
        <label class="field">Data<textarea rows="9" class="input" data-c="contacts-import-text" style="font-family:var(--mono);font-size:12px">${imp.text}</textarea></label></div>
      <div class="card"><h3>✦ Mapping and duplicates</h3>${p ? h`<p class="small">${ui.actor("agent", "Agent mapped")} ${Object.entries(p.ix).filter(([, i]) => i >= 0).map(([k, i]) => `${p.head[i]} → ${k}`).join(" · ")}</p>
        ${ui.table([{ label: "Name", render: (r) => h`<b>${r.name}</b>` }, { label: "Email", render: (r) => r.email || "—" }, { label: "Kind", render: (r) => (r.kind === "org" ? "Organization" : "Person") },
          { label: "Result", render: (r) => { const d = dupOf(r); return d ? ui.status("conflict", `Duplicate: ${d.c.name} (${d.why})`) : ui.status("new", "New"); } }], p.rows, { empty: "No rows found" })}
        <div class="row" style="margin-top:8px"><span class="muted small grow">${p.rows.filter((r) => !dupOf(r)).length} new, ${p.rows.filter(dupOf).length} skipped as duplicates (kept as aliases for review).</span>
          ${ui.btn({ label: "Send for approval", a: "contacts-import-send", primary: true, disabled: !p.rows.length || pend.length > 0, why: pend.length ? "An import is already waiting" : "Nothing to import" })}</div>`
        : ui.empty("Not checked yet", " Press ✦ Map and check.")}</div></div>
      ${pend.map((g) => ui.gateCard(g))}</div>`);
  }
  A.change("contacts-import-text", (el) => { st.imp = { ...(st.imp || {}), text: el.value, mapped: false }; A.refresh(); });
  A.change("contacts-import-src", (el) => { st.imp = { ...(st.imp || {}), src: el.value }; });
  A.action("contacts-import-map", () => { if (!st.imp) return; A.agent.run({ title: "Map and de-duplicate contact import", steps: ["Reading columns", "Matching against contacts and aliases"], done() { st.imp.mapped = true; return { title: `${parseCsv(st.imp.text).rows.length} rows checked` }; } }); st.imp.mapped = true; A.refresh(); });
  A.action("contacts-import-send", () => {
    const p = st.imp && parseCsv(st.imp.text); if (!p || !A.can("contacts.edit", "act")) return;
    const fresh = p.rows.filter((r) => r.name && !dupOf(r)), dups = p.rows.filter(dupOf);
    A.gate({ kind: "contact.import", cap: "conflicts.clear", title: `Import ${fresh.length} contacts`, preparedBy: A.me().id,
      summary: `${fresh.length} new contacts enter the conflict index. ${dups.length} duplicates are skipped.`, body: String(h`<ul>${fresh.map((r) => h`<li>${r.name}${r.email ? ` · ${r.email}` : ""}</li>`)}</ul>`),
      checks: [{ label: "Duplicates removed", ok: true }, { label: "Every row has a name", ok: fresh.every((r) => r.name) }], effect: "contacts-import", payload: { rows: fresh } });
    st.imp = null; A.toast("Import sent to a lawyer for conflict review");
  });
  A.effect("contacts-import", (p) => { p.rows.forEach((r) => A.store.put("contacts", { kind: r.kind, name: r.name, email: r.email, phone: r.phone, proposed: true })); A.audit("contact.imported", { note: `${p.rows.length} contacts` }); });

  /* ---------- client sharing and secure links (matter tab) ---------- */
  A.seed("docshares", [{ id: "d-will", matterId: "m4", by: "u-linda", at: "2026-08-21 09:00" }]);
  A.seed("seclinks", [{ id: "sl-1", matterId: "m1", docId: "d-claim", to: "Co-counsel: R. Patel, Patel Litigation", expires: "2026-10-14", by: "u-linda", revoked: false }]);
  const shareOf = (d) => A.store.get("docshares", d.id);
  function sharingTab(m) {
    if (!canDocs(m)) return h`<div class="locked"><b>No access</b>${A.roleName(A.me().role)} cannot open documents on this matter.</div>`;
    const ds = docsOf(m), can = canEditDocs(m), links = A.store.where("seclinks", (l) => l.matterId === m.id);
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.matterId === m.id && (g.effect === "matters-share" || g.effect === "matters-link"));
    return h`<p class="muted small">The client sees shared documents in the portal. Sharing sends firm work to the client, so a lawyer approves each share. ${demo}</p>
      ${pend.map((g) => ui.gateCard(g, { compact: true }))}
      ${ui.table([{ label: "Document", render: (d) => h`<b>${d.name}</b>` }, { label: "Status", render: (d) => chip(meta(d).status) },
        { label: "Client portal", render: (d) => (shareOf(d) ? ui.status("sent", `Shared ${A.dateTime(shareOf(d).at)}`) : pend.some((g) => g.payload.docId === d.id) ? chip("pending") : ui.status("draft", "Not shared")) },
        { label: "", render: (d) => h`${shareOf(d) ? ui.btn({ label: "Stop sharing", a: "matters-unshare", id: d.id, small: true, disabled: !can, why: roleWhy("share documents") })
          : ui.btn({ label: "Share with client", a: "matters-share", id: d.id, small: true, disabled: !can || meta(d).status === "draft" || pend.some((g) => g.payload.docId === d.id), why: meta(d).status === "draft" ? "Drafts cannot be shared" : roleWhy("share documents") })}
          ${ui.btn({ label: "Secure link", a: "matters-link", id: d.id, small: true, disabled: !can || meta(d).status === "draft", why: meta(d).status === "draft" ? "Drafts cannot be shared" : roleWhy("share documents") })}` }], ds, { empty: "No documents to share" })}
      <h3 style="margin:16px 0 8px">Secure links</h3>
      ${ui.table([{ label: "Document", render: (l) => (A.store.get("docs", l.docId) || { name: "(deleted)" }).name }, { label: "For", key: "to" }, { label: "Expires", render: (l) => (l.revoked ? ui.status("declined", "Revoked") : ui.due(l.expires, { soon: 3 })) },
        { label: "", render: (l) => (l.revoked ? "" : ui.btn({ label: "Revoke", a: "matters-link-revoke", id: l.id, small: true, danger: true, disabled: !can, why: roleWhy("share documents") })) }], links, { empty: "No secure links", emptyHint: " Links expire on their own; revoke one to end it now." })}`;
  }
  A.action("matters-share", (el) => {
    const d = A.store.get("docs", el.dataset.id); const m = d && A.matter(d.matterId); if (!m || !canEditDocs(m)) return;
    A.gate({ kind: "document.share", cap: "documents.release", title: `Share ${d.name} with ${contactName(m.clientId)}`, matterId: m.id, preparedBy: A.me().id, summary: "The client will see and download this document in the portal.",
      checks: [{ label: "Document is final or filed", ok: meta(d).status !== "draft", blocking: true }, { label: "Not tagged privileged", ok: !meta(d).tags.includes("privileged"), blocking: true }], effect: "matters-share", payload: { docId: d.id, matterId: m.id } });
    A.toast("Share sent for approval");
  });
  A.effect("matters-share", (p) => { A.store.put("docshares", { id: p.docId, matterId: p.matterId, by: A.me().id, at: A.now() }); A.audit("document.shared", { target: p.docId, matterId: p.matterId, note: "client portal" }); });
  A.action("matters-unshare", (el) => { const s = A.store.get("docshares", el.dataset.id); if (!s) return; A.store.remove("docshares", s.id); A.audit("document.unshared", { target: s.id, matterId: s.matterId }); A.toast("No longer shared", "ok"); });
  A.action("matters-link", (el) => {
    const d = A.store.get("docs", el.dataset.id); const m = d && A.matter(d.matterId); if (!m || !canEditDocs(m)) return;
    A.modal(`Secure link · ${d.name}`, h`<div class="form"><label class="field">For (name and firm)<input name="to" placeholder="Co-counsel or expert"></label><label class="field">Expires after (days)<input name="days" type="number" min="1" max="30" value="${docSettings().linkDays}"></label></div><p class="muted small">A lawyer approves the link before it works. ${demo}</p>`,
      [{ label: "Cancel" }, { label: "Request link", primary: true, run: (dl) => { const v = A.formValues(dl); const to = String(v.to || "").trim() || "Recipient to be named";
        A.gate({ kind: "document.link", cap: "documents.release", title: `Secure link: ${d.name}`, matterId: m.id, preparedBy: A.me().id, summary: `For ${to}, expires in ${+v.days || 7} days.`,
          checks: [{ label: "Document is final or filed", ok: meta(d).status !== "draft", blocking: true }], effect: "matters-link", payload: { docId: d.id, matterId: m.id, to, days: +v.days || 7 } }); } }]);
  });
  A.effect("matters-link", (p) => { const l = A.store.put("seclinks", { matterId: p.matterId, docId: p.docId, to: p.to, expires: A.addDays(A.today, p.days), by: A.me().id, revoked: false }); A.audit("document.link", { target: l.id, matterId: p.matterId, note: p.to }); });
  A.action("matters-link-revoke", (el) => { const l = A.store.get("seclinks", el.dataset.id); if (!l) return; A.store.put("seclinks", { id: l.id, revoked: true }); A.audit("document.link.revoked", { target: l.id, matterId: l.matterId }); });

  /* ---------- deleted documents ---------- */
  A.seed("doctrash", [{ id: "d-old-draft", doc: { id: "d-old-draft", matterId: "m1", name: "Draft demand letter (superseded)", kind: "letter", added: "2026-09-02", by: "u-ayo", pages: [["Re: Tesfaye v. Northgate Storage", "Our client demands payment within 14 days."]] }, by: "u-ayo", at: "2026-09-20 10:12" }]);
  function trashDoc(d) {
    const m = A.store.get("docs", d.id); if (!m) return;
    A.store.put("doctrash", { id: d.id, doc: { ...m }, meta: A.store.get("docmeta", d.id) || null, by: A.me().id, at: A.now() });
    A.store.remove("docs", d.id); A.selection("mdocs").clear(); A.audit("document.deleted", { target: d.id, matterId: d.matterId, note: d.name });
  }
  const purgeWhy = (t) => { const m = A.store.get("matters", t.doc.matterId); if (t.meta && t.meta.status === "filed") return "Filed with the court: retention requires keeping it";
    if (m && m.hold) return "Matter is on legal hold"; return ""; };
  function trashPage(el) {
    A.setContext({ kind: "documents", label: "Deleted documents" });
    if (!A.can("documents.view")) return A.render(el, locked(`${A.roleName(A.me().role)} cannot open matter documents.`));
    const days = docSettings().trashDays;
    const rows = A.store.all("doctrash").filter((t) => { const m = A.matter(t.doc.matterId); return m && canDocs(m); });
    A.render(el, h`<div class="page">${docsHeader("Deleted documents", `Restore within ${days} days. After that, or on purge, the document is gone unless retention or a hold keeps it.`)}${docTabs("documents/trash")}
      ${ui.bulkBar("mtrash", [{ label: "Restore", a: "docs-restore", primary: true }])}
      ${ui.table([{ label: "Document", render: (t) => h`<b>${t.doc.name}</b>` }, { label: "Matter", render: (t) => A.matter(t.doc.matterId).number }, { label: "Deleted", render: (t) => h`${A.dateTime(t.at)} · ${A.user(t.by).name}` },
        { label: "Purges", render: (t) => ui.due(A.addDays(t.at.slice(0, 10), days), { soon: 5 }) },
        { label: "", render: (t) => h`${ui.btn({ label: "Restore", a: "docs-restore", id: t.id, small: true, disabled: !canEditDocs(A.matter(t.doc.matterId)), why: roleWhy("restore documents") })}${ui.btn({ label: "Delete forever", a: "docs-purge", id: t.id, small: true, danger: true, disabled: !!purgeWhy(t) || !canEditDocs(A.matter(t.doc.matterId)), why: purgeWhy(t) || roleWhy("delete documents") })}` }],
        rows, { select: "mtrash", empty: "Nothing deleted", emptyHint: " Deleted documents wait here before they are purged." })}</div>`);
  }
  A.action("docs-restore", (el) => { const ids = el.dataset.id ? [el.dataset.id] : (el.dataset.ids || "").split(",").filter(Boolean); let n = 0;
    ids.forEach((id) => { const t = A.store.get("doctrash", id); const m = t && A.matter(t.doc.matterId); if (!m || !canEditDocs(m)) return;
      A.store.put("docs", { ...t.doc }); if (t.meta) A.store.put("docmeta", { ...t.meta }); A.store.remove("doctrash", id); n++; A.audit("document.restored", { target: id, matterId: m.id, note: t.doc.name }); });
    A.selection("mtrash").clear(); if (n) A.toast(`${n} restored`, "ok"); });
  A.action("docs-purge", (el) => { const t = A.store.get("doctrash", el.dataset.id); const m = t && A.matter(t.doc.matterId); if (!m || purgeWhy(t) || !canEditDocs(m)) return;
    A.gate({ kind: "document.purge", cap: "documents.release", title: `Delete forever: ${t.doc.name}`, matterId: m.id, preparedBy: A.me().id, summary: "Permanent deletion cannot be undone.",
      checks: [{ label: "No legal hold", ok: !m.hold, blocking: true }, { label: "Not a filed court document", ok: !(t.meta && t.meta.status === "filed"), blocking: true }], effect: "docs-purge", payload: { id: t.id } });
    A.toast("Permanent deletion sent for approval"); });
  A.effect("docs-purge", (p) => { const t = A.store.get("doctrash", p.id); if (!t) return; A.store.remove("doctrash", p.id); A.audit("document.purged", { target: p.id, matterId: t.doc.matterId, note: t.doc.name }); });

  /* ---------- analyze several documents (matter tab) ---------- */
  const ANALYSES = { chronology: "Chronology of events", parties: "Parties and roles", amounts: "Amounts and figures", obligations: "Obligations and deadlines", inconsistencies: "Inconsistencies between documents" };
  A.seed("analyses", []);
  function runAnalysis(m, kind, ids) {
    const ds = ids.map((id) => A.store.get("docs", id)).filter(Boolean); const all = ds.flatMap((d) => lines(d).map((l) => ({ l, d })));
    const pick = { chronology: (x) => datesIn(x.l.t).length, parties: (x) => /\b(Inc\.|Ltd\.|LLP|Corp\.|plaintiff|defendant|landlord|tenant|client|employer)\b/i.test(x.l.t), amounts: (x) => /\$\s?[\d,]+/.test(x.l.t),
      obligations: (x) => /\b(shall|must|will|responsible|required|due|within)\b/i.test(x.l.t), inconsistencies: () => false }[kind];
    let rows = all.filter(pick).map((x) => ({ text: x.l.t.replace(/^\d+\.\s*/, ""), cite: cite(x.l, x.d) }));
    if (kind === "inconsistencies") {
      const money = all.filter((x) => /\$\s?[\d,]+/.test(x.l.t)).map((x) => ({ x, w: new Set(words(x.l.t)), v: (x.l.t.match(/\$\s?[\d,]+/) || [""])[0] }));
      for (let i = 0; i < money.length; i++) for (let j = i + 1; j < money.length; j++) { const a = money[i], b = money[j]; const ov = [...a.w].filter((w) => b.w.has(w)).length;
        if (a.x.d.id !== b.x.d.id && ov >= 2 && a.v !== b.v) rows.push({ text: `${a.x.d.name} says ${a.v}; ${b.x.d.name} says ${b.v}`, cite: cite(a.x.l, a.x.d), cite2: cite(b.x.l, b.x.d) }); }
    }
    if (kind === "chronology") rows.sort((a, b) => (datesIn(a.text)[0] || "").localeCompare(datesIn(b.text)[0] || ""));
    return rows.slice(0, 30);
  }
  function analyzeTab(m) {
    if (!canDocs(m)) return h`<div class="locked"><b>No access</b>${A.roleName(A.me().role)} cannot open documents on this matter.</div>`;
    const ds = docsOf(m), sel = `an-${m.id}`, S = A.selection(sel), kind = st.an[m.id] || "chronology";
    const res = A.store.where("analyses", (a) => a.matterId === m.id).sort((a, b) => b.at.localeCompare(a.at)), cur = res[0];
    return h`<div class="grid g2"><div class="card"><h3>Documents</h3><p class="muted small">Pick the documents to read together.</p>${ui.list(ds.map((d) => ({ id: d.id, title: d.name, sub: `${d.kind} · ${d.pages.length} page${d.pages.length > 1 ? "s" : ""}`, on: S.has(d.id) })), "matters-an-pick", { select: sel, empty: "No documents" })}
        <label class="field" style="margin-top:8px">Question<select data-c="matters-an-kind" data-id="${m.id}">${Object.entries(ANALYSES).map(([k, v]) => h`<option value="${k}" ${k === kind ? A.raw("selected") : ""}>${v}</option>`)}</select></label>
        <div class="row"><span class="muted small grow">${S.size} selected</span>${ui.btn({ label: "✦ Analyze", a: "matters-an-run", id: m.id, agent: true, primary: true, disabled: S.size < 2, why: "Pick at least two documents" })}</div></div>
      <div class="card"><h3>Result</h3>${cur ? h`<div class="row wrap">${cur.kept ? ui.status("confirmed", `Kept by ${A.user(cur.keptBy).name}`) : ui.status("agent-prepared")}<span class="muted small grow">${ANALYSES[cur.kind]} · ${cur.docs.length} documents · ${A.dateTime(cur.at)}</span>
          ${cur.kept ? "" : ui.btn({ label: "Keep on file", a: "matters-an-keep", id: cur.id, small: true })}</div>
        ${cur.rows.length ? cur.rows.map((r) => h`<p class="small" style="margin:6px 0">${r.text} ${ui.cite(r.cite)}${r.cite2 ? ui.cite(r.cite2) : ""}</p>`) : h`<p class="muted">Nothing found for this question in the chosen documents.</p>`}
        <p class="muted small">Every row cites its line. The agent reports; it does not decide.</p>` : ui.empty("No analysis yet", " Pick documents and a question.")}</div></div>`;
  }
  A.action("matters-an-pick", (el) => { const mid = A.route().split("/")[1]; A.selection(`an-${mid}`).toggle(el.dataset.id); A.refresh(); });
  A.change("matters-an-kind", (el) => { st.an[el.dataset.id] = el.value; });
  A.action("matters-an-run", (el) => { const m = A.matter(el.dataset.id); if (!m || !canDocs(m)) return; const ids = A.selection(`an-${m.id}`).ids(), kind = st.an[m.id] || "chronology"; if (ids.length < 2) return A.toast("Pick at least two documents");
    A.agent.run({ title: `${ANALYSES[kind]}: ${m.number}`, matterId: m.id, steps: ["Reading the chosen documents", "Answering with line citations"], done() {
      const rows = runAnalysis(m, kind, ids); A.store.put("analyses", { matterId: m.id, kind, docs: ids, rows, at: A.now(), by: A.me().id, kept: false }); return { title: `${rows.length} findings`, cites: rows.slice(0, 4).map((r) => r.cite) }; } });
    A.audit("matter.analyzed", { target: m.id, matterId: m.id, note: `${ANALYSES[kind]} over ${ids.length} documents` }); });
  A.action("matters-an-keep", (el) => { const a = A.store.get("analyses", el.dataset.id); if (!a) return; A.store.put("analyses", { id: a.id, kept: true, keptBy: A.me().id });
    A.store.put("notes", { matterId: a.matterId, by: A.me().id, at: A.now(), text: `Kept analysis: ${ANALYSES[a.kind]} (${a.rows.length} findings) over ${a.docs.length} documents.` }); A.audit("analysis.kept", { target: a.id, matterId: a.matterId }); });

  /* ---------- documents area: shared header and tabs ---------- */
  const DOC_TABS = [["documents", "Library"], ["documents/templates", "Templates"], ["documents/assemble", "Assemble"], ["documents/precedents", "Precedents"], ["documents/engagement", "Engagement letter"],
    ["documents/signatures", "Signatures"], ["documents/bundle", "Bundles"], ["documents/trash", "Deleted"], ["documents/settings", "Settings"]];
  const docTabs = (on) => ui.tabs(DOC_TABS.filter(([p]) => A.can("documents.view") || ["documents", "documents/templates", "documents/settings"].includes(p)).map(([id, label]) => ({ id, label })), on, "matters-go");
  const docsHeader = (title, sub, primary, extra) => ui.header({ title, sub, primary, meta: extra || "" });
  const editableMatters = () => A.matters((m) => m.status === "open").filter(canEditDocs);

  /* ---------- firm library and private drafts ---------- */
  A.seed("libdocs", [
    { id: "lib-1", name: "Trust account handling policy", kind: "policy", owner: null, folder: "Policies", updated: "2026-06-30", lines: ["Only designated signatories authorize trust withdrawals.", "Reconcile monthly within 25 days of month end (By-Law 9).", "Two people touch every withdrawal: one prepares, one authorizes."] },
    { id: "lib-2", name: "New matter opening checklist", kind: "checklist", owner: null, folder: "Checklists", updated: "2026-09-01", lines: ["Conflict check cleared and recorded.", "Identity verified under By-Law 7.1.", "Engagement letter signed.", "Retainer received to trust."] },
    { id: "lib-3", name: "Form 14A statement of claim (blank)", kind: "form", owner: null, folder: "Court forms", updated: "2026-01-15", lines: ["ONTARIO SUPERIOR COURT OF JUSTICE", "BETWEEN: [plaintiff] and [defendant]", "STATEMENT OF CLAIM"] },
    { id: "lib-4", name: "Notes: limitation periods talk", kind: "memo", owner: "u-ayo", folder: "Private drafts", updated: "2026-10-06", lines: ["Basic limitation period: two years from discovery (Limitations Act, 2002, s. 4).", "Ultimate limitation: 15 years (s. 15)."] },
    { id: "lib-5", name: "Draft client intake script", kind: "memo", owner: "u-linda", folder: "Private drafts", updated: "2026-10-02", lines: ["Ask for every party's full name and former names.", "Ask about deadlines the client already knows of."] },
  ]);
  function libraryPage(el) {
    A.setContext({ kind: "documents", label: "Firm library" });
    const me = A.me().id, tab = st.lib;
    const rows = A.store.all("libdocs").filter((d) => (tab === "mine" ? d.owner === me : !d.owner)).filter((d) => !st.libq || `${d.name} ${d.folder} ${d.lines.join(" ")}`.toLowerCase().includes(st.libq.toLowerCase()));
    const sel = rows.find((d) => d.id === st.libSel) || null;
    const detail = sel && h`<div class="card"><div class="row wrap"><h3 class="grow" style="margin:0">${sel.name}</h3>${sel.owner ? chip("private") : ui.status("active", "Firm")}
        ${sel.owner === me ? ui.btn({ label: "Publish to firm", a: "docs-lib-publish", id: sel.id, small: true, disabled: !canTpl(), why: "Your role cannot publish firm material" }) : ""}
        ${sel.owner === me || (!sel.owner && canTpl()) ? ui.btn({ label: "Edit", a: "docs-lib-edit", id: sel.id, key: "e", small: true }) : ""}</div>
      <div class="muted small">${sel.folder} · updated ${A.date(sel.updated)}</div>
      <div class="docview" style="margin-top:8px">${sel.lines.map((t, i) => h`<div class="ln"><i>${i + 1}</i><span>${t}</span></div>`)}</div></div>`;
    A.render(el, h`<div class="page">${docsHeader("Documents", "Firm material that belongs to no matter, and your private drafts. Private drafts are visible only to you.", { label: "New private draft", a: "docs-lib-new", key: "n" })}${docTabs("documents")}
      ${ui.filters({ search: { c: "docs-libq", value: st.libq || "", placeholder: "Search the library" }, chips: [{ id: "firm", label: "Firm library", count: A.store.where("libdocs", (d) => !d.owner).length }, { id: "mine", label: "My private drafts", count: A.store.where("libdocs", (d) => d.owner === me).length }], active: tab, action: "docs-lib-tab" })}
      ${ui.split(ui.list(rows.map((d) => ({ id: d.id, title: d.name, sub: `${d.folder} · ${A.date(d.updated)}`, on: sel && d.id === sel.id })), "docs-lib-sel", { empty: tab === "mine" ? "No private drafts" : "Nothing in the library" }), detail, { emptyTitle: "Pick a document" })}</div>`);
  }
  A.action("docs-lib-tab", (el) => { st.lib = el.dataset.id; A.refresh(); });
  A.action("docs-lib-sel", (el) => { st.libSel = el.dataset.id; A.refresh(); });
  A.change("docs-libq", (el) => { st.libq = el.value; A.refresh(); });
  const libModal = (d) => A.modal(d ? `Edit ${d.name}` : "New private draft", h`<div class="form"><label class="field">Title<input name="name" value="${d ? d.name : ""}"></label><label class="field">Text<textarea name="text" rows="8">${d ? d.lines.join("\n") : ""}</textarea></label></div>`,
    [{ label: "Cancel" }, { label: "Save", primary: true, run: (dl) => { const v = A.formValues(dl); if (!String(v.name || "").trim()) { A.toast("Give it a title"); return false; }
      const r = A.store.put("libdocs", { ...(d ? { id: d.id } : { owner: A.me().id, folder: "Private drafts", kind: "memo" }), name: v.name.trim(), lines: String(v.text || "").split("\n").filter(Boolean), updated: A.today });
      st.libSel = r.id; if (!d) st.lib = "mine"; A.audit("library.saved", { target: r.id, note: r.name }); } }]);
  A.action("docs-lib-new", () => libModal(null));
  A.action("docs-lib-edit", (el) => { const d = A.store.get("libdocs", el.dataset.id); if (d && (d.owner === A.me().id || (!d.owner && canTpl()))) libModal(d); });
  A.action("docs-lib-publish", (el) => { const d = A.store.get("libdocs", el.dataset.id); if (!d || d.owner !== A.me().id || !canTpl()) return; A.store.put("libdocs", { id: d.id, owner: null, folder: "Firm guides", updated: A.today }); st.lib = "firm"; A.audit("library.published", { target: d.id, note: d.name }); A.toast("Published to the firm library", "ok"); });

  /* ---------- template library and document assembly ---------- */
  A.seed("doctemplates", [
    { id: "tpl-1", name: "Letter: request for consent to extension", area: "Civil litigation", version: 3, status: "approved", by: "u-linda", fields: ["client.name", "matter.number", "matter.name", "opposing_counsel.name", "extension_days"],
      body: ["Re: {matter.name}, our file {matter.number}", "We act for {client.name}.", "Our client consents to a {extension_days}-day extension to deliver the statement of defence.", "[if extension_days > 15] This is the only extension our client will consent to.", "Yours truly, Linda K Law"] },
    { id: "tpl-2", name: "Requisition letter", area: "Real estate", version: 2, status: "approved", by: "u-yoni", fields: ["matter.name", "matter.number", "property.address", "closing_date", "seller_lawyer.name"],
      body: ["Re: {matter.name} ({property.address}), closing {closing_date}", "We requisition a discharge of every registered mortgage on or before closing.", "[optional] Please provide a statutory declaration of possession.", "Yours truly, Linda K Law"] },
    { id: "tpl-3", name: "Reporting letter to estate trustee", area: "Estates", version: 1, status: "draft", by: "u-meraf", fields: ["client.name", "matter.name", "certificate_date"],
      body: ["Dear {client.name},", "The court issued the certificate of appointment on {certificate_date}.", "Next, we will gather the assets and file the estate information return within 180 days."] },
  ]);
  function fieldVal(m, f) {
    const oc = (m.opposing || []).map(A.contact).find((c) => /counsel/i.test(c.role || ""));
    return { "client.name": contactName(m.clientId), "matter.number": m.number, "matter.name": m.name, "opposing_counsel.name": oc ? oc.name : "" }[f] || "";
  }
  function templatesDocPage(el) {
    A.setContext({ kind: "documents", label: "Templates" });
    const rows = A.store.all("doctemplates"), sel = rows.find((t) => t.id === st.tplSel) || rows[0];
    const pend = sel && A.store.where("approvals", (g) => g.status === "pending" && g.effect === "docs-tpl-approve" && g.payload.id === sel.id);
    const detail = sel && h`<div class="card"><div class="row wrap"><h3 class="grow" style="margin:0">${sel.name}</h3>${chip(sel.status)}<span class="muted small">v${sel.version}</span></div>
      <div class="muted small" style="margin:4px 0 8px">${sel.area} · last edited by ${A.user(sel.by).name}</div>
      <div class="small"><b>Merge fields</b> ${sel.fields.map((f) => h`<code style="margin-right:6px">{${f}}</code>`)}</div>
      <div class="docview" style="margin-top:8px">${sel.body.map((t, i) => h`<div class="ln"><i>${i + 1}</i><span>${/^\[(if|optional)/.test(t) ? h`<span style="color:var(--accent)">${t}</span>` : t}</span></div>`)}</div>
      <p class="muted small">[if …] lines are conditional; [optional] lines are offered at assembly.</p>
      ${pend && pend.length ? pend.map((g) => ui.gateCard(g, { compact: true })) : ""}
      <div class="row wrap">${ui.btn({ label: "Edit", a: "docs-tpl-edit", id: sel.id, key: "e", small: true, disabled: !canTpl(), why: "Your role cannot edit templates" })}
        ${ui.btn({ label: "Submit for approval", a: "docs-tpl-submit", id: sel.id, small: true, disabled: !canTpl() || sel.status !== "draft" || (pend && pend.length > 0), why: sel.status !== "draft" ? "Only a draft version is submitted" : "Already waiting" })}
        <span class="grow"></span>${ui.btn({ label: "Assemble", to: `documents/assemble/${sel.id}`, primary: true, small: true, disabled: sel.status !== "approved" || !A.can("documents.edit", "act"), why: sel.status !== "approved" ? "Approve this version before use" : "Your role cannot draft documents" })}</div></div>`;
    A.render(el, h`<div class="page">${docsHeader("Documents", "Firm templates with merge fields and conditional clauses. Each version is approved before anyone can use it.", { label: "New template", a: "docs-tpl-edit", key: "n", disabled: !canTpl(), why: "Your role cannot edit templates" })}${docTabs("documents/templates")}
      ${ui.split(ui.list(rows.map((t) => ({ id: t.id, title: t.name, sub: `${t.area} · v${t.version}`, status: t.status, on: sel && t.id === sel.id })), "docs-tpl-sel"), detail)}</div>`);
  }
  A.action("docs-tpl-sel", (el) => { st.tplSel = el.dataset.id; A.refresh(); });
  A.action("docs-tpl-edit", (el) => {
    if (!canTpl()) return; const t = A.store.get("doctemplates", el.dataset.id || "") || null;
    A.modal(t ? `Edit ${t.name}` : "New template", h`<div class="form"><label class="field">Name<input name="name" value="${t ? t.name : ""}"></label>
      <label class="field">Practice area<select name="area">${A.store.all("practiceareas").map((a) => h`<option ${t && t.area === a.name ? A.raw("selected") : ""}>${a.name}</option>`)}</select></label>
      <label class="field">Text: {field} merges, [if …] and [optional] start conditional lines<textarea name="body" rows="8">${t ? t.body.join("\n") : "Re: {matter.name}, our file {matter.number}\n"}</textarea></label></div>
      <p class="muted small">Saving creates a new draft version. It needs approval before use.</p>`,
      [{ label: "Cancel" }, { label: "Save draft", primary: true, run: (d) => { const v = A.formValues(d); if (!String(v.name || "").trim()) { A.toast("Name the template"); return false; }
        const body = String(v.body || "").split("\n").filter(Boolean); const fields = [...new Set(body.join(" ").match(/\{([a-z_.]+)\}/g) || [])].map((x) => x.slice(1, -1));
        const r = A.store.put("doctemplates", { ...(t ? { id: t.id, version: t.version + 1 } : { version: 1 }), name: v.name.trim(), area: v.area, body, fields, status: "draft", by: A.me().id });
        st.tplSel = r.id; A.audit("template.saved", { target: r.id, note: `${r.name} v${r.version}` }); } }]);
  });
  A.action("docs-tpl-submit", (el) => { const t = A.store.get("doctemplates", el.dataset.id); if (!t || t.status !== "draft" || !canTpl()) return;
    A.gate({ kind: "template.approve", cap: "documents.release", title: `Approve template: ${t.name} v${t.version}`, preparedBy: t.by === A.me().id ? A.me().id : t.by, summary: `${t.body.length} lines, ${t.fields.length} merge fields.`, body: String(h`${t.body.map((l) => h`<p>${l}</p>`)}`),
      checks: [{ label: "Every merge field is known", ok: t.fields.length > 0 }, { label: "Approver did not write this version", ok: true }], effect: "docs-tpl-approve", payload: { id: t.id, version: t.version } });
    A.toast("Template sent for approval"); });
  A.effect("docs-tpl-approve", (p) => { const t = A.store.get("doctemplates", p.id); if (!t || t.version !== p.version) return; A.store.put("doctemplates", { id: t.id, status: "approved" }); A.audit("template.approved", { target: t.id, note: `v${t.version}` }); });
  function assemblePage(el, p) {
    A.setContext({ kind: "documents", label: "Assemble" });
    if (!A.can("documents.view")) return A.render(el, locked(`${A.roleName(A.me().role)} cannot draft matter documents.`));
    const tpls = A.store.where("doctemplates", (t) => t.status === "approved"), ms = editableMatters();
    const a = st.asm; if (p && p.id) a.tpl = p.id; if (!tpls.some((t) => t.id === a.tpl)) a.tpl = tpls[0] && tpls[0].id; if (!ms.some((m) => m.id === a.matter)) a.matter = ms[0] && ms[0].id;
    const t = A.store.get("doctemplates", a.tpl), m = a.matter && A.matter(a.matter);
    const missing = t && m ? t.fields.filter((f) => !fieldVal(m, f)) : [];
    A.render(el, h`<div class="page">${docsHeader("Documents", "Generate a document from an approved template, prefilled from the matter. You are asked only for what is missing.")}${docTabs("documents/assemble")}
      ${!tpls.length || !ms.length ? ui.empty(!tpls.length ? "No approved templates" : "No matters you can draft on", "") : h`<div class="grid g2"><div class="card">
        <label class="field">Template<select data-c="docs-asm" data-f="tpl">${tpls.map((x) => h`<option value="${x.id}" ${x.id === a.tpl ? A.raw("selected") : ""}>${x.name} (v${x.version})</option>`)}</select></label>
        <label class="field">Matter<select data-c="docs-asm" data-f="matter">${ms.map((x) => h`<option value="${x.id}" ${x.id === a.matter ? A.raw("selected") : ""}>${x.number} ${x.name}</option>`)}</select></label>
        <h3>Prefilled</h3><dl class="kv">${t.fields.filter((f) => fieldVal(m, f)).map((f) => h`<dt>${f}</dt><dd>${fieldVal(m, f)}</dd>`)}</dl>
        ${missing.length ? h`<h3>Missing answers</h3><div class="form" id="asm-missing">${missing.map((f) => h`<label class="field">${f}<input name="${f}" ${/date/.test(f) ? A.raw('type="date"') : /days/.test(f) ? A.raw('type="number"') : ""}></label>`)}</div>` : ""}
        ${t.body.some((l) => /^\[optional\]/.test(l)) ? h`<label class="row small"><input type="checkbox" id="asm-opt" checked> Include optional clauses</label>` : ""}
        <div class="row"><span class="grow"></span>${ui.btn({ label: "✦ Generate", a: "docs-asm-run", agent: true, primary: true, disabled: !m || !canEditDocs(m), why: "Your role cannot draft on this matter" })}</div></div>
      <div class="card"><h3>Preview</h3><div class="docview">${t.body.map((l, i) => h`<div class="ln"><i>${i + 1}</i><span>${l.replace(/\{([a-z_.]+)\}/g, (_, f) => fieldVal(m, f) || `[${f}]`)}</span></div>`)}</div></div></div>`}</div>`);
  }
  A.change("docs-asm", (el) => { st.asm[el.dataset.f] = el.value; A.refresh(); });
  A.action("docs-asm-run", () => {
    const t = A.store.get("doctemplates", st.asm.tpl), m = st.asm.matter && A.matter(st.asm.matter); if (!t || !m || !canEditDocs(m)) return;
    const box = document.getElementById("asm-missing"), ans = box ? A.formValues(box) : {}, opt = document.getElementById("asm-opt"), withOpt = !opt || opt.checked;
    const val = (f) => fieldVal(m, f) || ans[f] || "";
    const gaps = t.fields.filter((f) => !val(f)); if (gaps.length) return A.toast(`Answer: ${gaps.join(", ")}`, "bad");
    const out = t.body.filter((l) => { if (/^\[optional\]/.test(l)) return withOpt; const c = l.match(/^\[if (\w+) > (\d+)\]/); return !c || +val(c[1]) > +c[2]; })
      .map((l) => l.replace(/^\[(if [^\]]+|optional)\]\s*/, "").replace(/\{([a-z_.]+)\}/g, (_, f) => (/date/.test(f) ? A.date(val(f)) : val(f))));
    A.agent.run({ title: `Assemble ${t.name}: ${m.number}`, matterId: m.id, steps: ["Merging matter data", "Applying conditional clauses", "Checking every field is filled"], done() {
      return A.gate({ kind: "document.release", cap: "documents.release", title: `${t.name}: ${m.name}`, matterId: m.id, preparedBy: "agent", summary: `Assembled from template v${t.version}. Approve to file it on the matter.`,
        body: String(h`${out.map((l) => h`<p>${l}</p>`)}`), checks: [{ label: "Every merge field filled", ok: true }, { label: "Template version is approved", ok: t.status === "approved", blocking: true }],
        effect: "matters-doc-release", payload: { matterId: m.id, name: `${t.name} (${A.date(A.today)})`, text: out } }); } });
    A.toast("Assembling. It will appear in Approvals.");
  });

  /* ---------- precedent library ---------- */
  A.seed("precedents", [
    { id: "pr-1", name: "Consent to extension, limited to 15 days", kind: "clause", area: "Civil litigation", tags: ["extension", "pleadings"], text: "Our client consents to a 15-day extension to deliver the statement of defence. No further extension will be consented to.", anon: true, by: "u-linda", uses: 4 },
    { id: "pr-2", name: "Tenant-side HVAC carve-out", kind: "clause", area: "Commercial", tags: ["lease", "repairs"], text: "Tenant is responsible for HVAC repairs, except repairs caused by the landlord or by a structural defect.", anon: true, by: "u-yoni", uses: 2 },
    { id: "pr-3", name: "Wrongful dismissal demand letter", kind: "document", area: "Employment", tags: ["demand", "reasonable notice"], text: "Our client was employed for [years] years. At common law she is entitled to reasonable notice of [months] months.", anon: true, by: "u-ayo", uses: 6 },
  ]);
  function precedentsPage(el) {
    A.setContext({ kind: "documents", label: "Precedents" });
    if (!A.can("documents.view")) return A.render(el, locked(`${A.roleName(A.me().role)} cannot open precedents.`));
    const q = (st.prq || "").toLowerCase(), rows = A.store.all("precedents").filter((p) => !q || `${p.name} ${p.area} ${p.tags.join(" ")} ${p.text}`.toLowerCase().includes(q));
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "docs-precedent");
    A.render(el, h`<div class="page">${docsHeader("Documents", "Curated, anonymized past documents and clauses. The agent drafts from these; staff copy from them.", { label: "✦ Nominate a precedent", a: "docs-pr-nominate", agent: true, key: "n", disabled: !A.can("documents.edit", "act"), why: "Your role cannot nominate precedents" })}${docTabs("documents/precedents")}
      ${ui.filters({ search: { c: "docs-prq", value: st.prq || "", placeholder: "Search precedents and clauses" } })}
      ${pend.map((g) => ui.gateCard(g, { compact: true }))}
      ${ui.table([{ label: "Precedent", render: (p) => h`<b>${p.name}</b><div class="muted small">${p.text}</div>` }, { label: "Kind", render: (p) => cap1(p.kind) }, { label: "Area", key: "area" }, { label: "Tags", render: (p) => p.tags.map((t) => ui.badge(t)) },
        { label: "Uses", num: true, render: (p) => p.uses }, { label: "", render: (p) => ui.btn({ label: "Copy", a: "docs-pr-copy", id: p.id, small: true }) }], rows, { empty: "No precedents match" })}</div>`);
  }
  A.change("docs-prq", (el) => { st.prq = el.value; A.refresh(); });
  A.action("docs-pr-copy", (el) => { const p = A.store.get("precedents", el.dataset.id); if (!p) return; try { if (navigator.clipboard) navigator.clipboard.writeText(p.text); } catch (e) { /* blocked */ } A.store.put("precedents", { id: p.id, uses: p.uses + 1 }); A.toast("Copied"); });
  A.action("docs-pr-nominate", () => {
    const ds = A.store.all("docs").filter((d) => { const m = A.matter(d.matterId); return m && canDocs(m) && meta(d).status !== "draft"; });
    if (!ds.length) return A.toast("No final documents you can open");
    A.modal("Nominate a precedent", h`<label class="field">Final document<select name="doc">${ds.map((d) => h`<option value="${d.id}">${A.matter(d.matterId).number} · ${d.name}</option>`)}</select></label><p class="muted small">The agent removes names, numbers and addresses. A lawyer approves before it joins the library.</p>`,
      [{ label: "Cancel" }, { label: "✦ Anonymize", primary: true, run: (dl) => { const d = A.store.get("docs", A.formValues(dl).doc); const m = d && A.matter(d.matterId); if (!m) return;
        const names = [m.name, contactName(m.clientId), m.number].concat((m.opposing || []).map(contactName)).flatMap((n) => [n].concat(n.split(/\s+v\.\s+|\s+/).filter((w) => w.length > 3 && /^[A-Z]/.test(w))));
        const text = d.pages.flat().slice(0, 6).map((l) => names.reduce((t, n) => t.split(n).join("[party]"), l).replace(/\$\s?[\d,]+(\.\d\d)?/g, "$[amount]").replace(/\d{1,5} [A-Z][a-z]+ (Rd|St|Ave)\b/g, "[address]")).join(" ");
        A.agent.run({ title: `Anonymize ${d.name}`, steps: ["Removing names and numbers", "Tagging"], done() {
          return A.gate({ kind: "precedent.add", cap: "documents.release", title: `Add precedent: ${d.name}`, preparedBy: "agent", summary: "Anonymized copy for the firm precedent library.", body: String(h`<p>${text}</p>`),
            cites: [{ doc: d.id, page: 1, line: 1, quote: d.pages[0][0], label: d.name }], checks: [{ label: "No client or party names remain", ok: !names.some((n) => n.length > 3 && text.includes(n)), blocking: true }],
            effect: "docs-precedent", payload: { name: d.name.replace(/\s*\(.*\)$/, ""), area: m.area, text, tags: meta(d).tags } }); } }); } }]);
  });
  A.effect("docs-precedent", (p) => { const r = A.store.put("precedents", { name: p.name, kind: "document", area: p.area, tags: p.tags, text: p.text, anon: true, by: A.me().id, uses: 0 }); A.audit("precedent.added", { target: r.id, note: p.name }); });

  /* ---------- engagement letter composer ---------- */
  function engagementPage(el) {
    A.setContext({ kind: "documents", label: "Engagement letter" });
    const ms = editableMatters(); if (!ms.length) return A.render(el, h`<div class="page">${docsHeader("Documents")}${docTabs("documents/engagement")}${ui.empty("No matters you can draft on", "")}</div>`);
    const e = st.eng || (st.eng = { matter: ms[0].id, scope: "", fee: "", retainer: "500000", limited: false });
    const m = A.matter(e.matter) || ms[0];
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "docs-engagement" && g.matterId === m.id);
    A.render(el, h`<div class="page">${docsHeader("Documents", "The agent drafts the engagement or retainer letter from the matter and its conflict result. A lawyer approves it; then it goes for signature.")}${docTabs("documents/engagement")}
      <div class="grid g2"><div class="card"><div class="form">
        <label class="field">Matter<select data-c="docs-eng" data-f="matter">${ms.map((x) => h`<option value="${x.id}" ${x.id === m.id ? A.raw("selected") : ""}>${x.number} ${x.name}</option>`)}</select></label>
        <label class="field">Scope of the retainer<textarea data-c="docs-eng" data-f="scope" rows="3" placeholder="Leave blank to let the agent write it from the matter">${e.scope}</textarea></label>
        <label class="field">Fee basis<select data-c="docs-eng" data-f="fee">${[["", `Matter default (${m.fee || "hourly"})`], ["hourly", "Hourly"], ["flat", "Flat fee"], ["contingency", "Contingency (Ontario Reg. 563/20 rules)"]].map(([v, l]) => h`<option value="${v}" ${v === e.fee ? A.raw("selected") : ""}>${l}</option>`)}</select></label>
        <label class="field">Retainer to trust (cents)<input type="number" data-c="docs-eng" data-f="retainer" value="${e.retainer}"> <span class="muted small">${A.cents(e.retainer)}</span></label>
        <label class="row small"><input type="checkbox" data-c="docs-eng" data-f="limited" ${e.limited ? A.raw("checked") : ""}> Limited scope retainer (r. 3.2-1A)</label></div>
        <div class="row" style="margin-top:8px"><span class="grow"></span>${ui.btn({ label: "✦ Draft letter", a: "docs-eng-draft", agent: true, primary: true, key: "n", disabled: !canEditDocs(m) || pend.length > 0, why: pend.length ? "A draft is waiting for approval" : roleWhy("draft documents") })}</div></div>
      <div class="card"><h3>Waiting for approval</h3>${pend.length ? pend.map((g) => ui.gateCard(g)) : h`<p class="muted">Nothing waiting for this matter.</p>`}</div></div></div>`);
  }
  A.change("docs-eng", (el) => { st.eng[el.dataset.f] = el.type === "checkbox" ? el.checked : el.value; A.refresh(); });
  function engagementText(m, e) {
    const fee = e.fee || m.fee || "hourly", c = A.contact(m.clientId);
    return [`Dear ${c.name},`, `Re: ${m.name}, our file ${m.number}`, `Thank you for choosing Linda K Law. ${A.user(m.responsible).name} is the lawyer responsible for your matter.`,
      `Scope: ${e.scope || `We will act for you in ${m.name.toLowerCase().startsWith("purchase") || m.area === "Real estate" ? "this transaction" : "this matter"} (${m.area.toLowerCase()}).${e.limited ? " This is a limited scope retainer; anything outside it needs a new agreement." : ""}`}`,
      `Fees: ${fee === "hourly" ? "We bill hourly at the rates in Schedule A, monthly." : fee === "flat" ? "A flat fee as quoted, plus disbursements and HST." : "A contingency fee under a separate contingency fee agreement that complies with O. Reg. 563/20."}`,
      `Retainer: please deposit ${A.cents(e.retainer)} to our trust account before we start. It is held in trust and applied to approved bills only.`,
      "Conflicts: we searched our records and found no conflict that prevents us from acting.", "Please sign below to confirm. Yours truly, Linda K Law"];
  }
  A.action("docs-eng-draft", () => {
    const e = st.eng, m = e && A.matter(e.matter); if (!m || !canEditDocs(m)) return; const text = engagementText(m, e);
    A.agent.run({ title: `Engagement letter: ${m.number}`, matterId: m.id, steps: ["Reading the matter and conflict result", "Drafting scope, fees and retainer", "Checking Rules of Professional Conduct items"], done() {
      return A.gate({ kind: "document.release", cap: "documents.release", title: `Engagement letter: ${m.name}`, matterId: m.id, preparedBy: "agent", summary: `For ${contactName(m.clientId)}. Approve to file it and prepare a signature request.`,
        body: String(h`${text.map((l) => h`<p>${l}</p>`)}`), checks: [{ label: "Conflict search recorded", ok: true }, { label: "Fee basis stated", ok: true }, { label: "Retainer amount stated in CAD", ok: +e.retainer > 0 }],
        effect: "docs-engagement", payload: { matterId: m.id, text } }); } });
    A.toast("Drafting. It will appear in Approvals.");
  });
  A.effect("docs-engagement", (p, g) => {
    const m = A.store.get("matters", p.matterId); if (!m) return;
    const d = A.store.put("docs", { id: A.id("d"), matterId: m.id, name: `Engagement letter (${A.date(A.today)})`, kind: "letter", added: A.today, by: "agent", approvedBy: g.decidedBy, pages: toPages(p.text) });
    A.store.put("docmeta", { id: d.id, status: "final", tags: ["engagement"], received: "" });
    const c = A.contact(m.clientId); const sr = A.store.put("signreqs", { matterId: m.id, docId: d.id, docName: d.name, status: "draft", signers: [{ name: c.name, contactId: c.id, email: c.email || "", status: "waiting" }, { name: A.user(m.responsible).name, userId: m.responsible, status: "waiting" }], events: [], created: A.now() });
    A.audit("document.released", { target: d.id, matterId: m.id, note: "Engagement letter approved; signature request prepared" }); st.sigSel = sr.id;
  });

  /* ---------- signature requests and the client signing page ---------- */
  A.seed("signreqs", [
    { id: "sr-1", matterId: "m4", docId: "d-will", docName: "Consent to act as estate trustee", status: "sent", sent: "2026-10-06 11:20", created: "2026-10-06 11:02",
      signers: [{ name: "Grace Mendel", contactId: "c-grace", email: "grace.mendel@example.ca", status: "viewed", at: "2026-10-07 19:05" }, { name: "Linda Kassim", userId: "u-linda", status: "waiting" }],
      events: [{ at: "2026-10-06 11:20", what: "Sent to Grace Mendel", by: "u-linda" }, { at: "2026-10-07 19:05", what: "Viewed by Grace Mendel", by: "c-grace" }] },
    { id: "sr-2", matterId: "m2", docId: "d-aps", docName: "Direction re title", status: "complete", sent: "2026-09-29 10:00", created: "2026-09-29 09:40",
      signers: [{ name: "Samuel and Ruth Okafor", contactId: "c-okafor", status: "signed", at: "2026-09-29 18:30" }, { name: "Yonathan Degu", userId: "u-yoni", status: "signed", at: "2026-09-30 09:10" }],
      events: [{ at: "2026-09-29 10:00", what: "Sent to Samuel and Ruth Okafor", by: "u-yoni" }, { at: "2026-09-29 18:30", what: "Signed by Samuel and Ruth Okafor", by: "c-okafor" }, { at: "2026-09-30 09:10", what: "Signed by Yonathan Degu", by: "u-yoni" }] },
  ]);
  const evHash = (sr, i) => hash(sr.id + sr.events.slice(0, i + 1).map((e) => e.at + e.what).join("|"));
  const nextSigner = (sr) => sr.signers.find((s) => s.status !== "signed");
  const sigStatus = (sr) => (sr.status === "complete" ? "signed" : sr.status === "voided" ? "voided" : sr.status === "draft" ? "draft" : "sent");
  function addEvent(sr, what, by) { const events = sr.events.concat({ at: A.now(), what, by }); A.store.put("signreqs", { id: sr.id, events }); return events; }
  function signaturesPage(el) {
    A.setContext({ kind: "documents", label: "Signatures" });
    if (!A.can("documents.view")) return A.render(el, locked(`${A.roleName(A.me().role)} cannot open signature requests.`));
    const rows = A.store.all("signreqs").filter((s) => { const m = A.matter(s.matterId); return m && canDocs(m); }), sel = rows.find((s) => s.id === st.sigSel) || null;
    const m = sel && A.matter(sel.matterId), can = m && canEditDocs(m), nx = sel && nextSigner(sel);
    const detail = sel && h`<div class="card"><div class="row wrap"><h3 class="grow" style="margin:0">${sel.docName}</h3>${chip(sigStatus(sel))}${demo}</div>
      <div class="muted small">${m.number} ${m.name} · created ${A.dateTime(sel.created)} · order: ${docSettings().signOrder}</div>
      <h3 style="margin-top:10px">Signers</h3>${sel.signers.map((s, i) => h`<div class="row" style="padding:4px 0;border-bottom:1px solid var(--line)"><span class="muted small">${i + 1}</span><b class="grow">${s.name}</b>${chip(s.status === "waiting" ? (nx === s && sel.status === "sent" ? "pending" : "draft") : s.status)}<span class="muted small">${s.at ? A.dateTime(s.at) : ""}</span></div>`)}
      <div class="row wrap" style="margin-top:8px">${sel.status === "draft" ? ui.btn({ label: "Send for signature", a: "docs-sig-send", id: sel.id, primary: true, small: true, disabled: !can, why: roleWhy("send documents") }) : ""}
        ${sel.status === "sent" ? h`${ui.btn({ label: "Send reminder", a: "docs-sig-remind", id: sel.id, small: true, disabled: !can, why: roleWhy("send documents") })}${nx && nx.userId === A.me().id ? ui.btn({ label: "Sign as firm", to: `sign/${sel.id}`, primary: true, small: true }) : ""}${ui.btn({ label: "Void", a: "docs-sig-void", id: sel.id, small: true, danger: true, disabled: !can, why: roleWhy("void requests") })}` : ""}
        ${nx && nx.contactId && sel.status === "sent" ? ui.btn({ label: "Client signing page", to: `sign/${sel.id}`, small: true }) : ""}</div>
      <h3 style="margin-top:10px">Audit trail</h3><p class="muted small">Each entry chains a hash of every entry before it, so an edit to any step shows.</p>
      ${sel.events.length ? sel.events.map((e, i) => h`<div class="small" style="padding:3px 0"><span style="font-family:var(--mono)" class="muted">${evHash(sel, i)}</span> ${A.dateTime(e.at)} · ${e.what}</div>`) : h`<p class="muted small">Not sent yet.</p>`}</div>`;
    A.render(el, h`<div class="page">${docsHeader("Documents", "Track outgoing signature packages from send to completion.")}${docTabs("documents/signatures")}
      ${ui.split(ui.list(rows.map((s) => ({ id: s.id, title: s.docName, sub: `${A.matter(s.matterId).number} · ${s.signers.filter((x) => x.status === "signed").length} of ${s.signers.length} signed`, status: sigStatus(s) === "sent" ? "pending" : sigStatus(s), on: sel && s.id === sel.id })), "docs-sig-sel", { empty: "No signature requests", emptyHint: " Approve an engagement letter to prepare one." }), detail, { emptyTitle: "Pick a request" })}</div>`);
  }
  A.action("docs-sig-sel", (el) => { st.sigSel = el.dataset.id; A.refresh(); });
  A.action("docs-sig-send", (el) => { const sr = A.store.get("signreqs", el.dataset.id); const m = sr && A.matter(sr.matterId); if (!m || !canEditDocs(m) || sr.status !== "draft") return;
    A.store.put("signreqs", { id: sr.id, status: "sent", sent: A.now() }); addEvent(A.store.get("signreqs", sr.id), `Sent to ${sr.signers[0].name}`, A.me().id); A.audit("signature.sent", { target: sr.id, matterId: m.id }); A.toast("Sent for signature (demo)", "ok"); });
  A.action("docs-sig-remind", (el) => { const sr = A.store.get("signreqs", el.dataset.id); const nx = sr && nextSigner(sr); if (!nx) return; addEvent(sr, `Reminder sent to ${nx.name}`, A.me().id); A.toast(`Reminder sent to ${nx.name} (demo)`); });
  A.action("docs-sig-void", (el) => { const sr = A.store.get("signreqs", el.dataset.id); if (!sr) return;
    A.modal("Void signature request", h`<p>Void “${sr.docName}”? Signers can no longer sign it.</p>`, [{ label: "Cancel" }, { label: "Void", primary: true, run: () => { A.store.put("signreqs", { id: sr.id, status: "voided" }); addEvent(A.store.get("signreqs", sr.id), "Voided", A.me().id); A.audit("signature.voided", { target: sr.id, matterId: sr.matterId }); } }]); });
  /** Client signing page: no account needed in production (a one-time link); here the signed-in client or the firm signer sees it. */
  function signPage(el, { id }) {
    const sr = A.store.get("signreqs", id), me = A.me(), nx = sr && nextSigner(sr);
    const mine = nx && ((nx.contactId && nx.contactId === me.contactId) || (nx.userId && nx.userId === me.id));
    const firmView = sr && A.matter(sr.matterId) && canDocs(A.matter(sr.matterId));
    if (!sr || (!mine && !firmView && !sr.signers.some((s) => s.contactId && s.contactId === me.contactId))) return A.render(el, locked("This signing link is not for you, or it has expired."));
    const d = A.store.get("docs", sr.docId);
    A.setContext({ kind: "signing", id: sr.id, label: sr.docName });
    A.render(el, h`<div class="page" style="max-width:760px">${ui.header({ title: sr.docName, status: chip(sigStatus(sr)), meta: demo, sub: "Linda K Law · secure signing" })}
      <div class="card"><div class="docview">${(d ? d.pages.flat() : ["Document text is not available in this demo."]).map((t, i) => h`<div class="ln"><i>${i + 1}</i><span>${t}</span></div>`)}</div></div>
      ${sr.status !== "sent" ? h`<div class="card">${sr.status === "complete" ? "Everyone has signed. A signed copy is on file." : "This request is not open for signing."}</div>`
        : mine ? h`<div class="card"><h3>Questions before you sign?</h3><textarea id="sign-q" class="input" rows="2" placeholder="Your question goes to your lawyer"></textarea>
            <div class="row" style="margin:6px 0 12px"><span class="grow"></span>${ui.btn({ label: "Send question", a: "sign-ask", id: sr.id, small: true })}</div>
            <label class="field">Type your full name to sign<input id="sign-name" class="input" placeholder="${nx.name}"></label>
            <label class="row small"><input type="checkbox" id="sign-ok"> I have read this document and agree to sign it electronically.</label>
            <div class="row" style="margin-top:8px"><span class="grow"></span>${ui.btn({ label: "Sign", a: "sign-do", id: sr.id, primary: true })}</div></div>`
        : h`<div class="card muted">Waiting for ${nx ? nx.name : "the next signer"}.</div>`}</div>`);
  }
  A.action("sign-ask", (el) => { const sr = A.store.get("signreqs", el.dataset.id); const t = document.getElementById("sign-q"); const q = t ? t.value.trim() : ""; if (!sr || !q) return A.toast("Type your question first");
    A.store.put("notes", { matterId: sr.matterId, by: A.me().id, at: A.now(), text: `Question on “${sr.docName}” before signing: ${q}` }); addEvent(sr, `Question from ${A.me().name}`, A.me().id); A.toast("Sent to your lawyer", "ok"); });
  A.action("sign-do", (el) => {
    const sr = A.store.get("signreqs", el.dataset.id), nx = sr && nextSigner(sr), me = A.me(); if (!nx || sr.status !== "sent") return;
    if (!((nx.contactId && nx.contactId === me.contactId) || (nx.userId && nx.userId === me.id))) return A.toast("It is not your turn to sign", "bad");
    const nm = document.getElementById("sign-name"), ok = document.getElementById("sign-ok");
    if (!nm || nm.value.trim().toLowerCase() !== nx.name.toLowerCase().split(" and ")[0].toLowerCase() && nm.value.trim().toLowerCase() !== nx.name.toLowerCase()) return A.toast(`Type ${nx.name} to sign`, "bad");
    if (!ok || !ok.checked) return A.toast("Tick the box to agree", "bad");
    const signers = sr.signers.map((s) => (s === nx ? { ...s, status: "signed", at: A.now() } : s)); const done = signers.every((s) => s.status === "signed");
    A.store.put("signreqs", { id: sr.id, signers, status: done ? "complete" : "sent" }); addEvent(A.store.get("signreqs", sr.id), `Signed by ${nx.name}`, me.id);
    if (done) addEvent(A.store.get("signreqs", sr.id), "Completed; signed copy filed", "system");
    A.audit("signature.signed", { target: sr.id, matterId: sr.matterId, note: nx.name }); A.toast("Signed. Thank you.", "ok");
  });

  /* ---------- document settings ---------- */
  function docSettingsPage(el) {
    A.setContext({ kind: "documents", label: "Document settings" });
    const s = docSettings(), can = canTpl() || canSetup(), why = "Your role cannot change document settings";
    const ex = String(s.naming).replace("{YYYY}", A.today.slice(0, 4)).replace("{MM}", A.today.slice(5, 7)).replace("{DD}", A.today.slice(8, 10)).replace("{MATTER}", "2026-0142").replace("{NAME}", "Letter to opposing counsel");
    const row = (label, f, type, display) => h`<dt>${label}</dt><dd>${ui.inline({ name: "docs-set", id: f, value: s[f], type, display, disabled: !can, why, options: f === "signOrder" ? [["sequential", "One after another"], ["parallel", "All at once"]] : undefined })}</dd>`;
    A.render(el, h`<div class="page">${docsHeader("Documents", "Firm-wide rules for documents. Changes apply to new documents.")}${docTabs("documents/settings")}
      <div class="grid g2"><div class="card"><h3>Naming and limits</h3><dl class="kv">${row("File name pattern", "naming", "text")}<dt>Example</dt><dd class="small" style="font-family:var(--mono)">${ex}</dd>
        ${row("Keep deleted documents (days)", "trashDays", "number")}${row("Upload limit (MB)", "maxMb", "number")}${row("Secure link default (days)", "linkDays", "number")}</dl></div>
      <div class="card"><h3>Signing</h3><dl class="kv">${row("Signer order", "signOrder", "select", s.signOrder === "parallel" ? "All at once" : "One after another")}${row("Reminder every (days)", "reminderDays", "number")}</dl>
        <h3>Document types</h3><div>${s.types.map((t) => ui.badge(t))}</div><div class="row" style="margin-top:6px"><span class="grow"></span>${ui.btn({ label: "Add type", a: "docs-set-type", small: true, disabled: !can, why })}</div>
        <h3>Folder templates</h3><p class="muted small">Folders come from matter templates. <a href="#" data-a="go" data-to="matters/setup/templates">Edit matter templates</a></p></div></div></div>`);
  }
  A.inlineEdit("docs-set", (f, v) => { if (!(canTpl() || canSetup())) return false; const num = ["trashDays", "maxMb", "linkDays", "reminderDays"].includes(f);
    if (num && !(+v > 0)) { A.toast("Enter a positive number", "bad"); return false; } A.store.put("mxsettings", { id: "docs", [f]: num ? +v : v }); A.audit("setup.documents", { note: `${f} = ${v}` }); });
  A.action("docs-set-type", () => A.modal("Add document type", h`<label class="field">Type<input name="t"></label>`, [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const t = String(A.formValues(d).t || "").trim().toLowerCase(); if (!t) return false;
    const s = docSettings(); if (!s.types.includes(t)) A.store.put("mxsettings", { id: "docs", types: s.types.concat(t) }); A.audit("setup.documents", { note: `type ${t}` }); } }]));

  /* ---------- bundle builder ---------- */
  function bundlePage(el, p) {
    A.setContext({ kind: "documents", label: "Bundle builder" });
    if (!A.can("documents.view")) return A.render(el, locked(`${A.roleName(A.me().role)} cannot open matter documents.`));
    const ms = A.matters((m) => m.status === "open").filter(canDocs); const m = (p && p.id && A.matter(p.id)) || ms.find((x) => x.id === st.bmatter) || ms[0];
    if (!m) return A.render(el, h`<div class="page">${docsHeader("Documents")}${docTabs("documents/bundle")}${ui.empty("No matters you can open", "")}</div>`);
    st.bmatter = m.id; const b = st.bundle[m.id] || (st.bundle[m.id] = { ids: [], title: "Brief of documents" });
    const ds = docsOf(m), chosen = b.ids.map((id) => A.store.get("docs", id)).filter(Boolean); let page = 2;
    A.render(el, h`<div class="page">${docsHeader("Documents", "Combine chosen documents into one paginated bundle with tabs and an index.")}${docTabs("documents/bundle")}
      ${ui.filters({ selects: [{ c: "docs-b-matter", value: m.id, label: "Matter", options: ms.map((x) => [x.id, `${x.number} ${x.name}`]) }], right: h`<label class="small">Title <input class="input" style="width:220px" data-c="docs-b-title" value="${b.title}"></label>` })}
      <div class="grid g2"><div class="card"><h3>Documents on the matter</h3>${ui.table([{ label: "Document", render: (d) => d.name }, { label: "Pages", num: true, render: (d) => d.pages.length },
        { label: "", render: (d) => (b.ids.includes(d.id) ? ui.status("confirmed", "Added") : ui.btn({ label: "Add", a: "docs-b-add", id: d.id, small: true })) }], ds, { empty: "No documents" })}</div>
      <div class="card"><h3>Index</h3>${chosen.length ? h`<table class="tbl"><tbody>${chosen.map((d, i) => { const at = page; page += d.pages.length;
          return h`<tr><td>Tab ${i + 1}</td><td>${d.name}</td><td class="num">p. ${at}</td><td>${ui.btn({ label: "↑", a: "docs-b-mv", id: d.id, data: { dir: "-1" }, small: true, disabled: i === 0 })}${ui.btn({ label: "↓", a: "docs-b-mv", id: d.id, data: { dir: "1" }, small: true, disabled: i === chosen.length - 1 })}${ui.btn({ label: "Remove", a: "docs-b-rm", id: d.id, small: true })}</td></tr>`; })}</tbody></table>
        <p class="muted small">${page - 1} pages including the index page. ${demo}</p>` : ui.empty("Nothing added", " Add documents from the left.")}
        <div class="row"><span class="grow"></span>${ui.btn({ label: "✦ Build bundle", a: "docs-b-build", id: m.id, agent: true, primary: true, disabled: !chosen.length || !canEditDocs(m), why: !chosen.length ? "Add documents first" : roleWhy("create documents") })}</div></div></div></div>`);
  }
  const bcur = () => st.bundle[st.bmatter];
  A.change("docs-b-matter", (el) => { st.bmatter = el.value; A.go("documents/bundle"); });
  A.change("docs-b-title", (el) => { if (bcur()) bcur().title = el.value; });
  A.action("docs-b-add", (el) => { const b = bcur(); if (b && !b.ids.includes(el.dataset.id)) b.ids.push(el.dataset.id); A.refresh(); });
  A.action("docs-b-rm", (el) => { const b = bcur(); if (b) b.ids = b.ids.filter((x) => x !== el.dataset.id); A.refresh(); });
  A.action("docs-b-mv", (el) => { const b = bcur(); if (!b) return; const i = b.ids.indexOf(el.dataset.id), j = i + +el.dataset.dir; if (i < 0 || j < 0 || j >= b.ids.length) return; [b.ids[i], b.ids[j]] = [b.ids[j], b.ids[i]]; A.refresh(); });
  A.action("docs-b-build", (el) => {
    const m = A.matter(el.dataset.id), b = m && st.bundle[m.id]; if (!m || !b || !b.ids.length || !canEditDocs(m)) return;
    const ds = b.ids.map((id) => A.store.get("docs", id)).filter(Boolean); let pg = 2; const index = ds.map((d, i) => { const l = `Tab ${i + 1}  ${d.name}  p. ${pg}`; pg += d.pages.length; return l; });
    const text = [b.title, m.name, `Court file / our file ${m.number}`, "INDEX"].concat(index, ds.flatMap((d, i) => [`TAB ${i + 1}: ${d.name}`].concat(d.pages.flat())));
    A.agent.run({ title: `Bundle: ${b.title} (${m.number})`, matterId: m.id, steps: ["Ordering documents", "Paginating and tabbing", "Writing the index"], done() {
      return A.gate({ kind: "document.release", cap: "documents.release", title: `${b.title}: ${m.name}`, matterId: m.id, preparedBy: "agent", summary: `${ds.length} documents, ${pg - 1} pages with index.`,
        body: String(h`<p><b>INDEX</b></p>${index.map((l) => h`<p>${l}</p>`)}`), checks: [{ label: "No draft documents in the bundle", ok: ds.every((d) => meta(d).status !== "draft") }, { label: "Page numbers continuous", ok: true }],
        effect: "matters-doc-release", payload: { matterId: m.id, name: `${b.title} (${A.date(A.today)})`, text } }); } });
    st.bundle[m.id] = { ids: [], title: b.title }; A.toast("Building. The bundle goes to Approvals.");
  });

  /* ---------- intents ---------- */
  const ctxMatter = (c) => (c && c.kind === "matter" ? A.matter(c.id) : null);
  const pickMatter = (verb) => ({ text: `Which matter should I ${verb}? Open one, or pick below.`, actions: A.matters((m) => m.status === "open").slice(0, 5).map((m) => ({ label: `${m.number} ${m.name}`, go: `matters/${m.id}` })) });
  A.intent({ match: /summari[sz]e (this|the)? ?matter|matter brief/i, label: "Summarize this matter", example: "Summarize this matter", run(text, c) {
    const m = ctxMatter(c); if (!m) return pickMatter("summarize");
    if (!canDocs(m)) return { text: `I can't summarize ${m.name}: you cannot open its documents.` };
    const b = brief(m); if (!b.length) return { text: `${m.name} has no documents yet, so I have nothing to cite.` };
    return { text: b.map((s) => s.text).join(" "), cites: b.flatMap((s) => s.cites), actions: [{ label: "Draft client update", run: () => startDraft(m, "client") }] };
  } });
  A.intent({ match: /what did (the )?opposing counsel (ask|want|request)/i, label: "What did opposing counsel ask?", example: "What did opposing counsel ask?", run(text, c) {
    const m = ctxMatter(c) || A.matter("m1");
    if (!m || !canDocs(m)) return { text: "I can't find correspondence from opposing counsel on any matter you can open." };
    const a = asks(m); if (!a.length) return { text: `No correspondence from opposing counsel is on file for ${m.name}.` };
    const oc = (m.opposing || []).map(A.contact).find((x) => /counsel/i.test(x.role || ""));
    return { text: `${oc ? oc.name : "Opposing counsel"} asked for ${a.length} things on ${m.name}: ${a.map((x) => x.quote.replace(/^We (also )?/, "")).join(" ")}`, cites: a, actions: [{ label: "Draft a reply", run: () => startDraft(m, "opposing") }] };
  } });
  A.intent({ match: /^\s*draft (a |an )?(\w+ )?(letter|update|demand)/i, label: "Draft a letter", example: "Draft a letter to opposing counsel", run(text, c) {
    const m = ctxMatter(c); if (!m) return pickMatter("draft for");
    const type = /demand/i.test(text) ? "demand" : /client|update/i.test(text) ? "client" : "opposing";
    if (!(A.canMatter(m, "documents.edit") && A.can("documents.edit", "act"))) return { text: `Your role cannot draft documents on ${m.name}.` };
    startDraft(m, type);
    return { text: `Drafting a ${TYPES[type].toLowerCase()} for ${m.name} from its documents. It goes to a lawyer for approval before release.`, actions: [{ label: "Open documents", run: () => { st.tab[m.id] = "documents"; A.go(`matters/${m.id}`); } }] };
  } });
  A.intent({ match: /^\s*(find|search for) (.+)/i, label: "Find text in documents", example: "find roof membrane", run(text) {
    const q = text.replace(/^\s*(find|search for)\s+/i, "").replace(/^["']|["']$/g, "").trim().toLowerCase(); if (!q) return;
    const hits = [];
    A.store.all("docs").forEach((d) => { const m = A.store.get("matters", d.matterId); if (!m || !canDocs(m)) return; lines(d).forEach((l) => { if (l.t.toLowerCase().includes(q)) hits.push(cite(l, d)); }); });
    if (!hits.length) return { text: `No line in documents you can open contains "${q}".` };
    return { text: `Found "${q}" on ${hits.length} line${hits.length > 1 ? "s" : ""}${hits.length > 8 ? "; showing the first 8" : ""}.`, cites: hits.slice(0, 8) };
  } });
  A.suggest("matter", (c) => { const m = A.matter(c.id); if (!m) return [];
    return ["Summarize this matter"].concat(asks(m).length ? ["What did opposing counsel ask?"] : [], [m.opposing && m.opposing.length ? "Draft a letter to opposing counsel" : "Draft a client update"]); });
  A.suggest("matters", () => ["find roof membrane"]);

  /* ---------- registration ---------- */
  const smokeDocs = ["m1", "m2", "m7", "m8"];
  A.module({ id: "matters", label: "Matters", icon: "▤", order: 20, cap: "matters.directory",
    smoke: ["matters", "matters/unfiled", "matters/setup", "matters/setup/templates", "matters/setup/numbering", "matters/access"].concat(smokeDocs.map((x) => `matters/${x}`), ["matters/nope", "matters/m1/chronology", "matters/m1/communications", "matters/m5/communications",
      "matters/m1/analyze", "matters/m1/sharing", "matters/m4/sharing", "matters/m6/doc/d-lease", "matters/m4/doc/d-will", "matters/m1/doc/d-claim"]),
    routes: { matters: listPage, "matters/unfiled": unfiledPage, "matters/setup": areasPage, "matters/access": accessPage, "matters/setup/templates": templatesPage, "matters/setup/numbering": numberingPage,
      "matters/:id": workspace, "matters/:id/:tab": (el, p) => { st.tab[p.id] = p.tab; workspace(el, p); },
      "matters/:id/doc/:doc": (el, p) => { st.tab[p.id] = "documents"; st.doc[p.id] = p.doc; workspace(el, p); } } });
  A.module({ id: "contacts", label: "Contacts", icon: "◍", order: 25, cap: "contacts.view",
    smoke: ["contacts", "contacts/import", "contacts/fields", "contacts/c-hanna", "contacts/c-northgate", "contacts/c-selam", "contacts/c-osei"],
    routes: { contacts: contactsList, "contacts/import": importPage, "contacts/fields": fieldsPage, "contacts/:id": contactDetail } });
  /** Documents area: client users never see it (they use the portal and the signing page). */
  const staff = (fn) => (el, p) => (A.me().role === "client" ? A.render(el, locked("Clients see shared documents in the portal.")) : fn(el, p));
  A.module({ id: "documents", label: "Documents", icon: "▧", order: 22, visible: () => A.me().role !== "client" && (A.can("documents.view") || A.can("documents.templates")),
    smoke: ["documents", "documents/templates", "documents/assemble", "documents/assemble/tpl-1", "documents/precedents", "documents/engagement", "documents/signatures", "documents/bundle", "documents/bundle/m1", "documents/trash", "documents/settings"],
    routes: { documents: staff(libraryPage), "documents/templates": staff(templatesDocPage), "documents/assemble": staff(assemblePage), "documents/assemble/:id": staff(assemblePage), "documents/precedents": staff(precedentsPage),
      "documents/engagement": staff(engagementPage), "documents/signatures": staff(signaturesPage), "documents/bundle": staff(bundlePage), "documents/bundle/:id": staff(bundlePage), "documents/trash": staff(trashPage), "documents/settings": staff(docSettingsPage) } });
  A.module({ id: "sign", label: "Signing", nav: false, smoke: ["sign/sr-1", "sign/sr-2", "sign/nope"], routes: { "sign/:id": signPage } });
})();
