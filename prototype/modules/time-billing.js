/* Time and billing: agent-suggested time, timesheets, timer, pre-bill checks, Ontario accounts with HST, A/R aging. */
(function () {
  "use strict";
  const A = window.App, h = A.h, S = A.store;

  /* ---------- reference data (fictional) ---------- */
  const RATES = { "u-linda": 450, "u-yoni": 375, "u-ayo": 300, "u-meraf": 175, "u-kaleab": 120 };
  const BUDGET = { m1: 15000, m4: 6000, m5: 2500, m7: 25000 };
  const HST_NO = "GST/HST 81234 5678 RT0001", QST_NO = "QST 1230456789 TQ0001";
  /* Sales tax by the client's province (place of supply). ON is the firm's home rate; the others are examples to verify. */
  /* Tax rates in ppm (1% = 10000), the billing engine's unit. */
  const TAX = {
    ON: { name: "Ontario", lines: [["HST", 130000]] },
    AB: { name: "Alberta", lines: [["GST", 50000]] },
    BC: { name: "British Columbia", lines: [["GST", 50000], ["PST", 70000]] },
    MB: { name: "Manitoba", lines: [["GST", 50000], ["RST", 70000]] },
    SK: { name: "Saskatchewan", lines: [["GST", 50000], ["PST", 60000]] },
    QC: { name: "Quebec", lines: [["GST", 50000], ["QST", 99750]] },
  };
  const CLIENT_PROV = { "c-selam": "BC", "c-elm": "QC", "c-tsion": "AB" };
  const provOf = (inv) => inv.province || CLIENT_PROV[inv.clientId] || "ON";
  const pct = (ppm) => `${+(ppm / 10000).toFixed(3)}%`;
  const SURCHARGE_CAP = 2.4;
  /* Money: integer CAD cents inside, dollars at the record boundary (other modules read dollars).
     rhu = round half away from zero, applied once per value: time line amount, each tax line on the summed base. */
  const rhu = (num, den) => { const s = Math.sign(num) * Math.sign(den), a = Math.abs(num), d = Math.abs(den); return s * Math.floor((2 * a + d) / (2 * d)); };
  const toC = (d) => Math.sign(+d || 0) * Math.round(Math.abs(+d || 0) * 100);
  const toD = (c) => c / 100;
  /* every amount on screen goes through integer cents (App.cents) */
  const cad = (d) => A.cents(toC(d));
  const tenths = (hrs) => Math.round((+hrs || 0) * 10);
  const lineC = (hours, rt) => rhu(Math.round((+hours || 0) * 100) * toC(rt), 100);
  const r2 = (n) => toD(toC(n));
  const sumH = (l) => tenths(l.reduce((s, e) => s + tenths(e.hours), 0) / 10) / 10;
  const rateRows = (uid) => S.where("rates", (r) => r.userId === uid && r.status !== "pending").sort((a, b) => b.from.localeCompare(a.from));
  /** Standard rate for a timekeeper on a date, from the rate table (effective-dated). */
  const rate = (uid, date) => { const d = date || A.today, r = rateRows(uid).find((x) => x.from <= d); return r ? r.rate : RATES[uid] || 0; };
  const amt = (e) => toD(lineC(e.hours, e.rate));
  const sumD = (l, f) => toD(l.reduce((s, x) => s + toC(f(x)), 0));
  const surchargeC = (balD, pctV) => rhu(toC(balD) * Math.round((+pctV || 0) * 10000), 1e6);
  const sumAmt = (l) => toD(l.reduce((s, e) => s + lineC(e.hours, e.rate), 0));

  /* ---------- seed ---------- */
  const T = (id, userId, matterId, date, hours, narrative, status, source, rt) => ({ id, userId, matterId, date, hours, rate: rt || RATES[userId], narrative, status, source: source || null });
  A.seed("timeEntries", () => [
    T("te-s1", "u-linda", "m1", "2026-10-09", 0.3, "Reviewed email from D. Osei requesting 30-day extension for statement of defence and productions; considered response", "suggested", { kind: "email", ref: "d-email-osei" }),
    T("te-s2", "u-linda", "m4", "2026-10-09", 0.8, "Reviewed will and revised schedule of estate assets for certificate of appointment application", "suggested", { kind: "doc", ref: "d-will" }),
    T("te-s3", "u-linda", "m7", "2026-10-09", 0.5, "Telephone call with client re discovery scheduling and undertakings", "suggested", { kind: "calendar", ref: "Call: Selam Mekonnen, 10:30-11:00" }),
    T("te-s4", "u-ayo", "m5", "2026-10-09", 1.1, "Revised demand letter re common-law reasonable notice; reviewed termination letter", "suggested", { kind: "doc", ref: "d-term" }),
    T("te-s5", "u-ayo", "m3", "2026-10-09", 0.6, "Follow-up call with client re physiotherapy records and OHIP summary", "suggested", { kind: "calendar", ref: "Call: Daniel Fortier, 14:00-14:35" }),
    T("te-s6", "u-meraf", "m1", "2026-10-09", 0.4, "Collected storage agreement and inventory invoices requested by opposing counsel", "suggested", { kind: "email", ref: "d-email-osei" }),
    T("te-s7", "u-kaleab", "m2", "2026-10-09", 0.7, "Prepared draft requisition letter to seller's solicitor", "suggested", { kind: "doc", ref: "d-aps" }),
    T("te-s8", "u-yoni", "m6", "2026-10-09", 0.9, "Marked up relocation and personal guarantee clauses in lease renewal", "suggested", { kind: "doc", ref: "d-lease" }),
    T("te-s9", "u-linda", "m1", "2026-10-09", 0.3, "Reviewed email from D. Osei re extension for statement of defence", "suggested", { kind: "email", ref: "d-email-osei" }),
    T("te-14", "u-ayo", "m7", "2026-10-04", 7.5, "Reviewed defendant's productions and prepared discovery brief", "accepted"),
    T("te-1", "u-linda", "m1", "2026-10-05", 1.2, "Reviewed defendant's roof repair notices and drafted request to admit facts", "accepted"),
    T("te-2", "u-ayo", "m1", "2026-10-06", 3.5, "Research bailee liability; draft memo; call with client; revise claim chronology", "accepted"),
    T("te-3", "u-meraf", "m1", "2026-10-07", 0.4, "File review", "accepted"),
    T("te-4", "u-linda", "m1", "2026-10-08", 0.6, "Telephone call with client re extension request from opposing counsel; took instructions", "accepted", null, 425),
    T("te-5", "u-linda", "m4", "2026-10-02", 1.5, "Prepared application for certificate of appointment of estate trustee with a will (Form 74A)", "accepted"),
    T("te-6", "u-meraf", "m4", "2026-10-06", 2.0, "Prepared affidavit of execution of will and compiled asset list from bank statements", "accepted"),
    T("te-7", "u-ayo", "m5", "2026-10-01", 4.0, "Drafted demand letter to Northgate Logistics re reasonable notice applying Bardal factors", "accepted"),
    T("te-8", "u-ayo", "m5", "2026-10-07", 3.0, "Calculated notice period and benefits loss; drafted damages summary for client", "accepted"),
    T("te-9", "u-linda", "m5", "2026-10-08", 0.8, "Reviewed and revised demand letter before delivery", "accepted"),
    T("te-10", "u-linda", "m7", "2026-10-06", 2.2, "Prepared for examination for discovery of Orion Signs representative; reviewed productions", "accepted"),
    T("te-11", "u-kaleab", "m2", "2026-10-06", 1.5, "Title search review and summary of registered instruments on 14 Birchmount Rd", "accepted"),
    T("te-12", "u-yoni", "m6", "2026-10-07", 2.0, "Reviewed lease renewal draft; noted abatement and HVAC issues for client", "accepted"),
    T("te-13", "u-yoni", "m2", "2026-10-08", 0.5, "Reviewed mortgage instructions from lender", "accepted"),
  ]);
  /* passive activity log: what the user touched, per day; entryId links a row to the suggestion built from it */
  const AL = (id, userId, date, at, kind, matterId, what, mins, entryId, ref, narr) => ({ id, userId, date, at, kind, matterId, what, mins, entryId: entryId || null, ref: ref || null, narr: narr || null });
  A.seed("activityLog", () => [
    AL("al-1", "u-linda", "2026-10-09", "08:42", "email", "m1", "Read: D. Osei, extension request for defence and productions", 4, "te-s1", "d-email-osei"),
    AL("al-2", "u-linda", "2026-10-09", "08:51", "email", "m1", "Drafted reply to D. Osei (saved, not sent)", 9, "te-s1"),
    AL("al-3", "u-linda", "2026-10-09", "09:20", "doc", "m4", "Opened Last Will of Margaret Lindqvist", 18, "te-s2", "d-will"),
    AL("al-4", "u-linda", "2026-10-09", "09:40", "doc", "m4", "Edited schedule of estate assets", 27, "te-s2"),
    AL("al-5", "u-linda", "2026-10-09", "10:30", "calendar", "m7", "Call: Selam Mekonnen, 10:30-11:00", 30, "te-s3"),
    AL("al-6", "u-linda", "2026-10-09", "10:31", "call", "m7", "Outgoing call 416-555-0171, 27 min", 27, "te-s3"),
    AL("al-7", "u-linda", "2026-10-09", "13:05", "doc", "m1", "Opened storage agreement (Northgate)", 11, null, null, "reviewed storage agreement re limitation of liability clause"),
    AL("al-8", "u-linda", "2026-10-09", "13:20", "email", "m1", "Sent: Hanna Tesfaye, documents for review", 6, null, null, "email to client enclosing documents for review"),
    AL("al-9", "u-linda", "2026-10-09", "15:10", "email", null, "Read: CPD webinar reminder", 2),
    AL("al-10", "u-ayo", "2026-10-09", "09:05", "doc", "m5", "Opened Termination letter", 25, "te-s4", "d-term"),
    AL("al-11", "u-ayo", "2026-10-09", "09:31", "doc", "m5", "Edited demand letter v3", 41, "te-s4"),
    AL("al-12", "u-ayo", "2026-10-09", "14:00", "calendar", "m3", "Call: Daniel Fortier, 14:00-14:35", 35, "te-s5"),
    AL("al-13", "u-ayo", "2026-10-09", "16:12", "email", "m5", "Sent: Tsion Haile, revised demand letter", 5, null, null, "email to client enclosing revised demand letter for instructions"),
    AL("al-14", "u-meraf", "2026-10-09", "11:02", "email", "m1", "Read: D. Osei, documents requested", 8, "te-s6", "d-email-osei"),
    AL("al-15", "u-meraf", "2026-10-09", "11:15", "doc", "m1", "Uploaded inventory invoices (4 files)", 16, "te-s6"),
    AL("al-16", "u-kaleab", "2026-10-09", "10:10", "doc", "m2", "Edited requisition letter to seller's solicitor", 42, "te-s7", "d-aps"),
    AL("al-17", "u-yoni", "2026-10-09", "13:30", "doc", "m6", "Edited Lease renewal draft (clauses 14, 22)", 54, "te-s8", "d-lease"),
    AL("al-18", "u-linda", "2026-10-09", "08:44", "email", "m1", "Opened D. Osei email again (thread view)", 3, "te-s9", "d-email-osei"),
    AL("al-19", "u-linda", "2026-10-08", "15:00", "calendar", "m1", "Call: Hanna Tesfaye re extension", 36, "te-4"),
    AL("al-20", "u-linda", "2026-10-08", "16:20", "doc", "m5", "Opened demand letter v2", 48, "te-9"),
  ]);
  A.seed("rates", () => Object.entries(RATES).map(([u, r]) => ({ id: `rt-${u}`, userId: u, rate: r, from: "2026-01-01", by: "u-linda", status: "approved" })).concat([{ id: "rt-linda-2025", userId: "u-linda", rate: 425, from: "2025-01-01", by: "u-linda", status: "approved" }]));
  A.seed("paySettings", [{ id: "ps", surcharge: false, rate: 2.4, cost: 2.2, notice: "2026-08-15", card: true, eft: true, pad: true, etransfer: true }]);
  const L = (date, userId, hours, narrative) => ({ date, userId, hours, rate: RATES[userId], narrative, amount: toD(lineC(hours, RATES[userId])) });
  const SEED_INV = [
    { id: "inv-107", number: "2026-0107", matterId: "m7", date: "2026-07-20", dueDate: "2026-08-19", status: "sent", sentAt: "2026-07-20", trustApplied: 0, preparedBy: "agent",
      lines: [L("2026-07-08", "u-linda", 4.5, "Prepared affidavit of documents and schedule A productions"), L("2026-07-15", "u-linda", 3.0, "Attended to discovery plan negotiations with opposing counsel")], disb: [{ desc: "Courier", amount: 21, taxable: true }] },
    { id: "inv-109", number: "2026-0109", matterId: "m4", date: "2026-08-31", dueDate: "2026-09-30", status: "sent", sentAt: "2026-08-31", trustApplied: 500, preparedBy: "agent",
      lines: [L("2026-08-20", "u-linda", 1.8, "Initial meeting with estate trustee; reviewed will and asset information"), L("2026-08-26", "u-meraf", 2.5, "Requested bank and land registry records for estate assets")], disb: [{ desc: "Land registry search", amount: 35, taxable: true }] },
    { id: "inv-110", number: "2026-0110", matterId: "m5", date: "2026-09-05", dueDate: "2026-10-05", status: "paid", sentAt: "2026-09-05", trustApplied: 0, preparedBy: "agent",
      lines: [L("2026-09-02", "u-ayo", 1.5, "Intake meeting; reviewed termination letter and employment history")], disb: [] },
    { id: "inv-112", number: "2026-0112", matterId: "m1", date: "2026-09-25", dueDate: "2026-10-25", status: "sent", sentAt: "2026-09-25", trustApplied: 0, preparedBy: "agent",
      lines: [L("2026-09-16", "u-linda", 2.6, "Drafted statement of claim"), L("2026-09-22", "u-meraf", 0.8, "Arranged personal service on corporate defendant")],
      disb: [{ desc: "Court fee: issuing statement of claim", amount: 270, taxable: false }, { desc: "Process server", amount: 95, taxable: true }] },
  ];
  SEED_INV.forEach((i) => (i.clientId = window.SEED.matters.find((m) => m.id === i.matterId).clientId));
  A.seed("invoices", SEED_INV);
  A.seed("payments", () => [
    { id: "pay-1", invoiceId: "inv-109", matterId: "m4", date: "2026-09-20", amount: 1000, method: "EFT", by: "u-yohannes" },
    { id: "pay-2", invoiceId: "inv-110", matterId: "m5", date: "2026-09-18", amount: totals(SEED_INV[2], true).total, method: "Cheque", by: "u-yohannes" },
  ]);

  /* ---------- calculations ---------- */
  /* line net = amount - write-down; tax once per bill per tax line on the summed taxable base (never per line). */
  const netC = (l) => toC(l.amount) - toC(l.writeDown || 0);
  function totals(inv, bare) {
    const grossC = (inv.lines || []).reduce((s, l) => s + toC(l.amount), 0), feesC = (inv.lines || []).reduce((s, l) => s + netC(l), 0);
    const dtC = (inv.disb || []).filter((d) => d.taxable).reduce((s, d) => s + toC(d.amount), 0);
    const dnC = (inv.disb || []).filter((d) => !d.taxable).reduce((s, d) => s + toC(d.amount), 0);
    const prov = provOf(inv), baseC = feesC + dtC;
    const taxLines = TAX[prov].lines.map(([name, ppm]) => ({ name, rate: ppm, label: `${name} ${pct(ppm)}`, amount: toD(rhu(baseC * ppm, 1e6)) }));
    const taxC = taxLines.reduce((s, x) => s + toC(x.amount), 0), totalC = baseC + dnC + taxC;
    const fees = toD(feesC), dt = toD(dtC), dn = toD(dnC), hst = toD(taxC), total = toD(totalC), base = toD(baseC), writeDown = toD(grossC - feesC);
    if (bare) return { fees, dt, dn, hst, total, prov, taxLines, base, writeDown };
    const trustGates = S.where("approvals", (g) => g.effect === "trust.withdraw" && g.payload && g.payload.invoiceId === inv.id);
    const trustC = toC(inv.trustApplied || 0) + trustGates.filter((g) => g.status === "approved").reduce((s, g) => s + toC(g.payload.amount), 0);
    const trustPending = trustGates.filter((g) => g.status === "pending");
    const paidC = S.where("payments", (p) => p.invoiceId === inv.id).reduce((s, p) => s + toC(p.amount), 0), woC = toC(inv.writtenOff || 0);
    const cns = S.where("creditNotes", (c) => c.invoiceId === inv.id && c.status === "approved"), crC = cns.reduce((s, c) => s + c.preC + c.taxLines.reduce((x, l) => x + l.amountC, 0), 0);
    return { fees, dt, dn, hst, total, prov, taxLines, base, writeDown, trust: toD(trustC), paid: toD(paidC), writtenOff: toD(woC), credited: toD(crC), credits: cns, trustPending, balance: toD(Math.max(0, totalC - trustC - paidC - woC - crC)) };
  }
  const visM = (m) => A.canMatter(m, "billing.view");
  const billMatter = (id) => { const m = S.get("matters", id); return visM(m) ? m : null; };
  const invoicesVisible = () => S.where("invoices", (i) => visM(S.get("matters", i.matterId)) && (A.me().role !== "client" || i.status !== "draft"));
  const viewAll = () => A.can("time.view_all", "all");
  const entriesVisible = (f) => S.where("timeEntries", (e) => (e.userId === A.me().id || (viewAll() && A.canMatter(S.get("matters", e.matterId)))) && (!f || f(e)));
  const mySuggested = () => S.where("timeEntries", (e) => e.userId === A.me().id && e.status === "suggested");
  const unbilled = (mid) => S.where("timeEntries", (e) => e.matterId === mid && e.status === "accepted");
  const onDraft = (eid) => S.where("invoices", (i) => i.status === "draft").some((i) => (i.lines || []).some((l) => l.entryId === eid));
  const age = (inv) => A.daysBetween(inv.sentAt || inv.date, A.today);
  const bucket = (d) => (d <= 30 ? "0-30" : d <= 60 ? "31-60" : d <= 90 ? "61-90" : "90+");
  const BUCKETS = ["0-30", "31-60", "61-90", "90+"];
  const openInv = () => invoicesVisible().filter((i) => (i.status === "sent" || i.status === "approved") && totals(i).balance > 0.005);
  const mName = (id) => { const m = S.get("matters", id); return m ? `${m.number} ${m.name}` : id; };
  const docName = (id) => (S.get("docs", id) || {}).name;
  const srcCell = (e) => { const n = activityFor(e).length; return h`${srcCell0(e)}${n ? h` <button class="btn small" data-a="time-activity" data-id="${e.id}">${n} activit${n > 1 ? "ies" : "y"}</button>` : ""}`; };
  const srcCell0 = (e) => !e.source ? A.ui.badge("manual") : e.source.kind === "calendar" ? h`${A.ui.badge("calendar")} <span class="muted small">${e.source.ref}</span>`
    : h`${A.ui.badge(e.source.kind)} ${docName(e.source.ref) ? A.ui.cite({ doc: e.source.ref, page: 1, label: docName(e.source.ref) }) : ""}`;
  /* record status -> shared chip vocabulary (App.STATUS word, label) */
  const ST = { suggested: ["suggested"], accepted: ["confirmed", "Accepted"], billed: ["closed", "Billed"], draft: ["draft"], approved: ["approved"], sent: ["sent", "Delivered"], paid: ["paid"], "written off": ["closed", "Written off"],
    pending: ["needs-approval"], returned: ["returned"], unbilled: ["open", "Unbilled"], "non-billable": ["inactive", "Non-billable"], active: ["active"], paused: ["hold", "Paused"], cancelled: ["closed", "Cancelled"], unallocated: ["needs-you", "To allocate"], allocated: ["done", "Allocated"], inactive: ["inactive"] };
  const statusBadge = (s) => { const x = ST[s] || [s]; return A.ui.status(x[0], x[1]); };
  const flagChip = (f) => A.ui.status(f.tone === "bad" ? "conflict" : "unconfirmed", f.label);

  /* ---------- entry flags: duplicates and unusual entries ---------- */
  const words = (t) => new Set(String(t).toLowerCase().split(/\W+/).filter((w) => w.length > 3));
  const overlap = (a, b) => { const x = words(a), y = words(b); if (!x.size || !y.size) return 0; let n = 0; x.forEach((w) => y.has(w) && n++); return n / Math.min(x.size, y.size); };
  function flags(e) {
    const out = [], others = S.where("timeEntries", (o) => o.id !== e.id && o.userId === e.userId && o.matterId === e.matterId && o.date === e.date);
    const dup = others.find((o) => (e.source && o.source && e.source.ref === o.source.ref) || overlap(o.narrative, e.narrative) >= 0.6);
    if (dup) out.push({ tone: "bad", label: "Possible duplicate", why: `Overlaps ${dup.hours.toFixed(1)}h entry: ${dup.narrative.slice(0, 70)}` });
    const day = sumH(S.where("timeEntries", (o) => o.userId === e.userId && o.date === e.date));
    const dow = new Date(e.date + "T12:00:00").getDay();
    if (e.hours >= 6) out.push({ tone: "warn", label: "Unusual: long entry", why: `${e.hours.toFixed(1)}h in one entry; split by task` });
    if (day > 10) out.push({ tone: "warn", label: "Unusual: long day", why: `${day.toFixed(1)}h recorded on ${A.date(e.date)}` });
    if (dow === 0 || dow === 6) out.push({ tone: "warn", label: "Weekend", why: "Recorded on a weekend; confirm the date" });
    return out;
  }
  const flagCell = (e) => h`${flags(e).map((f) => h`<span title="${f.why}">${flagChip(f)}</span> `)}`;
  const activityFor = (e) => S.where("activityLog", (a) => a.entryId === e.id);
  const unlinked = (a) => !a.excluded && (!a.entryId || !S.get("timeEntries", a.entryId));
  const KIND = { email: "Email", doc: "Document", call: "Call", calendar: "Calendar" };

  /* ---------- pre-bill checks ---------- */
  function preBill(m, entries) {
    const block = entries.filter((e) => e.narrative.split(";").filter((x) => x.trim()).length >= 3 && e.hours >= 2);
    const vague = entries.filter((e) => e.narrative.trim().split(/\s+/).length < 5);
    const mism = entries.filter((e) => e.rate !== rate(e.userId, e.date));
    const fees = sumAmt(entries);
    const before = sumD(S.where("invoices", (i) => i.matterId === m.id && i.status !== "draft"), (i) => totals(i, true).fees);
    const budget = BUDGET[m.id], spent = toD(toC(before) + toC(fees)), over = budget && spent > budget;
    const who = (l) => l.map((e) => `${A.user(e.userId).initials} ${A.date(e.date)}`).join(", ");
    return [
      { label: block.length ? `Possible block billing: ${who(block)} (several tasks under one time block)` : "No block-billed entries", ok: !block.length, blocking: false },
      { label: vague.length ? `Vague narrative: ${who(vague)} (client cannot tell what was done)` : "Narratives describe the work", ok: !vague.length, blocking: false },
      { label: mism.length ? `Rate differs from timekeeper's standard rate: ${mism.map((e) => `${A.user(e.userId).initials} ${cad(e.rate)} vs ${cad(rate(e.userId, e.date))}`).join(", ")}` : "Rates match the rate card", ok: !mism.length, blocking: false },
      { label: budget ? (over ? `Over budget: ${cad(spent)} billed with this account vs estimate ${cad(budget)}` : `Within estimate (${cad(spent)} of ${cad(budget)})`) : "No fee estimate on file", ok: !over, blocking: false },
    ];
  }
  function nextNumber() {
    const n = S.all("invoices").map((i) => /^2026-01(\d\d)$/.test(i.number || "") ? +i.number.slice(-2) : 0);
    return "2026-01" + String(Math.max(0, ...n) + 1).padStart(2, "0");
  }
  function prepareBills(onlyMatter) {
    if (!A.can("billing.draft", "act")) return A.toast("You cannot prepare bills", "bad");
    const me = A.me().id;
    const ms = A.matters((m) => m.fee === "hourly" && (!onlyMatter || m.id === onlyMatter), "billing.view").filter((m) => unbilled(m.id).some((e) => !onDraft(e.id)));
    if (!ms.length) return A.toast("No accepted unbilled time to bill");
    A.agent.run({ title: onlyMatter ? `Prepare bill: ${mName(onlyMatter)}` : `Prepare bills (${ms.length} matters)`, matterId: onlyMatter || null,
      steps: ["Collecting accepted unbilled time", "Adding disbursements", "Running pre-bill checks", "Drafting accounts"],
      done() {
        ms.forEach((m) => {
          const es = unbilled(m.id).filter((e) => !onDraft(e.id));
          const inv = S.put("invoices", { id: A.id("inv"), number: "", matterId: m.id, clientId: m.clientId, date: A.today, status: "draft", trustApplied: 0, preparedBy: me,
            lines: es.map((e) => ({ entryId: e.id, date: e.date, userId: e.userId, hours: e.hours, rate: e.rate, narrative: e.narrative, amount: amt(e) })), disb: expUnbilled(m.id).map((x) => ({ expenseId: x.id, desc: x.desc, amount: toD(x.amountC), taxable: x.agency === "firm" })) });
          const t = totals(inv, true), checks = preBill(m, es), split = S.get("splits", m.id);
          if (split && split.status === "active") checks.push({ label: `Split billing: ${split.payers.map((p) => `${A.contact(p.contactId).name} ${p.pct}%`).join(", ")}; one bill per payer on approval`, ok: true });
          const g = A.gate({ kind: "bill", cap: "billing.approve", matterId: m.id, preparedBy: me, title: `Bill ${m.number} ${m.name}`,
            summary: `${es.length} entries, fees ${cad(t.fees)}, total with tax ${cad(t.total)}. ${checks.filter((c) => !c.ok).length} check(s) to review.`,
            body: h`<p>Draft account to ${A.contact(m.clientId).name}. Open <b>Billing</b> to see the full account.</p><p>${es.map((e) => h`${A.date(e.date)} · ${A.user(e.userId).initials} · ${e.hours}h · ${e.narrative}<br>`)}</p>`,
            checks, effect: "billing.issue", payload: { invoiceId: inv.id } });
          S.put("invoices", { id: inv.id, gateId: g.id });
        });
        return { title: `${ms.length} draft bill${ms.length > 1 ? "s" : ""} for approval` };
      } });
  }
  A.effect("billing.issue", (p) => {
    const inv = S.get("invoices", p.invoiceId); if (!inv) return;
    const num = nextNumber();
    S.put("invoices", { id: inv.id, number: num, status: "approved", date: A.today, dueDate: A.addDays(A.today, termsDays()), approvedBy: A.me().id, layoutId: layoutFor(inv.clientId).id });
    (inv.lines || []).forEach((l) => l.entryId && S.put("timeEntries", { id: l.entryId, status: "billed", invoiceId: inv.id }));
    (inv.disb || []).forEach((d) => d.expenseId && S.put("expenses", { id: d.expenseId, status: "billed", invoiceId: inv.id }));
    A.audit("billing.approved", { target: inv.id, note: `Account ${num} approved, ${mName(inv.matterId)}` });
  });

  /* ---------- tables and bulk selection: core App.ui (table select, bulkBar, hints) ---------- */
  /** Table with optional selection column bound to App.selection(opts.list). opts: {list, onRow, pick, empty, active} */
  const ktable = (cols, rows, opts = {}) => A.ui.table(cols, rows, { onRow: opts.onRow, empty: opts.empty, active: opts.active, select: opts.pick ? opts.list : undefined });
  const bulkBar = (name, buttons) => A.ui.bulkBar(name, buttons);
  const hint = (pairs) => A.ui.hints(pairs);
  const idsOf = (el) => (el.dataset.ids || "").split(",").filter(Boolean);
  A.onUser(() => { expSel = null; layoutSel = null; utilSel = null; payInSel = null; });

  /* ---------- time actions ---------- */
  function accept(id) { const e = S.get("timeEntries", id); if (!e || e.userId !== A.me().id) return; S.put("timeEntries", { id, status: "accepted", acceptedAt: A.now() }); A.audit("time.accepted", { target: id, note: `${e.hours}h ${mName(e.matterId)}` }); }
  A.action("time-accept", (el) => { accept(el.dataset.id); A.toast("Entry accepted", "ok"); });
  A.action("time-accept-all", () => { const l = mySuggested(); l.forEach((e) => accept(e.id)); A.toast(`${l.length} entries accepted`, "ok"); });
  function dismiss(id, quiet) { const e = S.get("timeEntries", id); if (!e || e.userId !== A.me().id || e.status !== "suggested") return 0; S.remove("timeEntries", e.id); A.audit("time.dismissed", { target: e.id, note: e.narrative.slice(0, 60) }); if (!quiet) A.toast("Suggestion dismissed"); return 1; }
  A.action("time-dismiss", (el) => dismiss(el.dataset.id));
  A.action("time-bulk-accept", (el) => { const ids = idsOf(el).filter((id) => (S.get("timeEntries", id) || {}).status === "suggested"); ids.forEach(accept); A.selection("sug").clear(); A.toast(`${ids.length} entries accepted`, "ok"); });
  A.action("time-bulk-dismiss", (el) => { const n = idsOf(el).reduce((s, id) => s + dismiss(id, true), 0); A.selection("sug").clear(); A.toast(`${n} suggestions dismissed`); });
  const matterOpts = (sel) => A.matters((m) => m.status === "open").map((m) => h`<option value="${m.id}" ${m.id === sel ? A.raw("selected") : ""}>${m.number} ${m.name}</option>`);
  function editModal(e, isNew) {
    A.modal(isNew ? "New time entry" : "Edit time entry", h`<div class="form">
      <label class="field">Matter<select name="matterId">${matterOpts(e.matterId)}</select></label>
      <label class="field">Date<input name="date" type="date" value="${e.date}"></label>
      <label class="field">Hours<input name="hours" type="number" step="${tset().inc === 15 ? 0.25 : tset().inc === 1 ? 0.01 : 0.1}" min="0.01" value="${e.hours}"></label>
      <label class="field" style="grid-column:1/-1">Narrative<textarea name="narrative" id="tb-narr" rows="3">${e.narrative}</textarea></label></div>
      <div class="row wrap"><span class="muted small">Snippets</span>${snippets().map((x) => h`<button class="btn small" data-a="time-snip" data-q="${x}">${x}</button>`)}</div>
      <p class="muted small">Rate ${cad(rate(A.me().id, e.date))}/h, rounded ${tset().mode === "nearest" ? "to the nearest" : "up to"} ${incLabel()}. ${tset().style || ""}</p>`,
    [{ label: "Cancel" }, { label: isNew ? "Save" : e.status === "suggested" ? "Save and accept" : "Save", primary: true, run: (d) => {
      const v = A.formValues(d); const hours = parseFloat(v.hours) > 0 ? roundH(parseFloat(v.hours) * 60) : 0;
      if (!(hours > 0) || !v.narrative.trim() || !v.matterId) { A.toast("Matter, hours and narrative are required", "bad"); return false; }
      const o = S.put("timeEntries", { ...e, matterId: v.matterId, date: v.date || A.today, hours, narrative: v.narrative.trim(), status: "accepted", userId: A.me().id, rate: e.rate || rate(A.me().id, v.date || A.today) });
      const was = isNew ? "" : ` (was ${(+e.hours).toFixed(1)}h${e.matterId !== o.matterId ? ` on ${mName(e.matterId)}` : ""})`;
      A.audit(isNew ? "time.recorded" : e.status === "suggested" ? "time.edited_accepted" : "time.edited", { target: o.id, note: `${hours.toFixed(1)}h ${mName(o.matterId)}${was}` });
    } }]);
  }
  const SNIPPETS = ["Reviewed and considered", "Telephone call with client re", "Email to opposing counsel re", "Drafted", "Revised", "Prepared for", "Attended", "Reported to client re"];
  A.action("time-snip", (el) => { const t = document.getElementById("tb-narr"); if (!t) return; t.value = (t.value.trim() ? t.value.trim().replace(/[;.]?$/, "; ") : "") + el.dataset.q + " "; t.focus(); });
  A.action("time-activity", (el) => {
    const e = S.get("timeEntries", el.dataset.id); if (!e) return; const rows = activityFor(e);
    A.modal(`Activity behind this entry`, h`<p class="muted small">${A.date(e.date)} · ${mName(e.matterId)} · ${e.hours.toFixed(1)}h suggested from ${rows.reduce((s, a) => s + a.mins, 0)} min of activity</p>
      ${A.ui.table([{ label: "Time", key: "at" }, { label: "Kind", render: (a) => A.ui.badge(KIND[a.kind]) }, { label: "What", render: (a) => h`${a.what}${a.ref && docName(a.ref) ? h` ${A.ui.cite({ doc: a.ref, page: 1, label: docName(a.ref) })}` : ""}` }, { label: "Min", num: true, key: "mins" }], rows)}
      <p class="muted small">Narrative: ${e.narrative}</p>`);
  });
  function draftFromActivity(day) {
    const me = A.me().id, rows = S.where("activityLog", (a) => a.userId === me && a.date === day && a.matterId && unlinked(a) && A.canMatter(S.get("matters", a.matterId)));
    if (!rows.length) return A.toast("All billable activity for this day is already in a suggestion");
    A.agent.run({ title: `Draft time from activity, ${A.date(day)}`, steps: ["Reading the activity log", "Grouping by matter", "Writing narratives in the firm's style", "Checking for duplicates"], done() {
      const by = {}; rows.forEach((a) => (by[a.matterId] = by[a.matterId] || []).push(a));
      Object.entries(by).forEach(([mid, as]) => {
        const mins = as.reduce((s, a) => s + a.mins, 0), hours = Math.max(0.1, Math.ceil(mins / 6) / 10);
        const narr = as.map((a) => a.narr || a.what.replace(/^(Read|Sent|Opened|Edited): ?/i, (m) => ({ "read:": "Reviewed", "sent:": "Email to", "opened:": "Reviewed", "edited:": "Revised" }[m.trim().toLowerCase()] || m))).join("; ");
        const te = S.put("timeEntries", T(A.id("te"), me, mid, day, hours, narr.charAt(0).toUpperCase() + narr.slice(1), "suggested", { kind: as[0].kind, ref: as.find((a) => a.ref) ? as.find((a) => a.ref).ref : as[0].what }));
        as.forEach((a) => S.put("activityLog", { id: a.id, entryId: te.id }));
      });
      return { title: `${Object.keys(by).length} suggestion(s) from ${rows.length} activities` };
    } });
  }
  let actDay = null;
  A.action("time-al-exclude", (el) => { const a = S.get("activityLog", el.dataset.id); if (!a || a.userId !== A.me().id) return; S.put("activityLog", { id: a.id, excluded: true }); A.audit("time.activity.excluded", { target: a.id }); });
  A.action("time-al-include", (el) => { const a = S.get("activityLog", el.dataset.id); if (!a || a.userId !== A.me().id) return; S.put("activityLog", { id: a.id, excluded: false }); });
  A.change("time-al-assign", (el) => { const a = S.get("activityLog", el.dataset.id); if (!a || a.userId !== A.me().id || !el.value) return; S.put("activityLog", { id: a.id, matterId: el.value }); A.toast(`Assigned to ${mName(el.value)}`, "ok"); });
  A.action("time-capture", () => { const me = A.me().id, c = S.get("capture", me) || { id: me }; S.put("capture", { id: me, paused: !c.paused }); A.audit(c.paused ? "time.capture.resumed" : "time.capture.paused", { target: me }); A.toast(c.paused ? "Capture resumed" : "Capture paused; nothing new is logged"); });
  A.action("time-actday", (el) => { actDay = el.dataset.id; A.refresh(); });
  A.action("time-draft-activity", (el) => draftFromActivity(el.dataset.id));
  function activityCard(uid) {
    const all = S.where("activityLog", (a) => a.userId === uid), days = [...new Set(all.map((a) => a.date))].sort().reverse();
    if (!days.length) return h`<div class="card"><h2>Activity log</h2>${A.ui.empty("No activity captured", " Email, documents, calls and calendar appear here as you work.")}</div>`;
    const day = days.includes(actDay) ? actDay : days[0];
    const rows = all.filter((a) => a.date === day).sort((a, b) => a.at.localeCompare(b.at)), loose = rows.filter((a) => a.matterId && unlinked(a));
    const paused = (S.get("capture", uid) || {}).paused;
    return h`<div class="card"><div class="row wrap"><h2 class="grow">Activity log <span class="muted small">${paused ? "capture paused" : "captured passively; only you and the agent see it"}</span></h2>
        <button class="btn small" data-a="time-capture" title="Pause or resume passive capture">${paused ? "Resume capture" : "Pause capture"}</button>
        ${loose.length ? h`<button class="btn agent" data-a="time-draft-activity" data-id="${day}">✦ Draft entries from ${loose.length} unlinked</button>` : ""}</div>
      ${A.ui.tabs(days.map((d) => ({ id: d, label: A.date(d), count: all.filter((a) => a.date === d && a.matterId && unlinked(a)).length })), day, "time-actday")}
      ${A.ui.table([{ label: "Time", key: "at" }, { label: "Kind", render: (a) => A.ui.badge(KIND[a.kind]) }, { label: "Matter", render: (a) => (a.matterId ? mName(a.matterId) : a.excluded ? h`<span class="muted">Not billable</span>` : h`<select data-c="time-al-assign" data-id="${a.id}" aria-label="Assign to matter"><option value="">Assign to matter…</option>${matterOpts()}</select>`) },
        { label: "What", render: (a) => h`${a.what}${a.ref && docName(a.ref) ? h` ${A.ui.cite({ doc: a.ref, page: 1, label: docName(a.ref) })}` : ""}` }, { label: "Min", num: true, key: "mins" },
        { label: "Entry", render: (a) => { const e = a.entryId && S.get("timeEntries", a.entryId); return a.excluded ? h`${A.ui.status("dismissed", "Excluded")} <button class="btn small" data-a="time-al-include" data-id="${a.id}">Include</button>` : e ? h`${statusBadge(e.status)} <span class="muted small">${e.hours.toFixed(1)}h</span>` : h`${a.matterId ? A.ui.status("unconfirmed", "Unlinked") : ""} <button class="btn small" data-a="time-al-exclude" data-id="${a.id}" title="Personal or not work; never suggested">Exclude</button>`; } }], rows)}</div>`;
  }
  function editEntry(id) { const e = S.get("timeEntries", id); if (!e || e.userId !== A.me().id) return; if (e.status === "billed" || onDraft(e.id)) return A.toast("This entry is on a bill; change it in pre-bill review", "bad"); if (lockedEntry(e)) return A.toast(`Time on or before ${A.date(lockedEntry(e))} is locked`, "bad"); editModal(e); }
  function newEntry(matterId) { if (!A.can("time.record", "act")) return; editModal({ id: A.id("te"), matterId: matterId || (timer && timer.matterId) || (A.matters((m) => m.status === "open")[0] || {}).id, date: A.today, hours: 0.5, narrative: "", rate: rate(A.me().id), source: null }, true); }
  A.action("time-edit", (el) => editEntry(el.dataset.id));
  A.action("time-new", (el) => newEntry(el.dataset.matter));

  /* timer: one interval at most; updates the clock text without re-rendering */
  let timer = null, tick = null;
  const fmt = (ms) => { const s = Math.floor(ms / 1000); return `${String(Math.floor(s / 3600)).padStart(2, "0")}:${String(Math.floor((s % 3600) / 60)).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`; };
  function ensureTick() {
    if (tick || !timer) return;
    tick = setInterval(() => { if (!timer) { clearInterval(tick); tick = null; return; } const c = document.getElementById("tb-clock"); if (c) c.textContent = fmt(Date.now() - timer.start); }, 1000);
  }
  A.action("time-start", () => {
    if (timer || !A.can("time.record", "act")) return;
    const sel = document.getElementById("tb-tmatter");
    timer = { start: Date.now(), matterId: (sel && sel.value) || (A.matters((m) => m.status === "open")[0] || {}).id, userId: A.me().id };
    ensureTick(); A.refresh();
  });
  A.action("time-stop", () => {
    if (!timer) return; const t = timer; timer = null; if (tick) { clearInterval(tick); tick = null; }
    const hours = roundH((Date.now() - t.start) / 60000);
    A.refresh();
    if (t.userId !== A.me().id) return;
    editModal({ id: A.id("te"), matterId: t.matterId, date: A.today, hours, narrative: "", rate: rate(t.userId), source: null }, true);
  });

  /* ---------- time page ---------- */
  function weekDays() { const dow = new Date(A.today + "T12:00:00").getDay(); const mon = A.addDays(A.today, -((dow + 6) % 7)); return [0, 1, 2, 3, 4, 5, 6].map((i) => A.addDays(mon, i)); }
  function weekGrid(uid) {
    const days = weekDays(), es = S.where("timeEntries", (e) => e.userId === uid && e.status !== "suggested" && days.includes(e.date));
    const mids = [...new Set(es.map((e) => e.matterId))];
    const sum = (f) => sumH(es.filter(f));
    const dn = (d) => new Date(d + "T12:00:00").toLocaleDateString("en-CA", { weekday: "short", day: "numeric" });
    if (!mids.length) return A.ui.empty("No time this week", " Accept suggestions or start the timer.");
    return h`<div class="tblwrap"><table class="tbl"><thead><tr><th>Matter</th>${days.map((d) => h`<th class="num">${dn(d)}</th>`)}<th class="num">Total</th></tr></thead><tbody>
      ${mids.map((mid) => h`<tr><td>${mName(mid)}</td>${days.map((d) => { const v = sum((e) => e.matterId === mid && e.date === d); return h`<td class="num">${v ? v.toFixed(1) : ""}</td>`; })}<td class="num"><b>${sum((e) => e.matterId === mid).toFixed(1)}</b></td></tr>`)}
      <tr><td><b>Total</b></td>${days.map((d) => h`<td class="num"><b>${sum((e) => e.date === d) ? sum((e) => e.date === d).toFixed(1) : ""}</b></td>`)}<td class="num"><b>${sum(() => true).toFixed(1)}</b></td></tr></tbody></table></div>`;
  }
  function timerCard() {
    const mine = timer && timer.userId === A.me().id;
    return h`<div class="row wrap">${mine ? h`<b id="tb-clock" style="font-size:18px;font-variant-numeric:tabular-nums">${fmt(Date.now() - timer.start)}</b><span class="muted">${mName(timer.matterId)}</span><button class="btn primary" data-a="time-stop">Stop and record</button>`
        : h`<select id="tb-tmatter" class="input" style="max-width:300px" aria-label="Matter for timer">${matterOpts()}</select><button class="btn" data-a="time-start" title="Start a timer on this matter">▶ Start timer</button>`}</div>`;
  }
  function todayBar(uid) {
    const done = sumH(S.where("timeEntries", (e) => e.userId === uid && e.date === A.today && e.status !== "suggested"));
    const waiting = sumH(S.where("timeEntries", (e) => e.userId === uid && e.date === A.today && e.status === "suggested")), tgt = target(uid);
    const pctDone = tgt ? Math.min(100, Math.round((done / tgt) * 100)) : 0, pctWait = tgt ? Math.min(100 - pctDone, Math.round((waiting / tgt) * 100)) : 0;
    return h`<div style="min-width:220px"><div class="small"><b>${done.toFixed(1)} h</b> recorded today${waiting ? h` · <span style="color:var(--accent)">${waiting.toFixed(1)} h suggested</span>` : ""}${tgt ? ` · target ${tgt.toFixed(1)} h` : ""}</div>
      ${tgt ? h`<div class="bar" style="display:flex;height:6px;border-radius:3px;background:var(--soft);overflow:hidden;margin-top:4px"><i style="display:block;width:${pctDone}%;background:var(--ok)"></i><i style="display:block;width:${pctWait}%;background:var(--accent);opacity:.5"></i></div>` : ""}</div>`;
  }
  let timeFilter = "";
  A.change("time-filter", (el) => { timeFilter = el.value; A.refresh(); });
  const wkKey = (uid) => `${uid}:${weekDays()[0]}`;
  function timePage(el) {
    A.setContext({ kind: "time", label: "Time" });
    const me = A.me(), sug = mySuggested().sort((a, b) => b.date.localeCompare(a.date)), canRec = A.can("time.record", "act");
    if (timer && timer.userId === me.id) ensureTick();
    const q = timeFilter.trim().toLowerCase();
    const recent = entriesVisible((e) => e.status !== "suggested" && (!q || `${e.narrative} ${mName(e.matterId)} ${A.user(e.userId).name}`.toLowerCase().includes(q))).sort((a, b) => b.date.localeCompare(a.date)).slice(0, 40);
    const days = weekDays();
    const team = viewAll() ? Object.keys(RATES).map((uid) => { const es = S.where("timeEntries", (e) => e.userId === uid && e.status !== "suggested" && days.includes(e.date)); return { id: uid, hours: sumH(es), value: sumAmt(es), waiting: S.where("timeEntries", (e) => e.userId === uid && e.status === "suggested").length, wk: S.get("timesheets", wkKey(uid)) }; }) : [];
    const navBtns = timeNav();
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Time", meta: canRec ? todayBar(me.id) : "", actions: canRec ? [timerCard()] : [], primary: canRec ? { label: "Add entry", a: "time-new", key: "n" } : null,
        sub: `${canRec ? `Your rate ${cad(rate(me.id))}/h, rounded to ${incLabel()}. ` : ""}The agent rebuilds each day from email, documents, calls and calendar. Nothing is recorded until the timekeeper accepts it.` })}
      ${navBtns}
      ${canRec ? h`<div class="card"><div class="row"><h2 class="grow">✦ Suggested by agent <span class="muted small">${sug.length} waiting · ${sumH(sug).toFixed(1)} h · ${cad(sumAmt(sug))}</span></h2>${A.ui.actor("agent", "Agent drafted")} ${A.ui.actor("person", "You accept")}${sug.length > 1 ? A.ui.btn({ label: `Accept all ${sug.length}`, a: "time-accept-all", primary: true, small: true }) : ""}</div>
        ${bulkBar("sug", [{ label: "Accept", a: "time-bulk-accept", primary: true }, { label: "Dismiss", a: "time-bulk-dismiss" }])}
        ${ktable([
          { label: "Date", render: (e) => A.date(e.date) }, { label: "Matter", render: (e) => mName(e.matterId) }, { label: "Narrative", key: "narrative" },
          { label: "Hours", num: true, render: (e) => e.hours.toFixed(1) }, { label: "Value", num: true, render: (e) => cad(amt(e)) }, { label: "From", render: srcCell }, { label: "Flags", render: flagCell },
          { label: "", render: (e) => h`<div class="row">${A.ui.btn({ label: "Accept", a: "time-accept", id: e.id, small: true, primary: true, key: "a", hint: false })}${A.ui.btn({ label: "Edit", a: "time-edit", id: e.id, small: true, key: "e", hint: false })}${A.ui.btn({ label: "Dismiss", a: "time-dismiss", id: e.id, small: true, key: "d", hint: false })}</div>` },
        ], sug, { list: "sug", pick: true, onRow: "time-open", empty: "Nothing waiting. Today's activity is all accounted for." })}
        ${sug.length ? hint([["j", "next"], ["k", "previous"], ["a", "accept"], ["e", "edit"], ["d", "dismiss"], ["x", "select"], ["Enter", "open"], ["n", "new entry"]]) : ""}</div>
      <div class="grid g2" style="margin-top:14px"><div class="card"><h2>My week</h2>${weekGrid(me.id)}</div>${activityCard(me.id)}</div>` : ""}
      ${team.length ? h`<div class="card" style="margin-top:14px"><h2>Team this week <span class="muted small">week of ${A.date(days[0])}</span></h2>${A.ui.table([{ label: "Timekeeper", render: (r) => h`${A.ui.avatar(r.id)} ${A.user(r.id).name}` }, { label: "Rate", num: true, render: (r) => cad(rate(r.id)) }, { label: "Hours", num: true, render: (r) => r.hours.toFixed(1) }, { label: "Value", num: true, render: (r) => cad(r.value) }, { label: "Suggestions waiting", num: true, key: "waiting" },
        { label: "Timesheet", render: (r) => tsCell(r) }], team)}</div>` : ""}
      <div class="card" style="margin-top:14px"><h2>${viewAll() ? "All entries" : "My entries"}</h2>${A.ui.filters({ search: { c: "time-filter", value: timeFilter, placeholder: "Filter by matter, person, words" } })}${entryTable(recent, "recent")}</div></div>`);
  }
  function tsCell(r) {
    const w = r.wk, canAp = A.can("time.approve", "approve") && r.id !== A.me().id;
    if (w && w.status === "approved") return h`${A.ui.status("approved")} <span class="muted small">${A.user(w.by).initials} ${w.at}</span>`;
    return h`${w && w.status === "returned" ? h`${A.ui.status("returned")} ` : ""}${canAp && r.hours ? h`<button class="btn small" data-a="time-wk-approve" data-id="${r.id}">Approve week</button> <button class="btn small" data-a="time-wk-return" data-id="${r.id}">Return</button>` : r.id === A.me().id ? h`<span class="muted small">Another supervisor approves your week</span>` : ""}`;
  }
  A.action("time-wk-approve", (el) => {
    const uid = el.dataset.id; if (!A.can("time.approve", "approve") || uid === A.me().id) return A.toast("You cannot approve your own timesheet", "bad");
    S.put("timesheets", { id: wkKey(uid), userId: uid, week: weekDays()[0], status: "approved", by: A.me().id, at: A.now() });
    A.audit("time.week.approved", { target: wkKey(uid), note: `${A.user(uid).name}, week of ${A.date(weekDays()[0])}` }); A.toast(`Week approved for ${A.user(uid).name}`, "ok");
  });
  A.action("time-wk-return", (el) => {
    const uid = el.dataset.id; if (!A.can("time.approve", "approve") || uid === A.me().id) return;
    A.modal(`Return ${A.user(uid).name}'s week`, h`<label class="field">What needs fixing?<textarea id="tb-wknote" rows="3" class="input"></textarea></label>`, [{ label: "Cancel" }, { label: "Return", primary: true, run: () => {
      const note = (document.getElementById("tb-wknote") || {}).value || "";
      S.put("timesheets", { id: wkKey(uid), userId: uid, week: weekDays()[0], status: "returned", by: A.me().id, at: A.now(), note });
      A.audit("time.week.returned", { target: wkKey(uid), note: note || A.user(uid).name }); A.toast("Returned to timekeeper");
    } }]);
  });
  A.action("time-open", (el) => A.go(`time/e/${el.dataset.id}`));
  const entryTable = (rows, list) => ktable([
    { label: "Date", render: (e) => A.date(e.date) }, { label: "Who", render: (e) => A.ui.avatar(e.userId) }, { label: "Matter", render: (e) => mName(e.matterId) },
    { label: "Narrative", key: "narrative" }, { label: "Hours", num: true, render: (e) => e.hours.toFixed(1) }, { label: "Rate", num: true, render: (e) => cad(e.rate) },
    { label: "Value", num: true, render: (e) => cad(amt(e)) }, { label: "Status", render: (e) => h`${statusBadge(e.status)} ${flagCell(e)}` },
  ], rows, { list, onRow: "time-open", empty: "No entries" });

  /* ---------- time entry detail ---------- */
  function entryPage(el, p) {
    const e = S.get("timeEntries", p.id), m = e && S.get("matters", e.matterId);
    if (!e || !(e.userId === A.me().id || (viewAll() && A.canMatter(m)))) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>This entry is not available to you.</div></div>`);
    A.setContext({ kind: "time", id: e.id, label: `Time entry ${A.date(e.date)}` });
    const mine = e.userId === A.me().id, inv = e.invoiceId ? S.get("invoices", e.invoiceId) : S.where("invoices", (i) => (i.lines || []).some((l) => l.entryId === e.id))[0];
    const editable = mine && e.status !== "billed" && !onDraft(e.id), hist = S.where("audit", (a) => a.target === e.id);
    const rateNote = e.rate === rate(e.userId, e.date) ? `Standard rate for ${A.user(e.userId).name}` : `Differs from the standard rate (${cad(rate(e.userId, e.date))}); check the engagement letter`;
    const locked = lockedEntry(e);
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Time entry", status: statusBadge(e.status), back: { label: "Time", to: "time" }, meta: e.source ? A.ui.actor("agent", "Agent suggested") : "",
        actions: editable && !locked ? [{ label: "Edit", a: "time-edit", id: e.id, key: "e" }, { label: "Delete", a: "time-delete", id: e.id, danger: true }] : [],
        primary: mine && e.status === "suggested" ? { label: "Accept", a: "time-accept", id: e.id, key: "a" } : null, sub: locked ? `Locked: entries dated on or before ${A.date(locked)} cannot change (Time settings).` : "" })}
      <div class="grid g2"><div class="card"><dl class="kv"><dt>Matter</dt><dd>${mName(e.matterId)}</dd><dt>Timekeeper</dt><dd>${A.user(e.userId).name}</dd><dt>Date</dt><dd>${A.date(e.date)}</dd>
          <dt>Hours</dt><dd>${e.hours.toFixed(1)} h <span class="muted small">(0.1 h increments)</span></dd><dt>Rate</dt><dd>${cad(e.rate)}/h <span class="muted small">${rateNote}</span></dd>
          <dt>Value</dt><dd><b>${cad(amt(e))}</b></dd><dt>Narrative</dt><dd>${e.narrative}</dd><dt>Source</dt><dd>${srcCell(e)}</dd>
          <dt>Bill</dt><dd>${inv && visM(S.get("matters", inv.matterId)) ? h`<button class="btn small" data-a="go" data-to="billing/${inv.id}">${inv.number ? `Account ${inv.number}` : "Draft account"}</button> ${statusBadge(inv.status)}` : e.status === "accepted" ? "Unbilled; goes on the next bill for this matter" : "Not billable until accepted"}</dd></dl>
          ${flags(e).length ? h`<ul class="checks" style="margin-top:10px">${flags(e).map((f) => h`<li class="bad">✕ ${f.label}: ${f.why}</li>`)}</ul>` : ""}</div>
        <div class="card"><h2>History</h2>${hist.length ? h`<ul class="small" style="margin:0;padding-left:18px">${hist.map((a) => h`<li>${a.at} · ${A.user(a.actor).name} · ${a.action.replace(/^time\./, "")}${a.note ? ` · ${a.note}` : ""}</li>`)}</ul>` : h`<p class="muted small">${e.source ? "Suggested by the agent from the activity log." : "Recorded by hand."} No changes since.</p>`}</div></div></div>`);
  }
  A.action("time-delete", (el) => {
    const e = S.get("timeEntries", el.dataset.id); if (!e || e.userId !== A.me().id || e.status === "billed" || onDraft(e.id) || lockedEntry(e)) return A.toast("Billed, drafted or locked entries cannot be deleted", "bad");
    A.modal("Delete time entry", h`<p>${e.hours.toFixed(1)} h on ${mName(e.matterId)}, ${A.date(e.date)}. This cannot be undone.</p>`, [{ label: "Cancel" }, { label: "Delete", primary: true, run: () => {
      S.remove("timeEntries", e.id); A.audit("time.deleted", { target: e.id, note: `${e.hours}h ${mName(e.matterId)}` }); A.toast("Entry deleted"); if (A.route() !== "time") A.go("time");
    } }]);
  });

  /* ---------- billing page ---------- */
  function wipRows() {
    return A.matters((m) => m.fee === "hourly", "billing.view").map((m) => { const es = unbilled(m.id); return { id: m.id, m, hours: sumH(es), value: sumAmt(es), n: es.length, drafted: es.filter((e) => onDraft(e.id)).length }; }).filter((r) => r.n);
  }
  function agingRows() {
    const by = {}; openInv().forEach((i) => { const k = i.clientId; const b = (by[k] = by[k] || { id: k, name: A.contact(k).name, "0-30": 0, "31-60": 0, "61-90": 0, "90+": 0, total: 0 }); const bal = toC(totals(i).balance); b[bucket(age(i))] += bal; b.total += bal; });
    Object.values(by).forEach((b) => { BUCKETS.concat("total").forEach((k) => (b[k] = toD(b[k]))); });
    return Object.values(by).sort((a, b) => b.total - a.total);
  }
  const invCols = [
    { label: "No.", render: (i) => i.number || A.ui.status("draft") }, { label: "Matter", render: (i) => mName(i.matterId) }, { label: "Client", render: (i) => A.contact(i.clientId).name },
    { label: "Date", render: (i) => A.date(i.date) }, { label: "Total", num: true, render: (i) => cad(totals(i).total) }, { label: "Balance", num: true, render: (i) => cad(totals(i).balance) },
    { label: "Age", num: true, render: (i) => (i.status === "sent" && totals(i).balance > 0 ? h`${age(i)}d ${age(i) > 60 ? A.ui.status("overdue", bucket(age(i)) + " days") : ""}` : "") },
    { label: "Status", render: (i) => statusBadge(i.status) },
  ];
  /* ---------- payment settings (Canadian card surcharge rules) ---------- */
  const ps = () => S.get("paySettings", "ps") || {};
  const surchargeRate = (p = ps()) => (p.surcharge ? Math.min(+p.rate || 0, SURCHARGE_CAP, +p.cost || 0) : 0);
  function surchargeRules(p) {
    const noticeDays = p.notice ? A.daysBetween(p.notice, A.today) : -1;
    return [
      { label: "Credit cards only; never on debit, prepaid cards or wallets", ok: true },
      { label: `Rate ${p.rate}% is within the ${SURCHARGE_CAP}% cap`, ok: +p.rate <= SURCHARGE_CAP },
      { label: `Rate is not above the firm's cost of acceptance (${p.cost}%)`, ok: +p.rate <= +p.cost },
      { label: "Never charged on money paid into trust", ok: true },
      { label: "Not charged to clients billed in Quebec", ok: true },
      { label: "A surcharge-free way to pay is offered (EFT, pre-authorized debit or e-Transfer)", ok: !!(p.eft || p.pad || p.etransfer) },
      { label: p.notice ? `Card networks and acquirer notified ${A.date(p.notice)} (${noticeDays} days ago; 30 needed)` : "Card networks and acquirer not yet notified (30 days before start)", ok: noticeDays >= 30 },
      { label: "Surcharge shown as its own line with the percentage", ok: true },
    ];
  }
  function settingsPanel() {
    const p = ps(), can = A.can("billing.payments", "all"), rules = surchargeRules(p), dis = can ? "" : A.raw(`disabled title="${A.esc(A.roleName(A.me().role))} cannot change payment settings"`);
    const ck = (n, label) => h`<label class="row"><input type="checkbox" name="${n}" ${p[n] ? A.raw("checked") : ""} ${dis}> ${label}</label>`;
    return h`<div class="grid g2" style="margin-top:10px"><div><h3>Ways clients can pay</h3>
        ${ck("card", "Credit and debit card")}${ck("eft", "EFT / bank transfer to the general account")}${ck("pad", "Pre-authorized debit (signed PAD agreement)")}${ck("etransfer", "Interac e-Transfer request")}
        <h3 style="margin-top:12px">Credit card surcharge</h3>${ck("surcharge", "Add a surcharge to credit card payments")}
        <div class="form"><label class="field">Surcharge %<input name="rate" type="number" step="0.1" min="0" value="${p.rate}" ${dis}></label>
          <label class="field">Cost of acceptance %<input name="cost" type="number" step="0.1" min="0" value="${p.cost}" ${dis}></label>
          <label class="field">Networks notified on<input name="notice" type="date" value="${p.notice || ""}" ${dis}></label></div>
        <div class="row" style="margin-top:8px"><button class="btn primary" data-a="billing-settings-save" ${dis}>Save settings</button></div></div>
      <div><h3>Rules checked before a surcharge is applied</h3><ul class="checks">${rules.map((k) => h`<li class="${k.ok ? "ok" : "bad"}">${k.ok ? "✓" : "✕"} ${k.label}</li>`)}</ul>
        <p class="muted small">Applied now: ${p.surcharge ? `${surchargeRate(p)}% on credit cards` : "no surcharge"}. Confirm the Law Society's current position before turning this on (verify).</p></div></div>`;
  }
  A.action("billing-settings-save", (el) => {
    if (!A.can("billing.payments", "all")) return A.toast("You cannot change payment settings", "bad");
    const root = el.closest(".card"), v = A.formValues(root), next = { id: "ps", card: v.card, eft: v.eft, pad: v.pad, etransfer: v.etransfer, surcharge: v.surcharge, rate: +v.rate || 0, cost: +v.cost || 0, notice: v.notice || "" };
    const bad = next.surcharge ? surchargeRules(next).filter((k) => !k.ok) : [];
    if (bad.length) return A.toast(`Surcharge not saved: ${bad[0].label}`, "bad");
    S.put("paySettings", next); A.audit("billing.settings", { target: "ps", note: next.surcharge ? `Card surcharge ${surchargeRate(next)}%` : "No card surcharge" }); A.toast("Payment settings saved", "ok");
  });

  /* ---------- LEDES 1998B ---------- */
  function ledes(inv) {
    const t = totals(inv), m = S.get("matters", inv.matterId), d8 = (x) => String(x || "").replace(/-/g, "");
    const ds = (inv.lines || []).map((l) => l.date).sort(), start = d8(ds[0] || inv.date), end = d8(ds[ds.length - 1] || inv.date);
    const head = "INVOICE_DATE|INVOICE_NUMBER|CLIENT_ID|LAW_FIRM_MATTER_ID|INVOICE_TOTAL|BILLING_START_DATE|BILLING_END_DATE|INVOICE_DESCRIPTION|LINE_ITEM_NUMBER|EXP/FEE/INV_ADJ_TYPE|LINE_ITEM_NUMBER_OF_UNITS|LINE_ITEM_ADJUSTMENT_AMOUNT|LINE_ITEM_TOTAL|LINE_ITEM_DATE|LINE_ITEM_TASK_CODE|LINE_ITEM_EXPENSE_CODE|LINE_ITEM_ACTIVITY_CODE|TIMEKEEPER_ID|LINE_ITEM_DESCRIPTION|LAW_FIRM_ID|LINE_ITEM_UNIT_COST|TIMEKEEPER_NAME|TIMEKEEPER_CLASSIFICATION|CLIENT_MATTER_ID[]";
    const base = [d8(inv.date), inv.number || "DRAFT", inv.clientId, m.number, t.total.toFixed(2), start, end, m.name.replace(/\|/g, " ")];
    const clean = (x) => String(x).replace(/[|\[\]]/g, " ");
    const rows = (inv.lines || []).map((l, i) => [...base, i + 1, "F", l.hours.toFixed(1), "0.00", l.amount.toFixed(2), d8(l.date), "L100", "", "A103", l.userId, clean(l.narrative), "LKLAW-ON", l.rate.toFixed(2), A.user(l.userId).name, A.user(l.userId).title || "", m.number].join("|") + "[]")
      .concat((inv.disb || []).map((x, j) => [...base, (inv.lines || []).length + j + 1, "E", "1", "0.00", x.amount.toFixed(2), d8(inv.date), "", "E107", "", "", clean(x.desc), "LKLAW-ON", x.amount.toFixed(2), "", "", m.number].join("|") + "[]"));
    return ["LEDES1998B[]", head, ...rows].join("\n");
  }
  A.action("billing-ledes", (el) => {
    const inv = S.get("invoices", el.dataset.id); if (!inv || A.me().role === "client") return;
    const txt = ledes(inv); A.audit("billing.ledes", { target: inv.id, note: `LEDES 1998B export ${inv.number || "draft"}` });
    A.toast(`LEDES 1998B file ready: ${(inv.number || "draft")}.txt`, "ok");
    A.modal(`LEDES 1998B · ${inv.number || "draft"}`, h`<p class="muted small">Pipe-delimited e-billing file for the client's billing portal. Taxes are in the invoice total; use 1998BI if the client needs separate tax fields.</p><pre style="white-space:pre;overflow:auto;max-height:340px;font-size:11px">${txt}</pre>`);
  });

  /* ---------- payment requests ---------- */
  A.action("billing-etransfer", (el) => {
    const inv = S.get("invoices", el.dataset.id); if (!inv || !A.can("billing.payments", "all")) return;
    S.put("invoices", { id: inv.id, etRequested: A.now() }); A.audit("billing.etransfer", { target: inv.id, note: `Interac e-Transfer request ${cad(totals(inv).balance)} to ${A.contact(inv.clientId).email || A.contact(inv.clientId).name}` });
    A.toast(`e-Transfer request for ${cad(totals(inv).balance)} sent to ${A.contact(inv.clientId).name}`, "ok");
  });
  A.action("billing-pad", (el) => {
    const inv = S.get("invoices", el.dataset.id); if (!inv || !A.can("billing.payments", "all")) return;
    S.put("invoices", { id: inv.id, padSent: A.now() }); A.audit("billing.pad", { target: inv.id, note: `PAD agreement sent to ${A.contact(inv.clientId).name}` });
    A.toast("Pre-authorized debit agreement sent for signature", "ok");
  });
  function payCard(inv, t) {
    const p = ps(), qc = t.prov === "QC", sr = qc ? 0 : surchargeRate(p), can = A.can("billing.payments", "all");
    return h`<div class="card" style="max-width:860px;margin-top:14px"><h2>How to pay ${cad(t.balance)}</h2><div class="grid g2">
      ${p.card ? h`<div><h3>Credit or debit card</h3><p class="small">${sr ? h`Credit card surcharge ${sr}%: ${cad(toD(surchargeC(t.balance, sr)))} (total ${cad(toD(toC(t.balance) + surchargeC(t.balance, sr)))}). No surcharge on debit.` : qc ? "No surcharge (billed in Quebec)." : "No surcharge."}</p></div>` : ""}
      ${p.eft ? h`<div><h3>EFT / bank transfer</h3><p class="small">General account · Institution 004 · Transit 01234 · Account ••••8820<br>Reference: ${inv.number}. No fee.</p></div>` : ""}
      ${p.pad ? h`<div><h3>Pre-authorized debit</h3><p class="small">${inv.padSent ? `Agreement sent ${inv.padSent}.` : "Client signs a PAD agreement once; we debit on the due date with 10 days' notice."}</p>${can && !inv.padSent ? h`<button class="btn small" data-a="billing-pad" data-id="${inv.id}">Send PAD agreement</button>` : ""}</div>` : ""}
      ${p.etransfer ? h`<div><h3>Interac e-Transfer</h3><p class="small">${inv.etRequested ? `Request sent ${inv.etRequested}.` : "Send a money request to the client's email."}</p>${can ? h`<button class="btn small" data-a="billing-etransfer" data-id="${inv.id}">${inv.etRequested ? "Send again" : "Request e-Transfer"}</button>` : ""}</div>` : ""}
      </div><p class="muted small">Surcharges never apply to money paid into trust.</p></div>`;
  }
  A.change("billing-prov", (el) => { const inv = S.get("invoices", el.dataset.id); if (inv && inv.status === "draft") S.put("invoices", { id: inv.id, province: el.value }); });

  let billTab = null;
  A.action("billing-tab", (el) => { billTab = el.dataset.id; A.refresh(); });
  /* What each person must do next: approve, send, chase, or wait on someone else. */
  function deskItems() {
    const me = A.me().id, out = [];
    invoicesVisible().forEach((i) => {
      const g = i.gateId && S.get("approvals", i.gateId), t = totals(i);
      if (i.status === "draft" && g && g.status === "pending") {
        const c = A.canApprove(g);
        if (c.ok) out.push({ id: i.id, inv: i, what: "Approve and sign", tone: "gate", who: "you", act: A.ui.btn({ label: "Approve", a: "gate-approve", id: g.id, small: true, primary: true, key: "a", hint: false }) });
        else if (g.preparedBy === me) out.push({ id: i.id, inv: i, what: "Waiting for a lawyer to approve", tone: "", who: "other" });
      } else if (i.status === "draft" && g && g.status === "returned" && A.can("billing.draft", "act")) out.push({ id: i.id, inv: i, what: `Sent back: ${g.note || "see note"}`, tone: "warn", who: "you" });
      else if (i.status === "approved" && A.can("billing.draft", "act")) out.push({ id: i.id, inv: i, what: "Approved; send to client", tone: "ok", who: "you", act: A.ui.btn({ label: "Send", a: "billing-send", id: i.id, small: true, primary: true, key: "s", hint: false }) });
      else if (i.status === "sent" && t.balance > 0.005 && age(i) > 30 && A.can("billing.draft", "act")) out.push({ id: i.id, inv: i, what: `${age(i)} days since delivery${lastReminder(i) ? `; last reminder ${lastReminder(i)}` : ""}`, tone: age(i) > 60 ? "bad" : "warn", who: "you", act: h`<button class="btn small" data-a="billing-remind" data-id="${i.id}">${age(i) > 60 ? "Draft firm notice" : "Send reminder"}</button>` });
    });
    const order = { gate: 0, ok: 1, warn: 2, bad: 2, "": 3 };
    return out.sort((a, b) => order[a.tone] - order[b.tone]);
  }
  const lastReminder = (i) => (i.reminders || []).slice(-1).map((r) => A.date(r.date))[0] || "";
  function billingPage(el) {
    A.setContext({ kind: "billing", label: "Billing" });
    const client = A.me().role === "client", canDraft = A.can("billing.draft", "act");
    const wip = client ? [] : wipRows(), inv = invoicesVisible().sort((a, b) => (b.date || "").localeCompare(a.date || "")), open = openInv(), aging = client ? [] : agingRows();
    const ar = sumD(open, (i) => totals(i).balance), over60 = sumD(open.filter((i) => age(i) > 60), (i) => totals(i).balance);
    const drafts = inv.filter((i) => i.status === "draft"), approved = inv.filter((i) => i.status === "approved"), desk = client ? [] : deskItems();
    const pays = S.where("payments", (p) => visM(S.get("matters", p.matterId))).sort((a, b) => b.date.localeCompare(a.date));
    const tabs = client ? [{ id: "all", label: "My accounts", count: inv.length }]
      : [{ id: "desk", label: "Needs you", count: desk.filter((d) => d.who === "you").length }, { id: "wip", label: "Unbilled work", count: wip.length }, { id: "ar", label: "Receivables", count: open.length },
        { id: "all", label: "All accounts" }, { id: "payments", label: "Payments", count: pays.length }, { id: "settings", label: "Payment settings" }];
    const tab = tabs.some((t) => t.id === billTab) ? billTab : tabs[0].id;
    const openInvK = (id) => A.go(`billing/${id}`);
    const canSendAll = approved.length && canDraft;
    const body = tab === "settings" ? settingsPanel()
      : tab === "desk" ? h`${canSendAll && approved.length > 1 ? h`<div class="row" style="margin-bottom:8px"><span class="grow"></span><button class="btn small primary" data-a="billing-send-all">Send all ${approved.length} approved</button></div>` : ""}
          ${ktable([{ label: "Account", render: (d) => d.inv.number || A.ui.status("draft") }, { label: "Matter", render: (d) => mName(d.inv.matterId) }, { label: "Total", num: true, render: (d) => cad(totals(d.inv).total) },
            { label: "Next step", render: (d) => h`${d.who === "you" ? A.ui.status(d.tone === "gate" ? "needs-approval" : d.tone === "ok" ? "approved" : d.tone === "bad" ? "overdue" : d.tone === "warn" ? "due-soon" : "needs-you", d.tone === "gate" ? "Needs approval" : d.tone === "ok" ? "Ready to send" : "Follow up") : A.ui.status("waiting", "Waiting")} <span class="small">${d.what}</span>` },
            { label: "", render: (d) => d.act || "" }], desk, { list: "desk", onRow: "billing-open", empty: "Nothing needs you. Bills, sends and follow-ups will appear here." })}
          ${desk.length ? hint([["j", "next"], ["k", "previous"], ["Enter", "open"], ["a", "approve"], ["s", "send"], ["n", "prepare bills"]]) : ""}`
      : tab === "wip" ? h`${ktable([
          { label: "Matter", render: (r) => h`${r.m.number} ${r.m.name}` }, { label: "Responsible", render: (r) => A.ui.avatar(r.m.responsible) }, { label: "Entries", num: true, render: (r) => h`${r.n}${r.drafted ? h` <span class="muted small">(${r.drafted} on draft)</span>` : ""}` },
          { label: "Hours", num: true, render: (r) => r.hours.toFixed(1) }, { label: "Value", num: true, render: (r) => cad(r.value) },
          { label: "Estimate", num: true, render: (r) => (BUDGET[r.id] ? cad(BUDGET[r.id]) : "") },
          { label: "", render: (r) => h`<div class="row">${canDraft && r.n > r.drafted ? A.ui.btn({ label: "✦ Prepare bill", a: "billing-prepare", small: true, agent: true, data: { matter: r.id } }) : ""}${canDraft ? A.ui.btn({ label: S.get("splits", r.id) ? "Split ✓" : "Split", to: `billing/split/${r.id}`, small: true }) : ""}</div>` }], wip, { list: "wip", empty: "No unbilled time" })}
          ${wip.length ? hint([["j", "next"], ["k", "previous"], ["p", "prepare all bills"]]) : ""}`
      : tab === "ar" ? h`<h3>By client and age since delivery</h3>${A.ui.table([{ label: "Client", key: "name" }].concat(BUCKETS.map((b) => ({ label: b + " days", num: true, render: (r) => (r[b] ? cad(r[b]) : "") }))).concat([{ label: "Total", num: true, render: (r) => h`<b>${cad(r.total)}</b>` }]), aging, { empty: "Nothing outstanding" })}
          <h3 style="margin-top:14px">Open accounts</h3>${ktable(invCols.concat([{ label: "Last reminder", render: (i) => lastReminder(i) }]), open, { list: "ar", onRow: "billing-open", empty: "Nothing outstanding" })}`
      : tab === "payments" ? h`${paymentsInbox()}${A.ui.table([{ label: "Date", render: (p) => A.date(p.date) }, { label: "Account", render: (p) => { const i = S.get("invoices", p.invoiceId); return i ? h`<button class="btn small" data-a="go" data-to="billing/${i.id}">${i.number || "draft"}</button>` : statusBadge("unallocated"); } },
          { label: "Matter", render: (p) => mName(p.matterId) }, { label: "Method", key: "method" }, { label: "Amount", num: true, render: (p) => cad(p.amount) }, { label: "Recorded by", render: (p) => A.ui.avatar(p.by) }], pays, { empty: "No payments recorded" })}`
      : ktable(invCols, inv, { list: "all", onRow: "billing-open", empty: "No accounts" });
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Billing", primary: canDraft ? { label: "✦ Prepare bills", a: "billing-prepare", key: "n", agent: true, disabled: !wip.some((r) => r.n > r.drafted), why: "No accepted unbilled time" } : null,
        sub: client ? "Your accounts with Linda K Law. Amounts in Canadian dollars." : "The agent drafts accounts from accepted time and runs pre-bill checks. A lawyer who did not prepare a bill approves it before it is numbered. Amounts in CAD." })}
      ${client ? "" : billNav()}
      <div class="stats">${client ? "" : A.ui.stat("Unbilled work", cad(sumD(wip, (r) => r.value)), `${wip.length} matters`)}
        ${A.ui.stat(client ? "You owe" : "Receivables", cad(ar), `${open.length} open account${open.length === 1 ? "" : "s"}`)}${A.ui.stat("Over 60 days", cad(over60), "", over60 ? "bad" : "")}
        ${client ? "" : A.ui.stat("Bills awaiting approval", drafts.length, approved.length ? `${approved.length} approved, not sent` : "", drafts.length ? "gate" : "")}</div>
      ${A.ui.filters({ chips: tabs, active: tab, action: "billing-tab" })}<div class="card">${body}</div></div>`);
  }
  function sendInv(id, quiet) {
    const inv = S.get("invoices", id); if (!inv || inv.status !== "approved" || !A.can("billing.draft", "act")) return 0;
    S.put("invoices", { id: inv.id, status: "sent", sentAt: A.today, sentBy: A.me().id });
    A.audit("billing.delivered", { target: inv.id, note: `Account ${inv.number} delivered to ${A.contact(inv.clientId).name} on ${A.today} (Solicitors Act: delivery date starts the one-month and assessment periods)` });
    if (!quiet) A.toast(`Account ${inv.number} sent`, "ok");
    return 1;
  }
  A.action("billing-send-all", () => { const n = invoicesVisible().filter((i) => i.status === "approved").reduce((s, i) => s + sendInv(i.id, true), 0); A.toast(`${n} accounts sent`, "ok"); });
  /* Collections: friendly reminder under the approved template; a firm notice (over 60 days) is a lawyer approval. */
  A.action("billing-remind", (el) => {
    const inv = S.get("invoices", el.dataset.id); if (!inv || !A.can("billing.draft", "act")) return;
    const t = totals(inv), d = age(inv), c = A.contact(inv.clientId);
    if (d <= 60) {
      S.put("invoices", { id: inv.id, reminders: (inv.reminders || []).concat({ date: A.today, kind: "friendly", by: "agent" }) });
      A.audit("billing.reminder", { actor: "agent", target: inv.id, note: `Friendly reminder (approved template) to ${c.name}: ${cad(t.balance)} on ${inv.number}` });
      return A.toast(`Reminder sent to ${c.name}`, "ok");
    }
    if (S.where("approvals", (g) => g.effect === "billing.notice" && g.status === "pending" && g.payload.invoiceId === inv.id).length) return A.toast("A firm notice is already waiting for approval");
    A.gate({ kind: "collections", cap: "billing.approve", matterId: inv.matterId, preparedBy: "agent", title: `Firm notice: account ${inv.number}`,
      summary: `${cad(t.balance)} unpaid ${d} days after delivery on ${A.date(inv.sentAt)}.`,
      body: h`<p>Dear ${c.name},</p><p>Our account ${inv.number} dated ${A.date(inv.date)} for ${cad(t.total)} was delivered on ${A.date(inv.sentAt)}. ${cad(t.balance)} remains unpaid. Please pay within 14 days or contact us to discuss a payment plan. Interest on overdue amounts runs under s. 33 of the Solicitors Act. You may also have this account assessed under the Solicitors Act.</p><p>Linda K Law</p>`,
      checks: [{ label: `Account delivered ${A.date(inv.sentAt)}`, ok: !!inv.sentAt }, { label: "No dispute or payment plan on file", ok: !inv.dispute }], effect: "billing.notice", payload: { invoiceId: inv.id } });
    A.toast("Firm notice drafted for a lawyer's approval");
  });
  A.effect("billing.notice", (p) => { const inv = S.get("invoices", p.invoiceId); if (!inv) return; S.put("invoices", { id: inv.id, reminders: (inv.reminders || []).concat({ date: A.today, kind: "firm", by: A.me().id }) }); A.audit("billing.notice.sent", { target: inv.id, note: `Firm notice on ${inv.number}` }); });
  /* Write-off of an uncollectible balance: G-B5, approved by someone with billing.credit who did not request it. */
  A.action("billing-writeoff", (el) => {
    const inv = S.get("invoices", el.dataset.id); if (!inv || inv.status !== "sent" || !A.can("billing.draft", "act")) return;
    const t = totals(inv);
    A.modal(`Write off balance on ${inv.number}`, h`<p>Balance ${cad(t.balance)}. A write-off reduces receivables; the account stays on file. To reduce fees and reverse tax, issue a credit note instead.</p><label class="field">Reason<textarea id="tb-wo" rows="2" class="input">Uncollectible after reminders</textarea></label>`,
      [{ label: "Cancel" }, { label: "Request write-off", primary: true, run: () => {
        const why = ((document.getElementById("tb-wo") || {}).value || "").trim(); if (!why) { A.toast("Give a reason", "bad"); return false; }
        A.gate({ kind: "write-off", cap: "billing.credit", matterId: inv.matterId, preparedBy: A.me().id, title: `Write off ${cad(t.balance)} on account ${inv.number}`, summary: why,
          checks: [{ label: `Delivered ${A.date(inv.sentAt)}, ${age(inv)} days ago`, ok: true }, { label: "No trust withdrawal pending on this account", ok: !t.trustPending.length }], effect: "billing.writeoff", payload: { invoiceId: inv.id, amount: t.balance } });
        A.toast("Write-off sent for approval");
      } }]);
  });
  A.effect("billing.writeoff", (p) => { const inv = S.get("invoices", p.invoiceId); if (!inv) return; const amtC = Math.min(toC(p.amount), toC(totals(inv).balance)); S.put("invoices", { id: inv.id, writtenOff: toD(toC(inv.writtenOff || 0) + amtC) }); if (totals(S.get("invoices", inv.id)).balance <= 0.005) S.put("invoices", { id: inv.id, status: "written off" }); A.audit("billing.writeoff", { target: inv.id, note: `${cad(toD(amtC))} written off on ${inv.number}` }); });
  A.action("billing-open", (el) => A.go(`billing/${el.dataset.id}`));
  A.action("billing-prepare", (el) => prepareBills(el.dataset.matter));
  A.action("billing-send", (el) => sendInv(el.dataset.id));
  A.action("billing-pay", (el) => {
    const inv = S.get("invoices", el.dataset.id); if (!inv || !A.can("billing.payments", "all")) return;
    const bal = totals(inv).balance;
    A.modal(`Record payment on ${inv.number}`, h`<div class="form"><label class="field">Amount<input name="amount" type="number" step="0.01" min="0.01" value="${bal.toFixed(2)}"></label>
      <label class="field">Date<input name="date" type="date" value="${A.today}"></label><label class="field">Method<select name="method"><option>EFT</option><option>Cheque</option><option>Credit card</option><option>Interac e-Transfer</option></select></label></div>
      <p class="muted small">Payments go to the general account. Money held in trust is applied with "Pay from trust", which needs a signatory.</p>`,
    [{ label: "Cancel" }, { label: "Record", primary: true, run: (d) => {
      const v = A.formValues(d), a = r2(parseFloat(v.amount));
      if (!(a > 0) || a > bal + 0.005) { A.toast(`Enter an amount up to ${cad(bal)}`, "bad"); return false; }
      const p = S.put("payments", { id: A.id("pay"), invoiceId: inv.id, matterId: inv.matterId, date: v.date || A.today, amount: a, method: v.method, by: A.me().id });
      if (a >= bal - 0.005) S.put("invoices", { id: inv.id, status: "paid", paidAt: p.date });
      A.audit("billing.payment", { target: inv.id, note: `${cad(a)} ${v.method} on ${inv.number}` }); A.toast("Payment recorded", "ok");
    } }]);
  });
  A.action("billing-trust", (el) => {
    const inv = S.get("invoices", el.dataset.id); if (!inv || !A.can("trust.request", "act")) return;
    const t = totals(inv);
    A.modal(`Pay ${inv.number} from trust`, h`<div class="form"><label class="field">Amount<input name="amount" type="number" step="0.01" min="0.01" value="${t.balance.toFixed(2)}"></label>
      <label class="field" style="grid-column:1/-1">Reason<input name="reason" value="${`Payment of account ${inv.number} delivered ${A.date(inv.sentAt || inv.date)}`}"></label></div>
      <p class="muted small">Under LSO By-Law 9, trust money may pay fees only after the account is delivered to the client. A trust signatory must authorize the withdrawal.</p>`,
    [{ label: "Cancel" }, { label: "Request withdrawal", primary: true, run: (d) => {
      const v = A.formValues(d), a = r2(parseFloat(v.amount));
      if (!(a > 0) || a > t.balance + 0.005) { A.toast(`Enter an amount up to ${cad(t.balance)}`, "bad"); return false; }
      A.gate({ kind: "trust", cap: "trust.authorize", matterId: inv.matterId, preparedBy: A.me().id, title: `Trust withdrawal ${cad(a)} for account ${inv.number}`,
        summary: `Transfer ${cad(a)} from trust to general to pay account ${inv.number} (${mName(inv.matterId)}).`,
        checks: [{ label: inv.status === "sent" ? `Account delivered ${A.date(inv.sentAt)}` : "Account not yet delivered to client", ok: inv.status === "sent", blocking: true }],
        effect: "trust.withdraw", payload: { matterId: inv.matterId, amount: a, invoiceId: inv.id, reason: v.reason } });
      A.toast("Trust withdrawal sent for authorization");
    } }]);
  });

  /* ---------- pre-bill line edits (draft only; original kept, change logged, gate summary refreshed) ---------- */
  function lineFlags(inv, l) {
    const out = [], n = l.narrative || "";
    if (n.split(";").filter((x) => x.trim()).length >= 3 && l.hours >= 2) out.push({ tone: "warn", label: "Block billing", why: "Several tasks under one time block; split or describe each" });
    if (n.trim().split(/\s+/).length < 5) out.push({ tone: "warn", label: "Vague", why: "Client cannot tell what was done" });
    if (l.rate !== rate(l.userId, l.date)) out.push({ tone: "warn", label: "Rate differs", why: `Standard ${cad(rate(l.userId, l.date))}/h` });
    if ((inv.lines || []).some((o) => o !== l && o.userId === l.userId && o.date === l.date && overlap(o.narrative, n) >= 0.6)) out.push({ tone: "bad", label: "Possible duplicate", why: "Same person, same day, similar narrative" });
    return out;
  }
  function refreshGate(inv) {
    const g = inv.gateId && S.get("approvals", inv.gateId), m = S.get("matters", inv.matterId); if (!g || g.status !== "pending") return;
    const t = totals(inv, true), checks = preBill(m, inv.lines || []);
    S.put("approvals", { id: g.id, checks, summary: `${(inv.lines || []).length} entries, fees ${cad(t.fees)}${t.writeDown ? ` after write-downs of ${cad(t.writeDown)}` : ""}, total with tax ${cad(t.total)}. ${checks.filter((c) => !c.ok).length} check(s) to review.` });
  }
  function lineModal(invId, n) {
    const inv = S.get("invoices", invId), l = inv && (inv.lines || [])[n];
    if (!l || inv.status !== "draft" || !A.can("billing.draft", "act")) return;
    A.modal("Edit bill line", h`<div class="form"><label class="field">Hours<input name="hours" type="number" step="0.1" min="0" value="${l.hours.toFixed(1)}"></label>
        <label class="field">Write-down $<input name="wd" type="number" step="0.01" min="0" value="${(l.writeDown || 0).toFixed(2)}"></label>
        <label class="field" style="grid-column:1/-1">Narrative<textarea name="narrative" rows="3">${l.narrative}</textarea></label>
        <label class="field" style="grid-column:1/-1">Reason for change<input name="why" value="${l.writeDownWhy || ""}" placeholder="Required for a write-down"></label></div>
      <p class="muted small">${A.user(l.userId).name} at ${cad(l.rate)}/h. Line amount is hours × rate rounded to the cent; HST is computed once on the bill total.</p>`,
    [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
      const v = A.formValues(d), hrs = tenths(v.hours) / 10, wdC = toC(v.wd), amtC = lineC(hrs, l.rate);
      if (!(hrs >= 0) || !v.narrative.trim()) { A.toast("Hours and narrative are required", "bad"); return false; }
      if (wdC < 0 || wdC > amtC) { A.toast(`Write-down must be between $0.00 and ${cad(toD(amtC))}`, "bad"); return false; }
      if (wdC && !v.why.trim()) { A.toast("Give a reason for the write-down", "bad"); return false; }
      const lines = inv.lines.map((x, i) => (i !== n ? x : { ...x, orig: x.orig || { hours: x.hours, narrative: x.narrative, amount: x.amount }, hours: hrs, narrative: v.narrative.trim(), amount: toD(amtC), writeDown: toD(wdC), writeDownWhy: v.why.trim() }));
      S.put("invoices", { id: inv.id, lines }); refreshGate(S.get("invoices", inv.id));
      A.audit("billing.prebill.edit", { target: inv.id, note: `Line ${n + 1}: ${l.hours.toFixed(1)}h → ${hrs.toFixed(1)}h${wdC ? `, write-down ${cad(toD(wdC))} (${v.why.trim()})` : ""}` });
      A.toast("Line updated", "ok");
    } }]);
  }
  A.action("billing-line", (el) => lineModal(el.dataset.id, +el.dataset.n));

  /* line order under the bill layout; keeps the original index for pre-bill edits */
  const lineOrder = (inv, lay) => { const ls = (inv.lines || []).map((l, n) => [l, n]); return lay.grouping === "by timekeeper" ? ls.sort((a, b) => a[0].userId.localeCompare(b[0].userId) || a[1] - b[1]) : ls; };
  /* ---------- invoice page ---------- */
  function invoicePage(el, p) {
    const inv = S.get("invoices", p.id), m = inv && billMatter(inv.matterId);
    if (!inv || !m || (A.me().role === "client" && inv.status === "draft")) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>This account is not available to you.</div></div>`);
    A.setContext({ kind: "invoice", id: inv.id, label: `Account ${inv.number || "(draft)"}` });
    const t = totals(inv), c = A.contact(inv.clientId), g = inv.gateId && S.get("approvals", inv.gateId), firm = window.SEED.firm;
    const pays = S.where("payments", (x) => x.invoiceId === inv.id), signer = A.user(inv.approvedBy || m.responsible);
    const canPay = A.can("billing.payments", "all") && (inv.status === "sent" || inv.status === "approved") && t.balance > 0.005;
    const canTrust = A.can("trust.request", "act") && inv.status === "sent" && t.balance > 0.005 && !t.trustPending.length;
    const taxable = (inv.disb || []).filter((d) => d.taxable), exempt = (inv.disb || []).filter((d) => !d.taxable);
    const dRows = (l) => l.map((d) => h`<tr><td>${d.desc}</td><td class="num">${cad(d.amount)}</td></tr>`);
    const preEdit = inv.status === "draft" && A.can("billing.draft", "act");
    const canCredit = A.can("billing.draft", "act") && (inv.status === "sent" || inv.status === "paid") && creditRoom(inv) > 0;
    const lay = layoutFor(inv.clientId), colOn = (k) => lay.columns.includes(k);
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: inv.number ? `Account ${inv.number}` : "Draft account", status: statusBadge(inv.status), back: { label: "Billing", to: "billing" },
        actions: [canPay ? { label: "Record payment", a: "billing-pay", id: inv.id } : null, canTrust ? { label: "Pay from trust", a: "billing-trust", id: inv.id } : null,
          canCredit ? { label: "Credit note", a: "billing-credit-new", id: inv.id } : null,
          inv.status === "sent" && t.balance > 0.005 && A.can("billing.draft", "act") ? { label: "Write off balance", a: "billing-writeoff", id: inv.id } : null,
          inv.status === "sent" && t.balance > 0.005 && A.can("billing.draft", "act") ? { label: "Payment plan", a: "billing-plan-new", id: inv.id } : null,
          A.me().role !== "client" ? { label: "Export LEDES 1998B", a: "billing-ledes", id: inv.id } : null].filter(Boolean),
        primary: inv.status === "approved" && A.can("billing.draft", "act") ? { label: "Send to client", a: "billing-send", id: inv.id, key: "s" } : null,
        meta: inv.preparedBy === "agent" ? A.ui.actor("agent", "Agent prepared") : "" })}
      ${preEdit ? h`<div class="card" style="max-width:860px;margin-bottom:14px"><h2>Pre-bill review</h2><ul class="checks">${preBill(m, (inv.lines || []).map((l) => ({ ...l, narrative: l.narrative }))).map((k) => h`<li class="${k.ok ? "ok" : "bad"}">${k.ok ? "✓" : "✕"} ${k.label}</li>`)}</ul><p class="muted small">Changes are logged and the original is kept. Approving the bill numbers and locks it.</p></div>` : ""}
      ${g && g.status !== "approved" && A.me().role !== "client" ? A.ui.gateCard(g) : ""}
      ${t.trustPending.map((x) => A.ui.gateCard(x, { compact: true }))}
      <div class="card" style="max-width:860px">
        <div class="row wrap" style="align-items:flex-start"><div class="grow"><h2 style="font-size:18px">${firm.name}</h2><div class="muted small">Barristers and Solicitors · ${firm.city}<br>${HST_NO}${t.prov === "QC" ? h`<br>${QST_NO}` : ""}</div></div>
          <dl class="kv"><dt>Account no.</dt><dd>${inv.number || "Not issued"}</dd><dt>Date</dt><dd>${A.date(inv.date)}</dd><dt>Due</dt><dd>${inv.dueDate ? A.date(inv.dueDate) : "On approval + 30 days"}</dd>${inv.sentAt ? h`<dt>Delivered</dt><dd>${A.date(inv.sentAt)}</dd>` : ""}</dl></div>
        <dl class="kv" style="margin:14px 0"><dt>To</dt><dd>${c.name}${c.email ? h`<br><span class="muted small">${c.email}</span>` : ""}</dd><dt>Re</dt><dd>${m.number} ${m.name}</dd><dt>Responsible lawyer</dt><dd>${A.user(m.responsible).name}</dd>
          <dt>Place of supply</dt><dd>${inv.status === "draft" && A.can("billing.draft", "act") ? h`<select data-c="billing-prov" data-id="${inv.id}" aria-label="Place of supply">${Object.entries(TAX).map(([k, x]) => h`<option value="${k}" ${k === t.prov ? A.raw("selected") : ""}>${x.name}</option>`)}</select>` : TAX[t.prov].name} <span class="muted small">(client's address)</span></dd></dl>
        <h3>Professional services</h3>
        <div class="tblwrap"><table class="tbl"><thead><tr>${colOn("date") ? h`<th>Date</th>` : ""}${colOn("timekeeper") ? h`<th>By</th>` : ""}<th>Description</th>${colOn("hours") ? h`<th class="num">Hours</th>` : ""}${colOn("rate") ? h`<th class="num">Rate</th>` : ""}<th class="num">Amount</th></tr></thead><tbody>
          ${lineOrder(inv, lay).map(([l, n]) => h`<tr>${colOn("date") ? h`<td>${A.date(l.date)}</td>` : ""}${colOn("timekeeper") ? h`<td>${A.user(l.userId).initials}</td>` : ""}<td>${l.narrative}${l.writeDown ? h`<div class="muted small">Write-down ${cad(l.writeDown)}: ${l.writeDownWhy || ""}</div>` : ""}${preEdit ? h`<div class="row wrap small" style="margin-top:4px">${lineFlags(inv, l).map((f) => h`<span title="${f.why}">${flagChip(f)}</span>`)}<button class="btn small" data-a="billing-line" data-id="${inv.id}" data-n="${n}" title="Edit or write down (e)">Edit / write down</button>${l.orig ? h`<span class="muted">was ${l.orig.hours.toFixed(1)} h</span>` : ""}</div>` : ""}</td>${colOn("hours") ? h`<td class="num">${l.hours.toFixed(1)}</td>` : ""}${colOn("rate") ? h`<td class="num">${cad(l.rate)}</td>` : ""}<td class="num">${cad(l.writeDown ? toD(netC(l)) : l.amount)}</td></tr>`)}
          <tr><td colspan="${2 + ["date", "timekeeper", "hours", "rate"].filter(colOn).length - 1}"><b>Total fees</b>${t.writeDown ? h` <span class="muted small">after write-downs of ${cad(t.writeDown)}</span>` : ""}</td><td class="num"><b>${cad(t.fees)}</b></td></tr></tbody></table></div>
        ${taxable.length ? h`<h3 style="margin-top:14px">Disbursements (taxable)</h3><div class="tblwrap"><table class="tbl"><tbody>${dRows(taxable)}</tbody></table></div>` : ""}
        ${exempt.length ? h`<h3 style="margin-top:14px">Disbursements (not taxable)</h3><div class="tblwrap"><table class="tbl"><tbody>${dRows(exempt)}</tbody></table></div>` : ""}
        <div class="tblwrap" style="margin-top:14px;margin-left:auto;max-width:380px"><table class="tbl"><tbody>
          <tr><td>Fees</td><td class="num">${cad(t.fees)}</td></tr><tr><td>Taxable disbursements</td><td class="num">${cad(t.dt)}</td></tr>
          ${t.taxLines.map((x) => h`<tr><td>${x.label} on ${cad(t.base)}</td><td class="num">${cad(x.amount)}</td></tr>`)}<tr><td>Non-taxable disbursements</td><td class="num">${cad(t.dn)}</td></tr>
          <tr><td><b>Total this account</b></td><td class="num"><b>${cad(t.total)}</b></td></tr>
          <tr><td>Applied from trust</td><td class="num">${cad(-t.trust)}</td></tr>${t.credits.map((c) => h`<tr><td>Credit note ${c.number} (incl. ${c.taxLines.map((l) => l.name).join("/")} reversed)</td><td class="num">${A.cents(-(c.preC + c.taxLines.reduce((s, l) => s + l.amountC, 0)))}</td></tr>`)}${t.writtenOff ? h`<tr><td>Written off</td><td class="num">${cad(-t.writtenOff)}</td></tr>` : ""}
          ${pays.map((x) => h`<tr><td>Paid ${A.date(x.date)} · ${x.method}</td><td class="num">${cad(-x.amount)}</td></tr>`)}
          <tr><td><b>Balance due</b></td><td class="num"><b>${cad(t.balance)}</b></td></tr></tbody></table></div>
        <p class="muted small" style="margin-top:14px">${inv.number ? `This is our account. ${firm.name}, per ${signer.name}.` : "Draft. Not an account until approved and signed by a lawyer."}
          Payment is due within 30 days of delivery. Interest on overdue amounts runs under s. 33 of the Solicitors Act at the post-judgment rate.
          You may have this account assessed by the Superior Court of Justice under the Solicitors Act.${t.prov !== "ON" ? ` Tax charged at ${TAX[t.prov].name} rates (place of supply); provincial rates shown are examples to verify.` : ""}</p></div>
      ${(inv.status === "sent" || inv.status === "approved") && t.balance > 0.005 ? payCard(inv, t) : ""}</div>`);
  }

  /* ---------- firm rules and the screens round 1 deferred ----------
     Rate, settings, layout default, split, plan, credit and ambiguous allocation changes are approvals:
     the preparer never approves, and the agent only drafts. */
  const noAccess = (el, why) => A.render(el, h`<div class="page"><div class="locked"><b>No access</b>${why}</div></div>`);
  const demo = h`<span class="muted small" title="No backend in this prototype">demo</span>`;
  A.action("tb-nav", (el) => A.go(el.dataset.id));
  const navBar = (items) => { const r = A.route(); return h`<div class="fchips" style="margin:0 0 10px">${items.map(([p, l]) => h`<button type="button" class="fchip ${p === r ? "on" : ""}" data-a="tb-nav" data-id="${p}" aria-pressed="${p === r ? "true" : "false"}">${l}</button>`)}</div>`; };
  function timeNav() {
    const ok = { "time/expenses": A.can("time.record", "act") || A.can("billing.draft", "act"), "time/rates": A.can("time.rates"), "time/categories": A.can("time.record", "act"), "time/targets": A.can("time.record", "act") || A.can("time.view_all"), "time/utilization": A.can("reports.firm"), "time/settings": A.can("time.view_all") };
    return navBar([["time", "My time"], ["time/expenses", "Expenses"], ["time/rates", "Rates"], ["time/categories", "Categories"], ["time/targets", "Targets"], ["time/utilization", "Utilization"], ["time/settings", "Settings"]].filter(([p]) => p === "time" || ok[p]));
  }
  const billNav = () => navBar([["billing", "Desk"], ["billing/credits", "Credit notes"], ["billing/plans", "Payment plans"], ["billing/settings", "Bill settings"], ["billing/layouts", "Bill layouts"]]);

  /* ----- time settings: rounding, minimum, lock date, reminders, narrative style ----- */
  A.seed("timeSettings", [{ id: "ts", inc: 6, mode: "up", minEntry: 6, defBillable: true, lock: "2026-09-30", reminder: "17:30", idle: 20, others: "Law clerk may enter time for Linda Kassim",
    style: "Past tense. One task per entry. Say what was done and why. Name the document or person. No block billing." }]);
  const tset = () => S.get("timeSettings", "ts") || { inc: 6, mode: "up" };
  const INC = { 1: "1 min", 6: "6 min (0.1 h)", 15: "15 min (0.25 h)" };
  const incLabel = () => (INC[tset().inc] || INC[6]).replace(/^.*\((.*)\)$/, "$1");
  /** minutes -> hours under the firm rounding rule (hundredths of an hour) */
  function roundH(mins, o = tset()) {
    const i = +o.inc || 6, m = Math.round(Math.max(+o.minEntry || 0, mins) * 1000) / 1000;
    const q = o.mode === "nearest" ? Math.max(1, Math.round(m / i)) : Math.ceil(m / i);
    return Math.round(((q * i) / 60) * 100) / 100;
  }
  const lockedEntry = (e) => { const l = tset().lock; return l && e.date <= l ? l : ""; };
  function roundingImpact(inc) {
    const from = A.addDays(A.today, -30), es = S.where("timeEntries", (e) => e.status !== "suggested" && e.date >= from);
    const dh = es.reduce((s, e) => s + roundH(e.hours * 60, { ...tset(), inc, minEntry: 0 }) - e.hours, 0);
    const dc = es.reduce((s, e) => s + lineC(roundH(e.hours * 60, { ...tset(), inc, minEntry: 0 }), e.rate) - lineC(e.hours, e.rate), 0);
    return { n: es.length, hours: Math.round(dh * 100) / 100, cents: dc };
  }
  function timeSettingsPage(el) {
    A.setContext({ kind: "time", label: "Time settings" });
    if (!A.can("time.view_all")) return noAccess(el, "Time settings are for lawyers and the bookkeeper.");
    const o = tset(), can = A.can("time.approve", "approve"), dis = can ? "" : A.raw("disabled");
    const pend = S.where("approvals", (g) => g.effect === "time.settings.apply" && g.status === "pending");
    const sel = (n, opts) => h`<select name="${n}" ${dis}>${opts.map(([v, l]) => h`<option value="${v}" ${String(o[n]) === String(v) ? A.raw("selected") : ""}>${l}</option>`)}</select>`;
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Time settings", back: { label: "Time", to: "time" }, status: pend.length ? "needs-approval" : "", sub: "Firm rules for capture, rounding and locking. Changes apply to new entries only and need a second lawyer's approval." })}
      ${timeNav()}${pend.map((g) => A.ui.gateCard(g, { compact: true }))}
      <div class="grid g2"><div class="card" id="tb-ts"><h2>Rules</h2><div class="form">
        <label class="field">Rounding increment${sel("inc", Object.entries(INC))}</label><label class="field">Rounding mode${sel("mode", [["up", "Round up"], ["nearest", "Nearest"]])}</label>
        <label class="field">Minimum entry (min)<input name="minEntry" type="number" min="0" step="1" value="${o.minEntry}" ${dis}></label>
        <label class="field">Lock entries dated on or before<input name="lock" type="date" value="${o.lock || ""}" ${dis}></label>
        <label class="field">Daily reminder<input name="reminder" type="time" value="${o.reminder || ""}" ${dis}></label><label class="field">Idle timer alert (min)<input name="idle" type="number" min="0" value="${o.idle}" ${dis}></label>
        <label class="field" style="grid-column:1/-1">Who may enter time for others<input name="others" value="${o.others || ""}" ${dis}></label>
        <label class="field" style="grid-column:1/-1">Narrative style the agent follows<textarea name="style" rows="3" ${dis}>${o.style || ""}</textarea></label></div>
        <div class="row" style="margin-top:8px">${A.ui.btn({ label: "Send changes for approval", a: "time-ts-save", primary: true, disabled: !can, why: "A lawyer proposes; another approves" })}${A.ui.btn({ label: "Unlock a period", a: "time-ts-unlock", disabled: !can || !o.lock, why: "Nothing is locked" })}</div></div>
      <div class="card"><h2>✦ Rounding impact <span class="muted small">last 30 days, accepted time</span></h2>
        ${A.ui.table([{ label: "Increment", render: (r) => h`${INC[r.id]}${+r.id === +o.inc ? h` ${A.ui.status("active", "Current")}` : ""}` }, { label: "Entries", num: true, render: (r) => r.n }, { label: "Hours change", num: true, render: (r) => (r.hours > 0 ? "+" : "") + r.hours.toFixed(2) }, { label: "Value change", num: true, render: (r) => A.cents(r.cents) }],
          Object.keys(INC).map((k) => ({ id: k, ...roundingImpact(+k) })))}
        <p class="muted small">Existing entries keep the rounding they were saved with. The billing engine prices hours to the hundredth, so any increment prices exactly.</p></div></div></div>`);
  }
  A.action("time-ts-save", (el) => {
    if (!A.can("time.approve", "approve")) return A.toast("Only a lawyer can propose time rules", "bad");
    const v = A.formValues(el.closest(".card")), next = { inc: +v.inc || 6, mode: v.mode || "up", minEntry: +v.minEntry || 0, lock: v.lock || "", reminder: v.reminder || "", idle: +v.idle || 0, others: v.others || "", style: v.style || "" };
    const o = tset(), changed = Object.keys(next).filter((k) => String(next[k]) !== String(o[k] ?? ""));
    if (!changed.length) return A.toast("No changes to send");
    const imp = roundingImpact(next.inc);
    A.gate({ kind: "settings", cap: "time.approve", preparedBy: A.me().id, title: `Time settings: ${changed.join(", ")}`, summary: `${changed.map((k) => `${k} ${o[k] ?? "none"} → ${next[k]}`).join("; ")}.${next.inc !== o.inc ? ` Last 30 days under the new rounding: ${imp.hours >= 0 ? "+" : ""}${imp.hours.toFixed(2)} h, ${A.cents(imp.cents)}.` : ""}`,
      checks: [{ label: "Applies to new entries only", ok: true }, { label: next.lock && next.lock > A.today ? "Lock date is in the future" : "Lock date is today or earlier", ok: !next.lock || next.lock <= A.today, blocking: true }], effect: "time.settings.apply", payload: next });
    A.toast("Time settings sent for approval");
  });
  A.effect("time.settings.apply", (p) => { S.put("timeSettings", { id: "ts", ...p }); A.audit("time.settings", { target: "ts", note: `Rounding ${INC[p.inc]}, ${p.mode}; lock ${p.lock || "none"}` }); });
  A.action("time-ts-unlock", () => {
    const o = tset(); if (!A.can("time.approve", "approve") || !o.lock) return;
    A.modal("Unlock a period", h`<div class="form"><label class="field">New lock date<input name="lock" type="date" value="${A.addDays(o.lock, -30)}"></label><label class="field" style="grid-column:1/-1">Reason (audited)<input name="why" value="Correct entries for September month-end"></label></div><p class="muted small">Entries after the new lock date become editable again until it is moved forward.</p>`,
      [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
        const v = A.formValues(d); if (!v.why.trim()) { A.toast("Give a reason", "bad"); return false; }
        A.gate({ kind: "settings", cap: "time.approve", preparedBy: A.me().id, title: `Unlock time from ${A.date(v.lock || o.lock)} to ${A.date(o.lock)}`, summary: v.why.trim(), checks: [{ label: "Reason given", ok: true }], effect: "time.settings.apply", payload: { ...o, lock: v.lock || "" } });
        A.toast("Unlock sent for approval");
      } }]);
  });

  /* ----- rate tables with effective dates ----- */
  let rateTab = "users";
  A.action("time-rate-tab", (el) => { rateTab = el.dataset.id; A.refresh(); });
  function ratesPage(el) {
    A.setContext({ kind: "time", label: "Rates" });
    if (!A.can("time.rates")) return noAccess(el, "Rate tables are visible to lawyers and the bookkeeper.");
    const canAct = A.can("time.rates", "act"), users = Object.keys(RATES).map((id) => ({ id }));
    const pendingRate = (u) => S.where("rates", (r) => r.userId === u && r.status === "pending")[0];
    const flat = S.where("activityCats", (c) => c.kind === "time" && (c.method === "flat" || c.method === "custom"));
    const body = rateTab === "users" ? A.ui.table([{ label: "Timekeeper", render: (u) => h`${A.ui.avatar(u.id)} ${A.user(u.id).name}` }, { label: "Role", render: (u) => A.user(u.id).title }, { label: "Rate", num: true, render: (u) => h`<b>${cad(rate(u.id))}</b>/h` },
        { label: "Since", render: (u) => A.date((rateRows(u.id).find((x) => x.from <= A.today) || {}).from) }, { label: "Pending change", render: (u) => { const p = pendingRate(u.id); return p ? h`${statusBadge("pending")} ${cad(p.rate)} from ${A.date(p.from)}` : ""; } },
        { label: "", render: (u) => (canAct && !pendingRate(u.id) ? A.ui.btn({ label: "Change rate", a: "time-rate-change", id: u.id, small: true }) : "") }], users)
      : rateTab === "activity" ? A.ui.table([{ label: "Activity", key: "name" }, { label: "Method", render: (c) => METHOD[c.method] }, { label: "Amount", num: true, render: (c) => cad(c.amount) }, { label: "UTBMS", key: "code" }], flat, { empty: "No flat or custom activity rates", emptyHint: "Set them in Categories." })
      : A.ui.table([{ label: "Timekeeper", render: (r) => A.user(r.userId).name }, { label: "Rate", num: true, render: (r) => cad(r.rate) }, { label: "Effective from", render: (r) => A.date(r.from) }, { label: "Status", render: (r) => statusBadge(r.status === "pending" ? "pending" : r.from > A.today ? "scheduled" : "active") }],
        S.all("rates").slice().sort((a, b) => b.from.localeCompare(a.from)));
    A.render(el, h`<div class="page">${A.ui.header({ title: "Rate tables", back: { label: "Time", to: "time" }, sub: "Precedence: manual > flat activity > matter > client or named table > activity custom rate > user default. A change never re-prices billed or drafted entries." })}
      ${timeNav()}${A.ui.filters({ chips: [{ id: "users", label: "User rates" }, { id: "activity", label: "Activity rates", count: flat.length }, { id: "history", label: "Rate history" }], active: rateTab, action: "time-rate-tab" })}<div class="card">${body}</div></div>`);
  }
  A.action("time-rate-change", (el) => {
    const uid = el.dataset.id; if (!A.can("time.rates", "act")) return A.toast("Only the managing lawyer proposes rate changes", "bad");
    const cur = rate(uid);
    A.modal(`Change rate: ${A.user(uid).name}`, h`<div class="form"><label class="field">New hourly rate $<input name="rate" type="number" step="5" min="0" value="${cur + 25}"></label><label class="field">Effective from<input name="from" type="date" value="${A.addDays(A.today, 1)}"></label></div><p class="muted small">No retroactive change to billed entries. Unbilled entries dated on or after the effective date are re-rated when a second lawyer approves.</p>`,
      [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
        const v = A.formValues(d), nr = Math.round(+v.rate * 100) / 100, from = v.from || A.today;
        if (!(nr > 0)) { A.toast("Enter a rate", "bad"); return false; }
        if (from < A.today) { A.toast("The effective date cannot be in the past", "bad"); return false; }
        const hit = S.where("timeEntries", (e) => e.userId === uid && e.date >= from && e.status !== "billed" && !onDraft(e.id));
        const r = S.put("rates", { id: A.id("rt"), userId: uid, rate: nr, from, by: A.me().id, status: "pending" });
        A.gate({ kind: "rate", cap: "billing.approve", preparedBy: A.me().id, title: `Rate ${A.user(uid).name}: ${cad(cur)} → ${cad(nr)}/h from ${A.date(from)}`,
          summary: `${hit.length} unbilled entr${hit.length === 1 ? "y" : "ies"} dated on or after ${A.date(from)} would be re-rated. Billed and drafted entries keep their rate.`,
          checks: [{ label: "Effective date is not in the past", ok: from >= A.today, blocking: true }, { label: "Engagement letters allow a rate change on notice (verify per matter)", ok: true }], effect: "time.rate.apply", payload: { rateId: r.id } });
        A.toast("Rate change sent for approval");
      } }]);
  });
  A.effect("time.rate.apply", (p) => {
    const r = S.get("rates", p.rateId); if (!r) return; S.put("rates", { id: r.id, status: "approved", approvedBy: A.me().id });
    S.where("timeEntries", (e) => e.userId === r.userId && e.date >= r.from && e.status !== "billed" && !onDraft(e.id)).forEach((e) => S.put("timeEntries", { id: e.id, rate: r.rate }));
    A.audit("time.rate.changed", { target: r.userId, note: `${cad(r.rate)}/h from ${r.from}` });
  });

  /* ----- activity categories (time and expense) ----- */
  const METHOD = { user: "User rate", custom: "Custom hourly", flat: "Flat amount", none: "No charge" };
  const TAXC = { taxable: "HST taxable", zero: "Zero-rated", exempt: "Exempt" };
  A.seed("activityCats", () => [
    { id: "cat-call", kind: "time", name: "Telephone call with client", method: "user", amount: 0, snippet: "Telephone call with client re", code: "A106", active: true },
    { id: "cat-draft", kind: "time", name: "Drafting", method: "user", amount: 0, snippet: "Drafted", code: "A103", active: true },
    { id: "cat-court", kind: "time", name: "Court attendance", method: "user", amount: 0, snippet: "Attended at", code: "A109", active: true },
    { id: "cat-comm", kind: "time", name: "Commissioning an affidavit", method: "flat", amount: 50, snippet: "Commissioned affidavit of", code: "A101", active: true },
    { id: "cat-int", kind: "time", name: "Internal conference", method: "none", amount: 0, snippet: "Internal conference re", code: "A105", active: true },
    { id: "cat-fee", kind: "expense", name: "Court filing fee", tax: "exempt", agency: "agent", unit: 0, code: "E112", active: true },
    { id: "cat-gov", kind: "expense", name: "Government tax or levy", tax: "exempt", agency: "agent", unit: 0, code: "E112", active: true },
    { id: "cat-courier", kind: "expense", name: "Courier", tax: "taxable", agency: "firm", unit: 0, code: "E107", active: true },
    { id: "cat-proc", kind: "expense", name: "Process server", tax: "taxable", agency: "firm", unit: 0, code: "E113", active: true },
    { id: "cat-transcript", kind: "expense", name: "Transcript or court reporter", tax: "taxable", agency: "firm", unit: 0, code: "E115", active: true },
    { id: "cat-copy", kind: "expense", name: "Photocopies", tax: "taxable", agency: "firm", unit: 0.25, code: "E101", active: true },
    { id: "cat-lrs", kind: "expense", name: "Land registry search", tax: "taxable", agency: "firm", unit: 0, code: "E112", active: true },
  ]);
  const snippets = () => [...new Set(S.where("activityCats", (c) => c.kind === "time" && c.active && c.snippet).map((c) => c.snippet).concat(SNIPPETS))].slice(0, 10);
  const canCats = () => A.can("time.approve", "approve");
  let catTab = "time";
  A.action("time-cat-tab", (el) => { catTab = el.dataset.id; A.refresh(); });
  A.inlineEdit("time-cat-snip", (id, v) => { if (!canCats()) return false; S.put("activityCats", { id, snippet: String(v).trim() }); A.audit("time.category.edited", { target: id, note: "Narrative snippet" }); });
  function categoriesPage(el) {
    A.setContext({ kind: "time", label: "Activity categories" });
    const can = canCats(), rows = S.where("activityCats", (c) => c.kind === catTab), sugg = rows.filter((c) => c.proposed);
    const pend = (c) => S.where("approvals", (g) => g.effect === "time.cat.apply" && g.status === "pending" && g.payload.id === c.id).length;
    const cols = catTab === "time"
      ? [{ label: "Name", render: (c) => h`${c.name}${c.proposed ? h` ${statusBadge("suggested")}` : ""}` }, { label: "Billing", render: (c) => h`${METHOD[c.method]}${c.amount ? ` ${cad(c.amount)}` : ""}${pend(c) ? h` ${statusBadge("pending")}` : ""}` },
        { label: "Default narrative", render: (c) => A.ui.inline({ name: "time-cat-snip", id: c.id, value: c.snippet || "", disabled: !can, why: "A lawyer edits categories" }) }, { label: "UTBMS", key: "code" }]
      : [{ label: "Name", render: (c) => h`${c.name}${c.proposed ? h` ${statusBadge("suggested")}` : ""}` }, { label: "Tax", render: (c) => TAXC[c.tax] }, { label: "Rebilled as", render: (c) => (c.agency === "agent" ? "Agent for client (no HST added)" : "Firm cost (HST on rebill)") },
        { label: "Unit price", num: true, render: (c) => (c.unit ? cad(c.unit) : "") }, { label: "UTBMS", key: "code" }];
    cols.push({ label: "Status", render: (c) => statusBadge(c.active ? "active" : "inactive") }, { label: "", render: (c) => (!can ? "" : c.proposed
      ? h`<div class="row">${A.ui.btn({ label: "Add", a: "time-cat-adopt", id: c.id, small: true, primary: true })}${A.ui.btn({ label: "Dismiss", a: "time-cat-drop", id: c.id, small: true })}</div>`
      : h`<div class="row">${catTab === "time" && !pend(c) ? A.ui.btn({ label: "Billing", a: "time-cat-rate", id: c.id, small: true }) : ""}${A.ui.btn({ label: c.active ? "Deactivate" : "Activate", a: "time-cat-toggle", id: c.id, small: true })}</div>`) });
    A.render(el, h`<div class="page">${A.ui.header({ title: "Activity categories", back: { label: "Time", to: "time" }, actions: can ? [{ label: "✦ Suggest from history", a: "time-cat-suggest", agent: true }] : [], primary: can ? { label: "New category", a: "time-cat-new", key: "n" } : null,
        sub: "Categories set the default narrative, billing method and codes. Rate-bearing changes need a second lawyer; deactivated categories stay on old entries." })}
      ${timeNav()}${A.ui.filters({ chips: [{ id: "time", label: "Time", count: S.where("activityCats", (c) => c.kind === "time").length }, { id: "expense", label: "Expense", count: S.where("activityCats", (c) => c.kind === "expense").length }], active: catTab, action: "time-cat-tab", right: sugg.length ? A.ui.actor("agent", `${sugg.length} proposed`) : "" })}
      <div class="card">${A.ui.table(cols, rows, { empty: "No categories" })}</div></div>`);
  }
  A.action("time-cat-toggle", (el) => { const c = S.get("activityCats", el.dataset.id); if (!c || !canCats()) return; S.put("activityCats", { id: c.id, active: !c.active }); A.audit("time.category.toggled", { target: c.id, note: `${c.name} ${c.active ? "deactivated" : "activated"}` }); });
  A.action("time-cat-adopt", (el) => { const c = S.get("activityCats", el.dataset.id); if (!c || !canCats()) return; S.put("activityCats", { id: c.id, proposed: false, active: true }); A.audit("time.category.added", { target: c.id, note: `${c.name} (agent proposal)` }); A.toast("Category added", "ok"); });
  A.action("time-cat-drop", (el) => { const c = S.get("activityCats", el.dataset.id); if (!c || !canCats() || !c.proposed) return; S.remove("activityCats", c.id); A.toast("Proposal dismissed"); });
  const catGate = (c, method, amount) => A.gate({ kind: "rate", cap: "billing.approve", preparedBy: A.me().id, title: `Category billing: ${c.name} → ${METHOD[method]}${amount ? ` ${cad(amount)}` : ""}`, summary: `Applies to entries recorded from approval. Was ${METHOD[c.method] || "new"}${c.amount ? ` ${cad(c.amount)}` : ""}.`,
    checks: [{ label: method === "flat" || method === "custom" ? "Amount set" : "No amount needed", ok: method === "user" || method === "none" || amount > 0, blocking: true }], effect: "time.cat.apply", payload: { id: c.id, method, amount } });
  A.effect("time.cat.apply", (p) => { S.put("activityCats", { id: p.id, method: p.method, amount: p.amount, active: true }); A.audit("time.category.rate", { target: p.id, note: `${METHOD[p.method]} ${p.amount ? cad(p.amount) : ""}` }); });
  const methodForm = (c) => h`<label class="field">Billing method<select name="method">${Object.entries(METHOD).map(([k, l]) => h`<option value="${k}" ${k === c.method ? A.raw("selected") : ""}>${l}</option>`)}</select></label><label class="field">Rate or flat amount $<input name="amount" type="number" step="5" min="0" value="${c.amount || 0}"></label>`;
  A.action("time-cat-rate", (el) => {
    const c = S.get("activityCats", el.dataset.id); if (!c || !canCats()) return;
    A.modal(`Billing for ${c.name}`, h`<div class="form">${methodForm(c)}</div>`, [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => { const v = A.formValues(d); catGate(c, v.method, Math.round(+v.amount * 100) / 100); A.toast("Sent for approval"); } }]);
  });
  A.action("time-cat-new", () => {
    if (!canCats()) return;
    A.modal(`New ${catTab} category`, h`<div class="form"><label class="field">Name<input name="name" value="${catTab === "time" ? "Reporting letter to client" : "Search: corporate profile report"}"></label><label class="field">UTBMS code<input name="code" value="${catTab === "time" ? "A104" : "E112"}"></label>
        ${catTab === "time" ? h`${methodForm({ method: "user", amount: 0 })}<label class="field" style="grid-column:1/-1">Default narrative<input name="snippet" value="Reported to client re"></label>`
          : h`<label class="field">Tax<select name="tax">${Object.entries(TAXC).map(([k, l]) => h`<option value="${k}">${l}</option>`)}</select></label><label class="field">Rebilled as<select name="agency"><option value="firm">Firm cost (HST on rebill)</option><option value="agent">Agent for client (no HST added)</option></select></label>`}</div>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
        const v = A.formValues(d); if (!v.name.trim()) { A.toast("Name is required", "bad"); return false; }
        const amount = Math.round(+(v.amount || 0) * 100) / 100, rateBearing = catTab === "time" && (v.method === "flat" || v.method === "custom");
        const c = S.put("activityCats", { id: A.id("cat"), kind: catTab, name: v.name.trim(), code: v.code, snippet: v.snippet || "", method: rateBearing ? "user" : v.method || "user", amount: 0, tax: v.tax, agency: v.agency, unit: 0, active: !rateBearing });
        if (rateBearing) { catGate(c, v.method, amount); A.toast("Saved inactive; billing sent for approval"); } else { A.audit("time.category.added", { target: c.id, note: c.name }); A.toast("Category added", "ok"); }
      } }]);
  });
  A.action("time-cat-suggest", () => {
    if (!canCats()) return;
    A.agent.run({ title: "Suggest categories from past narratives", steps: ["Reading accepted narratives", "Clustering similar work", "Checking against existing categories"], done() {
      const have = new Set(S.all("activityCats").map((c) => c.name));
      const props = [{ id: "cat-p-req", kind: "time", name: "Requisitions on title", method: "user", amount: 0, snippet: "Prepared requisition letter re", code: "A103" }, { id: "cat-p-disc", kind: "time", name: "Discovery preparation", method: "user", amount: 0, snippet: "Prepared for examination for discovery of", code: "L330" }]
        .filter((p) => !have.has(p.name) && !S.get("activityCats", p.id));
      props.forEach((p) => S.put("activityCats", { ...p, proposed: true, active: false }));
      return { title: props.length ? `${props.length} categories proposed from ${S.where("timeEntries", (e) => e.status !== "suggested").length} entries` : "No new categories found" };
    } });
  });

  /* ----- timekeeper targets ----- */
  A.seed("targets", () => Object.entries({ "u-linda": [5, 4500000], "u-yoni": [6, 4000000], "u-ayo": [6.5, 3500000], "u-meraf": [5, 1500000], "u-kaleab": [5, 1000000] })
    .map(([id, [dayHrs, monthC]]) => ({ id, dayHrs, monthC, dayLen: 7.5, days: "Mon–Fri", fye: "2026-12-31" })));
  const target = (uid) => (S.get("targets", uid) || {}).dayHrs || 0;
  const canTargets = () => A.can("time.approve", "approve");
  A.inlineEdit("time-tgt-day", (id, v) => { const n = Math.round(+v * 10) / 10; if (!canTargets() || !(n >= 0 && n <= 12)) { A.toast("Enter 0 to 12 hours", "bad"); return false; } S.put("targets", { id, dayHrs: n }); A.audit("time.target", { target: id, note: `${n} h/day` }); });
  A.inlineEdit("time-tgt-month", (id, v) => { const c = toC(v); if (!canTargets() || c < 0) return false; S.put("targets", { id, monthC: c }); A.audit("time.target", { target: id, note: `${A.cents(c)}/month` }); });
  const workdaysSoFar = () => weekDays().filter((d) => d <= A.today && new Date(d + "T12:00:00").getDay() % 6 !== 0).length;
  function paceRow(uid) {
    const t = S.get("targets", uid) || { dayHrs: 0, monthC: 0 }, mStart = A.today.slice(0, 8) + "01";
    const wk = sumH(S.where("timeEntries", (e) => e.userId === uid && e.status !== "suggested" && weekDays().includes(e.date))), need = Math.round(t.dayHrs * workdaysSoFar() * 10) / 10;
    const mtdC = S.where("timeEntries", (e) => e.userId === uid && e.status !== "suggested" && e.date >= mStart).reduce((s, e) => s + lineC(e.hours, e.rate), 0);
    const waiting = sumH(S.where("timeEntries", (e) => e.userId === uid && e.status === "suggested"));
    return { id: uid, t, wk, need, mtdC, waiting, behind: wk < need };
  }
  function targetsPage(el) {
    A.setContext({ kind: "time", label: "Timekeeper targets" });
    const can = canTargets(), me = A.me().id, ids = (A.can("time.view_all") ? Object.keys(RATES) : [me]).filter((u) => RATES[u]);
    if (!ids.length) return noAccess(el, "You do not have a billable target.");
    const rows = ids.map(paceRow);
    A.render(el, h`<div class="page">${A.ui.header({ title: "Timekeeper targets", back: { label: "Time", to: "time" }, actions: [can ? { label: "Copy from last year", a: "time-tgt-copy" } : null, !can && RATES[me] ? { label: "Propose my target", a: "time-tgt-propose" } : null].filter(Boolean),
        sub: `Standard day ${(rows[0].t.dayLen || 7.5).toFixed(1)} h, Monday to Friday; fiscal year ends ${A.date(rows[0].t.fye || "2026-12-31")}. Targets feed the bar on My time.` })}
      ${timeNav()}<div class="card">${A.ui.table([{ label: "Timekeeper", render: (r) => h`${A.ui.avatar(r.id)} ${A.user(r.id).name}` },
        { label: "Billable h/day", num: true, render: (r) => A.ui.inline({ name: "time-tgt-day", id: r.id, value: r.t.dayHrs, type: "number", display: r.t.dayHrs.toFixed(1), disabled: !can, why: "The managing lawyer sets targets" }) },
        { label: "Billed target/month", num: true, render: (r) => A.ui.inline({ name: "time-tgt-month", id: r.id, value: toD(r.t.monthC), type: "money", disabled: !can, why: "The managing lawyer sets targets" }) },
        { label: "This week", num: true, render: (r) => `${r.wk.toFixed(1)} of ${r.need.toFixed(1)} h` }, { label: "Recorded this month", num: true, render: (r) => A.cents(r.mtdC) },
        { label: "Pace", render: (r) => A.ui.status(r.behind ? "overdue" : "done", r.behind ? "Behind pace" : "On pace") },
        { label: "", render: (r) => (r.behind ? A.ui.btn({ label: "✦ Explain", a: "time-tgt-explain", id: r.id, small: true, agent: true }) : "") }], rows)}</div></div>`);
  }
  A.action("time-tgt-copy", () => { if (!canTargets()) return; A.audit("time.target.copied", { target: "targets", note: "Copied 2025 targets; no differences" }); A.toast("2025 targets copied; nothing changed this year"); });
  A.action("time-tgt-explain", (el) => {
    const r = paceRow(el.dataset.id);
    A.agent.run({ title: `Pace note: ${A.user(r.id).name}`, steps: ["Comparing recorded hours with the target", "Looking for unrecorded activity"], done() {
      return { title: `${A.user(r.id).name}: ${r.wk.toFixed(1)} h recorded against ${r.need.toFixed(1)} h due so far this week; ${r.waiting.toFixed(1)} h of suggestions are waiting to be accepted${r.waiting ? ", which would close " + Math.min(100, Math.round(((r.wk + r.waiting) / (r.need || 1)) * 100)) + "% of the gap" : ""}.` };
    } });
  });
  A.action("time-tgt-propose", () => {
    const me = A.me().id, t = S.get("targets", me); if (!t) return;
    A.modal("Propose my target", h`<div class="form"><label class="field">Billable hours per day<input name="h" type="number" step="0.5" min="0" max="12" value="${t.dayHrs}"></label><label class="field" style="grid-column:1/-1">Why<input name="why" value="Reduced schedule during trial preparation"></label></div>`,
      [{ label: "Cancel" }, { label: "Send to managing lawyer", primary: true, run: (d) => { const v = A.formValues(d), n = Math.round(+v.h * 10) / 10;
        A.gate({ kind: "target", cap: "time.approve", preparedBy: me, title: `Target for ${A.user(me).name}: ${t.dayHrs} → ${n} h/day`, summary: v.why, checks: [{ label: "Between 0 and 12 hours", ok: n >= 0 && n <= 12, blocking: true }], effect: "time.target.apply", payload: { id: me, dayHrs: n } });
        A.toast("Proposal sent"); } }]);
  });
  A.effect("time.target.apply", (p) => { S.put("targets", { id: p.id, dayHrs: p.dayHrs }); A.audit("time.target", { target: p.id, note: `${p.dayHrs} h/day (proposed)` }); });

  /* ----- utilization and realization ----- */
  let utilPeriod = "month", utilSel = null;
  A.action("time-util-period", (el) => { utilPeriod = el.dataset.id; A.refresh(); });
  A.action("time-util-open", (el) => { utilSel = utilSel === el.dataset.id ? null : el.dataset.id; A.refresh(); });
  const periodStart = () => (utilPeriod === "week" ? weekDays()[0] : utilPeriod === "month" ? A.today.slice(0, 8) + "01" : "2000-01-01");
  function utilRow(uid) {
    const from = periodStart(), inP = (d) => d >= from && d <= A.today;
    const es = S.where("timeEntries", (e) => e.userId === uid && e.status !== "suggested" && inP(e.date)), hours = sumH(es);
    let days = 0; for (let d = from < "2026-01-01" ? "2026-09-01" : from; d <= A.today; d = A.addDays(d, 1)) if (new Date(d + "T12:00:00").getDay() % 6) days++;
    const cap = target(uid) * days;
    let stdC = 0, billedC = 0, collC = 0;
    S.where("invoices", (i) => i.status !== "draft").forEach((i) => {
      const t = totals(i), ls = (i.lines || []).filter((l) => l.userId === uid && inP(l.date)); if (!ls.length) return;
      const net = ls.reduce((s, l) => s + netC(l), 0), share = toC(t.total) ? (toC(t.total) - toC(t.balance)) / toC(t.total) : 0;
      stdC += ls.reduce((s, l) => s + lineC(l.hours, rate(uid, l.date)), 0); billedC += net; collC += Math.round(net * share);
    });
    const fromAgent = es.filter((e) => e.source), edited = S.where("audit", (a) => a.actor === uid && a.action === "time.edited_accepted").length, dismissed = S.where("audit", (a) => a.actor === uid && a.action === "time.dismissed").length;
    const pctOf = (a, b) => (b ? Math.round((a / b) * 100) : null);
    return { id: uid, hours, cap, util: pctOf(hours, cap), real: pctOf(billedC, stdC), coll: pctOf(collC, billedC), billedC, agentH: sumH(fromAgent), accept: pctOf(fromAgent.length - edited, fromAgent.length + dismissed), edited, dismissed };
  }
  function utilizationPage(el) {
    A.setContext({ kind: "time", label: "Utilization" });
    if (!A.can("reports.firm")) return noAccess(el, "Firm reports are for lawyers and the bookkeeper.");
    const rows = Object.keys(RATES).map(utilRow), pc = (v) => (v == null ? "—" : `${v}%`);
    const tot = (k) => rows.reduce((s, r) => s + r[k], 0), sel = rows.find((r) => r.id === utilSel);
    const byMatter = sel ? [...new Set(S.where("timeEntries", (e) => e.userId === sel.id && e.status !== "suggested" && e.date >= periodStart()).map((e) => e.matterId))].map((mid) => { const es = S.where("timeEntries", (e) => e.userId === sel.id && e.matterId === mid && e.status !== "suggested" && e.date >= periodStart()); return { id: mid, hours: sumH(es), billed: sumH(es.filter((e) => e.status === "billed")), value: sumAmt(es) }; }) : [];
    A.render(el, h`<div class="page">${A.ui.header({ title: "Utilization and realization", back: { label: "Time", to: "time" }, actions: [{ label: "✦ Explain trend", a: "time-util-explain", agent: true }, { label: "Export CSV", a: "time-util-csv" }], sub: "Utilization = billable hours / target capacity. Realization = billed value / standard value. Collection = collected / billed." })}
      ${timeNav()}${A.ui.filters({ chips: [{ id: "week", label: "This week" }, { id: "month", label: "This month" }, { id: "all", label: "All time" }], active: utilPeriod, action: "time-util-period" })}
      <div class="stats">${A.ui.stat("Billable hours", tot("hours").toFixed(1), `capacity ${tot("cap").toFixed(1)} h`)}${A.ui.stat("Utilization", pc(tot("cap") ? Math.round((tot("hours") / tot("cap")) * 100) : null))}${A.ui.stat("Billed", A.cents(tot("billedC")))}${A.ui.stat("Recovered by agent", `${tot("agentH").toFixed(1)} h`, "accepted from suggestions", "")}</div>
      ${A.ui.split(A.ui.table([{ label: "Timekeeper", render: (r) => h`${A.ui.avatar(r.id)} ${A.user(r.id).name}` }, { label: "Hours", num: true, render: (r) => r.hours.toFixed(1) }, { label: "Utilization", num: true, render: (r) => pc(r.util) }, { label: "Realization", num: true, render: (r) => pc(r.real) }, { label: "Collection", num: true, render: (r) => pc(r.coll) }, { label: "Suggestions kept", num: true, render: (r) => pc(r.accept) }], rows, { onRow: "time-util-open", active: utilSel }),
        sel ? h`<div class="card"><h2>${A.user(sel.id).name} by matter</h2><p class="muted small">${sel.agentH.toFixed(1)} h came from agent suggestions; ${sel.edited} edited before accepting, ${sel.dismissed} dismissed.</p>${A.ui.table([{ label: "Matter", render: (r) => mName(r.id) }, { label: "Hours", num: true, render: (r) => r.hours.toFixed(1) }, { label: "Billed h", num: true, render: (r) => r.billed.toFixed(1) }, { label: "Value", num: true, render: (r) => cad(r.value) }], byMatter, { empty: "No time in this period" })}</div>` : null,
        { emptyTitle: "Pick a timekeeper", emptyHint: "See their hours by matter." })}</div>`);
  }
  A.action("time-util-csv", () => {
    const rows = Object.keys(RATES).map(utilRow), csv = ["timekeeper,hours,capacity,utilization_pct,realization_pct,collection_pct,billed_cad,agent_hours"].concat(rows.map((r) => [A.user(r.id).name, r.hours.toFixed(1), r.cap.toFixed(1), r.util ?? "", r.real ?? "", r.coll ?? "", (r.billedC / 100).toFixed(2), r.agentH.toFixed(1)].join(","))).join("\n");
    A.audit("time.report.export", { target: "utilization", note: utilPeriod }); A.modal(`utilization-${utilPeriod}.csv`, h`<pre style="white-space:pre;overflow:auto;font-size:11px">${csv}</pre>`);
  });
  A.action("time-util-explain", () => {
    const rows = Object.keys(RATES).map(utilRow).filter((r) => r.util != null).sort((a, b) => a.util - b.util);
    A.agent.run({ title: "Explain utilization", steps: ["Reading the report figures", "Comparing timekeepers"], done() {
      const lo = rows[0], hi = rows[rows.length - 1];
      return { title: lo ? `${A.user(hi.id).name} leads at ${hi.util}% of capacity; ${A.user(lo.id).name} is lowest at ${lo.util}%. ${rows.reduce((s, r) => s + r.agentH, 0).toFixed(1)} h this period came from accepted agent suggestions.` : "No data in this period" };
    } });
  });

  /* ----- expenses and disbursements ----- */
  const RECEIPT_C = 10000; // receipt required above $100
  const X = (id, matterId, date, catId, vendor, desc, amountC, taxPaidC, agency, paidFrom, by, receipt, status, from) => ({ id, matterId, date, catId, vendor, desc, amountC, taxPaidC, agency, paidFrom, by, receipt, status: status || "unbilled", from: from || null, billable: status !== "non-billable" });
  A.seed("expenses", () => [
    X("ex-1", "m1", "2026-10-03", "cat-courier", "Dynamex Courier", "Courier to Brampton & Vale LLP", 2450, 318, "firm", "operating", "u-meraf", true),
    X("ex-2", "m4", "2026-10-02", "cat-gov", "Minister of Finance", "Estate administration tax deposit filing", 7500, 0, "agent", "operating", "u-meraf", true),
    X("ex-3", "m4", "2026-10-06", "cat-copy", "In-house", "Photocopies, 120 pages at $0.25", 3000, 0, "firm", "operating", "u-meraf", false),
    X("ex-4", "m5", "2026-10-07", "cat-courier", "Dynamex Courier", "Courier: demand letter to Northgate Logistics", 1850, 241, "firm", "card", "u-ayo", true),
    X("ex-5", "m7", "2026-10-05", "cat-transcript", "Victory Verbatim Reporting", "Court reporter, examination for discovery", 41200, 5356, "firm", "operating", "u-ayo", false),
    X("ex-6", "m6", "2026-10-07", "cat-courier", "Dynamex Courier", "Courier: lease documents to landlord", 1600, 208, "firm", "operating", "u-yoni", true, "non-billable"),
    X("ex-s1", "m1", "2026-10-09", "cat-proc", "ProServe Process Servers", "Service of motion record on Northgate Storage Inc.", 9500, 1235, "firm", "card", "u-meraf", true, "suggested", "Email receipt from ProServe, 9 Oct 2026"),
  ]);
  const catName = (id) => (S.get("activityCats", id) || {}).name || "";
  const expVisible = () => S.where("expenses", (x) => A.canMatter(S.get("matters", x.matterId)) && (x.status !== "suggested" || x.by === A.me().id || A.can("billing.draft", "act")));
  const onDraftExp = (xid) => S.where("invoices", (i) => i.status === "draft").some((i) => (i.disb || []).some((d) => d.expenseId === xid));
  const expUnbilled = (mid) => S.where("expenses", (x) => x.matterId === mid && x.status === "unbilled" && !onDraftExp(x.id));
  const expFlags = (x) => [!x.receipt && x.amountC > RECEIPT_C ? { tone: "warn", label: "Receipt needed", why: `Over ${A.cents(RECEIPT_C)}` } : null, x.paidFrom === "trust" && x.status !== "billed" ? { tone: "warn", label: "Trust request", why: "Paid from trust; a signatory authorizes" } : null].filter(Boolean);
  let expFilter = "all", expSel = null, expMatter = "";
  A.action("time-exp-filter", (el) => { expFilter = el.dataset.id; A.refresh(); });
  A.change("time-exp-matter", (el) => { expMatter = el.value; A.refresh(); });
  A.action("time-exp-open", (el) => { expSel = el.dataset.id; A.refresh(); });
  const canExp = () => A.can("time.record", "act") || A.can("billing.draft", "act");
  function expensesPage(el) {
    A.setContext({ kind: "time", label: "Expenses" });
    if (!canExp()) return noAccess(el, "Your role does not record expenses.");
    const all = expVisible(), F = { all: () => true, unbilled: (x) => x.status === "unbilled", unreceipted: (x) => !x.receipt && x.amountC > RECEIPT_C, inbox: (x) => x.status === "suggested", billed: (x) => x.status === "billed" };
    const rows = all.filter(F[expFilter] || F.all).filter((x) => !expMatter || x.matterId === expMatter).sort((a, b) => b.date.localeCompare(a.date)), x = expSel && S.get("expenses", expSel);
    const chips = [["all", "All"], ["inbox", "✦ Receipt inbox"], ["unbilled", "Unbilled"], ["unreceipted", "No receipt"], ["billed", "Billed"]].map(([id, label]) => ({ id, label, count: all.filter(F[id]).length }));
    const list = h`${A.ui.bulkBar("exp", [{ label: "Mark billable", a: "time-exp-bulk", data: { v: "unbilled" } }, { label: "Mark non-billable", a: "time-exp-bulk", data: { v: "non-billable" } }])}
      ${A.ui.table([{ label: "Date", render: (r) => A.date(r.date) }, { label: "Matter", render: (r) => (S.get("matters", r.matterId) || {}).number }, { label: "Description", render: (r) => h`${r.desc}<div class="muted small">${r.vendor} · ${catName(r.catId)}</div>` },
        { label: "Amount", num: true, render: (r) => A.cents(r.amountC) }, { label: "Status", render: (r) => h`${statusBadge(r.status)} ${expFlags(r).map((f) => h`<span title="${f.why}">${flagChip(f)}</span> `)}` }], rows, { onRow: "time-exp-open", select: "exp", active: expSel, empty: "No expenses", emptyHint: "Record a cost paid for a client, or let the agent read receipts." })}`;
    const detail = x && A.canMatter(S.get("matters", x.matterId)) ? h`<div class="card"><div class="row"><h2 class="grow">${x.desc}</h2>${statusBadge(x.status)}</div>
      ${x.from ? h`<p>${A.ui.actor("agent", "Agent read the receipt")} <span class="muted small">${x.from}</span> ${A.ui.actor("person", "You accept")}</p>` : ""}
      <dl class="kv"><dt>Matter</dt><dd>${mName(x.matterId)}</dd><dt>Date</dt><dd>${A.date(x.date)}</dd><dt>Category</dt><dd>${catName(x.catId)}</dd><dt>Vendor</dt><dd>${x.vendor}</dd>
        <dt>Amount</dt><dd><b>${A.cents(x.amountC)}</b> before tax${x.taxPaidC ? ` · HST paid ${A.cents(x.taxPaidC)} (input tax credit)` : ""}</dd>
        <dt>On the bill</dt><dd>${x.agency === "agent" ? "Paid as agent for the client: rebilled at cost, no HST added" : `Firm's own cost: rebilled at ${A.cents(x.amountC)} plus HST on the account`} <span class="muted small">(CRA P-209R; verify per category)</span></dd>
        <dt>Paid from</dt><dd>${{ operating: "General account", trust: "Trust (needs a signatory)", card: "Firm card" }[x.paidFrom]}</dd><dt>Recorded by</dt><dd>${A.user(x.by).name}</dd>
        <dt>Receipt</dt><dd>${x.receipt ? h`${A.ui.status("done", "On file")} ${demo}` : x.amountC > RECEIPT_C ? A.ui.status("unconfirmed", "Required") : "Optional under the firm threshold"}</dd>
        ${x.invoiceId ? h`<dt>Bill</dt><dd>${A.ui.btn({ label: (S.get("invoices", x.invoiceId) || {}).number || "Draft", to: `billing/${x.invoiceId}`, small: true })}</dd>` : ""}</dl>
      <div class="row wrap" style="margin-top:10px">${x.status === "suggested" ? A.ui.btn({ label: "Accept", a: "time-exp-accept", id: x.id, primary: true, small: true }) : ""}
        ${x.status !== "billed" && !onDraftExp(x.id) ? h`${A.ui.btn({ label: "Edit", a: "time-exp-edit", id: x.id, small: true, key: "e" })}${A.ui.btn({ label: x.status === "non-billable" ? "Make billable" : "Non-billable", a: "time-exp-bill", id: x.id, small: true })}` : ""}
        ${!x.receipt ? A.ui.btn({ label: "Attach receipt", a: "time-exp-receipt", id: x.id, small: true }) : ""}${x.status === "suggested" ? A.ui.btn({ label: "Dismiss", a: "time-exp-dismiss", id: x.id, small: true, danger: true }) : ""}</div></div>` : null;
    A.render(el, h`<div class="page">${A.ui.header({ title: "Expenses and disbursements", back: { label: "Time", to: "time" }, actions: [{ label: "✦ Read receipts from email", a: "time-exp-read", agent: true }], primary: { label: "New expense", a: "time-exp-new", key: "n" },
        sub: "Costs paid for clients. Unbilled, billable expenses go on the next bill as disbursements, taxed by how they were paid." })}
      ${timeNav()}${A.ui.filters({ chips, active: expFilter, action: "time-exp-filter", selects: [{ c: "time-exp-matter", value: expMatter, label: "Matter", options: [["", "All matters"]].concat(A.matters((m) => m.status === "open").map((m) => [m.id, `${m.number} ${m.name}`])) }] })}
      ${A.ui.split(list, detail, { emptyTitle: "Pick an expense", wide: true })}</div>`);
  }
  const expMine = (x) => x && (x.by === A.me().id || A.can("billing.draft", "act")) && x.status !== "billed" && !onDraftExp(x.id);
  A.action("time-exp-accept", (el) => { const x = S.get("expenses", el.dataset.id); if (!expMine(x)) return; S.put("expenses", { id: x.id, status: "unbilled" }); A.audit("expense.accepted", { target: x.id, note: `${A.cents(x.amountC)} ${x.vendor}` }); A.toast("Expense accepted", "ok"); });
  A.action("time-exp-dismiss", (el) => { const x = S.get("expenses", el.dataset.id); if (!expMine(x) || x.status !== "suggested") return; S.remove("expenses", x.id); expSel = null; A.toast("Receipt dismissed"); });
  A.action("time-exp-bill", (el) => { const x = S.get("expenses", el.dataset.id); if (!expMine(x)) return; const v = x.status === "non-billable" ? "unbilled" : "non-billable"; S.put("expenses", { id: x.id, status: v, billable: v === "unbilled" }); A.audit("expense.billable", { target: x.id, note: v }); });
  A.action("time-exp-bulk", (el) => { const v = el.dataset.v; let n = 0; idsOf(el).forEach((id) => { const x = S.get("expenses", id); if (expMine(x) && x.status !== "suggested") { S.put("expenses", { id, status: v, billable: v === "unbilled" }); n++; } }); A.selection("exp").clear(); A.toast(`${n} expense${n === 1 ? "" : "s"} updated`, "ok"); });
  A.action("time-exp-receipt", (el) => { const x = S.get("expenses", el.dataset.id); if (!x) return; S.put("expenses", { id: x.id, receipt: true }); A.audit("expense.receipt", { target: x.id }); A.toast("Receipt attached (demo file)", "ok"); });
  function expModal(x, isNew) {
    const cats = S.where("activityCats", (c) => c.kind === "expense" && c.active);
    A.modal(isNew ? "New expense" : "Edit expense", h`<div class="form"><label class="field">Matter<select name="matterId">${matterOpts(x.matterId)}</select></label><label class="field">Date<input name="date" type="date" value="${x.date}"></label>
        <label class="field">Category<select name="catId">${cats.map((c) => h`<option value="${c.id}" ${c.id === x.catId ? A.raw("selected") : ""}>${c.name}</option>`)}</select></label><label class="field">Vendor<input name="vendor" value="${x.vendor}"></label>
        <label class="field">Amount before tax $<input name="amount" type="number" step="0.01" min="0.01" value="${toD(x.amountC).toFixed(2)}"></label><label class="field">HST paid $<input name="tax" type="number" step="0.01" min="0" value="${toD(x.taxPaidC).toFixed(2)}"></label>
        <label class="field">Paid from<select name="paidFrom">${[["operating", "General account"], ["card", "Firm card"], ["trust", "Trust (signatory authorizes)"]].map(([v, l]) => h`<option value="${v}" ${v === x.paidFrom ? A.raw("selected") : ""}>${l}</option>`)}</select></label>
        <label class="field">Rebilled as<select name="agency"><option value="firm" ${x.agency === "firm" ? A.raw("selected") : ""}>Firm cost (HST on rebill)</option><option value="agent" ${x.agency === "agent" ? A.raw("selected") : ""}>Agent for client (no HST added)</option></select></label>
        <label class="field" style="grid-column:1/-1">Description<input name="desc" value="${x.desc}"></label></div><p class="muted small">Receipt required above ${A.cents(RECEIPT_C)}. Paying from trust sends a withdrawal to a trust signatory.</p>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
        const v = A.formValues(d), c = toC(v.amount), tc = toC(v.tax || 0);
        if (!(c > 0) || !v.desc.trim() || !v.vendor.trim() || !v.matterId) { A.toast("Matter, vendor, description and amount are required", "bad"); return false; }
        const o = S.put("expenses", { ...x, matterId: v.matterId, date: v.date || A.today, catId: v.catId, vendor: v.vendor.trim(), desc: v.desc.trim(), amountC: c, taxPaidC: tc, paidFrom: v.paidFrom, agency: v.agency, status: x.status === "suggested" ? "unbilled" : x.status });
        if (v.paidFrom === "trust" && x.paidFrom !== "trust") A.gate({ kind: "trust", cap: "trust.authorize", matterId: o.matterId, preparedBy: A.me().id, title: `Trust payment ${A.cents(c + tc)} to ${o.vendor}`, summary: `${o.desc} (${mName(o.matterId)}). Disbursement paid from the client's trust money.`,
          checks: [{ label: "Receipt or vendor invoice on file", ok: !!o.receipt || c <= RECEIPT_C }], effect: "trust.withdraw", payload: { matterId: o.matterId, amount: toD(c + tc), kind: "disbursement", method: "cheque", payee: o.vendor, purpose: "disbursement", ref: o.id, requestedBy: A.me().id } });
        A.audit(isNew ? "expense.recorded" : "expense.edited", { target: o.id, note: `${A.cents(c)} ${o.vendor} ${mName(o.matterId)}` }); expSel = o.id; A.toast("Expense saved", "ok");
      } }]);
  }
  A.action("time-exp-new", () => { if (!canExp()) return; expModal(X(A.id("ex"), expMatter || (A.matters((m) => m.status === "open")[0] || {}).id, A.today, "cat-courier", "", "", 2000, 260, "firm", "operating", A.me().id, false), true); });
  A.action("time-exp-edit", (el) => { const x = S.get("expenses", el.dataset.id); if (!expMine(x)) return A.toast("Billed or drafted expenses cannot change", "bad"); expModal(x); });
  A.action("time-exp-read", () => {
    A.agent.run({ title: "Read receipts from email and uploads", steps: ["Scanning the receipts mailbox", "Matching vendors and matters", "Drafting expenses"], done() {
      if (S.get("expenses", "ex-s2")) return { title: "No new receipts" };
      S.put("expenses", X("ex-s2", "m2", "2026-10-09", "cat-lrs", "Teraview (land registry)", "Parcel register and instrument copies, 14 Birchmount Rd", 4300, 0, "firm", "card", A.me().id, true, "suggested", "Upload: teraview-receipt-1009.pdf"));
      return { title: "1 expense drafted from a receipt; review it in the receipt inbox" };
    } });
  });

  /* ----- credit notes (HST reversed at the source bill's rate) ----- */
  const creditsFor = (invId, st) => S.where("creditNotes", (c) => c.invoiceId === invId && (!st || c.status === st));
  const creditRoom = (inv) => toC(totals(inv, true).base) - creditsFor(inv.id).filter((c) => c.status !== "returned").reduce((s, c) => s + c.preC, 0);
  /** Per tax line: -rhu(credit pre-tax x line tax / bill taxable base); the credit that exhausts the base takes the exact remaining tax (BIL-T-070). */
  function creditTax(inv, preC) {
    const t = totals(inv, true), baseC = toC(t.base), prior = creditsFor(inv.id, "approved"), priorPre = prior.reduce((s, c) => s + c.preC, 0);
    return t.taxLines.map((x, n) => { const lC = toC(x.amount), priorTax = prior.reduce((s, c) => s + ((c.taxLines[n] || {}).amountC || 0), 0);
      return { name: x.name, label: x.label, amountC: priorPre + preC >= baseC ? lC - priorTax : rhu(preC * lC, baseC) }; });
  }
  const CN_KIND = { reduction: "Fee reduction", error: "Billing error", refund: "Refund" };
  let cnSel = null;
  A.action("billing-cn-open", (el) => { cnSel = el.dataset.id; A.refresh(); });
  function creditsPage(el) {
    A.setContext({ kind: "billing", label: "Credit notes" });
    if (A.me().role === "client") return noAccess(el, "Credit notes appear on your accounts.");
    const cns = S.where("creditNotes", (c) => visM(S.get("matters", c.matterId))).sort((a, b) => b.date.localeCompare(a.date)), x = cnSel && S.get("creditNotes", cnSel);
    const wos = S.where("approvals", (g) => g.effect === "billing.writeoff" && visM(S.get("matters", g.matterId)));
    const total = (c) => c.preC + c.taxLines.reduce((s, l) => s + l.amountC, 0);
    const g = x && x.gateId && S.get("approvals", x.gateId), inv = x && S.get("invoices", x.invoiceId);
    const detail = x && inv ? h`${g && g.status === "pending" ? A.ui.gateCard(g) : ""}<div class="card"><div class="row"><h2 class="grow">${x.number || "Draft credit note"}</h2>${statusBadge(x.status)}</div>
      <dl class="kv"><dt>Against account</dt><dd>${A.ui.btn({ label: inv.number, to: `billing/${inv.id}`, small: true })} ${mName(inv.matterId)}</dd><dt>Kind</dt><dd>${CN_KIND[x.kind]}</dd><dt>Reason</dt><dd>${x.reason}</dd></dl>
      <div class="tblwrap" style="margin-top:10px;max-width:420px"><table class="tbl"><tbody><tr><td>Credit before tax</td><td class="num">${A.cents(-x.preC)}</td></tr>
        ${x.taxLines.map((l) => h`<tr><td>${l.label} reversed</td><td class="num">${A.cents(-l.amountC)}</td></tr>`)}<tr><td><b>Total credit</b></td><td class="num"><b>${A.cents(-total(x))}</b></td></tr></tbody></table></div>
      ${x.trustC ? h`<p class="small">${A.ui.status("unconfirmed", "Refund due")} ${A.cents(x.trustC)} was already paid; refund it to the client${toC(inv.trustApplied || 0) ? ", to trust first" : ""} (G-B6, Trust module).</p>` : ""}
      <p class="muted small">Tax reversed per tax line at the source bill's rate: credit × line tax ÷ taxable base, rounded half away from zero; the credit that uses up the base takes the exact remaining tax.</p></div>` : null;
    A.render(el, h`<div class="page">${A.ui.header({ title: "Credit notes and write-offs", back: { label: "Billing", to: "billing" }, primary: A.can("billing.draft", "act") ? { label: "New credit note", a: "billing-credit-new", key: "n" } : null,
        sub: "A credit note reduces a delivered account and reverses its tax; a write-off clears an uncollectible balance. Both need approval by someone who did not prepare them." })}
      ${billNav()}${A.ui.split(h`${A.ui.list(cns.map((c) => ({ id: c.id, title: `${c.number || "Draft"} · ${(S.get("invoices", c.invoiceId) || {}).number}`, sub: `${CN_KIND[c.kind]} · ${A.date(c.date)}`, right: h` ${A.cents(-total(c))}`, status: c.status === "pending" ? "needs-approval" : c.status, on: c.id === cnSel })), "billing-cn-open", { empty: "No credit notes", emptyHint: "Open a delivered account and choose Credit note." })}
        <h3 style="margin-top:14px">Write-off requests</h3>${A.ui.table([{ label: "Request", key: "title" }, { label: "Status", render: (w) => statusBadge(w.status) }], wos, { empty: "No write-offs" })}`, detail, { emptyTitle: "Pick a credit note" })}</div>`);
  }
  A.action("billing-credit-new", (el) => {
    if (!A.can("billing.draft", "act")) return;
    const elig = invoicesVisible().filter((i) => (i.status === "sent" || i.status === "paid") && creditRoom(i) > 0), pre = S.get("invoices", el.dataset.id) || elig[0];
    if (!pre) return A.toast("No delivered account has room for a credit");
    A.modal("New credit note", h`<div class="form"><label class="field">Account<select name="inv">${elig.map((i) => h`<option value="${i.id}" ${i.id === pre.id ? A.raw("selected") : ""}>${i.number} · ${mName(i.matterId)} · base ${cad(toD(creditRoom(i)))}</option>`)}</select></label>
        <label class="field">Kind<select name="kind">${Object.entries(CN_KIND).map(([k, l]) => h`<option value="${k}">${l}</option>`)}</select></label>
        <label class="field">Credit before tax $<input name="amount" type="number" step="0.01" min="0.01" value="${toD(Math.min(creditRoom(pre), rhu(toC(totals(pre, true).fees), 10))).toFixed(2)}"></label>
        <label class="field" style="grid-column:1/-1">Reason<input name="why" value="Courtesy reduction agreed with client"></label></div><p class="muted small">Tax is reversed per line at the account's rate. A lawyer who did not prepare the credit approves it (G-B5).</p>`,
      [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
        const v = A.formValues(d), inv = S.get("invoices", v.inv), preC = toC(v.amount); if (!inv) return false;
        if (!(preC > 0) || preC > creditRoom(inv)) { A.toast(`Enter up to ${cad(toD(creditRoom(inv)))}`, "bad"); return false; }
        if (!v.why.trim()) { A.toast("Give a reason", "bad"); return false; }
        const tl = creditTax(inv, preC), totC = preC + tl.reduce((s, l) => s + l.amountC, 0), bal = toC(totals(inv).balance);
        const cn = S.put("creditNotes", { id: A.id("cn"), number: "", invoiceId: inv.id, matterId: inv.matterId, date: A.today, kind: v.kind, preC, taxLines: tl, reason: v.why.trim(), trustC: Math.max(0, totC - bal), status: "pending", by: A.me().id });
        const g = A.gate({ kind: "credit", cap: "billing.credit", matterId: inv.matterId, preparedBy: A.me().id, title: `Credit note ${A.cents(totC)} on account ${inv.number}`,
          summary: `${CN_KIND[v.kind]}: ${A.cents(preC)} before tax, ${tl.map((l) => `${l.label} ${A.cents(l.amountC)}`).join(", ")} reversed. ${v.why.trim()}`,
          checks: [{ label: `Account delivered ${A.date(inv.sentAt || inv.date)}`, ok: !!inv.sentAt }, { label: "Within the account's taxable base", ok: preC <= creditRoom(inv) + preC, blocking: true }, { label: totC > bal ? `Exceeds the balance by ${A.cents(totC - bal)}: refund needed` : "No refund needed", ok: totC <= bal }],
          effect: "billing.credit", payload: { creditId: cn.id } });
        S.put("creditNotes", { id: cn.id, gateId: g.id }); cnSel = cn.id; A.toast("Credit note sent for approval");
        if (A.route() !== "billing/credits") A.go("billing/credits");
      } }]);
  });
  A.effect("billing.credit", (p) => {
    const c = S.get("creditNotes", p.creditId), inv = c && S.get("invoices", c.invoiceId); if (!inv) return;
    const n = S.where("creditNotes", (x) => x.number).length + 1, tl = creditTax(inv, c.preC);
    S.put("creditNotes", { id: c.id, number: `CN-2026-${String(n).padStart(4, "0")}`, status: "approved", taxLines: tl, approvedBy: A.me().id });
    A.audit("billing.credit", { target: inv.id, note: `Credit note on ${inv.number}: ${A.cents(c.preC)} + tax ${A.cents(tl.reduce((s, l) => s + l.amountC, 0))}` });
  });

  /* ----- bill settings ----- */
  A.seed("billSettings", [{ id: "bs", format: "YYYY-NNNN", workflow: "single", terms: "net30", interest: "cja", autoTrust: false, delivery: "both", page: "Letter", showTrust: true, showPrev: true, showAging: false, showTk: true, showLink: true }]);
  const bset = () => S.get("billSettings", "bs") || { terms: "net30" };
  const TERMS = { receipt: ["Due on receipt", 0], net15: ["Net 15", 15], net30: ["Net 30", 30] };
  const termsDays = () => (TERMS[bset().terms] || TERMS.net30)[1];
  function billSettingsPage(el) {
    A.setContext({ kind: "billing", label: "Bill settings" });
    if (A.me().role === "client") return noAccess(el, "Firm settings are not shown to clients.");
    const o = bset(), can = A.can("billing.approve", "approve"), dis = can ? "" : A.raw("disabled"), pend = S.where("approvals", (g) => g.effect === "billing.bs.apply" && g.status === "pending");
    const sel = (n, opts) => h`<select name="${n}" ${dis}>${opts.map(([v, l]) => h`<option value="${v}" ${String(o[n]) === v ? A.raw("selected") : ""}>${l}</option>`)}</select>`;
    const ck = (n, l) => h`<label class="row"><input type="checkbox" name="${n}" ${o[n] ? A.raw("checked") : ""} ${dis}> ${l}</label>`;
    A.render(el, h`<div class="page">${A.ui.header({ title: "Bill settings", back: { label: "Billing", to: "billing" }, status: pend.length ? "needs-approval" : "", sub: "Numbering, approval, terms, interest and delivery for every bill. Changes need a second lawyer's approval." })}
      ${billNav()}${pend.map((g) => A.ui.gateCard(g, { compact: true }))}
      <div class="card"><div class="grid g2"><div><h3>Numbering and approval</h3><div class="form"><label class="field">Number format<input name="format" value="${o.format}" ${dis}></label>
          <label class="field">Approval${sel("workflow", [["single", "One lawyer"], ["two", "Responsible lawyer, then billing partner"]])}</label></div>
          <p class="muted small">Numbers are never reused; a voided bill keeps its number.</p>
          <h3>Terms and interest</h3><div class="form"><label class="field">Default terms${sel("terms", Object.entries(TERMS).map(([k, v]) => [k, v[0]]))}</label>
          <label class="field">Interest rate${sel("interest", [["cja", "Courts of Justice Act postjudgment rate (verify quarterly)"], ["fixed", "Fixed rate agreed in the retainer"]])}</label></div></div>
        <div><h3>Trust and delivery</h3>${ck("autoTrust", "Propose applying trust when a bill is approved (still authorized by a signatory)")}
          <div class="form"><label class="field">Delivery${sel("delivery", [["email", "Email PDF"], ["portal", "Client portal"], ["both", "Both"]])}</label><label class="field">Page size${sel("page", [["Letter", "Letter"], ["Legal", "Legal"], ["A4", "A4"]])}</label></div>
          <h3>Show on the bill</h3>${ck("showTrust", "Trust balance")}${ck("showPrev", "Previous balance")}${ck("showAging", "Aging summary")}${ck("showTk", "Timekeeper summary")}${ck("showLink", "Payment link")}</div></div>
        <div class="row" style="margin-top:10px">${A.ui.btn({ label: "Send changes for approval", a: "billing-bs-save", primary: true, disabled: !can, why: "A lawyer proposes; another approves" })}</div></div></div>`);
  }
  A.action("billing-bs-save", (el) => {
    if (!A.can("billing.approve", "approve")) return;
    const v = A.formValues(el.closest(".card")), o = bset(), next = { ...o, ...v }, changed = Object.keys(v).filter((k) => String(v[k]) !== String(o[k]));
    if (!changed.length) return A.toast("No changes to send");
    A.gate({ kind: "settings", cap: "billing.approve", preparedBy: A.me().id, title: `Bill settings: ${changed.join(", ")}`, summary: changed.map((k) => `${k}: ${o[k]} → ${v[k]}`).join("; "), checks: [{ label: "Number format keeps a year and a sequence", ok: /Y/.test(next.format) && /N/.test(next.format), blocking: true }], effect: "billing.bs.apply", payload: next });
    A.toast("Bill settings sent for approval");
  });
  A.effect("billing.bs.apply", (p) => { S.put("billSettings", { ...p, id: "bs" }); A.audit("billing.settings.bills", { target: "bs", note: `Terms ${p.terms}, approval ${p.workflow}` }); });

  /* ----- bill layouts ----- */
  const COLS = { date: "Date", timekeeper: "Timekeeper", narrative: "Narrative", hours: "Hours", rate: "Rate", amount: "Amount" };
  const GROUPS = ["chronological", "by timekeeper", "fees then disbursements"];
  A.seed("billLayouts", [
    { id: "lay-std", name: "Standard", columns: ["date", "timekeeper", "narrative", "hours", "rate", "amount"], grouping: "chronological", summary: true, footer: "Payment is due within 30 days of delivery.", payLink: true, clients: [], isDefault: true },
    { id: "lay-ins", name: "Third-party payer summary", columns: ["date", "narrative", "amount"], grouping: "fees then disbursements", summary: false, footer: "Narratives summarized for a third-party payer.", payLink: false, clients: [] },
  ]);
  const layoutFor = (cid) => S.where("billLayouts", (l) => (l.clients || []).includes(cid))[0] || S.where("billLayouts", (l) => l.isDefault)[0] || { columns: Object.keys(COLS), grouping: "chronological" };
  let layoutSel = null;
  const canLay = () => A.can("billing.approve", "approve") || A.can("billing.draft", "all");
  A.action("billing-lay-open", (el) => { layoutSel = el.dataset.id; A.refresh(); });
  A.change("billing-lay-col", (el) => { const l = S.get("billLayouts", el.dataset.id); if (!l || !canLay()) return; const k = el.dataset.k, cols = l.columns.filter((c) => c !== k).concat(el.checked ? [k] : []); S.put("billLayouts", { id: l.id, columns: Object.keys(COLS).filter((c) => cols.includes(c) || c === "narrative" || c === "amount") }); });
  A.change("billing-lay-group", (el) => { if (canLay()) S.put("billLayouts", { id: el.dataset.id, grouping: el.value }); });
  A.change("billing-lay-footer", (el) => { if (canLay()) S.put("billLayouts", { id: el.dataset.id, footer: el.value }); });
  A.change("billing-lay-client", (el) => {
    const l = S.get("billLayouts", el.dataset.id); if (!l || !canLay() || !el.value) return;
    S.all("billLayouts").forEach((x) => (x.clients || []).includes(el.value) && S.put("billLayouts", { id: x.id, clients: x.clients.filter((c) => c !== el.value) }));
    S.put("billLayouts", { id: l.id, clients: (l.clients || []).concat(el.value) }); A.audit("billing.layout.assigned", { target: l.id, note: A.contact(el.value).name }); A.toast(`${A.contact(el.value).name} now gets "${l.name}"`, "ok");
  });
  A.action("billing-lay-dup", (el) => { const l = S.get("billLayouts", el.dataset.id); if (!l || !canLay()) return; const n = S.put("billLayouts", { ...l, id: A.id("lay"), name: `${l.name} (copy)`, clients: [], isDefault: false }); layoutSel = n.id; A.toast("Layout duplicated", "ok"); });
  A.action("billing-lay-default", (el) => {
    const l = S.get("billLayouts", el.dataset.id); if (!l || !A.can("billing.approve", "approve")) return A.toast("A lawyer sets the firm default", "bad");
    if (S.where("approvals", (g) => g.effect === "billing.layout.default" && g.status === "pending").length) return A.toast("A default change is already waiting");
    A.gate({ kind: "settings", cap: "billing.approve", preparedBy: A.me().id, title: `Firm default bill layout: ${l.name}`, summary: `Columns: ${l.columns.map((c) => COLS[c]).join(", ")}; ${l.grouping}.`, checks: [{ label: "Shows narrative and amount", ok: l.columns.includes("narrative") && l.columns.includes("amount"), blocking: true }], effect: "billing.layout.default", payload: { id: l.id } });
    A.toast("Default change sent for approval");
  });
  A.effect("billing.layout.default", (p) => { S.all("billLayouts").forEach((x) => S.put("billLayouts", { id: x.id, isDefault: x.id === p.id })); A.audit("billing.layout.default", { target: p.id }); });
  function layoutsPage(el) {
    A.setContext({ kind: "billing", label: "Bill layouts" });
    if (A.me().role === "client") return noAccess(el, "Layouts are firm settings.");
    const ls = S.all("billLayouts"), l = S.get("billLayouts", layoutSel) || ls[0], can = canLay(), dis = can ? "" : A.raw("disabled");
    const sample = [{ date: "2026-10-05", userId: "u-linda", hours: 1.2, rate: 450, narrative: "Reviewed notices and drafted request to admit facts" }, { date: "2026-10-06", userId: "u-ayo", hours: 3.5, rate: 300, narrative: "Researched bailee liability and drafted memo" }];
    const ordered = l && l.grouping === "by timekeeper" ? sample.slice().sort((a, b) => a.userId.localeCompare(b.userId)) : sample;
    const cell = { date: (r) => A.date(r.date), timekeeper: (r) => A.user(r.userId).initials, narrative: (r) => r.narrative, hours: (r) => r.hours.toFixed(1), rate: (r) => cad(r.rate), amount: (r) => A.cents(lineC(r.hours, r.rate)) };
    const detail = l ? h`<div class="card"><div class="row"><h2 class="grow">${l.name}</h2>${l.isDefault ? A.ui.status("active", "Firm default") : ""}</div>
      <div class="grid g2"><div><h3>Columns</h3>${Object.entries(COLS).map(([k, lab]) => h`<label class="row"><input type="checkbox" data-c="billing-lay-col" data-id="${l.id}" data-k="${k}" ${l.columns.includes(k) ? A.raw("checked") : ""} ${k === "narrative" || k === "amount" ? A.raw("disabled") : dis}> ${lab}</label>`)}
          <label class="field">Grouping<select data-c="billing-lay-group" data-id="${l.id}" ${dis}>${GROUPS.map((g) => h`<option ${g === l.grouping ? A.raw("selected") : ""}>${g}</option>`)}</select></label>
          <label class="field">Footer<input data-c="billing-lay-footer" data-id="${l.id}" value="${l.footer || ""}" ${dis}></label>
          <label class="field">Assign to client<select data-c="billing-lay-client" data-id="${l.id}" ${dis}><option value="">Choose a client…</option>${[...new Set(A.matters(() => true, "billing.view").map((m) => m.clientId))].map((c) => h`<option value="${c}">${A.contact(c).name}</option>`)}</select></label>
          <p class="muted small">Used by: ${(l.clients || []).map((c) => A.contact(c).name).join(", ") || (l.isDefault ? "every client without a layout" : "nobody yet")}</p>
          <div class="row wrap">${A.ui.btn({ label: "Duplicate", a: "billing-lay-dup", id: l.id, small: true, disabled: !can })}${!l.isDefault ? A.ui.btn({ label: "Make firm default", a: "billing-lay-default", id: l.id, small: true, disabled: !A.can("billing.approve", "approve"), why: "A lawyer sets the default" }) : ""}</div></div>
        <div><h3>Preview <span class="muted small">fictional lines</span></h3>${A.ui.table(l.columns.map((k) => ({ label: COLS[k], num: k === "hours" || k === "rate" || k === "amount", render: cell[k] })), ordered.map((r, i) => ({ ...r, id: String(i) })))}<p class="muted small">${l.footer || ""}</p></div></div></div>` : null;
    A.render(el, h`<div class="page">${A.ui.header({ title: "Bill layouts", back: { label: "Billing", to: "billing" }, sub: "How a bill looks to the client. Accounts use the client's layout, else the firm default." })}
      ${billNav()}${A.ui.split(A.ui.list(ls.map((x) => ({ id: x.id, title: x.name, sub: `${x.columns.length} columns · ${x.grouping}`, status: x.isDefault ? "active" : "", on: l && x.id === l.id })), "billing-lay-open"), detail)}</div>`);
  }

  /* ----- split billing (per matter) ----- */
  const splitDraft = {};
  const splitRows = (m) => splitDraft[m.id] || (splitDraft[m.id] = (S.get("splits", m.id) || { payers: [{ contactId: m.clientId, pct: 100, applies: "all", consent: true }] }).payers.map((p) => ({ ...p })));
  /** Share integer cents by percentage; the rounding remainder goes to the first payer. */
  function shareC(totalC, payers) { const out = payers.map((p) => Math.floor((totalC * (+p.pct || 0)) / 100)); if (out.length) out[0] += totalC - out.reduce((s, x) => s + x, 0); return out; }
  A.change("billing-split-f", (el) => { const m = billMatter(el.dataset.id); if (!m) return; const r = splitRows(m)[+el.dataset.n]; if (!r) return; r[el.dataset.k] = el.type === "checkbox" ? el.checked : el.dataset.k === "pct" ? Math.round(+el.value * 100) / 100 : el.value; A.refresh(); });
  A.action("billing-split-add", (el) => { const m = billMatter(el.dataset.id); if (!m) return; const c = S.all("contacts").find((x) => !/opposing/.test(x.role || "") && !splitRows(m).some((p) => p.contactId === x.id)); splitRows(m).push({ contactId: c ? c.id : m.clientId, pct: 0, applies: "all", consent: false }); A.refresh(); });
  A.action("billing-split-del", (el) => { const m = billMatter(el.dataset.id); if (!m) return; const rows = splitRows(m); if (rows.length > 1) rows.splice(+el.dataset.n, 1); A.refresh(); });
  function splitPage(el, p) {
    const m = billMatter(p.id); if (!m || A.me().role === "client") return noAccess(el, "This matter's billing is not available to you.");
    A.setContext({ kind: "billing", id: m.id, label: `Split billing ${m.number}` });
    const rows = splitRows(m), can = A.can("billing.draft", "act"), saved = S.get("splits", m.id), sum = Math.round(rows.reduce((s, r) => s + (+r.pct || 0), 0) * 100) / 100;
    const feesC = unbilled(m.id).reduce((s, e) => s + lineC(e.hours, e.rate), 0), disbC = expUnbilled(m.id).reduce((s, x) => s + x.amountC, 0);
    const base = (r) => (r.applies === "fees" ? feesC : r.applies === "disb" ? disbC : feesC + disbC), shares = sum === 100 ? shareC(feesC + disbC, rows) : rows.map((r) => Math.floor((base(r) * (+r.pct || 0)) / 100));
    const pend = saved && saved.gateId && S.get("approvals", saved.gateId);
    const people = S.all("contacts").filter((c) => !/opposing/.test(c.role || ""));
    A.render(el, h`<div class="page">${A.ui.header({ title: `Split billing · ${m.number}`, status: !saved ? A.ui.status("draft", "No split") : saved.status === "pending" ? "needs-approval" : "active", back: { label: m.name, to: `matters/${m.id}` },
        primary: can ? { label: "Send split for approval", a: "billing-split-save", id: m.id, disabled: sum !== 100, why: "Shares must add up to 100%" } : null, sub: "Each payer gets their own bill for their share. Trust money of one payer never pays another payer's share." })}
      ${pend && pend.status === "pending" ? A.ui.gateCard(pend, { compact: true }) : ""}
      <div class="card">${A.ui.table([{ label: "Payer", render: (r) => h`<select data-c="billing-split-f" data-id="${m.id}" data-n="${r.n}" data-k="contactId" ${can ? "" : A.raw("disabled")}>${people.map((c) => h`<option value="${c.id}" ${c.id === r.contactId ? A.raw("selected") : ""}>${c.name}</option>`)}</select>${r.contactId !== m.clientId ? h`<div class="muted small">Not the client: confidentiality and conflicts apply; consider a summary-only bill.</div>` : ""}` },
          { label: "Share %", num: true, render: (r) => h`<input type="number" min="0" max="100" step="1" style="width:80px" data-c="billing-split-f" data-id="${m.id}" data-n="${r.n}" data-k="pct" value="${r.pct}" ${can ? "" : A.raw("disabled")}>` },
          { label: "Applies to", render: (r) => h`<select data-c="billing-split-f" data-id="${m.id}" data-n="${r.n}" data-k="applies" ${can ? "" : A.raw("disabled")}>${[["all", "All lines"], ["fees", "Fees only"], ["disb", "Disbursements only"]].map(([v, l]) => h`<option value="${v}" ${v === r.applies ? A.raw("selected") : ""}>${l}</option>`)}</select>` },
          { label: "Written consent", render: (r) => h`<label class="row"><input type="checkbox" data-c="billing-split-f" data-id="${m.id}" data-n="${r.n}" data-k="consent" ${r.consent ? A.raw("checked") : ""} ${can ? "" : A.raw("disabled")}> on file</label>` },
          { label: "Share of next bill", num: true, render: (r) => A.cents(shares[r.n] || 0) },
          { label: "", render: (r) => (can && rows.length > 1 ? A.ui.btn({ label: "Remove", a: "billing-split-del", id: m.id, small: true, data: { n: r.n } }) : "") }], rows.map((r, n) => ({ ...r, n, id: String(n) })))}
        <div class="row wrap" style="margin-top:8px">${can ? A.ui.btn({ label: "Add payer", a: "billing-split-add", id: m.id, small: true }) : ""}<span class="grow"></span>
          ${A.ui.status(sum === 100 ? "done" : "unbalanced", `Total ${sum}%`)} <span class="muted small">Next bill ${A.cents(feesC + disbC)} (fees ${A.cents(feesC)}, disbursements ${A.cents(disbC)}); rounding cents go to the first payer.</span></div></div></div>`);
  }
  A.action("billing-split-save", (el) => {
    const m = billMatter(el.dataset.id); if (!m || !A.can("billing.draft", "act")) return;
    const rows = splitRows(m).map((r) => ({ ...r })), sum = Math.round(rows.reduce((s, r) => s + (+r.pct || 0), 0) * 100) / 100;
    if (sum !== 100) return A.toast("Shares must add up to 100%", "bad");
    const g = A.gate({ kind: "split", cap: "billing.approve", matterId: m.id, preparedBy: A.me().id, title: `Split billing on ${m.number}`, summary: rows.map((r) => `${A.contact(r.contactId).name} ${r.pct}% (${r.applies})`).join("; "),
      checks: [{ label: "Shares add up to 100%", ok: sum === 100, blocking: true }, { label: "Every payer's written consent is on file", ok: rows.every((r) => r.consent), blocking: true }, { label: "Each payer has a billing contact", ok: rows.every((r) => r.contactId), blocking: true }],
      effect: "billing.split.apply", payload: { matterId: m.id, payers: rows } });
    S.put("splits", { id: m.id, payers: (S.get("splits", m.id) || {}).payers || rows, status: "pending", gateId: g.id, from: A.today }); A.toast("Split sent for approval");
  });
  A.effect("billing.split.apply", (p) => { S.put("splits", { id: p.matterId, payers: p.payers, status: "active", from: A.today }); delete splitDraft[p.matterId]; A.audit("billing.split", { target: p.matterId, note: p.payers.map((r) => `${r.pct}%`).join("/") }); });

  /* ----- payment plans and autopay ----- */
  const FREQ = { weekly: ["Weekly", 7], biweekly: ["Every 2 weeks", 14], monthly: ["Monthly", 0] };
  const nextDate = (d, f) => { if (FREQ[f][1]) return A.addDays(d, FREQ[f][1]); const [y, mo, da] = d.split("-").map(Number), n = new Date(Date.UTC(y, mo, Math.min(da, 28))); return n.toISOString().slice(0, 10); };
  function schedule(p) { const out = []; let left = p.coveredC, d = p.start; while (left > 0 && out.length < 60) { const a = Math.min(p.instC, left); out.push({ date: d, amountC: a, paid: (p.charges || []).some((c) => c.date === d) }); left -= a; d = nextDate(d, p.freq); } return out; }
  A.seed("payPlans", () => [{ id: "pp-1", invoiceId: "inv-107", matterId: "m7", clientId: "c-selam", coveredC: toC(totals(SEED_INV[0], true).total), freq: "monthly", instC: 50000, start: "2026-09-15", auth: "PAD agreement signed 10 Sep 2026", autopay: false, trigger: "due", status: "active", charges: [{ date: "2026-09-15", amountC: 50000 }], by: "u-yohannes" }]);
  let planSel = null;
  A.action("billing-plan-open", (el) => { planSel = el.dataset.id; A.refresh(); });
  const canPlans = () => A.can("billing.draft", "act") || A.can("billing.payments", "all");
  function plansPage(el) {
    A.setContext({ kind: "billing", label: "Payment plans" });
    const ps = S.where("payPlans", (p) => visM(S.get("matters", p.matterId))), p = S.get("payPlans", planSel) || ps[0], client = A.me().role === "client";
    const next = (x) => schedule(x).find((s) => !s.paid);
    const g = p && p.gateId && S.get("approvals", p.gateId);
    const detail = p && visM(S.get("matters", p.matterId)) ? h`${g && g.status === "pending" && !client ? A.ui.gateCard(g, { compact: true }) : ""}<div class="card"><div class="row"><h2 class="grow">${A.contact(p.clientId).name} · account ${(S.get("invoices", p.invoiceId) || {}).number}</h2>${statusBadge(p.status)}</div>
      <dl class="kv"><dt>Covers</dt><dd>${A.cents(p.coveredC)}</dd><dt>Instalments</dt><dd>${A.cents(p.instC)} ${FREQ[p.freq][0].toLowerCase()} from ${A.date(p.start)}</dd><dt>Authorization</dt><dd>${p.auth} ${demo}</dd>
        <dt>Autopay new bills</dt><dd>${p.autopay ? `Yes, ${p.trigger === "due" ? "on the due date" : "10 days after delivery"}; never before delivery` : "No"}</dd><dt>Pays into</dt><dd>General account only, never trust</dd></dl>
      ${A.ui.table([{ label: "Date", render: (s) => A.date(s.date) }, { label: "Amount", num: true, render: (s) => A.cents(s.amountC) }, { label: "Status", render: (s) => (s.paid ? A.ui.status("paid") : s.date <= A.today ? A.ui.status("due-soon", "Due") : A.ui.status("scheduled")) }], schedule(p).map((s, i) => ({ ...s, id: String(i) })))}
      ${!client && canPlans() ? h`<div class="row wrap" style="margin-top:8px">${p.status === "active" ? h`${A.ui.btn({ label: "Run due charges", a: "billing-plan-run", id: p.id, small: true, primary: true })} ${demo}${A.ui.btn({ label: "Pause", a: "billing-plan-status", id: p.id, small: true, data: { v: "paused" } })}` : p.status === "paused" ? A.ui.btn({ label: "Resume", a: "billing-plan-status", id: p.id, small: true, data: { v: "active" } }) : ""}
        ${p.status === "active" || p.status === "paused" ? A.ui.btn({ label: "Cancel plan", a: "billing-plan-status", id: p.id, small: true, danger: true, data: { v: "cancelled" } }) : ""}</div>` : ""}</div>` : null;
    A.render(el, h`<div class="page">${A.ui.header({ title: "Payment plans and autopay", back: { label: "Billing", to: "billing" }, primary: !client && canPlans() ? { label: "New plan", a: "billing-plan-new", key: "n" } : null,
        sub: "Instalments on delivered accounts, charged from a method the processor stores (we never store card or bank numbers). Plans pay the general account, never trust." })}
      ${client ? "" : billNav()}${A.ui.split(A.ui.list(ps.map((x) => { const n = next(x); return { id: x.id, title: A.contact(x.clientId).name, sub: n ? `Next ${A.cents(n.amountC)} on ${A.date(n.date)}` : "Fully paid", status: x.status === "pending" ? "needs-approval" : x.status === "paused" ? "hold" : x.status, on: p && x.id === p.id }; }), "billing-plan-open", { empty: "No payment plans", emptyHint: "Offer one from an overdue account." }), detail)}</div>`);
  }
  A.action("billing-plan-new", (el) => {
    if (!canPlans()) return;
    const elig = openInv().filter((i) => i.status === "sent" && !S.where("payPlans", (p) => p.invoiceId === i.id && p.status !== "cancelled").length), pre = S.get("invoices", el.dataset.id) || elig[0];
    if (!pre || !elig.some((i) => i.id === pre.id)) return A.toast("No delivered account without a plan");
    A.modal("New payment plan", h`<div class="form"><label class="field">Account<select name="inv">${elig.map((i) => h`<option value="${i.id}" ${i.id === pre.id ? A.raw("selected") : ""}>${i.number} · ${A.contact(i.clientId).name} · ${cad(totals(i).balance)}</option>`)}</select></label>
        <label class="field">Frequency<select name="freq">${Object.entries(FREQ).map(([k, v]) => h`<option value="${k}" ${k === "monthly" ? A.raw("selected") : ""}>${v[0]}</option>`)}</select></label>
        <label class="field">Instalment $<input name="inst" type="number" step="0.01" min="1" value="${(Math.ceil(totals(pre).balance / 3 * 100) / 100).toFixed(2)}"></label><label class="field">First charge<input name="start" type="date" value="${A.addDays(A.today, 7)}"></label>
        <label class="row" style="grid-column:1/-1"><input type="checkbox" name="auth" checked> Signed PAD agreement or card-on-file consent received</label><label class="row" style="grid-column:1/-1"><input type="checkbox" name="autopay"> Also autopay new bills on their due date</label></div>`,
      [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
        const v = A.formValues(d), inv = S.get("invoices", v.inv), instC = toC(v.inst); if (!inv) return false;
        if (!v.auth) { A.toast("The client's authorization must be on file", "bad"); return false; }
        if (!(instC > 0)) { A.toast("Enter an instalment", "bad"); return false; }
        const plan = S.put("payPlans", { id: A.id("pp"), invoiceId: inv.id, matterId: inv.matterId, clientId: inv.clientId, coveredC: toC(totals(inv).balance), freq: v.freq, instC, start: v.start || A.today, auth: `Authorization received ${A.date(A.today)}`, autopay: !!v.autopay, trigger: "due", status: "pending", charges: [], by: A.me().id });
        const n = schedule(plan).length;
        const g = A.gate({ kind: "plan", cap: "billing.approve", matterId: inv.matterId, preparedBy: A.me().id, title: `Payment plan: ${A.contact(inv.clientId).name}, ${n} × up to ${A.cents(instC)}`, summary: `${A.cents(plan.coveredC)} on account ${inv.number}, ${FREQ[v.freq][0].toLowerCase()} from ${A.date(plan.start)}.`,
          checks: [{ label: "Account delivered", ok: inv.status === "sent", blocking: true }, { label: "Client authorization on file", ok: true, blocking: true }, { label: "Pays the general account, not trust", ok: true }], effect: "billing.plan.apply", payload: { planId: plan.id } });
        S.put("payPlans", { id: plan.id, gateId: g.id }); planSel = plan.id; A.toast("Plan sent for approval");
        if (A.route() !== "billing/plans") A.go("billing/plans");
      } }]);
  });
  A.effect("billing.plan.apply", (p) => { S.put("payPlans", { id: p.planId, status: "active" }); A.audit("billing.plan", { target: p.planId, note: "Payment plan approved" }); });
  A.action("billing-plan-status", (el) => { const p = S.get("payPlans", el.dataset.id); if (!p || !canPlans()) return; S.put("payPlans", { id: p.id, status: el.dataset.v }); A.audit("billing.plan.status", { target: p.id, note: el.dataset.v }); A.toast(`Plan ${el.dataset.v === "active" ? "resumed" : el.dataset.v}`); });
  A.action("billing-plan-run", (el) => {
    const p = S.get("payPlans", el.dataset.id); if (!p || p.status !== "active" || !canPlans()) return;
    const inv = S.get("invoices", p.invoiceId), due = schedule(p).filter((s) => !s.paid && s.date <= A.today);
    if (!inv || !due.length) return A.toast("Nothing due today");
    let n = 0; due.forEach((s) => { const bal = toC(totals(S.get("invoices", inv.id)).balance), a = Math.min(bal, s.amountC); if (a <= 0) return;
      S.put("payments", { id: A.id("pay"), invoiceId: inv.id, matterId: inv.matterId, date: A.today, amount: toD(a), method: "Pre-authorized debit (plan)", by: A.me().id }); n++;
      S.put("payPlans", { id: p.id, charges: (S.get("payPlans", p.id).charges || []).concat({ date: s.date, amountC: a }) }); });
    if (totals(S.get("invoices", inv.id)).balance <= 0.005) S.put("invoices", { id: inv.id, status: "paid", paidAt: A.today });
    A.audit("billing.plan.charged", { target: p.id, note: `${n} instalment(s) on ${inv.number}` }); A.toast(`${n} instalment${n === 1 ? "" : "s"} charged`, "ok");
  });

  /* ----- incoming payments: agent allocation suggestions ----- */
  A.seed("incoming", () => [
    { id: "in-1", at: "2026-10-08 10:14", payerId: "c-hanna", amountC: toC(totals(SEED_INV[3], true).total), method: "EFT", dest: "operating", ref: "LKL ACCT 2026-0112", feeC: 0, status: "unallocated" },
    { id: "in-2", at: "2026-10-07 15:40", payerId: "c-grace", amountC: 50000, method: "Cheque", dest: "operating", ref: "", feeC: 0, status: "unallocated" },
    { id: "in-3", at: "2026-10-09 09:02", payerId: "c-selam", amountC: 50000, method: "Card", dest: "operating", ref: "proc ch_8KQ2", feeC: 1100, status: "unallocated" },
    { id: "in-4", at: "2026-10-09 11:30", payerId: null, payer: "J. Haile", amountC: 25000, method: "Interac e-Transfer", dest: "operating", ref: "for Tsion", feeC: 0, status: "unallocated" },
  ]);
  let payInSel = null;
  A.action("billing-in-open", (el) => { payInSel = el.dataset.id; A.refresh(); });
  const payerName = (p) => (p.payerId ? A.contact(p.payerId).name : p.payer || "Unknown payer");
  function suggestAlloc(p) {
    let best = null;
    openInv().forEach((i) => { const bal = toC(totals(i).balance); let c = 0;
      if (p.ref && i.number && p.ref.includes(i.number)) c += 60; if (bal === p.amountC) c += 35; if (p.payerId && i.clientId === p.payerId) c += 30; if (c && p.amountC < bal) c += 5;
      if (c && (!best || c > best.conf)) best = { invoiceId: i.id, conf: Math.min(99, c), amountC: Math.min(bal, p.amountC) }; });
    return best && best.conf >= 30 ? best : null;
  }
  function postAlloc(p, lines) {
    lines.forEach((l) => { const inv = S.get("invoices", l.invoiceId); if (!inv || l.amountC <= 0) return;
      S.put("payments", { id: A.id("pay"), invoiceId: inv.id, matterId: inv.matterId, date: p.at.slice(0, 10), amount: toD(l.amountC), method: p.method, by: A.me().id, incomingId: p.id });
      if (totals(S.get("invoices", inv.id)).balance <= 0.005) S.put("invoices", { id: inv.id, status: "paid", paidAt: p.at.slice(0, 10) }); });
    const used = lines.reduce((s, l) => s + l.amountC, 0);
    S.put("incoming", { id: p.id, status: "allocated", alloc: lines, creditC: p.amountC - used });
    A.audit("billing.allocated", { target: p.id, note: `${A.cents(p.amountC)} from ${payerName(p)}: ${lines.map((l) => `${(S.get("invoices", l.invoiceId) || {}).number} ${A.cents(l.amountC)}`).join(", ") || "none"}${p.amountC - used ? `; ${A.cents(p.amountC - used)} unapplied credit` : ""}` });
  }
  A.action("billing-in-accept", (el) => {
    const p = S.get("incoming", el.dataset.id); if (!p || p.status !== "unallocated" || !A.can("billing.payments", "all")) return;
    const s = suggestAlloc(p), lines = s ? [{ invoiceId: s.invoiceId, amountC: s.amountC }] : [];
    if (s && s.conf >= 90 && p.dest === "operating") { postAlloc(p, lines); return A.toast("Payment allocated", "ok"); }
    if (S.where("approvals", (g) => g.effect === "billing.alloc.apply" && g.status === "pending" && g.payload.id === p.id).length) return A.toast("Already waiting for approval");
    A.gate({ kind: "allocation", cap: "billing.approve", matterId: s ? S.get("invoices", s.invoiceId).matterId : null, preparedBy: A.me().id, title: `Allocate ${A.cents(p.amountC)} from ${payerName(p)}`,
      summary: s ? `${A.cents(s.amountC)} to account ${S.get("invoices", s.invoiceId).number} (${s.conf}% match)${p.amountC > s.amountC ? `; ${A.cents(p.amountC - s.amountC)} held as unapplied credit` : ""}.` : `No matching account; hold ${A.cents(p.amountC)} as unapplied credit until the payer is identified.`,
      checks: [{ label: s ? `Match confidence ${s.conf}%` : "No match", ok: !!s && s.conf >= 90 }, { label: "Goes to the general account, not trust", ok: p.dest === "operating" }], effect: "billing.alloc.apply", payload: { id: p.id, lines } });
    A.toast("Ambiguous match sent for approval (G-B3)");
  });
  A.effect("billing.alloc.apply", (x) => { const p = S.get("incoming", x.id); if (p && p.status === "unallocated") postAlloc(p, x.lines); });
  A.action("billing-in-split", (el) => {
    const p = S.get("incoming", el.dataset.id); if (!p || p.status !== "unallocated" || !A.can("billing.payments", "all")) return;
    const cands = openInv().filter((i) => !p.payerId || i.clientId === p.payerId); let left = p.amountC;
    A.modal(`Split ${A.cents(p.amountC)} from ${payerName(p)}`, h`<div class="form">${cands.map((i) => { const bal = toC(totals(i).balance), a = Math.min(bal, left); left -= a; return h`<label class="field">${i.number} · ${mName(i.matterId)} (owes ${cad(toD(bal))})<input name="${i.id}" type="number" step="0.01" min="0" value="${toD(a).toFixed(2)}"></label>`; })}</div><p class="muted small">Anything not allocated is held as unapplied credit. Processor fees are charged to the general account, never trust.</p>`,
      [{ label: "Cancel" }, { label: "Allocate", primary: true, run: (d) => {
        const v = A.formValues(d), lines = cands.map((i) => ({ invoiceId: i.id, amountC: toC(v[i.id] || 0) })).filter((l) => l.amountC > 0);
        if (lines.reduce((s, l) => s + l.amountC, 0) > p.amountC) { A.toast("Allocation exceeds the payment", "bad"); return false; }
        if (lines.some((l) => l.amountC > toC(totals(S.get("invoices", l.invoiceId)).balance))) { A.toast("A line exceeds the account balance", "bad"); return false; }
        postAlloc(p, lines); A.toast("Payment allocated", "ok");
      } }]);
  });
  function paymentsInbox() {
    const can = A.can("billing.payments", "all"), ins = S.where("incoming", (p) => !p.payerId || A.matters((m) => m.clientId === p.payerId, "billing.view").length).sort((a, b) => b.at.localeCompare(a.at)), p = S.get("incoming", payInSel);
    const sCell = (x) => { if (x.status !== "unallocated") return statusBadge("allocated"); const s = suggestAlloc(x); return s ? h`${A.ui.actor("agent", `${(S.get("invoices", s.invoiceId) || {}).number} · ${s.conf}%`)}` : A.ui.status("unconfirmed", "No match"); };
    const detail = p ? h`<div class="card"><div class="row"><h2 class="grow">${A.cents(p.amountC)} from ${payerName(p)}</h2>${statusBadge(p.status)}</div>
      <dl class="kv"><dt>Received</dt><dd>${A.dateTime(p.at)}</dd><dt>Method</dt><dd>${p.method}</dd><dt>Reference</dt><dd>${p.ref || "None"}</dd><dt>Account</dt><dd>${p.dest === "trust" ? "Trust" : "General"}</dd>
        ${p.feeC ? h`<dt>Processor fee</dt><dd>${A.cents(p.feeC)} charged to the general account, never trust</dd>` : ""}
        ${p.status === "allocated" ? h`<dt>Allocated</dt><dd>${(p.alloc || []).map((l) => `${(S.get("invoices", l.invoiceId) || {}).number} ${A.cents(l.amountC)}`).join(", ") || "None"}${p.creditC ? `; unapplied credit ${A.cents(p.creditC)}` : ""}</dd>` : ""}</dl>
      ${p.status === "unallocated" ? h`<p>${sCell(p)} ${A.ui.actor("person", "You allocate")}</p>${can ? h`<div class="row wrap">${A.ui.btn({ label: suggestAlloc(p) && suggestAlloc(p).conf >= 90 ? "Accept suggestion" : "Send for approval", a: "billing-in-accept", id: p.id, primary: true, small: true, key: "a", hint: false })}${A.ui.btn({ label: "Split across accounts", a: "billing-in-split", id: p.id, small: true })}</div>` : h`<p class="muted small">The bookkeeper or a principal allocates payments.</p>`}` : ""}</div>` : null;
    return h`<h3>Incoming payments</h3>${A.ui.split(A.ui.table([{ label: "Received", render: (x) => A.dateTime(x.at) }, { label: "Payer", render: (x) => payerName(x) }, { label: "Method", key: "method" }, { label: "Amount", num: true, render: (x) => A.cents(x.amountC) }, { label: "Suggested", render: sCell }], ins, { onRow: "billing-in-open", active: payInSel, empty: "No incoming payments" }), detail, { emptyTitle: "Pick a payment", emptyHint: "The agent suggests an account; clear matches post on accept, the rest need approval." })}<h3 style="margin-top:14px">Recorded payments</h3>`;
  }

  /* ---------- matter tabs ---------- */
  A.matterTab({ id: "time", label: "Time", cap: "time.record", order: 40, render(el, m) {
    const rows = entriesVisible((e) => e.matterId === m.id).sort((a, b) => b.date.localeCompare(a.date));
    const hrs = sumH(rows.filter((e) => e.status !== "suggested"));
    A.render(el, h`<div class="row" style="margin-bottom:10px"><span class="muted grow">${hrs.toFixed(1)} h recorded${viewAll() ? "" : " by you"}; ${rows.filter((e) => e.status === "suggested").length} suggestion(s) waiting</span>
      ${A.can("time.record", "act") ? h`<button class="btn" data-a="time-new" data-matter="${m.id}">Add entry</button>` : ""}</div>${entryTable(rows)}`);
  } });
  A.matterTab({ id: "billing", label: "Billing", cap: "billing.view", order: 45, render(el, m) {
    if (!visM(m)) return A.render(el, A.ui.empty("No billing access on this matter"));
    const es = unbilled(m.id), inv = invoicesVisible().filter((i) => i.matterId === m.id), ar = sumD(inv.filter((i) => i.status === "sent"), (i) => totals(i).balance);
    const canPrep = A.can("billing.draft", "act") && m.fee === "hourly" && es.some((e) => !onDraft(e.id));
    A.render(el, h`<div class="stats">${A.ui.stat("Unbilled WIP", cad(sumAmt(es)), `${es.length} entries`)}${A.ui.stat("Outstanding", cad(ar))}${BUDGET[m.id] ? A.ui.stat("Fee estimate", cad(BUDGET[m.id])) : A.ui.stat("Fee basis", m.fee)}</div>
      <div class="row" style="margin-bottom:10px">${canPrep ? A.ui.btn({ label: "✦ Prepare bill for this matter", a: "billing-prepare", agent: true, data: { matter: m.id } }) : ""}${A.can("billing.draft", "act") && A.me().role !== "client" ? A.ui.btn({ label: S.get("splits", m.id) ? `Split billing (${S.get("splits", m.id).status === "active" ? "active" : "awaiting approval"})` : "Split billing", to: `billing/split/${m.id}` }) : ""}</div>
      ${A.ui.table(invCols, inv, { onRow: "billing-open", empty: "No accounts on this matter" })}`);
  } });

  /* ---------- modules ---------- */
  A.module({ id: "time", label: "Time", icon: "◴", order: 40, cap: "time.record", badge: () => (A.can("time.record", "act") ? mySuggested().length : 0),
    smoke: ["time", "time/e/te-1", "time/e/te-s1", "time/e/te-2", "time/expenses", "time/rates", "time/categories", "time/settings", "time/targets", "time/utilization"],
    routes: { time: timePage, "time/expenses": expensesPage, "time/rates": ratesPage, "time/categories": categoriesPage, "time/settings": timeSettingsPage, "time/targets": targetsPage, "time/utilization": utilizationPage, "time/e/:id": entryPage } });
  A.module({ id: "billing", label: "Billing", icon: "$", order: 45, cap: "billing.view",
    smoke: ["billing", "billing/inv-107", "billing/inv-109", "billing/inv-110", "billing/inv-112", "billing/credits", "billing/plans", "billing/settings", "billing/layouts", "billing/split/m1", "billing/split/m7"],
    routes: { billing: billingPage, "billing/credits": creditsPage, "billing/plans": plansPage, "billing/settings": billSettingsPage, "billing/layouts": layoutsPage, "billing/split/:id": splitPage, "billing/:id": invoicePage } });
  /* page keys (shown in the ? sheet) */
  A.ui.keys((r) => r === "time", { t: { label: "Start or stop the timer", fn: () => { const b = A.can("time.record", "act") && document.querySelector('[data-a="time-stop"],[data-a="time-start"]'); if (!b) return false; b.click(); return true; } } });
  A.ui.keys((r) => r === "billing", { p: { label: "Prepare bills", fn: () => { if (!A.can("billing.draft", "act")) return false; prepareBills(); return true; } } });

  /* ---------- agent ---------- */
  A.intent({ match: /what did i (work|do)|worked on today|my time today|time today/i, label: "What did I work on today?", example: "what did I work on today",
    run() {
      if (!A.can("time.record", "act")) return { text: "You do not record time in this role." };
      const s = mySuggested();
      if (!s.length) return { text: "Your day is fully recorded; no suggestions are waiting.", actions: [{ label: "Open time", go: "time" }] };
      const hrs = sumH(s);
      return { text: `From your email, documents and calendar I rebuilt ${hrs.toFixed(1)} h across ${s.length} entries: ${s.map((e) => `${e.hours.toFixed(1)}h ${S.get("matters", e.matterId).number} (${e.narrative.split(/[;.]/)[0]})`).join("; ")}. Nothing is recorded until you accept.`,
        cites: s.filter((e) => e.source && e.source.kind !== "calendar" && docName(e.source.ref)).map((e) => ({ doc: e.source.ref, page: 1, label: docName(e.source.ref) })),
        actions: [{ label: "Review in Time", go: "time" }, { label: `Accept all ${s.length}`, run: () => { s.forEach((e) => accept(e.id)); A.toast(`${s.length} entries accepted`, "ok"); } }] };
    } });
  A.intent({ match: /prepare (the )?bills|billing cycle|draft (the )?bills|run billing/i, label: "Prepare bills", example: "prepare bills",
    run() {
      if (!A.can("billing.draft", "act")) return { text: "Your role cannot prepare bills. A lawyer or the bookkeeper can." };
      const w = wipRows().filter((r) => r.n > r.drafted);
      if (!w.length) return { text: "There is no accepted, unbilled time on hourly matters.", actions: [{ label: "Open billing", go: "billing" }] };
      return { text: `${w.length} matters have accepted unbilled time worth ${cad(sumD(w, (r) => r.value))}: ${w.map((r) => r.m.number).join(", ")}. I will draft each account with HST, run pre-bill checks and send it for approval by a lawyer who did not prepare it.`,
        actions: [{ label: "Prepare drafts", run: () => prepareBills() }, { label: "Open billing", go: "billing" }] };
    } });
  A.intent({ match: /who owes|owes us|aging|ageing|receivable|unpaid (bills|accounts|invoices)/i, label: "Who owes us money?", example: "who owes us money",
    run() {
      if (!A.can("billing.view")) return { text: "Your role cannot see billing." };
      const a = agingRows();
      if (!a.length) return { text: "No accounts are outstanding." };
      return { text: `${cad(sumD(a, (r) => r.total))} outstanding. ${a.map((r) => `${r.name}: ${cad(r.total)}${r["90+"] || r["61-90"] ? ` (${cad(toD(toC(r["90+"]) + toC(r["61-90"])))} over 60 days)` : ""}`).join("; ")}.`,
        actions: openInv().filter((i) => age(i) > 30).slice(0, 3).map((i) => ({ label: `Account ${i.number}`, go: `billing/${i.id}` })).concat([{ label: "Open billing", go: "billing" }]) };
    } });
  A.suggest("home", () => [].concat(A.can("time.record", "act") && mySuggested().length ? ["What did I work on today?"] : [], A.can("billing.draft", "act") ? ["Who owes us money?"] : []));
  A.suggest("time", () => ["What did I work on today?"]);
  A.suggest("billing", () => (A.can("billing.draft", "act") ? ["Prepare bills", "Who owes us money?"] : ["Who owes us money?"]));
  A.suggest("invoice", () => ["Who owes us money?"]);
})();
