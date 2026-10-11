/* Trust accounting (Ontario By-Law 9) and firm reports.
   Trust: mixed trust account, client ledgers, withdrawals (request -> signatory authorizes -> executed at bank),
   bank matching and the monthly three-way reconciliation, which locks the month once signed off.
   Reports: a catalogue of live reports computed from whatever collections the other modules have loaded. */
(function () {
  "use strict";
  const A = window.App, h = A.h;
  const ACCOUNT = { name: "Mixed trust account", bank: "Chartered bank, Toronto main branch", number: "••••4471", opened: { date: "2026-06-30", balance: 0 } };
  const GENERAL = "Linda K Law general account";
  const CASH_LIMIT = 7500; // By-Law 9: no cash receipt of $7,500 or more in respect of one client matter

  /* ---------- seed ---------- */
  A.seed("trustTx", [
    { id: "tt-1", date: "2026-07-30", matterId: "m6", kind: "receipt", amount: 2000, method: "cheque", party: "Harbourview Bakery Inc.", ref: "Chq 3381", memo: "Retainer", status: "posted", cleared: true, by: "u-yohannes" },
    { id: "tt-2", date: "2026-08-24", matterId: "m4", kind: "receipt", amount: 48000, method: "wire", party: "Grace Mendel, estate trustee", ref: "Wire FT8824", memo: "Estate funds: deceased's chequing and savings accounts closed", status: "posted", cleared: true, by: "u-yohannes" },
    { id: "tt-3", date: "2026-09-04", matterId: "m1", kind: "receipt", amount: 5000, method: "e-transfer", party: "Hanna Tesfaye", ref: "ET 55120", memo: "Retainer", status: "posted", cleared: true, by: "u-yohannes" },
    { id: "tt-4", date: "2026-09-12", matterId: "m5", kind: "receipt", amount: 3000, method: "cheque", party: "Tsion Haile", ref: "Chq 118", memo: "Retainer", status: "posted", cleared: true, by: "u-yohannes" },
    { id: "tt-5", date: "2026-09-26", matterId: "m6", kind: "transfer", amount: 1695, method: "EFT", party: GENERAL, ref: "INV-2026-0418", memo: "Fees: lease review, flat fee $1,500 + HST, invoice delivered 2026-09-24", status: "posted", cleared: true, by: "u-yohannes", requestedBy: "u-yohannes", authorizedBy: "u-yoni", bankRef: "EFT 260926-07" },
    { id: "tt-6", date: "2026-09-30", matterId: "m4", kind: "disbursement", amount: 1130, method: "cheque", party: "Willowdale Appraisal Services", ref: "Chq 1042", memo: "Appraisal of 41 Willow Cres ($1,000 + HST) for the estate inventory", status: "posted", cleared: true, by: "u-yohannes", requestedBy: "u-meraf", authorizedBy: "u-linda", bankRef: "Chq 1042" },
    { id: "tt-7", date: "2026-10-02", matterId: "m2", kind: "receipt", amount: 230000, method: "wire", party: "Samuel and Ruth Okafor", ref: "Wire FT1002", memo: "Closing funds for 14 Birchmount Rd (held until closing 2026-10-30)", status: "posted", cleared: true, by: "u-yohannes" },
    { id: "tt-8", date: "2026-10-07", matterId: "m1", kind: "receipt", amount: 2500, method: "cheque", party: "Hanna Tesfaye", ref: "Chq 204", memo: "Further retainer", status: "posted", cleared: false, by: "u-yohannes" },
    { id: "tt-9", date: "2026-10-08", matterId: "m4", kind: "disbursement", amount: 6420, method: "cheque", party: "Parkview Funeral Home Ltd.", ref: "Chq 1043", memo: "Funeral account of the deceased, paid on the estate trustee's direction", status: "pending", cleared: false, by: "u-yohannes", requestedBy: "u-meraf", authorizedBy: "u-linda" },
  ]);
  A.seed("trustStatements", [
    { id: "st-2026-07", month: "2026-07", file: "Trust statement 2026-07.pdf", pages: 1, closing: 2000, by: "u-yohannes", at: "2026-08-03 09:12" },
    { id: "st-2026-08", month: "2026-08", file: "Trust statement 2026-08.pdf", pages: 2, closing: 50000, by: "u-yohannes", at: "2026-09-02 08:55" },
    { id: "st-2026-09", month: "2026-09", file: "Trust statement 2026-09.pdf", pages: 2, closing: 56292.5, by: "u-yohannes", at: "2026-10-02 09:30" },
  ]);
  /* Minimum trust balance the firm asks each client to keep (fictional policy), and funds held for a purpose that cannot pay fees. */
  A.seed("trustSettings", [
    { id: "m1", minimum: 2000, protected: 0, alert: 2500 }, { id: "m4", minimum: 5000, protected: 0, alert: 7500 }, { id: "m5", minimum: 2000, protected: 0, alert: 2000 },
    { id: "m6", minimum: 0, protected: 0, alert: 0 }, { id: "m7", minimum: 10000, protected: 0, alert: 10000 },
    { id: "m2", minimum: 0, protected: 230000, purpose: "Closing funds for 14 Birchmount Rd; pay only on closing per the agreement of purchase and sale", alert: 0 },
  ]);
  const setting = (id) => A.store.get("trustSettings", id) || { id, minimum: 0, protected: 0, alert: 0 };
  A.seed("bankLines", [
    { id: "bl-1", date: "2026-07-31", amount: 2000, text: "DEPOSIT CHQ 3381" },
    { id: "bl-2", date: "2026-08-25", amount: 48000, text: "WIRE IN FT8824" },
    { id: "bl-3", date: "2026-09-04", amount: 5000, text: "INTERAC E-TRF DEP" },
    { id: "bl-4", date: "2026-09-14", amount: 3000, text: "DEPOSIT CHQ 118" },
    { id: "bl-5", date: "2026-09-26", amount: -1695, text: "EFT OUT 260926-07" },
    { id: "bl-6", date: "2026-09-30", amount: -12.5, text: "MONTHLY ACCOUNT FEE" },
    { id: "bl-7", date: "2026-10-02", amount: 230000, text: "WIRE IN FT1002" },
    { id: "bl-8", date: "2026-10-03", amount: -1130, text: "CHQ 1042 PAID" },
  ]);
  A.seed("trustRecons", [
    { id: "rc-2026-07", month: "2026-07", status: "signed", signedBy: "u-linda", signedAt: "2026-08-14 09:40", bank: 2000, journal: 2000, ledgers: 2000 },
    { id: "rc-2026-08", month: "2026-08", status: "signed", signedBy: "u-linda", signedAt: "2026-09-12 10:14", bank: 50000, journal: 50000, ledgers: 50000 },
  ]);

  /* ---------- trust math ---------- */
  const sign = (t) => (t.kind === "receipt" ? 1 : -1) * t.amount;
  const live = (t) => t.status !== "void";
  const txs = () => A.store.where("trustTx", live).slice().sort((a, b) => a.date.localeCompare(b.date) || a.id.localeCompare(b.id));
  const lines = () => A.store.all("bankLines").slice().sort((a, b) => a.date.localeCompare(b.date));
  const r2 = (n) => Math.round(n * 100) / 100;
  const monthEnd = (m) => { const [y, mo] = m.split("-").map(Number); return A.addDays(`${mo === 12 ? y + 1 : y}-${String(mo === 12 ? 1 : mo + 1).padStart(2, "0")}-01`, -1); };
  const monthName = (m) => new Date(m + "-15T12:00:00").toLocaleDateString("en-CA", { month: "long", year: "numeric" });
  const recon = (m) => A.store.get("trustRecons", "rc-" + m);
  const locked = (d) => { const r = recon(String(d).slice(0, 7)); return !!(r && r.status === "signed"); };
  const statement = (m) => A.store.get("trustStatements", "st-" + m);
  const prevMonth = (m) => A.addDays(m + "-01", -1).slice(0, 7);
  /** Months after the account opened up to `m` (exclusive) that are not signed off: they block sign-off of `m`. */
  const blockers = (m) => { const out = []; let x = prevMonth(m); while (x > ACCOUNT.opened.date.slice(0, 7)) { const r = recon(x); if (!r || r.status !== "signed") out.push(x); x = prevMonth(x); } return out.reverse(); };
  const laterSigned = (m) => A.store.where("trustRecons", (r) => r.month > m && r.status === "signed").map((r) => r.month).sort();
  const lastClosedMonth = () => A.addDays(A.today.slice(0, 7) + "-01", -1).slice(0, 7);

  /** Bank line -> trust entry matches: persisted txId first, then same signed amount within 10 days (one to one). */
  function matches() {
    const used = new Set(), map = {};
    lines().forEach((l) => { if (l.txId) { map[l.id] = l.txId; used.add(l.txId); } });
    const pool = txs().filter((t) => t.status === "posted" && !t.internal);
    lines().forEach((l) => {
      if (map[l.id]) return;
      const t = pool.find((x) => !used.has(x.id) && r2(sign(x)) === r2(l.amount) && x.date <= l.date && A.daysBetween(x.date, l.date) <= 10);
      if (t) { map[l.id] = t.id; used.add(t.id); }
    });
    return map;
  }
  function balance(matterId, upto = "9999-12-31") { return r2(txs().filter((t) => t.matterId === matterId && t.date <= upto).reduce((s, t) => s + sign(t), 0)); }
  function cleared(matterId) { return r2(balance(matterId) - txs().filter((t) => t.matterId === matterId && t.kind === "receipt" && !t.cleared).reduce((s, t) => s + t.amount, 0)); }
  /** Funds that can be paid out now (b3 BIL-C-053): cleared, less protected funds, the minimum balance, and withdrawals waiting for authorization. */
  function available(matterId) {
    const st = setting(matterId), waiting = A.store.where("approvals", (g) => g.status === "pending" && /^trust\.(withdraw|transfer)$/.test(g.effect) && g.payload && (g.payload.matterId === matterId || g.payload.from === matterId)).reduce((s, g) => s + (+g.payload.amount || 0), 0);
    return r2(cleared(matterId) - (st.protected || 0) - (st.minimum || 0) - waiting);
  }
  /** Cash received on one client file (By-Law 9: aggregate of $7,500 or more needs a recorded exception). */
  const cashOnFile = (matterId) => r2(txs().filter((t) => t.matterId === matterId && t.kind === "receipt" && t.method === "cash").reduce((s, t) => s + t.amount, 0));
  /** Three-way reconciliation at a date: bank (adjusted) vs trust journal vs sum of client ledgers. */
  function threeWay(end) {
    const mt = matches(), byTx = new Set(Object.entries(mt).filter(([lid]) => (A.store.get("bankLines", lid) || {}).date <= end).map(([, t]) => t));
    const ls = lines().filter((l) => l.date <= end), ts = txs().filter((t) => t.date <= end);
    const bank = r2(ACCOUNT.opened.balance + ls.reduce((s, l) => s + l.amount, 0));
    const journal = r2(ts.reduce((s, t) => s + sign(t), 0));
    const ids = [...new Set(ts.map((t) => t.matterId))];
    const ledgerRows = ids.map((id) => ({ matterId: id, balance: balance(id, end) }));
    const ledgers = r2(ledgerRows.reduce((s, x) => s + x.balance, 0));
    const outstanding = ts.filter((t) => t.kind !== "receipt" && !t.internal && !byTx.has(t.id));
    const transit = ts.filter((t) => t.kind === "receipt" && !t.internal && !byTx.has(t.id));
    const unmatched = ls.filter((l) => !mt[l.id]);
    const adjusted = r2(bank - unmatched.reduce((s, l) => s + l.amount, 0) + transit.reduce((s, t) => s + t.amount, 0) - outstanding.reduce((s, t) => s + t.amount, 0));
    return { end, bank, journal, ledgers, adjusted, outstanding, transit, unmatched, ledgerRows, variance: r2(adjusted - journal), ledgerVariance: r2(ledgers - journal), negatives: ledgerRows.filter((x) => x.balance < 0) };
  }

  /* ---------- access helpers ---------- */
  const firmWide = () => ["all", "approve", "view"].includes(A.grant("trust.view"));
  const trustMatters = (cap = "trust.view") => A.matters(null, cap);
  const tmatter = (id) => { const m = A.store.get("matters", id); return A.canMatter(m, "trust.view") ? m : null; };
  const mlabel = (id) => { const m = A.store.get("matters", id); return m ? `${m.number} ${m.name}` : id; };
  /** Old tone names -> words in the shared App.STATUS vocabulary (labels stay; colour comes from the vocabulary). */
  const TONE_WORD = { ok: "done", gate: "needs-approval", warn: "due-soon", bad: "overdue", agent: "agent-prepared", "": "draft" };
  const why = (cap, level) => (A.can(cap, level) ? "" : `${A.roleName(A.me().role)} cannot do this (${A.capName(cap)})`);
  const btn = (label, a, cap, level, extra = {}, cls = "btn") => {
    const w = extra.why || why(cap, level);
    return h`<button class="${cls}" data-a="${a}" ${A.raw(Object.entries(extra).filter(([k]) => k !== "why").map(([k, v]) => `data-${k}="${A.esc(v)}"`).join(" "))} ${w ? A.raw(`disabled title="${A.esc(w)}"`) : ""}>${label}</button>`;
  };
  const kindBadge = (t) => t.kind === "receipt" ? A.ui.status("done", "Receipt") : t.kind === "transfer" ? A.ui.status("Fee transfer") : A.ui.status("Disbursement");
  const statusBadges = (t) => h`${t.status === "pending" ? A.ui.status("needs-approval", "Authorized, not sent") : ""}${t.kind === "receipt" && !t.cleared ? A.ui.status("due-soon", "Uncleared") : ""}`;

  /* ---------- shared layout: sub-nav, page header, search, keyboard ---------- */
  const TABS = () => [
    { id: "trust", label: "Today" }, { id: "trust/ledgers", label: "Ledgers" }, { id: "trust/journals", label: "Journals" }, { id: "trust/withdrawals", label: "Withdrawals", count: A.store.where("approvals", (g) => g.status === "pending" && /^trust\.(withdraw|transfer)$/.test(g.effect) && (!g.matterId || tmatter(g.matterId))).length },
    ...(firmWide() ? [{ id: "trust/reconcile", label: "Reconcile" }] : []), { id: "trust/accounts", label: "Accounts & settings" },
    ...(firmWide() ? [{ id: "trust/interest", label: "Interest" }, { id: "trust/year-end", label: "Year-end" }, { id: "trust/audit", label: "Audit trail" }] : []),
  ];
  const subnav = (active) => A.ui.tabs(TABS(), active, "trust-tab");
  A.action("trust-tab", (el) => A.go(el.dataset.id));
  /** Same header everywhere: title, status chip, actions on the right, one-line context below. */
  const head = (title, chip, actions, sub) => A.ui.header({ title, status: chip || undefined, actions: actions ? [actions] : [], sub });
  const key = (k) => h` <kbd>${k}</kbd>`;
  const search = (ph) => h`<input class="input" id="trust-q" data-live="trust-q" data-key="/" placeholder="${ph}  ( / )" value="${q}" autocomplete="off" style="max-width:320px">`;
  let q = "";
  const hit = (...parts) => !q || parts.join(" ").toLowerCase().includes(q.toLowerCase());
  A.live("trust-q", (v) => { q = v; });
  /* Keyboard: the core layer moves over rows (j/k, Enter, a, e, x, n, /); this module adds page keys only through App.ui.keys. */
  const after = () => {};
  /** Who has to act: the agent's work vs a person's. */
  const who = (agent, label) => A.ui.actor(agent ? "agent" : "person", label);

  /* ---------- today (overview) ---------- */
  function overview(el) {
    A.setContext({ kind: "trust", label: "Trust accounts" });
    const fw = firmWide(), now = threeWay(A.today), lm = lastClosedMonth(), tw = threeWay(monthEnd(lm)), rc = recon(lm);
    const gates = A.store.where("approvals", (g) => g.status === "pending" && /^trust\./.test(g.cap) && (!g.matterId || tmatter(g.matterId)));
    const pend = A.store.where("trustTx", (t) => t.status === "pending" && tmatter(t.matterId));
    const unclearedTx = txs().filter((t) => t.kind === "receipt" && !t.cleared && tmatter(t.matterId));
    const low = trustMatters().filter((m) => m.status !== "closed" && setting(m.id).alert && balance(m.id) - (setting(m.id).protected || 0) < setting(m.id).alert);
    const unexplained = fw ? tw.unmatched.filter((l) => !l.explained) : [];
    const due = A.addDays(monthEnd(lm), 25), reconOpen = fw && !(rc && rc.status === "signed");
    const lastFeed = (lines().pop() || {}).date, stale = lastFeed && A.daysBetween(lastFeed, A.today) > 3;
    // One list of everything that needs a person today; each row says who acts and has its one action.
    const items = [
      ...gates.map((g) => ({ tone: "gate", what: g.title, detail: g.summary, owner: who(false, g.cap === "trust.signoff" ? "Licensee signs" : "Signatory authorizes"), prepared: g.preparedBy === "agent" ? "Prepared by the agent" : `Requested by ${A.user(g.preparedBy).name}`,
        act: A.canApprove(g).ok ? h`<button class="btn small" data-a="go" data-to="approvals/${g.id}">Review</button><button class="btn small primary" data-a="gate-approve" data-id="${g.id}" title="Approve (a)">Approve</button>` : h`<span class="muted small">${A.canApprove(g).why}</span>` })),
      ...pend.map((t) => ({ tone: "gate", what: `Send ${A.money(t.amount)} to ${t.party}`, detail: `${mlabel(t.matterId)} · authorized by ${A.user(t.authorizedBy).name} · ${t.method}`, owner: who(false, "Bookkeeper releases at bank"), prepared: "Authorized", act: btn("Mark sent", "trust-execute", "trust.execute", "act", { id: t.id }, "btn small primary") })),
      ...unclearedTx.map((t) => ({ tone: "warn", what: `Confirm deposit ${t.ref} cleared`, detail: `${A.money(t.amount)} · ${mlabel(t.matterId)} · received ${A.date(t.date)}. Not usable until cleared.`, owner: who(false, "Bookkeeper"), prepared: "Recorded", act: btn("Mark cleared", "trust-clear", "trust.record", "act", { id: t.id }, "btn small") })),
      ...unexplained.map((l) => ({ tone: "bad", what: `Explain bank item: ${l.text}`, detail: `${A.money(l.amount)} on ${A.date(l.date)} has no trust entry. A bank charge cannot be paid from client money.`, owner: who(false, "Bookkeeper"), prepared: "✦ Found by bank match", act: btn("Explain", "trust-explain", "trust.reconcile", "act", { id: l.id }, "btn small") })),
      ...(reconOpen ? [{ tone: A.today > due ? "bad" : "warn", what: `Reconcile ${monthName(lm)}`, detail: `Three-way reconciliation due ${A.date(due)} (25 days after month end).`, owner: who(true, "Agent prepares, licensee signs"), prepared: "", act: h`<button class="btn small" data-a="go" data-to="trust/reconcile">Open</button>` }] : []),
      ...now.negatives.filter((x) => tmatter(x.matterId)).map((x) => ({ tone: "bad", what: `Negative ledger ${mlabel(x.matterId)}`, detail: `${A.money(x.balance)}. Replenish from the general account today and report the shortage.`, owner: who(false, "Licensee"), prepared: "", act: h`<button class="btn small" data-a="trust-ledger" data-id="${x.matterId}">Open ledger</button>` })),
      ...low.map((m) => ({ tone: "warn", what: `Retainer below alert: ${m.number}`, detail: `${A.money(balance(m.id) - (setting(m.id).protected || 0))} usable against an alert level of ${A.money(setting(m.id).alert)}.`, owner: who(true, "Agent drafts top-up"), prepared: "", act: btn("✦ Draft top-up", "trust-topup", "trust.request", "act", { matter: m.id }, "btn small agent") })),
      ...(fw && stale ? [{ tone: "warn", what: "Bank feed is stale", detail: `Last bank line ${A.date(lastFeed)}. Import the latest statement before matching.`, owner: who(false, "Bookkeeper"), prepared: "", act: h`<button class="btn small" data-a="go" data-to="trust/reconcile">Bank lines</button>` }] : []),
    ];
    const ok = !items.some((x) => x.tone === "bad");
    A.render(el, h`<div class="page">
      ${head("Trust", A.ui.status(TONE_WORD[ok ? (items.length ? "gate" : "ok") : "bad"], items.length ? `${items.length} to do` : "All clear"),
        h`${btn(h`Record receipt${key("n")}`, "trust-receipt", "trust.record", "act", { key: "n" }, "btn primary")}${btn("Request withdrawal", "trust-request", "trust.request", "act", {}, "btn agent")}`,
        `${ACCOUNT.name} · ${ACCOUNT.bank} ${ACCOUNT.number} · as of ${A.date(A.today)}`)}
      ${subnav("trust")}
      ${fw ? h`<div class="stats">
        ${A.ui.stat("Bank balance", A.money(now.bank), "bank feed to " + A.date(lastFeed), stale ? "gate" : "")}
        ${A.ui.stat("Trust journal", A.money(now.journal), "receipts less payments")}
        ${A.ui.stat("Client ledgers", A.money(now.ledgers), now.ledgerVariance ? `differs by ${A.money(now.ledgerVariance)}` : "agrees with journal", now.ledgerVariance ? "bad" : "")}
        ${A.ui.stat("Uncleared", A.money(unclearedTx.reduce((s, t) => s + t.amount, 0)), `${unclearedTx.length} deposit${unclearedTx.length === 1 ? "" : "s"}`, unclearedTx.length ? "gate" : "")}
        ${A.ui.stat(`${monthName(lm)} reconciliation`, rc && rc.status === "signed" ? "Signed" : "Open", rc && rc.status === "signed" ? `by ${A.user(rc.signedBy).name}` : `due ${A.date(due)}`, rc && rc.status === "signed" ? "" : "gate")}
      </div>` : ""}
      <section class="card"><h2>Needs a person today <span class="muted small">j/k to move · Enter to open · a to approve</span></h2>
        ${items.length ? h`<div>${items.map((x) => h`<div class="row kb-item" style="padding:8px 4px;border-bottom:1px solid var(--line);align-items:flex-start">
          ${A.ui.status(TONE_WORD[x.tone], { gate: "Act", warn: "Check", bad: "Problem" }[x.tone])}
          <div class="grow"><b>${x.what}</b> ${x.owner}<div class="muted small">${x.detail}${x.prepared ? ` · ${x.prepared}` : ""}</div></div><div class="row">${x.act}</div></div>`)}</div>`
          : A.ui.empty("Nothing waiting", "Every deposit has cleared, every payment is sent and the last month is signed off.")}
      </section>
    </div>`);
    after();
  }

  /* ---------- ledgers: list -> detail split ---------- */
  let ledgerView = "matter";
  A.action("trust-ledgerview", (el) => { ledgerView = el.dataset.id; A.refresh(); });
  function ledgerRows() {
    return trustMatters().map((m) => ({ id: m.id, m, bal: balance(m.id), clr: cleared(m.id), av: available(m.id), last: (txs().filter((t) => t.matterId === m.id).pop() || {}).date }))
      .filter((r) => (r.bal !== 0 || r.last) && hit(r.m.number, r.m.name, A.contact(r.m.clientId).name));
  }
  function ledgerList(active) {
    if (ledgerView === "client") return A.ui.table([{ label: "Client", render: (r) => h`<b>${A.contact(r.id).name}</b><div class="muted small">${r.ms.map((m) => m.number).join(", ")}</div>` }, { label: "Balance", num: true, render: (r) => A.money(r.bal) }], clientRows().filter((r) => hit(A.contact(r.id).name)), { onRow: "trust-client", empty: "No client funds held in trust" });
    return A.ui.table([
      { label: "Matter", render: (r) => h`<b>${r.m.number}</b><div class="muted small">${A.contact(r.m.clientId).name}</div>` },
      { label: "Balance", num: true, render: (r) => h`${A.money(r.bal)}${r.bal < 0 ? A.ui.status("overdue", "Negative") : ""}` },
      { label: "Available", num: true, render: (r) => h`${A.money(Math.max(0, r.av))}${r.clr !== r.bal ? h`<div>${A.ui.status("due-soon", "Uncleared")}</div>` : setting(r.id).protected ? h`<div>${A.ui.status(TONE_WORD[""], "Protected")}</div>` : ""}` },
    ], ledgerRows(), { onRow: "trust-ledger", active, empty: q ? "No ledger matches" : "No client funds held in trust" });
  }
  function ledgersRoute(el, p) {
    const m = p && p.matterId ? tmatter(p.matterId) : null;
    if (p && p.matterId && !m) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>You cannot see the trust ledger for this matter.</div></div>`);
    if (m) A.setContext({ kind: "matter", id: m.id, label: `${m.number} ${m.name} · trust ledger` }); else A.setContext({ kind: "trust", label: "Client trust ledgers" });
    A.render(el, h`<div class="page">
      ${head(m ? h`Client trust ledger · ${m.number}` : "Client trust ledgers", m ? (balance(m.id) < 0 ? A.ui.status("overdue", "Negative") : balance(m.id) ? A.ui.status("done", "Funds held") : A.ui.status("Zero balance")) : "",
        m ? h`${A.canMatter(m) ? h`<button class="btn" data-a="go" data-to="matters/${m.id}">Open matter</button>` : ""}` : "", m ? `${m.name} · ${A.contact(m.clientId).name}` : "Pick a ledger on the left. Balances include uncleared deposits; Available is what can be paid out now.")}
      ${subnav("trust/ledgers")}
      <div class="row wrap" style="margin-bottom:10px">${search("Search matter or client")}${A.ui.tabs([{ id: "matter", label: "By matter" }, { id: "client", label: "By client" }], ledgerView, "trust-ledgerview")}</div>
      ${A.ui.split(h`<section class="card">${ledgerList(m && m.id)}</section>`, m ? h`<section class="card">${ledgerHtml(m)}</section>` : null, { emptyTitle: "No ledger open" })}
    </div>`);
    after();
  }
  const ledgerRoute = ledgersRoute;

  /* ---------- journals (books of original entry) ---------- */
  let jtab = "receipts"; const picked = A.selection("trust-receipts");
  A.action("trust-jtab", (el) => { jtab = el.dataset.id; picked.clear(); A.refresh(); });
    A.action("trust-bulk-clear", (el) => {
    if (!A.can("trust.record", "act")) return A.toast(why("trust.record", "act"), "bad");
    const ids = String(el.dataset.ids || picked.ids().join(",")).split(",").filter((id) => { const t = A.store.get("trustTx", id); return t && t.kind === "receipt" && !t.cleared; });
    if (!ids.length) return A.toast("Select uncleared deposits first");
    ids.forEach((id) => { const t = A.store.get("trustTx", id); A.store.put("trustTx", { id, cleared: true, clearedAt: A.now() }); A.audit("trust.cleared", { target: id, note: `${t.ref} ${A.money(t.amount)}` }); });
    picked.clear(); A.toast(`${ids.length} deposit${ids.length === 1 ? "" : "s"} marked cleared`, "ok");
  });
  const stale = (t) => t.method === "cheque" && t.status === "posted" && t.kind !== "receipt" && !t.internal && !Object.values(matches()).includes(t.id) && A.daysBetween(t.date, A.today) > 180;
  function journalsRoute(el) {
    A.setContext({ kind: "trust", label: "Trust journals" });
    const mt = new Set(Object.values(matches()));
    const rec = txs().filter((t) => t.kind === "receipt" && tmatter(t.matterId) && hit(t.party, t.ref, t.memo, mlabel(t.matterId))).reverse();
    const out = txs().filter((t) => t.kind !== "receipt" && tmatter(t.matterId) && hit(t.party, t.ref, t.memo, mlabel(t.matterId))).reverse();
    const recStatus = (t) => t.reversedBy ? A.ui.status("overdue", "Reversed") : !t.cleared ? A.ui.status("due-soon", "Uncleared") : locked(t.date) ? A.ui.status("done", "Posted · locked") : A.ui.status("done", "Posted");
    const outStatus = (t) => t.status === "void" ? A.ui.status("Void") : t.status === "pending" ? A.ui.status("needs-approval", "Authorized, not sent") : t.internal ? A.ui.status("done", "Transfer posted") : stale(t) ? A.ui.status("overdue", "Stale cheque") : mt.has(t.id) ? A.ui.status("done", "Cleared at bank") : A.ui.status("due-soon", "Outstanding");
    const uncl = rec.filter((t) => !t.cleared);
    A.render(el, h`<div class="page">
      ${head("Trust journals", "", h`${btn(h`Record receipt${key("n")}`, "trust-receipt", "trust.record", "act", { key: "n" }, "btn primary")}`, "Books of original entry in date order. Receipts and payments are never edited; a correction is a reversing entry in the current month.")}
      ${subnav("trust/journals")}
      <div class="row wrap" style="margin-bottom:10px">${search("Search payer, payee, ref or matter")}${A.ui.tabs([{ id: "receipts", label: "Receipts", count: rec.length }, { id: "disbursements", label: "Payments and transfers", count: out.length }], jtab, "trust-jtab")}<span class="grow"></span>
        </div>
      <section class="card">${jtab === "receipts" ? A.ui.table([
        { label: "Received", render: (t) => A.date(t.date) }, { label: "Matter", render: (t) => mlabel(t.matterId) }, { label: "Payer", render: (t) => h`${t.party}<div class="muted small">${t.memo}</div>` },
        { label: "Method · ref", render: (t) => h`${t.method} · ${t.ref}${t.cashException ? A.ui.status("due-soon", "Cash exception") : ""}` }, { label: "Amount", num: true, render: (t) => A.money(t.amount) },
        { label: "Status", render: recStatus },
        { label: "", render: (t) => h`<div class="row">${!t.cleared ? btn("Cleared", "trust-clear", "trust.record", "act", { id: t.id }, "btn small") : ""}${!t.reversedBy && !t.internal ? btn("Reverse", "trust-reverse", "trust.record", "act", { id: t.id, key: "e" }, "btn small") : ""}</div>` },
      ], rec, { empty: q ? "No receipt matches" : "No receipts recorded", select: uncl.length ? "trust-receipts" : undefined })
      : A.ui.table([
        { label: "Date", render: (t) => A.date(t.date) }, { label: "Matter", render: (t) => mlabel(t.matterId) }, { label: "Payee", render: (t) => h`${t.party}<div class="muted small">${t.memo}</div>` },
        { label: "Instrument", render: (t) => h`${t.method} · ${t.bankRef || t.ref || "—"}` }, { label: "Amount", num: true, render: (t) => A.money(t.amount) }, { label: "Status", render: outStatus },
        { label: "", render: (t) => h`<div class="row">${isWithdrawal(t) ? h`<button class="btn small" data-a="trust-auth" data-id="${t.id}">Authorization</button>` : ""}${t.status === "pending" ? btn("Void", "trust-void", "trust.execute", "act", { id: t.id }, "btn small danger") : ""}</div>` },
      ], out, { empty: q ? "No payment matches" : "No payments out of trust" })}</section>
      ${jtab === "receipts" ? A.ui.bulkBar("trust-receipts", [{ label: "Mark cleared", a: "trust-bulk-clear", primary: true, disabled: !A.can("trust.record", "act"), why: why("trust.record", "act") }], { note: "Only uncleared deposits change" }) : ""}
    </div>`);
    after();
  }
  A.action("trust-reverse", (el) => {
    if (!A.can("trust.record", "act")) return A.toast(why("trust.record", "act"), "bad");
    const t = A.store.get("trustTx", el.dataset.id); if (!t || t.reversedBy) return;
    if (balance(t.matterId) - t.amount < 0) return A.toast(`Reversing would make ${mlabel(t.matterId)} negative`, "bad");
    A.modal(`Reverse receipt ${t.ref} · ${A.money(t.amount)}`, h`<p class="small">Posts a reversing entry dated ${A.date(A.today)}. The original stays in the journal. Use for a returned (NSF) cheque or a deposit recorded to the wrong ledger.</p><label class="field">Reason<textarea name="reason" rows="2" class="input">Cheque returned by the bank (NSF).</textarea></label>`, [{ label: "Cancel" }, { label: "Post reversal", primary: true, run: (d) => {
      const reason = d.querySelector("[name=reason]").value.trim(); if (!reason) { A.toast("Enter a reason", "bad"); return false; }
      if (locked(A.today)) { A.toast("This month is locked", "bad"); return false; }
      const r = A.store.put("trustTx", { id: A.id("tt"), date: A.today, matterId: t.matterId, kind: "disbursement", amount: t.amount, method: "reversal", party: t.party, ref: `REV ${t.ref}`, memo: `Reversal of ${t.ref}: ${reason}`, status: "posted", cleared: true, by: A.me().id, reverses: t.id, internal: !t.cleared });
      A.store.put("trustTx", { id: t.id, reversedBy: r.id }); A.audit("trust.receipt.reversed", { target: t.id, note: `${t.ref} ${A.money(t.amount)}: ${reason}` }); A.toast("Reversal posted", "ok");
    } }]);
  });
  A.action("trust-void", (el) => {
    if (!A.can("trust.execute", "act")) return A.toast(why("trust.execute", "act"), "bad");
    const t = A.store.get("trustTx", el.dataset.id); if (!t || t.status !== "pending") return;
    A.modal(`Void ${t.ref || "payment"} · ${A.money(t.amount)} to ${t.party}`, h`<p class="small">The payment was authorized but not released at the bank. Voiding returns the funds to ${mlabel(t.matterId)}. Keep the spoiled cheque with the records.</p><label class="field">Reason<input name="reason" class="input" value="Cheque spoiled in printing"></label>`, [{ label: "Cancel" }, { label: "Void", primary: true, run: (d) => {
      const reason = d.querySelector("[name=reason]").value.trim() || "Voided";
      A.store.put("trustTx", { id: t.id, status: "void", voidedAt: A.now(), voidedBy: A.me().id, voidReason: reason }); A.audit("trust.withdraw.voided", { target: t.id, note: `${A.money(t.amount)} to ${t.party}: ${reason}` }); A.toast("Voided", "ok");
    } }]);
  });

  /* ---------- withdrawals and transfers ---------- */
  function withdrawalsRoute(el) {
    A.setContext({ kind: "trust", label: "Trust withdrawals" });
    const gates = A.store.where("approvals", (g) => g.status === "pending" && /^trust\.(withdraw|transfer)$/.test(g.effect) && (!g.matterId || tmatter(g.matterId)));
    const pend = A.store.where("trustTx", (t) => t.status === "pending" && tmatter(t.matterId));
    const sent = txs().filter((t) => isWithdrawal(t) && t.status === "posted" && tmatter(t.matterId)).reverse().slice(0, 12);
    A.render(el, h`<div class="page">
      ${head("Withdrawals", gates.length ? A.ui.status("needs-approval", `${gates.length} awaiting authorization`) : "", h`${btn("Transfer between matters", "trust-transfer", "trust.request", "act")}${btn(h`Request withdrawal${key("n")}`, "trust-request", "trust.request", "act", { key: "n" }, "btn primary")}`,
        "Request → designated signatory authorizes (not the requester) → bookkeeper releases at the bank and records the confirmation. Only cheque, bank draft or electronic transfer; never cash.")}
      ${subnav("trust/withdrawals")}
      <section style="margin-bottom:14px"><h2 style="font-size:14.5px">1 · Awaiting authorization</h2>${gates.length ? gates.map((g) => h`<div>${A.ui.gateCard(g)}</div>`) : A.ui.empty("Nothing waiting", "New requests appear here once the agent has checked them.")}</section>
      <section class="card" style="margin-bottom:14px"><h2>2 · Authorized, to release at the bank</h2>${pendingTable(pend)}</section>
      <section class="card"><h2>3 · Sent</h2>${A.ui.table([{ label: "Sent", render: (t) => A.date(t.date) }, { label: "Matter", render: (t) => mlabel(t.matterId) }, { label: "Payee", key: "party" }, { label: "Bank ref", render: (t) => t.bankRef || t.ref }, { label: "Amount", num: true, render: (t) => A.money(t.amount) }, { label: "Authorized by", render: (t) => A.user(t.authorizedBy).name }], sent, { onRow: "trust-auth", empty: "No withdrawals yet" })}</section>
    </div>`);
    after();
  }
  A.action("trust-transfer", () => {
    if (!A.can("trust.request", "act")) return A.toast(why("trust.request", "act"), "bad");
    const from = trustMatters("trust.request").filter((m) => available(m.id) > 0), to = trustMatters("trust.request");
    if (!from.length) return A.toast("No matter has funds available to transfer", "bad");
    A.modal("Transfer between client ledgers", h`<div class="form">
      <label class="field">From<select name="from">${from.map((m) => h`<option value="${m.id}">${m.number} ${m.name} (${A.money(available(m.id))} available)</option>`)}</select></label>
      <label class="field">To<select name="to">${to.map((m) => h`<option value="${m.id}">${m.number} ${m.name}</option>`)}</select></label>
      <label class="field">Amount (CAD)<input name="amount" inputmode="decimal" placeholder="0.00"></label>
      <label class="field">Client's written direction (ref)<input name="authority" placeholder="Direction dated …"></label>
      <label class="field">Reason<input name="reason" placeholder="Funds moved to the new matter"></label></div>
      <p class="muted small">Moving money between client ledgers needs the client's written direction and a signatory's authorization. It does not touch the bank.</p>`, [{ label: "Cancel" }, { label: "Prepare transfer", primary: true, run: (d) => {
      const v = A.formValues(d), amount = r2(parseFloat(String(v.amount).replace(/[$,]/g, "")));
      if (v.from === v.to) { A.toast("Pick two different ledgers", "bad"); return false; }
      if (!(amount > 0)) { A.toast("Enter an amount", "bad"); return false; }
      const fm = A.store.get("matters", v.from), tm = A.store.get("matters", v.to), same = fm.clientId === tm.clientId;
      A.gate({ kind: "trust-transfer", cap: "trust.authorize", matterId: v.from, preparedBy: A.me().id, effect: "trust.transfer", title: `Trust transfer ${A.money(amount)} ${fm.number} → ${tm.number}`,
        summary: `${v.reason || "Transfer between client ledgers"}. Requested by ${A.me().name}.`,
        checks: [{ label: `Available ${A.money(available(v.from))} covers ${A.money(amount)}`, ok: available(v.from) >= amount, blocking: true },
          { label: v.authority ? `Client direction on file (${v.authority})` : "No written client direction", ok: !!v.authority, blocking: true },
          { label: same ? "Same client on both matters" : "Different clients: the paying client's direction must name the receiving client", ok: true }],
        payload: { from: v.from, to: v.to, matterId: v.from, amount, authority: v.authority, reason: v.reason, requestedBy: A.me().id } });
      A.toast("Transfer sent for authorization");
    } }]);
  });
  A.effect("trust.transfer", (p, g) => {
    if (locked(A.today)) return A.toast("This month is locked; not posted", "bad");
    if (available(p.from) < p.amount) { A.audit("trust.transfer.blocked", { target: g.id, note: "Insufficient available funds" }); return A.toast("Funds no longer cover this; not posted", "bad"); }
    const ref = "TRF-" + g.id.slice(-5), base = { date: A.today, amount: p.amount, method: "ledger transfer", ref, status: "posted", cleared: true, internal: true, by: A.me().id, requestedBy: p.requestedBy, authorizedBy: A.me().id, approvalId: g.id };
    A.store.put("trustTx", { ...base, id: A.id("tt"), matterId: p.from, kind: "transfer", party: mlabel(p.to), memo: `Transfer to ${mlabel(p.to)} (${p.authority})` });
    A.store.put("trustTx", { ...base, id: A.id("tt"), matterId: p.to, kind: "receipt", party: mlabel(p.from), memo: `Transfer from ${mlabel(p.from)} (${p.authority})` });
    A.audit("trust.transfer.posted", { target: g.id, note: `${A.money(p.amount)} ${mlabel(p.from)} → ${mlabel(p.to)}` });
  });

  /* ---------- retainer top-up, ledger export, share ---------- */
  A.action("trust-topup", (el) => {
    if (!A.can("trust.request", "act")) return A.toast(why("trust.request", "act"), "bad");
    const m = tmatter(el.dataset.matter); if (!m) return;
    const st = setting(m.id), need = r2(Math.max(st.alert || st.minimum || 0, st.minimum || 0) * 2 - (balance(m.id) - (st.protected || 0)));
    A.agent.run({ title: `Draft retainer top-up: ${m.number}`, matterId: m.id, steps: ["Reading the trust ledger", "Totalling unbilled work", "Drafting the request to the client"], done() {
      return A.gate({ kind: "trust-topup", cap: "documents.release", matterId: m.id, preparedBy: "agent", effect: "trust.topup", title: `Retainer top-up request: ${m.number}`,
        summary: `Ask ${A.contact(m.clientId).name} for a further retainer of ${A.money(Math.max(need, 500))} to bring the trust balance back above the agreed minimum.`,
        body: `<p>Dear ${A.esc(A.contact(m.clientId).name)},</p><p>Your trust balance on file ${A.esc(m.number)} is ${A.esc(A.money(balance(m.id)))}. Under our retainer agreement we keep at least ${A.esc(A.money(st.minimum || 0))} in trust. Please send a further retainer of ${A.esc(A.money(Math.max(need, 500)))} by e-transfer or cheque payable to Linda K Law in trust.</p>`,
        checks: [{ label: "Amount based on the matter's minimum balance setting", ok: true }], payload: { matterId: m.id, amount: Math.max(need, 500) } });
    } });
  });
  A.effect("trust.topup", (p) => { A.audit("trust.topup.sent", { target: p.matterId, note: `Top-up request ${A.money(p.amount)} sent` }); A.toast("Top-up request sent to the client", "ok"); });
  A.action("trust-ledger-export", (el) => {
    const m = tmatter(el.dataset.matter); if (!m) return; let run = 0;
    const txt = [`CLIENT TRUST LEDGER,${m.number} ${m.name},${A.contact(m.clientId).name},as of ${A.today}`, "date,type,ref,party,received,paid out,balance", ...txs().filter((t) => t.matterId === m.id).map((t) => [t.date, t.kind, t.ref, `"${t.party}"`, t.kind === "receipt" ? t.amount.toFixed(2) : "", t.kind !== "receipt" ? t.amount.toFixed(2) : "", (run = r2(run + sign(t))).toFixed(2)].join(","))].join("\n");
    A.audit("trust.ledger.exported", { target: m.id, note: `${m.number} ledger statement` });
    A.modal(`Ledger statement · ${m.number}`, h`<pre style="white-space:pre;overflow:auto;max-height:320px;font-size:11px">${txt}</pre>`);
  });
  A.action("trust-ledger-share", (el) => {
    const m = tmatter(el.dataset.matter); if (!m) return;
    if (!A.can("trust.record", "act")) return A.toast(why("trust.record", "act"), "bad");
    A.gate({ kind: "trust-statement-share", cap: "documents.release", matterId: m.id, preparedBy: A.me().id, effect: "trust.share", title: `Share trust statement with ${A.contact(m.clientId).name}`, summary: `Post the ${m.number} trust ledger statement (balance ${A.money(balance(m.id))}) to the client portal.`, checks: [{ label: "Statement covers this matter only", ok: true }], payload: { matterId: m.id } });
    A.toast("Sent for approval before it reaches the client");
  });
  A.effect("trust.share", (p) => { A.audit("trust.ledger.shared", { target: p.matterId, note: "Trust statement shared to portal" }); A.toast("Shared to the client portal", "ok"); });

  /* ---------- accounts, signatories, matter settings ---------- */
  function accountsRoute(el) {
    A.setContext({ kind: "trust", label: "Trust accounts and settings" });
    const sigs = A.store.where("users", (u) => u.signatory), ms = trustMatters().filter((m) => m.status !== "closed" && hit(m.number, m.name));
    const canSet = A.can("trust.record", "act");
    A.render(el, h`<div class="page">
      ${head("Accounts & settings", "", btn("Change signatories", "trust-signatories", "trust.signatories", "act"), "Trust bank accounts, who may authorize withdrawals, and each matter's minimum and protected funds.")}
      ${subnav("trust/accounts")}
      <div class="grid g2">
        <section class="card"><h2>${ACCOUNT.name} ${A.ui.status("done", "Active")}</h2><dl class="kv">
          <dt>Type</dt><dd>Pooled (mixed) trust account</dd><dt>Institution</dt><dd>${ACCOUNT.bank}</dd><dt>Account</dt><dd>${ACCOUNT.number}</dd>
          <dt>Title</dt><dd>Linda K Law, in trust</dd><dt>Opened</dt><dd>${A.date(ACCOUNT.opened.date)}</dd><dt>Bank connection</dt><dd>Statement import (monthly)</dd></dl>
          <p class="muted small">Separate interest-bearing accounts: ${A.store.all("trustSepAccounts").filter((a) => tmatter(a.matterId)).map((a) => `${a.number} (${(A.store.get("matters", a.matterId) || {}).number})`).join(", ") || "none"}. ${firmWide() ? h`<button class="btn small" data-a="go" data-to="trust/interest">Interest and accounts</button>` : ""}</p></section>
        <section class="card"><h2>Signing authority</h2>${A.ui.table([{ label: "Signatory", render: (u) => h`${A.ui.avatar(u.id)} <b>${u.name}</b>` }, { label: "Role", render: (u) => A.roleName(u.role) }, { label: "", render: () => A.ui.status("done", "Licensee") }], sigs, { empty: "No signatories: no withdrawal can be authorized" })}
          <p class="muted small">Only these licensees can authorize a withdrawal or transfer. Changes need another signatory's approval.</p></section>
      </div>
      <section class="card" style="margin-top:14px"><h2>Matter trust settings</h2><div class="row" style="margin-bottom:8px">${search("Search matter")}</div>
        ${A.ui.table([{ label: "Matter", render: (m) => h`<b>${m.number}</b> ${m.name}` }, { label: "Minimum ($)", num: true, render: (m) => A.money(setting(m.id).minimum || 0) }, { label: "Alert below ($)", num: true, render: (m) => A.money(setting(m.id).alert || 0) },
          { label: "Protected ($)", num: true, render: (m) => h`${A.money(setting(m.id).protected || 0)}${setting(m.id).purpose ? h`<div class="muted small">${setting(m.id).purpose}</div>` : ""}` },
          { label: "", render: (m) => h`<div class="row">${btn(h`Edit${key("e")}`, "trust-setting", "trust.record", "act", { id: m.id, key: "e" }, "btn small")}${setting(m.id).protected ? btn("Release", "trust-release", "trust.request", "act", { id: m.id }, "btn small") : ""}</div>` }], ms.map((m) => ({ ...m, id: m.id })), { empty: "No open matters" })}
        ${canSet ? "" : h`<p class="muted small">${why("trust.record", "act")}</p>`}</section>
    </div>`);
    after();
  }
  A.action("trust-setting", (el) => {
    if (!A.can("trust.record", "act")) return A.toast(why("trust.record", "act"), "bad");
    const m = tmatter(el.dataset.id); if (!m) return; const st = setting(m.id);
    A.modal(`Trust settings · ${m.number}`, h`<div class="form"><label class="field">Minimum balance (CAD)<input name="minimum" inputmode="decimal" value="${st.minimum || 0}"></label>
      <label class="field">Alert when usable balance falls below (CAD)<input name="alert" inputmode="decimal" value="${st.alert || 0}"></label></div>
      <p class="muted small">Protected funds can only be added by recording a receipt for a purpose and released with a signatory's approval.</p>`, [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
      const v = A.formValues(d), mn = r2(+String(v.minimum).replace(/[$,]/g, "")), al = r2(+String(v.alert).replace(/[$,]/g, ""));
      if (!(mn >= 0) || !(al >= 0)) { A.toast("Enter amounts of zero or more", "bad"); return false; }
      A.store.put("trustSettings", { id: m.id, minimum: mn, alert: al, protected: st.protected || 0, purpose: st.purpose });
      A.audit("trust.settings.changed", { target: m.id, note: `${m.number}: minimum ${A.money(st.minimum || 0)} → ${A.money(mn)}, alert ${A.money(al)}` }); A.toast("Saved", "ok");
    } }]);
  });
  A.action("trust-release", (el) => {
    if (!A.can("trust.request", "act")) return A.toast(why("trust.request", "act"), "bad");
    const m = tmatter(el.dataset.id), st = m && setting(m.id); if (!m || !st.protected) return;
    if (A.store.where("approvals", (g) => g.status === "pending" && g.effect === "trust.release" && g.payload.matterId === m.id).length) return A.toast("A release is already waiting");
    A.gate({ kind: "trust-release", cap: "trust.authorize", matterId: m.id, preparedBy: A.me().id, effect: "trust.release", title: `Release protected funds ${A.money(st.protected)} on ${m.number}`, summary: `${st.purpose || "Funds held for a purpose"}. Release only when the condition is met.`, checks: [{ label: "Condition met (confirm on the file)", ok: true }], payload: { matterId: m.id, amount: st.protected } });
    A.toast("Release sent for authorization");
  });
  A.effect("trust.release", (p) => { A.store.put("trustSettings", { id: p.matterId, protected: 0, purpose: "" }); A.audit("trust.protected.released", { target: p.matterId, note: `${A.money(p.amount)} released` }); });
  A.action("trust-signatories", () => {
    if (!A.can("trust.signatories", "act")) return A.toast(why("trust.signatories", "act"), "bad");
    const lic = A.store.where("users", (u) => ["owner", "lawyer", "associate"].includes(u.role));
    A.modal("Change signatories", h`<p class="small">Only licensees can hold trust signing authority. Another signatory approves the change.</p>${lic.map((u) => h`<label class="field"><span><input type="checkbox" name="s-${u.id}" ${u.signatory ? A.raw("checked") : ""}> ${u.name} · ${A.roleName(u.role)}</span></label>`)}`, [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
      const v = A.formValues(d), ids = lic.filter((u) => v["s-" + u.id]).map((u) => u.id);
      if (!ids.length) { A.toast("At least one signatory is required", "bad"); return false; }
      A.gate({ kind: "trust-signatories", cap: "trust.signatories", preparedBy: A.me().id, effect: "trust.signatories", title: "Change trust signatories", summary: `New signatories: ${ids.map((id) => A.user(id).name).join(", ")}.`, checks: [{ label: "All are licensees", ok: true }, { label: "At least one signatory", ok: true }], payload: { ids } });
      A.toast("Sent for approval");
    } }]);
  });
  A.effect("trust.signatories", (p) => { A.store.where("users", (u) => ["owner", "lawyer", "associate"].includes(u.role)).forEach((u) => A.store.put("users", { id: u.id, signatory: p.ids.includes(u.id) })); A.audit("trust.signatories.changed", { note: p.ids.map((id) => A.user(id).name).join(", ") }); });

  /* ---------- year-end: LSO annual report, trust section ---------- */
  function yearEndRoute(el) {
    A.setContext({ kind: "trust", label: "LSO annual report: trust" });
    if (!firmWide()) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>The year-end worksheet needs firm-wide trust access.</div></div>`);
    const yr = A.today.slice(0, 4), ye = A.store.get("trustYearEnd", "ye-" + yr);
    const months = []; for (let m = ACCOUNT.opened.date.slice(0, 7); m < lastClosedMonth();) { const [y, mo] = m.split("-").map(Number); m = `${mo === 12 ? y + 1 : y}-${String(mo === 12 ? 1 : mo + 1).padStart(2, "0")}`; if (m <= lastClosedMonth() && m.startsWith(yr)) months.push(m); }
    const signed = months.filter((m) => recon(m) && recon(m).status === "signed"), late = signed.filter((m) => recon(m).signedAt.slice(0, 10) > A.addDays(monthEnd(m), 25));
    const shortages = months.filter((m) => threeWay(monthEnd(m)).negatives.length);
    const cash = trustMatters().filter((m) => cashOnFile(m.id) >= CASH_LIMIT);
    const dormant = trustMatters().filter((m) => { const d = (txs().filter((t) => t.matterId === m.id).pop() || {}).date; return balance(m.id) && d && A.daysBetween(d, A.today) >= 730; });
    const eft = txs().filter((t) => isWithdrawal(t) && t.method !== "cheque" && t.date.startsWith(yr)).length;
    const rows = [
      ["Trust accounts held", "1 pooled (mixed) trust account; no separate interest-bearing accounts", true],
      ["Monthly reconciliations completed", `${signed.length} of ${months.length} months to ${monthName(lastClosedMonth())}`, signed.length === months.length],
      ["Reconciliations completed late (over 25 days)", late.length ? late.map(monthName).join(", ") : "None", !late.length],
      ["Trust shortages in the year", shortages.length ? shortages.map(monthName).join(", ") : "None", !shortages.length],
      [`Cash of ${A.money(CASH_LIMIT)} or more on one file`, cash.length ? cash.map((m) => m.number).join(", ") : "None", !cash.length],
      ["Unclaimed or dormant balances (2 years+)", dormant.length ? dormant.map((m) => m.number).join(", ") : "None", !dormant.length],
      ["Electronic trust transfers used", eft ? `Yes, ${eft} in ${yr}` : "No", true],
    ];
    A.render(el, h`<div class="page">
      ${head(`LSO annual report · trust section ${yr}`, ye ? A.ui.status("done", `Confirmed by ${A.user(ye.by).name}`) : rows.some((r) => !r[2]) ? A.ui.status("due-soon", "Needs attention") : A.ui.status("needs-approval", "Draft"),
        h`<button class="btn" data-a="trust-ye-export" data-year="${yr}">Export evidence pack</button>${ye ? "" : btn("Confirm answers", "trust-ye-confirm", "trust.signoff", "act", { year: yr }, "btn primary")}`,
        "The agent fills the worksheet from the trust records; the licensee confirms it and files the report on the LSO portal themselves. Year to date; final answers after the fiscal year end.")}
      ${subnav("trust/year-end")}
      <section class="card">${A.ui.table([{ label: "Question", render: (r) => r[0] }, { label: "Answer from the records", render: (r) => r[1] }, { label: "", render: (r) => (r[2] ? A.ui.status("done", "OK") : A.ui.status("due-soon", "Review")) }], rows.map((r, i) => Object.assign(r, { id: String(i) })))}</section>
    </div>`);
    after();
  }
  A.action("trust-ye-confirm", (el) => {
    if (!A.can("trust.signoff", "approve")) return A.toast(`${A.roleName(A.me().role)} cannot confirm (licensee with sign-off authority only)`, "bad");
    A.store.put("trustYearEnd", { id: "ye-" + el.dataset.year, year: el.dataset.year, by: A.me().id, at: A.now() }); A.audit("trust.lso.confirmed", { target: "ye-" + el.dataset.year, note: "Annual report trust answers confirmed" }); A.toast("Answers confirmed", "ok");
  });
  A.action("trust-ye-export", (el) => { A.audit("trust.lso.exported", { target: "ye-" + el.dataset.year }); A.modal(`Evidence pack ${el.dataset.year}`, h`<p class="small">Signed reconciliation packages for each month, the client trust listing at each month end, the receipts and payments journals, and the withdrawal authorizations, as one PDF with a CSV index.</p>`); });

  /* ---------- audit trail ---------- */
  let auditWho = "all";
  A.action("trust-audit-who", (el) => { auditWho = el.dataset.id; A.refresh(); });
  const trustAudit = () => A.store.where("audit", (a) => /^(trust|gate|agent\.run)/.test(a.action) && (/^trust/.test(a.action) || /trust|reconcil|withdraw|transfer/i.test(a.note || "")));
  const chainHash = (rows) => rows.slice().reverse().reduce((hsh, a) => { const s = hsh + "|" + a.at + a.actor + a.action + (a.target || "") + (a.note || ""); let x = 0; for (let i = 0; i < s.length; i++) x = (x * 31 + s.charCodeAt(i)) >>> 0; return x.toString(16).padStart(8, "0"); }, "0");
  function auditRoute(el) {
    A.setContext({ kind: "trust", label: "Trust audit trail" });
    if (!firmWide()) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>The trust audit trail needs firm-wide trust access.</div></div>`);
    const rows = trustAudit().filter((a) => (auditWho === "all" || (auditWho === "agent" ? a.actor === "agent" : a.actor !== "agent")) && hit(a.action, a.note, A.user(a.actor).name));
    A.render(el, h`<div class="page">
      ${head("Trust audit trail", A.ui.status("done", "Append-only"), h`<button class="btn" data-a="trust-audit-verify">Verify chain</button><button class="btn" data-a="trust-audit-export">Export</button>`, "Every trust event, who did it, and the approval that allowed it. Trust journals and ledgers are kept 10 years.")}
      ${subnav("trust/audit")}
      <div class="row wrap" style="margin-bottom:10px">${search("Search events")}${A.ui.tabs([{ id: "all", label: "All" }, { id: "human", label: "People" }, { id: "agent", label: "✦ Agent" }], auditWho, "trust-audit-who")}</div>
      <section class="card">${A.ui.table([{ label: "When", key: "at" }, { label: "Who", render: (a) => h`${A.ui.avatar(a.actor)} ${A.user(a.actor).name}` }, { label: "Event", render: (a) => h`<code>${a.action}</code>` }, { label: "Detail", render: (a) => a.note || a.target || "" }], rows.slice(0, 80), { empty: "No trust events yet" })}</section>
      ${retentionCard()}
    </div>`);
    after();
  }
  A.action("trust-audit-verify", () => { const rows = trustAudit(); A.audit("trust.audit.verified", { note: `${rows.length} events, head ${chainHash(rows)}` }); A.toast(`Chain intact: ${rows.length} events, head ${chainHash(rows)}`, "ok"); });
  A.action("trust-audit-export", () => { const rows = trustAudit(); A.modal("Trust audit export", h`<pre style="white-space:pre;overflow:auto;max-height:320px;font-size:11px">${["at,actor,event,target,note", ...rows.map((a) => [a.at, A.user(a.actor).name, a.action, a.target || "", `"${(a.note || "").replace(/"/g, "'")}"`].join(","))].join("\n")}</pre>`); });

  function pendingTable(pend) {
    return A.ui.table([{ label: "Authorized", render: (t) => h`${A.date(t.date)}<div class="muted small">by ${A.user(t.authorizedBy).name}</div>` }, { label: "Matter", render: (t) => mlabel(t.matterId) }, { label: "Payee", key: "party" }, { label: "Method", key: "method" },
      { label: "Amount", num: true, render: (t) => A.money(t.amount) }, { label: "", render: (t) => h`<div class="row">${t.method === "cheque" ? btn("Print cheque", "trust-cheque", "trust.execute", "act", { id: t.id }, "btn small") : ""}<button class="btn small" data-a="trust-auth" data-id="${t.id}">Authorization</button>${btn("Mark sent at bank", "trust-execute", "trust.execute", "act", { id: t.id }, "btn small")}</div>` }], pend);
  }
  function threeWayTable(tw) {
    const sum = (a, f) => a.reduce((s, x) => s + f(x), 0);
    const ok = tw.variance === 0 && tw.ledgerVariance === 0;
    return h`<dl class="kv">
      <dt>Bank statement</dt><dd class="num">${A.money(tw.bank)}</dd>
      <dt>Less outstanding cheques/EFTs</dt><dd>${A.money(-sum(tw.outstanding, (t) => t.amount))} <span class="muted small">${tw.outstanding.map((t) => t.ref).join(", ")}</span></dd>
      <dt>Add deposits in transit</dt><dd>${A.money(sum(tw.transit, (t) => t.amount))} <span class="muted small">${tw.transit.map((t) => t.ref).join(", ")}</span></dd>
      <dt>Unmatched bank items</dt><dd>${A.money(-sum(tw.unmatched, (l) => l.amount))} <span class="muted small">${tw.unmatched.map((l) => l.text).join(", ")}</span></dd>
      <dt><b>Adjusted bank</b></dt><dd><b>${A.money(tw.adjusted)}</b></dd>
      <dt><b>Trust journal</b></dt><dd><b>${A.money(tw.journal)}</b></dd>
      <dt><b>Client ledgers</b></dt><dd><b>${A.money(tw.ledgers)}</b></dd>
      <dt>Variance</dt><dd>${ok ? A.ui.status("done", "Nil: three-way balanced") : A.ui.status("overdue", `Bank ${A.money(tw.variance)} · ledgers ${A.money(tw.ledgerVariance)}`)}</dd>
    </dl>`;
  }

  /* ---------- client ledger ---------- */
  function ledgerHtml(m) {
    let run = 0;
    const rows = txs().filter((t) => t.matterId === m.id).map((t) => ({ ...t, run: (run = r2(run + sign(t))) })).reverse();
    const bal = balance(m.id), clr = cleared(m.id), av = available(m.id), st = setting(m.id), last = (txs().filter((t) => t.matterId === m.id).pop() || {}).date;
    const closing = /clos/i.test(m.status || "") && bal !== 0, dormant = bal && last && A.daysBetween(last, A.today) >= 730;
    return h`<div class="stats">${A.ui.stat("Trust balance", A.money(bal), A.contact(m.clientId).name, bal < 0 ? "bad" : "")}${A.ui.stat("Uncleared", A.money(r2(bal - clr)), clr !== bal ? "not usable yet" : "all funds cleared", clr !== bal ? "gate" : "")}
        ${A.ui.stat("Available to pay out", A.money(Math.max(0, av)), `after minimum ${A.money(st.minimum || 0)}${st.protected ? ` and protected ${A.money(st.protected)}` : ""}`, av <= 0 && bal > 0 ? "gate" : "")}</div>
      ${st.protected ? h`<p class="small">${A.ui.status("needs-approval", "Protected")} ${A.money(st.protected)}: ${st.purpose || "held for a purpose"}</p>` : ""}
      ${closing || dormant ? h`<div class="card" style="margin-bottom:10px;border-color:var(--gate)"><b>${dormant ? "Dormant balance (no activity for 2 years)" : "Matter closing with money in trust"}</b><div class="muted small">Bring the ledger to zero: apply to a delivered final bill, refund the client, or ${dormant ? "apply to the LSO as unclaimed trust funds after trying to reach the client" : "pay out on the client's direction"}.</div>
        <div class="row" style="margin-top:6px">${btn("✦ Propose refund", "trust-refund", "trust.request", "act", { matter: m.id }, "btn small agent")}</div></div>` : ""}
      <div class="row wrap" style="margin-bottom:10px">${btn("Request withdrawal", "trust-request", "trust.request", "act", { matter: m.id, why: !A.canMatter(m, "trust.request") ? "Not on this matter" : av <= 0 ? "No funds available to pay out" : why("trust.request", "act") }, "btn agent")}
        ${btn("Record receipt", "trust-receipt", "trust.record", "act", { matter: m.id })}${btn("✦ Request top-up", "trust-topup", "trust.request", "act", { matter: m.id }, "btn")}
        <span class="grow"></span><button class="btn small" data-a="trust-ledger-export" data-matter="${m.id}">Export statement</button>${btn("Share with client", "trust-ledger-share", "trust.record", "act", { matter: m.id }, "btn small")}</div>
      ${A.ui.table([
        { label: "Date", render: (t) => A.date(t.date) },
        { label: "Entry", render: (t) => h`${kindBadge(t)} ${t.memo}<div class="muted small">${t.kind === "receipt" ? "From" : "To"} ${t.party} · ${t.method}${t.authorizedBy ? ` · authorized by ${A.user(t.authorizedBy).name}` : ""}</div>${statusBadges(t)}` },
        { label: "Ref", render: (t) => h`${t.ref}${t.bankRef && t.bankRef !== t.ref ? h`<div class="muted small">${t.bankRef}</div>` : ""}${isWithdrawal(t) ? h`<div><button class="btn small" data-a="trust-auth" data-id="${t.id}">Authorization</button></div>` : ""}` },
        { label: "Received", num: true, render: (t) => (t.kind === "receipt" ? A.money(t.amount) : "") },
        { label: "Paid out", num: true, render: (t) => (t.kind !== "receipt" ? A.money(t.amount) : "") },
        { label: "Balance", num: true, render: (t) => h`<b>${A.money(t.run)}</b>` },
      ], rows, { empty: "No trust entries on this matter" })}`;
  }
  /* ---------- withdrawals ---------- */
  function invoiceFor(matterId, ref) {
    const inv = A.store.all("invoices").filter((i) => i.matterId === matterId);
    if (!inv.length) return null;
    return inv.find((i) => (ref && (i.number === ref || i.id === ref))) || inv.find((i) => /deliver|sent|issued|approved|paid/i.test(i.status || ""));
  }
  function requestModal(prefill) {
    const ms = trustMatters("trust.request").filter((m) => available(m.id) > 0);
    if (!ms.length) return A.toast("No matter you can act on has funds available to pay out", "bad");
    const pre = typeof prefill === "object" && prefill ? prefill : { matterId: prefill };
    A.modal("Request a trust withdrawal", h`<div class="form">
      <label class="field">Matter<select name="matterId">${ms.map((m) => h`<option value="${m.id}" ${m.id === pre.matterId ? A.raw("selected") : ""}>${m.number} ${m.name} (${A.money(available(m.id))} available)</option>`)}</select></label>
      <label class="field">Purpose<select name="purpose">${[["fee", "Fees on a delivered bill"], ["disbursement", "Pay a disbursement"], ["refund", "Refund to client"], ["third", "Pay a third party on client direction"]].map(([v, l]) => h`<option value="${v}" ${pre.purpose === v ? A.raw("selected") : ""}>${l}</option>`)}</select></label>
      <label class="field">Amount (CAD)<input name="amount" inputmode="decimal" placeholder="0.00" value="${pre.amount || ""}"></label>
      <label class="field">Payee<input name="payee" placeholder="${GENERAL} for fees" value="${pre.payee || ""}"></label>
      <label class="field">Method<select name="method"><option>EFT</option><option>cheque</option><option>bank draft</option><option>wire</option></select></label>
      <label class="field">Bill number or supporting document<input name="ref" placeholder="2026-0112" value="${pre.ref || ""}"></label>
    </div><p class="muted small">Available = cleared funds less protected funds, the matter's minimum balance and requests already waiting. Trust money never leaves in cash. The agent checks the request; a designated signatory other than you authorizes it.</p>`, [
      { label: "Cancel" },
      { label: "Prepare request", primary: true, run: (d) => {
        const v = A.formValues(d); const amount = r2(parseFloat(String(v.amount).replace(/[$,]/g, "")));
        if (!(amount > 0)) { A.toast("Enter an amount", "bad"); return false; }
        if (!v.payee.trim()) { A.toast("Enter the payee", "bad"); return false; }
        prepareWithdrawal({ ...v, amount, payee: v.payee.trim(), ref: v.ref.trim() });
      } },
    ]);
  }
  function prepareWithdrawal(v) {
    const m = A.store.get("matters", v.matterId), me = A.me(), client = A.contact(m.clientId).name;
    A.agent.run({ title: `Check trust withdrawal: ${m.number}`, matterId: m.id, steps: ["Reading the client ledger", "Checking cleared funds", "Matching the invoice and payee", "Preparing the authorization"], done() {
      const clr = available(m.id), inv = v.purpose === "fee" ? invoiceFor(m.id, v.ref) : null, delivered = !!(inv && (inv.sentAt || /sent|deliver|paid/i.test(inv.status || "")));
      const payeeOk = v.purpose === "fee" ? /general/i.test(v.payee) : v.purpose === "refund" ? v.payee.toLowerCase().includes(client.toLowerCase().split(" ")[0]) : !!v.ref;
      const checks = [
        { label: `Available ${A.money(clr)} covers ${A.money(v.amount)} (ledger cannot go below zero, minimum or protected funds)`, ok: clr >= v.amount, blocking: true },
        v.purpose === "fee" ? { label: delivered ? `Bill ${inv.number || inv.id} delivered to the client ${A.date(inv.sentAt || "")}` : inv ? `Bill ${inv.number || inv.id} not yet delivered: fees cannot come out of trust` : "No delivered bill for these fees", ok: delivered, blocking: true } : { label: v.ref ? `Supporting document ${v.ref}` : "No supporting document", ok: !!v.ref, blocking: v.purpose !== "refund" },
        { label: v.purpose === "fee" ? "Payee is the firm's general account" : v.purpose === "refund" ? `Payee matches client (${client})` : "Payee supported by client direction", ok: payeeOk, blocking: true },
        { label: /cash|bearer/i.test(v.method + " " + v.payee) ? "Trust money cannot be paid in cash or to cash/bearer" : `Not cash or bearer (${v.method})`, ok: !/cash|bearer/i.test(v.method + " " + v.payee), blocking: true },
        { label: locked(A.today) ? "Current month is locked" : "Posting month open", ok: !locked(A.today), blocking: true },
      ];
      const kind = v.purpose === "fee" ? "transfer" : "disbursement";
      return A.gate({ kind: "trust-withdrawal", cap: "trust.authorize", matterId: m.id, preparedBy: me.id, effect: "trust.withdraw",
        title: `${kind === "transfer" ? "Fee transfer" : "Trust payment"} ${A.money(v.amount)} from ${m.number}`,
        summary: `${A.money(v.amount)} to ${v.payee} by ${v.method}${v.ref ? ` (${v.ref})` : ""}. Requested by ${me.name}. Ledger balance after: ${A.money(balance(m.id) - v.amount)}.`,
        body: String(h`<dl class="kv"><dt>Client</dt><dd>${client}</dd><dt>Ledger balance</dt><dd>${A.money(balance(m.id))}</dd><dt>Cleared</dt><dd>${A.money(clr)}</dd><dt>Purpose</dt><dd>${v.purpose}</dd><dt>Payee</dt><dd>${v.payee}</dd></dl>`),
        checks, payload: { matterId: m.id, amount: v.amount, payee: v.payee, method: v.method, ref: v.ref, kind, purpose: v.purpose, invoiceId: inv ? inv.id : null, requestedBy: me.id } });
    } });
    A.toast("The agent is checking the request");
  }
  A.effect("trust.withdraw", (p, g) => {
    if (locked(A.today)) { A.audit("trust.withdraw.blocked", { target: g.id, note: "Month locked" }); return A.toast("This month is locked; not posted", "bad"); }
    if (available(p.matterId) < p.amount) { A.audit("trust.withdraw.blocked", { target: g.id, note: "Insufficient cleared funds at authorization" }); return A.toast("Cleared funds no longer cover this; not posted", "bad"); }
    const t = A.store.put("trustTx", { id: A.id("tt"), date: A.today, matterId: p.matterId, kind: p.kind || "disbursement", amount: p.amount, method: p.method, party: p.payee, ref: p.ref || "", memo: p.purpose === "fee" ? "Fees on delivered invoice" : p.purpose === "refund" ? "Refund to client" : "Payment on client's behalf", status: "pending", cleared: false, by: A.me().id, requestedBy: p.requestedBy, authorizedBy: A.me().id, approvalId: g.id });
    A.audit("trust.withdraw.authorized", { target: t.id, note: `${A.money(p.amount)} from ${mlabel(p.matterId)} to ${p.payee}` });
  });

  /* ---------- reconciliation ---------- */
  let reconMonth = null;
  function reconRoute(el) {
    A.setContext({ kind: "trust", label: "Trust reconciliation" });
    if (!firmWide()) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>Reconciliations need firm-wide trust access.</div></div>`);
    const months = [...new Set([ACCOUNT.opened.date.slice(0, 7), ...lines().map((l) => l.date.slice(0, 7)), ...txs().map((t) => t.date.slice(0, 7)), A.today.slice(0, 7)])].filter((m) => m > ACCOUNT.opened.date.slice(0, 7)).sort();
    const mon = reconMonth && months.includes(reconMonth) ? reconMonth : lastClosedMonth();
    const end = monthEnd(mon), tw = threeWay(end), rc = recon(mon), mt = matches();
    const ended = end < A.today, unexplained = tw.unmatched.filter((l) => !l.explained);
    const pendingGate = A.store.where("approvals", (g) => g.effect === "trust.recon" && g.payload && g.payload.month === mon).slice(0, 3);
    const monthLines = lines().filter((l) => l.date.slice(0, 7) === mon);
    const blk = blockers(mon);
    const prepWhy = rc && rc.status === "signed" ? "Already signed off" : !ended ? "Month has not ended" : blk.length ? `Sign off ${monthName(blk[0])} first (reopened or not reconciled)` : !statement(mon) ? "Attach the bank statement first" : unexplained.length ? "Explain every unmatched bank item first" : pendingGate.some((g) => g.status === "pending") ? "Already waiting for sign-off" : "";
    A.render(el, h`<div class="page">
      <div class="page-h"><h1 class="grow">Monthly trust reconciliation ${rc && rc.status === "signed" ? A.ui.status("done", "Signed · locked") : rc && rc.status === "reopened" ? A.ui.status("overdue", "Reopened") : !ended ? A.ui.status("Month in progress") : prepWhy ? A.ui.status(TONE_WORD[A.today > A.addDays(end, 25) ? "bad" : "warn"], A.today > A.addDays(end, 25) ? "Overdue" : "Differences open") : A.ui.status("needs-approval", "Ready to prepare")}</h1>
        ${!statement(mon) && ended ? btn("Attach statement", "trust-statement", "trust.reconcile", "act", { month: mon }) : ""}
        ${btn("✦ Run bank match", "trust-match", "trust.reconcile", "act", {}, "btn agent")}
        ${btn("✦ Prepare reconciliation", "trust-prepare", "trust.reconcile", "act", { month: mon, why: why("trust.reconcile", "act") || prepWhy }, "btn primary")}
        <button class="btn" data-a="go" data-to="trust/package/${mon}">Reconciliation package</button>
        ${rc && rc.status === "signed" ? btn("Reopen month", "trust-reopen", "trust.reconcile", "act", { month: mon }) : ""}
        <div class="sub">${ACCOUNT.name} ${ACCOUNT.number}. Bank, trust journal and client ledgers must agree at each month end; By-Law 9 requires this within 25 days. A signed-off month is locked.</div></div>
      ${subnav("trust/reconcile")}
      ${A.ui.tabs(months.map((m) => ({ id: m, label: monthName(m) + (recon(m) && recon(m).status === "signed" ? " ✓" : recon(m) && recon(m).status === "reopened" ? " (reopened)" : "") })), mon, "trust-month")}
      <div class="grid g2">
        <section class="card"><h2>Three-way at ${A.date(end)}</h2>
          <div class="row wrap" style="margin-bottom:8px">${rc && rc.status === "reopened" ? A.ui.status("overdue", `Reopened ${rc.reopenedAt}: ${rc.reopenReason}`) : ""}${blk.length ? A.ui.status("overdue", `Blocked until ${blk.map(monthName).join(", ")} signed off`) : ""}
            ${statement(mon) ? A.ui.status("done", `Statement attached: ${statement(mon).file}`) : ended ? A.ui.status("due-soon", "No bank statement attached") : ""}${rc && rc.status === "signed" ? A.ui.status("done", `Locked · signed off by ${A.user(rc.signedBy).name} ${rc.signedAt}`) : ended ? A.ui.status(TONE_WORD[A.today > A.addDays(end, 25) ? "bad" : "warn"], `Due ${A.date(A.addDays(end, 25))}`) : A.ui.status("Month in progress")}</div>
          ${threeWayTable(tw)}
          ${tw.negatives.length ? h`<p class="small" style="color:var(--bad)">Negative client ledger: ${tw.negatives.map((x) => mlabel(x.matterId)).join(", ")}</p>` : ""}
        </section>
        <section class="card"><h2>Exceptions</h2>
          ${tw.unmatched.length || tw.negatives.length ? h`${tw.unmatched.map((l) => h`<div class="row" style="padding:6px 0;border-bottom:1px solid var(--line)"><div class="grow"><b>${l.text}</b> ${A.money(l.amount)} <span class="muted small">${A.date(l.date)}</span>
              <div class="muted small">${l.explained ? `Explained by ${A.user(l.explainedBy).name}: ${l.explained}` : "Bank item with no trust entry. A bank charge cannot be paid from client money; ask the bank to reverse it and debit the general account."}</div></div>
              ${l.explained ? A.ui.status("done", "Explained") : btn("Explain", "trust-explain", "trust.reconcile", "act", { id: l.id }, "btn small")}</div>`)}` : A.ui.empty("No exceptions", "Every bank line matches a trust entry.")}
        </section>
      </div>
      <section class="card" style="margin-top:14px"><h2>Bank lines, ${monthName(mon)}</h2>
        ${A.ui.table([{ label: "Date", render: (l) => A.date(l.date) }, { label: "Bank text", key: "text" }, { label: "Amount", num: true, render: (l) => A.money(l.amount) },
          { label: "Matched to", render: (l) => { const t = mt[l.id] && A.store.get("trustTx", mt[l.id]); return t ? h`${A.ui.status(TONE_WORD[l.txId ? "ok" : "agent"], l.txId ? "Matched" : "✦ Suggested")} ${t.ref} · ${mlabel(t.matterId)}` : A.ui.status(TONE_WORD[l.explained ? "warn" : "bad"], l.explained ? "Explained" : "Unmatched"); } },
          { label: "", render: (l) => { const t = mt[l.id]; return h`<div class="row">${t && !l.txId ? btn("Confirm", "trust-confirm-match", "trust.reconcile", "act", { id: l.id }, "btn small") : ""}${!t && l.amount > 0 && !l.explained ? btn("Create receipt", "trust-receipt-from-line", "trust.record", "act", { id: l.id }, "btn small") : ""}${!t && !l.explained ? btn("Mark bank error", "trust-explain", "trust.reconcile", "act", { id: l.id }, "btn small") : ""}</div>`; } }], monthLines, { empty: "No bank lines this month" })}
        <p class="muted small">Bank feed: statement import, last line ${A.date((lines().pop() || {}).date)}. ✦ Suggested matches are the agent's; confirm each, or run the match to confirm all.</p>
      </section>
      ${pendingGate.length ? h`<section style="margin-top:14px">${pendingGate.map((g) => A.ui.gateCard(g))}</section>` : ""}
    </div>`);
  }
  function prepareRecon(mon) {
    const end = monthEnd(mon), me = A.me();
    if (recon(mon) && recon(mon).status === "signed") return A.toast("Already signed off", "bad");
    if (end >= A.today) return A.toast("The month has not ended", "bad");
    if (blockers(mon).length) return A.toast(`Sign off ${monthName(blockers(mon)[0])} first`, "bad");
    if (!statement(mon)) return A.toast("Attach the bank statement first", "bad");
    A.agent.run({ title: `Prepare ${monthName(mon)} trust reconciliation`, steps: ["Loading the bank statement", "Matching bank lines to the journal", "Totalling client ledgers", "Listing reconciling items", "Drafting the reconciliation"], done() {
      persistMatches();
      const tw = threeWay(end), unexplained = tw.unmatched.filter((l) => !l.explained);
      return A.gate({ kind: "trust-reconciliation", cap: "trust.signoff", preparedBy: me.id, effect: "trust.recon",
        title: `${monthName(mon)} trust reconciliation`,
        summary: `Adjusted bank ${A.money(tw.adjusted)}, journal ${A.money(tw.journal)}, client ledgers ${A.money(tw.ledgers)}. Signing off locks ${monthName(mon)} against further posting.`,
        body: String(threeWayTable(tw)),
        checks: [
          { label: "Adjusted bank equals trust journal", ok: tw.variance === 0, blocking: true },
          { label: "Client ledgers total equals trust journal", ok: tw.ledgerVariance === 0, blocking: true },
          { label: "No negative client ledger", ok: !tw.negatives.length, blocking: true },
          { label: unexplained.length ? `${unexplained.length} unexplained bank item` : `All unmatched bank items explained (${tw.unmatched.length})`, ok: !unexplained.length, blocking: true },
          { label: statement(mon) ? `Bank statement attached (${statement(mon).file})` : "Bank statement attached", ok: !!statement(mon), blocking: true },
          { label: "Every earlier month is signed off", ok: !blockers(mon).length, blocking: true },
          { label: `Prepared within 25 days of month end (${A.date(A.addDays(end, 25))})`, ok: A.today <= A.addDays(end, 25), blocking: false },
        ],
        payload: { month: mon, bank: tw.bank, adjusted: tw.adjusted, journal: tw.journal, ledgers: tw.ledgers, preparedBy: me.id } });
    } });
    A.toast("The agent is preparing the reconciliation");
  }
  function persistMatches() { const mt = matches(); lines().forEach((l) => { if (mt[l.id] && !l.txId) A.store.put("bankLines", { id: l.id, txId: mt[l.id] }); }); return mt; }
  A.effect("trust.recon", (p, g) => {
    A.store.put("trustRecons", { id: "rc-" + p.month, month: p.month, status: "signed", signedBy: A.me().id, signedAt: A.now(), bank: p.bank, adjusted: p.adjusted, journal: p.journal, ledgers: p.ledgers, preparedBy: p.preparedBy, approvalId: g.id });
    A.audit("trust.recon.signed", { target: "rc-" + p.month, note: `${monthName(p.month)} signed off and locked` });
  });

  /* ---------- receipts, clearing, execution ---------- */
  function receiptModal(prefill) {
    const ms = trustMatters("trust.record");
    if (!ms.length) return A.toast("No matters available", "bad");
    A.modal("Record a trust receipt", h`<div class="form">
      <label class="field">Matter<select name="matterId">${ms.map((m) => h`<option value="${m.id}" ${m.id === prefill ? A.raw("selected") : ""}>${m.number} ${m.name}</option>`)}</select></label>
      <label class="field">Date received<input name="date" type="date" value="${A.today}"></label>
      <label class="field">Amount<input name="amount" inputmode="decimal" placeholder="0.00"></label>
      <label class="field">From<input name="party"></label>
      <label class="field">Method<select name="method"><option>cheque</option><option>e-transfer</option><option>wire</option><option>bank draft</option><option>cash</option></select></label>
      <label class="field">Reference<input name="ref"></label>
      <label class="field">Purpose<input name="memo" placeholder="Retainer"></label>
      <label class="field"><span><input type="checkbox" name="cashException"> Cash exception applies (from a financial institution, public body, or for bail)</span></label>
    </div><p class="muted small">Cash on one client file may not reach ${A.money(CASH_LIMIT)} in total unless an exception applies (By-Law 9). Wires and cash are treated as cleared on receipt.</p>`, [{ label: "Cancel" }, { label: "Record", primary: true, run: (d) => {
      const v = A.formValues(d), amount = r2(parseFloat(String(v.amount).replace(/[$,]/g, "")));
      if (!(amount > 0) || !v.party.trim()) { A.toast("Enter the amount and who paid", "bad"); return false; }
      if (locked(v.date)) { A.toast(`${monthName(v.date.slice(0, 7))} is reconciled and locked`, "bad"); return false; }
      if (v.date > A.today) { A.toast("Receipt date cannot be in the future", "bad"); return false; }
      if (v.method === "cash" && cashOnFile(v.matterId) + amount >= CASH_LIMIT && !v.cashException) { A.toast(`Cash on this file would total ${A.money(cashOnFile(v.matterId) + amount)}; ${A.money(CASH_LIMIT)} or more needs a recorded exception`, "bad"); return false; }
      const t = A.store.put("trustTx", { id: A.id("tt"), date: v.date, matterId: v.matterId, kind: "receipt", amount, method: v.method, party: v.party.trim(), ref: v.ref, memo: v.memo || "Funds received", status: "posted", cleared: v.method === "wire" || v.method === "cash", by: A.me().id, ...(v.cashException && v.method === "cash" ? { cashException: true } : {}) });
      if (v.cashException && v.method === "cash") A.audit("trust.cash.exception", { target: t.id, note: `Cash exception recorded on ${mlabel(v.matterId)}` });
      A.audit("trust.receipt", { target: t.id, note: `${A.money(amount)} into ${mlabel(v.matterId)}` });
      A.toast("Receipt recorded", "ok");
    } }]);
  }
  A.action("trust-receipt", (el) => { if (!A.can("trust.record", "act")) return A.toast(why("trust.record", "act"), "bad"); receiptModal(el.dataset.matter); });
  A.action("trust-request", (el) => { if (!A.can("trust.request", "act")) return A.toast(why("trust.request", "act"), "bad"); requestModal(el.dataset.matter); });
  A.action("trust-ledger", (el) => A.go(`trust/ledger/${el.dataset.id}`));
  A.action("trust-refund", (el) => { if (!A.can("trust.request", "act")) return A.toast(why("trust.request", "act"), "bad"); const m = tmatter(el.dataset.matter); if (!m) return; requestModal({ matterId: m.id, purpose: "refund", amount: Math.max(0, cleared(m.id) - (setting(m.id).protected || 0)).toFixed(2), payee: A.contact(m.clientId).name, ref: "Final account, balance refund" }); });
  A.action("trust-clear", (el) => {
    if (!A.can("trust.record", "act")) return;
    const t = A.store.get("trustTx", el.dataset.id); if (!t) return;
    A.store.put("trustTx", { id: t.id, cleared: true, clearedAt: A.now() }); A.audit("trust.cleared", { target: t.id, note: `${t.ref} ${A.money(t.amount)}` });
  });
  A.action("trust-execute", (el) => {
    if (!A.can("trust.execute", "act")) return A.toast(why("trust.execute", "act"), "bad");
    const t = A.store.get("trustTx", el.dataset.id); if (!t || t.status !== "pending") return;
    A.modal(`Mark sent: ${A.money(t.amount)} to ${t.party}`, h`<label class="field">Bank confirmation or cheque number<input name="bankRef" class="input"></label><p class="muted small">Authorized by ${A.user(t.authorizedBy).name}. Record only after the payment has been released at the bank.</p>`, [{ label: "Cancel" }, { label: "Mark sent", primary: true, run: (d) => {
      const ref = d.querySelector("[name=bankRef]").value.trim(); if (!ref) { A.toast("Enter the bank reference", "bad"); return false; }
      A.store.put("trustTx", { id: t.id, status: "posted", bankRef: ref, sentAt: A.now(), sentBy: A.me().id });
      A.audit("trust.withdraw.sent", { target: t.id, note: `${A.money(t.amount)} sent, ref ${ref}` }); A.toast("Marked sent", "ok");
    } }]);
  });
  A.action("trust-confirm-match", (el) => {
    if (!A.can("trust.reconcile", "act")) return A.toast(why("trust.reconcile", "act"), "bad");
    const l = A.store.get("bankLines", el.dataset.id), t = l && matches()[l.id]; if (!t) return;
    A.store.put("bankLines", { id: l.id, txId: t }); A.audit("trust.match.confirmed", { target: l.id, note: `${l.text} → ${(A.store.get("trustTx", t) || {}).ref}` });
  });
  A.action("trust-receipt-from-line", (el) => {
    if (!A.can("trust.record", "act")) return A.toast(why("trust.record", "act"), "bad");
    const l = A.store.get("bankLines", el.dataset.id); if (!l) return; receiptModal(null);
    const d = document.getElementById("modal"); const set = (n, v) => { const f = d && d.querySelector(`[name=${n}]`); if (f) f.value = v; };
    set("amount", l.amount.toFixed(2)); set("date", l.date); set("ref", l.text); set("method", /WIRE/.test(l.text) ? "wire" : /E-TRF|INTERAC/.test(l.text) ? "e-transfer" : "cheque");
  });
  A.action("trust-month", (el) => { reconMonth = el.dataset.id; A.refresh(); });
  A.action("trust-match", () => {
    if (!A.can("trust.reconcile", "act")) return;
    A.agent.run({ title: "Match bank lines to the trust journal", steps: ["Reading the bank feed", "Matching by amount, date and reference"], done() { const mt = persistMatches(); const un = lines().filter((l) => !mt[l.id]).length; A.audit("trust.match", { actor: "agent", note: `${Object.keys(mt).length} matched, ${un} unmatched` }); return { title: `${Object.keys(mt).length} matched, ${un} unmatched` }; } });
  });
  A.action("trust-prepare", (el) => { if (A.can("trust.reconcile", "act")) prepareRecon(el.dataset.month); });
  A.action("trust-explain", (el) => {
    if (!A.can("trust.reconcile", "act")) return;
    const l = A.store.get("bankLines", el.dataset.id); if (!l) return;
    A.modal(`Explain: ${l.text} ${A.money(l.amount)}`, h`<label class="field">Explanation for the reconciliation<textarea name="note" rows="3" class="input">Bank charge debited to trust in error. Bank asked to reverse and charge the general account.</textarea></label>`, [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
      const note = d.querySelector("[name=note]").value.trim(); if (!note) { A.toast("Enter an explanation", "bad"); return false; }
      A.store.put("bankLines", { id: l.id, explained: note, explainedBy: A.me().id }); A.audit("trust.exception.explained", { target: l.id, note });
    } }]);
  });

  /* ---------- reopen (chained), statements, package ---------- */
  A.action("trust-reopen", (el) => {
    if (!A.can("trust.reconcile", "act")) return A.toast(why("trust.reconcile", "act"), "bad");
    const mon = el.dataset.month, later = laterSigned(mon);
    A.modal(`Reopen ${monthName(mon)}`, h`<p>Opening balances chain month to month, so reopening ${monthName(mon)} also reopens ${later.length ? later.map(monthName).join(", ") : "no later month"}. Each must be reconciled and signed off again, in order.</p>
      <label class="field">Reason<textarea name="reason" rows="2" class="input">Late-cleared cheque posted to the wrong client ledger; correct and re-sign.</textarea></label><p class="muted small">A licensee with sign-off authority, other than you, must approve.</p>`,
    [{ label: "Cancel" }, { label: "Request reopen", primary: true, run: (d) => {
      const reason = d.querySelector("[name=reason]").value.trim(); if (!reason) { A.toast("Enter a reason", "bad"); return false; }
      if (A.store.where("approvals", (g) => g.status === "pending" && g.effect === "trust.reopen" && g.payload.month === mon).length) { A.toast("A reopen request is already waiting"); return; }
      A.gate({ kind: "trust-reopen", cap: "trust.signoff", preparedBy: A.me().id, effect: "trust.reopen", title: `Reopen ${monthName(mon)} trust reconciliation`,
        summary: `${reason} Also reopens: ${later.length ? later.map(monthName).join(", ") : "none"}.`, checks: [{ label: "Reason given", ok: true }, { label: `${later.length} later month(s) reopened with it`, ok: true }],
        payload: { month: mon, reason, later } });
      A.toast("Reopen request sent for approval");
    } }]);
  });
  A.effect("trust.reopen", (p) => {
    [p.month, ...laterSigned(p.month)].forEach((m) => {
      if (!recon(m)) return;
      A.store.put("trustRecons", { id: "rc-" + m, status: "reopened", reopenedAt: A.now(), reopenedBy: A.me().id, reopenReason: m === p.month ? p.reason : `Chained: ${monthName(p.month)} reopened` });
      A.audit("trust.recon.reopened", { target: "rc-" + m, note: `${monthName(m)} reopened${m === p.month ? `: ${p.reason}` : ` with ${monthName(p.month)}`}` });
    });
  });
  A.action("trust-statement", (el) => {
    if (!A.can("trust.reconcile", "act")) return A.toast(why("trust.reconcile", "act"), "bad");
    const mon = el.dataset.month, closing = r2(ACCOUNT.opened.balance + lines().filter((l) => l.date <= monthEnd(mon)).reduce((s, l) => s + l.amount, 0));
    A.store.put("trustStatements", { id: "st-" + mon, month: mon, file: `Trust statement ${mon}.pdf`, pages: 2, closing, by: A.me().id, at: A.now() });
    A.audit("trust.statement.attached", { target: "st-" + mon, note: `${monthName(mon)} bank statement, closing ${A.money(closing)}` }); A.toast("Bank statement attached", "ok");
  });
  function packageData(mon) {
    const end = monthEnd(mon), start = mon + "-01", prevEnd = A.addDays(start, -1), tw = threeWay(end), mt = matches(), matched = new Set(Object.values(mt));
    const tx = txs().filter((t) => t.date >= start && t.date <= end);
    const ids = [...new Set(txs().filter((t) => t.date <= end).map((t) => t.matterId))];
    const effect = ids.map((id) => { const mine = tx.filter((t) => t.matterId === id); return { id, open: balance(id, prevEnd), inn: r2(mine.filter((t) => t.kind === "receipt").reduce((s, t) => s + t.amount, 0)), out: r2(mine.filter((t) => t.kind !== "receipt").reduce((s, t) => s + t.amount, 0)), close: balance(id, end) }; }).filter((r) => r.open || r.inn || r.out || r.close);
    const charges = lines().filter((l) => l.date >= start && l.date <= end && !mt[l.id]);
    return { end, tw, tx, matched, effect, charges, st: statement(mon), rc: recon(mon) };
  }
  function packageRoute(el, p) {
    A.setContext({ kind: "trust", label: "Reconciliation package" });
    if (!firmWide()) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>Reconciliation packages need firm-wide trust access.</div></div>`);
    const mon = /^\d{4}-\d{2}$/.test(p.month) ? p.month : lastClosedMonth(), d = packageData(mon), ledgersVisible = d.effect.filter((r) => tmatter(r.id));
    A.render(el, h`<div class="page">
      ${subnav("trust/reconcile")}<div class="page-h"><h1 class="grow">Reconciliation package · ${monthName(mon)}</h1><button class="btn" data-a="trust-goto-recon" data-month="${mon}">Back to reconciliation</button>
        <button class="btn primary" data-a="trust-export" data-month="${mon}">Export package</button>
        <div class="sub">${ACCOUNT.name} ${ACCOUNT.number} · period ${A.date(mon + "-01")} to ${A.date(d.end)} · ${d.rc && d.rc.status === "signed" ? `signed off by ${A.user(d.rc.signedBy).name} ${d.rc.signedAt}` : d.rc && d.rc.status === "reopened" ? "reopened, not signed" : "not signed off"}. Keep for the By-Law 9 retention period (verify).</div></div>
      <div class="grid g2">
        <section class="card"><h2>1. Bank statement</h2>${d.st ? h`<dl class="kv"><dt>File</dt><dd>${d.st.file} · ${d.st.pages} page(s)</dd><dt>Closing balance</dt><dd>${A.money(d.st.closing)}</dd><dt>Attached</dt><dd>${A.user(d.st.by).name} · ${d.st.at}</dd>
            <dt>Agrees to bank feed</dt><dd>${r2(d.st.closing) === d.tw.bank ? A.ui.status("done", "Yes") : A.ui.status("overdue", `No: feed ${A.money(d.tw.bank)}`)}</dd></dl>`
          : h`${A.ui.empty("No statement attached", " Sign-off is blocked until the bank's statement is attached.")}${btn("Attach statement", "trust-statement", "trust.reconcile", "act", { month: mon })}`}</section>
        <section class="card"><h2>2. Summary</h2>${threeWayTable(d.tw)}</section>
      </div>
      <section class="card" style="margin-top:14px"><h2>3. Transaction detail</h2>
        ${A.ui.table([{ label: "Date", render: (t) => A.date(t.date) }, { label: "Matter", render: (t) => mlabel(t.matterId) }, { label: "Type", render: kindBadge }, { label: "Party", key: "party" }, { label: "Ref", key: "ref" },
          { label: "In", num: true, render: (t) => (t.kind === "receipt" ? A.money(t.amount) : "") }, { label: "Out", num: true, render: (t) => (t.kind !== "receipt" ? A.money(t.amount) : "") },
          { label: "Bank", render: (t) => (d.matched.has(t.id) ? A.ui.status("done", "Cleared") : t.kind === "receipt" ? A.ui.status("due-soon", "In transit") : A.ui.status("due-soon", "Outstanding")) }], d.tx, { empty: "No trust entries this month" })}
        ${d.charges.length ? h`<h3 style="margin-top:12px">Charges and adjustments on the statement</h3>${A.ui.table([{ label: "Date", render: (l) => A.date(l.date) }, { label: "Bank text", key: "text" }, { label: "Amount", num: true, render: (l) => A.money(l.amount) }, { label: "Explanation", render: (l) => l.explained || A.ui.status("overdue", "Unexplained") }], d.charges)}` : ""}
      </section>
      <section class="card" style="margin-top:14px"><h2>4. Effect on each client ledger</h2>
        ${A.ui.table([{ label: "Client / matter", render: (r) => h`${A.contact(A.store.get("matters", r.id).clientId).name}<div class="muted small">${mlabel(r.id)}</div>` }, { label: "Opening", num: true, render: (r) => A.money(r.open) },
          { label: "Received", num: true, render: (r) => A.money(r.inn) }, { label: "Paid out", num: true, render: (r) => A.money(r.out) }, { label: "Closing", num: true, render: (r) => h`<b>${A.money(r.close)}</b>${r.close < 0 ? A.ui.status("overdue", "Negative") : ""}` }], ledgersVisible)}
      </section></div>`);
  }
  A.action("trust-goto-recon", (el) => { reconMonth = el.dataset.month; A.go("trust/reconcile"); });
  A.action("trust-export", (el) => {
    const mon = el.dataset.month, d = packageData(mon);
    const txt = [`TRUST RECONCILIATION PACKAGE,${ACCOUNT.name} ${ACCOUNT.number},${mon}`, `Statement,${d.st ? d.st.file : "MISSING"},${d.st ? d.st.closing.toFixed(2) : ""}`,
      `Summary,bank ${d.tw.bank.toFixed(2)},adjusted ${d.tw.adjusted.toFixed(2)},journal ${d.tw.journal.toFixed(2)},ledgers ${d.tw.ledgers.toFixed(2)}`,
      "DETAIL,date,matter,type,party,ref,amount,bank", ...d.tx.map((t) => ["", t.date, mlabel(t.matterId), t.kind, t.party, t.ref, sign(t).toFixed(2), d.matched.has(t.id) ? "cleared" : "uncleared"].join(",")),
      "LEDGERS,matter,opening,received,paid out,closing", ...d.effect.map((r) => ["", mlabel(r.id), r.open.toFixed(2), r.inn.toFixed(2), r.out.toFixed(2), r.close.toFixed(2)].join(","))].join("\n");
    A.audit("trust.package.exported", { target: "rc-" + mon, note: `${monthName(mon)} reconciliation package exported` });
    A.toast(`Package for ${monthName(mon)} exported (PDF + CSV)`, "ok");
    A.modal(`Export · ${monthName(mon)}`, h`<p class="muted small">PDF for the file, CSV for the accountant. Preview of the CSV:</p><pre style="white-space:pre;overflow:auto;max-height:320px;font-size:11px">${txt}</pre>`);
  });

  /* ---------- client-level balances ---------- */
  function clientRows() {
    const by = {};
    trustMatters().forEach((m) => { const b = balance(m.id); if (!b && !txs().some((t) => t.matterId === m.id)) return; const r = (by[m.clientId] = by[m.clientId] || { id: m.clientId, ms: [], bal: 0, clr: 0 }); r.ms.push(m); r.bal = r2(r.bal + b); r.clr = r2(r.clr + cleared(m.id)); });
    return Object.values(by).sort((a, b) => b.bal - a.bal);
  }
  function clientRoute(el, p) {
    const r = clientRows().find((x) => x.id === p.clientId);
    if (!r) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>No trust ledger you can see for this client.</div></div>`);
    A.setContext({ kind: "trust", label: `${A.contact(r.id).name} · client trust` });
    let run = 0; const ids = new Set(r.ms.map((m) => m.id));
    const rows = txs().filter((t) => ids.has(t.matterId)).map((t) => ({ ...t, run: (run = r2(run + sign(t))) })).reverse();
    A.render(el, h`<div class="page">${A.ui.header({ title: `Client trust ledger · ${A.contact(r.id).name}`, back: { label: "Ledgers", to: "trust/ledgers" }, sub: "All matters for this client rolled up. Funds stay allocated to a matter; moving money between matters is a transfer that needs authorization." })}
      <div class="stats">${A.ui.stat("Client balance", A.money(r.bal), `${r.ms.length} matter(s)`)}${A.ui.stat("Cleared", A.money(r.clr), "", r.clr !== r.bal ? "gate" : "")}</div>
      <div class="card"><h2>By matter</h2>${A.ui.table([{ label: "Matter", render: (m) => h`<b>${m.number}</b> ${m.name}` }, { label: "Balance", num: true, render: (m) => A.money(balance(m.id)) }, { label: "Cleared", num: true, render: (m) => A.money(cleared(m.id)) }], r.ms, { onRow: "trust-ledger" })}</div>
      <div class="card" style="margin-top:14px"><h2>Combined ledger</h2>${A.ui.table([{ label: "Date", render: (t) => A.date(t.date) }, { label: "Matter", render: (t) => A.store.get("matters", t.matterId).number }, { label: "Entry", render: (t) => h`${kindBadge(t)} ${t.memo}` },
        { label: "Received", num: true, render: (t) => (t.kind === "receipt" ? A.money(t.amount) : "") }, { label: "Paid out", num: true, render: (t) => (t.kind !== "receipt" ? A.money(t.amount) : "") }, { label: "Balance", num: true, render: (t) => h`<b>${A.money(t.run)}</b>` }], rows)}</div></div>`);
  }
  A.action("trust-client", (el) => A.go(`trust/client/${el.dataset.id}`));

  /* ---------- cheques and withdrawal authorizations ---------- */
  const ONES = ["", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"];
  const TENS = ["", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"];
  const under1000 = (n) => { const hd = Math.floor(n / 100), r = n % 100; return [hd ? ONES[hd] + " hundred" : "", r < 20 ? ONES[r] : TENS[Math.floor(r / 10)] + (r % 10 ? "-" + ONES[r % 10] : "")].filter(Boolean).join(" "); };
  const words = (amt) => { const d = Math.floor(amt), c = Math.round((amt - d) * 100); const parts = []; const m = Math.floor(d / 1e6), k = Math.floor((d % 1e6) / 1000), u = d % 1000;
    if (m) parts.push(under1000(m) + " million"); if (k) parts.push(under1000(k) + " thousand"); if (u || !parts.length) parts.push(u ? under1000(u) : "zero");
    const s = parts.join(" "); return `${s.charAt(0).toUpperCase() + s.slice(1)} and ${String(c).padStart(2, "0")}/100 dollars`; };
  const isWithdrawal = (t) => t && t.kind !== "receipt" && t.authorizedBy;
  A.action("trust-cheque", (el) => {
    const t = A.store.get("trustTx", el.dataset.id); if (!isWithdrawal(t) || !tmatter(t.matterId)) return;
    if (!A.can("trust.execute", "act")) return A.toast(why("trust.execute", "act"), "bad");
    const m = A.store.get("matters", t.matterId), no = (t.ref.match(/\d+/) || ["—"])[0];
    A.modal(`Cheque ${no} · print preview`, h`<div style="border:1px solid var(--line);border-radius:6px;padding:14px;font-family:ui-monospace,monospace;font-size:12.5px">
      <div class="row"><b class="grow">Linda K Law · TRUST ACCOUNT</b><span>No. ${no}</span></div>
      <div class="muted small">${ACCOUNT.bank}</div>
      <div class="row" style="margin-top:10px"><span class="grow"></span><span>Date ${t.date.replace(/-/g, "")} (YYYYMMDD)</span></div>
      <div style="margin-top:10px">Pay to the order of <b>${t.party}</b><span style="float:right">**${A.money(t.amount)}**</span></div>
      <div style="margin-top:6px;border-bottom:1px solid var(--line)">${words(t.amount)} ********</div>
      <div class="row" style="margin-top:14px"><span class="grow">Re: ${m.number} ${A.contact(m.clientId).name} · ${t.memo}</span><span style="border-top:1px solid var(--line);padding-top:2px;min-width:180px;text-align:center">${A.user(t.authorizedBy).name}, authorized signatory</span></div>
      <div class="muted" style="margin-top:10px">⑈${no}⑈ ⑆01234⑉004⑆ 4471⑈</div></div>
      <p class="muted small">Authorized by ${A.user(t.authorizedBy).name}. Print on the firm's trust cheque stock; void and record any spoiled cheque.</p>`,
    [{ label: "Close" }, { label: "Print", primary: true, run: () => { A.audit("trust.cheque.printed", { target: t.id, note: `Cheque ${no} ${A.money(t.amount)} to ${t.party}` }); A.toast(`Cheque ${no} sent to printer`, "ok"); } }]);
  });
  function authRoute(el, p) {
    const t = A.store.get("trustTx", p.id), m = t && tmatter(t.matterId);
    if (!isWithdrawal(t) || !m) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>No authorized withdrawal you can see with this reference.</div></div>`);
    A.setContext({ kind: "trust", label: "Withdrawal authorization" });
    const tele = t.method !== "cheque";
    A.render(el, h`<div class="page"><div class="page-h"><h1 class="grow">Authorization of trust withdrawal</h1><button class="btn" data-a="trust-ledger" data-id="${m.id}">Client ledger</button>
        ${t.method === "cheque" ? btn("Print cheque", "trust-cheque", "trust.execute", "act", { id: t.id }) : ""}
        <div class="sub">Laid out after Ontario Form 9A, authorization of withdrawal by electronic transfer (verify against the current form). Kept with the trust records.</div></div>
      <div class="card" style="max-width:760px"><h2>${tele ? "Electronic transfer" : "Cheque"} from ${ACCOUNT.name} ${ACCOUNT.number}</h2>
        <dl class="kv">
          <dt>1. Amount</dt><dd>${A.money(t.amount)} <span class="muted small">(${words(t.amount)})</span></dd>
          <dt>2. Name of client</dt><dd>${A.contact(m.clientId).name}</dd>
          <dt>3. File / matter</dt><dd>${m.number} ${m.name}</dd>
          <dt>4. Reason for payment</dt><dd>${t.memo}</dd>
          <dt>5. Payee</dt><dd>${t.party}</dd>
          <dt>6. Bank / account of payee</dt><dd>${/general/i.test(t.party) ? "Firm general account (same bank)" : t.method === "cheque" ? "Paid by cheque" : "Per payee's written banking instructions on file"}</dd>
          <dt>7. Method · reference</dt><dd>${t.method} · ${t.ref || "—"}${t.bankRef ? ` · bank ${t.bankRef}` : ""}</dd>
          <dt>8. Requested by</dt><dd>${A.user(t.requestedBy || t.by).name}</dd>
          <dt>9. Authorized by (licensee)</dt><dd>${A.user(t.authorizedBy).name} · signatory ${A.user(t.authorizedBy).signatory ? A.ui.status("done", "designated") : A.ui.status("overdue", "not designated")}</dd>
          <dt>10. Date authorized</dt><dd>${A.date(t.date)}</dd>
          <dt>11. Status</dt><dd>${t.status === "pending" ? A.ui.status("needs-approval", "Authorized, not yet sent") : A.ui.status("done", `Sent${t.sentAt ? " " + t.sentAt : ""}`)}</dd>
          ${tele ? h`<dt>12. Confirmation</dt><dd>${t.bankRef ? `Bank confirmation ${t.bankRef}; printed confirmation to be attached within 5 days (verify)` : "Awaiting bank confirmation"}</dd>` : ""}
        </dl>
        <p class="muted small" style="margin-top:12px">The person who requested the withdrawal did not authorize it. The approval record and audit trail are attached to this entry.</p></div></div>`);
  }
  A.action("trust-auth", (el) => A.go(`trust/authorization/${el.dataset.id}`));

  /* ---------- separate interest-bearing accounts and interest allocation (amounts in integer cents) ---------- */
  /* A mixed account's interest goes to the Law Foundation of Ontario (Law Society Act s.57, verify); a separate
     interest-bearing account holds one client's money and its interest belongs to that client. */
  A.seed("trustSepAccounts", [
    { id: "sa-1", matterId: "m4", title: "Linda K Law in trust for the Estate of Margaret Lindqvist", bank: "Chartered bank, Toronto main branch", number: "••••5530", opened: "2026-08-26", rateBp: 310, status: "active", openedBy: "u-linda" },
  ]);
  A.seed("trustSepTx", [
    { id: "sx-1", accountId: "sa-1", date: "2026-08-26", kind: "deposit", cents: 4000000, memo: "Estate funds placed in a separate interest-bearing account on the estate trustee's direction", by: "u-yohannes" },
    { id: "sx-2", accountId: "sa-1", date: "2026-08-31", kind: "interest", cents: 2038, memo: "Interest August 2026 (allocated)", by: "u-yohannes", allocation: "ia-2026-08-sa-1" },
  ]);
  A.seed("trustInterestBank", [
    { id: "ib-2026-08-sa-1", accountId: "sa-1", month: "2026-08", cents: 2038 },
    { id: "ib-2026-09-sa-1", accountId: "sa-1", month: "2026-09", cents: 10190 },
  ]);
  A.seed("trustInterestAlloc", [
    { id: "ia-2026-08-sa-1", accountId: "sa-1", month: "2026-08", computed: 2038, bank: 2038, status: "approved", approvedBy: "u-linda", at: "2026-09-03 10:20" },
  ]);
  const sepAccounts = () => A.store.all("trustSepAccounts").filter((a) => tmatter(a.matterId));
  const sepTx = (accId, upto = "9999-12-31") => A.store.where("trustSepTx", (t) => t.accountId === accId && t.date <= upto).sort((a, b) => a.date.localeCompare(b.date));
  const sepBalance = (accId, upto) => sepTx(accId, upto).reduce((s, t) => s + (t.kind === "withdrawal" ? -t.cents : t.cents), 0);
  /** Interest from daily closing balances for one month: sum(balance_day x rate / 365), rounded to the cent at month end. */
  function dailyInterest(acc, month) {
    const end = monthEnd(month); let d = month + "-01", sum = 0;
    while (d <= end) { if (d >= acc.opened) sum += sepTx(acc.id, d).filter((t) => t.kind !== "interest" || t.date < d).reduce((s, t) => s + (t.kind === "withdrawal" ? -t.cents : t.cents), 0) * (acc.rateBp / 10000) / 365; d = A.addDays(d, 1); }
    return Math.round(sum);
  }
  const bankInterest = (accId, month) => (A.store.get("trustInterestBank", `ib-${month}-${accId}`) || {}).cents;
  const allocation = (accId, month) => A.store.get("trustInterestAlloc", `ia-${month}-${accId}`);
  let interestAcct = "sa-1", interestMonth = "2026-09";
  A.change("trust-int-acct", (el) => { interestAcct = el.value; A.refresh(); });
  A.change("trust-int-month", (el) => { interestMonth = el.value; A.refresh(); });
  function interestRoute(el) {
    A.setContext({ kind: "trust", label: "Trust interest allocation" });
    if (!firmWide()) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>Interest allocation needs firm-wide trust access.</div></div>`);
    const accs = sepAccounts(), months = ["2026-08", "2026-09"].filter((m) => m <= lastClosedMonth());
    const acc = accs.find((a) => a.id === interestAcct);
    const rows = accs.flatMap((a) => months.filter((m) => monthEnd(m) >= a.opened).map((m) => {
      const al = allocation(a.id, m), comp = al ? al.computed : dailyInterest(a, m), bank = bankInterest(a.id, m);
      return { id: `${m}|${a.id}`, a, m, comp, bank, diff: bank == null ? null : bank - comp, al };
    })).filter((r) => interestAcct === "all" || r.a.id === interestAcct);
    const st = (r) => r.al && r.al.status === "approved" ? A.ui.status("done", "Posted to ledger") : r.al && r.al.status === "pending" ? A.ui.status("needs-approval") : r.bank == null ? A.ui.status("waiting", "No bank figure yet") : A.ui.status("draft", "Not allocated");
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Trust interest", sub: "Interest earned on separate interest-bearing trust accounts, computed from daily balances and allocated to the client it belongs to. A lawyer approves each allocation before it posts.",
        actions: [A.ui.btn({ label: "Open separate account", a: "trust-sep-open", disabled: !A.can("trust.record", "act"), why: why("trust.record", "act") })],
        primary: { label: "✦ Compute allocation", a: "trust-int-compute", key: "n", agent: true, disabled: !A.can("trust.reconcile", "act"), why: why("trust.reconcile", "act") } })}
      ${subnav("trust/interest")}
      ${A.ui.filters({ selects: [{ c: "trust-int-acct", label: "Account", value: interestAcct, options: [["all", "All separate accounts"], ...accs.map((a) => [a.id, `${a.number} · ${(tmatter(a.matterId) || {}).number}`]), ["mixed", `${ACCOUNT.name} ${ACCOUNT.number}`]] },
        { c: "trust-int-month", label: "Month", value: interestMonth, options: months.map((m) => [m, monthName(m)]) }] })}
      ${interestAcct === "mixed" ? A.ui.empty("Mixed account: no allocation", "Interest on the pooled (mixed) trust account is paid to the Law Foundation of Ontario, not to clients (verify). Pick a separate interest-bearing account.")
        : h`<section class="card">${A.ui.table([
          { label: "Month", render: (r) => monthName(r.m) },
          { label: "Client · matter", render: (r) => h`<b>${A.contact(A.store.get("matters", r.a.matterId).clientId).name}</b><div class="muted small">${mlabel(r.a.matterId)} · ${r.a.number} at ${(r.a.rateBp / 100).toFixed(2)}%</div>` },
          { label: "Computed (daily balances)", num: true, render: (r) => A.cents(r.comp) },
          { label: "Bank credited", num: true, render: (r) => (r.bank == null ? "—" : A.cents(r.bank)) },
          { label: "Difference", num: true, render: (r) => (r.diff == null ? "—" : r.diff ? h`${A.cents(r.diff)} ${A.ui.status("due-soon", "Check")}` : A.cents(0)) },
          { label: "Status", render: st },
        ], rows, { active: `${interestMonth}|${acc ? acc.id : ""}`, empty: "No separate interest-bearing accounts open" })}
        <p class="muted small">The posted amount is the bank's figure; the agent flags any difference from its own daily-balance calculation. Residual cents still need a disposition.</p></section>`}
      <section class="card" style="margin-top:14px"><h2>Separate interest-bearing accounts</h2>${A.ui.table([
        { label: "Account", render: (a) => h`<b>${a.number}</b><div class="muted small">${a.title}</div>` }, { label: "Matter", render: (a) => mlabel(a.matterId) },
        { label: "Rate", num: true, render: (a) => `${(a.rateBp / 100).toFixed(2)}%` }, { label: "Opened", render: (a) => A.date(a.opened) },
        { label: "Balance", num: true, render: (a) => A.cents(sepBalance(a.id)) }, { label: "Status", render: (a) => A.ui.status(a.status) }], A.store.all("trustSepAccounts").filter((a) => tmatter(a.matterId)), { empty: "None open" })}</section>
    </div>`);
  }
  A.action("trust-int-compute", () => {
    if (!A.can("trust.reconcile", "act")) return A.toast(why("trust.reconcile", "act"), "bad");
    if (interestAcct === "mixed") return A.toast("Mixed account interest belongs to the Law Foundation of Ontario; nothing to allocate", "bad");
    const accs = sepAccounts().filter((a) => interestAcct === "all" || a.id === interestAcct), mon = interestMonth;
    const todo = accs.filter((a) => !allocation(a.id, mon) && bankInterest(a.id, mon) != null);
    if (!todo.length) return A.toast(`${monthName(mon)}: nothing left to allocate`);
    A.agent.run({ title: `Compute trust interest: ${monthName(mon)}`, steps: ["Reading daily balances", "Applying each account's rate", "Comparing with the bank's credit"], done() {
      const lines = todo.map((a) => ({ a, comp: dailyInterest(a, mon), bank: bankInterest(a.id, mon) }));
      lines.forEach((l) => A.store.put("trustInterestAlloc", { id: `ia-${mon}-${l.a.id}`, accountId: l.a.id, month: mon, computed: l.comp, bank: l.bank, status: "pending" }));
      return A.gate({ kind: "trust-interest", cap: "trust.signoff", preparedBy: "agent", effect: "trust.interest", title: `Allocate ${monthName(mon)} trust interest`,
        summary: lines.map((l) => `${A.contact(A.store.get("matters", l.a.matterId).clientId).name}: bank ${A.cents(l.bank)}, computed ${A.cents(l.comp)}`).join("; "),
        checks: [{ label: "Separate interest-bearing accounts only (no mixed account)", ok: true }, ...lines.map((l) => ({ label: `${l.a.number}: bank and computed agree within $1.00`, ok: Math.abs(l.bank - l.comp) <= 100 }))],
        payload: { month: mon, ids: lines.map((l) => l.a.id) } });
    } });
  });
  A.effect("trust.interest", (p, g) => {
    p.ids.forEach((accId) => { const al = allocation(accId, p.month); if (!al) return;
      A.store.put("trustSepTx", { id: A.id("sx"), accountId: accId, date: monthEnd(p.month), kind: "interest", cents: al.bank, memo: `Interest ${monthName(p.month)} (allocated)`, by: g && g.decidedBy || A.me().id, allocation: al.id });
      A.store.put("trustInterestAlloc", { id: al.id, status: "approved", approvedBy: A.me().id, at: A.now() });
      A.audit("trust.interest.posted", { target: accId, note: `${monthName(p.month)} ${A.cents(al.bank)}` }); });
    A.toast("Interest posted to the client's account ledger", "ok");
  });
  A.action("trust-sep-open", () => {
    if (!A.can("trust.record", "act")) return A.toast(why("trust.record", "act"), "bad");
    const ms = trustMatters("trust.record").filter((m) => m.status !== "closed" && !A.store.all("trustSepAccounts").some((a) => a.matterId === m.id));
    if (!ms.length) return A.toast("Every open matter you can see already has a separate account");
    A.modal("Open separate interest-bearing account", h`<div class="form">
      <label class="field">Client matter<select name="matterId">${ms.map((m) => h`<option value="${m.id}">${m.number} ${m.name}</option>`)}</select></label>
      <label class="field">Account (last 4)<input name="last4" class="input" inputmode="numeric" maxlength="4" value="6612"></label>
      <label class="field">Interest rate (%)<input name="rate" class="input" type="number" step="0.01" value="2.85"></label></div>
      <p class="muted small">Open it at the bank first, titled in trust for the client. A licensee approves before the account is used.</p>`, [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
      const v = A.formValues(d), rate = Math.round(+v.rate * 100);
      if (!/^\d{4}$/.test(v.last4)) { A.toast("Enter the last 4 digits", "bad"); return false; }
      if (!(rate > 0 && rate < 2000)) { A.toast("Enter a rate between 0 and 20%", "bad"); return false; }
      const m = A.store.get("matters", v.matterId);
      A.gate({ kind: "trust-sep-account", cap: "trust.signoff", matterId: m.id, preparedBy: A.me().id, effect: "trust.sepOpen", title: `Use separate trust account ••••${v.last4} for ${m.number}`, summary: `${A.contact(m.clientId).name}, ${(rate / 100).toFixed(2)}% interest to the client.`,
        checks: [{ label: "Titled in trust for one client", ok: true }, { label: "At an eligible institution", ok: true }], payload: { matterId: m.id, last4: v.last4, rateBp: rate } });
      A.toast("Sent for a licensee's approval");
    } }]);
  });
  A.effect("trust.sepOpen", (p) => {
    const m = A.store.get("matters", p.matterId);
    A.store.put("trustSepAccounts", { id: A.id("sa"), matterId: p.matterId, title: `Linda K Law in trust for ${A.contact(m.clientId).name}`, bank: ACCOUNT.bank, number: "••••" + p.last4, opened: A.today, rateBp: p.rateBp, status: "active", openedBy: A.me().id });
    A.audit("trust.account.opened", { target: p.matterId, note: `Separate interest-bearing ••••${p.last4}` }); A.toast("Account ready to use", "ok");
  });

  /* ---------- record retention (By-Law 9 ss.21-23, verify) ---------- */
  A.seed("trustRetention", [{ id: "policy", trustYears: 10, otherYears: 6, hold: false, by: "u-linda", at: "2026-06-30 09:00" }]);
  const retention = () => A.store.get("trustRetention", "policy") || { trustYears: 10, otherYears: 6 };
  const FYE = "12-31";
  /** Retention end: the fiscal year end of the record's year plus N years. */
  const keepUntil = (date, years) => `${+String(date).slice(0, 4) + years}-${FYE}`;
  function retentionCard() {
    const r = retention(), canAct = A.can("trust.signoff", "approve"), oldest = txs()[0];
    return h`<section class="card" style="margin-top:14px"><h2>Record retention <span class="muted small">demo</span></h2>
      <dl class="kv"><dt>Trust journals and ledgers</dt><dd>${r.trustYears} years after fiscal year end</dd><dt>Other financial records (fees book)</dt><dd>${r.otherYears} years</dd>
        <dt>Legal hold</dt><dd>${r.hold ? A.ui.status("hold", "On: nothing is purged") : "Off"}</dd><dt>Oldest trust entry</dt><dd>${oldest ? h`${A.date(oldest.date)}, kept until ${A.date(keepUntil(oldest.date, r.trustYears))}` : "—"}</dd>
        <dt>Last changed</dt><dd>${A.user(r.by).name} · ${A.dateTime(r.at)}</dd></dl>
      <div class="row" style="margin-top:8px">${A.ui.btn({ label: "Change retention", a: "trust-ret-change", small: true, disabled: !canAct, why: canAct ? "" : "Only a licensee can propose a retention change" })}
        ${A.ui.btn({ label: r.hold ? "Lift legal hold" : "Place legal hold", a: "trust-ret-hold", small: true, disabled: !canAct, why: canAct ? "" : "Only a licensee can place a hold" })}
        ${A.ui.btn({ label: "Test a purge", a: "trust-ret-test", small: true })}</div>
      <p class="muted small">Periods cannot go below the By-Law 9 minimums (10 and 6 years, verify). Purging before the end is refused. Every change needs a second licensee's approval.</p></section>`;
  }
  A.action("trust-ret-change", () => {
    const r = retention();
    A.modal("Change record retention", h`<div class="form"><label class="field">Trust journals and ledgers (years)<input name="trustYears" class="input" type="number" min="10" value="${r.trustYears}"></label>
      <label class="field">Other financial records (years)<input name="otherYears" class="input" type="number" min="6" value="${r.otherYears}"></label></div>`, [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
      const v = A.formValues(d), ty = Math.floor(+v.trustYears), oy = Math.floor(+v.otherYears);
      if (!(ty >= 10) || !(oy >= 6)) { A.toast("Below the By-Law 9 minimum: trust 10 years, other 6", "bad"); return false; }
      if (ty === r.trustYears && oy === r.otherYears) { A.toast("No change"); return false; }
      A.gate({ kind: "trust-retention", cap: "trust.signoff", preparedBy: A.me().id, effect: "trust.retention", title: `Change trust record retention to ${ty} / ${oy} years`, summary: `Trust journals and ledgers ${r.trustYears} → ${ty} years; other records ${r.otherYears} → ${oy} years.`, checks: [{ label: "At or above the By-Law 9 minimums", ok: true }], payload: { trustYears: ty, otherYears: oy } });
      A.toast("Sent for a second licensee's approval");
    } }]);
  });
  A.effect("trust.retention", (p) => { A.store.put("trustRetention", { id: "policy", ...p, by: A.me().id, at: A.now() }); A.audit("trust.retention.changed", { note: `${p.trustYears} / ${p.otherYears} years` }); A.toast("Retention updated", "ok"); });
  A.action("trust-ret-hold", () => {
    if (!A.can("trust.signoff", "approve")) return A.toast("Only a licensee can place a hold", "bad");
    const on = !retention().hold;
    A.store.put("trustRetention", { id: "policy", hold: on, by: A.me().id, at: A.now() }); A.audit("trust.retention.hold", { note: on ? "Legal hold placed" : "Legal hold lifted" });
    A.toast(on ? "Legal hold placed: nothing is purged" : "Legal hold lifted", "ok");
  });
  A.action("trust-ret-test", () => {
    const r = retention(), t = txs()[0]; if (!t) return A.toast("No trust records yet");
    const end = keepUntil(t.date, r.trustYears);
    A.audit("trust.retention.tested", { target: t.id, note: `Purge refused until ${end}` });
    A.modal("Purge test (demo)", h`<p>Purge of <b>${t.ref}</b> (${A.date(t.date)}, ${mlabel(t.matterId)}): ${A.ui.status("blocked", "Refused")}</p><p class="small">Kept until ${A.date(end)}: ${r.trustYears} years after the fiscal year end of the record.${r.hold ? " A legal hold is also on." : ""} Nothing was deleted.</p>`);
  });

  /* ---------- registration ---------- */
  A.module({ id: "trust", label: "Trust", icon: "⚖", order: 50, cap: "trust.view",
    badge: () => A.pendingFor().filter((g) => /^trust\./.test(g.cap) && A.canApprove(g).ok).length,
    smoke: ["trust", "trust/ledgers", "trust/journals", "trust/withdrawals", "trust/accounts", "trust/year-end", "trust/audit", "trust/reconcile", "trust/ledger/m1", "trust/ledger/m2", "trust/ledger/m4", "trust/ledger/m6", "trust/package/2026-09", "trust/package/2026-08", "trust/client/c-hanna", "trust/client/c-grace", "trust/authorization/tt-5", "trust/authorization/tt-6", "trust/authorization/tt-9", "trust/interest"],
    routes: { trust: overview, "trust/ledgers": ledgersRoute, "trust/journals": journalsRoute, "trust/withdrawals": withdrawalsRoute, "trust/accounts": accountsRoute, "trust/year-end": yearEndRoute, "trust/audit": auditRoute, "trust/interest": interestRoute,
      "trust/reconcile": reconRoute, "trust/ledger/:matterId": ledgerRoute, "trust/package/:month": packageRoute, "trust/client/:clientId": clientRoute, "trust/authorization/:id": authRoute } });
  A.matterTab({ id: "trust", label: "Trust", cap: "trust.view", order: 50, render(el, m) {
    if (!A.canMatter(m, "trust.view")) return A.render(el, A.ui.empty("No trust access", "Your role cannot see trust figures for this matter."));
    A.render(el, ledgerHtml(m));
  } });

  /* ---------- agent ---------- */
  function findMatter(text, ctx) {
    const t = text.toLowerCase();
    const ms = trustMatters();
    const hit = ms.find((m) => t.includes(m.number.toLowerCase()) || [m.name, A.contact(m.clientId).name].join(" ").toLowerCase().split(/[^a-z0-9]+/).some((w) => w.length > 4 && !["purchase", "estate", "review", "lease"].includes(w) && t.includes(w)));
    if (hit) return hit;
    if (/this matter|this file|this client|\bhere\b/.test(t) && ctx && ctx.kind === "matter") return tmatter(ctx.id);
    return null;
  }
  A.intent({ match: /trust balance|balance in trust|how much.*trust/i, label: "Trust balance for a matter", example: "trust balance for this matter", run(text, ctx) {
    if (!A.can("trust.view")) return { text: "Your role cannot see trust figures." };
    const m = findMatter(text, ctx) || (ctx && ctx.kind === "matter" ? tmatter(ctx.id) : null);
    if (m) { const b = balance(m.id), c = cleared(m.id); return { text: `${m.number} ${m.name}: ${A.money(b)} in trust for ${A.contact(m.clientId).name}${c !== b ? `, of which ${A.money(c)} has cleared` : ", all cleared"}.`, actions: [{ label: "Open ledger", go: `trust/ledger/${m.id}` }] }; }
    const rows = trustMatters().map((x) => ({ m: x, b: balance(x.id) })).filter((x) => x.b);
    if (!rows.length) return { text: "No trust funds are held on matters you can see." };
    return { text: `Trust held: ${rows.map((x) => `${x.m.number} ${A.money(x.b)}`).join("; ")}. Total ${A.money(rows.reduce((s, x) => s + x.b, 0))}.`, actions: [{ label: "Trust overview", go: "trust" }] };
  } });
  A.intent({ match: /pay (the |this |our )?(bill|invoice|account|fees?) (out of|from) trust|from trust.*(bill|invoice)/i, label: "Can we pay the bill from trust?", example: "can we pay the bill from trust", run(text, ctx) {
    if (!A.can("trust.view")) return { text: "Your role cannot see trust figures." };
    const m = findMatter(text, ctx) || (ctx && ctx.kind === "matter" ? tmatter(ctx.id) : null);
    if (!m) return { text: "Which matter? Open it and ask again, or name the client.", actions: [{ label: "Trust overview", go: "trust" }] };
    const c = available(m.id), inv = A.store.all("invoices").filter((i) => i.matterId === m.id && !/paid|void|draft/i.test(i.status || "")).pop();
    const due = inv ? +(inv.total ?? inv.amount ?? inv.balance ?? 0) : null;
    const lines2 = [`Trust funds available to pay out on ${m.number}: ${A.money(c)}.`];
    if (inv) lines2.push(`Unpaid invoice ${inv.number || inv.id}: ${A.money(due)}. ${c >= due ? "Funds cover it." : `Short by ${A.money(due - c)}; transfer only what is covered.`} Fees can only come out of trust after the invoice is delivered to the client, and a signatory must authorize.`);
    else lines2.push("I found no delivered, unpaid invoice on this matter. Fees cannot be taken from trust until a bill is delivered.");
    return { text: lines2.join(" "), actions: A.can("trust.request", "act") && c > 0 ? [{ label: "Request withdrawal", run: () => requestModal(m.id) }, { label: "Open ledger", go: `trust/ledger/${m.id}` }] : [{ label: "Open ledger", go: `trust/ledger/${m.id}` }] };
  } });
  A.intent({ match: /reconcil/i, label: "Reconcile September", example: "reconcile September", run(text) {
    if (!firmWide()) return { text: "Reconciliations need firm-wide trust access." };
    const names = ["january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"];
    const i = names.findIndex((n) => text.toLowerCase().includes(n.slice(0, 3)));
    const yr = A.today.slice(0, 4), mon = i >= 0 ? `${i + 1 > +A.today.slice(5, 7) ? yr - 1 : yr}-${String(i + 1).padStart(2, "0")}` : lastClosedMonth();
    const tw = threeWay(monthEnd(mon)), rc = recon(mon), un = tw.unmatched.filter((l) => !l.explained);
    const go = { label: "Open reconciliation", run: () => { reconMonth = mon; A.go("trust/reconcile"); } };
    if (rc && rc.status === "signed") return { text: `${monthName(mon)} is already signed off by ${A.user(rc.signedBy).name} and locked.`, actions: [go] };
    const txt = `${monthName(mon)}: adjusted bank ${A.money(tw.adjusted)}, journal ${A.money(tw.journal)}, client ledgers ${A.money(tw.ledgers)}. ${tw.outstanding.length} outstanding item(s), ${tw.transit.length} deposit(s) in transit, ${un.length} unexplained bank item(s).`;
    const acts = [go];
    if (!un.length && monthEnd(mon) < A.today && A.can("trust.reconcile", "act")) acts.push({ label: "Prepare for sign-off", run: () => prepareRecon(mon) });
    return { text: un.length ? `${txt} Explain ${un.map((l) => l.text).join(", ")} before I prepare it for sign-off.` : txt, actions: acts };
  } });
  A.suggest("trust", () => ["Reconcile September", "Trust balance for Okafor", "Can we pay the bill from trust for Harbourview?"]);
  A.suggest("matter", (c) => (A.can("trust.view") && tmatter(c.id) ? ["Trust balance for this matter"] : []));

  /* ---------- reports ---------- */
  const col = (c) => A.store.all(c) || [];
  const pick = (o, ...ks) => { for (const k of ks) if (o && o[k] != null) return o[k]; return undefined; };
  const num = (v) => +v || 0;
  const visibleMatter = (id) => !id || A.canMatter(A.store.get("matters", id), "trust.view") || A.canMatter(A.store.get("matters", id), "billing.view") || A.canMatter(A.store.get("matters", id));
  const hrs = (t) => num(pick(t, "hours")) || num(pick(t, "minutes")) / 60;
  const invTotal = (i) => num(pick(i, "total", "amount"));
  const invDate = (i) => pick(i, "delivered", "issued", "date", "created") || "";
  const invPaid = (i) => num(pick(i, "paid", "amountPaid"));
  const hoursAgo = (a, b) => { if (!a || !b) return null; const p = (s) => new Date(s.replace(" ", "T") + (s.length <= 10 ? "T12:00" : "")).getTime(); return (p(b) - p(a)) / 36e5; };
  const groupSum = (rows, kf, vf) => { const m = {}; rows.forEach((r) => { const k = kf(r); m[k] = (m[k] || 0) + vf(r); }); return Object.entries(m); };
  const REPORTS = [
    { id: "ar-aging", group: "Money", title: "Accounts receivable aging", needs: ["invoices"], desc: "Unpaid invoices by age since delivery.", run() {
      const b = { "0-30": 0, "31-60": 0, "61-90": 0, "90+": 0 }, rows = col("invoices").filter((i) => visibleMatter(i.matterId) && !/void|draft/i.test(i.status || "") && invTotal(i) - invPaid(i) > 0);
      rows.forEach((i) => { const d = A.daysBetween(String(invDate(i)).slice(0, 10) || A.today, A.today); b[d <= 30 ? "0-30" : d <= 60 ? "31-60" : d <= 90 ? "61-90" : "90+"] += invTotal(i) - invPaid(i); });
      return { cols: ["Age (days)", "Outstanding"], rows: Object.entries(b).map(([k, v]) => [k, A.money(v)]) }; } },
    { id: "wip", group: "Money", title: "Unbilled work in progress", needs: ["time"], alt: ["timeEntries"], desc: "Recorded, unbilled time by matter at standard rates.", run(c) {
      const rows = groupSum(c.filter((t) => visibleMatter(t.matterId) && !t.invoiceId && !/billed|written/i.test(t.status || "")), (t) => mlabel(t.matterId), (t) => hrs(t) * num(pick(t, "rate")));
      return { cols: ["Matter", "Unbilled value"], rows: rows.sort((a, b) => b[1] - a[1]).map(([k, v]) => [k, A.money(v)]) }; } },
    { id: "billings", group: "Money", title: "Billings by responsible lawyer", needs: ["invoices"], desc: "Invoiced fees by the matter's responsible lawyer.", run(c) {
      return { cols: ["Lawyer", "Invoiced"], rows: groupSum(c.filter((i) => visibleMatter(i.matterId) && !/void|draft/i.test(i.status || "")), (i) => A.user((A.store.get("matters", i.matterId) || {}).responsible).name, invTotal).map(([k, v]) => [k, A.money(v)]) }; } },
    { id: "collections", group: "Money", title: "Collections this month", needs: ["payments"], desc: "Payments received in the current month.", run(c) {
      const rows = c.filter((p) => String(pick(p, "date", "received") || "").slice(0, 7) === A.today.slice(0, 7) && visibleMatter(p.matterId));
      return { cols: ["Date", "Matter", "Amount"], rows: rows.map((p) => [A.date(pick(p, "date", "received")), mlabel(p.matterId), A.money(pick(p, "amount"))]) }; } },
    { id: "trust-listing", group: "Trust", title: "Trust listing by client", needs: ["trustTx"], desc: "Client ledger balances at the last month end, as filed with the reconciliation.", run() {
      const end = monthEnd(lastClosedMonth()), ms = trustMatters().filter((m) => balance(m.id, end) !== 0);
      return { cols: ["Client", "Matter", `Balance ${A.date(end)}`], rows: ms.map((m) => [A.contact(m.clientId).name, `${m.number} ${m.name}`, A.money(balance(m.id, end))]).concat([["Total", "", A.money(ms.reduce((s, m) => s + balance(m.id, end), 0))]]) }; } },
    { id: "trust-coverage", group: "Trust", title: "WIP vs trust held vs minimum balance", needs: ["trustTx"], desc: "Per matter: unbilled work, trust held, funds held for a purpose, the minimum to keep, and any top-up to ask for.", run() {
      const wip = (id) => r2(col("timeEntries").filter((t) => t.matterId === id && t.status === "accepted").reduce((s, t) => s + hrs(t) * num(t.rate), 0));
      const rows = trustMatters().filter((m) => m.status !== "closed").map((m) => { const w = wip(m.id), b = balance(m.id), pr = setting(m.id).protected || 0, min = setting(m.id).minimum || 0, free = r2(b - pr - w); return { m, w, b, pr, min, free, short: r2(Math.max(0, min - free)) }; }).filter((x) => x.w || x.b || x.min);
      return { cols: ["Matter", "Unbilled WIP ($)", "Trust balance", "Held for a purpose ($)", "Minimum ($)", "Available after WIP ($)", "Top-up to request ($)"],
        rows: rows.sort((a, b) => b.short - a.short).map((x) => [mlabel(x.m.id), A.money(x.w), A.money(x.b), A.money(x.pr), A.money(x.min), A.money(x.free), x.short ? A.money(x.short) : "—"]) }; } },
    { id: "trust-journal", group: "Trust", title: "Trust receipts and disbursements journal", needs: ["trustTx"], desc: "Every trust entry in date order, last 90 days.", run() {
      return { cols: ["Date", "Matter", "Kind", "Party", "Ref", "Amount"], rows: txs().filter((t) => tmatter(t.matterId) && A.daysBetween(t.date, A.today) <= 90).map((t) => [A.date(t.date), mlabel(t.matterId), t.kind, t.party, t.ref, A.money(sign(t))]) }; } },
    { id: "trust-uncleared", group: "Trust", title: "Uncleared and unsent items", needs: ["trustTx"], desc: "Deposits not yet cleared and authorized payments not yet sent.", run() {
      return { cols: ["Date", "Matter", "Item", "Amount"], rows: txs().filter((t) => tmatter(t.matterId) && ((t.kind === "receipt" && !t.cleared) || t.status === "pending")).map((t) => [A.date(t.date), mlabel(t.matterId), t.kind === "receipt" ? `Uncleared deposit ${t.ref}` : `Authorized, not sent: ${t.party}`, A.money(t.amount)]) }; } },
    { id: "trust-inactive", group: "Trust", title: "Inactive trust balances", needs: ["trustTx"], desc: "Balances with no activity for 60 days or more (unclaimed after 2 years must go to the LSO).", run() {
      return { cols: ["Matter", "Balance", "Last activity", "Days"], rows: trustMatters().map((m) => ({ m, b: balance(m.id), d: (txs().filter((t) => t.matterId === m.id).pop() || {}).date })).filter((x) => x.b && x.d && A.daysBetween(x.d, A.today) >= 60).map((x) => [mlabel(x.m.id), A.money(x.b), A.date(x.d), String(A.daysBetween(x.d, A.today))]) }; } },
    { id: "trust-recons", group: "Trust", title: "Reconciliation history", needs: ["trustRecons"], desc: "Monthly three-way reconciliations and who signed them off.", run(c) {
      return { cols: ["Month", "Journal", "Client ledgers", "Signed off"], rows: c.slice().sort((a, b) => b.month.localeCompare(a.month)).map((r) => [monthName(r.month), A.money(r.journal), A.money(r.ledgers), `${A.user(r.signedBy).name} ${r.signedAt}`]) }; } },
    { id: "hours", group: "Time", title: "Hours by timekeeper, this month", needs: ["time"], alt: ["timeEntries"], desc: "Recorded hours in the current month.", run(c) {
      return { cols: ["Timekeeper", "Hours"], rows: groupSum(c.filter((t) => String(pick(t, "date") || "").slice(0, 7) === A.today.slice(0, 7) && visibleMatter(t.matterId)), (t) => A.user(pick(t, "userId", "by", "user")).name, hrs).map(([k, v]) => [k, v.toFixed(1)]) }; } },
    { id: "time-unapproved", group: "Time", title: "Time awaiting review", needs: ["time"], alt: ["timeEntries"], desc: "Entries not yet reviewed for billing.", run(c) {
      return { cols: ["Date", "Matter", "Who", "Hours"], rows: c.filter((t) => visibleMatter(t.matterId) && /draft|pending|suggest|review/i.test(t.status || "")).map((t) => [A.date(pick(t, "date")), mlabel(t.matterId), A.user(pick(t, "userId", "by", "user")).name, hrs(t).toFixed(1)]) }; } },
    { id: "matters-area", group: "Matters", title: "Open matters by practice area", needs: ["matters"], desc: "Count of open matters per area.", run(c) {
      return { cols: ["Area", "Open matters"], rows: groupSum(c.filter((m) => m.status !== "closed" && A.canMatter(m)), (m) => m.area, () => 1).map(([k, v]) => [k, String(v)]) }; } },
    { id: "matters-new", group: "Matters", title: "New matters, last 90 days", needs: ["matters"], desc: "Matters opened recently, with responsible lawyer.", run(c) {
      return { cols: ["Opened", "Matter", "Area", "Responsible"], rows: c.filter((m) => A.canMatter(m) && A.daysBetween(m.opened, A.today) <= 90).sort((a, b) => b.opened.localeCompare(a.opened)).map((m) => [A.date(m.opened), `${m.number} ${m.name}`, m.area, A.user(m.responsible).name]) }; } },
    { id: "deadlines", group: "Compliance", title: "Deadline compliance", needs: ["deadlines"], desc: "Deadlines met, open, overdue, and unverified.", run(c) {
      const vis = c.filter((d) => visibleMatter(d.matterId)), due = (d) => String(pick(d, "due", "date") || "");
      const done = (d) => /done|met|complete|filed/i.test(d.status || "") || d.done;
      return { cols: ["Measure", "Count"], rows: [["Met", vis.filter(done).length], ["Open, not yet due", vis.filter((d) => !done(d) && due(d) >= A.today).length], ["Overdue", vis.filter((d) => !done(d) && due(d) && due(d) < A.today).length], ["Not verified by a lawyer", vis.filter((d) => !d.verified && !d.verifiedBy && !/verified/i.test(d.status || "")).length]].map(([k, v]) => [k, String(v)]) }; } },
    { id: "audit-sensitive", group: "Compliance", title: "Sensitive actions log", needs: ["audit"], desc: "Trust, approval and access events, latest first.", run(c) {
      return { cols: ["When", "Who", "Action", "Note"], rows: c.filter((a) => /^(trust|gate|access|breakglass)/.test(a.action)).slice(0, 40).map((a) => [a.at, A.user(a.actor).name, a.action, a.note || ""]) }; } },
    { id: "agent-turnaround", group: "Agent", title: "Approval turnaround", needs: ["approvals"], desc: "Hours from agent or staff preparation to a decision, by kind.", run(c) {
      const dec = c.filter((g) => g.decidedAt && visibleMatter(g.matterId)), m = {};
      dec.forEach((g) => { const k = g.kind || g.cap; const x = hoursAgo(g.created, g.decidedAt); (m[k] = m[k] || []).push(x || 0); });
      const pend = c.filter((g) => g.status === "pending" && visibleMatter(g.matterId));
      return { cols: ["Kind", "Decided", "Avg hours", "Pending"], rows: [...new Set([...Object.keys(m), ...pend.map((g) => g.kind || g.cap)])].map((k) => [k, String((m[k] || []).length), m[k] ? (m[k].reduce((s, x) => s + x, 0) / m[k].length).toFixed(1) : "–", String(pend.filter((g) => (g.kind || g.cap) === k).length)]) }; } },
    { id: "agent-sendback", group: "Agent", title: "Agent drafts sent back", needs: ["approvals"], desc: "How often reviewers return what the agent prepared.", run(c) {
      const a = c.filter((g) => g.preparedBy === "agent" && g.status !== "pending" && visibleMatter(g.matterId));
      const back = a.filter((g) => g.status === "returned").length;
      return { cols: ["Decided agent drafts", "Approved", "Sent back", "Send-back rate"], rows: [[String(a.length), String(a.length - back), String(back), a.length ? Math.round((back / a.length) * 100) + "%" : "–"]] }; } },
    { id: "agent-runs", group: "Agent", title: "Agent work log", needs: ["runs"], desc: "Recent agent runs and outcomes.", run(c) {
      return { cols: ["Started", "Task", "Status", "Result"], rows: c.filter((r) => visibleMatter(r.matterId)).slice(0, 30).map((r) => [r.started, r.title, r.status, r.result || ""]) }; } },
  ];
  const GROUPS = ["Money", "Trust", "Time", "Matters", "Compliance", "Agent"];
  const groupsFor = () => { const r = A.me().role; if (r === "bookkeeper") return ["Money", "Trust"]; return GROUPS.filter((g) => g !== "Trust" || A.can("trust.view")); };
  const dataFor = (r) => { for (const c of [...r.needs, ...(r.alt || [])]) { const d = col(c); if (d.length) return d; } return null; };
  let reportsFilter = "all";
  const DATASETS = [["trustTx", "Trust entries"], ["invoices", "Bills"], ["timeEntries", "Time entries"], ["matters", "Matters"], ["approvals", "Approvals"], ["runs", "Agent runs"]];
  const savedReports = () => A.store.where("reportPresets", (r) => r.owner === A.me().id || r.shared);
  function customRun(cfg) {
    const rows = col(cfg.dataset).filter((r) => cfg.dataset === "matters" ? A.canMatter(r) : visibleMatter(r.matterId)).filter((r) => (cfg.dataset !== "trustTx" || tmatter(r.matterId)));
    const label = (r) => { const v = r[cfg.group]; return cfg.group === "matterId" ? mlabel(v) : /^(u-|agent)/.test(String(v)) ? A.user(v).name : String(v ?? "(blank)"); };
    const g = groupSum(rows, label, (r) => (cfg.measure === "count" ? 1 : num(r[cfg.measure])));
    return { cols: [cfg.group, cfg.measure === "count" ? "Count" : `Sum of ${cfg.measure}`], rows: g.sort((a, b) => b[1] - a[1]).map(([k, v]) => [k, cfg.measure === "count" ? String(v) : A.money(v)]) };
  }
  function kpis() {
    const wip = r2(col("timeEntries").filter((t) => visibleMatter(t.matterId) && t.status === "accepted" && !t.invoiceId).reduce((s, t) => s + hrs(t) * num(t.rate), 0));
    const held = A.can("trust.view") ? r2(trustMatters().reduce((s, m) => s + balance(m.id), 0)) : null;
    const pend = A.pendingFor().length, dec = col("approvals").filter((g) => g.decidedAt && visibleMatter(g.matterId));
    const turn = dec.length ? dec.reduce((s, g) => s + (hoursAgo(g.created, g.decidedAt) || 0), 0) / dec.length : null;
    const open = A.matters((m) => m.status !== "closed").length;
    return h`<div class="stats">${A.ui.stat("Open matters", String(open), "you can see")}${A.ui.stat("Unbilled WIP", A.money(wip), "accepted time not yet billed")}${held != null ? A.ui.stat("Held in trust", A.money(held), "all client ledgers") : ""}${A.ui.stat("Approvals waiting", String(pend), "you can approve", pend ? "gate" : "")}${A.ui.stat("Approval turnaround", turn == null ? "–" : `${turn.toFixed(1)} h`, "average, prepared to decided")}</div>`;
  }
  function reportsRoute(el) {
    A.setContext({ kind: "reports", label: "Reports" });
    const gs = groupsFor(), list = REPORTS.filter((r) => gs.includes(r.group) && (reportsFilter === "all" || r.group === reportsFilter) && hit(r.title, r.desc, r.group));
    const saved = savedReports().filter((r) => hit(r.title));
    A.render(el, h`<div class="page">${head("Reports", "", h`<button class="btn" data-a="ask" data-q="reconcile September">✦ Ask a question</button><button class="btn primary" data-a="go" data-to="reports/custom" data-key="n">New report${key("n")}</button>`, `Computed live from the firm's records as of ${A.date(A.today)}. ${A.can("reports.firm", "all") ? "" : "Read-only access."}`)}
      ${kpis()}
      <div class="row wrap" style="margin-bottom:6px">${search("Search reports")}${A.ui.tabs([{ id: "all", label: "All" }, ...gs.map((g) => ({ id: g, label: g }))], reportsFilter, "reports-group")}</div>
      ${reportsFilter === "all" ? h`<section class="card row" style="margin:12px 0"><div class="grow"><b>Firm insights</b><div class="muted small">Utilization, realization, collection, lock-up and intake trends against the firm's targets.</div></div>${A.ui.btn({ label: "Open", to: "reports/insights", small: true })}</section>${schedulesSection()}` : ""}
      ${saved.length && reportsFilter === "all" ? h`<h2 style="font-size:13px;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);margin:16px 0 8px">Saved</h2>${A.ui.table([{ label: "Report", render: (r) => h`<b>${r.title}</b>` }, { label: "Dataset", render: (r) => (DATASETS.find((d) => d[0] === r.dataset) || [, r.dataset])[1] }, { label: "Schedule", render: (r) => r.schedule || "—" }], saved, { onRow: "reports-saved" })}` : ""}
      ${gs.filter((g) => list.some((r) => r.group === g)).map((g) => h`<h2 style="font-size:13px;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);margin:16px 0 8px">${g}</h2>
        ${A.ui.table([{ label: "Report", render: (r) => h`<b>${r.title}</b><div class="muted small">${r.desc}</div>` }, { label: "Data", render: (r) => (dataFor(r) ? A.ui.status("done", "Live") : A.ui.status("No data yet")) }], list.filter((r) => r.group === g), { onRow: "reports-open" })}`)}
      ${!list.length && !saved.length ? A.ui.empty("No report matches", "Try another word, or build one with New report.") : ""}
    </div>`);
    after();
  }
  function reportActions(id, title) {
    return h`${A.ui.btn({ label: "✦ Explain this report", a: "reports-explain", id, agent: true, key: "i" })}<button class="btn" data-a="reports-export" data-id="${id}">Export CSV</button><button class="btn" data-a="reports-schedule" data-id="${id}" data-title="${title}">Schedule</button>${btn("Send outside the firm", "reports-send", "reports.firm", "view", { id, title }, "btn")}`;
  }
  let lastOut = null;
  function reportRoute(el, p) {
    if (p.id === "custom") return customRoute(el);
    if (p.id === "insights") return insightsRoute(el);
    const r = REPORTS.find((x) => x.id === p.id);
    if (!r || !groupsFor().includes(r.group)) return A.render(el, h`<div class="page"><div class="locked"><b>No access</b>This report is not available to your role.</div></div>`);
    A.setContext({ kind: "reports", label: r.title });
    const d = dataFor(r); let out = null, err = "";
    if (d) { try { out = r.run(d); } catch (e) { err = e.message; } }
    lastOut = out && { title: r.title, ...out };
    A.render(el, h`<div class="page">${head(r.title, d ? A.ui.status("done", "Live") : A.ui.status("No data yet"), h`<button class="btn" data-a="go" data-to="reports">All reports</button>${out ? reportActions(r.id, r.title) : ""}`, `${r.group} · ${r.desc} · as of ${A.date(A.today)}`)}
      ${out ? explainCard(r.id) : ""}
      ${!d ? A.ui.empty("No data yet", `This report reads ${r.needs.join(", ")}, which has no records.`) : err ? A.ui.empty("Could not compute", err) : table(out)}</div>`);
    after();
  }
  const table = (out) => A.ui.table(out.cols.map((c, i) => ({ label: c, num: i > 0 && /amount|balance|outstanding|value|invoiced|hours|count|sum|journal|ledgers|days|\$/i.test(c), render: (row) => row[i] })), out.rows.map((row, i) => Object.assign(row, { id: String(i) })), { empty: "Nothing to report" });
  let draft = { dataset: "trustTx", group: "matterId", measure: "amount", title: "" };
  const fieldsOf = (c) => { const r = col(c)[0] || {}; return Object.keys(r).filter((k) => !/^(id|body|cites|checks|payload|lines|disb|plan|steps|team|wall)$/.test(k) && typeof r[k] !== "object"); };
  function customRoute(el) {
    A.setContext({ kind: "reports", label: "New report" });
    const f = fieldsOf(draft.dataset), nums = f.filter((k) => typeof (col(draft.dataset)[0] || {})[k] === "number");
    if (!f.includes(draft.group)) draft.group = f.includes("matterId") ? "matterId" : f[0] || "";
    if (draft.measure !== "count" && !nums.includes(draft.measure)) draft.measure = nums[0] || "count";
    let out = null; try { out = draft.group ? customRun(draft) : null; } catch (e) { out = null; }
    lastOut = out && { title: draft.title || "Custom report", ...out };
    A.render(el, h`<div class="page">${head("New report", A.ui.status("needs-approval", "Preview"), h`<button class="btn" data-a="go" data-to="reports">All reports</button><button class="btn primary" data-a="reports-save">Save</button>`, "Pick a dataset, what to group by and what to total. Only records you are allowed to see are counted.")}
      <div class="card" style="margin-bottom:12px"><div class="row wrap">
        <label class="field">Dataset<select data-c="reports-draft" name="dataset">${DATASETS.map(([k, l]) => h`<option value="${k}" ${k === draft.dataset ? A.raw("selected") : ""}>${l}</option>`)}</select></label>
        <label class="field">Group by<select data-c="reports-draft" name="group">${f.map((k) => h`<option ${k === draft.group ? A.raw("selected") : ""}>${k}</option>`)}</select></label>
        <label class="field">Total<select data-c="reports-draft" name="measure"><option value="count" ${draft.measure === "count" ? A.raw("selected") : ""}>Count of records</option>${nums.map((k) => h`<option value="${k}" ${k === draft.measure ? A.raw("selected") : ""}>Sum of ${k}</option>`)}</select></label>
      </div></div>
      ${out ? table(out) : A.ui.empty("No data yet", "This dataset has no records you can see.")}</div>`);
  }
  A.change("reports-draft", (el) => { draft = { ...draft, [el.name]: el.value }; A.refresh(); });
  A.action("reports-save", () => A.modal("Save report", h`<label class="field">Name<input name="title" class="input" value="${draft.title || ""}" placeholder="Trust entries by matter"></label><label class="field"><span><input type="checkbox" name="shared"> Share with the firm</span></label>`, [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
    const v = A.formValues(d); if (!v.title.trim()) { A.toast("Enter a name", "bad"); return false; }
    A.store.put("reportPresets", { id: A.id("rp"), ...draft, title: v.title.trim(), shared: !!v.shared, owner: A.me().id }); A.audit("report.saved", { note: v.title.trim() }); A.toast("Saved", "ok");
  } }]));
  A.action("reports-saved", (el) => { const r = A.store.get("reportPresets", el.dataset.id); if (r) { draft = { dataset: r.dataset, group: r.group, measure: r.measure, title: r.title }; A.go("reports/custom"); } });
  A.action("reports-export", () => {
    if (!lastOut) return; const csv = [lastOut.cols.join(","), ...lastOut.rows.map((r) => r.slice(0, lastOut.cols.length).map((x) => `"${String(x).replace(/"/g, "'")}"`).join(","))].join("\n");
    A.audit("report.exported", { note: lastOut.title }); A.modal(`Export · ${lastOut.title}`, h`<pre style="white-space:pre;overflow:auto;max-height:320px;font-size:11px">${csv}</pre>`);
  });
  A.action("reports-send", (el) => {
    if (!lastOut) return;
    A.modal(`Send outside the firm · ${el.dataset.title}`, h`<label class="field">Recipient<select name="to"><option>Firm's external accountant</option><option>LSO spot auditor</option><option>LAWPRO (insurer)</option></select></label><p class="muted small">Anything leaving the firm needs a lawyer's approval.</p>`, [{ label: "Cancel" }, { label: "Send for approval", primary: true, run: (d) => {
      const to = d.querySelector("[name=to]").value;
      A.gate({ kind: "report-send", cap: "documents.release", preparedBy: A.me().id, effect: "report.send", title: `Send "${el.dataset.title}" to ${to}`, summary: `${lastOut.rows.length} rows as of ${A.date(A.today)}.`, checks: [{ label: "Contains only firm-level figures you can see", ok: true }], payload: { title: el.dataset.title, to } });
      A.toast("Sent for approval");
    } }]);
  });
  A.effect("report.send", (p) => { A.audit("report.sent", { note: `${p.title} to ${p.to}` }); A.toast(`Sent to ${p.to}`, "ok"); });
  /* ---------- firm insights: KPI trends against firm-set targets ---------- */
  /* Month-end snapshots (the prototype has only a few weeks of live records, so history is seeded). Percent KPIs in whole %, lock-up in days, amounts in cents. */
  const KPI = [
    { id: "utilization", label: "Utilization", unit: "%", up: true, def: "Billable hours recorded ÷ available hours (1,500 a year basis), by activity date." },
    { id: "realization", label: "Realization", unit: "%", up: true, def: "Fees billed ÷ value of time recorded at standard rates, by bill date." },
    { id: "collection", label: "Collection", unit: "%", up: true, def: "Fees collected ÷ fees billed, by payment date." },
    { id: "lockup", label: "Lock-up days", unit: "d", up: false, def: "Days of unbilled WIP plus unpaid receivables, at the month-end run rate." },
    { id: "avgBill", label: "Average bill", unit: "$", up: true, def: "Fees per delivered bill, before HST." },
    { id: "opened", label: "Matters opened", unit: "", up: true, def: "New matters opened in the month (conflict check cleared)." },
  ];
  const H = (m, u, r, c, l, b, o) => ({ month: m, utilization: u, realization: r, collection: c, lockup: l, avgBill: b, opened: o });
  A.seed("firmKpiHistory", [
    { id: "firm", rows: [H("2026-04", 61, 88, 79, 86, 214000, 3), H("2026-05", 64, 90, 81, 82, 238000, 4), H("2026-06", 66, 89, 83, 79, 226000, 2), H("2026-07", 63, 92, 80, 81, 251000, 3), H("2026-08", 67, 91, 85, 76, 247500, 4), H("2026-09", 69, 90, 84, 74, 262000, 5)] },
    { id: "u-linda", rows: [H("2026-04", 66, 90, 82, 80, 268000, 2), H("2026-05", 70, 92, 84, 77, 281000, 2), H("2026-06", 71, 90, 85, 75, 270000, 1), H("2026-07", 68, 93, 83, 76, 295000, 2), H("2026-08", 72, 93, 87, 71, 288000, 2), H("2026-09", 74, 91, 86, 69, 306000, 3)] },
    { id: "u-yoni", rows: [H("2026-04", 57, 85, 75, 93, 158000, 1), H("2026-05", 59, 87, 77, 90, 176000, 2), H("2026-06", 62, 88, 80, 85, 169000, 1), H("2026-07", 58, 90, 76, 88, 182000, 1), H("2026-08", 63, 89, 82, 82, 190000, 2), H("2026-09", 65, 88, 81, 80, 197500, 2)] },
  ]);
  A.seed("firmTargets", [{ id: "utilization", value: 70 }, { id: "realization", value: 90 }, { id: "collection", value: 90 }, { id: "lockup", value: 60 }, { id: "avgBill", value: 250000 }, { id: "opened", value: 4 }]);
  const target = (k) => (A.store.get("firmTargets", k) || {}).value;
  const fmtKpi = (k, v) => (v == null ? "—" : k.unit === "$" ? A.cents(v) : k.unit === "%" ? `${v}%` : k.unit === "d" ? `${v} days` : String(v));
  const onTarget = (k, v) => target(k.id) == null || (k.up ? v >= target(k.id) : v <= target(k.id));
  let insightUser = "firm", insightKpi = "utilization", insightSpan = 6;
  const canSeeColleagues = () => ["owner", "admin"].includes(A.me().role);
  const insightUsers = () => [["firm", "Whole firm"], ...A.store.where("users", (u) => u.signatory && (canSeeColleagues() || u.id === A.me().id)).map((u) => [u.id, u.name])];
  A.onUser(() => { insightUser = "firm"; });
  A.change("insights-user", (el) => { insightUser = el.value; A.refresh(); });
  A.action("insights-span", (el) => { insightSpan = +el.dataset.id; A.refresh(); });
  A.action("insights-kpi", (el) => { insightKpi = el.dataset.id; A.refresh(); });
  A.inlineEdit("insights-target", (id, value) => {
    if (!A.can("reports.firm", "all")) { A.toast("Only a lawyer or admin sets targets", "bad"); return false; }
    const k = KPI.find((x) => x.id === id), n = k && k.unit === "$" ? Math.round(+value * 100) : Math.round(+value);
    if (!k || !(n >= 0)) { A.toast("Enter a number", "bad"); return false; }
    A.store.put("firmTargets", { id, value: n }); A.audit("insights.target.set", { note: `${k.label}: ${fmtKpi(k, n)}` }); A.toast(`${k.label} target set`, "ok");
  });
  /** Small inline SVG line chart; currentColor/var() keep it legible in both themes. */
  function spark(vals, tgt, o = {}) {
    const w = o.w || 120, ht = o.h || 32, pad = o.pad || 3, all = vals.concat(tgt == null ? [] : [tgt]), lo = Math.min(...all), hi = Math.max(...all), span = hi - lo || 1;
    const x = (i) => pad + (i * (w - 2 * pad)) / Math.max(1, vals.length - 1), y = (v) => ht - pad - ((v - lo) / span) * (ht - 2 * pad);
    const pts = vals.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(" ");
    return A.raw(`<svg width="${w}" height="${ht}" viewBox="0 0 ${w} ${ht}" role="img" aria-label="${A.esc(o.label || "trend")}" style="display:block;max-width:100%">${tgt == null ? "" : `<line x1="${pad}" x2="${w - pad}" y1="${y(tgt).toFixed(1)}" y2="${y(tgt).toFixed(1)}" stroke="var(--muted)" stroke-dasharray="3 3" stroke-width="1"/>`}<polyline points="${pts}" fill="none" stroke="var(--accent)" stroke-width="${o.big ? 2.5 : 1.75}" stroke-linejoin="round"/>${o.big ? vals.map((v, i) => `<circle cx="${x(i).toFixed(1)}" cy="${y(v).toFixed(1)}" r="3.5" fill="var(--accent)"/>`).join("") : `<circle cx="${x(vals.length - 1).toFixed(1)}" cy="${y(vals[vals.length - 1]).toFixed(1)}" r="2.5" fill="var(--accent)"/>`}</svg>`);
  }
  function insightsRoute(el) {
    A.setContext({ kind: "reports", label: "Firm insights" });
    if (!insightUsers().some(([v]) => v === insightUser)) insightUser = "firm";
    const hist = (A.store.get("firmKpiHistory", insightUser) || { rows: [] }).rows.slice(-insightSpan), last = hist[hist.length - 1], prev = hist[hist.length - 2];
    const k = KPI.find((x) => x.id === insightKpi) || KPI[0], canSet = A.can("reports.firm", "all"), note = A.store.get("reportNotes", "insights|" + insightUser);
    const series = hist.map((r) => r[k.id]);
    A.render(el, h`<div class="page">
      ${A.ui.header({ title: "Firm insights", back: { label: "Reports", to: "reports" }, sub: `Month-end figures to ${last ? monthName(last.month) : "—"}. Targets are the firm's own, not outside benchmarks. Dashed line = target.`,
        actions: [A.ui.btn({ label: "Open catalogue report", to: "reports/billings" })], primary: { label: "✦ Explain change", a: "insights-explain", agent: true, key: "i" } })}
      ${A.ui.filters({ chips: [{ id: "3", label: "Quarter" }, { id: "6", label: "6 months" }], active: String(insightSpan), action: "insights-span", selects: [{ c: "insights-user", label: "Person", value: insightUser, options: insightUsers() }],
        right: h`<span class="muted small">Basis: activity date for time, bill date for fees</span>` })}
      ${hist.length < 3 ? A.ui.empty("Not enough data yet", "Trends appear after three month ends.") : h`
      <div class="grid g3" style="margin-bottom:14px">${KPI.map((x) => { const v = last[x.id], d = prev ? v - prev[x.id] : 0; return h`<div role="button" tabindex="0" class="card kb-item ${x.id === k.id ? "on" : ""}" data-a="insights-kpi" data-id="${x.id}" title="${x.def}" style="text-align:left;cursor:pointer">
        <div class="row"><b class="grow">${x.label}</b>${A.ui.status(onTarget(x, v) ? "done" : "due-soon", onTarget(x, v) ? "On target" : "Below target")}</div>
        <div style="font-size:20px;font-weight:600;margin:4px 0">${fmtKpi(x, v)} <span class="muted small">${d === 0 ? "no change" : `${d > 0 ? "▲" : "▼"} ${fmtKpi(x, Math.abs(d))}`}</span></div>
        ${spark(hist.map((r) => r[x.id]), target(x.id), { label: `${x.label} trend` })}
        <div class="muted small">Target ${canSet ? A.ui.inline({ name: "insights-target", id: x.id, value: x.unit === "$" ? (target(x.id) || 0) / 100 : target(x.id), display: fmtKpi(x, target(x.id)), type: "number" }) : fmtKpi(x, target(x.id))}</div></div>`; })}</div>
      <section class="card"><h2>${k.label} <span class="muted small">${k.def}</span></h2>
        ${spark(series, target(k.id), { w: 640, h: 180, pad: 14, big: true, label: `${k.label} by month` })}
        <div class="row" style="justify-content:space-between;max-width:640px">${hist.map((r) => h`<span class="muted small">${monthName(r.month).split(" ")[0].slice(0, 3)} · <b>${fmtKpi(k, r[k.id])}</b></span>`)}</div></section>
      ${note ? h`<section class="card" style="margin-top:14px"><h2>What changed ${A.ui.actor("agent", "Agent draft")}</h2><p>${note.text}</p><ul class="small">${note.cites.map((c) => h`<li>${c}</li>`)}</ul>
        <div class="row">${A.ui.btn({ label: "Dismiss", a: "reports-note-dismiss", id: note.id, small: true })}</div></section>` : ""}`}
    </div>`);
  }
  A.action("insights-explain", () => {
    const hist = (A.store.get("firmKpiHistory", insightUser) || { rows: [] }).rows.slice(-insightSpan), last = hist[hist.length - 1], prev = hist[hist.length - 2];
    if (!last || !prev) return A.toast("Not enough data to compare");
    const who = insightUser === "firm" ? "the firm" : A.user(insightUser).name;
    A.agent.run({ title: `Explain ${monthName(last.month)} insights`, steps: ["Comparing month-end figures", "Finding the matters and bills behind the moves"], done() {
      const moves = KPI.map((k) => ({ k, d: last[k.id] - prev[k.id] })).filter((x) => x.d).sort((a, b) => Math.abs(b.d / (prev[b.k.id] || 1)) - Math.abs(a.d / (prev[a.k.id] || 1)));
      const misses = KPI.filter((k) => !onTarget(k, last[k.id]));
      const text = `For ${who}, ${monthName(last.month)} vs ${monthName(prev.month)}: ${moves.slice(0, 3).map((x) => `${x.k.label.toLowerCase()} ${x.d > 0 ? "up" : "down"} ${fmtKpi(x.k, Math.abs(x.d))} to ${fmtKpi(x.k, last[x.k.id])}`).join("; ")}. ${misses.length ? `Below target: ${misses.map((k) => `${k.label.toLowerCase()} (${fmtKpi(k, last[k.id])} vs ${fmtKpi(k, target(k.id))})`).join(", ")}.` : "Every KPI is on target."}`;
      A.store.put("reportNotes", { id: "insights|" + insightUser, report: "insights", text, cites: [`Month-end snapshot ${last.month} and ${prev.month} (${who})`, "Delivered bills and payments in the period: Reports › Billings by responsible lawyer, Collections", "Accepted time entries by activity date: Reports › Unbilled work in progress"], at: A.now() });
      return { title: "Draft note ready on Firm insights" };
    } });
  });

  /* ---------- "Explain this report" (agent draft, no gate: it changes nothing) ---------- */
  A.action("reports-explain", (el) => {
    const out = lastOut, id = el.dataset.id; if (!out) return A.toast("Nothing to explain yet");
    A.agent.run({ title: `Explain: ${out.title}`, steps: ["Reading the result rows", "Writing it in plain words with the rows behind each claim"], done() {
      const rows = out.rows.filter((r) => r[0] !== "Total"), nc = out.cols.findIndex((c, i) => i > 0 && rows.some((r) => /^-?\$/.test(String(r[i]))));
      const val = (r) => +String(r[nc]).replace(/[^0-9.-]/g, "") || 0;
      const sorted = nc > 0 ? rows.slice().sort((a, b) => val(b) - val(a)) : rows, total = nc > 0 ? rows.reduce((s, r) => s + val(r), 0) : null;
      const top = sorted[0];
      const text = !rows.length ? `${out.title}: no rows, so there is nothing to act on as of ${A.date(A.today)}.`
        : nc > 0 ? `${out.title} has ${rows.length} row${rows.length === 1 ? "" : "s"} totalling ${A.money(total)} (${out.cols[nc].toLowerCase()}). The largest is ${top[0]}${top[1] && nc !== 1 ? ` (${top[1]})` : ""} at ${top[nc]}, ${total ? Math.round((val(top) / total) * 100) : 0}% of the total.${rows.some((r) => val(r) < 0) ? " At least one figure is negative: check it first." : ""}`
          : `${out.title} has ${rows.length} row${rows.length === 1 ? "" : "s"}; the first is ${rows[0].slice(0, 3).join(" · ")}.`;
      const cites = (nc > 0 ? sorted : rows).slice(0, 3).map((r) => `Row: ${r.slice(0, out.cols.length).join(" · ")}`);
      A.store.put("reportNotes", { id: "report|" + id, report: id, text, cites, at: A.now() });
      A.audit("report.explained", { note: out.title, actor: "agent" });
      return { title: "Explanation drafted on the report" };
    } });
  });
  A.action("reports-note-dismiss", (el) => { A.store.remove("reportNotes", el.dataset.id); A.toast("Dismissed"); });
  const explainCard = (id) => { const n = A.store.get("reportNotes", "report|" + id); return n ? h`<section class="card" style="margin-bottom:12px"><h2>In plain words ${A.ui.actor("agent", "Agent draft")} <span class="muted small">${A.dateTime(n.at)}</span></h2><p>${n.text}</p><ul class="small">${n.cites.map((c) => h`<li>${c}</li>`)}</ul>${A.ui.btn({ label: "Dismiss", a: "reports-note-dismiss", id: n.id, small: true })}</section>` : ""; };

  /* ---------- scheduled delivery (demo: no mail is sent; deliveries are logged) ---------- */
  A.seed("reportSchedules", [{ id: "rs-1", report: "trust-listing", title: "Trust listing by client", cadence: "monthly", day: "1", time: "08:00", start: "2026-09-01", end: "2027-08-31", format: "PDF", to: ["u-linda", "u-yohannes"], owner: "u-linda", status: "scheduled" }]);
  A.seed("reportDeliveries", [{ id: "rd-1", schedule: "rs-1", title: "Trust listing by client", at: "2026-10-01 08:00", to: ["u-linda", "u-yohannes"], format: "PDF", rows: 5 }]);
  const CADENCE = [["daily", "Every business day"], ["weekly", "Weekly"], ["monthly", "Monthly"]];
  const WEEKDAYS = [["1", "Monday"], ["2", "Tuesday"], ["3", "Wednesday"], ["4", "Thursday"], ["5", "Friday"]];
  /** Next run on or after today (Toronto calendar days): daily skips weekends; weekly on the chosen weekday; monthly on day N. */
  function nextRun(s) {
    let d = s.start > A.today ? s.start : A.today;
    for (let i = 0; i < 400 && d <= s.end; i++, d = A.addDays(d, 1)) {
      const wd = new Date(d + "T12:00:00").getDay();
      if (s.cadence === "daily" ? wd >= 1 && wd <= 5 : s.cadence === "weekly" ? String(wd) === String(s.day) : +d.slice(8) === +s.day) return d;
    }
    return null;
  }
  const schedules = () => A.store.where("reportSchedules", (s) => Array.isArray(s.to) && (s.owner === A.me().id || s.to.includes(A.me().id)));
  const staff = () => A.store.where("users", (u) => u.role !== "client");
  function scheduleModal(reportId, title, cur) {
    const s = cur || { cadence: "monthly", day: "1", time: "08:00", start: A.today, end: A.addDays(A.today, 364), format: "PDF", to: [A.me().id] };
    A.modal(`Schedule delivery · ${title}`, h`<div class="form">
      <label class="field">Cadence<select name="cadence">${CADENCE.map(([v, l]) => h`<option value="${v}" ${v === s.cadence ? A.raw("selected") : ""}>${l}</option>`)}</select></label>
      <label class="field">Day (weekday for weekly, 1–28 for monthly)<input name="day" class="input" value="${s.day}"></label>
      <label class="field">Time (Toronto)<input name="time" class="input" type="time" value="${s.time}"></label>
      <label class="field">Starts<input name="start" class="input" type="date" value="${s.start}"></label><label class="field">Ends<input name="end" class="input" type="date" value="${s.end}"></label>
      <label class="field">Format<select name="format">${["PDF", "CSV", "XLSX"].map((f) => h`<option ${f === s.format ? A.raw("selected") : ""}>${f}</option>`)}</select></label>
      <fieldset class="field"><legend>Recipients (inside the firm only)</legend>${staff().map((u) => h`<label class="small"><input type="checkbox" name="to-${u.id}" ${s.to.includes(u.id) ? A.raw("checked") : ""}> ${u.name}</label> `)}</fieldset></div>
      <p class="muted small">Demo: deliveries are logged here, no email is sent. Sending outside the firm needs a lawyer's approval (use "Send outside the firm").</p>`, [{ label: "Cancel" }, { label: cur ? "Save" : "Schedule", primary: true, run: (d) => {
      const v = A.formValues(d), to = staff().filter((u) => v["to-" + u.id]).map((u) => u.id);
      if (!to.length) { A.toast("Pick at least one recipient", "bad"); return false; }
      if (!(v.start && v.end && v.end >= v.start)) { A.toast("The end date must be after the start", "bad"); return false; }
      if (v.cadence === "weekly" && !WEEKDAYS.some(([n]) => n === String(v.day))) { A.toast("Weekly: enter a weekday 1 (Mon) to 5 (Fri)", "bad"); return false; }
      if (v.cadence === "monthly" && !(+v.day >= 1 && +v.day <= 28)) { A.toast("Monthly: enter a day from 1 to 28", "bad"); return false; }
      const row = { id: cur ? cur.id : A.id("rs"), report: reportId, title, cadence: v.cadence, day: String(v.day), time: v.time || "08:00", start: v.start, end: v.end, format: v.format, to, owner: cur ? cur.owner : A.me().id, status: "scheduled" };
      A.store.put("reportSchedules", row); A.audit("report.scheduled", { note: `${title}: ${v.cadence}, ${to.length} recipient(s)` }); A.toast(cur ? "Schedule saved" : "Scheduled", "ok");
    } }]);
  }
  A.action("reports-schedule", (el) => scheduleModal(el.dataset.id, el.dataset.title));
  A.action("reports-sched-edit", (el) => { const s = A.store.get("reportSchedules", el.dataset.id); if (s) scheduleModal(s.report, s.title, s); });
  const schedIds = (el) => String(el.dataset.ids || el.dataset.id || "").split(",").filter(Boolean).map((id) => A.store.get("reportSchedules", id)).filter((s) => s && (s.owner === A.me().id || A.can("reports.firm", "all")));
  A.action("reports-sched-send", (el) => {
    const ss = schedIds(el); if (!ss.length) return A.toast("Only the owner can send this schedule", "bad");
    ss.forEach((s) => { const r = REPORTS.find((x) => x.id === s.report), d = r && dataFor(r); let n = 0; try { n = d ? r.run(d).rows.length : 0; } catch (e) { n = 0; }
      A.store.put("reportDeliveries", { id: A.id("rd"), schedule: s.id, title: s.title, at: A.now(), to: s.to, format: s.format, rows: n }); A.audit("report.delivered", { note: `${s.title} to ${s.to.map((u) => A.user(u).name).join(", ")} (demo)` }); });
    A.selection("report-sched").clear(); A.toast(`${ss.length} report${ss.length === 1 ? "" : "s"} delivered (demo)`, "ok");
  });
  A.action("reports-sched-pause", (el) => { const ss = schedIds(el); ss.forEach((s) => A.store.put("reportSchedules", { id: s.id, status: s.status === "hold" ? "scheduled" : "hold" })); A.selection("report-sched").clear(); A.toast(ss.length ? "Updated" : "Only the owner can change this schedule", ss.length ? "ok" : "bad"); });
  A.action("reports-sched-delete", (el) => { const ss = schedIds(el); ss.forEach((s) => { A.store.remove("reportSchedules", s.id); A.audit("report.unscheduled", { note: s.title }); }); A.selection("report-sched").clear(); A.toast(ss.length ? "Schedule removed" : "Only the owner can remove this schedule", ss.length ? "ok" : "bad"); });
  function schedulesSection() {
    const ss = schedules(); if (!ss.length) return "";
    const ds = A.store.where("reportDeliveries", (d) => ss.some((s) => s.id === d.schedule)).sort((a, b) => b.at.localeCompare(a.at)).slice(0, 5);
    const when = (s) => `${(CADENCE.find((c) => c[0] === s.cadence) || [, s.cadence])[1]}${s.cadence === "weekly" ? ` on ${(WEEKDAYS.find((w) => w[0] === s.day) || [, s.day])[1]}` : s.cadence === "monthly" ? ` on day ${s.day}` : ""} at ${s.time}`;
    return h`<h2 style="font-size:13px;text-transform:uppercase;letter-spacing:.05em;color:var(--muted);margin:16px 0 8px">Scheduled delivery <span class="muted small" style="text-transform:none">demo</span></h2>
      ${A.ui.table([{ label: "Report", render: (s) => h`<b>${s.title}</b><div class="muted small">${when(s)} · ${s.format}</div>` }, { label: "To", render: (s) => s.to.map((u) => A.user(u).name).join(", ") },
        { label: "Next", render: (s) => (s.status === "hold" ? "—" : nextRun(s) ? A.ui.due(nextRun(s)) : A.ui.status("closed", "Ended")) }, { label: "Ends", render: (s) => h`${A.date(s.end)}${A.daysBetween(A.today, s.end) <= 30 ? h` ${A.ui.status("due-soon", "Renew soon")}` : ""}` },
        { label: "Status", render: (s) => A.ui.status(s.status) }, { label: "", render: (s) => A.ui.btn({ label: "Edit", a: "reports-sched-edit", id: s.id, small: true, key: "e", hint: false }) }], ss, { select: "report-sched" })}
      ${A.ui.bulkBar("report-sched", [{ label: "Send now", a: "reports-sched-send", primary: true }, { label: "Pause / resume", a: "reports-sched-pause" }, { label: "Remove", a: "reports-sched-delete", danger: true }])}
      ${ds.length ? h`<p class="muted small">Last deliveries: ${ds.map((d) => `${d.title} ${A.dateTime(d.at)} (${d.rows} rows, ${d.format})`).join(" · ")}</p>` : ""}`;
  }

  A.action("reports-group", (el) => { reportsFilter = el.dataset.id; A.refresh(); });
  A.action("reports-open", (el) => A.go(`reports/${el.dataset.id}`));
  A.module({ id: "reports", label: "Reports", icon: "▥", order: 60, cap: "reports.firm",
    smoke: ["reports", "reports/custom", "reports/insights", ...REPORTS.map((r) => `reports/${r.id}`)],
    routes: { reports: reportsRoute, "reports/insights": insightsRoute, "reports/:id": reportRoute } });
})();
