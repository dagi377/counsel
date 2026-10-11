/* Intake: lead inbox, agent triage, conflict checks, ID verification, engagement and matter opening. */
(function () {
  "use strict";
  const A = window.App, h = A.h, S = A.store;
  const STAGES = ["New", "Accepted", "Conflict check", "Consult", "Engagement", "Opened"];
  const RULE = "Limitations Act, 2002 (Ontario), s. 4: no proceeding after the second anniversary of the day the claim was discovered. s. 5: a claim is discovered on the day the person knew, or reasonably ought to have known, of the injury, that it was caused by the act or omission of the other party, and that a proceeding is an appropriate remedy. s. 15: ultimate limit of 15 years from the act or omission.";
  let view = "list";

  /* ---------- seed ---------- */
  const lines = (arr) => [arr];
  A.seed("leads", [
    { id: "lead-north", name: "Northgate Storage Inc.", kind: "org", contactName: "Janet Park, office manager", email: "jpark@northgatestorage.example.ca", phone: "416-555-0161",
      source: "Email", received: "2026-10-08 16:22", area: "Commercial litigation", conf: 0.82, urgency: "medium", discovered: "2026-07-20", limit: "basic",
      summary: "Storage operator wants to enforce a lien and sue a former tenant, Brightwater Imports Ltd., for $14,200 in unpaid rent.",
      parties: [{ name: "Northgate Storage Inc.", role: "client" }, { name: "Brightwater Imports Ltd.", role: "adverse" }],
      assignee: "u-yoni", why: "Commercial collections; Yonathan carries the lease and storage files.", stage: "Conflict check", agentHandling: true,
      prescreen: { result: "hit", note: "The firm acts against Northgate Storage Inc. in 2026-0142 (Tesfaye v. Northgate Storage)." },
      facts: [["Claimant", "Northgate Storage Inc.", 2], ["Other side", "Brightwater Imports Ltd.", 4], ["Amount", "$14,200 unpaid rent", 5], ["Default since", "July 20, 2026", 5], ["Ask", "Lien sale and claim for the balance", 6]],
      raw: lines(["From: Janet Park <jpark@northgatestorage.example.ca>", "Subject: Need a lawyer - Northgate Storage Inc.", "Date: October 8, 2026 4:22 p.m.", "We have a tenant, Brightwater Imports Ltd., at our Finch Ave W facility.", "They owe $14,200 in rent and have not paid since July 20, 2026.", "We want to sell the stored goods under our lien and sue for the rest.", "Can someone call me this week?"]) },
    { id: "lead-fall", name: "Abebe Girma", kind: "person", email: "abebe.girma@example.ca", phone: "647-555-0133",
      source: "Phone note", received: "2026-10-09 09:05", area: "Personal injury", conf: 0.94, urgency: "high", discovered: "2024-10-28", limit: "basic",
      summary: "Fell on a wet, uneven ramp at Fairview Plaza on 2024-10-28; fractured wrist; the plaza's insurer denied the claim. No lawyer yet.",
      parties: [{ name: "Abebe Girma", role: "client" }, { name: "Fairview Plaza Management Ltd.", role: "adverse" }],
      assignee: "u-ayo", why: "Personal injury; Ayo runs the PI files and has capacity this week.", stage: "New", prescreen: { result: "clear", note: "No match in contacts, matters or documents." },
      facts: [["Incident date", "October 28, 2024", 3], ["Place", "Ramp at Fairview Plaza, 2250 Lawrence Ave E", 3], ["Injury", "Fractured right wrist, surgery Nov 2024", 4], ["Occupier", "Fairview Plaza Management Ltd.", 5], ["Insurer position", "Denied, letter of Sept 30, 2026", 6]],
      raw: lines(["Phone note - Meraf Abay - October 9, 2026 9:05 a.m.", "Caller: Abebe Girma, 647-555-0133", "Slipped on a wet, uneven ramp at Fairview Plaza, 2250 Lawrence Ave E, on October 28, 2024.", "Fractured right wrist; surgery in November 2024; still off work.", "Plaza is managed by Fairview Plaza Management Ltd.", "Their insurer denied the claim by letter dated September 30, 2026.", "Wants to know if it is too late."]) },
    { id: "lead-dismiss", name: "Priya Raman", kind: "person", email: "priya.raman@example.ca", phone: "416-555-0190",
      source: "Web form", received: "2026-10-08 21:40", area: "Employment", conf: 0.91, urgency: "high", discovered: "2026-09-18", limit: "basic",
      summary: "Senior dispatcher of 9 years terminated without cause on 2026-09-18 with ESA minimums only; release must be signed by 2026-10-16.",
      parties: [{ name: "Priya Raman", role: "client" }, { name: "Brightline Couriers Ltd.", role: "adverse" }],
      assignee: "u-ayo", why: "Employment; Ayo has the Haile file with the same issue.", stage: "New", prescreen: { result: "clear", note: "No match in contacts, matters or documents." },
      facts: [["Employer", "Brightline Couriers Ltd.", 2], ["Position", "Senior dispatcher", 3], ["Service", "9 years (start March 2017)", 3], ["Termination", "September 18, 2026, without cause", 4], ["Offer", "8 weeks' pay (ESA minimum) for a full release", 5], ["Release deadline", "October 16, 2026", 5]],
      raw: lines(["Web intake form - submitted October 8, 2026 9:40 p.m.", "Name: Priya Raman. Employer: Brightline Couriers Ltd.", "Role: senior dispatcher since March 2017.", "I was let go on September 18, 2026. They said it was not for cause.", "They offered 8 weeks' pay if I sign a full release by October 16, 2026.", "I have not signed anything."]) },
    { id: "lead-condo", name: "Tomas and Lea Brandt", kind: "person", email: "brandt.home@example.ca", phone: "905-555-0148",
      source: "Web form", received: "2026-10-07 11:12", area: "Real estate", conf: 0.97, urgency: "low", discovered: "", limit: "none",
      summary: "First-time buyers taking an assignment of a pre-construction condo at 88 Queens Quay W; final closing 2026-12-15.",
      parties: [{ name: "Tomas and Lea Brandt", role: "client" }, { name: "Harbourfront Towers Ltd.", role: "other" }],
      assignee: "u-yoni", why: "Residential real estate; Yonathan is the real estate lawyer.", stage: "Accepted", prescreen: { result: "clear", note: "No match in contacts, matters or documents." },
      facts: [["Property", "Unit 1907, 88 Queens Quay W", 2], ["Transaction", "Assignment of agreement of purchase and sale", 3], ["Developer", "Harbourfront Towers Ltd.", 3], ["Closing", "December 15, 2026", 4], ["Buyers", "First-time buyers (land transfer tax rebate)", 5]],
      raw: lines(["Web intake form - submitted October 7, 2026 11:12 a.m.", "Names: Tomas Brandt and Lea Brandt. Unit 1907, 88 Queens Quay W.", "We are taking an assignment of a pre-construction purchase from Harbourfront Towers Ltd.", "Final closing is December 15, 2026.", "We are first-time buyers."]) },
    { id: "lead-estate", name: "Eric Lindquist", kind: "person", email: "", phone: "289-555-0175",
      source: "Phone note", received: "2026-10-09 08:30", area: "Estate litigation", conf: 0.76, urgency: "medium", discovered: "2026-05-12", limit: "basic",
      summary: "Says his cousin is administering his aunt's estate and he has not been told about the house at 41 Willow Cres; asks whether he can challenge.",
      parties: [{ name: "Eric Lindquist", role: "client" }, { name: "Grace Mendel", role: "adverse" }],
      assignee: "u-linda", why: "Estates; Linda supervises estate litigation.", stage: "New",
      prescreen: { result: "possible", note: "Grace Mendel is a current client (2026-0155). The caller is likely Erik Lindqvist, named in the will on that file." },
      facts: [["Deceased", "His aunt (name not given)", 3], ["Date of death", "May 12, 2026", 3], ["Estate trustee", "Cousin, Grace Mendel", 4], ["Asset in issue", "House at 41 Willow Cres", 5], ["Ask", "Can he challenge the administration", 6]],
      raw: lines(["Phone note - Kaleab Bereket - October 9, 2026 8:30 a.m.", "Caller: Eric Lindquist, 289-555-0175 (spelling not confirmed)", "His aunt died May 12, 2026.", "His cousin Grace Mendel is the estate trustee.", "He has heard nothing about the house at 41 Willow Cres.", "Wants to know whether he can challenge what she is doing."]) },
    { id: "lead-kestrel", name: "Kestrel Print Co.", kind: "org", contactName: "Mina Yusuf, owner", email: "mina@kestrelprint.example.ca", phone: "416-555-0127",
      source: "Email", received: "2026-10-02 10:48", area: "Commercial litigation", conf: 0.88, urgency: "low", discovered: "2025-01-31", limit: "basic",
      summary: "Orion Signs Ltd. has not paid printing invoices totalling $23,750 due January 31, 2025; demand letter ignored.",
      parties: [{ name: "Kestrel Print Co.", role: "client" }, { name: "Orion Signs Ltd.", role: "adverse" }],
      assignee: "u-linda", why: "Debt claim against Orion; Linda knows the party from 2025-0311.", stage: "Consult",
      prescreen: { result: "clear", note: "Orion Signs Ltd. is adverse on 2025-0311 as well; no conflict." },
      conflict: { status: "cleared", ranAt: "2026-10-03 09:10", by: "u-meraf", checkId: "cc-kestrel", clearedBy: "u-linda", clearedAt: "2026-10-03 11:02",
        matches: [{ party: "Orion Signs Ltd.", role: "adverse", hit: "Orion Signs Ltd.", score: 1, where: "Contact, opposing party on 2025-0311", tone: "ok", note: "Also adverse on another file. No conflict." }] },
      facts: [["Creditor", "Kestrel Print Co.", 2], ["Debtor", "Orion Signs Ltd.", 3], ["Amount", "$23,750 (4 invoices)", 3], ["Due date", "January 31, 2025", 4], ["Demand", "Sent July 2, 2026; no reply", 5]],
      raw: lines(["From: Mina Yusuf <mina@kestrelprint.example.ca>", "Kestrel Print Co. - unpaid account", "Orion Signs Ltd. owes us $23,750 on four invoices.", "All were due January 31, 2025.", "We sent a demand letter on July 2, 2026 and heard nothing.", "We would like to sue."]) },
  ]);
  A.seed("conflictChecks", [
    { id: "cc-kestrel", leadId: "lead-kestrel", name: "Kestrel Print Co.", parties: "Kestrel Print Co.; Orion Signs Ltd.", ranAt: "2026-10-03 09:10", by: "u-meraf", result: "clear", matches: 1, clearedBy: "u-linda", clearedAt: "2026-10-03 11:02" },
    { id: "cc-fortier", leadId: null, name: "Daniel Fortier", parties: "Daniel Fortier; Lakeshore Transit Corp.", ranAt: "2026-10-04 14:20", by: "u-meraf", result: "clear", matches: 0, clearedBy: "u-ayo", clearedAt: "2026-10-04 16:05", matterId: "m3" },
    { id: "cc-haile", leadId: null, name: "Tsion Haile", parties: "Tsion Haile; Northgate Logistics Inc.", ranAt: "2026-09-09 10:02", by: "u-ayo", result: "review", matches: 1, clearedBy: "u-linda", clearedAt: "2026-09-09 15:40", matterId: "m5" },
  ]);

  A.seed("intakeEmails", [
    { id: "em-1", from: "Daniel Okafor <d.okafor@example.ca>", subject: "Wrongful dismissal question", at: "2026-10-09 07:12", status: "new",
      body: "I was a warehouse supervisor at Lakeview Distribution Inc. for 6 years and was let go on September 30, 2026 without cause. They offered 6 weeks' pay. Do I have a case?", name: "Daniel Okafor", area: "Employment", adverse: "Lakeview Distribution Inc.", discovered: "2026-09-30" },
    { id: "em-2", from: "Selam Haile <selam.h@example.ca>", subject: "Buying a house in Scarborough", at: "2026-10-09 08:03", status: "new",
      body: "We signed an agreement to buy 17 Birchmount Rd; closing is November 27, 2026. We need a lawyer for the closing.", name: "Selam Haile", area: "Real estate", adverse: "", discovered: "" },
    { id: "em-3", from: "no-reply@billing-alerts.example.com", subject: "Your statement is ready", at: "2026-10-09 08:20", status: "new",
      body: "Your monthly statement is available. Log in to view it.", name: "", area: "", adverse: "", discovered: "" },
    { id: "em-4", from: "M. Tran <mtran88@example.ca>", subject: "help please", at: "2026-10-08 23:51", status: "review", reason: "No facts or practice area; asks only for a call back.",
      body: "My brother needs help, can someone call me tomorrow? 416-555-0119.", name: "M. Tran", area: "", adverse: "", discovered: "" },
  ]);
  const PRACTICE = ["Commercial litigation", "Personal injury", "Employment", "Real estate", "Estate litigation", "Estates"];
  const DISCLOSE = "This reply is from the Linda K Law intake assistant, an automated assistant. It is not a lawyer and this is not legal advice. No lawyer-client relationship exists until the firm confirms in writing that it will act for you.";

  /* ---------- helpers ---------- */
  const lead = (id) => S.get("leads", id);
  const me = () => A.me();
  const addYears = (d, n) => (+d.slice(0, 4) + n) + d.slice(4);
  function limitation(l) {
    if (l.limit === "none" || !l.discovered) return { flag: "n/a", tone: "", label: "No limitation (transaction)" };
    const deadline = addYears(l.discovered, 2), left = A.daysBetween(A.today, deadline);
    const flag = left < 0 ? "expired" : left <= 90 ? "at risk" : left <= 180 ? "watch" : "ok";
    const tone = flag === "expired" || flag === "at risk" ? "bad" : flag === "watch" ? "warn" : "ok";
    return { deadline, left, flag, tone, label: left < 0 ? `Expired ${A.date(deadline)}` : `${A.date(deadline)} · ${left} days` };
  }
  /** How well the lead fits the firm: {label: good|possible|poor, tone, reasons}. */
  function fit(l) {
    const r = []; let n = 0;
    if (PRACTICE.includes(l.area)) { n += 2; r.push(`${l.area} is a practice area of the firm`); } else { n -= 2; r.push(`${l.area} is outside the firm's practice areas`); }
    const lim = limitation(l);
    if (lim.flag === "expired") { n -= 3; r.push("The basic limitation period appears to have expired"); }
    else if (lim.flag === "at risk") r.push(`Limitation in ${lim.left} days: act this week`);
    const cleared = l.conflict && l.conflict.status === "cleared";
    if (!cleared && l.prescreen && l.prescreen.result === "hit") { n -= 2; r.push("Pre-screen found a likely direct conflict"); }
    else if (!cleared && l.prescreen && l.prescreen.result === "possible") { n -= 1; r.push("A party resembles an existing client or party"); }
    const amt = /\$([\d,]+)/.exec(l.summary || ""); const v = amt ? +amt[1].replace(/,/g, "") : 0;
    if (v && v <= 50000 && /litigation/i.test(l.area)) { n -= 1; r.push(`${A.cents(v * 100)} is within Small Claims Court's $50,000 limit; fees may outweigh recovery`); }
    if ((l.conf || 0) < 0.6) { n -= 1; r.push("The facts are thin; the area is a guess"); }
    const label = n >= 2 ? "good" : n >= 0 ? "possible" : "poor";
    return { label, tone: { good: "ok", possible: "warn", poor: "bad" }[label], reasons: r };
  }
  /** Client risk rating for matter opening (money laundering and identity rules). */
  function kyc(l) {
    const r = []; let n = 0;
    if (!(l.idv && l.idv.status === "verified")) { n += 2; r.push("Identity not verified yet (By-Law 7.1)"); }
    if (l.kind === "org") { n += 1; r.push("Organization: record directors and anyone who owns or controls 25% or more"); }
    if (/real estate/i.test(l.area)) { n += 1; r.push("Real estate: funds pass through trust; confirm the source of the purchase funds"); }
    if (l.prescreen && l.prescreen.result === "possible") { n += 1; r.push("Connected to an existing client's file"); }
    if (!l.email) { n += 1; r.push("No email on file; contact by phone only"); }
    const level = n >= 3 ? "high" : n >= 1 ? "medium" : "low";
    if (!r.length) r.push("Individual, verified, ordinary fee retainer, no unusual payments");
    return { level, tone: { low: "ok", medium: "warn", high: "bad" }[level], reasons: r };
  }
  const urgTone = { high: "bad", medium: "warn", low: "" };
  const preTone = { hit: "bad", possible: "warn", clear: "ok" };
  const gatesFor = (id) => S.where("approvals", (g) => g.payload && g.payload.leadId === id);
  const pendingGate = (id, eff) => gatesFor(id).find((g) => g.effect === eff && g.status === "pending");
  const approvedGate = (id, eff) => gatesFor(id).find((g) => g.effect === eff && g.status === "approved");
  const closed = (l) => l.stage === "Opened" || l.stage === "Declined";
  const docId = (l) => `d-lead-${l.id}`;
  function ensureDocs() {
    const docs = S.all("docs");
    S.all("leads").forEach((l) => { if (l.raw && !docs.some((d) => d.id === docId(l))) docs.push({ id: docId(l), matterId: null, leadId: l.id, name: `${l.source}: ${l.name}`, kind: "intake", added: l.received.slice(0, 10), by: "agent", pages: l.raw }); });
  }
  const cite = (l, line) => ({ doc: docId(l), page: 1, line, label: l.source, quote: (l.raw && l.raw[0][line - 1]) || "" });
  const canWhy = (cap, level, what) => (A.can(cap, level) ? "" : `${A.roleName(me().role)} cannot ${what} (${cap})`);

  /* ---------- fuzzy conflict search ---------- */
  const STOP = new Set(["inc", "ltd", "llp", "corp", "corporation", "co", "the", "and", "of", "limited", "estate", "mr", "ms", "mrs"]);
  const toks = (s) => String(s).toLowerCase().replace(/[^a-z0-9 ]/g, " ").split(/\s+/).filter((w) => w && !STOP.has(w));
  function lev(a, b) {
    const d = Array.from({ length: b.length + 1 }, (_, i) => i);
    for (let i = 1; i <= a.length; i++) { let p = d[0]; d[0] = i; for (let j = 1; j <= b.length; j++) { const t = d[j]; d[j] = Math.min(d[j] + 1, d[j - 1] + 1, p + (a[i - 1] === b[j - 1] ? 0 : 1)); p = t; } }
    return d[b.length];
  }
  const sim = (a, b) => 1 - lev(a, b) / Math.max(a.length, b.length, 1);
  function score(q, cand) {
    const qt = toks(q), ct = toks(cand); if (!qt.length || !ct.length) return 0;
    return qt.reduce((s, w) => s + Math.max(...ct.map((c) => sim(w, c))), 0) / qt.length;
  }
  /** Search the whole firm (all matters, not only ones the user can see) for one name. */
  function search(name, role, opts = {}) {
    const out = [];
    const exactKey = toks(name).join(" ");
    const label = (m) => (A.canMatter(m) ? `${m.number} ${m.name}` : `${m.number} (restricted matter)`);
    S.all("contacts").forEach((c) => {
      const sc = opts.exact ? (toks(c.name).join(" ") === exactKey ? 1 : 0) : score(name, c.name); if (sc < ccfg().threshold / 100) return;
      const asClient = S.where("matters", (m) => m.clientId === c.id), asAdverse = S.where("matters", (m) => (m.opposing || []).includes(c.id));
      const where = [...asClient.map((m) => `client on ${label(m)}${m.status === "closed" ? " (closed)" : ""}`), ...asAdverse.map((m) => `opposing party on ${label(m)}`)].join("; ") || `contact (${c.role || "no matter"})`;
      let tone = "warn", note = "Similar name. Confirm it is a different person or entity.";
      if (sc >= 0.85) {
        const openClient = asClient.some((m) => m.status !== "closed"), openAdverse = asAdverse.some((m) => m.status !== "closed");
        if (role === "client" && openAdverse) { tone = "bad"; note = "Direct conflict: the firm acts against this party on an open matter (Rules of Professional Conduct r. 3.4-1)."; }
        else if (role === "adverse" && openClient) { tone = "bad"; note = "Direct conflict: adverse to a current client (r. 3.4-1)."; }
        else if (role === "adverse" && asClient.length) { tone = "warn"; note = "Former client. Check for confidential information relevant to this matter (r. 3.4-10)."; }
        else if (role === "client" && asClient.length) { tone = "ok"; note = "Existing client. No conflict from this match."; }
        else if (role === "adverse" && asAdverse.length) { tone = "ok"; note = "Also adverse on another file. No conflict."; }
        else { tone = "ok"; note = "Known contact; no adverse relationship."; }
      }
      out.push({ party: name, role, hit: c.name, score: Math.round(sc * 100) / 100, where, tone, note });
    });
    const qt = toks(name).filter((w) => w.length >= 4);
    if (qt.length && opts.mode !== "names") S.all("docs").forEach((d) => {
      if (d.leadId) return;
      const m = S.get("matters", d.matterId); if (!m) return;
      (d.pages || []).forEach((pg, pi) => pg.forEach((ln, li) => {
        const words = toks(ln); if (!words.length) return;
        if ((opts.exact ? (" " + words.join(" ") + " ").includes(" " + exactKey + " ") : qt.every((w) => Math.max(...words.map((x) => sim(w, x))) >= 0.75)) && !out.some((o) => o.where.includes(m.number))) {
          out.push({ party: name, role, hit: ln.length > 70 ? ln.slice(0, 70) + "…" : ln, score: 0.8, where: `${d.name} on ${label(m)}, p.${pi + 1} l.${li + 1}`, tone: role === "adverse" || m.clientId ? "warn" : "warn",
            note: `Named in a document on a file for ${A.contact(m.clientId).name}. Likely the same person; treat as related to that client.`, cite: { doc: d.id, page: pi + 1, line: li + 1, quote: ln } });
        }
      }));
    });
    return out;
  }
  /** Plain keyword search across every matter document (names, addresses, file numbers). */
  function kwSearch(q) {
    const t = q.toLowerCase().trim(), out = []; if (t.length < 3) return out;
    S.all("docs").forEach((d) => { if (d.leadId) return; const m = S.get("matters", d.matterId); if (!m) return;
      (d.pages || []).forEach((pg, pi) => pg.forEach((ln, li) => { if (String(ln).toLowerCase().includes(t)) out.push({ party: q, role: "keyword", hit: ln.length > 70 ? ln.slice(0, 70) + "…" : ln, score: 1,
        where: `${d.name} on ${A.canMatter(m) ? `${m.number} ${m.name}` : `${m.number} (restricted matter)`}, p.${pi + 1} l.${li + 1}`, tone: "warn", note: "Keyword found in a matter document. Review for relevance.", cite: { doc: d.id, page: pi + 1, line: li + 1, quote: ln } }); })); });
    return out;
  }
  const resultOf = (matches) => (matches.some((x) => x.tone === "bad") ? "conflict" : matches.some((x) => x.tone === "warn") ? "review" : "clear");

  /* ---------- letters ---------- */
  function engagementBody(l) {
    const lim = limitation(l), who = A.user(l.assignee);
    const fee = /injury/i.test(l.area) ? "a contingency fee of 30% of damages recovered, excluding costs, under a separate contingency fee agreement that complies with O. Reg. 563/20" : `our hourly rates: ${who.name} $425, associates $295, law clerks $160. HST applies`;
    return String(h`<p>${A.date(A.today)}</p><p>${l.name}${l.contactName ? h`<br>Attention: ${l.contactName}` : ""}<br>By email${l.email ? `: ${l.email}` : ""}</p>
      <p><b>Re: Engagement of Linda K Law: ${l.area.toLowerCase()} matter</b></p>
      <p>Thank you for choosing Linda K Law. This letter sets out the terms on which we will act for you.</p>
      <p><b>Scope.</b> ${l.summary} We will advise you and, on your instructions, start or defend a proceeding. Anything else, including appeals, needs a new engagement.</p>
      <p><b>Who will work on it.</b> ${who.name} will be responsible, assisted by others at the firm as needed.</p>
      <p><b>Fees and retainer.</b> We will charge ${fee}. We ask for a retainer of $2,500, held in our trust account and applied to accounts as rendered. We bill monthly; accounts are payable within 30 days and you may have them assessed under the Solicitors Act.</p>
      ${lim.deadline ? h`<p><b>Limitation period.</b> On the facts you gave us, the last day to start a claim is ${A.date(lim.deadline)} (Limitations Act, 2002, s. 4). We will diarize it once you sign.</p>` : ""}
      <p><b>Identification.</b> The Law Society requires us to verify your identity before we act (By-Law 7.1).</p>
      <p><b>Ending the retainer.</b> You may end our retainer at any time. We may withdraw only for good cause and on reasonable notice (r. 3.7).</p>
      <p>Please sign and return a copy. Our retainer begins when we receive it with the trust deposit.</p>
      <p>Yours truly,<br>LINDA K LAW<br>Per: ${who.name}</p><p>I agree to these terms. ______________________ Date: __________</p>`);
  }
  function declineBody(l, referTo) {
    const lim = limitation(l);
    return String(h`<p>${A.date(A.today)}</p><p>${l.name}${l.contactName ? h`<br>Attention: ${l.contactName}` : ""}</p>
      <p><b>Re: Your enquiry of ${A.date(l.received.slice(0, 10))}</b></p>
      <p>Thank you for contacting Linda K Law. We are not able to act for you in this matter, and we have not formed an opinion on its merits.</p>
      ${lim.deadline ? h`<p><b>Time limits apply.</b> A claim like yours generally must be started within two years of the day it was discovered (Limitations Act, 2002, s. 4). On the dates you gave us, that may be as early as <b>${A.date(lim.deadline)}</b>. If you miss it, you may lose your right to sue. Please get legal advice promptly.</p>` : ""}
      <p>${referTo ? `We suggest you contact ${referTo}. ` : ""}You can also find a lawyer through the Law Society Referral Service at lsrs.lso.ca. We are returning any documents you sent and have kept no originals.</p>
      <p>Yours truly,<br>LINDA K LAW</p>`);
  }
  function memoBody(l, matches, res) {
    return String(h`<p><b>Conflict clearance memo</b> · ${l.name} · ${l.area}</p>
      <p>Parties searched: ${l.parties.map((p) => `${p.name} (${p.role})`).join("; ")}. Searched all contacts, all matters (open, closed and restricted) and all matter documents, with fuzzy name matching.</p>
      ${matches.length ? h`<ul>${matches.map((x) => h`<li><b>${x.party}</b> ~ ${x.hit} (${Math.round(x.score * 100)}%): ${x.where}. ${x.note}</li>`)}</ul>` : h`<p>No matches.</p>`}
      <p><b>Recommendation:</b> ${res === "conflict" ? "Do not act. A direct conflict with a current client cannot be cured by consent on these facts; send a non-engagement letter." : res === "review" ? "Clear after confirming the similar names are different people, or obtain informed consent in writing (r. 3.4-2)." : "Clear. No conflict found."}</p>`);
  }

  /* ---------- effects ---------- */
  /* G2 sign-off: the lawyer picks one of four outcomes on the clearance page. */
  const OUTCOMES = { cleared: "Cleared", consent: "Cleared subject to consent", wall: "Cleared subject to a screen (wall)", decline: "Decline" };
  A.effect("intake-clear", (p) => {
    const l = lead(p.leadId); if (!l) return;
    const by = me().id, at = A.now(), oc = p.outcome || "cleared";
    const status = oc === "decline" ? "declined" : oc === "consent" ? "awaiting consent" : "cleared";
    S.put("leads", { id: l.id, conflict: { ...l.conflict, status, outcome: oc, conditions: p.conditions || [], rationale: p.rationale || "", wall: p.wall || [], clearedBy: by, clearedAt: at }, stage: status === "cleared" && STAGES.indexOf(l.stage) < 3 ? "Consult" : l.stage });
    if (p.checkId) S.put("conflictChecks", { id: p.checkId, clearedBy: by, clearedAt: at, outcome: oc });
    if (oc === "consent") (p.consentParties || []).forEach((party) => S.put("consents", { id: A.id("cn"), leadId: l.id, party, type: "consent to act (r. 3.4-2)", status: "draft", ila: true, disclosure: "", scope: "This matter only.", trail: [[A.now(), `Required by clearance signed by ${me().name}`]] }));
    if (oc === "decline") startDecline(lead(l.id), "");
    A.audit("conflict.signed", { target: l.id, note: `${OUTCOMES[oc]}: ${l.name}` });
  });
  A.effect("intake-ack", (p) => { (p.emailIds || []).forEach((id) => S.put("intakeEmails", { id, replied: A.now() })); A.audit("intake.ack_sent", { note: `${(p.emailIds || []).length} replies from the intake assistant` }); });
  A.effect("intake-retainer", (p) => { const l = lead(p.leadId); if (!l) return; S.put("leads", { id: l.id, retainer: { cents: p.cents, method: p.method, account: "Trust account", status: "requested", at: A.now() } }); A.audit("retainer.requested", { target: l.id, note: `${A.cents(p.cents)} to trust` }); });
  A.effect("intake-decline", (p) => { const l = lead(p.leadId); if (!l) return; S.put("leads", { id: l.id, stage: "Declined", declinedAt: A.now() }); A.audit("lead.declined", { target: l.id, note: "Non-engagement letter sent" }); });
  A.effect("intake-engage", (p) => { const l = lead(p.leadId); if (!l) return; S.put("leads", { id: l.id, engagement: "approved", stage: "Engagement" }); A.audit("engagement.released", { target: l.id, note: l.name }); });
  A.effect("intake-idv", (p) => { const l = lead(p.leadId); if (!l) return; S.put("leads", { id: l.id, idv: { ...l.idv, status: "verified", verifiedBy: me().id, at: A.now() } }); A.audit("client.id.verified", { target: l.id, note: `${p.method}; ${p.document}` }); });
  A.effect("intake-open", (p) => {
    const l = lead(p.leadId); if (!l || l.matterId) return;
    const c = S.put("contacts", { id: A.id("c"), kind: l.kind, name: l.name, email: l.email, phone: l.phone, idStatus: l.idv && l.idv.status === "verified" ? "verified" : "identified" });
    const opposing = l.parties.filter((x) => x.role === "adverse").map((x) => S.put("contacts", { id: A.id("c"), kind: /inc|ltd|corp|co\.?$/i.test(x.name) ? "org" : "person", name: x.name, role: "opposing party" }).id);
    const nums = S.all("matters").map((m) => /^2026-0(\d+)$/.exec(m.number)).filter(Boolean).map((x) => +x[1]);
    const number = `2026-0${Math.max(160, ...nums) + 1}`;
    const adverse = l.parties.find((x) => x.role === "adverse");
    const team = [...new Set([l.assignee, "u-linda", g0(l)].filter(Boolean))];
    const m = S.put("matters", { id: A.id("m"), number, name: adverse ? `${short(l)} v. ${short(adverse)}` : `${l.name}: ${l.area}`, area: l.area, clientId: c.id, opposing,
      responsible: l.assignee, team, wall: (l.conflict && l.conflict.wall) || [], stage: "Intake complete", status: "open", opened: A.today, fee: /injury/i.test(l.area) ? "contingency" : "hourly", summary: l.summary, leadId: l.id, kycRisk: kyc(l).level });
    S.put("leads", { id: l.id, stage: "Opened", matterId: m.id });
    if (l.conflict && l.conflict.checkId) S.put("conflictChecks", { id: l.conflict.checkId, matterId: m.id });
    A.audit("matter.opened", { target: m.id, note: `${number} from lead ${l.name}` });
    A.go(`matters/${m.id}`);
  });
  const short = (p) => (/ (Inc|Ltd|Corp|Co)\.?$/.test(p.name) ? p.name.replace(/,? (Inc|Ltd|Corp|Co)\.?$/, "") : p.name.split(" ").pop());
  const g0 = (l) => (l.idv && l.idv.verifiedBy) || null;

  /* ---------- actions ---------- */
  const getL = (el) => lead(el.dataset.id);
  A.action("intake-open", (el) => A.go(`intake/${el.dataset.id}`));
  A.action("intake-accept", (el) => {
    const l = getL(el); if (!l || l.stage !== "New" || !A.can("intake.convert", "act")) return;
    S.put("leads", { id: l.id, stage: "Accepted", acceptedBy: me().id }); A.audit("lead.accepted", { target: l.id, note: l.name }); A.toast(`Accepted: ${l.name}`, "ok");
  });
  function startDecline(l, referTo) {
    if (!l || closed(l) || pendingGate(l.id, "intake-decline") || !A.can("intake.convert", "act")) return;
    A.agent.run({ title: `Non-engagement letter: ${l.name}`, steps: ["Reading the intake", "Checking limitation dates", "Drafting the letter"], done: () => A.gate({
      kind: "letter", cap: "intake.convert", title: `Decline ${l.name}: non-engagement letter`, summary: `The agent drafted a non-engagement letter with a limitation warning${referTo ? ` and a referral to ${referTo}` : ""}. Approving sends it and closes the lead.`,
      preparedBy: me().id, body: declineBody(l, referTo), cites: [cite(l, 1)], checks: [{ label: "Limitation warning included", ok: true }, { label: "No opinion on merits given", ok: true }], effect: "intake-decline", payload: { leadId: l.id } }) });
    A.toast("Drafting non-engagement letter");
  }
  A.action("intake-decline", (el) => startDecline(getL(el), ""));
  function runCheck(l) {
    const checkId = A.id("cc");
    S.put("leads", { id: l.id, stage: STAGES.indexOf(l.stage) < 2 ? "Conflict check" : l.stage, conflict: { status: "running", ranAt: A.now(), by: me().id, checkId, matches: [] } });
    A.agent.run({ title: `Conflict check: ${l.name}`, steps: ["Expanding names and aliases", "Searching contacts and matters", "Scanning matter documents", "Writing the clearance memo"], done: () => {
      const matches = [].concat(...l.parties.map((p) => search(p.name, p.role)));
      const res = resultOf(matches), by = me().id;
      S.put("leads", { id: l.id, conflict: { status: res, ranAt: A.now(), by, checkId, matches } });
      S.put("conflictChecks", { id: checkId, leadId: l.id, name: l.name, parties: l.parties.map((p) => p.name).join("; "), ranAt: A.now(), by, result: res, matches: matches.length });
      A.audit("conflict.run", { target: l.id, note: `${res}, ${matches.length} matches` });
      return A.gate({ kind: "conflict", cap: "conflicts.clear", title: `Clear conflicts: ${l.name}`, summary: `${matches.length} match${matches.length === 1 ? "" : "es"}; result ${res}.`, preparedBy: by, body: memoBody(l, matches, res),
        cites: matches.filter((x) => x.cite).map((x) => x.cite), checks: [{ label: "No direct conflict with a current client", ok: res !== "conflict", blocking: true }, { label: "Similar names reviewed", ok: res === "clear" }],
        effect: "intake-clear", payload: { leadId: l.id, checkId } });
    } });
  }
  A.action("intake-conflict", (el) => { const l = getL(el); if (!l || closed(l) || !A.can("conflicts.run", "act") || pendingGate(l.id, "intake-clear")) return; runCheck(l); });
  const idvApprovable = () => A.roles.some((r) => A.grant("conflicts.identify", r.id) === "approve");
  A.action("intake-idv", (el) => {
    const l = getL(el); if (!l || !A.can("conflicts.identify", "act")) return;
    A.modal(`Verify identity: ${l.name}`, h`<div class="grid">
      <label class="field">Method (By-Law 7.1, s. 23)<select name="method"><option>Government-issued photo ID, in person</option><option>Government-issued photo ID, video with authentication</option><option>Credit file (Canadian, 3 years)</option><option>Dual process (two reliable sources)</option>${l.kind === "org" ? h`<option>Corporate registry search (organization)</option>` : ""}</select></label>
      <label class="field">Document<input name="document" value="${l.kind === "org" ? "Ontario Business Registry profile report" : "Ontario driver's licence"}"></label>
      <label class="field">Number (last 4)<input name="last4" maxlength="4" value="4417"></label>
      <label class="field">Expiry<input name="expiry" type="date" value="2029-03-31"></label></div>`,
      [{ label: "Cancel" }, { label: "Record verification", primary: true, run: (d) => {
        const v = A.formValues(d); const rec = { method: v.method, document: `${v.document} ····${v.last4}`, expiry: v.expiry, by: me().id, at: A.now() };
        if (idvApprovable()) {
          S.put("leads", { id: l.id, idv: { ...rec, status: "pending" } });
          A.gate({ kind: "identity", cap: "conflicts.identify", title: `Verify identity: ${l.name}`, summary: `${rec.method}; ${rec.document}, expires ${rec.expiry}.`, preparedBy: me().id, checks: [{ label: "Document is current", ok: rec.expiry > A.today, blocking: true }], effect: "intake-idv", payload: { leadId: l.id, method: rec.method, document: rec.document } });
        } else {
          S.put("leads", { id: l.id, idv: { ...rec, status: rec.expiry > A.today ? "verified" : "expired", verifiedBy: me().id } });
          A.audit("client.id.verified", { target: l.id, note: `${rec.method}; ${rec.document}` });
        }
        A.toast("Identity verification recorded", "ok");
      } }]);
  });
  A.action("intake-engage", (el) => {
    const l = getL(el); if (!l || !l.conflict || l.conflict.status !== "cleared" || pendingGate(l.id, "intake-engage") || !A.can("documents.release", "act")) return;
    A.agent.run({ title: `Engagement letter: ${l.name}`, steps: ["Picking the fee model", "Adding the limitation date", "Drafting the letter"], done: () => A.gate({
      kind: "letter", cap: "documents.release", title: `Engagement letter: ${l.name}`, summary: `Retainer letter for ${l.area.toLowerCase()}, responsible lawyer ${A.user(l.assignee).name}.`, preparedBy: me().id, body: engagementBody(l),
      cites: [cite(l, 1)], checks: [{ label: "Conflicts cleared", ok: true, blocking: true }, { label: "Identity verified", ok: !!(l.idv && l.idv.status === "verified") }], effect: "intake-engage", payload: { leadId: l.id } }) });
    S.put("leads", { id: l.id, stage: "Engagement" });
  });
  A.action("intake-matter", (el) => {
    const l = getL(el); if (!l || !readyToOpen(l) || pendingGate(l.id, "intake-open") || !A.can("intake.convert", "act")) return;
    const k = kyc(l);
    A.gate({ kind: "matter", cap: "intake.convert", title: `Open matter: ${l.name}`, summary: `Creates the client record and a new ${l.area.toLowerCase()} matter, responsible ${A.user(l.assignee).name}. Client risk: ${k.level}.`, preparedBy: me().id,
      body: String(h`<p><b>Client risk rating: ${k.level}</b></p><ul>${k.reasons.map((x) => h`<li>${x}</li>`)}</ul>`),
      checks: [{ label: `Client risk rated ${k.level}${k.level === "high" ? " (enhanced review: confirm source of funds before any deposit)" : ""}`, ok: k.level !== "high" }, { label: "Conflicts cleared", ok: true, blocking: true }, { label: "Engagement letter approved", ok: true, blocking: true }, { label: "Identity verified (By-Law 7.1)", ok: !!(l.idv && l.idv.status === "verified") }], effect: "intake-open", payload: { leadId: l.id } });
    A.toast("Sent for approval");
  });
  /* email intake agent */
  const ackText = (e) => `Thank you for contacting Linda K Law. We received your message about “${e.subject}”. A member of our team will review it and contact you within one business day. Please do not send original documents yet. ${DISCLOSE}`;
  function leadFromEmail(e, by) {
    const id = A.id("lead"), addr = (/<([^>]+)>/.exec(e.from) || [])[1] || e.from;
    const hits = search(e.name, "client"), pre = resultOf(hits);
    S.put("leads", { id, name: e.name, kind: /inc|ltd|corp/i.test(e.name) ? "org" : "person", email: addr, phone: "", source: "Email", received: e.at, area: e.area || "General", conf: e.area ? 0.85 : 0.5,
      urgency: e.discovered && A.daysBetween(A.today, addYears(e.discovered, 2)) < 90 ? "high" : e.discovered ? "medium" : "low", discovered: e.discovered, limit: e.discovered ? "basic" : "none",
      summary: e.body.length > 180 ? e.body.slice(0, 180) + "…" : e.body, parties: [{ name: e.name, role: "client" }].concat(e.adverse ? [{ name: e.adverse, role: "adverse" }] : []),
      assignee: /real estate|commercial/i.test(e.area) ? "u-yoni" : /estate/i.test(e.area) ? "u-linda" : "u-ayo", why: "Matched by practice area.", stage: "New", agentHandling: true,
      prescreen: { result: pre === "conflict" ? "hit" : pre === "review" ? "possible" : "clear", note: hits.length ? hits.map((x) => `${x.hit}: ${x.where}`).join("; ") : "No match in contacts, matters or documents." },
      facts: [["From", e.from, 1], ["Subject", e.subject, 2], ["Message", e.body.slice(0, 90), 4]], raw: [[`From: ${e.from}`, `Subject: ${e.subject}`, `Date: ${e.at}`, e.body]] });
    S.put("intakeEmails", { id: e.id, status: "lead", leadId: id, by });
    return id;
  }
  A.action("intake-mail", () => {
    if (!A.can("intake.leads", "act")) return;
    const fresh = S.where("intakeEmails", (e) => e.status === "new"); if (!fresh.length) return A.toast("No new email in the intake mailbox");
    const by = me().id;
    A.agent.run({ title: `Read ${fresh.length} intake emails`, steps: ["Reading new email", "Separating enquiries from other mail", "Creating leads and pre-screening conflicts", "Drafting acknowledgements"], done: () => {
      const made = [];
      fresh.forEach((e) => {
        if (!e.name || /no-reply|noreply/i.test(e.from)) S.put("intakeEmails", { id: e.id, status: "ignored", reason: "Automated message, not an enquiry." });
        else if (!e.area) S.put("intakeEmails", { id: e.id, status: "review", reason: "No practice area or facts; a person should decide." });
        else { leadFromEmail(e, by); made.push(e); }
      });
      A.audit("intake.email_triaged", { actor: "agent", note: `${made.length} leads from ${fresh.length} emails` });
      if (!made.length) return { title: "No new leads" };
      return A.gate({ kind: "letter", cap: "intake.convert", title: `Send ${made.length} intake acknowledgement${made.length > 1 ? "s" : ""}`, summary: "Replies from the intake assistant. They confirm receipt only and say plainly they are not from a lawyer.", preparedBy: "agent",
        body: String(h`${made.map((e) => h`<p><b>To ${e.from}</b><br>${ackText(e)}</p>`)}`), effect: "intake-ack", payload: { emailIds: made.map((e) => e.id) },
        checks: [{ label: "Each reply says it is from an intake assistant, not a lawyer", ok: true, blocking: true }, { label: "No legal advice or opinion on merits", ok: true }, { label: "No promise to act", ok: true }] });
    } });
    A.toast("The intake assistant is reading the mailbox");
  });
  A.action("intake-mail-lead", (el) => { const e = S.get("intakeEmails", el.dataset.id); if (!e || e.status !== "review" || !A.can("intake.leads", "act")) return; const id = leadFromEmail({ ...e, name: e.name || e.from, area: e.area || "General" }, me().id); A.audit("intake.email_promoted", { target: e.id }); A.go(`intake/${id}`); });
  A.action("intake-mail-drop", (el) => { const e = S.get("intakeEmails", el.dataset.id); if (!e || e.status !== "review" || !A.can("intake.leads", "act")) return; S.put("intakeEmails", { id: e.id, status: "ignored", reason: `Not a lead (${me().name})` }); A.audit("intake.email_dismissed", { target: e.id }); });
  A.action("intake-takeover", (el) => {
    const l = getL(el); if (!l || !l.agentHandling || !A.can("intake.leads", "act")) return;
    S.put("leads", { id: l.id, agentHandling: false, handler: me().id, takenAt: A.now() }); A.audit("lead.taken_over", { target: l.id, note: `${me().name} took over from the intake assistant` }); A.toast("You now handle this lead; the assistant will not reply", "ok");
  });
  A.action("intake-retainer", (el) => {
    const l = getL(el); if (!l || !A.can("documents.release", "act") || (l.retainer && l.retainer.status) || pendingGate(l.id, "intake-retainer")) return;
    A.modal(`Retainer request: ${l.name}`, h`<div class="form"><label class="field">Amount<input name="amount" type="number" min="1" step="50" class="input" value="${/injury/i.test(l.area) ? 500 : /real estate/i.test(l.area) ? 1500 : 2500}"></label>
      <label class="field">Payment method<select name="method"><option>Bank e-transfer</option><option>Card (payment link)</option><option>Cheque</option></select></label>
      <label class="field">Deposit to<select name="acct" disabled><option>Trust account</option></select></label></div>
      <p class="muted small">Money received before work is done belongs to the client and always goes to trust (By-Law 9). It moves to the firm only against an issued account.</p>`,
    [{ label: "Cancel" }, { label: "Prepare request", primary: true, run: (d) => {
      const v = A.formValues(d), amount = +v.amount, cents = Math.round(amount * 100); if (!(amount > 0)) { A.toast("Enter an amount", "bad"); return false; }
      if (amount >= 7500 && v.method === "Cheque") A.toast("Large cheque: confirm source of funds", "bad");
      A.gate({ kind: "letter", cap: "documents.release", title: `Retainer request to ${l.name}: ${A.cents(cents)} to trust`, summary: `${v.method}. Paid into the trust account and held for ${l.name}.`, preparedBy: me().id,
        body: String(h`<p>Dear ${l.contactName ? l.contactName.split(",")[0] : l.name},</p><p>To begin work we ask for a retainer of <b>${A.cents(cents)}</b>. It will be held in our trust account in your name and applied only to accounts we send you. Pay by ${v.method.toLowerCase()} using the details on the attached sheet.</p><p>Yours truly,<br>LINDA K LAW</p>`),
        checks: [{ label: "Deposit goes to the trust account, never the general account", ok: true, blocking: true }, { label: "Conflicts cleared", ok: !!(l.conflict && l.conflict.status === "cleared"), blocking: true }, { label: "Identity verified before funds are accepted", ok: !!(l.idv && l.idv.status === "verified") }, { label: "Not a cash payment over $7,500", ok: true, blocking: true }],
        effect: "intake-retainer", payload: { leadId: l.id, cents, method: v.method } });
      A.toast("Retainer request sent for approval");
    } }]);
  });
  let sq = { q: "Brightwater", mode: "all", exact: false, results: null };
  A.action("intake-search", (el) => {
    if (!A.can("conflicts.run", "act")) return;
    const card = el.closest(".card"), v = A.formValues(card);
    sq = { q: (v.q || "").trim(), mode: v.mode, exact: !!v.exact, results: null };
    if (sq.q.length < 3) { A.toast("Type at least 3 characters", "bad"); return A.refresh(); }
    sq.results = sq.mode === "keyword" ? kwSearch(sq.q) : search(sq.q, "client", { mode: sq.mode, exact: sq.exact }).concat(search(sq.q, "adverse", { mode: sq.mode, exact: sq.exact }).filter((x) => x.tone === "bad"));
    A.audit("conflict.search", { target: sq.q, note: `${sq.mode}${sq.exact ? ", full name" : ""}: ${sq.results.length} matches` });
    A.refresh();
  });
  const readyToOpen = (l) => l.conflict && l.conflict.status === "cleared" && l.engagement === "approved" && !l.matterId;

  /* ---------- daily-use layer: chips, filters, path to matter ---------- */
  const SEL = "intake-leads";
  const st = A.ui.status;
  const stageChip = (l) => st({ New: "new", Accepted: "open", "Conflict check": "in-progress", Consult: "scheduled", Engagement: "in-progress", Opened: "done", Declined: "declined" }[l.stage] || l.stage, l.stage);
  const CSTATUS = { running: ["running", "Agent searching"], clear: ["needs-approval", "Clear: needs sign-off"], review: ["to-review", "Review hits"], conflict: ["conflict", "Conflict"], cleared: ["approved", "Cleared"], "awaiting consent": ["waiting", "Awaiting consent"], declined: ["declined", "Declined"], blocked: ["blocked", "Blocked: consent refused"] };
  const conflictChip = (c) => (c ? st(...(CSTATUS[c.status] || [c.status])) : st("todo", "Not run"));
  const limChip = (lim) => (lim.deadline ? st({ expired: "overdue", "at risk": "blocked", watch: "due-soon", ok: "done" }[lim.flag], `Limitation ${lim.flag}`) : st("closed", "No limitation"));
  const fitChip = (f) => st({ good: "done", possible: "due-soon", poor: "failed" }[f.label], `${f.label} fit`);
  const riskChip = (k) => st({ low: "done", medium: "due-soon", high: "blocked" }[k.level], `${k.level} risk`);
  const hitChip = (x) => st(x.tone === "bad" ? "conflict" : x.tone === "warn" ? "to-review" : "done", x.tone === "bad" ? "Conflict" : x.tone === "warn" ? "Review" : "No conflict");
  const LAWYERS = () => S.where("users", (u) => ["owner", "lawyer", "associate"].includes(u.role));
  const opts = (list, cur) => list.map(([v, t]) => h`<option value="${v}" ${String(v) === String(cur ?? "") ? A.raw("selected") : ""}>${t}</option>`);
  const onOff = (b) => (b ? A.raw("checked") : "");
  const demo = h`<span class="muted small" title="Works on demo data; no message, call or payment leaves the app">demo</span>`;
  const formOf = (el) => A.formValues(el.closest(".card") || el.closest(".page") || document.body);
  let fq = "", ff = "open", sel = null;
  A.onUser(() => { sel = null; });

  /** Why an action is not allowed on this lead ("" when it is). Shared by buttons, keys and bulk actions. */
  function why(l, act) {
    const off = closed(l), c = l.conflict, cleared = c && c.status === "cleared";
    const pend = (eff) => !!pendingGate(l.id, eff);
    const stt = off ? `Lead is ${l.stage.toLowerCase()}` : "";
    switch (act) {
      case "intake-accept": return canWhy("intake.convert", "act", "accept leads") || (l.stage === "New" ? "" : `Already ${l.stage.toLowerCase()}`);
      case "intake-decline": return canWhy("intake.convert", "act", "decline leads") || stt || (pend("intake-decline") ? "A decline letter is awaiting approval" : "");
      case "intake-conflict": return canWhy("conflicts.run", "act", "run conflict searches") || stt || (pend("intake-clear") ? "A check is awaiting sign-off" : c && c.status === "running" ? "A check is running" : cleared ? "Conflicts already cleared" : "");
      case "intake-idv": return canWhy("conflicts.identify", "act", "verify identity") || stt || (l.idv && ["verified", "pending"].includes(l.idv.status) ? "Identity already recorded" : "");
      case "intake-engage": return canWhy("documents.release", "act", "prepare client letters") || stt || (!cleared ? "Clear conflicts first" : l.engagement === "approved" ? "Engagement letter already approved" : pend("intake-engage") ? "A draft is awaiting approval" : "");
      case "intake-retainer": return canWhy("documents.release", "act", "send retainer requests") || stt || (!cleared ? "Clear conflicts first" : l.retainer ? "Retainer already requested" : pend("intake-retainer") ? "A request is awaiting approval" : "");
      case "intake-matter": return canWhy("intake.convert", "act", "open matters") || (l.matterId ? "Matter already opened" : !cleared ? "Clear conflicts first" : l.engagement !== "approved" ? "Engagement letter must be approved first" : pend("intake-open") ? "Awaiting approval" : "");
      case "intake-consult": return canWhy("intake.leads", "act", "book consultations") || stt || (l.consult && l.consult.outcome ? "Consult already held" : "");
      case "intake-edit": return canWhy("intake.leads", "act", "edit leads") || (l.matterId ? "Matter opened; edit the matter instead" : "");
      case "intake-takeover": return canWhy("intake.leads", "act", "handle leads") || stt || (l.agentHandling ? "" : "You already handle this lead");
      case "intake-q-send": return canWhy("intake.leads", "act", "send questionnaires") || stt;
      default: return "";
    }
  }
  const lb = (l, label, a, o = {}) => { const w = why(l, a); return { label, a, id: l.id, disabled: !!w, why: w, ...o }; };

  /** The path from enquiry to open matter: who did each step, who must do the next one. */
  function path(l) {
    const c = l.conflict, cs = c && c.status, g = (eff) => pendingGate(l.id, eff);
    const idOk = l.idv && l.idv.status === "verified", cleared = cs === "cleared";
    return [
      { k: "triage", label: "Triage", st: "done", by: "agent", note: `${l.area} (${Math.round(l.conf * 100)}%), pre-screen ${l.prescreen.result}` },
      { k: "accept", label: "Accept or decline", st: l.stage === "New" ? "todo" : "done", act: "intake-accept", key: "a", note: l.stage === "New" ? "A person decides whether the firm looks at this" : l.acceptedBy ? `Accepted by ${A.user(l.acceptedBy).name}` : "" },
      { k: "conflict", label: "Conflict check and sign-off", st: cleared ? "done" : cs === "running" ? "agent" : g("intake-clear") || cs === "awaiting consent" ? "gate" : "todo", act: "intake-conflict", key: "c",
        note: cleared ? `${OUTCOMES[c.outcome || "cleared"]} by ${A.user(c.clearedBy).name}, ${A.dateTime(c.clearedAt)}` : cs === "running" ? "The agent is searching" : cs === "awaiting consent" ? "Signed subject to consent; waits for every signed consent" : g("intake-clear") ? `Result ${cs}; a lawyer signs the outcome` : c ? `Last result ${cs}; run again after changes` : "The agent searches every party; a lawyer signs the outcome" },
      { k: "consult", label: "Consultation", optional: true, st: l.consult && l.consult.outcome ? "done" : "todo", act: "intake-consult", key: "b",
        note: l.consult ? (l.consult.outcome ? `${l.consult.outcome} · ${A.user(l.consult.with).name}` : `Booked ${A.date(l.consult.date)} ${l.consult.time} with ${A.user(l.consult.with).name}; record the outcome`) : "Optional. Book it, then record retain, decline or refer" },
      { k: "idv", label: l.kind === "org" ? "Organization and owners (By-Law 7.1)" : "Identity (By-Law 7.1)", st: idOk ? "done" : g("intake-idv") || g("intake-org") ? "gate" : "todo", act: "intake-idv", key: "i", note: idOk ? `${l.idv.method}` : l.idv ? `Recorded, ${l.idv.status}` : "Identify before acting; verify before funds move" },
      { k: "engage", label: "Engagement letter", st: l.engagement === "approved" ? "done" : g("intake-engage") ? "gate" : cleared ? "todo" : "wait", act: "intake-engage", key: "l",
        note: l.engagement === "approved" ? "Released to the client" : g("intake-engage") ? "Drafted; another lawyer releases it" : cleared ? "The agent drafts it; a second person releases it" : "Waits for conflict clearance" },
      { k: "retainer", label: "Retainer to trust", optional: true, st: l.retainer ? "done" : g("intake-retainer") ? "gate" : cleared ? "todo" : "wait", act: "intake-retainer", key: "r",
        note: l.retainer ? `${A.cents(l.retainer.cents)} requested, ${l.retainer.method.toLowerCase()}` : "Optional. Money before work always goes to trust" },
      { k: "open", label: "Open matter", st: l.matterId ? "done" : g("intake-open") ? "gate" : readyToOpen(l) ? "todo" : "wait", act: "intake-matter", key: "o",
        note: l.matterId ? `Opened as ${(S.get("matters", l.matterId) || {}).number || "a matter"}` : "Creates the client, matter number and team after approval" },
    ];
  }
  const nextStep = (l) => { if (closed(l)) return null; const p = path(l); return p.find((x) => !x.optional && x.st !== "done") || p.find((x) => x.st !== "done" && x.st !== "wait") || null; };
  const nextText = (l) => {
    if (l.stage === "Declined") return "Declined";
    if (l.matterId) return "Matter opened";
    if (pendingGate(l.id, "intake-decline")) return "Needs approval: decline letter";
    const n = nextStep(l); if (!n) return "";
    return n.st === "gate" ? `Needs approval: ${n.label}` : n.st === "agent" ? `✦ Agent working: ${n.label}` : n.st === "wait" ? `Waiting: ${n.label}` : `Next: ${n.label}`;
  };
  const approvableHere = (l) => gatesFor(l.id).filter((g) => g.status === "pending" && A.canApprove(g).ok);
  const needsMe = (l) => !closed(l) && (l.assignee === me().id || approvableHere(l).length > 0 || (l.stage === "New" && A.can("intake.convert", "act")));
  const atRisk = (l) => !closed(l) && limitation(l).tone === "bad";
  const flagged = (l) => !closed(l) && (l.conflict ? ["conflict", "review", "blocked"].includes(l.conflict.status) : l.prescreen && l.prescreen.result !== "clear");
  const FILTERS = [
    { id: "me", label: "Needs me", f: needsMe }, { id: "open", label: "All open", f: (l) => !closed(l) }, { id: "new", label: "New", f: (l) => l.stage === "New" },
    { id: "risk", label: "Limitation at risk", f: atRisk }, { id: "flags", label: "Conflict flags", f: flagged }, { id: "closed", label: "Opened or declined", f: closed },
  ];
  function filtered() {
    const f = (FILTERS.find((x) => x.id === ff) || FILTERS[1]).f, q = fq.toLowerCase();
    const order = (l) => (closed(l) ? 9 : 0) + (l.stage === "New" ? -1 : 0) + (atRisk(l) ? -0.5 : 0) + ({ high: 0, medium: 1, low: 2 }[l.urgency] || 0) / 10;
    return S.all("leads").filter((l) => f(l) && (!q || `${l.name} ${l.area} ${l.summary} ${l.source} ${l.parties.map((p) => p.name).join(" ")}`.toLowerCase().includes(q))).sort((a, b) => order(a) - order(b) || (a.received < b.received ? 1 : -1));
  }

  /* ---------- page frame ---------- */
  const CONFLICT_TABS = [["conflicts", "Register"], ["consents", "Consents"], ["records", "Party records"], ["laterals", "Lateral hires"], ["client-id", "Client ID"], ["conflict-settings", "Search settings"]];
  const SETUP_TABS = [["forms", "Forms"], ["sequences", "Follow-ups"], ["receptionist", "AI receptionist"], ["consult-types", "Consult types"], ["settings", "Lists and rules"]];
  const ROUTES = new Set([...CONFLICT_TABS, ...SETUP_TABS].map((x) => x[0]).concat(["review", "sources"]));
  function head(active, o = {}) {
    const ls = S.all("leads");
    const signs = S.where("approvals", (g) => g.effect === "intake-clear" && g.status === "pending" && A.canApprove(g).ok).length;
    const tabs = [{ id: "list", label: "Inbox", count: ls.filter(needsMe).length }, { id: "board", label: "Pipeline" }, { id: "review", label: "Email intake", count: S.where("intakeEmails", (e) => e.status === "review").length },
      { id: "conflicts", label: "Conflicts", count: signs }, { id: "sources", label: "Sources" }, { id: "setup", label: "Setup" }];
    const sub = o.sub ?? h`✦ The agent triaged ${ls.filter((l) => l.stage === "New").length} new leads and pre-screened conflicts. You accept or decline; every letter and every new matter needs a second person.`;
    const sec = o.section === "conflicts" ? CONFLICT_TABS : o.section === "setup" ? SETUP_TABS : null;
    return h`${A.ui.header({ title: o.title || "Intake", sub, actions: o.actions || [], primary: o.primary !== undefined ? o.primary : A.can("intake.leads", "act") ? { label: "New lead", a: "intake-new", key: "n" } : null })}
      ${A.ui.tabs(tabs, active, "intake-view")}${sec ? A.ui.tabs(sec.map(([id, label]) => ({ id, label })), o.sub2, "intake-view") : ""}`;
  }
  A.action("intake-view", (el) => { const v = el.dataset.id; if (v === "setup") return A.go("intake/forms"); if (ROUTES.has(v)) return A.go(`intake/${v}`); view = v; A.go(v === "list" && sel && lead(sel) ? `intake/${sel}` : "intake"); });
  const page = (el, raw) => A.render(el, h`<div class="page">${raw}</div>`);
  const noRole = (what) => A.ui.empty(`Your role cannot see ${what}`, " Ask the office manager for access.");

  /* ---------- inbox, pipeline, lead ---------- */
  function stats() {
    const ls = S.all("leads"), active = ls.filter((l) => !closed(l));
    return h`<div class="stats">${A.ui.stat("Needs me", ls.filter(needsMe).length, "accept, act or approve")}
      ${A.ui.stat("Limitation at risk", active.filter(atRisk).length, "within 90 days", active.some(atRisk) ? "bad" : "")}
      ${A.ui.stat("Conflict flags", active.filter(flagged).length, "need review", active.some(flagged) ? "warn" : "")}
      ${A.ui.stat("Ready to open", active.filter(readyToOpen).length, "cleared and engaged")}</div>`;
  }
  function listPane(list, selId) {
    const canBulk = A.can("intake.leads", "act");
    const bar = A.ui.filters({ search: { c: "intake-q", value: fq, placeholder: "Filter leads" }, chips: FILTERS.map((x) => ({ id: x.id, label: x.label, count: S.all("leads").filter(x.f).length })), active: ff, action: "intake-filter" });
    const rows = list.map((l) => { const lim = limitation(l);
      return { id: l.id, on: l.id === selId, title: l.name, sub: `${l.area} · ${l.source} · ${A.dateTime(l.received)} · ${nextText(l)}`,
        right: h`${stageChip(l)}${l.urgency === "high" && !closed(l) ? st("overdue", "Urgent") : ""}${lim.deadline && lim.tone !== "ok" && !closed(l) ? limChip(lim) : ""}${flagged(l) ? st("to-review", "Conflict?") : ""}${A.ui.avatar(l.assignee)}` }; });
    const bulk = A.ui.bulkBar(SEL, [{ label: "Accept", a: "intake-bulk", data: { op: "accept" } }, { label: "✦ Run conflict checks", a: "intake-bulk", agent: true, data: { op: "conflict" } }, { label: "Assign…", a: "intake-bulk", data: { op: "assign" } }]);
    return h`${bar}${A.ui.list(rows, "intake-open", { select: canBulk ? SEL : null, empty: ff === "me" ? "Nothing needs you" : "No leads match", emptyHint: fq ? " Clear the filter to see more." : "" })}${bulk}`;
  }
  function inbox(el) {
    ensureDocs();
    if (view === "board") return board(el);
    const list = filtered();
    const l = (sel && lead(sel) && list.some((x) => x.id === sel) ? lead(sel) : list[0]) || null;
    if (l) sel = l.id;
    A.setContext(l ? { kind: "lead", id: l.id, label: l.name } : { kind: "intake", label: "Lead inbox" });
    page(el, h`${head("list")}${stats()}${A.ui.split(listPane(list, l && l.id), l ? leadPane(l) : null, { emptyTitle: "No lead selected", emptyHint: " Pick one on the left, or press n to record a new one." })}`);
  }
  function board(el) {
    A.setContext({ kind: "intake", label: "Intake pipeline" });
    const ls = S.all("leads");
    page(el, h`${head("board")}${stats()}<div class="board">${STAGES.map((s) => { const col = ls.filter((l) => l.stage === s); return h`<div class="lane"><h4>${s}<span>${col.length}</span></h4>${col.map((l) => { const lim = limitation(l);
      return h`<div class="kcard ${gatesFor(l.id).some((g) => g.status === "pending") ? "gate" : l.stage === "New" ? "agent" : ""}" data-a="intake-open" data-id="${l.id}"><b>${l.name}</b><small>${l.area} · ${l.source}</small>
        <div class="row wrap">${l.urgency === "high" ? st("overdue", "Urgent") : ""}${lim.flag !== "ok" && lim.flag !== "n/a" ? limChip(lim) : ""}${flagged(l) ? st("to-review", "Conflict?") : ""}${A.ui.avatar(l.assignee)}</div><small>${nextText(l)}</small></div>`; })}</div>`; })}</div>
      ${ls.some((l) => l.stage === "Declined") ? h`<p class="muted small">Declined: ${ls.filter((l) => l.stage === "Declined").map((l) => l.name).join(", ")}</p>` : ""}`);
  }
  function detail(el, { id }) {
    ensureDocs(); const l = lead(id);
    if (!l) return page(el, A.ui.empty("Lead not found", "", { label: "Back to intake", to: "intake" }));
    sel = l.id; view = "list";
    const list = filtered(); if (!list.some((x) => x.id === l.id)) list.unshift(l);
    A.setContext({ kind: "lead", id: l.id, label: l.name });
    page(el, h`${head("list")}${A.ui.split(listPane(list, l.id), leadPane(l))}`);
  }

  /** Right-hand pane: header with one primary action, path to matter, approvals waiting, then details on demand. */
  function leadPane(l) {
    const lim = limitation(l), c = l.conflict, off = closed(l), n = nextStep(l);
    const PRIMARY = { accept: "Accept", conflict: "✦ Run conflict check", consult: l.consult ? "Record consult outcome" : "Book consult", idv: "Verify identity", engage: "✦ Draft engagement letter", retainer: "Request retainer", open: "Open matter" };
    const pl = n && n.st === "todo" && n.act ? PRIMARY[n.k] || n.label : "";
    const pathCard = h`<section class="card"><h3>Path to matter</h3><ol class="ik-path">${path(l).map((s, i) => {
      const dot = s.st === "done" ? "✓" : s.st === "gate" ? "◆" : s.st === "agent" ? "✦" : String(i + 1);
      const who = s.st === "done" ? (s.by === "agent" ? A.ui.actor("agent", "Agent did this") : st("done")) : s.st === "gate" ? st("needs-approval") : s.st === "agent" ? st("running") : s.st === "wait" ? st("waiting", "Waiting") : s.optional ? st("todo", "Optional") : A.ui.actor("person");
      const can = s.st === "todo" && s.act && !off && !why(l, s.act);
      return h`<li><span class="ik-dot ${s.st === "todo" && !s.optional ? "todo" : s.st}">${dot}</span><div class="grow"><b class="small">${s.label}</b> ${who}<div class="muted small">${s.note}</div></div>
        ${can ? A.ui.btn({ label: s.k === "consult" && l.consult ? "Outcome" : "Do it", a: s.act, id: l.id, small: true, title: `Shortcut: ${s.key}` }) : ""}</li>`; })}</ol></section>`;
    const waiting = gatesFor(l.id).filter((g) => g.status === "pending"), donegates = gatesFor(l.id).filter((g) => g.status !== "pending");
    const handling = l.agentHandling ? h`<div class="card" style="margin-top:10px;padding:8px 12px"><div class="row"><span class="av agent">✦</span><span class="grow small"><b>The intake assistant is replying to this person.</b> Its messages say they come from an assistant, not a lawyer, and give no advice.</span>${A.ui.btn(lb(l, "Take over", "intake-takeover", { key: "t", small: true }))}</div></div>`
      : l.handler ? h`<p class="muted small" style="margin:8px 0 0">Handled by ${A.user(l.handler).name} since ${A.dateTime(l.takenAt)}; the assistant does not reply.</p>` : "";
    const f = fit(l), k = kyc(l), hits = (c && c.matches) || [];
    const qs = l.questionnaires || [];
    return h`${A.ui.header({ title: l.name, status: stageChip(l),
        sub: h`${l.source} · received ${A.dateTime(l.received)} · ${l.area} · ${A.ui.avatar(l.assignee)} ${A.user(l.assignee).name}${lim.deadline ? h` · ${limChip(lim)} ${lim.label}` : ""}`,
        actions: [lb(l, "Edit", "intake-edit", { key: "e" }), lb(l, "Decline", "intake-decline", { key: "d", danger: true })].concat(l.matterId ? [{ label: "Go to matter", to: `matters/${l.matterId}` }] : []),
        primary: pl ? lb(l, pl, n.act, { key: n.key, agent: pl[0] === "✦" }) : null })}
      ${handling}
      <section class="card" style="margin-top:10px"><h3>✦ Agent summary</h3><p style="margin:0 0 6px">${l.summary}</p>
        <p class="small" style="margin:0">${fitChip(f)} ${f.reasons.join(" · ")}</p>
        <div class="cites"><span class="muted">Source</span> ${A.ui.cite(cite(l, 1))}</div></section>
      ${waiting.length ? h`<h3 style="margin:14px 0 8px">Waiting for approval <span class="muted small">${waiting.length}</span></h3>${waiting.map((g) => A.ui.gateCard(g))}` : ""}
      <div style="margin-top:10px">${pathCard}</div>
      <details class="ik-more" ${c && c.status !== "cleared" ? A.raw("open") : ""}><summary>Conflict check ${conflictChip(c)}${hits.length ? h` <span class="muted small">${hits.filter((x) => x.disp).length} of ${hits.length} hits marked</span>` : ""}</summary>
        ${c ? h`<p class="small muted">Run by ${A.user(c.by).name} · ${A.dateTime(c.ranAt)}${c.clearedBy ? ` · ${OUTCOMES[c.outcome || "cleared"]} by ${A.user(c.clearedBy).name}, ${A.dateTime(c.clearedAt)}` : ""}</p>
          ${c.status === "running" ? h`<p class="muted">✦ Searching…</p>` : h`<p class="small">${hits.length} match${hits.length === 1 ? "" : "es"}: ${hits.filter((x) => x.tone === "bad").length} conflict, ${hits.filter((x) => x.tone === "warn").length} to review.</p>`}
          <div class="row wrap">${A.ui.btn({ label: "Open clearance memo", to: `intake/clearance/${l.id}`, small: true, primary: !!pendingGate(l.id, "intake-clear") })}${c.status === "awaiting consent" ? A.ui.btn({ label: "Consents", to: "intake/consents", small: true }) : ""}</div>`
          : h`<p class="muted">Not run yet. Pre-screen: ${l.prescreen.note}</p>`}
        <p class="small muted" style="margin:6px 0 0">Parties searched: ${l.parties.map((p) => `${p.name} (${p.role})`).join("; ")}.</p></details>
      <details class="ik-more"><summary>Facts and contact</summary>
        <dl class="kv"><dt>Contact</dt><dd>${l.contactName ? l.contactName + " · " : ""}${l.email || "no email"} · ${l.phone || "no phone"}</dd><dt>Why this lawyer</dt><dd>${l.why}</dd>${l.referredBy ? h`<dt>Referred by</dt><dd>${l.referredBy}</dd>` : ""}</dl>
        ${A.ui.table([{ label: "Fact", key: 0 }, { label: "Value", key: 1 }, { label: "Source", render: (x) => A.ui.cite(cite(l, x[2])) }], l.facts.map((x, i) => Object.assign([...x], { id: i })))}</details>
      <details class="ik-more" ${qs.length ? A.raw("open") : ""}><summary>Questionnaire ${qs.length ? st(qs[0].status === "submitted" ? "done" : qs[0].status === "expired" ? "overdue" : "sent", qs[0].status) : st("todo", "Not sent")}</summary>${questionnaireCard(l)}</details>
      <details class="ik-more" ${lim.tone === "bad" && !off ? A.raw("open") : ""}><summary>Limitation ${limChip(lim)}</summary>${lim.deadline ? h`<dl class="kv"><dt>Discovered</dt><dd>${A.date(l.discovered)}</dd><dt>Basic deadline</dt><dd><b>${A.date(lim.deadline)}</b> · ${lim.left} days</dd><dt>Ultimate</dt><dd>${A.date(addYears(l.discovered, 15))}</dd></dl>
        <p class="small muted">${RULE}</p><p class="small">Discovery date comes from the intake; confirm it with the client before relying on it.</p>` : h`<p class="muted">A transaction, not a claim: no limitation period runs. Diarize the closing date instead.</p>`}</details>
      <details class="ik-more"><summary>Identity and client risk ${riskChip(k)}</summary>
        ${l.idv ? h`<dl class="kv"><dt>Status</dt><dd>${st(l.idv.status === "verified" ? "done" : "pending", l.idv.status)}</dd><dt>Method</dt><dd>${l.idv.method}</dd><dt>Document</dt><dd>${l.idv.document}${l.idv.expiry ? `, expires ${A.date(l.idv.expiry)}` : ""}</dd><dt>Recorded by</dt><dd>${A.user(l.idv.by).name} · ${A.dateTime(l.idv.at)}</dd></dl>` : h`<p class="muted">Not verified. Identify the client before acting; verify before any funds move.</p>`}
        ${l.kind === "org" ? h`<div class="row wrap" style="margin-top:6px">${A.ui.btn({ label: "Organization and beneficial owners", to: `intake/org/${l.id}`, small: true })}${l.org ? st(l.org.status === "verified" ? "done" : "in-progress", l.org.status) : ""}</div>` : ""}
        <ul class="small" style="margin:6px 0 0;padding-left:18px">${k.reasons.map((x) => h`<li>${x}</li>`)}</ul><p class="muted small" style="margin:6px 0 0">Shown on the open-matter approval. High risk needs source-of-funds confirmation before any deposit.</p></details>
      ${donegates.length ? h`<details class="ik-more"><summary>Decided approvals <span class="muted small">${donegates.length}</span></summary>${donegates.map((g) => A.ui.gateCard(g, { compact: true }))}</details>` : ""}`;
  }

  function review(el) {
    A.setContext({ kind: "intake", label: "Email intake" });
    const all = S.all("intakeEmails"), canAct = A.can("intake.leads", "act"), cw = canWhy("intake.leads", "act", "handle leads");
    const fresh = all.filter((e) => e.status === "new").length, rev = all.filter((e) => e.status === "review");
    const word = { new: "new", review: "to-review", lead: "done", ignored: "closed" };
    const acks = S.where("approvals", (g) => g.effect === "intake-ack" && g.status === "pending");
    page(el, h`${head("review", { sub: "intake@lindaklaw.example.ca · The intake assistant turns enquiries into leads, files automated mail, and leaves anything unclear for a person. Its replies always say they come from an assistant, not a lawyer.",
        actions: [{ label: `✦ Read new email (${fresh})`, a: "intake-mail", agent: true, disabled: !canAct, why: cw }] })}
      ${acks.map((g) => A.ui.gateCard(g))}
      <h3 style="margin:14px 0 8px">For a person to decide <span class="muted small">${rev.length}</span></h3>
      ${rev.length ? rev.map((e) => h`<div class="card" style="margin-bottom:8px"><div class="row wrap"><span class="grow"><b>${e.subject}</b> <span class="muted small">${e.from} · ${A.dateTime(e.at)}</span><br><span class="small">${e.body}</span><br><span class="muted small">✦ ${e.reason}</span></span>
        ${A.ui.btn({ label: "Make a lead", a: "intake-mail-lead", id: e.id, small: true, primary: true, disabled: !canAct, why: cw })}${A.ui.btn({ label: "Not a lead", a: "intake-mail-drop", id: e.id, small: true, disabled: !canAct, why: cw })}</div></div>`) : A.ui.empty("Nothing waiting", " Unclear emails land here.")}
      <h3 style="margin:18px 0 8px">Mailbox</h3>
      ${A.ui.table([
        { label: "Received", render: (e) => A.dateTime(e.at) },
        { label: "From", render: (e) => h`<b>${e.from}</b><div class="muted small">${e.subject}</div>` },
        { label: "Outcome", render: (e) => h`${st(word[e.status] || e.status, e.status === "lead" ? "Lead created" : e.status === "ignored" ? "Filed" : e.status === "review" ? "To review" : "New")}${e.reason ? h`<div class="muted small">${e.reason}</div>` : ""}` },
        { label: "Reply", render: (e) => (e.replied ? h`<span class="small">Assistant reply sent ${A.dateTime(e.replied)}</span>` : h`<span class="muted small">None</span>`) },
      ], all, { onRow: "intake-reg", rowId: (e) => e.leadId || "" })}`);
  }
  function sources(el) {
    A.setContext({ kind: "intake", label: "Lead sources" });
    const ls = S.all("leads"), by = {};
    ls.forEach((l) => { const k = l.referredBy ? `Referral: ${l.referredBy}` : l.source; const r = by[k] || (by[k] = { id: k, n: 0, opened: 0, declined: 0, open: 0 }); r.n++; if (l.matterId) r.opened++; else if (l.stage === "Declined") r.declined++; else r.open++; });
    const rows = Object.values(by).sort((a, b) => b.n - a.n);
    page(el, h`${head("sources", { sub: "Where enquiries come from and how many become matters. Fees per source appear once matters bill." })}${A.ui.table([
      { label: "Source", render: (r) => h`<b>${r.id}</b>` }, { label: "Leads", num: true, render: (r) => r.n }, { label: "In progress", num: true, render: (r) => r.open },
      { label: "Opened", num: true, render: (r) => r.opened }, { label: "Declined", num: true, render: (r) => r.declined },
      { label: "Conversion", num: true, render: (r) => (r.opened + r.declined ? `${Math.round((r.opened / (r.opened + r.declined)) * 100)}%` : "—") },
    ], rows, { empty: "No leads yet" })}`);
  }
  A.action("intake-reg", (el) => { if (el.dataset.id) A.go(`intake/${el.dataset.id}`); });

  /* ---------- questionnaire (send, remind, resend, map answers) ---------- */
  function questionnaireCard(l) {
    const qs = l.questionnaires || [], w = why(l, "intake-q-send");
    return h`${qs.length ? A.ui.table([
      { label: "Form", render: (q) => h`<b>${q.form}</b><div class="muted small">${q.lang === "fr" ? "French" : "English"} · ${q.channel} · link expires ${A.date(q.expires)}</div>` },
      { label: "Status", render: (q) => h`${st(q.status === "submitted" ? "done" : q.status === "expired" ? "overdue" : q.status === "partial" ? "in-progress" : "sent", q.status)}${q.due ? h` ${A.ui.due(q.due, { done: q.status === "submitted" })}` : ""}${q.flag ? h`<div class="small">✦ ${q.flag}</div>` : ""}` },
      { label: "", render: (q) => h`<div class="row wrap">${q.status === "submitted" ? (q.mapped ? st("done", "Mapped") : A.ui.btn({ label: "✦ Map answers", a: "intake-q-map", id: l.id, data: { q: q.id }, small: true, agent: true, disabled: !!w, why: w }))
        : h`${A.ui.btn({ label: "Remind now", a: "intake-q-remind", id: l.id, data: { q: q.id }, small: true, disabled: !!w, why: w })}${A.ui.btn({ label: "Resend fresh link", a: "intake-q-resend", id: l.id, data: { q: q.id }, small: true, disabled: !!w, why: w })}${A.ui.btn({ label: "Simulate reply", a: "intake-q-reply", id: l.id, data: { q: q.id }, small: true, disabled: !!w, why: w })}`}</div>` },
    ], qs) : h`<p class="muted small">✦ The agent picks the form for ${l.area.toLowerCase()} and prefills it from the lead. The prospect can correct anything.</p>`}
      <div class="row wrap" style="margin-top:6px">${A.ui.btn({ label: "Send questionnaire", a: "intake-q-send", id: l.id, small: true, disabled: !!w, why: w })}${demo}</div>`;
  }
  const formsFor = (area) => { const f = S.where("intakeForms", (x) => x.status === "published"); return f.filter((x) => x.area === area).concat(f.filter((x) => x.area !== area)); };
  const qUpdate = (l, qid, patch, note) => { const qs = (l.questionnaires || []).map((q) => (q.id === qid ? { ...q, ...patch, history: [...(q.history || []), [A.now(), note]] } : q)); S.put("leads", { id: l.id, questionnaires: qs }); A.audit("questionnaire." + note.split(" ")[0].toLowerCase(), { target: l.id, note }); };
  A.action("intake-q-send", (el) => {
    const l = getL(el); if (!l || why(l, "intake-q-send")) return;
    const forms = formsFor(l.area); if (!forms.length) return A.toast("Publish a form first (Setup → Forms)", "bad");
    A.modal(`Send questionnaire: ${l.name}`, h`<div class="grid g2">
      <label class="field">Form<select name="form">${opts(forms.map((f) => [f.id, `${f.name} (v${f.version})`]), forms[0].id)}</select></label>
      <label class="field">Language<select name="lang">${opts([["en", "English"], ["fr", "French"]], "en")}</select></label>
      <label class="field">Channel<select name="channel">${opts([["Email", "Email"], ["SMS", "Text message"], ["Email and SMS", "Email and text"]], l.email ? "Email" : "SMS")}</select></label>
      <label class="field">Due<input name="due" type="date" class="input" value="${A.addDays(A.today, 5)}"></label>
      <label class="field">Link expires after (days)<input name="exp" type="number" min="1" max="30" class="input" value="7"></label></div>
      <p class="muted small">Prefilled with the name, contact details, other side and key date from the lead. The link is bound to the prospect's email or phone. Wording comes from the approved form, so no approval is needed.</p>`,
    [{ label: "Cancel" }, { label: "Send", primary: true, run: (d) => {
      const v = A.formValues(d), f = S.get("intakeForms", v.form); if (!f) return false;
      if (/SMS/.test(v.channel) && !l.phone) { A.toast("No phone number on the lead", "bad"); return false; }
      const q = { id: A.id("q"), form: f.name, version: f.version, lang: v.lang, channel: v.channel, due: v.due, expires: A.addDays(A.today, +v.exp || 7), status: "sent", history: [[A.now(), `Sent by ${me().name}`]] };
      S.put("leads", { id: l.id, questionnaires: [q, ...(l.questionnaires || [])] }); A.audit("questionnaire.sent", { target: l.id, note: f.name }); A.toast("Questionnaire sent", "ok");
    } }]);
  });
  const qOf = (el) => { const l = getL(el); return l && !why(l, "intake-q-send") ? [l, el.dataset.q] : []; };
  A.action("intake-q-remind", (el) => { const [l, q] = qOf(el); if (l) { qUpdate(l, q, {}, `Reminder sent by ${me().name}`); A.toast("Reminder sent", "ok"); } });
  A.action("intake-q-resend", (el) => { const [l, q] = qOf(el); if (l) { qUpdate(l, q, { status: "sent", expires: A.addDays(A.today, 7) }, "Fresh link sent; the old link no longer works"); A.toast("Fresh link sent", "ok"); } });
  A.action("intake-q-reply", (el) => { const [l, q] = qOf(el); if (!l) return; const cur = (l.questionnaires || []).find((x) => x.id === q); qUpdate(l, q, { status: cur && cur.status === "partial" ? "submitted" : "partial" }, cur && cur.status === "partial" ? "Submitted by the prospect" : "Partly answered and saved by the prospect"); });
  A.action("intake-q-map", (el) => {
    const [l, q] = qOf(el); if (!l) return;
    A.agent.run({ title: `Map questionnaire answers: ${l.name}`, steps: ["Reading the answers", "Matching them to lead fields", "Comparing with earlier notes"], done: () => {
      const d = l.discovered, alt = d ? A.addDays(d, 1) : "";
      qUpdate(lead(l.id), q, { mapped: true, flag: d ? `Answer gives the key date as ${A.date(alt)}; the intake note says ${A.date(d)}. Both kept; a lawyer confirms.` : "Answers match the intake note." }, "Answers mapped to the lead by the agent");
      return { title: "Answers mapped", outputs: [{ label: "Open lead", go: `intake/${l.id}` }] };
    } });
  });

  /* ---------- conflict register, clearance (four outcomes), consents ---------- */
  A.seed("intakeConfig", [
    { id: "conflict", sources: ["contacts", "parties", "leads", "sealed", "docnames", "doctext", "laterals"], match: "similar", threshold: 60, autoClose: 5, stale: 90, identifiers: ["name", "alias", "email", "phone", "address", "business number"] },
    { id: "receptionist", status: "live", version: 2, channels: ["after hours", "web chat"], hours: "Weekdays 6:00 p.m.–8:30 a.m., weekends", greeting: "Linda K Law, this is the firm's automated intake assistant.", emergency: "violence, unsafe, arrested today, court tomorrow, suicide", recording: true, transfer: "Meraf Abay (paralegal), 416-555-0110", draft: null },
    { id: "lists", sources: [{ name: "Web form", kind: "web", active: true }, { name: "Phone note", kind: "phone", active: true }, { name: "Email", kind: "web", active: true }, { name: "Referral", kind: "referral person", active: true }, { name: "Google Ads", kind: "ad", active: true }, { name: "google ads", kind: "ad", active: true }, { name: "AI receptionist", kind: "AI receptionist", active: true }, { name: "Walk-in", kind: "phone", active: true }],
      areas: [{ name: "Commercial litigation", lawyer: "u-yoni", type: "ct-paid" }, { name: "Personal injury", lawyer: "u-ayo", type: "ct-free" }, { name: "Employment", lawyer: "u-ayo", type: "ct-free" }, { name: "Real estate", lawyer: "u-yoni", type: "ct-free" }, { name: "Estate litigation", lawyer: "u-linda", type: "ct-paid" }, { name: "Estates", lawyer: "u-linda", type: "ct-paid" }],
      stages: STAGES.map((n, i) => ({ name: n, cat: n === "Opened" ? "retained" : "intake", sla: [1, 2, 2, 5, 7, 0][i] })).concat([{ name: "Declined", cat: "not retained", sla: 0 }]),
      service: ["Ontario: Greater Toronto Area", "Ontario: Durham, Peel, York, Halton"], scoring: [{ name: "Practice fit", weight: 40 }, { name: "Urgency", weight: 25 }, { name: "Value band", weight: 15 }, { name: "Location", weight: 10 }, { name: "Conflict pre-check", weight: 10 }], scoreVersion: 1,
      decline: ["Outside practice areas", "Conflict of interest", "Limitation expired", "Fees outweigh recovery", "Prospect chose another firm", "No reply"], key: "lck_live_••••7f2a", keyRotated: "2026-06-01" },
  ]);
  A.seed("consents", [
    { id: "cn-haile", leadId: null, matterId: "m5", party: "Tsion Haile", type: "advance waiver", status: "signed", ila: true, disclosure: "The firm acted for a former manager of Northgate Logistics Inc. on an unrelated lease in 2023 and holds no confidential information relevant to your claim.", scope: "Haile v. Northgate Logistics only.", trail: [["2026-09-09 15:40", "Drafted by the agent"], ["2026-09-09 16:10", "Approved by Linda Kassim"], ["2026-09-10 11:02", "Signed by Tsion Haile (e-signature)"]] },
  ]);
  const ccfg = () => S.get("intakeConfig", "conflict") || { threshold: 60, stale: 90, sources: [] };
  const checkAge = (r) => A.daysBetween(r.ranAt.slice(0, 10), A.today);
  let regF = "all";
  function register(el) {
    A.setContext({ kind: "intake", label: "Conflict register" });
    const stale = ccfg().stale, all = [...S.all("conflictChecks")].sort((a, b) => (a.ranAt < b.ranAt ? 1 : -1));
    const F = { all: () => true, open: (r) => !r.clearedBy, stale: (r) => r.clearedBy && !r.matterId && checkAge(r) > stale };
    const rows = all.filter(F[regF] || F.all), canRun = A.can("conflicts.run", "act");
    const dis = canRun ? "" : A.raw(`disabled title="${A.esc(canWhy("conflicts.run", "act", "run conflict searches"))}"`);
    const searchCard = h`<section class="card" style="margin-bottom:14px"><h3>Quick name search</h3><div class="row wrap">
      <input name="q" class="input grow" value="${sq.q}" placeholder="Name, company or keyword" ${dis}>
      <select name="mode" ${dis}>${opts([["all", "Names and documents"], ["names", "Names only (contacts and parties)"], ["keyword", "Keyword in documents"]], sq.mode)}</select>
      <label class="row small"><input type="checkbox" name="exact" ${onOff(sq.exact)} ${dis}> Full name only</label>
      ${A.ui.btn({ label: "Search", a: "intake-search", agent: true, disabled: !canRun, why: canWhy("conflicts.run", "act", "run conflict searches") })}</div>
      <p class="muted small" style="margin:6px 0 0">For a caller on the line, before facts are taken. Searches every matter, including closed and restricted ones (shown by number only).</p>
      ${sq.results ? A.ui.table([
        { label: "Match", render: (x) => h`<b>${x.hit}</b> <span class="muted small">${Math.round(x.score * 100)}%</span><div class="muted small">${x.where}</div>${x.cite ? A.ui.cite(x.cite) : ""}` },
        { label: "Assessment", render: (x) => h`${hitChip(x)}<div class="small">${x.note}</div>` },
      ], sq.results.map((x, i) => ({ ...x, id: i })), { empty: `No matches for “${sq.q}”` }) : ""}</section>`;
    const nStale = all.filter(F.stale).length;
    page(el, h`${head("conflicts", { section: "conflicts", sub2: "conflicts", sub: "Every check, its outcome and its age. A lawyer signs each outcome; stale checks are re-run before a matter opens." })}${searchCard}
      ${A.ui.filters({ chips: [{ id: "all", label: "All", count: all.length }, { id: "open", label: "Not signed", count: all.filter(F.open).length }, { id: "stale", label: `Stale (over ${stale} days)`, count: nStale }], active: regF, action: "intake-regf",
        right: h`${A.ui.btn({ label: "✦ Re-run stale checks", a: "intake-rerun", agent: true, small: true, disabled: !canRun || !nStale, why: !canRun ? canWhy("conflicts.run", "act", "run checks") : "No stale checks" })}${A.ui.btn({ label: "Export register", a: "intake-reg-export", small: true, disabled: !A.can("conflicts.clear", "approve"), why: "Lawyer only; the export is logged" })}` })}
      ${A.ui.table([
      { label: "Run", render: (r) => h`${A.dateTime(r.ranAt)}<div class="muted small">${checkAge(r)} days ago</div>` },
      { label: "Prospect or client", render: (r) => h`<b>${r.name}</b><div class="muted small">${r.parties}</div>` },
      { label: "Result", render: (r) => st(r.result === "clear" ? "done" : r.result === "conflict" ? "conflict" : "to-review", r.result) },
      { label: "Matches", num: true, render: (r) => r.matches },
      { label: "Run by", render: (r) => A.user(r.by).name },
      { label: "Signed", render: (r) => (r.clearedBy ? h`${OUTCOMES[r.outcome || "cleared"]}<div class="muted small">${A.user(r.clearedBy).name} · ${A.dateTime(r.clearedAt)}</div>` : st("needs-approval", "Needs sign-off")) },
      { label: "Matter", render: (r) => { const m = r.matterId && A.matter(r.matterId); return m ? m.number : ""; } },
    ], rows, { onRow: "intake-reg-open", rowId: (r) => r.leadId || "", empty: "No checks match" })}`);
  }
  A.action("intake-regf", (el) => { regF = el.dataset.id; A.refresh(); });
  A.action("intake-reg-open", (el) => { if (el.dataset.id && lead(el.dataset.id)) A.go(`intake/clearance/${el.dataset.id}`); });
  A.action("intake-rerun", () => {
    if (!A.can("conflicts.run", "act")) return;
    const stale = ccfg().stale, ls = S.all("conflictChecks").filter((r) => r.clearedBy && !r.matterId && checkAge(r) > stale).map((r) => lead(r.leadId)).filter((l) => l && !closed(l));
    if (!ls.length) return A.toast("No stale checks on open leads");
    ls.forEach(runCheck); A.toast(`Re-running ${ls.length} check${ls.length > 1 ? "s" : ""}`, "ok");
  });
  A.action("intake-reg-export", () => { if (!A.can("conflicts.clear", "approve")) return; A.audit("conflict.register_exported", { note: `${S.all("conflictChecks").length} checks, CSV` }); A.toast("Register exported to CSV (demo); the export is in the audit log", "ok"); });

  function clearance(el, { id }) {
    const l = lead(id); if (!l) return page(el, A.ui.empty("Lead not found", "", { label: "Back to the register", to: "intake/conflicts" }));
    A.setContext({ kind: "lead", id: l.id, label: l.name });
    const c = l.conflict, g = pendingGate(l.id, "intake-clear"), hits = (c && c.matches) || [];
    const canDisp = A.can("conflicts.run", "act") && c && !c.clearedBy, ca = g ? A.canApprove({ ...g, checks: [] }) : null; /* outcome-specific checks are applied on sign-off */
    const res = c ? (["clear", "review", "conflict"].includes(c.status) ? c.status : resultOf(hits)) : "";
    const memo = g ? A.raw(g.body) : c && c.status !== "running" ? A.raw(memoBody(l, hits, res)) : "";
    const open = [hits.some((x) => !x.disp) ? `${hits.filter((x) => !x.disp).length} hit(s) not marked` : "", !l.email ? "No email on file (identifier missing)" : "", c && c.ranAt && checkAge(c) > ccfg().stale ? "Check is stale; re-run before signing" : ""].filter(Boolean);
    const matchRows = A.ui.table([
      { label: "Searched", render: (x) => h`${x.party}<div class="muted small">${x.role}</div>` },
      { label: "Match", render: (x) => h`<b>${x.hit}</b> <span class="muted small">${Math.round(x.score * 100)}%</span><div class="muted small">${x.where}</div>${x.cite ? A.ui.cite(x.cite) : ""}` },
      { label: "Assessment", render: (x) => h`${hitChip(x)}<div class="small">${x.note}</div>` },
      { label: "Your call", render: (x) => h`<select data-c="intake-hit" data-id="${l.id}" data-i="${x.id}" aria-label="Disposition" ${canDisp ? "" : A.raw(`disabled title="${A.esc(c && c.clearedBy ? "Signed; dispositions are locked" : canWhy("conflicts.run", "act", "mark hits"))}"`)}>${opts([["", "Not reviewed"], ["not relevant", "Not relevant"], ["relevant", "Relevant"]], x.disp || "")}</select>` },
    ], hits.map((x, i) => ({ ...x, id: i })), { empty: "No matches in contacts, matters or documents" });
    const staff = S.where("users", (u) => !["client"].includes(u.role));
    const decision = c && c.clearedBy ? h`<dl class="kv"><dt>Outcome</dt><dd><b>${OUTCOMES[c.outcome || "cleared"]}</b></dd><dt>Signed</dt><dd>${A.user(c.clearedBy).name} · ${A.dateTime(c.clearedAt)}</dd>
        ${(c.conditions || []).length ? h`<dt>Conditions</dt><dd>${c.conditions.join("; ")}</dd>` : ""}${(c.wall || []).length ? h`<dt>Screened out</dt><dd>${c.wall.map((u) => A.user(u).name).join(", ")}</dd>` : ""}${c.rationale ? h`<dt>Rationale</dt><dd>${c.rationale}</dd>` : ""}</dl>`
      : !g ? h`<p class="muted">${c && c.status === "running" ? "✦ The agent is searching." : "Run the check first; the agent drafts the memo, then a lawyer signs here."}</p>${A.ui.btn(lb(l, "✦ Run conflict check", "intake-conflict", { agent: true }))}`
      : h`<label class="field">Outcome<select name="outcome">${opts(Object.entries(OUTCOMES), res === "conflict" ? "decline" : res === "review" ? "consent" : "cleared")}</select></label>
        <label class="field">Conditions (one per line)<textarea name="conditions" rows="2" class="input" placeholder="Written consent from …; wall excluding …"></textarea></label>
        <label class="field">Who must consent (for consent outcome, one per line)<textarea name="parties" rows="2" class="input">${[...new Set(hits.filter((x) => x.tone !== "ok" && !x.cite).map((x) => x.hit))].join("\n")}</textarea></label>
        <div class="field"><span>Screen out (for a wall)</span><div class="row wrap small">${staff.map((u) => h`<label class="row"><input type="checkbox" name="wall_${u.id}"> ${u.name}</label>`)}</div></div>
        <label class="field">Rationale in your words<textarea name="rationale" rows="3" class="input"></textarea></label>
        <label class="row small"><input type="checkbox" name="adopt"> I adopt the agent's memo as my reasons</label>
        <div class="row wrap" style="margin-top:8px">${A.ui.btn({ label: "Sign off", a: "intake-sign", id: l.id, primary: true, disabled: !ca.ok, why: ca.why })}${A.ui.btn({ label: "Send back for more search", a: "intake-sign-back", id: l.id, disabled: !(A.can("conflicts.clear", "approve") || g.preparedBy === me().id), why: "Lawyers with clearing authority" })}</div>
        <p class="muted small">Signing pins this result (${hits.length} hits, run ${A.dateTime(c.ranAt)}). Until it is signed the matter cannot open, no engagement letter goes out and no trust request is issued.</p>`;
    page(el, h`${A.ui.header({ title: `Conflict clearance: ${l.name}`, status: conflictChip(c), back: { label: l.name, to: `intake/${l.id}` }, sub: h`${l.area} · parties: ${l.parties.map((p) => `${p.name} (${p.role})`).join("; ")}`,
        actions: [{ label: "Check report (PDF)", a: "intake-report", id: l.id, disabled: !c || c.status === "running", why: "Run the check first" }] })}
      <div class="grid g2"><section class="card"><h3>✦ Agent memo</h3>${A.ui.actor("agent", "Agent drafted")}<div class="draft">${memo || h`<p class="muted">No memo yet.</p>`}</div>
        ${open.length ? h`<h3>Still outstanding</h3><ul class="small">${open.map((x) => h`<li>${x}</li>`)}</ul>` : ""}</section>
      <section class="card"><h3>Decision ${A.ui.actor("person", "Lawyer signs (G2)")}</h3>${decision}</section></div>
      <h3 style="margin:14px 0 8px">Hits</h3>${matchRows}`);
  }
  A.action("intake-sign", (el) => {
    const l = getL(el), g = l && pendingGate(l.id, "intake-clear"); if (!g) return;
    const v = formOf(el), oc = v.outcome || "cleared", conditions = (v.conditions || "").split("\n").map((x) => x.trim()).filter(Boolean);
    const wall = Object.keys(v).filter((k) => k.startsWith("wall_") && v[k]).map((k) => k.slice(5)), parties = (v.parties || "").split("\n").map((x) => x.trim()).filter(Boolean);
    const rationale = (v.rationale || "").trim() || (v.adopt ? "Adopts the agent's clearance memo." : "");
    if (!rationale) return A.toast("Write your rationale or adopt the memo", "bad");
    if (oc === "wall" && !wall.length) return A.toast("Pick who is screened out", "bad");
    if (oc === "consent" && !parties.length) return A.toast("Name who must consent", "bad");
    const res = l.conflict.status;
    const checks = oc === "decline" ? [{ label: "Decline: the non-engagement letter must not disclose the conflict", ok: true }]
      : [{ label: "No direct conflict with a current client", ok: res !== "conflict" || oc !== "cleared", blocking: true }, { label: "Every hit marked", ok: (l.conflict.matches || []).every((x) => x.disp) }, ...(oc === "wall" ? [{ label: "Screen covers the people named", ok: true }] : [])];
    const trial = { ...g, checks }, ok = A.canApprove(trial); if (!ok.ok) return A.toast(ok.why, "bad");
    S.put("approvals", { id: g.id, checks, payload: { ...g.payload, outcome: oc, conditions: oc === "wall" ? conditions.concat([`Wall excluding ${wall.map((u) => A.user(u).name).join(", ")}`]) : conditions, rationale, wall: oc === "wall" ? wall : [], consentParties: oc === "consent" ? parties : [] } });
    A.approve(g.id, rationale);
  });
  A.action("intake-sign-back", (el) => { const l = getL(el), g = l && pendingGate(l.id, "intake-clear"); if (g) A.sendBack(g.id, "More search needed before sign-off"); });
  A.action("intake-report", (el) => { const l = getL(el); if (!l || !l.conflict) return; A.agent.run({ title: `Check report: ${l.name}`, steps: ["Freezing searches and hits", "Adding dispositions and sign-off", "Filing the PDF"], done: () => { A.audit("conflict.report", { target: l.id, note: "Frozen check report generated" }); return { title: "Check report filed (demo PDF)" }; } }); });

  let cnSel = null, cnF = "open";
  function consents(el) {
    A.setContext({ kind: "intake", label: "Consents and waivers" });
    const all = S.all("consents"), F = { open: (x) => !["signed", "refused", "withdrawn"].includes(x.status), all: () => true, signed: (x) => x.status === "signed" };
    const list = all.filter(F[cnF] || F.all), cur = list.find((x) => x.id === cnSel) || list[0];
    const tone = { draft: "draft", "needs approval": "needs-approval", approved: "approved", sent: "sent", signed: "done", refused: "rejected", withdrawn: "closed" };
    const who = (x) => (x.leadId ? (lead(x.leadId) || {}).name : (A.matter(x.matterId) || { number: "restricted matter" }).number);
    const can = A.can("conflicts.run", "act"), cw = canWhy("conflicts.run", "act", "handle consents");
    const det = cur ? h`${A.ui.header({ title: cur.party, status: st(tone[cur.status] || cur.status, cur.status), sub: `${cur.type} · for ${who(cur)}`, actions: [
        { label: "✦ Draft consent letter", a: "intake-cn-draft", id: cur.id, agent: true, disabled: !can || cur.status !== "draft" || !!S.where("approvals", (g) => g.effect === "intake-cn-ok" && g.status === "pending" && g.payload.id === cur.id).length, why: !can ? cw : "Only a draft without a pending letter" },
        { label: "Send for e-signature", a: "intake-cn-send", id: cur.id, disabled: !can || cur.status !== "approved", why: !can ? cw : "A lawyer approves the letter first" },
        { label: "Chase", a: "intake-cn-chase", id: cur.id, disabled: !can || cur.status !== "sent", why: "Only sent letters" },
        { label: "Record signed copy", a: "intake-cn-signed", id: cur.id, disabled: !can || cur.status !== "sent", why: "Only sent letters" },
        { label: "Record oral consent", a: "intake-cn-oral", id: cur.id, disabled: !can || ["signed", "refused"].includes(cur.status), why: !can ? cw : "Already decided" },
        { label: "Mark refused", a: "intake-cn-refused", id: cur.id, danger: true, disabled: !can || ["signed", "refused"].includes(cur.status), why: !can ? cw : "Already decided" }] })}
      <div class="card"><dl class="kv"><dt>Independent legal advice</dt><dd>${cur.ila ? "Recommended in the letter" : "Not recommended"}</dd><dt>Scope limits</dt><dd>${cur.scope || "None"}</dd><dt>Disclosure</dt><dd>${cur.disclosure || h`<span class="muted">Not drafted yet</span>`}</dd></dl>
        ${cur.letter ? h`<div class="draft">${A.raw(cur.letter)}</div>` : ""}<h3 style="margin-top:10px">Trail</h3><ul class="small">${(cur.trail || []).map(([at, t]) => h`<li>${A.dateTime(at)} · ${t}</li>`)}</ul> ${cur.status === "sent" ? demo : ""}</div>` : null;
    page(el, h`${head("conflicts", { section: "conflicts", sub2: "consents", sub: "Every consent a clearance depends on. A clearance subject to consent takes effect only when each consent is signed." })}
      ${A.ui.filters({ chips: [{ id: "open", label: "Open", count: all.filter(F.open).length }, { id: "signed", label: "Signed", count: all.filter(F.signed).length }, { id: "all", label: "All", count: all.length }], active: cnF, action: "intake-cnf" })}
      ${A.ui.split(A.ui.list(list.map((x) => ({ id: x.id, on: cur && x.id === cur.id, title: x.party, sub: `${x.type} · ${who(x)}`, right: st(tone[x.status] || x.status, x.status) })), "intake-cn-pick", { empty: "No consents", emptyHint: " A clearance signed subject to consent adds them here." }), det)}`);
  }
  A.action("intake-cnf", (el) => { cnF = el.dataset.id; A.refresh(); });
  A.action("intake-cn-pick", (el) => { cnSel = el.dataset.id; A.refresh(); });
  const cnPut = (x, patch, note) => { S.put("consents", { id: x.id, ...patch, trail: [...(x.trail || []), [A.now(), note]] }); A.audit("consent." + (patch.status || "updated"), { target: x.id, note: `${x.party}: ${note}` }); };
  function consentSettled(leadId) {
    const l = lead(leadId); if (!l || !l.conflict || l.conflict.status !== "awaiting consent") return;
    const cs = S.where("consents", (x) => x.leadId === leadId);
    if (cs.some((x) => x.status === "refused")) S.put("leads", { id: l.id, conflict: { ...l.conflict, status: "blocked" } });
    else if (cs.length && cs.every((x) => x.status === "signed")) { S.put("leads", { id: l.id, conflict: { ...l.conflict, status: "cleared" }, stage: STAGES.indexOf(l.stage) < 3 ? "Consult" : l.stage }); A.toast(`All consents signed: ${l.name} is cleared`, "ok"); }
  }
  A.effect("intake-cn-ok", (p) => { const x = S.get("consents", p.id); if (x) cnPut(x, { status: "approved", letter: p.letter, disclosure: p.disclosure }, `Letter approved by ${me().name}`); });
  A.effect("intake-cn-oral", (p) => { const x = S.get("consents", p.id); if (!x) return; cnPut(x, { status: "signed", oral: true }, `Oral consent recorded; written confirmation sent (approved by ${me().name})`); consentSettled(x.leadId); });
  const cnGet = (el) => { const x = S.get("consents", el.dataset.id); return x && A.can("conflicts.run", "act") ? x : null; };
  A.action("intake-cn-draft", (el) => {
    const x = cnGet(el); if (!x || x.status !== "draft") return;
    const l = x.leadId && lead(x.leadId), by = me().id;
    A.agent.run({ title: `Consent letter: ${x.party}`, steps: ["Reading the clearance memo", "Writing a factual disclosure", "Checking it reveals no other client's confidences"], done: () => {
      const disclosure = `Linda K Law has been asked to act for ${l ? l.name : "another client"} in a matter in which your interests may differ from theirs. We hold no confidential information of yours that is relevant to it.`;
      const letter = String(h`<p>${A.date(A.today)}</p><p>${x.party}</p><p><b>Re: Request for your consent</b></p><p>${disclosure}</p><p>You may refuse. If you consent, you may withdraw it, and we may then have to stop acting for one or both of you. We recommend you get independent legal advice before you sign.</p><p>Scope: ${x.scope}</p><p>Yours truly,<br>LINDA K LAW</p><p>I consent. ______________________ Date: __________</p>`);
      S.put("consents", { id: x.id, status: "needs approval" });
      return A.gate({ kind: "letter", cap: "conflicts.clear", title: `Consent letter to ${x.party}`, summary: "G2a: a lawyer approves the text before it goes out.", preparedBy: by, body: letter,
        checks: [{ label: "Discloses no other client's confidential information", ok: true, blocking: true }, { label: "Recommends independent legal advice", ok: true }, { label: "States the right to refuse", ok: true }], effect: "intake-cn-ok", payload: { id: x.id, letter, disclosure } });
    } });
  });
  A.action("intake-cn-send", (el) => { const x = cnGet(el); if (x && x.status === "approved") { cnPut(x, { status: "sent" }, "Sent for e-signature"); A.toast("Sent for e-signature (demo)", "ok"); } });
  A.action("intake-cn-chase", (el) => { const x = cnGet(el); if (x && x.status === "sent") { cnPut(x, {}, "✦ Reminder sent by the agent"); A.toast("Reminder sent", "ok"); } });
  A.action("intake-cn-signed", (el) => { const x = cnGet(el); if (x && x.status === "sent") { cnPut(x, { status: "signed" }, `Signed copy recorded by ${me().name}`); consentSettled(x.leadId); } });
  A.action("intake-cn-refused", (el) => { const x = cnGet(el); if (x && !["signed", "refused"].includes(x.status)) { cnPut(x, { status: "refused" }, `Marked refused by ${me().name}`); consentSettled(x.leadId); A.toast("Refused: the clearance is blocked", "bad"); } });
  A.action("intake-cn-oral", (el) => {
    const x = cnGet(el); if (!x || ["signed", "refused"].includes(x.status)) return;
    A.gate({ kind: "consent", cap: "conflicts.clear", title: `Oral consent: ${x.party}`, summary: "Recorded by phone; a written confirmation goes out after approval.", preparedBy: me().id, checks: [{ label: "Written confirmation will be sent", ok: true, blocking: true }], effect: "intake-cn-oral", payload: { id: x.id } });
    A.toast("Sent for a lawyer's approval");
  });

  /* ---------- party records cleanup ---------- */
  A.seed("intakePairs", [
    { id: "ip-1", a: "Northgate Storage Inc.", aw: "opposing party on 2026-0142", b: "Northgate Logistics Inc.", bw: "opposing party on 2026-0151", reason: "Similar name; both list a Finch Ave W address", adverseTwo: true, status: "proposed" },
    { id: "ip-2", a: "Eric Lindquist", aw: "lead, phone note", b: "Erik Lindqvist", bw: "named in the will on 2026-0155", reason: "Similar name and the same family; caller's spelling unconfirmed", adverseTwo: false, status: "proposed" },
  ]);
  A.seed("intakeRels", [{ id: "ir-1", a: "Grace Mendel", b: "Estate of Margaret Lindqvist", rel: "estate trustee", by: "u-meraf" }, { id: "ir-2", a: "Northgate Storage Inc.", b: "Varga Holdings Inc.", rel: "parent/subsidiary", by: "agent" }]);
  let recTab = "dupes";
  const RELS = ["spouse", "parent/child", "director", "shareholder over 25%", "parent/subsidiary", "employer", "insurer", "guarantor", "estate trustee"];
  function records(el) {
    A.setContext({ kind: "intake", label: "Party records" });
    const can = A.can("conflicts.run", "act"), cw = canWhy("conflicts.run", "act", "edit party records");
    const pairs = S.all("intakePairs"), missing = S.all("contacts").filter((c) => !c.email || !c.phone || (c.kind === "org" && !c.corpNo));
    const tabs = A.ui.filters({ chips: [{ id: "dupes", label: "Possible duplicates", count: pairs.filter((p) => p.status === "proposed").length }, { id: "missing", label: "Missing identifiers", count: missing.length }, { id: "rels", label: "Relationships", count: S.all("intakeRels").length }], active: recTab, action: "intake-rec-tab",
      right: h`${A.ui.btn({ label: "✦ Propose duplicates", a: "intake-rec-propose", agent: true, small: true, disabled: !can, why: cw })}${recTab === "rels" ? A.ui.btn({ label: "Add relationship", a: "intake-rel-add", small: true, disabled: !can, why: cw }) : ""}` });
    const body = recTab === "dupes" ? A.ui.table([
      { label: "Record A", render: (p) => h`<b>${p.a}</b><div class="muted small">${p.aw}</div>` }, { label: "Record B", render: (p) => h`<b>${p.b}</b><div class="muted small">${p.bw}</div>` },
      { label: "Why", render: (p) => h`<span class="small">${p.reason}</span>${p.adverseTwo ? h`<div>${st("blocked", "Merge would join two clients' adverse parties")}</div>` : ""}` },
      { label: "Status", render: (p) => st({ proposed: "suggested", merged: "done", "not-duplicate": "closed", blocked: "blocked", "needs approval": "needs-approval" }[p.status] || p.status, p.status) },
      { label: "", render: (p) => (p.status === "proposed" ? h`<div class="row wrap">${A.ui.btn({ label: "Merge", a: "intake-rec-merge", id: p.id, small: true, disabled: !can, why: cw })}${A.ui.btn({ label: "Not a duplicate", a: "intake-rec-nodup", id: p.id, small: true, disabled: !can, why: cw })}</div>` : "") },
    ], pairs, { empty: "No possible duplicates" })
      : recTab === "missing" ? A.ui.table([{ label: "Contact", render: (c) => h`<b>${c.name}</b><div class="muted small">${c.role || c.kind}</div>` }, { label: "Missing", render: (c) => [!c.email && "email", !c.phone && "phone", c.kind === "org" && !c.corpNo && "corporate number"].filter(Boolean).join(", ") }], missing, { empty: "Every record has its identifiers" })
      : A.ui.table([{ label: "Party", key: "a" }, { label: "Relationship", key: "rel" }, { label: "Related to", key: "b" }, { label: "Recorded by", render: (r) => (r.by === "agent" ? A.ui.actor("agent", "Agent found") : A.user(r.by).name) }], S.all("intakeRels"), { empty: "No relationships recorded" });
    page(el, h`${head("conflicts", { section: "conflicts", sub2: "records", sub: "Keep the conflict database accurate. The agent proposes; it never merges. A lawyer approves each merge because it can create or remove a conflict." })}${tabs}${body}`);
  }
  A.action("intake-rec-tab", (el) => { recTab = el.dataset.id; A.refresh(); });
  A.action("intake-rec-propose", () => {
    if (!A.can("conflicts.run", "act")) return;
    A.agent.run({ title: "Find duplicate party records", steps: ["Comparing names, emails and phones", "Checking registry pulls", "Listing candidates"], done: () => {
      const known = new Set(S.all("intakePairs").map((p) => `${p.a}|${p.b}`)); let n = 0;
      S.all("leads").forEach((l) => S.all("contacts").forEach((c) => { const s = score(l.name, c.name); if (s >= 0.8 && s < 1 && !known.has(`${l.name}|${c.name}`)) { S.put("intakePairs", { id: A.id("ip"), a: l.name, aw: "lead", b: c.name, bw: c.role || "contact", reason: `Similar name (${Math.round(s * 100)}%)`, adverseTwo: false, status: "proposed" }); n++; } }));
      A.audit("records.proposed", { actor: "agent", note: `${n} new candidates` }); return { title: n ? `${n} new possible duplicates` : "No new duplicates found" };
    } });
  });
  A.effect("intake-merge", (p) => { const x = S.get("intakePairs", p.id); if (!x) return; S.put("intakePairs", { id: x.id, status: "merged" }); A.audit("records.merged", { target: x.id, note: `${x.b} merged into ${x.a}; both names kept as aliases` }); });
  A.action("intake-rec-merge", (el) => {
    const x = S.get("intakePairs", el.dataset.id); if (!x || x.status !== "proposed" || !A.can("conflicts.run", "act")) return;
    if (x.adverseTwo) { S.put("intakePairs", { id: x.id, status: "blocked" }); A.audit("records.merge_blocked", { target: x.id, note: "Would join adverse parties of two clients" }); return A.toast("Blocked: merging would join two clients' adverse parties. Raised as a conflict.", "bad"); }
    S.put("intakePairs", { id: x.id, status: "needs approval" });
    A.gate({ kind: "records", cap: "conflicts.clear", title: `Merge ${x.b} into ${x.a}`, summary: "Both names stay as aliases with all their roles. Affected checks are re-run.", preparedBy: me().id, checks: [{ label: "Does not join adverse parties of two clients", ok: true, blocking: true }], effect: "intake-merge", payload: { id: x.id } });
    A.toast("Merge sent for a lawyer's approval");
  });
  A.action("intake-rec-nodup", (el) => { const x = S.get("intakePairs", el.dataset.id); if (!x || !A.can("conflicts.run", "act")) return; S.put("intakePairs", { id: x.id, status: "not-duplicate" }); A.audit("records.not_duplicate", { target: x.id, note: `${x.a} / ${x.b}` }); });
  A.action("intake-rel-add", () => {
    if (!A.can("conflicts.run", "act")) return;
    const names = [...new Set(S.all("contacts").map((c) => c.name).concat(S.all("leads").map((l) => l.name)))].sort().map((n) => [n, n]);
    A.modal("Add relationship", h`<div class="grid g2"><label class="field">Party<select name="a">${opts(names, names[0][0])}</select></label><label class="field">Relationship<select name="rel">${opts(RELS.map((r) => [r, r]), RELS[0])}</select></label><label class="field">Related to<input name="b" class="input"></label></div>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d); if (!(v.b || "").trim()) { A.toast("Name the related party", "bad"); return false; } S.put("intakeRels", { id: A.id("ir"), a: v.a, b: v.b.trim(), rel: v.rel, by: me().id }); A.audit("records.relationship", { note: `${v.a} ${v.rel} ${v.b}` }); } }]);
  });

  /* ---------- lateral hire disclosure ---------- */
  A.seed("laterals", [{ id: "lat-tadesse", joiner: "Hiwot Tadesse", role: "Associate", formerFirm: "Bay & Adelaide LLP", start: "2026-11-02", status: "disclosed",
    matters: [{ client: "Lakeshore Transit Corp.", adverse: "Various personal injury claimants", nature: "Defence of bus passenger injury claims", dates: "2023–2025" }, { client: "Brightwater Imports Ltd.", adverse: "Harbour Freight Partners", nature: "Commercial lease dispute", dates: "2025–2026" }, { client: "Mina Yusuf", adverse: "Orion Signs Ltd.", nature: "Unpaid invoices", dates: "2024" }] }]);
  let latSel = null;
  function laterals(el) {
    A.setContext({ kind: "intake", label: "Lateral hires" });
    const all = S.all("laterals"), cur = all.find((x) => x.id === latSel) || all[0];
    const can = A.can("conflicts.run", "act"), cw = canWhy("conflicts.run", "act", "run lateral checks");
    const g = cur && S.where("approvals", (a) => a.effect === "intake-lateral" && a.status === "pending" && a.payload.id === cur.id)[0];
    const det = cur ? h`${A.ui.header({ title: `${cur.joiner}, ${cur.role}`, status: st({ disclosed: "new", checked: "agent-prepared", approved: "approved" }[cur.status] || cur.status, cur.status), sub: `From ${cur.formerFirm} · starts ${A.date(cur.start)} · account inactive until every wall is active`,
        actions: [{ label: "Add disclosed matter", a: "intake-lat-add", id: cur.id, disabled: !can || cur.status === "approved", why: cw }, { label: "✦ Run lateral check", a: "intake-lat-run", id: cur.id, agent: true, disabled: !can || cur.status === "approved", why: !can ? cw : "Start conditions already approved" }],
        primary: { label: "Approve start conditions", a: "intake-lat-ok", id: cur.id, disabled: cur.status !== "checked" || !!g, why: g ? "Waiting for a lawyer's sign-off" : "Run the lateral check first" } })}
      ${g ? A.ui.gateCard(g) : ""}
      <h3>Disclosed matters <span class="muted small">client names, other side and general nature only</span></h3>${A.ui.table([{ label: "Former client", key: "client" }, { label: "Other side", key: "adverse" }, { label: "Nature", key: "nature" }, { label: "Dates", key: "dates" }], cur.matters.map((m, i) => ({ ...m, id: i })))}
      ${cur.hits ? h`<h3 style="margin-top:12px">✦ Hits and proposed walls</h3>${A.ui.table([{ label: "Disclosed party", key: "party" }, { label: "Firm record", render: (x) => h`<b>${x.hit}</b><div class="muted small">${x.where}</div>` }, { label: "Proposal", render: (x) => h`${st(x.wall ? "blocked" : "done", x.wall ? "Wall" : "No action")}<div class="small">${x.note}</div>` }], cur.hits.map((x, i) => ({ ...x, id: i })), { empty: "No hits" })}` : ""}` : null;
    page(el, h`${head("conflicts", { section: "conflicts", sub2: "laterals", sub: "Before a new lawyer or staff member starts: what they worked on at their former firm, checked against ours. The disclosures are sealed and searched in future checks." })}
      ${A.ui.split(A.ui.list(all.map((x) => ({ id: x.id, on: cur && x.id === cur.id, title: x.joiner, sub: `${x.role} · ${x.formerFirm} · starts ${A.date(x.start)}`, right: st({ disclosed: "new", checked: "agent-prepared", approved: "approved" }[x.status] || x.status, x.status) })), "intake-lat-pick"), det)}`);
  }
  A.action("intake-lat-pick", (el) => { latSel = el.dataset.id; A.refresh(); });
  const latGet = (el) => { const x = S.get("laterals", el.dataset.id); return x && A.can("conflicts.run", "act") && x.status !== "approved" ? x : null; };
  A.action("intake-lat-add", (el) => {
    const x = latGet(el); if (!x) return;
    A.modal("Add disclosed matter", h`<div class="grid g2"><label class="field">Former client<input name="client" class="input"></label><label class="field">Other side<input name="adverse" class="input"></label><label class="field">General nature<input name="nature" class="input"></label><label class="field">Dates<input name="dates" class="input" placeholder="2024–2025"></label></div><p class="muted small">No privileged detail. A generic description is fine where naming the client would breach a confidence.</p>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d); if (!(v.client || "").trim()) { A.toast("Name the former client", "bad"); return false; } S.put("laterals", { id: x.id, status: "disclosed", hits: null, matters: [...x.matters, v] }); } }]);
  });
  A.action("intake-lat-run", (el) => {
    const x = latGet(el); if (!x) return;
    A.agent.run({ title: `Lateral check: ${x.joiner}`, steps: ["Searching every disclosed party", "Comparing with current and former matters", "Proposing walls"], done: () => {
      const hits = [];
      x.matters.forEach((m) => [[m.client, "client"], [m.adverse, "adverse"]].forEach(([name, role]) => search(name, role, { mode: "names" }).filter((s) => s.score >= 0.85).forEach((s) => {
        const mm = S.all("matters").find((q) => s.where.includes(q.number)); const wall = !!(mm && mm.status !== "closed" && role === "client" && (mm.opposing || []).some((o) => A.contact(o).name === s.hit));
        hits.push({ party: name, hit: s.hit, where: s.where, wall, matterId: mm && mm.id, note: wall ? `${x.joiner} acted for ${name}; the firm acts against it on ${mm.number}. Screen ${x.joiner} from that matter.` : "Same side or closed matter; no wall needed." });
      })));
      S.put("laterals", { id: x.id, status: "checked", hits }); A.audit("lateral.checked", { actor: "agent", target: x.id, note: `${hits.length} hits, ${hits.filter((h1) => h1.wall).length} walls proposed` });
      return { title: `${hits.filter((h1) => h1.wall).length} wall(s) proposed for ${x.joiner}` };
    } });
  });
  A.effect("intake-lateral", (p) => { const x = S.get("laterals", p.id); if (!x) return; S.put("laterals", { id: x.id, status: "approved", approvedBy: me().id }); A.audit("lateral.approved", { target: x.id, note: `${x.joiner}: ${(x.hits || []).filter((h1) => h1.wall).length} walls active before ${x.start}` }); });
  A.action("intake-lat-ok", (el) => {
    const x = S.get("laterals", el.dataset.id); if (!x || x.status !== "checked") return;
    const walls = (x.hits || []).filter((h1) => h1.wall);
    A.gate({ kind: "lateral", cap: "conflicts.walls", title: `Start conditions: ${x.joiner}`, summary: `${walls.length} wall(s) before ${A.date(x.start)}. The account stays inactive until they are active.`, preparedBy: me().id,
      body: String(h`<ul>${walls.map((w) => h`<li>${w.note}</li>`)}</ul>`), checks: [{ label: "Walls active before the start date", ok: true, blocking: true }], effect: "intake-lateral", payload: { id: x.id } });
    A.toast("Sent to a lawyer for sign-off (G2c)");
  });

  /* ---------- client ID records and retention ---------- */
  A.seed("idRecords", [
    { id: "idr-1", client: "Abdi Warsame", matter: "2019-0044", ended: "2019-06-28", hold: false, status: "due" },
    { id: "idr-2", client: "Lomond Bakery Ltd.", matter: "2018-0102", ended: "2018-11-30", hold: true, holdWhy: "LawPRO claim notice", status: "due" },
    { id: "idr-3", client: "Teresa Ng", matter: "2020-0017", ended: "2020-03-02", hold: false, status: "due" },
  ]);
  let idTab = "active";
  const retainUntil = (ended) => addYears(ended, 6);
  function clientId(el) {
    A.setContext({ kind: "intake", label: "Client ID records" });
    if (!A.can("conflicts.identify")) return page(el, h`${head("conflicts", { section: "conflicts", sub2: "client-id" })}${noRole("client ID records")}`);
    const clients = S.all("matters").filter((m) => m.status !== "closed").map((m) => ({ m, c: A.contact(m.clientId) })).filter((x) => x.c.id);
    const rows = clients.map(({ m, c }) => ({ id: m.id, client: c.name, number: A.canMatter(m) ? `${m.number} ${m.name}` : `${m.number} (restricted)`, idStatus: c.idStatus || "none", blocked: c.idStatus !== "verified", chased: c.idChased }));
    const due = S.all("idRecords").filter((r) => retainUntil(r.ended) <= A.today);
    const canAct = A.can("conflicts.identify", "act"), batchG = S.where("approvals", (g) => g.effect === "intake-destroy" && g.status === "pending")[0];
    const body = idTab === "retention" ? h`${A.ui.table([{ label: "Client", render: (r) => h`<b>${r.client}</b><div class="muted small">${r.matter} · ended ${A.date(r.ended)}</div>` }, { label: "Keep until", render: (r) => A.date(retainUntil(r.ended)) },
        { label: "Status", render: (r) => (r.hold ? st("hold", `Legal hold: ${r.holdWhy}`) : st({ due: "due-soon", "in batch": "needs-approval", destroyed: "closed" }[r.status] || r.status, r.status)) }], due, { empty: "Nothing due" })}
        ${batchG ? A.ui.gateCard(batchG) : ""}`
      : A.ui.table([{ label: "Client", render: (r) => h`<b>${r.client}</b><div class="muted small">${r.number}</div>` }, { label: "ID", render: (r) => st(r.idStatus === "verified" ? "done" : "unconfirmed", r.idStatus) },
        { label: "Trust funds", render: (r) => (r.blocked ? st("blocked", "Blocked until verified") : st("active", "Allowed")) }, { label: "Chased", render: (r) => (r.chased ? A.dateTime(r.chased) : "") }], rows.filter((r) => idTab === "active" || r.blocked), { empty: "No clients blocked on ID" });
    page(el, h`${head("conflicts", { section: "conflicts", sub2: "client-id", sub: "ID status for every active client, and when ID records may be destroyed (kept at least 6 years after the relationship ends, By-Law 7.1)." })}
      <div class="stats">${A.ui.stat("Blocked on ID", rows.filter((r) => r.blocked).length, "no trust funds move", rows.some((r) => r.blocked) ? "bad" : "")}${A.ui.stat("Records due", due.filter((r) => !r.hold && r.status === "due").length, "for destruction")}${A.ui.stat("On legal hold", due.filter((r) => r.hold).length, "destruction suspended")}</div>
      ${A.ui.filters({ chips: [{ id: "active", label: "Active", count: rows.length }, { id: "blocked", label: "Blocked", count: rows.filter((r) => r.blocked).length }, { id: "retention", label: "Retention due", count: due.length }], active: idTab, action: "intake-id-tab",
        right: h`${A.ui.btn({ label: "✦ Chase outstanding ID", a: "intake-id-chase", agent: true, small: true, disabled: !canAct, why: canWhy("conflicts.identify", "act", "chase ID") })}${A.ui.btn({ label: "✦ Propose destruction batch", a: "intake-id-batch", agent: true, small: true, disabled: !canAct || !!batchG, why: batchG ? "A batch is awaiting approval" : canWhy("conflicts.identify", "act", "propose batches") })}` })}${body}`);
  }
  A.action("intake-id-tab", (el) => { idTab = el.dataset.id; A.refresh(); });
  A.action("intake-id-chase", () => {
    if (!A.can("conflicts.identify", "act")) return;
    const cs = [...new Set(S.all("matters").filter((m) => m.status !== "closed").map((m) => A.contact(m.clientId)).filter((c) => c.id && c.idStatus !== "verified"))];
    if (!cs.length) return A.toast("Every active client is verified");
    A.agent.run({ title: `Chase ID from ${cs.length} client${cs.length > 1 ? "s" : ""}`, steps: ["Finding clients without verified ID", "Sending the secure ID request"], done: () => { cs.forEach((c) => S.put("contacts", { id: c.id, idChased: A.now() })); A.audit("client.id.chased", { actor: "agent", note: cs.map((c) => c.name).join(", ") }); return { title: `ID requests sent (demo)` }; } });
  });
  A.effect("intake-destroy", (p) => { (p.ids || []).forEach((id) => S.put("idRecords", { id, status: "destroyed", destroyedAt: A.now() })); A.audit("client.id.destroyed", { note: `${(p.ids || []).length} records; destruction certificate kept` }); });
  A.action("intake-id-batch", () => {
    if (!A.can("conflicts.identify", "act")) return;
    const rs = S.all("idRecords").filter((r) => retainUntil(r.ended) <= A.today && !r.hold && r.status === "due"); if (!rs.length) return A.toast("No records are due");
    A.agent.run({ title: "Destruction batch: client ID records", steps: ["Recomputing retention dates", "Excluding legal holds", "Drafting the certificate"], done: () => {
      rs.forEach((r) => S.put("idRecords", { id: r.id, status: "in batch" }));
      return A.gate({ kind: "retention", cap: "conflicts.identify", title: `Destroy ${rs.length} client ID records`, summary: "Past their retention date with no legal hold. A destruction certificate is kept.", preparedBy: "agent",
        body: String(h`<ul>${rs.map((r) => h`<li>${r.client} · ${r.matter} · kept until ${A.date(retainUntil(r.ended))}</li>`)}</ul>`), checks: [{ label: "No record under legal hold", ok: true, blocking: true }], effect: "intake-destroy", payload: { ids: rs.map((r) => r.id) } });
    } });
  });

  /* ---------- conflict search settings ---------- */
  const CSRC = [["contacts", "Contacts"], ["parties", "Parties on matters"], ["leads", "Leads"], ["sealed", "Sealed prospects"], ["notes", "Notes"], ["calendar", "Calendar entries"], ["docnames", "Document names"], ["doctext", "Document text"], ["email", "Emails"], ["laterals", "Former-firm disclosures"]];
  let impact = null;
  function conflictSettings(el) {
    A.setContext({ kind: "intake", label: "Conflict search settings" });
    const c = ccfg(), can = A.can("intake.forms", "all") || A.can("conflicts.clear", "approve"), dis = can ? "" : A.raw("disabled");
    const pend = S.where("approvals", (g) => g.effect === "intake-ccfg" && g.status === "pending")[0];
    page(el, h`${head("conflicts", { section: "conflicts", sub2: "conflict-settings", sub: "What a check searches and how loosely it matches. A change that narrows the search needs a lawyer's approval." })}
      ${pend ? A.ui.gateCard(pend) : ""}
      <section class="card"><div class="grid g2"><div><h3>Sources searched</h3>${CSRC.map(([k, t]) => h`<label class="row small"><input type="checkbox" name="src_${k}" ${onOff(c.sources.includes(k))} ${dis}> ${t}</label>`)}</div>
      <div><h3>Matching and lifecycle</h3><label class="field">Name matching<select name="match" ${dis}>${opts([["similar", "Similar (spelling, initials, transliteration)"], ["exact", "Exact only for identifiers; names stay similar"]], c.match)}</select></label>
        <label class="field">Similarity threshold (%)<input name="threshold" type="number" min="40" max="95" class="input" value="${c.threshold}" ${dis}></label>
        <label class="field">Unresolved check lapses after (days)<input name="autoClose" type="number" min="1" class="input" value="${c.autoClose}" ${dis}></label>
        <label class="field">Cleared check is stale after (days)<input name="stale" type="number" min="1" class="input" value="${c.stale}" ${dis}></label></div></div>
      <div class="row wrap" style="margin-top:8px">${A.ui.btn({ label: "✦ Estimate impact", a: "intake-ccfg-impact", agent: true, disabled: !can, why: "Lawyer or admin" })}${A.ui.btn({ label: "Save", a: "intake-ccfg-save", primary: true, disabled: !can || !!pend, why: pend ? "A change is awaiting approval" : "Lawyer or admin" })}</div>
      ${impact ? h`<p class="small">✦ ${impact}</p>` : ""}</section>`);
  }
  const ccfgFrom = (v) => ({ sources: CSRC.map((x) => x[0]).filter((k) => v["src_" + k]), match: v.match, threshold: Math.min(95, Math.max(40, +v.threshold || 60)), autoClose: +v.autoClose || 5, stale: +v.stale || 90 });
  const hitsAt = (thr) => S.all("leads").reduce((n, l) => n + l.parties.reduce((m, p) => m + S.all("contacts").filter((c) => score(p.name, c.name) * 100 >= thr).length, 0), 0);
  A.action("intake-ccfg-impact", (el) => {
    const v = ccfgFrom(formOf(el)), c = ccfg();
    A.agent.run({ title: "Estimate impact of search settings", steps: ["Re-running last month's checks", "Comparing hits"], done: () => {
      const a = hitsAt(c.threshold), b = hitsAt(v.threshold), lost = c.sources.filter((s) => !v.sources.includes(s));
      impact = `Name hits ${a} → ${b}${b < a ? ` (${a - b} hidden)` : ""}.${lost.length ? ` Turning off ${lost.join(", ")} would miss hits found there.` : ""}`; A.refresh(); return { title: "Impact estimated" };
    } });
  });
  A.effect("intake-ccfg", (p) => { S.put("intakeConfig", { id: "conflict", ...p.next }); A.audit("conflict.settings", { note: p.diff }); });
  A.action("intake-ccfg-save", (el) => {
    const v = ccfgFrom(formOf(el)), c = ccfg();
    const narrows = v.threshold > c.threshold || c.sources.some((s) => !v.sources.includes(s)) || v.stale > c.stale || v.match !== c.match;
    const diff = Object.keys(v).filter((k) => JSON.stringify(v[k]) !== JSON.stringify(c[k])).map((k) => `${k}: ${JSON.stringify(c[k])} → ${JSON.stringify(v[k])}`).join("; ");
    if (!diff) return A.toast("No changes");
    if (narrows) { A.gate({ kind: "settings", cap: "conflicts.clear", title: "Narrow the conflict search", summary: diff, preparedBy: me().id, checks: [{ label: "Hits a lawyer marked relevant stay visible", ok: true }], effect: "intake-ccfg", payload: { next: v, diff } }); return A.toast("This narrows the search; sent to a lawyer"); }
    S.put("intakeConfig", { id: "conflict", ...v }); A.audit("conflict.settings", { note: diff }); A.toast("Saved", "ok");
  });

  /* ---------- organization identification and beneficial owners ---------- */
  function orgView(el, { id }) {
    const l = lead(id); if (!l || l.kind !== "org") return page(el, A.ui.empty("No organization lead here", "", { label: "Back to intake", to: "intake" }));
    A.setContext({ kind: "lead", id: l.id, label: l.name });
    const o = l.org || { status: "not started", people: [] }, can = A.can("conflicts.identify", "act"), cw = canWhy("conflicts.identify", "act", "identify clients");
    const g = pendingGate(l.id, "intake-org");
    page(el, h`${A.ui.header({ title: `${l.name}: identification`, status: st(o.status === "verified" ? "done" : o.status === "not started" ? "todo" : "in-progress", o.status), back: { label: l.name, to: `intake/${l.id}` }, sub: "By-Law 7.1: verify the organization exists and record its directors and anyone who owns or controls 25% or more.",
        actions: [{ label: "✦ Pull registry profile", a: "intake-org-pull", id: l.id, agent: true, disabled: !can || o.status === "verified", why: cw }, { label: "✦ Request owner details", a: "intake-org-req", id: l.id, agent: true, disabled: !can || !o.legalName, why: !can ? cw : "Pull the registry profile first" }, { label: "Add person", a: "intake-org-add", id: l.id, disabled: !can || o.status === "verified", why: cw }],
        primary: { label: "Confirm organization verified", a: "intake-org-ok", id: l.id, disabled: !can || !o.legalName || !!g || o.status === "verified", why: g ? "Waiting for a lawyer" : o.status === "verified" ? "Already verified" : "Pull the registry profile first" } })}
      ${g ? A.ui.gateCard(g) : ""}
      <section class="card">${o.legalName ? h`<dl class="kv"><dt>Legal name</dt><dd>${o.legalName}</dd><dt>Type</dt><dd>${o.type}</dd><dt>Jurisdiction and number</dt><dd>${o.number}</dd><dt>Address</dt><dd>${o.address}</dd><dt>Business</dt><dd>${o.nature}</dd><dt>Instructs us</dt><dd>${o.instructing}</dd><dt>Existence evidence</dt><dd>${o.evidence} ${A.ui.actor("agent", "Agent pulled")}</dd></dl>` : h`<p class="muted">Nothing recorded yet. The agent pulls the public registry profile and compares it with what the client told us.</p>`}</section>
      <h3 style="margin:14px 0 8px">Directors, signers and owners</h3>${A.ui.table([{ label: "Name", render: (p) => h`<b>${p.name}</b>` }, { label: "Role", key: "role" }, { label: "Owns or controls", num: true, render: (p) => (p.pct ? `${p.pct}%` : "") }, { label: "Individual ID", render: (p) => st(p.id === "verified" ? "done" : "unconfirmed", p.id) }], o.people.map((p, i) => ({ ...p, id: i })), { empty: "No people recorded" })}
      ${o.chart ? h`<h3 style="margin:14px 0 8px">✦ Ownership</h3><div class="draft">${o.chart.map((x) => h`<p>${x}</p>`)}</div>` : ""}${o.requested ? h`<p class="small">Owner details requested from ${o.instructing} on ${A.dateTime(o.requested)} ${demo}</p>` : ""}`);
  }
  const orgGet = (el) => { const l = getL(el); return l && l.kind === "org" && A.can("conflicts.identify", "act") ? l : null; };
  A.action("intake-org-pull", (el) => {
    const l = orgGet(el); if (!l) return;
    A.agent.run({ title: `Registry profile: ${l.name}`, steps: ["Searching the Ontario Business Registry", "Comparing with the client's account", "Mapping ownership"], done: () => {
      const north = /northgate/i.test(l.name);
      const org = north ? { legalName: "Northgate Storage Inc.", type: "Corporation (Ontario)", number: "Ontario 2814467", address: "3550 Finch Ave W, Toronto ON", nature: "Self-storage facilities", instructing: l.contactName || "Janet Park", evidence: "Ontario Business Registry profile report, pulled " + A.date(A.today),
          people: [{ name: "Colin Varga", role: "Director, president", pct: 60, id: "unverified" }, { name: "Ruth Varga", role: "Director", pct: 40, id: "unverified" }, { name: "Janet Park", role: "Authorized signer", pct: 0, id: "unverified" }],
          chart: ["Northgate Storage Inc. is 100% owned by Varga Holdings Inc. (Ontario 1990342).", "Varga Holdings Inc.: Colin Varga 60%, Ruth Varga 40%. Both control the client indirectly."] }
        : { legalName: l.name, type: "Corporation (Ontario)", number: "Ontario 2650918", address: "Toronto ON", nature: l.area === "Commercial litigation" ? "Commercial printing" : "Business", instructing: l.contactName || l.name, evidence: "Ontario Business Registry profile report, pulled " + A.date(A.today),
          people: [{ name: (l.contactName || "Owner").split(",")[0], role: "Director, sole shareholder", pct: 100, id: "unverified" }], chart: [`${l.name} is owned directly by one individual; no holding company.`] };
      S.put("leads", { id: l.id, org: { ...org, status: "in progress" } }); A.audit("client.org.pulled", { actor: "agent", target: l.id, note: org.number });
      return { title: "Registry profile pulled", outputs: [{ label: "Open", go: `intake/org/${l.id}` }] };
    } });
  });
  A.action("intake-org-req", (el) => { const l = orgGet(el); if (!l || !l.org) return; S.put("leads", { id: l.id, org: { ...l.org, requested: A.now() } }); A.audit("client.org.requested", { target: l.id }); A.toast("Owner details requested through the portal (demo)", "ok"); });
  A.action("intake-org-add", (el) => {
    const l = orgGet(el); if (!l) return;
    A.modal("Add person", h`<div class="grid g2"><label class="field">Name<input name="name" class="input"></label><label class="field">Role<select name="role">${opts(["Director", "Officer", "Authorized signer", "Owner or controller"].map((x) => [x, x]), "Director")}</select></label><label class="field">Owns or controls (%)<input name="pct" type="number" min="0" max="100" class="input" value="0"></label></div>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d); if (!(v.name || "").trim()) { A.toast("Enter a name", "bad"); return false; } const o = l.org || { status: "in progress", people: [] }; S.put("leads", { id: l.id, org: { ...o, status: o.status === "not started" ? "in progress" : o.status, people: [...o.people, { name: v.name.trim(), role: v.role, pct: +v.pct || 0, id: "unverified" }] } }); } }]);
  });
  A.effect("intake-org", (p) => { const l = lead(p.leadId); if (!l || !l.org) return; S.put("leads", { id: l.id, org: { ...l.org, status: "verified" }, idv: { status: "verified", method: "Corporate registry search (organization)", document: l.org.evidence, by: me().id, at: A.now(), verifiedBy: me().id } }); A.audit("client.org.verified", { target: l.id, note: l.org.number }); });
  A.action("intake-org-ok", (el) => {
    const l = orgGet(el); if (!l || !l.org || pendingGate(l.id, "intake-org")) return;
    const o = l.org, owners = o.people.filter((p) => p.pct >= 25);
    A.gate({ kind: "identity", cap: "conflicts.identify", title: `Organization verified: ${o.legalName}`, summary: `${o.number}. ${owners.length} owner(s) of 25% or more recorded.`, preparedBy: me().id, body: String(h`<ul>${(o.chart || []).map((x) => h`<li>${x}</li>`)}</ul>`),
      checks: [{ label: "Existence verified from a registry record", ok: !!o.evidence, blocking: true }, { label: "At least one director recorded", ok: o.people.some((p) => /director/i.test(p.role)), blocking: true }, { label: "Owners of 25% or more recorded, or the gap explained", ok: owners.length > 0 }], effect: "intake-org", payload: { leadId: l.id } });
    A.toast("Sent to a lawyer (G4)");
  });

  /* ---------- setup: forms, public form, follow-ups, receptionist, consult types, lists ---------- */
  const LOCKED = ["Full name", "Email", "Phone", "Preferred contact", "Other parties", "What happened", "Key date", "Consent to be contacted", "I understand the firm does not act for me yet"];
  const LIB = ["Injury and treatment", "Insurer and claim number", "Employer and start date", "Termination letter (upload)", "Property address", "Closing date", "Deceased's name and date of death", "Amount owed", "Documents (upload)", "How did you hear about us"];
  A.seed("intakeForms", [
    { id: "f-pi", name: "Personal injury", slug: "personal-injury", area: "Personal injury", route: "u-ayo", status: "published", version: 2, booking: true, feeCents: 0, langs: "en, fr", fields: [...LOCKED, "Injury and treatment", "Insurer and claim number", "Documents (upload)"], ack: "Thank you. We received your enquiry and a member of our team will contact you within one business day. This is an automatic reply. It is not legal advice, and the firm does not act for you until we confirm in writing." },
    { id: "f-emp", name: "Employment: dismissal", slug: "dismissal", area: "Employment", route: "u-ayo", status: "published", version: 1, booking: true, feeCents: 0, langs: "en", fields: [...LOCKED, "Employer and start date", "Termination letter (upload)"], ack: "Thank you. We received your enquiry and will contact you within one business day. Please do not sign a release before you hear from us. This is an automatic reply, not legal advice." },
    { id: "f-re", name: "Real estate purchase", slug: "purchase", area: "Real estate", route: "u-yoni", status: "draft", version: 1, booking: false, feeCents: 0, langs: "en", fields: [...LOCKED, "Property address", "Closing date"], ack: "Thank you. Your closing is in good hands and you will definitely save on land transfer tax." },
  ]);
  const lint = (t) => [[/definitely|guarantee|will win|in good hands/i, "Promises a result"], [/you should|you must sign|you have a case/i, "Reads as legal advice"], [/not legal advice/i, null]].reduce((out, [re, msg]) => (msg && re.test(t) ? out.concat(msg) : out), []).concat(/not legal advice|not a lawyer/i.test(t) ? [] : ["Missing the not-legal-advice line"]);
  let fSel = null;
  const canForms = () => A.can("intake.forms", "all");
  function forms(el) {
    A.setContext({ kind: "intake", label: "Intake forms" });
    if (!A.can("intake.forms")) return page(el, h`${head("setup", { section: "setup", sub2: "forms" })}${noRole("intake setup")}`);
    const all = S.all("intakeForms"), f = all.find((x) => x.id === fSel) || all[0], can = canForms(), cw = "Lawyer or admin";
    const g = f && S.where("approvals", (a) => a.effect === "intake-publish" && a.status === "pending" && a.payload.id === f.id)[0];
    const issues = f ? lint(f.ack) : [];
    const det = f ? h`${A.ui.header({ title: f.name, status: st(f.status === "published" ? "active" : f.status === "archived" ? "archived" : g ? "needs-approval" : "draft", f.status), sub: `${f.area} · v${f.version} · lindaklaw.example.ca/intake/${f.slug}`,
        actions: [{ label: "Preview public form", to: `intake/public/${f.id}` }, f.status === "published" ? { label: "Unpublish", a: "intake-f-unpub", id: f.id, disabled: !can, why: cw } : null].filter(Boolean),
        primary: f.status !== "published" ? { label: "Publish", a: "intake-f-pub", id: f.id, disabled: !can || !!g, why: g ? "Waiting for approval" : cw } : null })}
      ${g ? A.ui.gateCard(g) : ""}
      <div class="grid g2"><section class="card"><h3>Fields</h3><ol class="small" style="padding-left:18px;margin:0">${f.fields.map((x, i) => h`<li>${x} ${LOCKED.includes(x) ? h`<span class="muted">locked</span>` : h`${A.ui.btn({ label: "↑", a: "intake-f-move", id: f.id, data: { i, d: -1 }, small: true, disabled: !can, why: cw, title: "Move up" })}${A.ui.btn({ label: "Remove", a: "intake-f-del", id: f.id, data: { i }, small: true, disabled: !can, why: cw })}`}</li>`)}</ol>
        <div class="row wrap" style="margin-top:8px"><select data-c="intake-f-addfield" data-id="${f.id}" aria-label="Add a standard field" ${can ? "" : A.raw("disabled")}><option value="">Add a field…</option>${opts(LIB.filter((x) => !f.fields.includes(x)).map((x) => [x, x]), "")}</select></div>
        <p class="muted small">Standard fields (parties, key date, consent) are locked on. Removing a field that past leads used hides it; old answers stay.</p></section>
      <section class="card"><h3>Settings</h3><div class="grid">
        <label class="field">Practice area<select name="area" ${can ? "" : A.raw("disabled")}>${opts(PRACTICE.map((p) => [p, p]), f.area)}</select></label>
        <label class="field">Route to<select name="route" ${can ? "" : A.raw("disabled")}>${opts(LAWYERS().map((u) => [u.id, u.name]), f.route)}</select></label>
        <label class="row small"><input type="checkbox" name="booking" ${onOff(f.booking)} ${can ? "" : A.raw("disabled")}> Allow self-booking (verified email only)</label>
        <label class="field">Consult fee at booking ($, plus HST)<input name="fee" type="number" min="0" step="5" class="input" value="${f.feeCents / 100}" ${can ? "" : A.raw("disabled")}></label>
        <label class="field">Automatic reply<textarea name="ack" rows="4" class="input" ${can ? "" : A.raw("disabled")}>${f.ack}</textarea></label></div>
        <p class="small">${issues.length ? h`✦ ${st("blocked", "Fix before publishing")} ${issues.join("; ")}` : h`✦ ${st("done", "Wording check passed")} No advice, no promised results.`}</p>
        <div class="row wrap">${A.ui.btn({ label: "Save draft", a: "intake-f-save", id: f.id, disabled: !can, why: cw })}</div>
        <p class="muted small">Embed: <code>&lt;iframe src="https://lindaklaw.example.ca/intake/${f.slug}"&gt;</code></p></section></div>` : null;
    page(el, h`${head("setup", { section: "setup", sub2: "forms", title: "Intake setup", primary: can ? { label: "✦ Draft a form", a: "intake-f-new", agent: true, key: "n" } : null, sub: "Public forms per practice area. Publishing or changing public wording needs a lawyer's approval." })}
      ${A.ui.split(A.ui.list(all.map((x) => ({ id: x.id, on: f && x.id === f.id, title: x.name, sub: `${x.area} · v${x.version} · ${x.fields.length} fields`, right: st(x.status === "published" ? "active" : x.status === "archived" ? "archived" : "draft", x.status) })), "intake-f-pick"), det)}`);
  }
  A.action("intake-f-pick", (el) => { fSel = el.dataset.id; A.refresh(); });
  const fGet = (el) => { const f = S.get("intakeForms", el.dataset.id); return f && canForms() ? f : null; };
  const fEdit = (f, patch) => S.put("intakeForms", { id: f.id, ...patch, status: f.status === "published" && (patch.ack || patch.fields) ? "draft" : f.status, version: f.status === "published" && (patch.ack || patch.fields) ? f.version + 1 : f.version });
  A.action("intake-f-move", (el) => { const f = fGet(el); const i = +el.dataset.i; if (!f || i <= 0) return; const fs = [...f.fields]; [fs[i - 1], fs[i]] = [fs[i], fs[i - 1]]; fEdit(f, { fields: fs }); });
  A.action("intake-f-del", (el) => { const f = fGet(el); if (!f) return; const fs = f.fields.filter((_, i) => i !== +el.dataset.i); fEdit(f, { fields: fs }); A.audit("intake.form_field_hidden", { target: f.id, note: f.fields[+el.dataset.i] }); });
  A.change("intake-f-addfield", (el) => { const f = S.get("intakeForms", el.dataset.id); if (!f || !el.value || !canForms()) return; fEdit(f, { fields: [...f.fields, el.value] }); });
  A.action("intake-f-save", (el) => { const f = fGet(el); if (!f) return; const v = formOf(el); fEdit(f, { area: v.area, route: v.route, booking: !!v.booking, feeCents: Math.round((+v.fee || 0) * 100), ack: v.ack }); A.toast("Saved as draft", "ok"); });
  A.action("intake-f-unpub", (el) => { const f = fGet(el); if (!f) return; S.put("intakeForms", { id: f.id, status: "archived" }); A.audit("intake.form_unpublished", { target: f.id, note: "Old submissions stay linked" }); });
  A.effect("intake-publish", (p) => { const f = S.get("intakeForms", p.id); if (f) { S.put("intakeForms", { id: f.id, status: "published" }); A.audit("intake.form_published", { target: f.id, note: `${f.name} v${f.version}` }); } });
  A.action("intake-f-pub", (el) => {
    const f = fGet(el); if (!f) return; const issues = lint(f.ack);
    const avail = S.where("consultTypes", (t) => !t.paused).length > 0;
    A.gate({ kind: "form", cap: "intake.convert", title: `Publish intake form: ${f.name} v${f.version}`, summary: `Public at lindaklaw.example.ca/intake/${f.slug}.`, preparedBy: me().id, body: String(h`<p><b>Automatic reply</b></p><p>${f.ack}</p><p><b>Fields</b>: ${f.fields.join(", ")}</p>`),
      checks: [{ label: "Reply has no advice or promised result and says it is not legal advice", ok: !issues.length, blocking: true }, { label: "Self-booking has lawyer availability", ok: !f.booking || avail, blocking: true }, { label: "Consult fee has a payment account (demo)", ok: true }], effect: "intake-publish", payload: { id: f.id } });
    A.toast(issues.length ? "Sent, but the wording check blocks approval" : "Sent to a lawyer for approval");
  });
  A.action("intake-f-new", () => {
    if (!canForms()) return;
    A.modal("✦ Draft a form", h`<label class="field">Describe it<input name="desc" class="input" value="A form for slip-and-fall cases"></label><label class="field">Practice area<select name="area">${opts(PRACTICE.map((p) => [p, p]), "Personal injury")}</select></label>`,
      [{ label: "Cancel" }, { label: "Draft", primary: true, run: (d) => {
        const v = A.formValues(d), id = A.id("f"), name = (v.desc || "New form").replace(/^a form for /i, "").replace(/^\w/, (x) => x.toUpperCase());
        A.agent.run({ title: `Draft form: ${name}`, steps: ["Picking questions for the practice area", "Writing the automatic reply", "Checking the wording"], done: () => {
          S.put("intakeForms", { id, name, slug: name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""), area: v.area, route: "u-ayo", status: "draft", version: 1, booking: false, feeCents: 0, langs: "en", fields: [...LOCKED, ...LIB.slice(0, 2)], ack: "Thank you. We received your enquiry and will contact you within one business day. This is an automatic reply. It is not legal advice, and the firm does not act for you until we confirm in writing." });
          fSel = id; return { title: `Draft form ready: ${name}`, outputs: [{ label: "Open", go: "intake/forms" }] };
        } });
      } }]);
  });

  let pubOut = null;
  function publicForm(el, { id }) {
    const f = S.get("intakeForms", id); if (!f || !A.can("intake.forms")) return page(el, A.ui.empty("Form not found", "", { label: "Back to forms", to: "intake/forms" }));
    A.setContext({ kind: "intake", label: `Public form: ${f.name}` });
    const field = (x) => /upload/i.test(x) ? h`<label class="field">${x}<input name="${x}" type="file" disabled title="Uploads are off in the demo"></label>`
      : /^Consent|^I understand/.test(x) ? h`<label class="row small"><input type="checkbox" name="${x}"> ${x}</label>`
      : x === "Preferred contact" ? h`<label class="field">${x}<select name="${x}">${opts([["email", "Email"], ["phone", "Phone"], ["text", "Text"]], "email")}</select></label>`
      : x === "Key date" ? h`<label class="field">${x} (incident, dismissal, death or closing)<input name="${x}" type="date" class="input"></label>`
      : /What happened/.test(x) ? h`<label class="field">${x}<textarea name="${x}" rows="4" class="input" maxlength="4000"></textarea></label>` : h`<label class="field">${x}${LOCKED.slice(0, 2).concat(["Other parties", "What happened"]).includes(x) ? " *" : ""}<input name="${x}" class="input"></label>`;
    page(el, h`${A.ui.header({ title: "Linda K Law", status: f.status === "published" ? st("active", "Published") : st("draft", "Draft preview"), back: { label: "Forms", to: "intake/forms" }, meta: demo, sub: `What a prospect sees at lindaklaw.example.ca/intake/${f.slug}. Linda K Law does not act for you until we confirm it in writing.` })}
      <div class="grid g2"><section class="card"><h3>${f.name}: tell us what happened</h3><div class="grid">${f.fields.map(field)}</div>
        <div class="row wrap" style="margin-top:8px">${A.ui.btn({ label: "Submit enquiry", a: "intake-pub-submit", id: f.id, primary: true })}${A.ui.btn({ label: "✦ Help me fill this in", a: "intake-pub-help", id: f.id, agent: true })}</div></section>
      <section class="card"><h3>✦ Intake helper</h3><p class="muted small">An automated assistant. It asks about dates, parties and places and never gives legal advice.</p>${pubOut ? h`<div class="draft">${pubOut.map((x) => h`<p>${x}</p>`)}</div>` : ""}</section></div>`);
  }
  A.action("intake-pub-help", (el) => {
    const v = formOf(el), story = v["What happened"] || "";
    pubOut = /should i|do i have a case|will i win|how much/i.test(story) ? ["I can't give legal advice. A lawyer will review your enquiry and contact you. If a court date is coming up, call 416-555-0100 now."]
      : /court (tomorrow|date)|hearing|arrest|violence|unsafe/i.test(story) ? ["This sounds urgent. Please call the firm now at 416-555-0100. If you are in danger, call 911."]
      : [!v["Key date"] ? "When did this happen? An exact date helps us check time limits." : "Thanks, the date is noted.", !v["Other parties"] ? "Who is on the other side? Include any insurer or employer." : "Thanks, the other side is noted.", "Where did it happen (city or address)?"];
    A.refresh();
  });
  A.action("intake-pub-submit", (el) => {
    const f = S.get("intakeForms", el.dataset.id); if (!f) return; const v = formOf(el);
    const name = (v["Full name"] || "").trim(), email = (v["Email"] || "").trim(), story = (v["What happened"] || "").trim(), other = (v["Other parties"] || "").trim();
    if (!name || !/@/.test(email) || !story || !other) return A.toast("Name, email, other parties and what happened are required", "bad");
    if (!v["Consent to be contacted"] || !v["I understand the firm does not act for me yet"]) return A.toast("Tick both acknowledgements", "bad");
    if (!A.can("intake.leads", "act")) return A.toast("Preview only for your role (demo)");
    const id = A.id("lead"), dd = v["Key date"] || "";
    A.agent.run({ title: `Triage web enquiry: ${name}`, steps: ["Classifying the practice area", "Checking limitation", "Pre-screening conflicts", "Sending the approved automatic reply"], done: () => {
      const hits = search(name, "client").concat(search(other, "adverse").filter((x) => x.tone === "bad")), pre = resultOf(hits);
      S.put("leads", { id, name, kind: /\b(inc|ltd|corp)\b/i.test(name) ? "org" : "person", email, phone: v["Phone"] || "", source: "Web form", received: A.now(), area: f.area, conf: 0.9, urgency: dd && A.daysBetween(A.today, addYears(dd, 2)) < 90 ? "high" : "medium", discovered: dd, limit: dd ? "basic" : "none",
        summary: story.slice(0, 220), parties: [{ name, role: "client" }, { name: other, role: "adverse" }], assignee: f.route, why: `Routed by the ${f.name} form.`, stage: "New", agentHandling: true,
        prescreen: { result: pre === "conflict" ? "hit" : pre === "review" ? "possible" : "clear", note: hits.length ? hits.map((x) => `${x.hit}: ${x.where}`).join("; ") : "No match in contacts, matters or documents." },
        facts: [["Name", name, 2], ["Other side", other, 3], ["Account", story.slice(0, 90), 4]], raw: [[`Web form ${f.name} v${f.version} - ${A.now()}`, `Name: ${name}, ${email}`, `Other side: ${other}`, story]] });
      A.audit("lead.created", { target: id, note: `Web form: ${name}; automatic reply sent` });
      return { title: `Lead created: ${name}`, outputs: [{ label: "Open lead", go: `intake/${id}` }] };
    } });
    A.toast("Enquiry submitted; the automatic reply went out", "ok");
  });

  A.seed("intakeSeqs", [
    { id: "sq-new", name: "New lead: acknowledge and book", trigger: "lead created", filters: "All practice areas", status: "active", version: 3, quiet: "8:00 a.m.–9:00 p.m. prospect time",
      steps: [{ wait: "0", type: "send email", text: "Acknowledgement from the approved form reply" }, { wait: "1 day", type: "send booking link", text: "Pick a time for a free 15-minute call." }, { wait: "2 days", type: "send SMS", text: "Reminder: book your call with Linda K Law. Reply STOP to opt out." }, { wait: "3 days", type: "create task", text: "Call the lead" }],
      stops: ["Lead replied", "Booked", "Declined", "Retained", "Opted out"] },
    { id: "sq-eng", name: "Engagement unsigned after 3 days", trigger: "engagement unsigned 3 days", filters: "All", status: "draft", version: 1, quiet: "8:00 a.m.–9:00 p.m. prospect time",
      steps: [{ wait: "0", type: "send email", text: "A reminder that your engagement letter is waiting for your signature." }, { wait: "4 days", type: "create task", text: "Phone the client about the unsigned engagement" }], stops: ["Signed", "Declined", "Opted out"] },
  ]);
  let sqSel = null, dry = null;
  function sequences(el) {
    A.setContext({ kind: "intake", label: "Follow-up sequences" });
    if (!A.can("intake.forms")) return page(el, h`${head("setup", { section: "setup", sub2: "sequences" })}${noRole("intake setup")}`);
    const all = S.all("intakeSeqs"), s = all.find((x) => x.id === sqSel) || all[0], can = canForms(), cw = "Lawyer or admin";
    const g = s && S.where("approvals", (a) => a.effect === "intake-seq" && a.status === "pending" && a.payload.id === s.id)[0];
    const det = s ? h`${A.ui.header({ title: s.name, status: st(s.status === "active" ? "active" : s.status === "paused" ? "hold" : g ? "needs-approval" : "draft", s.status), sub: `When: ${s.trigger} · ${s.filters} · v${s.version} · quiet hours ${s.quiet}`,
        actions: [{ label: "✦ Dry run on last 20 leads", a: "intake-seq-dry", id: s.id, agent: true, disabled: !can, why: cw }, s.status === "active" || s.status === "paused" ? { label: s.status === "active" ? "Pause" : "Resume", a: "intake-seq-pause", id: s.id, disabled: !can, why: cw } : null].filter(Boolean),
        primary: s.status === "draft" ? { label: "Activate", a: "intake-seq-go", id: s.id, disabled: !can || !!g, why: g ? "Waiting for a lawyer" : cw } : null })}
      ${g ? A.ui.gateCard(g) : ""}
      ${A.ui.table([{ label: "Wait", render: (x) => (x.wait === "0" ? "At once" : `After ${x.wait}`) }, { label: "Step", key: "type" }, { label: "Message or task", key: "text" }], s.steps.map((x, i) => ({ ...x, id: i })))}
      <p class="small"><b>Stops when</b>: ${s.stops.join(", ")}. A pre-conflict hit stops every sequence for that lead.</p>
      ${dry && dry.id === s.id ? h`<h3>✦ Dry run</h3>${A.ui.table([{ label: "Lead", key: "name" }, { label: "Would get", key: "out" }], dry.rows)}` : ""}` : null;
    page(el, h`${head("setup", { section: "setup", sub2: "sequences", title: "Intake setup", primary: can ? { label: "✦ Draft a sequence", a: "intake-seq-new", agent: true, key: "n" } : null, sub: "Automatic steps after a lead arrives. A lawyer approves every outgoing message before a sequence goes live." })}
      ${A.ui.split(A.ui.list(all.map((x) => ({ id: x.id, on: s && x.id === s.id, title: x.name, sub: `${x.trigger} · ${x.steps.length} steps`, right: st(x.status === "active" ? "active" : x.status === "paused" ? "hold" : "draft", x.status) })), "intake-seq-pick"), det)}`);
  }
  A.action("intake-seq-pick", (el) => { sqSel = el.dataset.id; A.refresh(); });
  const sqGet = (el) => { const s = S.get("intakeSeqs", el.dataset.id); return s && canForms() ? s : null; };
  A.action("intake-seq-pause", (el) => { const s = sqGet(el); if (!s) return; S.put("intakeSeqs", { id: s.id, status: s.status === "active" ? "paused" : "active" }); A.audit("intake.sequence_" + (s.status === "active" ? "paused" : "resumed"), { target: s.id }); });
  A.action("intake-seq-dry", (el) => {
    const s = sqGet(el); if (!s) return;
    A.agent.run({ title: `Dry run: ${s.name}`, steps: ["Replaying the last 20 leads", "Applying stop conditions"], done: () => {
      dry = { id: s.id, rows: S.all("leads").slice(0, 20).map((l) => ({ id: l.id, name: l.name, out: l.prescreen && l.prescreen.result === "hit" ? "Nothing: pre-conflict hit stops automation" : closed(l) ? "Nothing: lead closed" : l.consult ? `${s.steps[0].type}; stops at "Booked"` : s.steps.map((x) => x.type).join(" → ") })) };
      A.refresh(); return { title: "Dry run done; nothing was sent" };
    } });
  });
  A.effect("intake-seq", (p) => { const s = S.get("intakeSeqs", p.id); if (s) { S.put("intakeSeqs", { id: s.id, status: "active" }); A.audit("intake.sequence_activated", { target: s.id, note: `${s.name} v${s.version}` }); } });
  A.action("intake-seq-go", (el) => {
    const s = sqGet(el); if (!s || s.status !== "draft") return; const texts = s.steps.map((x) => x.text).join(" ");
    A.gate({ kind: "form", cap: "intake.convert", title: `Activate follow-up: ${s.name}`, summary: `${s.steps.length} steps; trigger: ${s.trigger}.`, preparedBy: me().id, body: String(h`<ol>${s.steps.map((x) => h`<li>${x.type}: ${x.text}</li>`)}</ol>`),
      checks: [{ label: "Opt-out is a stop condition", ok: s.stops.includes("Opted out"), blocking: true }, { label: "No advice in any message", ok: !/you should|you have a case|will win/i.test(texts), blocking: true }, { label: "Quiet hours set", ok: !!s.quiet }], effect: "intake-seq", payload: { id: s.id } });
    A.toast("Sent to a lawyer to approve the wording");
  });
  A.action("intake-seq-new", () => {
    if (!canForms()) return;
    A.modal("✦ Draft a sequence", h`<label class="field">Start from<select name="starter">${opts([["quick", "Quick intake"], ["screen", "Screening"], ["full", "Full intake"]], "quick")}</select></label><label class="field">Or describe it<input name="desc" class="input" placeholder="Remind leads who have not booked after 2 days"></label>`,
      [{ label: "Cancel" }, { label: "Draft", primary: true, run: (d) => {
        const v = A.formValues(d), id = A.id("sq"), name = (v.desc || "").trim() || { quick: "Quick intake", screen: "Screening", full: "Full intake" }[v.starter];
        A.agent.run({ title: `Draft sequence: ${name}`, steps: ["Writing the steps in the firm's voice", "Checking for advice and opt-out"], done: () => {
          S.put("intakeSeqs", { id, name, trigger: v.starter === "full" ? "form submitted" : "lead created", filters: "All practice areas", status: "draft", version: 1, quiet: "8:00 a.m.–9:00 p.m. prospect time",
            steps: [{ wait: "0", type: "send email", text: "Thank you for contacting Linda K Law. This is an automatic reply, not legal advice." }, { wait: "1 day", type: "send questionnaire", text: "Please complete our short intake questionnaire." }, { wait: "2 days", type: "send SMS", text: "Reminder from Linda K Law. Reply STOP to opt out." }],
            stops: ["Lead replied", "Booked", "Declined", "Retained", "Opted out"] });
          sqSel = id; return { title: `Draft ready: ${name}`, outputs: [{ label: "Open", go: "intake/sequences" }] };
        } });
      } }]);
  });

  A.seed("intakeCalls", [
    { id: "call-rahimi", at: "2026-10-09 06:40", channel: "web chat", caller: "Hamid Rahimi", area: "Landlord and tenant", fit: "poor", status: "new", recorded: false,
      summary: "Tenant says the landlord changed the locks on his apartment yesterday. Outside the firm's practice areas.", transcript: ["Assistant: Linda K Law, this is the firm's automated intake assistant. I can't give legal advice.", "Caller: My landlord changed the locks yesterday. What should I do?", "Assistant: I can't advise you. The firm doesn't handle residential tenancies. The Law Society Referral Service (lsrs.lso.ca) or a community legal clinic can help. Would you like the firm to note your enquiry?", "Caller: Yes please."] },
    { id: "call-varga", at: "2026-10-08 22:14", channel: "after hours", caller: "Colin Varga for Northgate Storage Inc.", area: "Commercial litigation", fit: "possible", status: "new", recorded: true, stopped: true,
      summary: "Names Northgate Storage Inc., an opposing party on an open matter. The assistant stopped collecting facts and took contact details only.", transcript: ["Assistant: Linda K Law, this is the firm's automated intake assistant. This call may be recorded; is that all right?", "Caller: Yes. I'm calling for Northgate Storage Inc. about a tenant who owes us rent.", "Assistant: Thank you. I'll take your name and number only, and someone from the firm will call you back.", "Caller: Colin Varga, 416-555-0161."] },
  ]);
  let callSel = null, rTab = "calls";
  function receptionist(el) {
    A.setContext({ kind: "intake", label: "AI receptionist" });
    if (!A.can("intake.forms")) return page(el, h`${head("setup", { section: "setup", sub2: "receptionist" })}${noRole("intake setup")}`);
    const r = S.get("intakeConfig", "receptionist"), can = canForms(), cw = "Lawyer or admin";
    const g = S.where("approvals", (a) => a.effect === "intake-rcp" && a.status === "pending")[0];
    const calls = S.all("intakeCalls"), c = calls.find((x) => x.id === callSel) || calls[0];
    const callDet = c ? h`${A.ui.header({ title: c.caller, status: st(c.status === "new" ? "to-review" : c.status === "lead" ? "done" : "closed", c.status), sub: `${c.channel} · ${A.dateTime(c.at)} · ${c.area} · ${c.recorded ? "recorded with consent" : "not recorded"}`,
        actions: [{ label: "Correct fields", a: "intake-call-edit", id: c.id, disabled: !A.can("intake.leads", "act"), why: "Intake staff" }, { label: "Mark reviewed", a: "intake-call-done", id: c.id, disabled: c.status !== "new" || !A.can("intake.leads", "act"), why: "Already reviewed" }],
        primary: { label: "Send to inbox", a: "intake-call-lead", id: c.id, disabled: c.status === "lead" || !A.can("intake.leads", "act"), why: c.status === "lead" ? "Lead already created" : "Intake staff" } })}
      <div class="card"><p>${A.ui.actor("agent", "Agent summary")} ${c.summary}</p>${c.stopped ? h`<p>${st("blocked", "Pre-conflict hit")} The caller names an opposing party on an open matter. Facts were not collected.</p>` : ""}
        <div class="draft">${c.transcript.map((x) => h`<p>${x}</p>`)}</div></div>` : null;
    const settings = h`${g ? A.ui.gateCard(g) : ""}<section class="card"><div class="grid g2">
      <div><label class="field">Channels<input name="channels" class="input" value="${r.channels.join(", ")}" ${can ? "" : A.raw("disabled")}></label><label class="field">Hours<input name="hours" class="input" value="${r.hours}" ${can ? "" : A.raw("disabled")}></label>
        <label class="field">Greeting<input name="greeting" class="input" value="${r.greeting}" ${can ? "" : A.raw("disabled")}></label><label class="field">Disclosure (always on)<input class="input" value="${DISCLOSE}" disabled></label></div>
      <div><label class="field">Emergency words (warm transfer or 911 line)<input name="emergency" class="input" value="${r.emergency}" ${can ? "" : A.raw("disabled")}></label><label class="field">Warm transfer list<input name="transfer" class="input" value="${r.transfer}" ${can ? "" : A.raw("disabled")}></label>
        <label class="row small"><input type="checkbox" name="recording" ${onOff(r.recording)} ${can ? "" : A.raw("disabled")}> Ask before recording</label></div></div>
      <div class="row wrap" style="margin-top:8px">${A.ui.btn({ label: "Publish script changes", a: "intake-rcp-pub", primary: true, disabled: !can || !!g, why: g ? "Waiting for a lawyer" : cw })}${A.ui.btn({ label: "✦ Suggest from firm website", a: "intake-rcp-suggest", agent: true, disabled: !can || !!g, why: cw })}${A.ui.btn({ label: "Test console", a: "intake-rcp-test" })}</div>
      <p class="muted small">Live: v${r.version}. The assistant never gives advice, never quotes fees beyond the published consult fee, and every lead it makes still goes through the inbox.</p></section>`;
    page(el, h`${head("setup", { section: "setup", sub2: "receptionist", title: "Intake setup", sub: "After-hours and overflow calls and web chat. The assistant collects facts, screens fit and hands a lead to the inbox with the transcript." })}
      ${A.ui.filters({ chips: [{ id: "calls", label: "Conversations", count: calls.filter((x) => x.status === "new").length }, { id: "settings", label: "Script and settings" }], active: rTab, action: "intake-rcp-tab", right: demo })}
      ${rTab === "settings" ? settings : A.ui.split(A.ui.list(calls.map((x) => ({ id: x.id, on: c && x.id === c.id, title: x.caller, sub: `${x.channel} · ${A.dateTime(x.at)} · ${x.area}`, right: st(x.status === "new" ? "to-review" : x.status === "lead" ? "done" : "closed", x.status) })), "intake-call-pick", { empty: "No conversations" }), callDet)}`);
  }
  A.action("intake-rcp-tab", (el) => { rTab = el.dataset.id; A.refresh(); });
  A.action("intake-call-pick", (el) => { callSel = el.dataset.id; A.refresh(); });
  const callGet = (el) => { const c = S.get("intakeCalls", el.dataset.id); return c && A.can("intake.leads", "act") ? c : null; };
  A.action("intake-call-done", (el) => { const c = callGet(el); if (c && c.status === "new") { S.put("intakeCalls", { id: c.id, status: "reviewed", by: me().id }); A.audit("receptionist.reviewed", { target: c.id }); } });
  A.action("intake-call-edit", (el) => {
    const c = callGet(el); if (!c) return;
    A.modal(`Correct fields: ${c.caller}`, h`<div class="grid g2"><label class="field">Caller<input name="caller" class="input" value="${c.caller}"></label><label class="field">Practice area<input name="area" class="input" value="${c.area}"></label></div>`,
      [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => { const v = A.formValues(d); S.put("intakeCalls", { id: c.id, caller: v.caller, area: v.area }); A.audit("receptionist.corrected", { target: c.id }); } }]);
  });
  A.action("intake-call-lead", (el) => {
    const c = callGet(el); if (!c || c.status === "lead") return;
    const id = A.id("lead"), name = c.caller.replace(/^.* for /, ""), hits = search(name, "client"), pre = resultOf(hits);
    S.put("leads", { id, name, kind: /\b(inc|ltd|corp)\b/i.test(name) ? "org" : "person", contactName: / for /.test(c.caller) ? c.caller.split(" for ")[0] : "", email: "", phone: (/(\d{3}-\d{3}-\d{4})/.exec(c.transcript.join(" ")) || [])[1] || "", source: "AI receptionist", received: c.at, area: PRACTICE.includes(c.area) ? c.area : "General", conf: 0.7, urgency: "medium", discovered: "", limit: "none",
      summary: c.summary, parties: [{ name, role: "client" }], assignee: "u-yoni", why: "From the AI receptionist.", stage: "New", prescreen: { result: pre === "conflict" ? "hit" : pre === "review" ? "possible" : "clear", note: hits.length ? hits.map((x) => `${x.hit}: ${x.where}`).join("; ") : "No match." },
      facts: [["Caller", c.caller, 1], ["Channel", c.channel, 1]], raw: [c.transcript] });
    S.put("intakeCalls", { id: c.id, status: "lead", leadId: id }); A.audit("receptionist.lead", { target: id, note: c.caller }); A.go(`intake/${id}`);
  });
  A.effect("intake-rcp", (p) => { const r = S.get("intakeConfig", "receptionist"); S.put("intakeConfig", { id: "receptionist", ...p.next, version: r.version + 1 }); A.audit("receptionist.published", { note: `v${r.version + 1}` }); });
  A.action("intake-rcp-pub", (el) => {
    if (!canForms()) return; const v = formOf(el), r = S.get("intakeConfig", "receptionist");
    const next = { channels: (v.channels || "").split(",").map((x) => x.trim()).filter(Boolean), hours: v.hours, greeting: v.greeting, emergency: v.emergency, transfer: v.transfer, recording: !!v.recording };
    A.gate({ kind: "form", cap: "intake.convert", title: `AI receptionist script v${r.version + 1}`, summary: "Greeting, hours, emergency words and transfer list.", preparedBy: me().id, body: String(h`<p>${next.greeting}</p><p>${DISCLOSE}</p>`),
      checks: [{ label: "Says it is an automated assistant", ok: true, blocking: true }, { label: "Greeting gives no advice or fee", ok: !/\$|you should/i.test(next.greeting), blocking: true }, { label: "Emergency words set", ok: !!next.emergency }], effect: "intake-rcp", payload: { next } });
    A.toast("Sent to a lawyer (G-form)");
  });
  A.action("intake-rcp-suggest", () => {
    if (!canForms()) return;
    A.agent.run({ title: "Suggest receptionist profiles from the firm website", steps: ["Reading lindaklaw.example.ca (demo)", "Drafting practice-area profiles and FAQ"], done: () => A.gate({ kind: "form", cap: "intake.convert", title: "Receptionist profiles and FAQ", summary: "Drafted from the firm website.", preparedBy: "agent",
      body: String(h`<ul>${PRACTICE.map((p) => h`<li><b>${p}</b>: ask for the other side, the key date and where it happened.</li>`)}<li><b>FAQ</b>: "Do you charge for a first call?" The first 15-minute call is free.</li></ul>`), checks: [{ label: "No advice in any answer", ok: true, blocking: true }], effect: "intake-rcp", payload: { next: {} } }) });
  });
  A.action("intake-rcp-test", () => {
    A.modal("Test console (sandbox, creates no leads)", h`<label class="field">Say something as a caller<input name="msg" class="input" value="I slipped at a mall last week. Do I have a case?"></label><div class="draft tc-out"><p class="muted">Replies appear here.</p></div>`,
      [{ label: "Close" }, { label: "Send", primary: true, run: (d) => {
        const m = A.formValues(d).msg || "", r = S.get("intakeConfig", "receptionist");
        const reply = new RegExp(r.emergency.split(/,\s*/).join("|"), "i").test(m) ? "This sounds urgent. I'm connecting you to a person now. If you are in danger, call 911."
          : /case|should i|win|how much/i.test(m) ? "I can't give legal advice, but I can take your details so a lawyer can call you. When did this happen?" : "Thank you. Who is on the other side, and when did this happen?";
        const out = d.querySelector(".tc-out"); if (out) out.innerHTML = `<p><b>You:</b> ${A.esc(m)}</p><p><b>✦ Assistant:</b> ${A.esc(reply)}</p>`; return false;
      } }]);
  });

  A.seed("consultTypes", [
    { id: "ct-free", name: "Free 15-minute call", lawyers: ["u-ayo", "u-yoni"], duration: 15, modes: "phone, video", days: [1, 2, 3, 4, 5], from: 9, to: 12, step: 15, notice: 4, ahead: 21, cap: 6, feeCents: 0, paused: false },
    { id: "ct-paid", name: "60-minute consultation", lawyers: ["u-linda", "u-yoni"], duration: 60, modes: "in person, video", days: [2, 4], from: 13, to: 17, step: 60, notice: 24, ahead: 30, cap: 3, feeCents: 15000, paused: false },
    { id: "ct-follow", name: "Follow-up call", lawyers: ["u-ayo"], duration: 30, modes: "phone", days: [5], from: 14, to: 16, step: 30, notice: 24, ahead: 14, cap: 2, feeCents: 0, paused: true },
  ]);
  const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  const dow = (iso) => new Date(iso + "T12:00:00").getDay();
  const slotsFor = (t) => { const out = []; for (let i = 1; i <= 14; i++) { const d = A.addDays(A.today, i); if (!t.days.includes(dow(d)) || i * 24 < t.notice) continue; const n = Math.min(t.cap, Math.floor(((t.to - t.from) * 60) / t.duration)); for (let k = 0; k < n; k++) { const m = t.from * 60 + k * Math.max(t.step, t.duration); out.push(`${d} ${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`); } } return out; };
  let ctSel = null;
  function consultTypes(el) {
    A.setContext({ kind: "intake", label: "Consultation types" });
    if (!A.can("intake.forms")) return page(el, h`${head("setup", { section: "setup", sub2: "consult-types" })}${noRole("intake setup")}`);
    const all = S.all("consultTypes"), t = all.find((x) => x.id === ctSel) || all[0], can = canForms(), cw = "Lawyer or admin", dis = can ? "" : A.raw("disabled");
    const g = t && S.where("approvals", (a) => a.effect === "intake-ct" && a.status === "pending" && a.payload.id === t.id)[0];
    const slots = t ? slotsFor(t) : [], openLeads = S.all("leads").filter((l) => !closed(l) && !l.consult).length;
    const det = t ? h`${A.ui.header({ title: t.name, status: st(t.paused ? "hold" : "active", t.paused ? "Paused" : "Bookable"), sub: `${t.duration} min · ${t.modes} · ${t.feeCents ? `${A.cents(t.feeCents)} + HST` : "no charge"}`,
        actions: [{ label: t.paused ? "Resume" : "Pause", a: "intake-ct-pause", id: t.id, disabled: !can, why: cw }, { label: "Copy booking link", a: "intake-ct-link", id: t.id }], primary: { label: "Save", a: "intake-ct-save", id: t.id, disabled: !can || !!g, why: g ? "A fee change is awaiting approval" : cw } })}
      ${g ? A.ui.gateCard(g) : ""}
      <section class="card"><div class="grid g2"><div>
        <label class="field">Name (public)<input name="name" class="input" value="${t.name}" ${dis}></label>
        <div class="field"><span>Lawyers</span><div class="row wrap small">${LAWYERS().map((u) => h`<label class="row"><input type="checkbox" name="law_${u.id}" ${onOff(t.lawyers.includes(u.id))} ${dis}> ${u.name}</label>`)}</div></div>
        <label class="field">Fee ($, plus HST; paid to trust before the consult)<input name="fee" type="number" min="0" step="5" class="input" value="${t.feeCents / 100}" ${dis}></label></div><div>
        <div class="field"><span>Days</span><div class="row wrap small">${[1, 2, 3, 4, 5].map((d) => h`<label class="row"><input type="checkbox" name="day_${d}" ${onOff(t.days.includes(d))} ${dis}> ${DAYS[d]}</label>`)}</div></div>
        <div class="row wrap"><label class="field">From (hour)<input name="from" type="number" min="7" max="20" class="input" value="${t.from}" ${dis}></label><label class="field">To<input name="to" type="number" min="8" max="21" class="input" value="${t.to}" ${dis}></label></div>
        <div class="row wrap"><label class="field">Minimum notice (hours)<input name="notice" type="number" min="0" class="input" value="${t.notice}" ${dis}></label><label class="field">Daily cap<input name="cap" type="number" min="1" class="input" value="${t.cap}" ${dis}></label></div></div></div></section>
      <h3 style="margin:14px 0 8px">Next two weeks <span class="muted small">${slots.length} slots, Toronto time</span></h3>
      ${slots.length < openLeads ? h`<p class="small">✦ ${st("due-soon", "Thin")} ${openLeads} open leads have no consult; ${slots.length} slots may not be enough.</p>` : ""}
      <p class="small">${slots.slice(0, 12).map((s) => A.dateTime(s)).join(" · ")}${slots.length > 12 ? " …" : ""}</p>` : null;
    page(el, h`${head("setup", { section: "setup", sub2: "consult-types", title: "Intake setup", primary: can ? { label: "New consult type", a: "intake-ct-new", key: "n" } : null, sub: "What prospects can book, with whom and when. Fee and public wording changes need a lawyer's approval." })}
      ${A.ui.split(A.ui.list(all.map((x) => ({ id: x.id, on: t && x.id === t.id, title: x.name, sub: `${x.duration} min · ${x.lawyers.map((u) => A.user(u).initials).join(", ")} · ${x.days.map((d) => DAYS[d]).join(" ")}`, right: st(x.paused ? "hold" : "active", x.paused ? "Paused" : "Bookable") })), "intake-ct-pick"), det)}`);
  }
  A.action("intake-ct-pick", (el) => { ctSel = el.dataset.id; A.refresh(); });
  const ctGet = (el) => { const t = S.get("consultTypes", el.dataset.id); return t && canForms() ? t : null; };
  A.action("intake-ct-pause", (el) => { const t = ctGet(el); if (!t) return; S.put("consultTypes", { id: t.id, paused: !t.paused }); A.audit("consult.type_" + (t.paused ? "resumed" : "paused"), { target: t.id }); });
  A.action("intake-ct-link", (el) => { const t = S.get("consultTypes", el.dataset.id); if (!t) return; const url = `https://lindaklaw.example.ca/book/${t.id.slice(3)}`; try { navigator.clipboard && navigator.clipboard.writeText(url).catch(() => {}); } catch (e) { /* demo */ } A.toast(`Copied ${url}`, "ok"); });
  A.effect("intake-ct", (p) => { S.put("consultTypes", { id: p.id, ...p.next }); A.audit("consult.type_published", { target: p.id, note: p.diff }); });
  A.action("intake-ct-save", (el) => {
    const t = ctGet(el); if (!t) return; const v = formOf(el);
    const next = { name: (v.name || t.name).trim(), lawyers: LAWYERS().map((u) => u.id).filter((u) => v["law_" + u]), feeCents: Math.round((+v.fee || 0) * 100), days: [1, 2, 3, 4, 5].filter((d) => v["day_" + d]), from: +v.from || t.from, to: +v.to || t.to, notice: +v.notice || 0, cap: +v.cap || 1 };
    if (!next.lawyers.length || !next.days.length || next.to <= next.from) return A.toast("Pick at least one lawyer and day, and an end after the start", "bad");
    const pub = next.feeCents !== t.feeCents || next.name !== t.name;
    if (pub) { A.gate({ kind: "fee", cap: "intake.convert", title: `Consult type: ${next.name}`, summary: `Fee ${A.cents(t.feeCents)} → ${A.cents(next.feeCents)} plus HST; public name "${next.name}".`, preparedBy: me().id, checks: [{ label: "Fee shown with HST and paid to trust", ok: true }], effect: "intake-ct", payload: { id: t.id, next, diff: "fee or wording" } }); return A.toast("Fee or public wording changed: sent to a lawyer"); }
    S.put("consultTypes", { id: t.id, ...next }); A.audit("consult.type_saved", { target: t.id }); A.toast("Saved", "ok");
  });
  A.action("intake-ct-new", () => { if (!canForms()) return; const id = A.id("ct"); S.put("consultTypes", { id, name: "New consult type", lawyers: [me().role === "admin" ? "u-linda" : me().id], duration: 30, modes: "video", days: [3], from: 10, to: 12, step: 30, notice: 24, ahead: 14, cap: 2, feeCents: 0, paused: true }); ctSel = id; A.audit("consult.type_created", { target: id }); A.toast("Created paused; set it up and resume", "ok"); });

  let setSec = "sources";
  const SECS = [["sources", "Sources"], ["areas", "Practice areas"], ["stages", "Stages"], ["service", "Service area"], ["scoring", "Scoring"], ["decline", "Decline reasons"], ["key", "Lead capture key"]];
  function settings(el) {
    A.setContext({ kind: "intake", label: "Intake lists and rules" });
    if (!A.can("intake.forms")) return page(el, h`${head("setup", { section: "setup", sub2: "settings" })}${noRole("intake setup")}`);
    const L = S.get("intakeConfig", "lists"), can = canForms(), cw = "Lawyer or admin", ls = S.all("leads");
    const used = (n) => ls.filter((l) => l.source === n || l.area === n || l.stage === n).length;
    const pend = S.where("approvals", (a) => ["intake-key", "intake-score"].includes(a.effect) && a.status === "pending");
    const body = setSec === "sources" ? A.ui.table([{ label: "Source", render: (x) => h`<b>${x.name}</b>` }, { label: "Kind", key: "kind" }, { label: "Leads", num: true, render: (x) => used(x.name) }, { label: "", render: (x) => (x.active ? A.ui.btn({ label: "Retire", a: "intake-set-retire", id: x.name, small: true, disabled: !can, why: cw }) : st("inactive", "Retired")) }], L.sources.map((x) => ({ ...x, id: x.name })))
      : setSec === "areas" ? A.ui.table([{ label: "Practice area", key: "name" }, { label: "Default lawyer", render: (x) => A.user(x.lawyer).name }, { label: "Default consult", render: (x) => (S.get("consultTypes", x.type) || { name: "" }).name }, { label: "Leads", num: true, render: (x) => used(x.name) }], L.areas.map((x) => ({ ...x, id: x.name })))
      : setSec === "stages" ? A.ui.table([{ label: "Stage", key: "name" }, { label: "Category", key: "cat" }, { label: "Target (days)", num: true, key: "sla" }, { label: "Leads now", num: true, render: (x) => used(x.name) }], L.stages.map((x) => ({ ...x, id: x.name })))
      : setSec === "service" ? h`<ul>${L.service.map((x) => h`<li>${x}</li>`)}</ul><p class="muted small">Leads outside the area are flagged, not dropped.</p>`
      : setSec === "scoring" ? h`${A.ui.table([{ label: "Factor", key: "name" }, { label: "Weight", num: true, render: (x) => `${x.weight}%` }], L.scoring.map((x) => ({ ...x, id: x.name })))}<p class="small">Score version ${L.scoreVersion}. Old scores keep their version.</p>`
      : setSec === "decline" ? h`<ul>${L.decline.map((x) => h`<li>${x}</li>`)}</ul><p class="muted small">A controlled list keeps lost-reason reports clean.</p>`
      : h`<dl class="kv"><dt>Key</dt><dd><code>${L.key}</dd><dt>Last rotated</dt><dd>${A.date(L.keyRotated)}</dd></dl><p class="muted small">Website integrations post leads with this key. After rotation the old key keeps working for 24 hours.</p>`;
    const acts = setSec === "sources" ? [{ label: "Add source", a: "intake-set-add", disabled: !can, why: cw }, { label: "✦ Find duplicates", a: "intake-set-dupes", agent: true, disabled: !can, why: cw }]
      : setSec === "scoring" ? [{ label: "✦ Suggest weights", a: "intake-set-score", agent: true, disabled: !can || pend.length > 0, why: pend.length ? "A change is awaiting approval" : cw }]
      : setSec === "key" ? [{ label: "Rotate key", a: "intake-set-key", danger: true, disabled: !A.can("admin.users", "all") || pend.length > 0, why: pend.length ? "A change is awaiting approval" : "Admin only" }] : [];
    page(el, h`${head("setup", { section: "setup", sub2: "settings", title: "Intake setup", sub: "The lists and rules every intake screen uses." })}${pend.map((g) => A.ui.gateCard(g))}
      ${A.ui.split(A.ui.list(SECS.map(([id, t]) => ({ id, title: t, on: id === setSec })), "intake-set-sec"), h`${A.ui.header({ title: (SECS.find((x) => x[0] === setSec) || [])[1], actions: acts })}${body}`)}`);
  }
  A.action("intake-set-sec", (el) => { setSec = el.dataset.id; A.refresh(); });
  const lists = () => S.get("intakeConfig", "lists");
  A.action("intake-set-retire", (el) => { if (!canForms()) return; const L = lists(); S.put("intakeConfig", { id: "lists", sources: L.sources.map((x) => (x.name === el.dataset.id ? { ...x, active: false } : x)) }); A.audit("intake.source_retired", { note: el.dataset.id }); });
  A.action("intake-set-add", () => {
    if (!canForms()) return;
    A.modal("Add source", h`<div class="grid g2"><label class="field">Name<input name="name" class="input"></label><label class="field">Kind<select name="kind">${opts(["web", "phone", "referral person", "directory", "ad", "AI receptionist"].map((x) => [x, x]), "web")}</select></label></div>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d), L = lists(); if (!(v.name || "").trim()) { A.toast("Enter a name", "bad"); return false; } if (L.sources.some((x) => x.name.toLowerCase() === v.name.trim().toLowerCase())) { A.toast("That source exists", "bad"); return false; } S.put("intakeConfig", { id: "lists", sources: [...L.sources, { name: v.name.trim(), kind: v.kind, active: true }] }); A.audit("intake.source_added", { note: v.name }); } }]);
  });
  A.action("intake-set-dupes", () => {
    if (!canForms()) return;
    A.agent.run({ title: "Find duplicate lead sources", steps: ["Comparing source names", "Counting usage"], done: () => {
      const L = lists(), seen = {}, dupes = []; L.sources.forEach((x) => { const k = x.name.toLowerCase(); if (seen[k] && x.active) dupes.push(x.name); seen[k] = true; });
      if (!dupes.length) return { title: "No duplicate sources" };
      S.put("intakeConfig", { id: "lists", sources: L.sources.map((x) => (dupes.includes(x.name) ? { ...x, active: false } : x)) }); A.audit("intake.sources_merged", { actor: "agent", note: dupes.join(", ") });
      return { title: `Merged ${dupes.join(", ")} into the existing source` };
    } });
  });
  A.effect("intake-score", (p) => { const L = lists(); S.put("intakeConfig", { id: "lists", scoring: p.scoring, scoreVersion: L.scoreVersion + 1 }); A.audit("intake.scoring_changed", { note: `v${L.scoreVersion + 1}` }); });
  A.action("intake-set-score", () => {
    if (!canForms()) return;
    A.agent.run({ title: "Suggest scoring weights", steps: ["Reading 12 months of accepted and lost leads", "Fitting weights"], done: () => {
      const scoring = [{ name: "Practice fit", weight: 35 }, { name: "Urgency", weight: 30 }, { name: "Value band", weight: 15 }, { name: "Location", weight: 5 }, { name: "Conflict pre-check", weight: 15 }];
      return A.gate({ kind: "settings", cap: "admin.users", title: "Apply new lead scoring weights", summary: "Urgency and conflict pre-check predicted retained leads better. Old scores keep their version.", preparedBy: "agent", body: String(h`<ul>${scoring.map((x) => h`<li>${x.name}: ${x.weight}%</li>`)}</ul>`), effect: "intake-score", payload: { scoring } });
    } });
  });
  A.effect("intake-key", () => { S.put("intakeConfig", { id: "lists", key: `lck_live_••••${A.id("k").slice(-4)}`, keyRotated: A.today }); A.audit("intake.key_rotated", { note: "Old key valid 24 hours" }); });
  A.action("intake-set-key", () => { if (!A.can("admin.users", "all")) return; A.gate({ kind: "settings", cap: "admin.users", title: "Rotate the lead capture key", summary: "Website forms need the new key within 24 hours.", preparedBy: me().id, effect: "intake-key", payload: {} }); A.toast("Sent for approval"); });

  /* ---------- daily-use actions ---------- */
  A.action("intake-filter", (el) => { ff = el.dataset.id; A.go("intake"); });
  A.change("intake-q", (el) => { fq = el.value.trim(); A.refresh(); });
  A.change("intake-hit", (el) => {
    const l = lead(el.dataset.id); if (!l || !l.conflict || !A.can("conflicts.run", "act") || l.conflict.clearedBy) return;
    const matches = (l.conflict.matches || []).map((x, i) => (i === +el.dataset.i ? { ...x, disp: el.value || undefined, dispBy: me().id } : x));
    S.put("leads", { id: l.id, conflict: { ...l.conflict, matches } });
    A.audit("conflict.hit_marked", { target: l.id, note: `${matches[+el.dataset.i].hit}: ${el.value || "not reviewed"}` });
  });
  A.action("intake-bulk", (el) => {
    const ids = (el.dataset.ids || "").split(",").filter(Boolean), ls = ids.map(lead).filter(Boolean), op = el.dataset.op;
    if (!A.can("intake.leads", "act")) return;
    if (op === "assign") return A.modal(`Assign ${ls.length} lead${ls.length === 1 ? "" : "s"}`, h`<label class="field">Responsible lawyer<select name="to">${opts(LAWYERS().map((u) => [u.id, u.name]), "")}</select></label>`,
      [{ label: "Cancel" }, { label: "Assign", primary: true, run: (d) => { const to = A.formValues(d).to, ok = ls.filter((l) => !closed(l)); ok.forEach((l) => S.put("leads", { id: l.id, assignee: to, why: `Assigned by ${me().name}.` })); A.audit("lead.assigned", { note: `${ok.length} leads to ${A.user(to).name}` }); A.selection(SEL).clear(); A.toast(`${ok.length} assigned to ${A.user(to).name}`, "ok"); } }]);
    const act = op === "accept" ? "intake-accept" : "intake-conflict";
    const ok = ls.filter((l) => !why(l, act)), skip = ls.length - ok.length;
    if (act === "intake-accept") ok.forEach((l) => { S.put("leads", { id: l.id, stage: "Accepted", acceptedBy: me().id }); A.audit("lead.accepted", { target: l.id, note: l.name }); });
    else ok.forEach(runCheck);
    A.selection(SEL).clear(); A.toast(`${act === "intake-accept" ? "Accepted" : "Checking"} ${ok.length}${skip ? `; skipped ${skip} (not allowed or not needed)` : ""}`, ok.length ? "ok" : "bad"); A.refresh();
  });
  const lawyerOpts = (cur) => opts(LAWYERS().map((u) => [u.id, u.name]), cur);
  A.action("intake-new", () => {
    if (!A.can("intake.leads", "act")) return;
    A.modal("New lead", h`<div class="grid">
      <label class="field">Name<input name="name" class="input" placeholder="Person or company"></label>
      <label class="field">How it came in<select name="source"><option>Phone note</option><option>Walk-in</option><option>Referral</option><option>Email (forwarded)</option></select></label>
      <label class="field">Phone<input name="phone" class="input" placeholder="416-555-0100"></label>
      <label class="field">Email<input name="email" class="input" type="email"></label>
      <label class="field">Practice area<select name="area"><option value="">Let the agent decide</option>${PRACTICE.map((p) => h`<option>${p}</option>`)}</select></label>
      <label class="field">Other side<input name="adverse" class="input" placeholder="Optional"></label>
      <label class="field">Key date (incident or discovery)<input name="discovered" class="input" type="date"></label>
      <label class="field">Referred by<input name="ref" class="input" placeholder="Optional"></label></div>
      <label class="field">What happened<textarea name="note" rows="3" class="input" placeholder="In the caller's words"></textarea></label>
      <p class="muted small">The agent classifies the area, checks the limitation date and pre-screens every name. Nothing is sent to the caller.</p>`,
    [{ label: "Cancel" }, { label: "Create lead", primary: true, run: (d) => {
      const v = A.formValues(d); const name = (v.name || "").trim(); if (!name) { A.toast("Enter a name", "bad"); return false; }
      const note = (v.note || "").trim() || "No details given.";
      const area = v.area || (/fall|slip|trip|injur|accident|crash|collision/i.test(note) ? "Personal injury" : /fired|dismiss|terminat|employ/i.test(note) ? "Employment" : /will|estate/i.test(note) ? "Estates" : /buy|sell|purchase|condo|house|closing/i.test(note) ? "Real estate" : /owe|invoice|contract|unpaid/i.test(note) ? "Commercial litigation" : "General");
      const id = A.id("lead"), by = me().id, dd = v.discovered || "";
      A.agent.run({ title: `Triage lead: ${name}`, steps: ["Classifying practice area", "Checking limitation", "Pre-screening conflicts"], done: () => {
        const hits = search(name, "client").concat(v.adverse ? search(v.adverse, "adverse").filter((x) => x.tone === "bad") : []), pre = resultOf(hits);
        const raw = [`${v.source} - ${A.user(by).name} - ${A.now()}`, `Caller: ${name}${v.phone ? `, ${v.phone}` : ""}`, note];
        S.put("leads", { id, name, kind: /\b(inc|ltd|corp|co)\b\.?/i.test(name) ? "org" : "person", email: v.email || "", phone: v.phone || "", source: v.source, received: A.now(), area, conf: v.area ? 1 : area === "General" ? 0.5 : 0.8,
          urgency: dd && A.daysBetween(A.today, addYears(dd, 2)) < 90 ? "high" : dd ? "medium" : "low", discovered: dd, limit: dd ? "basic" : "none", summary: note, referredBy: v.ref || "",
          parties: [{ name, role: "client" }].concat(v.adverse ? [{ name: v.adverse, role: "adverse" }] : []), assignee: /real estate|commercial/i.test(area) ? "u-yoni" : /estate/i.test(area) ? "u-linda" : "u-ayo", why: "Matched by practice area.", stage: "New",
          prescreen: { result: pre === "conflict" ? "hit" : pre === "review" ? "possible" : "clear", note: hits.length ? hits.map((x) => `${x.hit}: ${x.where}`).join("; ") : "No match in contacts, matters or documents." },
          facts: [["Name", name, 2], ["Account", note.slice(0, 90), 3]].concat(dd ? [["Key date", A.date(dd), 3]] : []), raw: [raw] });
        A.audit("lead.created", { target: id, note: `${v.source}: ${name}` });
        return { title: `Lead created: ${name}`, outputs: [{ label: "Open lead", go: `intake/${id}` }] };
      } });
      sel = id; ff = "open"; A.toast("The agent is triaging the lead");
    } }]);
  });
  A.action("intake-edit", (el) => {
    const l = getL(el); if (!l || why(l, "intake-edit")) return;
    A.modal(`Edit lead: ${l.name}`, h`<div class="grid">
      <label class="field">Name<input name="name" class="input" value="${l.name}"></label>
      <label class="field">Contact person<input name="contactName" class="input" value="${l.contactName || ""}"></label>
      <label class="field">Email<input name="email" class="input" value="${l.email || ""}"></label>
      <label class="field">Phone<input name="phone" class="input" value="${l.phone || ""}"></label>
      <label class="field">Practice area<select name="area">${[...new Set([l.area, ...PRACTICE])].map((p) => h`<option ${p === l.area ? A.raw("selected") : ""}>${p}</option>`)}</select></label>
      <label class="field">Responsible lawyer<select name="assignee">${lawyerOpts(l.assignee)}</select></label>
      <label class="field">Urgency<select name="urgency">${["high", "medium", "low"].map((u) => h`<option ${u === l.urgency ? A.raw("selected") : ""}>${u}</option>`)}</select></label>
      <label class="field">Discovery date<input name="discovered" type="date" class="input" value="${l.discovered || ""}"></label></div>
      <p class="muted small">Changing a party name does not re-run conflicts; run the check again after you save.</p>`,
    [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
      const v = A.formValues(d); if (!v.name.trim()) { A.toast("Name is required", "bad"); return false; }
      const parties = l.parties.map((p) => (p.role === "client" && p.name === l.name ? { ...p, name: v.name.trim() } : p));
      S.put("leads", { id: l.id, name: v.name.trim(), contactName: v.contactName, email: v.email, phone: v.phone, area: v.area, assignee: v.assignee, urgency: v.urgency, discovered: v.discovered, limit: v.discovered ? "basic" : "none", parties, why: v.assignee !== l.assignee ? `Reassigned by ${me().name}.` : l.why });
      A.audit("lead.edited", { target: l.id, note: v.name }); A.toast("Saved", "ok");
    } }]);
  });
  A.action("intake-consult", (el) => {
    const l = getL(el); if (!l || why(l, "intake-consult")) return;
    if (l.consult && !l.consult.outcome) return A.modal(`Consult outcome: ${l.name}`, h`<label class="field">Decision<select name="outcome"><option value="Retain">Retain: the firm will act</option><option value="Decline">Decline: send a non-engagement letter</option><option value="Refer">Refer elsewhere: non-engagement letter with referral</option></select></label>
      <label class="field">Refer to (if referring)<input name="refer" class="input" placeholder="Firm or the Law Society Referral Service"></label>
      <label class="field">Notes for the file<textarea name="note" rows="3" class="input"></textarea></label><p class="muted small">Notes stay on the lead and in conflict history. Decline and refer start the letter; it goes out only after approval.</p>`,
    [{ label: "Cancel" }, { label: "Record outcome", primary: true, run: (d) => {
      const v = A.formValues(d);
      S.put("leads", { id: l.id, consult: { ...l.consult, outcome: v.outcome, note: v.note, refer: v.refer, at: A.now(), by: me().id } });
      A.audit("consult.outcome", { target: l.id, note: v.outcome });
      if (v.outcome !== "Retain") startDecline(lead(l.id), v.outcome === "Refer" ? v.refer || "the Law Society Referral Service" : ""); else A.toast("Outcome recorded: retain", "ok");
    } }]);
    const d0 = A.addDays(A.today, new Date(A.today + "T12:00:00").getDay() === 5 ? 3 : 1);
    A.modal(`Book consult: ${l.name}`, h`<div class="grid">
      <label class="field">Date<input name="date" type="date" class="input" value="${d0}"></label>
      <label class="field">Time (Toronto)<input name="time" type="time" class="input" value="10:00"></label>
      <label class="field">With<select name="with">${lawyerOpts(l.assignee)}</select></label>
      <label class="field">How<select name="mode"><option>Video</option><option>Phone</option><option>In person, office</option></select></label>
      <label class="field">Fee<select name="fee"><option value="0">No charge</option><option value="15000">$150.00 + HST, paid to trust before the consult</option></select></label></div>
      <p class="muted small">The agent sends the booking with an intake questionnaire and a reminder the day before. Consult fees paid in advance are client money and go to trust.</p>`,
    [{ label: "Cancel" }, { label: "Book", primary: true, run: (d) => {
      const v = A.formValues(d); if (!v.date || v.date < A.today) { A.toast("Pick today or a later date", "bad"); return false; }
      S.put("leads", { id: l.id, consult: { date: v.date, time: v.time, with: v.with, mode: v.mode, feeCents: +v.fee, bookedBy: me().id }, stage: STAGES.indexOf(l.stage) < 3 ? "Consult" : l.stage });
      A.audit("consult.booked", { target: l.id, note: `${v.date} ${v.time} with ${A.user(v.with).name}` }); A.toast(`Consult booked ${A.date(v.date)} ${v.time}`, "ok");
    } }]);
  });

  /* ---------- keys: lead actions on the open lead (core handles j/k, Enter, x, /, n, ?) ---------- */
  const KEYACT = { c: "intake-conflict", b: "intake-consult", i: "intake-idv", l: "intake-engage", r: "intake-retainer", o: "intake-matter", e: "intake-edit", d: "intake-decline", t: "intake-takeover" };
  const onLead = (route) => (route === "intake" && view !== "board") || /^intake\/lead-|^intake\/[a-z]+-[a-z0-9]+$/.test(route) && !!lead(route.split("/")[1]);
  function fire(act, id) { const b = document.createElement("button"); b.dataset.a = act; b.dataset.id = id; document.body.appendChild(b); b.click(); b.remove(); }
  const leadKey = (label, k) => ({ label, fn: (e, route) => {
    if (!onLead(route) || document.querySelector("#view .kb-on")) return false;
    const l = sel && lead(sel); if (!l) return false;
    if (k === "a") { const g = approvableHere(l)[0]; if (g) return A.approve(g.id); const w = why(l, "intake-accept"); return w ? A.toast(w, "bad") : fire("intake-accept", l.id); }
    const w = why(l, KEYACT[k]); return w ? A.toast(w, "bad") : fire(KEYACT[k], l.id);
  } });
  A.ui.keys("intake", { a: leadKey("Approve the lead's waiting approval, else accept it", "a"), c: leadKey("✦ Run conflict check", "c"), b: leadKey("Book consult or record its outcome", "b"), i: leadKey("Verify identity", "i"), l: leadKey("✦ Draft engagement letter", "l"),
    r: leadKey("Request retainer to trust", "r"), o: leadKey("Open matter", "o"), e: leadKey("Edit lead", "e"), d: leadKey("Decline", "d"), t: leadKey("Take over from the assistant", "t") });

  /* ---------- intents ---------- */
  const findLead = (name) => { let best = null, bs = 0; S.all("leads").forEach((l) => { const s = score(name, l.name); if (s > bs) { bs = s; best = l; } }); return bs >= 0.7 ? best : null; };
  const noAccess = { text: "Intake is not available to your role." };
  A.intent({ match: /new lead from\s+(.+)/i, label: "New lead from…", example: "new lead from Rahel Bekele, slip and fall at a grocery store on 2025-01-20", run(text) {
    if (!A.can("intake.leads", "act")) return noAccess;
    const rest = /new lead from\s+(.+)/i.exec(text)[1].trim(); const name = rest.split(/[,;:]/)[0].trim(); const note = rest.slice(name.length).replace(/^[,;:\s]+/, "") || "No details given.";
    const d = (/(\d{4}-\d{2}-\d{2})/.exec(note) || [])[1] || "";
    const area = /fall|slip|trip|injur|accident|crash|collision/i.test(note) ? "Personal injury" : /fired|dismiss|terminat|employ/i.test(note) ? "Employment" : /will|estate/i.test(note) ? "Estates" : /buy|sell|purchase|condo|house/i.test(note) ? "Real estate" : /owe|invoice|contract/i.test(note) ? "Commercial litigation" : "General";
    const id = A.id("lead");
    A.agent.run({ title: `Triage lead: ${name}`, steps: ["Classifying practice area", "Checking limitation", "Pre-screening conflicts"], done: () => {
      const hits = search(name, "client"); const pre = resultOf(hits);
      S.put("leads", { id, name, kind: /inc|ltd|corp/i.test(name) ? "org" : "person", email: "", phone: "", source: "Agent chat", received: A.now(), area, conf: area === "General" ? 0.5 : 0.8, urgency: d && A.daysBetween(A.today, addYears(d, 2)) < 90 ? "high" : "medium", discovered: d, limit: d ? "basic" : "none",
        summary: note, parties: [{ name, role: "client" }], assignee: area === "Real estate" || area === "Commercial litigation" ? "u-yoni" : area === "Estates" ? "u-linda" : "u-ayo", why: "Matched by practice area.", stage: "New",
        prescreen: { result: pre === "conflict" ? "hit" : pre === "review" ? "possible" : "clear", note: hits.length ? hits.map((x) => `${x.hit}: ${x.where}`).join("; ") : "No match in contacts, matters or documents." },
        facts: [["Name", name, 2], ["Note", note, 3]].concat(d ? [["Date", d, 3]] : []), raw: [[`Agent chat - ${A.now()}`, `Name: ${name}`, note]] });
      return { title: `Lead created: ${name}` };
    } });
    return { text: `Triaging ${name} as ${area.toLowerCase()}. The lead will be in the inbox in a moment.`, actions: [{ label: "Open lead", go: `intake/${id}` }, { label: "Lead inbox", go: "intake" }] };
  } });
  A.intent({ match: /(?:run )?(?:a )?conflicts? check (?:on|for)\s+(.+)/i, label: "Run a conflict check", example: "run a conflict check on Northgate Storage", run(text) {
    if (!A.can("conflicts.run", "act")) return { text: "Your role cannot run conflict searches." };
    const name = /(?:on|for)\s+(.+)/i.exec(text)[1].replace(/[?.]$/, "").trim();
    const hits = [...search(name, "client"), ...search(name, "adverse").filter((x) => x.tone === "bad")];
    const ms = [...new Set(hits.map((x) => S.all("matters").find((m) => x.where.includes(m.number))).filter((m) => m && A.canMatter(m)))];
    if (!hits.length) return { text: `No matches for "${name}" in contacts, matters or documents. A full check on a lead also searches every other party.` };
    return { text: `${hits.length} match${hits.length > 1 ? "es" : ""} for "${name}": ${hits.slice(0, 4).map((x) => `${x.hit} (${Math.round(x.score * 100)}%) — ${x.where}. ${x.note}`).join(" ")}`,
      cites: hits.filter((x) => x.cite).map((x) => x.cite), actions: ms.slice(0, 3).map((m) => ({ label: `${m.number} ${m.name}`, go: `matters/${m.id}` })).concat([{ label: "Conflict register", go: "intake/conflicts" }]) };
  } });
  A.intent({ match: /open (?:a )?(?:new )?matter for\s+(.+)/i, label: "Open a matter for…", example: "open a matter for Kestrel Print", run(text) {
    if (!A.can("intake.leads")) return noAccess;
    const name = /for\s+(.+)/i.exec(text)[1].replace(/[?.]$/, "").trim(); const l = findLead(name);
    if (!l) {
      const c = S.all("contacts").find((x) => score(name, x.name) >= 0.85); const m = c && A.matters((x) => x.clientId === c.id)[0];
      if (m) return { text: `${c.name} is already a client on ${m.number} ${m.name}. To open another matter, start a lead so conflicts are checked for the new parties.`, actions: [{ label: m.number, go: `matters/${m.id}` }] };
      return { text: `No lead matches "${name}". Start one with "new lead from ${name}, …" so the agent can triage it.` };
    }
    const c = l.conflict, go = [{ label: `Open lead: ${l.name}`, go: `intake/${l.id}` }];
    if (l.matterId) return { text: `${l.name} was opened as a matter already.`, actions: [{ label: "Go to matter", go: `matters/${l.matterId}` }] };
    if (l.stage === "Declined") return { text: `${l.name} was declined; a non-engagement letter went out.`, actions: go };
    if (!c || c.status !== "cleared") return { text: `Next gate for ${l.name}: conflicts. ${c ? `The last check found "${c.status}" and it is not cleared yet; a lawyer with conflict-clearing authority must sign the clearance memo.` : "No conflict check has run. Run it from the lead; a lawyer then clears it."}`, actions: go };
    if (l.engagement !== "approved") return { text: `Conflicts are cleared for ${l.name}. Next gate: the engagement letter. ${pendingGate(l.id, "intake-engage") ? "A draft is waiting for approval by someone other than its preparer." : "Draft it from the lead; another lawyer approves its release."}`, actions: go };
    return { text: `${l.name} is ready: conflicts cleared and engagement approved. "Open matter" sends a final approval to a lawyer; on approval I create the client, the matter number and the team.`, actions: go };
  } });
  A.suggest("lead", (c) => { const l = lead(c.id); return l ? [`open a matter for ${l.name}`, `run a conflict check on ${l.parties[l.parties.length - 1].name}`] : []; });
  A.suggest("intake", () => ["run a conflict check on Northgate Storage", "open a matter for Kestrel Print", "new lead from Rahel Bekele, slip and fall at a grocery store on 2025-01-20"]);

  /* ---------- module ---------- */
  A.module({ id: "intake", label: "Intake", icon: "↘", order: 10, cap: "intake.leads",
    badge: () => S.all("leads").filter(needsMe).length,
    smoke: ["intake", "intake/conflicts", "intake/review", "intake/sources", "intake/lead-north", "intake/lead-fall", "intake/lead-kestrel", "intake/lead-estate", "intake/lead-condo", "intake/lead-dismiss",
      "intake/clearance/lead-north", "intake/clearance/lead-kestrel", "intake/consents", "intake/records", "intake/laterals", "intake/client-id", "intake/conflict-settings", "intake/org/lead-north", "intake/org/lead-kestrel",
      "intake/forms", "intake/public/f-pi", "intake/public/f-re", "intake/sequences", "intake/receptionist", "intake/consult-types", "intake/settings"],
    routes: { "intake": inbox, "intake/conflicts": register, "intake/review": review, "intake/sources": sources, "intake/consents": consents, "intake/records": records, "intake/laterals": laterals, "intake/client-id": clientId,
      "intake/conflict-settings": conflictSettings, "intake/forms": forms, "intake/sequences": sequences, "intake/receptionist": receptionist, "intake/consult-types": consultTypes, "intake/settings": settings,
      "intake/clearance/:id": clearance, "intake/org/:id": orgView, "intake/public/:id": publicForm, "intake/:id": detail } });
})();
