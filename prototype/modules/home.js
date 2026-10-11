/* Home module: Today (what needs me), the firm approvals queue (list + review pane) and Agent & audit (runs, event log). */
(function () {
  "use strict";
  const A = window.App, h = A.h;

  const st = (w, label) => A.ui.status(w, label);

  /* ---------- role helpers ---------- */
  const role = () => A.me().role;
  const isClient = () => role() === "client";
  const isAdmin = () => role() === "admin";
  const isLawyer = () => ["owner", "lawyer", "associate"].includes(role());
  const firstName = (u) => (u || A.me()).name.split(" ")[0];
  /** Firm-wide log needs admin.audit_log at view/all; "own" sees own events and own matters only. */
  const fullLog = () => ["view", "all", "approve"].includes(A.grant("admin.audit_log"));
  const plural = (n, w, ws) => `${n} ${n === 1 ? w : ws || w + "s"}`;
  /** Minutes since epoch for a stored "YYYY-MM-DD HH:MM" (Toronto wall clock); only used for differences. */
  const mins = (s) => { const t = Date.parse(String(s || "").replace(" ", "T") + ":00Z"); return isNaN(t) ? 0 : t / 60000; };
  /** Model cost in integer cents (older runs stored dollars). */
  const runCents = (r) => (r.costCents != null ? +r.costCents : r.cost != null ? Math.round(+r.cost * 100) : null);

  /* ---------- approvals: visibility, risk, due ---------- */
  const visibleGate = (g) => {
    if (isClient()) return false;
    if (g.preparedBy === A.me().id) return true;
    if (g.matterId) return A.canMatter(A.store.get("matters", g.matterId));
    return A.can(g.cap, "view");
  };
  const visibleGates = () => A.store.where("approvals", visibleGate);
  const KIND = { email: "Correspondence", letter: "Letters", "portal-reply": "Correspondence", court: "Court filings", trust: "Trust", "trust-withdrawal": "Trust", "trust-reconciliation": "Trust", "trust-reopen": "Trust",
    billing: "Billing", bill: "Billing", document: "Documents", deadline: "Deadlines", conflict: "Conflicts", identity: "Conflicts", wall: "Ethical walls", access: "Access", security: "Security", review: "Access",
    matter: "New matters", work: "Work", filing: "Mail filing", time: "Time", "agent-step": "Agent step" };
  const kindName = (k) => KIND[k] || (k ? k[0].toUpperCase() + k.slice(1).replace(/-/g, " ") : "Other");
  /** Money movement, deadlines, conflicts and access are always high; internal filing and time are low; outbound drafts medium. */
  const risk = (g) => g.risk || (/^(trust|billing|deadlines|conflicts)\.|^admin\./.test(g.cap) || /trust|bill|deadline|conflict|wall|access|security|identity/.test(g.kind || "") ? "high"
    : /^(filing|time|work)$/.test(g.kind || "") ? "low" : "medium");
  const riskChip = (g) => st(`${risk(g)}-risk`);
  const SLA = { high: 0, medium: 1, low: 3 };
  const gateDue = (g) => g.due || A.addDays((g.created || A.today).slice(0, 10), SLA[risk(g)]);
  const dueText = (d) => { const n = A.daysBetween(A.today, d); return n < 0 ? `${-n} days overdue` : n === 0 ? "today" : n === 1 ? "tomorrow" : A.date(d); };
  const isOverdue = (g) => g.status === "pending" && gateDue(g) < A.today;
  const order = (a, b) => (gateDue(a) < gateDue(b) ? -1 : gateDue(a) > gateDue(b) ? 1 : "hml".indexOf(risk(a)[0]) - "hml".indexOf(risk(b)[0]));
  const approverText = (g) => (g.assignee ? A.user(g.assignee).name : `Any: ${A.capName(g.cap)}`);
  /** People who may approve this gate: active, hold the capability at approve, did not prepare it, can see the matter. Agent never. */
  function eligible(g) {
    const m = g.matterId && A.store.get("matters", g.matterId);
    return A.store.all("users").filter((u) => {
      if (u.role === "client" || u.status === "inactive" || u.id === g.preparedBy || A.grant(g.cap, u.role) !== "approve") return false;
      if (g.cap === "trust.authorize" && !u.signatory) return false;
      if (!m) return true;
      if ((m.wall || []).includes(u.id)) return false;
      const team = (m.team || []).includes(u.id); if (m.restricted && !team) return false;
      const lv = A.grant("matters.view", u.role); return lv === "all" || lv === "approve" || lv === "view" || (lv === "own" && team);
    });
  }
  /** Supervisor for re-routing: the matter's responsible lawyer, then the principal, then any eligible approver. */
  function supervisorFor(uid, g) {
    const el = eligible(g).filter((u) => u.id !== uid), m = g.matterId && A.store.get("matters", g.matterId);
    const pref = [m && m.responsible, ...A.store.where("users", (u) => u.role === "owner").map((u) => u.id), ...A.store.where("users", (u) => u.role === "lawyer").map((u) => u.id)];
    return (pref.map((id) => el.find((u) => u.id === id)).find(Boolean) || el[0] || null);
  }
  /** One status chip for approvals and runs, from the shared vocabulary. */
  function chip(x) {
    if (x.steps) return st(x.status);
    if (x.status === "pending") {
      const l = lockedByOther(x);
      if (x.stale) return st("stale");
      if (l) return st("in-review", `In review: ${firstName(A.user(l.by))}`);
      if (x.assignee && x.assignee !== A.me().id) return st("waiting", `With ${firstName(A.user(x.assignee))}`);
      return st("needs-approval");
    }
    if (x.status === "approved" && x.decision === "approved with edits") return st("approved", "Approved with edits");
    return st(x.status);
  }
  /** For me: pending items I can approve that are not assigned to someone else. */
  const forMe = () => A.pendingFor().filter((g) => !g.assignee || g.assignee === A.me().id).sort(order);
  const effectLine = (g) => g.ifApproved || ({ email: "The reply is sent to the recipient and filed on the matter.", letter: "The letter is released, sent and filed on the matter.", "portal-reply": "The message is posted to the client portal.",
    filing: "The filings are confirmed on the matters; nothing leaves the firm.", time: "The entries become billable time on the matter.", "agent-step": "The paused agent run continues with its next step." }[g.kind] || "The prepared action is carried out and logged.");

  /* ---------- seed: agent-prepared approvals that cite shared docs ---------- */
  A.seed("approvals", [
    { id: "ap-home-osei", kind: "email", cap: "documents.release", matterId: "m1", status: "pending", created: "2026-10-09 08:12", preparedBy: "agent", due: "2026-10-09",
      title: "Reply to D. Osei: consent to 30-day extension, terms attached",
      summary: "Opposing counsel asked for a 30-day extension to deliver the defence and for the storage agreement and invoices. Draft consents on terms and declines documents until affidavits of documents.",
      body: "<p>Mr. Osei,</p><p>Thank you for your email of October 8, 2026. Our client consents to a 30-day extension for delivery of the statement of defence, to November 12, 2026, on the condition that Northgate Storage does not note our client in default on any step in the interim and that the extension is confirmed in writing.</p><p>The storage agreement and inventory invoices will be produced with our client's affidavit of documents under rule 30.03. We are prepared to discuss an earlier exchange if your client will produce its roof inspection and repair records for 2026.</p><p>Linda Kassim<br>Linda K Law</p>",
      cites: [{ doc: "d-email-osei", page: 1, line: 5, quote: "We ask for your consent to a 30-day extension to deliver the statement of defence." }, { doc: "d-email-osei", page: 1, line: 6, quote: "We also request copies of the storage agreement and the inventory invoices." }, { doc: "d-aos", page: 1, line: 3, quote: "On September 23, 2026 ... I served Northgate Storage Inc." }],
      checks: [{ label: "Service date confirmed from affidavit of service (Sept 23, 2026)", ok: true }, { label: "Defence due date recomputed: 20 days after service, extended to Nov 12, 2026", ok: true }, { label: "No privileged content in draft", ok: true, blocking: true }],
      ifApproved: "The reply is emailed to D. Osei, filed on Tesfaye, and the extended defence date (Nov 12, 2026) is proposed for verification.",
      effect: "home-release-doc", payload: { matterId: "m1", name: "Reply to D. Osei re extension", lines: ["To: Daniel Osei, Brampton & Vale LLP", "Re: Tesfaye v. Northgate Storage", "Consent to 30-day extension to November 12, 2026, on terms.", "Productions to follow with affidavit of documents (rule 30.03)."] } },
    { id: "ap-home-osei-v1", kind: "email", cap: "documents.release", matterId: "m1", status: "superseded", supersededBy: "ap-home-osei", created: "2026-10-08 18:20", preparedBy: "agent", decidedBy: "agent", decidedAt: "2026-10-09 08:12",
      title: "Reply to D. Osei: consent to extension (first draft)",
      summary: "First draft consented to the extension without terms. Replaced once the affidavit of service was read and the defence date recomputed.",
      body: "<p>Mr. Osei,</p><p>Our client consents to a 30-day extension for delivery of the statement of defence.</p>",
      cites: [{ doc: "d-email-osei", page: 1, line: 5, quote: "We ask for your consent to a 30-day extension to deliver the statement of defence." }], checks: [{ label: "Service date confirmed", ok: false }] },
    { id: "ap-home-haile", kind: "letter", cap: "documents.release", matterId: "m5", status: "pending", created: "2026-10-09 07:55", preparedBy: "agent",
      title: "Demand letter to Northgate Logistics: reasonable notice",
      summary: "Six years' service as warehouse supervisor; employer paid ESA minimum only. Draft demands common-law reasonable notice of 8 months and benefits continuation.",
      body: "<p>Northgate Logistics Inc.</p><p>Re: Tsion Haile, termination without cause, August 29, 2026</p><p>We act for Ms. Haile. Your letter of August 29, 2026 provides two weeks' pay in lieu of notice, the statutory minimum under the Employment Standards Act, 2000. Ms. Haile's contract contains no enforceable termination clause, so she is entitled to reasonable notice at common law.</p><p>Having regard to her age, six years of service as a supervisor and the market for comparable roles, we assess reasonable notice at eight months. We ask that you confirm within 14 days that you will pay eight months' salary and continue benefits for the notice period.</p>",
      cites: [{ doc: "d-term", page: 1, line: 4, quote: "Your employment as Warehouse Supervisor is terminated effective today, without cause." }, { doc: "d-term", page: 1, line: 5, quote: "You will receive two weeks' pay in lieu of notice" }, { doc: "d-term", page: 1, line: 6, quote: "Your start date was July 12, 2020. Benefits end September 30, 2026." }],
      checks: [{ label: "Start date and termination date match the letter", ok: true }, { label: "Employment contract on file", ok: false }, { label: "Limitation period: 2 years from Aug 29, 2026", ok: true }],
      effect: "home-release-doc", payload: { matterId: "m5", name: "Demand letter to Northgate Logistics", lines: ["To: Northgate Logistics Inc.", "Re: Tsion Haile", "Demand for 8 months' reasonable notice and benefits continuation.", "Response requested within 14 days."] } },
    { id: "ap-home-fortier", kind: "letter", cap: "documents.release", matterId: "m3", status: "pending", created: "2026-10-08 16:40", preparedBy: "agent",
      title: "Notice and preservation letter to Lakeshore Transit",
      summary: "Puts the transit operator on notice of the claim and asks it to preserve platform video, the cleaning log and staff records for August 14, 2026.",
      body: "<p>Lakeshore Transit Corp.</p><p>Re: Daniel Fortier, fall at Humber Bay station, August 14, 2026</p><p>We act for Mr. Fortier, who slipped on a wet surface on Platform 2 at about 8:10 a.m. Your incident report records that no wet-floor sign was in place and that the cleaning log for 7:00 to 9:00 a.m. was not completed.</p><p>Please preserve all video of Platform 2 and the stairwell for that morning, the cleaning log, the incident report and the shift records for R. Singh.</p>",
      cites: [{ doc: "d-incident", page: 1, line: 2, quote: "Date of incident: August 14, 2026, approx. 8:10 a.m." }, { doc: "d-incident", page: 1, line: 4, quote: "no wet-floor sign in place" }, { doc: "d-incident", page: 1, line: 5, quote: "Cleaning log for 7:00-9:00 a.m. not completed." }],
      checks: [{ label: "Notice within 10 days not required (not a municipal road claim)", ok: true }, { label: "Client authorized contact with the operator", ok: true }],
      effect: "home-release-doc", payload: { matterId: "m3", name: "Preservation letter to Lakeshore Transit", lines: ["To: Lakeshore Transit Corp.", "Re: Daniel Fortier, August 14, 2026", "Notice of claim and request to preserve video, cleaning log and staff records."] } },
    { id: "ap-home-file-m1", kind: "filing", cap: "work.approve", matterId: "m1", status: "pending", created: "2026-10-09 06:30", preparedBy: "agent", risk: "low",
      title: "Confirm filing of 2 incoming emails to Tesfaye",
      summary: "Two overnight emails matched Tesfaye v. Northgate Storage by sender and court file number. Confirm the filing; nothing leaves the firm.",
      cites: [{ doc: "d-email-osei", page: 1, line: 1, quote: "From: Daniel Osei" }], checks: [{ label: "Sender is counsel of record on the matter", ok: true }],
      effect: "home-confirm-filing", payload: { count: 2, matterId: "m1" } },
    { id: "ap-home-file-m3", kind: "filing", cap: "work.approve", matterId: "m3", status: "pending", created: "2026-10-09 06:31", preparedBy: "agent", risk: "low",
      title: "Confirm filing of clinic records email to Fortier",
      summary: "Humber Physiotherapy sent the treatment summary requested on October 2. Matched by client name and date of loss. Nothing leaves the firm.",
      cites: [{ doc: "d-incident", page: 1, line: 2, quote: "Date of incident: August 14, 2026" }], checks: [{ label: "Client name and date of loss match", ok: true }],
      effect: "home-confirm-filing", payload: { count: 1, matterId: "m3" } },
    { id: "ap-home-time-m4", kind: "time", cap: "time.approve", matterId: "m4", status: "pending", created: "2026-10-06 17:10", preparedBy: "agent", risk: "low", due: "2026-10-07", assignee: "u-linda",
      title: "Accept 5 proposed time entries on Lindqvist Estate (2.1 h)",
      summary: "Built from sent email and calendar: probate application review, call with the estate trustee, two letters to the bank and the Form 74.4 affidavit.",
      checks: [{ label: "Each entry links to an email or calendar event", ok: true }, { label: "No entry overlaps another on the same day", ok: true }],
      escalations: [{ level: 1, to: "u-linda", at: "2026-10-08 09:00", text: "Reminder: 5 time entries on Lindqvist Estate were due for acceptance on October 7." }],
      effect: "home-accept-time", payload: { matterId: "m4", count: 5, hours: 2.1 } },
    { id: "ap-home-expired", kind: "portal-reply", cap: "documents.release", matterId: "m4", status: "pending", created: "2026-10-05 10:00", preparedBy: "agent", expires: "2026-10-08",
      title: "Portal reply to the estate trustee: probate timeline",
      summary: "Answers the trustee's question about when the certificate of appointment may issue. The answer was time-sensitive, so the draft expired unanswered.",
      body: "<p>Thank you for your question. The application for a certificate of appointment of estate trustee with a will was filed on September 30, 2026. Current processing times at the Toronto estates office are several weeks; we will tell you as soon as the certificate issues.</p>",
      cites: [{ doc: "d-will", page: 1, line: 1, quote: "Last Will of Margaret Lindqvist" }], checks: [{ label: "Filing date matches the court record", ok: true }],
      effect: "home-release-doc", payload: { matterId: "m4", name: "Portal reply re probate timeline", lines: ["Probate application filed September 30, 2026; certificate pending."] } },
  ]);
  /* A reviewer already has the Fortier letter open (soft lock). */
  A.seed("approvalLocks", [{ id: "ap-home-fortier", by: "u-ayo", at: A.now() }]);

  /* ---------- seed: finished plan runs ---------- */
  A.seed("runs", [
    { id: "run-home-haile", title: "Prepare the demand letter on Haile", matterId: "m5", by: "u-ayo", started: "2026-10-09 07:41", finished: "2026-10-09 07:55", status: "done", step: 5, skill: "demand-letter v3", costCents: 42,
      steps: ["Read the termination letter", "Pull start date and pay in lieu", "Assess reasonable notice", "Draft the demand letter", "Send the draft for lawyer approval"],
      plan: [{ label: "Read the termination letter" }, { label: "Pull start date and pay in lieu" }, { label: "Assess reasonable notice" }, { label: "Draft the demand letter" }, { label: "Send the draft for lawyer approval" }],
      result: "Draft ready for approval", cites: [{ doc: "d-term", page: 1, line: 5, label: "Termination letter" }, { doc: "d-term", page: 1, line: 6, label: "Termination letter" }], outputs: [{ label: "Review the draft", go: "approvals/ap-home-haile" }] },
    { id: "run-home-mail", title: "File overnight mail", matterId: null, by: "u-linda", started: "2026-10-09 06:28", finished: "2026-10-09 06:31", status: "done", step: 3, skill: "mail-filing v4", costCents: 6,
      steps: ["Read 3 new emails", "Match each to a matter", "Ask for confirmation of the filings"], result: "3 emails matched to 2 matters; filings wait for confirmation", outputs: [{ label: "Confirm filings", go: "approvals/ap-home-file-m1" }] },
    { id: "run-home-fortier", title: "Collect records for Fortier", matterId: "m3", by: "u-ayo", started: "2026-10-08 15:02", finished: "2026-10-08 15:03", status: "stopped", step: 2, stoppedBy: "u-ayo", skill: "records-request v2", costCents: 11,
      steps: ["Read the incident report", "Draft records requests", "Send requests to the clinic and the operator"],
      plan: [{ label: "Read the incident report" }, { label: "Draft records requests" }, { label: "Send requests to the clinic and the operator", gate: true }], result: "Stopped at step 2 of 3" },
  ]);

  A.effect("home-release-doc", (p, g) => {
    const d = A.store.put("docs", { id: A.id("d"), matterId: p.matterId, name: p.name, kind: g.kind === "email" ? "email" : "letter", added: A.today, by: "agent", released: true, pages: [p.lines || [g.title]] });
    A.audit("document.released", { target: d.id, matterId: p.matterId, note: `${p.name} (approval ${g.id})` });
  });
  A.effect("home-confirm-filing", (p, g) => A.audit("mail.filed", { actor: "agent", target: g.id, matterId: p.matterId, note: `${plural(p.count, "email")} filed after confirmation` }));
  A.effect("home-accept-time", (p, g) => A.audit("time.accepted", { target: g.id, matterId: p.matterId, note: `${plural(p.count, "entry", "entries")}, ${p.hours} h` }));

  /* ---------- sweeps: expiry and leaver re-routing (agent actions, logged) ---------- */
  function sweepWork() {
    const left = new Set(A.store.where("users", (u) => u.status === "inactive").map((u) => u.id));
    return A.store.where("approvals", (g) => g.status === "pending" && ((g.expires && g.expires < A.today) || (g.assignee && left.has(g.assignee))));
  }
  function reassignFrom(uid, reason, demo) {
    let n = 0;
    A.store.where("approvals", (g) => g.status === "pending" && g.assignee === uid).forEach((g) => {
      const sup = supervisorFor(uid, g); if (!sup) return;
      A.store.put("approvals", { id: g.id, assignee: sup.id, reassigned: { from: uid, to: sup.id, at: A.now(), auto: true, reason, demo: !!demo } });
      A.audit("gate.reassigned", { actor: "agent", target: g.id, matterId: g.matterId || null, note: `${g.title}: ${A.user(uid).name} (${reason}) → ${sup.name}` }); n++;
    });
    return n;
  }
  function sweep() {
    const work = sweepWork(); if (!work.length) return;
    const left = new Set(A.store.where("users", (u) => u.status === "inactive").map((u) => u.id));
    work.forEach((g) => {
      if (g.expires && g.expires < A.today) { A.store.put("approvals", { id: g.id, status: "expired", decidedBy: "agent", decidedAt: A.now() }); A.audit("gate.expired", { actor: "agent", target: g.id, matterId: g.matterId || null, note: g.title }); }
    });
    left.forEach((uid) => reassignFrom(uid, "left the firm"));
  }
  let sweepQueued = false;
  /** Called from render: the store write is deferred so render stays pure. */
  const queueSweep = () => { if (sweepQueued || !sweepWork().length) return; sweepQueued = true; setTimeout(() => { sweepQueued = false; sweep(); }, 0); };

  /* ---------- soft lock: one reviewer at a time, with takeover ---------- */
  const LOCK_MIN = 15;
  const lockOf = (g) => { const l = g && A.store.get("approvalLocks", g.id); return l && l.by && mins(A.now()) - mins(l.at) < LOCK_MIN ? l : null; };
  const lockedByOther = (g) => { const l = lockOf(g); return l && l.by !== A.me().id ? l : null; };
  function claim(g) {
    if (!g || g.status !== "pending" || !A.canApprove(g).ok || lockOf(g)) return;
    A.store.where("approvalLocks", (l) => l.by === A.me().id && l.id !== g.id).forEach((l) => A.store.remove("approvalLocks", l.id));
    A.store.put("approvalLocks", { id: g.id, by: A.me().id, at: A.now() });
  }
  let claimQueued = null;
  const queueClaim = (g) => { if (!g || claimQueued === g.id || g.status !== "pending" || lockOf(g) || !A.canApprove(g).ok) return; claimQueued = g.id; setTimeout(() => { claimQueued = null; claim(A.store.get("approvals", g.id)); }, 0); };
  const release = (id) => { const l = A.store.get("approvalLocks", id); if (l) A.store.remove("approvalLocks", id); };
  A.action("hm-takeover", (el) => {
    const g = A.store.get("approvals", el.dataset.id), l = lockedByOther(g); if (!g || !l) return;
    if (!A.canApprove(g).ok) return A.toast(A.canApprove(g).why, "bad");
    A.store.put("approvalLocks", { id: g.id, by: A.me().id, at: A.now(), takenFrom: l.by });
    A.audit("gate.lock_taken_over", { target: g.id, matterId: g.matterId || null, note: `from ${A.user(l.by).name}` });
    A.toast(`You took over the review from ${A.user(l.by).name}; they are told`, "ok");
  });

  /* ---------- step-up re-authentication (mock) for money movement, matter close (G13) and access grants ---------- */
  const stepUp = {};
  const needsStepUp = (g) => A.needsStepUp(g);
  function withStepUp(g, fn) {
    const me = A.me().id;
    if (!needsStepUp(g) || (stepUp[me] || 0) > mins(A.now())) return fn();
    const ok = (how) => { stepUp[me] = mins(A.now()) + 10; A.audit("auth.step_up", { target: g.id, matterId: g.matterId || null, note: `${how}; valid 10 minutes` }); setTimeout(fn, 0); };
    A.modal("Confirm it is you", h`<p class="muted small">demo · no real sign-in check</p>
      <p class="small">${g.title}</p><p class="small">Approving money movement, a matter close or an access change needs a fresh sign-in. Enter the 6-digit code from your authenticator app, or use your passkey.</p>
      <label class="field">Code<input id="hm-otp" class="input" inputmode="numeric" maxlength="6" autocomplete="one-time-code" placeholder="6 digits"></label>`,
      [{ label: "Cancel", run: () => A.cancelStepUp() }, { label: "Use passkey", run: () => ok("passkey") },
        { label: "Verify", primary: true, run: () => { const v = ((document.getElementById("hm-otp") || {}).value || "").trim(); if (!/^\d{6}$/.test(v)) { A.toast("Enter the 6-digit code", "bad"); return false; } ok("authenticator code"); } }]);
  }

  A.onStepUp(withStepUp); // core App.approve asks for step-up through this prompt, from every module

  /* ---------- work due (deadlines + tasks) ---------- */
  const closed = (x) => /^(done|closed|complete|completed|cancelled)$/i.test(String(x.status || "")) || x.done;
  function upcoming(days = 7) {
    const me = A.me().id, end = A.addDays(A.today, days), rows = [];
    [["deadlines", "Deadline", "deadlines"], ["workItems", "Task", "work"]].forEach(([c, label, path]) => {
      A.store.all(c).forEach((x) => {
        const due = String(x.due || x.dueDate || x.date || "").slice(0, 10); if (!due || closed(x) || due > end) return;
        if (x.matterId && !A.canMatter(A.store.get("matters", x.matterId))) return;
        if (!x.matterId && x.assignee && x.assignee !== me) return;
        rows.push({ id: x.id, label, title: x.title || x.name || "Untitled", due, matterId: x.matterId, go: `${path}/${x.id}`, mine: x.assignee === me || x.owner === me, critical: !!x.critical || x.priority === "High" });
      });
    });
    return rows.sort((a, b) => (a.due < b.due ? -1 : a.due > b.due ? 1 : b.critical - a.critical));
  }
  const myMatters = () => A.matters((m) => m.status === "open" && (m.team || []).includes(A.me().id));
  const visibleRun = (r) => !isClient() && (r.by === A.me().id || fullLog() || (r.matterId && A.canMatter(A.store.get("matters", r.matterId))) || (!r.matterId && isLawyer()));
  const myRuns = () => A.store.where("runs", visibleRun);
  const runsWaitingOnMe = () => { const mine = new Set(A.pendingFor().map((g) => g.id)); return myRuns().filter((r) => r.status === "waiting" && r.gateId && mine.has(r.gateId)); };

  /* ---------- plan: answer the extension request end to end ---------- */
  const oseiCites = [{ doc: "d-email-osei", page: 1, line: 5, label: "Email from D. Osei" }, { doc: "d-aos", page: 1, line: 3, label: "Affidavit of service" }];
  function oseiPlan() {
    return { title: "Answer the extension request on Tesfaye", matterId: "m1", cap: "documents.release", cites: oseiCites,
      steps: ["Read D. Osei's email of October 8", "Confirm the service date from the affidavit of service", "Recompute the defence date with the extension", "Draft the reply on terms",
        { label: "Lawyer approves the reply before it goes to opposing counsel", gate: true, kind: "email", title: "Step approval: send extension reply to D. Osei",
          summary: "Consent to a 30-day extension to November 12, 2026, on terms; productions to follow with the affidavit of documents (r.30.03).",
          body: "<p>Mr. Osei, our client consents to a 30-day extension for delivery of the statement of defence, to November 12, 2026, provided your client takes no default step in the interim. The storage agreement and invoices will be produced with our affidavit of documents.</p>",
          cites: oseiCites, checks: [{ label: "Service date Sept 23, 2026 matches the affidavit", ok: true }, { label: "No privileged content", ok: true, blocking: true }] },
        "Send the reply and file it on the matter", "Diary the new defence date for confirmation"],
      done() {
        const d = A.store.put("docs", { id: A.id("d"), matterId: "m1", name: "Reply to D. Osei (sent by agent run)", kind: "email", added: A.today, by: "agent", released: true, pages: [["To: Daniel Osei, Brampton & Vale LLP", "Re: Tesfaye v. Northgate Storage", "Consent to 30-day extension to November 12, 2026, on terms."]] });
        A.audit("document.released", { target: d.id, matterId: "m1", note: "Extension reply sent after step approval" });
        return { title: "Reply sent and filed; new defence date diarized", cites: oseiCites, outputs: [{ label: "Open matter", go: "matters/m1" }, { label: "Deadlines", go: "deadlines" }] };
      } };
  }

  function brief() {
    const pend = forMe(), up = upcoming(), mm = myMatters();
    const overdue = up.filter((x) => x.due < A.today).length, today = up.filter((x) => x.due === A.today).length;
    if (isAdmin()) {
      const audits = A.store.where("audit", (a) => (a.at || "").startsWith(A.today)).length;
      return `${plural(pend.length, "access or security item")} wait for you. ${A.store.all("users").length} user accounts; ${plural(audits, "audited event")} today. Matter content is outside your role.`;
    }
    if (role() === "bookkeeper") {
      const money = pend.filter((g) => /^(trust|billing)\./.test(g.cap));
      return `${plural(money.length, "trust or billing item")} wait for you. September's trust reconciliation is due by October 25 (By-Law 9, s.22); I will draft it when you ask.`;
    }
    if (role() === "auditor") return `Read-only access to the audit log and firm reports. ${plural(A.store.all("audit").length, "audited event")} on record.`;
    const high = pend.filter((g) => risk(g) === "high").length, late = pend.filter(isOverdue).length;
    const parts = [pend.length ? `${plural(pend.length, "item")} need${pend.length === 1 ? "s" : ""} your approval${high ? `, ${high} high risk` : ""}${late ? `, ${late} overdue` : ""}.` : "Nothing is waiting for your approval."];
    if (overdue || today) parts.push(`${overdue ? `${overdue} overdue, ` : ""}${today} due today.`);
    const osei = A.store.get("approvals", "ap-home-osei");
    if (osei && osei.status === "pending" && A.matter("m1")) parts.push("Opposing counsel on Tesfaye asked for an extension; my reply is ready.");
    parts.push(`You are on ${plural(mm.length, "open matter")}.`);
    return `Good morning, ${firstName()}. ` + parts.join(" ");
  }

  /* ---------- keyboard: a / e / b act on the focused or open approval; j/k/Enter/x// come from core ---------- */
  const pick = { approvals: null, activity: null, runs: null };
  A.onUser(() => { pick.approvals = pick.activity = pick.runs = null; });
  function targetGate(route) {
    const cur = document.querySelector("#view .kb-on"); let id = cur && cur.dataset.id;
    if (id && id.startsWith("g:")) id = id.slice(2);
    if (!id || !A.store.get("approvals", id)) id = route.startsWith("approvals/") ? route.split("/")[1] : route === "approvals" ? pick.approvals : null;
    const g = id && A.store.get("approvals", id); return g && g.status === "pending" ? g : null;
  }
  const gateKey = (label, fn) => ({ label, fn: (e, r) => { const g = targetGate(r); if (!g) return false; fn(g.id); } });
  ["home", "approvals"].forEach((p) => A.ui.keys(p, { a: gateKey("Approve the focused approval", (id) => approveOne(id)), e: gateKey("Edit, then approve", (id) => editApprove(id)), b: gateKey("Send back with a reason", (id) => rejectOne(id)) }));

  /* ---------- approval actions (all go through App.approve / App.sendBack) ---------- */
  /** gateCard with this module's approve/send-back so the soft lock and edit rules apply. */
  const card = (g, o) => A.ui.gateCard(g, { ...o, approveAction: "home-approve", backAction: "home-reject" });
  function approveOne(id) {
    const g = A.store.get("approvals", id); if (!g) return;
    const c = A.canApprove(g); if (!c.ok) return A.toast(c.why, "bad");
    const l = lockedByOther(g); if (l) return A.toast(`In review by ${A.user(l.by).name}. Take over first.`, "bad");
    if (risk(g) === "high" && A.route() !== `approvals/${id}`) { A.go(`approvals/${id}`); return A.toast("High-risk item: review it in full, then approve", ""); }
    withStepUp(g, () => { A.approve(id); release(id); });
  }
  const toText = (html) => String(html || "").replace(/<br\s*\/?>/gi, "\n").replace(/<\/p>\s*/gi, "\n\n").replace(/<[^>]+>/g, "").replace(/&amp;/g, "&").replace(/&lt;/g, "<").replace(/&gt;/g, ">").replace(/&quot;/g, '"').replace(/&#39;/g, "'").trim();
  const toHtml = (txt) => txt.split(/\n{2,}/).map((p) => `<p>${A.esc(p.trim()).replace(/\n/g, "<br>")}</p>`).join("");
  function editApprove(id) {
    const g = A.store.get("approvals", id); if (!g) return; const c = A.canApprove(g);
    if (!c.ok) return A.toast(c.why, "bad");
    const l = lockedByOther(g); if (l) return A.toast(`In review by ${A.user(l.by).name}. Take over first.`, "bad");
    if (!g.body) return A.toast("Nothing to edit on this item; approve or send it back", "");
    withStepUp(g, () => A.modal(`Approve with edits: ${g.title}`, h`<label class="field">Draft (your edits are kept as a diff and used to improve the skill)<textarea id="hm-edit" rows="12" class="input">${toText(g.body)}</textarea></label>`,
      [{ label: "Cancel" }, { label: "Approve with edits", primary: true, run: () => {
        const v = (document.getElementById("hm-edit") || {}).value || ""; if (!v.trim()) { A.toast("The draft cannot be empty", "bad"); return false; }
        const nb = toHtml(v); const changed = toText(nb) !== toText(g.body);
        if (changed) { A.store.put("approvals", { id, originalBody: g.body, body: nb, decision: "approved with edits" }); A.audit("gate.edited", { target: id, matterId: g.matterId, note: g.title }); }
        A.approve(id, changed ? "Approved with edits" : ""); release(id);
      } }]));
  }
  function rejectOne(id) {
    const g = A.store.get("approvals", id); if (!g || g.status !== "pending") return;
    if (!A.can(g.cap, "approve") && g.preparedBy !== A.me().id) return A.toast("You cannot act on this approval", "bad");
    A.modal("Send back with reason", h`<label class="field">What should change? ${g.preparedBy === "agent" ? "The agent revises using your reason." : "The preparer is notified."}<textarea id="hm-why" rows="3" class="input"></textarea></label>`,
      [{ label: "Cancel" }, { label: "Send back", primary: true, run: () => { const v = ((document.getElementById("hm-why") || {}).value || "").trim(); if (!v) { A.toast("A reason is required", "bad"); return false; } A.sendBack(id, v); release(id); } }]);
  }
  function reassign(id) {
    const g = A.store.get("approvals", id); if (!g || g.status !== "pending") return;
    const ok = A.can(g.cap, "approve") || g.preparedBy === A.me().id; if (!ok) return A.toast(`${A.roleName(role())} cannot reassign this`, "bad");
    const el = eligible(g); if (!el.length) return A.toast("No other eligible approver", "bad");
    A.modal("Reassign approval", h`<p class="muted small">Only people who hold ${A.capName(g.cap)} at approve level, can see the matter and did not prepare it.</p>
      <label class="field">Approver<select id="hm-to" class="input">${el.map((u) => h`<option value="${u.id}" ${u.id === g.assignee ? A.raw("selected") : ""}>${u.name} · ${A.roleName(u.role)}</option>`)}</select></label>`,
      [{ label: "Cancel" }, { label: "Reassign", primary: true, run: () => { const to = (document.getElementById("hm-to") || {}).value; if (!to) return false; A.store.put("approvals", { id, assignee: to, reassigned: { from: g.assignee || null, to, at: A.now(), by: A.me().id } }); release(id); A.audit("gate.reassigned", { target: id, matterId: g.matterId, note: `${g.title} → ${A.user(to).name}` }); A.toast(`Reassigned to ${A.user(to).name}`); } }]);
  }
  /** Stale or expired item: the agent re-drafts it; the old item is marked superseded and points to the new one. */
  function redraft(id) {
    const g = A.store.get("approvals", id); if (!g || !(g.stale || g.status === "expired")) return;
    if (!(A.can(g.cap, "act") || A.can(g.cap, "approve"))) return A.toast(`${A.roleName(role())} cannot ask for a new draft of this`, "bad");
    A.agent.run({ title: `Re-draft: ${g.title}`, matterId: g.matterId || null, steps: ["Re-reading the sources", "Re-drafting with current facts", "Re-running the checks"], done() {
      const spec = { ...g }; ["id", "status", "created", "decidedBy", "decidedAt", "note", "escalations", "reassigned", "stale", "expires", "due", "supersededBy", "originalBody", "decision"].forEach((k) => delete spec[k]);
      const n = A.gate({ ...spec, preparedBy: "agent", title: g.title.replace(/^(Re-drafted: )?/, "Re-drafted: ") });
      A.store.put("approvals", { id: g.id, status: "superseded", supersededBy: n.id, decidedBy: "agent", decidedAt: A.now() });
      A.audit("gate.superseded", { actor: "agent", target: g.id, matterId: g.matterId || null, note: `replaced by ${n.id}` });
      return { title: "New draft waiting for approval", outputs: [{ label: "Review the new draft", go: `approvals/${n.id}` }] };
    } });
  }
  /** Overdue chase: approver first, then their supervisor once a day has passed. Internal reminders only (no gate). */
  function nextEscalation(g) {
    const late = A.daysBetween(gateDue(g), A.today), done = (g.escalations || []).map((x) => x.level);
    const approver = g.assignee ? A.user(g.assignee) : eligible(g)[0];
    if (!approver) return null;
    if (!done.includes(1)) return { level: 1, to: approver.id, text: `Reminder: "${g.title}" was due ${dueText(gateDue(g))}.` };
    if (late >= 1 && !done.includes(2)) { const sup = supervisorFor(approver.id, g); if (sup) return { level: 2, to: sup.id, text: `Escalation: "${g.title}" is ${plural(late, "day")} overdue with ${approver.name}.` }; }
    return null;
  }
  function chase() {
    const due = visibleGates().filter((g) => isOverdue(g) && nextEscalation(g));
    if (!due.length) return A.toast("Every overdue item has already been chased today");
    A.agent.run({ title: "Chase overdue approvals", steps: ["Finding approvals past their due time", "Drafting reminders", "Notifying approvers, then supervisors"], done() {
      let n = 0;
      due.forEach((g0) => { const g = A.store.get("approvals", g0.id); const x = g && g.status === "pending" && nextEscalation(g); if (!x) return;
        A.store.put("approvals", { id: g.id, escalations: [...(g.escalations || []), { ...x, at: A.now() }] });
        A.audit("gate.escalated", { actor: "agent", target: g.id, matterId: g.matterId || null, note: `level ${x.level} → ${A.user(x.to).name}` }); n++; });
      return { title: `${plural(n, "reminder")} sent`, outputs: [{ label: "Overdue approvals", go: "approvals" }] };
    } });
  }
  A.action("home-approve", (el) => approveOne(el.dataset.id));
  A.action("home-edit", (el) => editApprove(el.dataset.id));
  A.action("home-reject", (el) => rejectOne(el.dataset.id));
  A.action("home-reassign", (el) => reassign(el.dataset.id));
  A.action("hm-redraft", (el) => redraft(el.dataset.id));
  A.action("hm-chase", () => chase());
  A.action("hm-leaver", () => {
    if (!(isAdmin() || role() === "owner")) return A.toast("Only the owner or the admin can run this", "bad");
    const who = [...new Set(A.store.where("approvals", (g) => g.status === "pending" && g.assignee).map((g) => g.assignee))].map(A.user).filter((u) => u.status !== "inactive");
    if (!who.length) return A.toast("No one has approvals assigned by name");
    A.modal("Approver leaves the firm", h`<p class="muted small">demo · the user account is not changed; offboarding in Settings does this for real</p>
      <p class="small">Each named approval moves to the leaver's supervisor (the matter's responsible lawyer, else the principal), if that person is eligible.</p>
      <label class="field">Leaver<select id="hm-leaver" class="input">${who.map((u) => h`<option value="${u.id}">${u.name} · ${plural(A.store.where("approvals", (g) => g.status === "pending" && g.assignee === u.id).length, "item")}</option>`)}</select></label>`,
      [{ label: "Cancel" }, { label: "Reassign their items", primary: true, run: () => { const uid = (document.getElementById("hm-leaver") || {}).value; if (!uid) return false; const n = reassignFrom(uid, "left the firm", true); A.toast(n ? `${plural(n, "item")} reassigned to supervisors` : "No eligible supervisor; items stay put", n ? "ok" : "bad"); } }]);
  });

  /* ---------- Today ---------- */
  function needsMe() {
    const rows = [];
    runsWaitingOnMe().forEach((r) => rows.push({ id: `g:${r.gateId}`, status: "waiting", title: r.title, sub: `Paused run · step ${r.step} of ${r.steps.length} needs your approval`, due: A.today, gate: A.store.get("approvals", r.gateId) }));
    forMe().filter((g) => !rows.some((x) => x.id === `g:${g.id}`)).forEach((g) => { const m = g.matterId && A.store.get("matters", g.matterId);
      rows.push({ id: `g:${g.id}`, status: "needs-approval", title: g.title, sub: `${kindName(g.kind)}${m ? ` · ${m.number}` : ""} · ${g.preparedBy === "agent" ? "✦ agent prepared" : `prepared by ${A.user(g.preparedBy).name}`}`, due: gateDue(g), gate: g }); });
    upcoming(1).filter((x) => x.mine || x.critical).forEach((x) => { const m = x.matterId && A.store.get("matters", x.matterId);
      rows.push({ id: `w:${x.go}`, status: x.due < A.today ? "overdue" : "todo", title: `${x.label}: ${x.title}`, sub: `${m ? m.number + " · " : ""}${x.mine ? "yours" : "critical"}`, due: x.due }); });
    return rows.sort((a, b) => (a.due < b.due ? -1 : a.due > b.due ? 1 : 0));
  }
  function renderHome(el) {
    const me = A.me();
    A.setContext({ kind: "home", label: "your day" });
    if (isClient()) {
      return A.render(el, h`<div class="page">${A.ui.header({ title: `Welcome, ${firstName()}`, sub: A.date(A.today), primary: { label: "Open Portal", to: "portal" } })}
        <div class="card"><h3>Your matter, documents and invoices are in the Portal</h3><p class="muted">See updates from Linda K Law, share documents and pay invoices.</p></div></div>`);
    }
    queueSweep();
    const rows = needsMe(), runs = myRuns().slice(0, 5), up = upcoming().filter((x) => !rows.some((r) => r.id === `w:${x.go}`)), mm = myMatters();
    const showMatters = !isAdmin() && !["bookkeeper", "auditor"].includes(role());
    const pend = forMe();
    const actions = A.matter("m1") && A.can("documents.release", "act") ? [{ label: "✦ Answer the extension request", a: "home-plan-osei", agent: true, title: "Preview the plan; nothing runs until you press Run" }] : [];
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Today", sub: `${A.date(A.today)} · ${me.title}`, actions, primary: { label: `Review approvals${pend.length ? ` (${pend.length})` : ""}`, to: "approvals" } })}
      <div class="card" style="margin-bottom:14px"><div class="row"><span class="av agent">✦</span><p class="grow" style="margin:0">${brief()}</p>${A.ui.btn({ label: "✦ Refresh", a: "home-brief", small: true, agent: true, title: "Re-read approvals, deadlines and tasks" })}</div></div>
      <div class="hm-split">
        <section><h3 style="margin:0 0 8px;font-size:14.5px">Needs you <span class="muted small">${rows.length}</span></h3>
          ${A.ui.list(rows.map((r) => ({ id: r.id, title: r.title, sub: r.sub, status: r.status,
            right: h`${A.ui.due(r.due)}${r.gate && r.gate.status === "pending" && A.canApprove(r.gate).ok ? (risk(r.gate) === "high" ? h`<span class="muted small">review in full</span>` : h`<span class="muted small">${A.ui.kbd("a")} approve</span>`) : ""}` })),
            "home-open", { empty: "You are clear", emptyHint: "Approvals, paused agent runs and work due today land here." })}
          ${showMatters ? h`<div class="hm-sec">Later this week</div>${A.ui.list(up.slice(0, 8).map((x) => { const m = x.matterId && A.store.get("matters", x.matterId);
              return { id: `w:${x.go}`, title: `${x.label}: ${x.title}`, sub: `${m ? m.number : ""}${x.mine ? " · yours" : ""}`, right: A.ui.due(x.due) }; }), "home-open", { empty: "Nothing else due in 7 days" })}` : ""}
          ${A.ui.hints([["j", "next"], ["k", "previous"], ["Enter", "open"], ["a", "approve"], ["b", "send back"]])}
        </section>
        <section>
          <div class="row"><h3 class="grow" style="margin:0 0 8px;font-size:14.5px">What the agent did</h3>${A.ui.btn({ label: "All activity", to: "activity", small: true })}</div>
          <div class="card">${runs.length ? runs.map((r) => A.ui.runCard(r)) : A.ui.empty("No agent work yet", "Ask the agent for a draft or a check; progress shows here.")}</div>
          ${showMatters ? h`<div class="hm-sec">Your matters · ${mm.length}</div>${A.ui.list(mm.map((m) => { const n = forMe().filter((g) => g.matterId === m.id).length;
              return { id: `m:${m.id}`, title: `${m.number} ${m.name}`, sub: `${m.area} · ${m.stage} · ${A.contact(m.clientId).name}`, right: n ? st("needs-approval", `${n} to approve`) : "" }; }), "home-open", { empty: "You are not on any open matter" })}` : ""}
        </section>
      </div></div>`);
  }
  function openHome(key) {
    const kind = key.slice(0, key.indexOf(":")), id = key.slice(key.indexOf(":") + 1);
    if (kind === "g") { pick.approvals = id; return A.go(`approvals/${id}`); }
    if (kind === "w") return A.go(id);
    if (kind === "m") return A.go(`matters/${id}`);
  }
  A.action("home-open", (el) => openHome(el.dataset.id));
  A.action("home-plan-osei", () => { if (!A.matter("m1")) return A.toast("You are not on this matter", "bad"); A.agent.preview(oseiPlan(), "Here is how I would answer Mr. Osei. Step 5 waits for a lawyer before anything leaves the firm."); });
  A.action("home-brief", () => A.agent.run({ title: "Morning brief", steps: ["Reading approvals", "Checking deadlines and tasks", "Summarizing your matters"], done: () => ({ title: "Brief updated", outputs: [{ label: "Approvals", go: "approvals" }] }) }));

  /* ---------- Approvals: filters, queue, review pane ---------- */
  let qTab = "mine", qKind = "", qRisk = "", qMatter = "", qText = "";
  const SEL = "hm-approvals";
  const myTeam = () => { const ids = new Set(myMatters().map((m) => m.id)); return (g) => g.matterId && ids.has(g.matterId); };
  function queue() {
    const all = visibleGates(); let l;
    if (qTab === "mine") l = forMe();
    else if (qTab === "team") { const t = myTeam(); l = all.filter((g) => g.status === "pending" && t(g)); }
    else if (qTab === "pending") l = all.filter((g) => g.status === "pending");
    else if (qTab === "overdue") l = all.filter(isOverdue);
    else l = all.filter((g) => g.status !== "pending");
    const t = qText.toLowerCase();
    l = l.filter((g) => (!qKind || kindName(g.kind) === qKind) && (!qRisk || risk(g) === qRisk) && (!qMatter || g.matterId === qMatter || (qMatter === "firm" && !g.matterId))
      && (!t || `${g.title} ${g.summary || ""} ${(A.store.get("matters", g.matterId) || {}).name || ""}`.toLowerCase().includes(t)));
    return qTab === "decided" ? l.sort((a, b) => ((b.decidedAt || "") > (a.decidedAt || "") ? 1 : -1)) : l.sort(order);
  }
  const bulkable = (g) => g && risk(g) === "low" && A.canApprove(g).ok && !lockedByOther(g);
  function reviewPane(g) {
    if (!g) return null;
    const m = g.matterId && A.matter(g.matterId), c = A.canApprove(g), l = g.status === "pending" && lockedByOther(g), newer = g.supersededBy && A.store.get("approvals", g.supersededBy);
    const esc = g.escalations || [], rs = g.reassigned;
    return h`<div class="hm-pane">
      <div class="row wrap" style="margin-bottom:6px">${chip(g)}${riskChip(g)}${g.status === "pending" ? A.ui.due(gateDue(g)) : ""}<span class="muted small">${kindName(g.kind)}${m ? ` · ${m.number} ${m.name}` : ""}</span><span class="grow"></span>${A.ui.btn({ label: "Open", to: `approvals/${g.id}`, small: true, title: "Open the full page" })}</div>
      ${l ? h`<div class="card" style="margin-bottom:10px;padding:10px 12px"><div class="row wrap">${st("in-review")}<span class="grow small">${A.user(l.by).name} opened this at ${A.dateTime(l.at).split(" ").pop()}. Approving is held to one reviewer for ${LOCK_MIN} minutes.</span>${c.ok ? A.ui.btn({ label: "Take over", a: "hm-takeover", id: g.id, small: true }) : ""}</div></div>` : ""}
      ${g.status === "pending" ? h`<div class="card" style="margin-bottom:10px;padding:10px 12px"><b class="small">If you approve</b><div class="small">${effectLine(g)}</div>
        <div class="muted small" style="margin-top:4px">Approver: ${approverText(g)}${needsStepUp(g) ? " · fresh sign-in required" : ""}</div></div>` : ""}
      ${rs ? h`<div class="card small" style="margin-bottom:10px;padding:8px 12px">${st("reassigned")} ${rs.auto ? `Moved automatically from ${A.user(rs.from).name} (${rs.reason}) to ${A.user(rs.to).name}` : `Reassigned to ${A.user(rs.to).name} by ${A.user(rs.by).name}`} · ${A.dateTime(rs.at)}${rs.demo ? h` <span class="muted">demo</span>` : ""}</div>` : ""}
      ${esc.length || isOverdue(g) ? h`<div class="card small" style="margin-bottom:10px;padding:8px 12px"><div class="row wrap">${isOverdue(g) ? st("overdue") : ""}<b class="grow">Escalation</b>${isOverdue(g) && nextEscalation(g) ? A.ui.btn({ label: "✦ Chase now", a: "hm-chase", small: true, agent: true }) : ""}</div>
        ${esc.length ? h`<ol style="margin:6px 0 0 18px;padding:0">${esc.map((x) => h`<li>${x.level === 1 ? "Approver" : "Supervisor"} ${A.user(x.to).name} reminded · ${A.dateTime(x.at)}<div class="muted">${x.text}</div></li>`)}</ol>` : h`<div class="muted">Not chased yet. The agent reminds the approver, then their supervisor a day later.</div>`}</div>` : ""}
      ${g.status === "superseded" ? h`<div class="card small" style="margin-bottom:10px;padding:8px 12px">${st("superseded")} A newer draft replaced this one. ${newer && visibleGate(newer) ? A.ui.btn({ label: "Open the newer draft", to: `approvals/${newer.id}`, small: true }) : ""}</div>` : ""}
      ${g.status === "expired" || (g.status === "pending" && g.stale) ? h`<div class="card small" style="margin-bottom:10px;padding:8px 12px"><div class="row wrap">${st(g.status === "expired" ? "expired" : "stale")}<span class="grow">${g.status === "expired" ? "The decision window closed before anyone acted. Nothing was sent." : "A source changed after this was drafted. It cannot be approved as is."}</span>${A.ui.btn({ label: "✦ Re-draft", a: "hm-redraft", id: g.id, small: true, agent: true, disabled: !(A.can(g.cap, "act") || A.can(g.cap, "approve")), why: "You cannot ask for a new draft of this" })}</div></div>` : ""}
      ${card(g)}
      ${g.originalBody ? h`<details class="card small" style="margin-bottom:10px"><summary>Edited before approval: show the agent's original</summary><div class="draft">${A.raw(g.originalBody)}</div></details>` : ""}
      ${g.status === "pending" ? h`<div class="row wrap">${c.ok && g.body && !l ? A.ui.btn({ label: "Approve with edits", a: "home-edit", id: g.id, small: true, title: "Approve with edits (e)" }) : ""}
        ${A.can(g.cap, "approve") || g.preparedBy === A.me().id ? A.ui.btn({ label: "Reassign", a: "home-reassign", id: g.id, small: true, title: "Reassign to another eligible approver" }) : ""}
        ${g.matterId && m ? A.ui.btn({ label: "Matter", to: `matters/${g.matterId}`, small: true }) : ""}</div>` : ""}</div>`;
  }
  function renderApprovals(el, params) {
    A.setContext({ kind: "approvals", label: "the approvals queue" });
    if (isClient()) return A.render(el, h`<div class="page"><div class="locked"><b>Not available in the client view</b>Your documents and requests are under Portal.</div></div>`);
    queueSweep();
    if (params && params.id) return renderApproval(el, params.id);
    const all = visibleGates(), list = queue();
    if (!list.some((g) => g.id === pick.approvals)) pick.approvals = list[0] ? list[0].id : null;
    const pendAll = all.filter((g) => g.status === "pending"), t = myTeam(), late = pendAll.filter(isOverdue);
    const kinds = [...new Set(all.map((g) => kindName(g.kind)))].sort();
    const mats = [...new Set(all.map((g) => g.matterId || "firm"))].map((id) => [id, id === "firm" ? "Firm (no matter)" : ((m) => (m ? `${m.number} ${m.name}` : "Matter"))(A.store.get("matters", id))]);
    const sel = pick.approvals && A.store.get("approvals", pick.approvals);
    const actions = [];
    if (late.some(nextEscalation)) actions.push({ label: `✦ Chase ${late.length} overdue`, a: "hm-chase", agent: true, small: true, title: "Remind approvers, then supervisors" });
    if (isAdmin() || role() === "owner") actions.push({ label: "Approver leaves…", a: "hm-leaver", small: true, title: "Demo: re-route a leaver's named approvals to their supervisor" });
    A.render(el, h`<div class="page" style="max-width:1400px">
      ${A.ui.header({ title: "Approvals", sub: "The agent prepares; a person who holds the capability and did not prepare the item decides. The agent never approves.", actions })}
      ${A.ui.filters({ search: { c: "hm-q", value: qText, placeholder: "Search title or matter" },
        chips: [{ id: "mine", label: "For me", count: forMe().length }, { id: "team", label: "My matters", count: pendAll.filter(t).length }, { id: "pending", label: "All pending", count: pendAll.length }, { id: "overdue", label: "Overdue", count: late.length }, { id: "decided", label: "Decided", count: all.length - pendAll.length }],
        active: qTab, action: "approvals-filter",
        selects: [{ c: "hm-kind", label: "Gate type", value: qKind, options: [["", "All types"], ...kinds.map((k) => [k, k])] },
          { c: "hm-risk", label: "Risk", value: qRisk, options: [["", "Any risk"], ["high", "High risk"], ["medium", "Medium risk"], ["low", "Low risk"]] },
          { c: "hm-matter", label: "Matter", value: qMatter, options: [["", "All matters"], ...mats] }],
        right: qKind || qRisk || qMatter || qText ? A.ui.btn({ label: "Clear", a: "hm-clear", small: true }) : "" })}
      ${A.ui.split(A.ui.list(list.map((g) => { const m = g.matterId && A.store.get("matters", g.matterId);
          return { id: g.id, on: g.id === pick.approvals, title: g.title, sub: `${kindName(g.kind)} · ${m ? m.number : "Firm"} · ${approverText(g)}`,
            status: null, right: g.status === "pending" ? h`${A.ui.due(gateDue(g))}${riskChip(g)}${lockedByOther(g) ? st("in-review") : ""}` : chip(g) }; }), "hm-pick",
          { select: qTab === "decided" ? null : SEL, empty: qTab === "mine" ? "Nothing needs your approval" : "No approvals match", emptyHint: qTab === "mine" ? "All clear. Items you can approve land here, ordered by due time and risk." : "Clear the filters to see more." }),
        reviewPane(sel), { emptyTitle: "Select an item" })}
      ${A.ui.bulkBar(SEL, [{ label: "Approve low-risk", a: "hm-bulk-approve", primary: true }], { note: "Money, deadlines, conflicts and access are never bulk approved; they are skipped" })}
      ${A.ui.hints([["j", "next"], ["k", "previous"], ["Enter", "review"], ["a", "approve"], ["e", "edit"], ["b", "send back"], ["x", "select"], ["/", "search"]])}
    </div>`);
  }
  function renderApproval(el, id) {
    const g = A.store.get("approvals", id);
    const back = { label: "Approvals", to: "approvals" };
    if (!g || !visibleGate(g)) return A.render(el, h`<div class="page">${A.ui.header({ title: "Approval not found", back })}${A.ui.empty("Approval not found", "It may be on a matter you cannot see.")}</div>`);
    const m = g.matterId && A.matter(g.matterId), c = A.canApprove(g), el2 = eligible(g), l = lockOf(g);
    pick.approvals = g.id; queueClaim(g);
    A.setContext({ kind: "approvals", id: g.id, label: g.title });
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: g.title, status: chip(g), back, sub: `${kindName(g.kind)}${m ? ` · ${m.number} ${m.name}` : ""}` })}
      <div class="hm-split"><div>${reviewPane(g)}</div>
        <div class="card"><h3>Who can approve</h3><dl class="kv"><dt>Capability</dt><dd>${A.capName(g.cap)}</dd><dt>Approver</dt><dd>${approverText(g)}</dd>
          <dt>Eligible</dt><dd>${el2.length ? el2.map((u) => u.name).join(", ") : "No one; ask the admin to grant the capability"}</dd>
          <dt>Prepared by</dt><dd>${g.preparedBy === "agent" ? A.ui.actor("agent", "The agent, for the person who asked (cannot approve its own work)") : A.user(g.preparedBy).name}</dd>
          <dt>Risk · due</dt><dd>${riskChip(g)} ${A.date(gateDue(g))}${g.expires ? ` · expires ${A.date(g.expires)}` : ""}</dd>
          <dt>Sign-in</dt><dd>${needsStepUp(g) ? "Fresh sign-in (step-up) before approving" : "Normal session"}</dd>
          <dt>Review lock</dt><dd>${l ? `${A.user(l.by).name} since ${A.dateTime(l.at)}${l.takenFrom ? ` (taken over from ${A.user(l.takenFrom).name})` : ""}` : "Nobody has it open"}</dd>
          <dt>You</dt><dd>${g.status !== "pending" ? `Decided: ${(A.STATUS[g.status] || [g.status])[0]}` : c.ok ? A.ui.actor("person", "You can approve, edit, send back or reassign") : `Cannot approve: ${c.why}`}</dd></dl></div></div>
    </div>`);
  }
  A.action("approvals-filter", (el) => { qTab = el.dataset.id; pick.approvals = null; A.selection(SEL).clear(); A.refresh(); });
  A.action("hm-pick", (el) => { pick.approvals = el.dataset.id; const g = A.store.get("approvals", el.dataset.id); if (g && !lockOf(g) && g.status === "pending" && A.canApprove(g).ok) claim(g); else A.refresh(); });
  A.action("hm-clear", () => { qKind = qRisk = qMatter = qText = ""; A.refresh(); });
  A.action("hm-bulk-approve", (el) => {
    const ids = String(el.dataset.ids || "").split(",").filter(Boolean), ok = ids.filter((id) => bulkable(A.store.get("approvals", id)));
    A.selection(SEL).clear();
    if (!ok.length) { A.refresh(); return A.toast("Nothing eligible: bulk approve is for low-risk items you can approve", "bad"); }
    ok.forEach((id) => { A.approve(id, "Bulk approved (low risk)"); release(id); });
    A.audit("gate.bulk_approved", { note: plural(ok.length, "item") });
    A.toast(`Approved ${plural(ok.length, "item")}${ids.length > ok.length ? `; skipped ${ids.length - ok.length} not eligible for bulk` : ""}`, "ok");
  });
  A.change("hm-q", (el) => { qText = el.value; A.refresh(); });
  A.change("hm-kind", (el) => { qKind = el.value; A.refresh(); });
  A.change("hm-risk", (el) => { qRisk = el.value; A.refresh(); });
  A.change("hm-matter", (el) => { qMatter = el.value; A.refresh(); });

  /* ---------- Agent & audit ---------- */
  let aTab = "events", fMatter = "", fKind = "", fAction = "", fUser = "", fDate = "7";
  const runOf = (a) => A.store.get("runs", a.target);
  const gateOf = (a) => A.store.get("approvals", a.target);
  const evMatter = (a) => a.matterId || (gateOf(a) || {}).matterId || (runOf(a) || {}).matterId || null;
  /** Tamper evidence: each event's hash covers the previous hash (FNV-1a, display only). */
  function chain() {
    const list = A.store.all("audit").slice().reverse(); let prev = "00000000"; const out = {};
    list.forEach((a) => { let x = 0x811c9dc5; const s = prev + a.at + a.actor + a.action + (a.target || "") + (a.note || ""); for (let i = 0; i < s.length; i++) { x ^= s.charCodeAt(i); x = Math.imul(x, 16777619) >>> 0; } const hh = x.toString(16).padStart(8, "0"); out[a.id] = { hash: hh, prev }; prev = hh; });
    return out;
  }
  const outcome = (a) => (/blocked|denied|failed/.test(a.action) ? "blocked" : /stopped|returned/.test(a.action) ? "stopped" : "ok");
  function events() {
    const me = A.me().id, full = fullLog();
    return A.store.all("audit").map((a) => {
      const mid = evMatter(a), m = mid && A.store.get("matters", mid), seen = !m || A.canMatter(m);
      if (!full && a.actor !== me && !(m && seen && (m.team || []).includes(me))) return null;
      if (!seen && !(isAdmin() || A.grant("admin.audit_log") === "all")) return null;
      return { ...a, matterId: mid, walled: !seen };
    }).filter(Boolean);
  }
  const paused = (id) => { const p = A.store.get("agentPauses", id); return p && p.active ? p : null; };
  const canExport = () => A.can("admin.export", "act") || A.can("admin.audit_log", "act");
  function renderActivity(el) {
    A.setContext({ kind: "activity", label: "agent runs and audit" });
    if (isClient()) return A.render(el, h`<div class="page"><div class="locked"><b>Not available in the client view</b>Activity on your matter is shown under Portal.</div></div>`);
    const full = fullLog(), runs = myRuns(), base = events();
    const since = fDate ? A.addDays(A.today, -(+fDate - 1)) : "";
    const rows = base.filter((a) => (!fMatter || a.matterId === fMatter) && (!fKind || (fKind === "agent") === (a.actor === "agent")) && (!fAction || a.action === fAction) && (!fUser || a.actor === fUser) && (!since || (a.at || "") >= since));
    const actions = [...new Set(base.map((a) => a.action))].sort(), users = [...new Set(base.map((a) => a.actor))];
    const mats = [...new Set(base.map((a) => a.matterId).filter(Boolean))];
    const cost = runs.reduce((s, r) => s + (runCents(r) || 0), 0), withCost = runs.filter((r) => runCents(r) != null).length;
    const firm = paused("firm"), ch = chain(), exp = canExport(), canPause = isAdmin() || role() === "owner";
    const shown = rows.slice(0, 200);
    if (aTab === "events" && !shown.some((a) => a.id === pick.activity)) pick.activity = shown[0] ? shown[0].id : null;
    if (aTab === "runs" && !runs.some((r) => r.id === pick.runs)) pick.runs = runs[0] ? runs[0].id : null;
    const ev = aTab === "events" && pick.activity && rows.find((a) => a.id === pick.activity), run = aTab === "runs" && pick.runs && A.store.get("runs", pick.runs);
    const noExport = `${A.roleName(role())} cannot export the audit log`;
    A.render(el, h`<div class="page" style="max-width:1400px">
      ${A.ui.header({ title: "Agent & audit", status: firm ? st("blocked", `Agent paused firm-wide by ${A.user(firm.by).name}`) : null,
        sub: full ? "Every agent action and human change in the firm. Append-only; the agent writes events and cannot edit them." : "Your own actions and events on your matters. The firm-wide log needs the audit log permission.",
        actions: [{ label: firm ? "Resume agent firm-wide" : "Pause agent firm-wide", a: "hm-pause-firm", small: true, danger: !firm, disabled: !canPause, why: "Only an admin or the owner can pause the agent firm-wide" },
          { label: "Export CSV", a: "activity-export", small: true, data: { fmt: "csv" }, disabled: !exp, why: noExport },
          { label: "Export JSON", a: "activity-export", small: true, data: { fmt: "json" }, disabled: !exp, why: noExport }] })}
      <div class="stats">${A.ui.stat("Agent runs", runs.length, `${runs.filter((r) => r.status === "running").length} running · ${runs.filter((r) => r.status === "waiting").length} waiting`)}
        ${A.ui.stat("Events shown", rows.length, full ? "firm-wide" : "yours and your matters")}
        ${A.ui.stat("Model cost", A.cents(cost), `CAD · ${plural(withCost, "run")} with cost recorded`)}
        ${A.ui.stat("Blocked by policy", rows.filter((a) => outcome(a) === "blocked").length, "", rows.some((a) => outcome(a) === "blocked") ? "bad" : "")}</div>
      ${A.ui.filters({ chips: [{ id: "events", label: "Event log", count: rows.length }, { id: "runs", label: "Agent runs", count: runs.length }], active: aTab, action: "hm-atab",
        selects: aTab === "runs" ? [] : [
          { c: "hm-fdate", label: "Date", value: fDate, options: [["1", "Today"], ["7", "Last 7 days"], ["30", "Last 30 days"], ["", "All time"]] },
          { c: "hm-fkind", label: "Actor kind", value: fKind, options: [["", "Agent and people"], ["agent", "Agent only"], ["human", "People only"]] },
          { c: "hm-fuser", label: "Actor", value: fUser, options: [["", "Anyone"], ...users.map((u) => [u, A.user(u).name])] },
          { c: "hm-fmatter", label: "Matter", value: fMatter, options: [["", "All matters"], ...mats.map((id) => { const m = A.store.get("matters", id); return [id, m && A.canMatter(m) ? `${m.number} ${m.name}` : "Restricted matter"]; })] },
          { c: "hm-faction", label: "Action", value: fAction, options: [["", "All actions"], ...actions.map((x) => [x, x])] }],
        right: aTab === "events" && (fMatter || fKind || fAction || fUser || fDate !== "7") ? A.ui.btn({ label: "Clear", a: "hm-aclear", small: true }) : "" })}
      ${aTab === "runs"
        ? A.ui.split(A.ui.list(runs.map((r) => { const m = r.matterId && A.store.get("matters", r.matterId);
            return { id: r.id, on: r.id === pick.runs, title: r.title, sub: `${A.user(r.by).name}${m ? ` · ${m.number}` : ""}${r.skill ? ` · ${r.skill}` : ""}`, status: r.status, right: A.dateTime(r.started || "") }; }), "hm-run", { empty: "No runs yet" }),
          run ? h`<div class="card">${A.ui.runCard(run, { full: true })}<div class="row">${A.ui.btn({ label: "Details", to: `activity/run/${run.id}`, small: true })}</div></div>` : null, { emptyTitle: "Select a run" })
        : A.ui.split(A.ui.list(shown.map((a) => { const m = a.matterId && A.store.get("matters", a.matterId), o = outcome(a);
            return { id: a.id, on: a.id === pick.activity, title: a.action, sub: `${a.actor === "agent" ? "✦ agent" : A.user(a.actor).initials} · ${a.walled ? "Restricted matter (walled): details hidden" : `${m ? m.number + " · " : ""}${a.note || a.target || ""}`}`,
              status: o === "ok" ? null : o, right: A.dateTime(a.at) }; }), "hm-ev", { empty: "No events match", emptyHint: "Widen the date or clear filters." }),
          ev ? eventPane(ev, ch[ev.id]) : null, { emptyTitle: "Select an event" })}
    </div>`);
  }
  function eventPane(a, hc) {
    if (a.walled) return h`<div class="card"><h3>Restricted event</h3><p class="muted small">This event is on a matter you are walled from. Only its existence is shown.</p><dl class="kv"><dt>At</dt><dd>${A.dateTime(a.at)}</dd><dt>Event</dt><dd class="hm-mono">${a.id}</dd><dt>Hash</dt><dd class="hm-mono">${hc ? hc.hash : ""}</dd></dl></div>`;
    const m = a.matterId && A.matter(a.matterId), g = gateOf(a), r = runOf(a), mp = a.matterId && paused(a.matterId), o = outcome(a);
    return h`<div class="hm-pane"><div class="card"><div class="row"><h3 class="grow">${a.action}</h3>${o === "ok" ? st("done", "OK") : st(o)}${a.actor === "agent" ? A.ui.actor("agent", "Agent") : ""}</div>
      <dl class="kv"><dt>At</dt><dd>${A.dateTime(a.at)} (server time, America/Toronto)</dd><dt>Actor</dt><dd>${A.ui.avatar(a.actor)} ${a.actor === "agent" ? "Agent" : A.user(a.actor).name}</dd>
        ${r ? h`<dt>On behalf of</dt><dd>${A.user(r.by).name}</dd>${r.skill ? h`<dt>Skill</dt><dd>${r.skill}</dd>` : ""}` : ""}
        <dt>Matter</dt><dd>${m ? A.ui.btn({ label: `${m.number} ${m.name}`, to: `matters/${m.id}`, small: true }) : "Firm"}</dd>
        <dt>Target</dt><dd class="hm-mono">${a.target || "—"}</dd>${a.note ? h`<dt>Note</dt><dd>${a.note}</dd>` : ""}
        <dt>Event</dt><dd class="hm-mono">${a.id}</dd><dt>Hash</dt><dd class="hm-mono" title="Covers the previous event's hash">${hc ? `${hc.hash} ← ${hc.prev}` : ""}</dd></dl>
      <p class="muted small" style="margin:8px 0 0">The log stores references only, never document or conversation text.</p></div>
      ${g ? h`<div class="hm-sec">Gate decision</div>${card(g, { compact: true })}` : ""}
      ${r ? h`<div class="hm-sec">Agent run</div><div class="card">${A.ui.runCard(r)}</div>` : ""}
      ${m && isLawyer() ? h`<div class="row wrap" style="margin-top:10px">${mp ? st("blocked", `Agent paused on ${m.number}`) : ""}${A.ui.btn({ label: mp ? "Resume agent on this matter" : "Pause agent on this matter", a: "hm-pause-matter", id: m.id, small: true, danger: !mp })}</div>` : ""}</div>`;
  }
  function renderRun(el, p) {
    const r = A.store.get("runs", p.id);
    const back = { label: "Agent & audit", to: "activity" };
    if (!r || !visibleRun(r)) return A.render(el, h`<div class="page">${A.ui.header({ title: "Run not found", back })}${A.ui.empty("Run not found", "It may be on a matter you cannot see.")}</div>`);
    A.setContext({ kind: "activity", id: r.id, label: r.title });
    const m = r.matterId && A.matter(r.matterId), g = r.gateId && A.store.get("approvals", r.gateId);
    const steps = r.plan || r.steps.map((label) => ({ label }));
    const reported = A.store.where("evalCases", (x) => x.runId === r.id), cents = runCents(r);
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: r.title, status: r.status, back, sub: `Started ${A.dateTime(r.started)} for ${A.user(r.by).name}${m ? ` · ${m.number} ${m.name}` : ""}${r.skill ? ` · ${r.skill}` : ""}${cents != null ? ` · model cost ${A.cents(cents)}` : ""}`,
        actions: [{ label: "Report a bad output", a: "hm-report", id: r.id, small: true, title: "Becomes an eval case for this skill" }] })}
      <div class="grid g2">
        <section class="card"><h3>Plan</h3>${A.ui.runCard(r, { full: true })}
          <p class="muted small">${plural(steps.length, "step")}${steps.some((x) => x.gate) ? " · steps marked approval pause the run until an eligible person approves" : ""}.</p></section>
        <section class="card"><h3>Result</h3>
          ${r.status === "done" ? h`<p>${r.result || "Done"}</p>${(r.outputs || []).length ? h`<div class="row wrap">${r.outputs.map((x) => A.ui.btn({ label: x.label, to: x.go, small: true }))}</div>` : ""}
              ${(r.cites || []).length ? h`<div class="cites"><span class="muted">Sources</span> ${r.cites.map(A.ui.cite)}</div>` : h`<p class="muted small">No sources recorded for this run.</p>`}`
            : r.status === "stopped" ? h`<p>${r.result}${r.stoppedBy ? ` · by ${A.user(r.stoppedBy).name}` : ""}. Nothing after the stopped step ran.</p>`
            : g ? card(g) : h`<p class="muted">Working: step ${Math.max(1, r.step)} of ${steps.length}.</p>`}
          ${reported.length ? h`<div class="hm-sec">Reported</div>${reported.map((x) => h`<p class="small">${A.user(x.by).name} · ${A.dateTime(x.at)}: ${x.reason}</p>`)}` : ""}</section>
      </div></div>`);
  }
  A.action("hm-atab", (el) => { aTab = el.dataset.id; A.refresh(); });
  A.action("hm-ev", (el) => { pick.activity = el.dataset.id; A.refresh(); });
  A.action("hm-run", (el) => { pick.runs = el.dataset.id; A.refresh(); });
  A.action("hm-aclear", () => { fMatter = fKind = fAction = fUser = ""; fDate = "7"; A.refresh(); });
  A.change("hm-fdate", (el) => { fDate = el.value; A.refresh(); });
  A.change("hm-fkind", (el) => { fKind = el.value; A.refresh(); });
  A.change("hm-fuser", (el) => { fUser = el.value; A.refresh(); });
  A.change("hm-fmatter", (el) => { fMatter = el.value; A.refresh(); });
  A.change("hm-faction", (el) => { fAction = el.value; A.refresh(); });
  function togglePause(id, label) {
    const p = paused(id);
    A.store.put("agentPauses", { id, active: !p, by: A.me().id, at: A.now() });
    A.audit(p ? "agent.resumed" : "agent.paused", { target: id, matterId: id === "firm" ? null : id, note: label });
    A.toast(p ? `Agent resumed: ${label}` : `Agent paused: ${label}. New runs are held until resumed.`, p ? "ok" : "bad");
  }
  A.action("hm-pause-firm", (el) => { if (el.disabled || !(isAdmin() || role() === "owner")) return; togglePause("firm", "firm-wide"); });
  A.action("hm-pause-matter", (el) => { const m = A.matter(el.dataset.id); if (!m || !isLawyer()) return A.toast("Only a lawyer on the matter can pause the agent", "bad"); togglePause(m.id, m.number); });
  A.action("hm-report", (el) => {
    const r = A.store.get("runs", el.dataset.id); if (!r) return;
    A.modal("Report a bad output", h`<label class="field">What was wrong? It becomes an eval case for ${r.skill || "this skill"}.<textarea id="hm-bad" rows="3" class="input"></textarea></label>`,
      [{ label: "Cancel" }, { label: "Report", primary: true, run: () => { const v = ((document.getElementById("hm-bad") || {}).value || "").trim(); if (!v) { A.toast("Say what was wrong", "bad"); return false; }
        A.store.put("evalCases", { id: A.id("ev"), runId: r.id, skill: r.skill || "", reason: v, by: A.me().id, at: A.now() }); A.audit("agent.output_reported", { target: r.id, matterId: r.matterId, note: r.title }); A.toast("Reported; added as an eval case", "ok"); } }]);
  });
  /** Export the visible log (walled events left out) with the hash chain, as CSV or JSON. The export itself is logged. */
  A.action("activity-export", (el) => {
    if (el.disabled || !canExport()) return;
    const fmt = el.dataset.fmt === "json" ? "json" : "csv", ch = chain(), rows = events().filter((a) => !a.walled);
    const recs = rows.map((a) => ({ event_id: a.id, at: a.at, tz: A.tz || "America/Toronto", actor: a.actor, actor_kind: a.actor === "agent" ? "agent" : "human", action: a.action, matter: a.matterId || "", target: a.target || "", note: a.note || "", outcome: outcome(a), hash: (ch[a.id] || {}).hash, prev_hash: (ch[a.id] || {}).prev }));
    const body = fmt === "json" ? JSON.stringify({ firm: "Linda K Law", exported_at: A.now(), exported_by: A.me().id, chain: "fnv1a-32 over prev_hash+at+actor+action+target+note", events: recs }, null, 2)
      : [Object.keys(recs[0] || { event_id: 1 })].concat(recs.map((r) => Object.values(r))).map((r) => r.map((v) => `"${String(v ?? "").replace(/"/g, '""')}"`).join(",")).join("\n");
    try { if (window.URL && URL.createObjectURL && typeof Blob !== "undefined") { const a = document.createElement("a"); a.href = URL.createObjectURL(new Blob([body], { type: fmt === "json" ? "application/json" : "text/csv" })); a.download = `audit-${A.today}.${fmt}`; document.body.appendChild(a); a.click(); a.remove(); } } catch (e) { /* download unavailable; the export is still logged */ }
    A.audit("audit.exported", { note: `${rows.length} events as ${fmt.toUpperCase()} with hash chain` });
    A.toast(`Exported ${rows.length} events (${fmt.toUpperCase()} with hash chain). The export is logged.`, "ok");
  });

  /* ---------- modules ---------- */
  A.module({ id: "home", label: "Today", icon: "◎", order: 1, smoke: ["home"], routes: { home: renderHome } });
  A.module({ id: "approvals", label: "Approvals", icon: "◆", order: 2, visible: () => A.me().role !== "client", badge: () => (isClient() ? 0 : forMe().length),
    smoke: ["approvals", "approvals/ap-home-osei", "approvals/ap-home-osei-v1", "approvals/ap-home-haile", "approvals/ap-home-fortier", "approvals/ap-home-file-m1", "approvals/ap-home-time-m4", "approvals/ap-home-expired", "approvals/missing"],
    routes: { approvals: renderApprovals, "approvals/:id": renderApprovals } });
  /* No module cap: everyone sees their own runs and events; the firm-wide log needs admin.audit_log. */
  A.module({ id: "activity", label: "Agent & audit", icon: "≡", order: 90, visible: () => A.me().role !== "client",
    smoke: ["activity", "activity/run/run-home-haile", "activity/run/run-home-fortier", "activity/run/run-home-mail", "activity/run/missing"], routes: { activity: renderActivity, "activity/run/:id": renderRun } });

  /* ---------- agent intents and suggestions ---------- */
  A.intent({ match: /(needs? my approval|waiting (for|on) me|pending approvals?|what.*to approve)/i, label: "What needs my approval", example: "What needs my approval?",
    run() {
      if (isClient()) return { text: "Approvals are handled by the firm. Your documents and requests are under Portal.", actions: [{ label: "Open Portal", go: "portal" }] };
      const p = forMe();
      if (!p.length) return { text: "Nothing is waiting for your approval.", actions: [{ label: "All approvals", go: "approvals" }] };
      return { text: `${plural(p.length, "item")} need${p.length === 1 ? "s" : ""} you, most urgent first: ${p.slice(0, 3).map((g) => `${g.title} (${risk(g)}, due ${dueText(gateDue(g))})`).join("; ")}${p.length > 3 ? "; and more" : ""}.`,
        cites: [].concat(...p.slice(0, 2).map((g) => (g.cites || []).slice(0, 1))), actions: [{ label: "Open Approvals", go: "approvals" }].concat(p.slice(0, 2).map((g) => ({ label: `Review: ${g.title.slice(0, 40)}`, go: `approvals/${g.id}` }))) };
    } });
  A.intent({ match: /(chase|escalate) (the )?overdue/i, label: "Chase overdue approvals", example: "Chase overdue approvals",
    run() {
      if (isClient()) return { text: "Approvals are handled by the firm." };
      const late = visibleGates().filter(isOverdue);
      return { text: late.length ? `${plural(late.length, "approval")} ${late.length === 1 ? "is" : "are"} overdue. I remind the approver first, then their supervisor a day later.` : "Nothing is overdue.", actions: late.length ? [{ label: "Chase now", run: chase }, { label: "Overdue list", go: "approvals" }] : [] };
    } });
  A.intent({ match: /(brief me|what'?s on today|what is on today|my day|morning brief)/i, label: "Brief me", example: "Brief me",
    run() {
      if (isClient()) return { text: "Your matter updates are under Portal.", actions: [{ label: "Open Portal", go: "portal" }] };
      return { text: brief(), actions: [{ label: "Open Today", go: "home" }, { label: "Approvals", go: "approvals" }] };
    } });
  A.intent({ match: /(answer|reply to|handle) (the )?(extension|osei)/i, label: "Answer the extension request", example: "Answer the extension request on Tesfaye",
    run() {
      if (isClient() || !A.matter("m1")) return { text: "I cannot see that matter for you." };
      return { text: "This takes several steps and sends a letter outside the firm, so here is the plan first.", preview: oseiPlan() };
    } });
  A.intent({ match: /what did the agent do/i, label: "What did the agent do today", example: "What did the agent do today?",
    run() {
      if (isClient()) return { text: "Activity on your matter is under Portal." };
      const ev = events().filter((a) => a.actor === "agent" && (a.at || "").startsWith(A.today) && !a.walled);
      return { text: ev.length ? `${plural(ev.length, "agent event")} today: ${ev.slice(0, 4).map((a) => a.action + (a.note ? ` (${a.note})` : "")).join("; ")}.` : "No agent events today that you can see.", actions: [{ label: "Open Agent & audit", go: "activity" }] };
    } });
  A.suggest("home", () => {
    if (isClient()) return ["What is the status of my matter?"];
    if (isAdmin()) return ["Brief me", "Who changed access this week?"];
    if (role() === "bookkeeper") return ["Brief me", "What needs my approval?", "Trust balance"];
    return ["Brief me", "What needs my approval?", "Answer the extension request on Tesfaye"];
  });
  A.suggest("approvals", (c) => (c.id ? ["Why can't I approve this?", "Summarize the sources"] : ["What needs my approval?", "Chase overdue approvals"]));
  A.suggest("activity", () => ["What did the agent do today?", "What needs my approval?"]);
})();
