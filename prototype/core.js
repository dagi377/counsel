/* Counsel prototype core: store, permissions, gates, simulated agent, router, shell. No backend. */
(function () {
  "use strict";
  const KEY = "counsel-proto-v2";
  const A = (window.App = {});
  const SEED = window.SEED, ACC = window.ACCESS;

  /* ---------- html helpers ---------- */
  const esc = (v) => String(v ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  class Raw { constructor(s) { this.s = s; } toString() { return this.s; } }
  const flat = (v) => v == null || v === false ? "" : v instanceof Raw ? v.s : Array.isArray(v) ? v.map(flat).join("") : esc(v);
  A.esc = esc;
  A.raw = (s) => new Raw(String(s ?? ""));
  /** Tagged template: interpolations are escaped; arrays are joined; App.raw() passes through. Returns Raw. */
  A.h = (strs, ...vals) => new Raw(strs.reduce((o, s, i) => o + s + (i < vals.length ? flat(vals[i]) : ""), ""));
  /** Render a Raw/HTML into an element. */
  A.render = (el, raw) => { el.innerHTML = String(raw); };
  A.money = (n) => (n < 0 ? "-$" : "$") + Math.abs(+n || 0).toLocaleString("en-CA", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  /** "Oct 9, 2026" for a "YYYY-MM-DD" (or "YYYY-MM-DD HH:MM") date, read as a Toronto calendar day. */
  A.date = (d) => { if (!d) return ""; const s = String(d); const x = new Date(s.slice(0, 10) + "T12:00:00Z"); if (isNaN(x)) return s;
    try { return x.toLocaleDateString("en-CA", { timeZone: "America/Toronto", month: "short", day: "numeric", year: "numeric" }); } catch (e) { return x.toLocaleDateString("en-CA", { month: "short", day: "numeric", year: "numeric" }); } };
  A.today = SEED.firm.today;
  A.addDays = (d, n) => { const x = new Date(d + "T12:00:00"); x.setDate(x.getDate() + n); return x.toISOString().slice(0, 10); };
  A.daysBetween = (a, b) => Math.round((new Date(b + "T12:00:00") - new Date(a + "T12:00:00")) / 864e5);
  let seq = 0;
  A.id = (p) => `${p}-${Date.now().toString(36)}${(seq++).toString(36)}`;
  /** "YYYY-MM-DD HH:MM": the demo's today at the current Toronto wall-clock time (the firm's time zone). */
  A.tz = "America/Toronto";
  A.now = () => { let hm; try { hm = new Date().toLocaleTimeString("en-GB", { timeZone: A.tz, hour: "2-digit", minute: "2-digit", hour12: false }); } catch (e) { const t = new Date(); hm = `${String(t.getHours()).padStart(2, "0")}:${String(t.getMinutes()).padStart(2, "0")}`; } return `${A.today} ${hm}`; };

  /* ---------- store ---------- */
  let db = null; const subs = [];
  function fresh() { return { users: SEED.users, contacts: SEED.contacts, matters: SEED.matters, docs: SEED.docs, approvals: [], runs: [], audit: [], _seeded: {}, me: "u-linda" }; }
  function load() { try { const s = JSON.parse(localStorage.getItem(KEY) || "null"); if (s && s.users) return s; } catch (e) {} return fresh(); }
  function save() { try { localStorage.setItem(KEY, JSON.stringify(db)); } catch (e) {} }
  let pending = false;
  function changed() { save(); if (pending) return; pending = true; setTimeout(() => { pending = false; subs.forEach((f) => f()); }, 0); }
  A.store = {
    all: (c) => db[c] || (db[c] = []),
    get: (c, id) => (db[c] || []).find((x) => x.id === id),
    where: (c, f) => (db[c] || []).filter(f),
    put(c, obj) { const l = A.store.all(c); if (!obj.id) obj.id = A.id(c.slice(0, 3)); const i = l.findIndex((x) => x.id === obj.id); if (i >= 0) l[i] = { ...l[i], ...obj }; else l.unshift(obj); changed(); return A.store.get(c, obj.id); },
    remove(c, id) { db[c] = A.store.all(c).filter((x) => x.id !== id); changed(); },
    on: (f) => subs.push(f),
    reset() { db = fresh(); grantOverrides = {}; seeders.forEach(([c, rows]) => seedOne(c, rows)); readyFns.forEach((f) => { try { f(); } catch (e) { console.error(e); } }); changed(); },
  };
  const seeders = [];
  const readyFns = []; let ready = false;
  /** Run fn once the store has loaded (now, if it already has) and again after Reset demo. */
  A.onReady = (fn) => { readyFns.push(fn); if (ready) fn(); };
  function seedOne(c, rows) { if (db._seeded[c]) return; db._seeded[c] = 1; db[c] = (db[c] || []).concat(JSON.parse(JSON.stringify(typeof rows === "function" ? rows() : rows))); }
  /** Seed a module collection once (kept across reloads; restored by Reset demo). */
  A.seed = (c, rows) => { seeders.push([c, rows]); if (db) { seedOne(c, rows); save(); } };

  /* ---------- people, roles, permissions ---------- */
  const RANK = { none: 0, view: 1, own: 2, all: 3, approve: 4 };
  A.roles = ACC.roles; A.caps = ACC.caps; A.sod = ACC.sod;
  A.me = () => A.store.get("users", db.me);
  A.user = (id) => A.store.get("users", id) || { id, name: id === "agent" ? "Agent" : id, initials: "··" };
  A.roleName = (id) => (A.roles.find((r) => r.id === id) || { name: id }).name;
  let grantOverrides = {}; // role -> {cap: level}: approved role edits on top of access-data.js; cleared by Reset demo
  /** Replace the grant overrides: {roleId: {capId: level}}. A.grant reads them before access-data.js. */
  A.setGrantOverrides = (map) => { grantOverrides = JSON.parse(JSON.stringify(map || {})); };
  A.grant = (cap, roleId) => { const r = roleId || A.me().role; const o = grantOverrides[r]; return (o && o[cap]) || ((ACC.grants[r] || {})[cap]) || "none"; };
  /** level: "view" | "act" | "all" | "approve". "act" means own-or-better. */
  A.can = (cap, level = "view") => { const g = RANK[A.grant(cap)]; const need = { view: 1, act: 2, own: 2, all: 3, approve: 4 }[level] || 1; return g >= need; };
  /** Can the current user see this matter (team, grant level, restriction, walls)? */
  A.canMatter = (m, cap = "matters.view") => {
    if (!m) return false; const me = A.me();
    if (me.role === "client") return m.clientId === me.contactId;
    if ((m.wall || []).includes(me.id)) return false;
    if (A.store.where("breakglass", (b) => b.userId === me.id && b.matterId === m.id && b.active && (!b.expires || b.expires > A.now())).length) return true;
    const onTeam = (m.team || []).includes(me.id);
    if (m.restricted && !onTeam) return false;
    const g = A.grant(cap); return g === "all" || g === "approve" || g === "view" || (g === "own" && onTeam);
  };
  /** Matters the current user may see through a capability (default matters.view; billing pages pass "billing.view"). */
  A.matters = (f, cap = "matters.view") => A.store.all("matters").filter((m) => A.canMatter(m, cap) && (!f || f(m)));
  A.matter = (id) => { const m = A.store.get("matters", id); return A.canMatter(m) ? m : null; };
  A.contact = (id) => A.store.get("contacts", id) || { name: "Unknown" };
  const userHooks = [];
  /** Run fn(newUserId) after the demo user changes (modules clear their own selections and cursors here). */
  A.onUser = (fn) => userHooks.push(fn);
  A.setUser = (id) => { db.me = id; save(); Object.values(sels).forEach((x) => x.clear()); kbKey = null; userHooks.forEach((f) => { try { f(id); } catch (e) { console.error(e); } }); A.toast(`Now viewing as ${A.me().name} (${A.roleName(A.me().role)})`); const r = A.route(); A.go(A.canRoute(r) ? r : "home"); };

  /* ---------- audit ---------- */
  A.audit = (action, detail = {}) => { A.store.all("audit").unshift({ id: A.id("au"), at: A.now(), actor: detail.actor || db.me, action, ...detail }); changed(); };

  /* ---------- gates (human approvals) ---------- */
  const effects = {};
  /** Register what happens when a gate of this effect is approved: fn(payload, approval). */
  A.effect = (name, fn) => (effects[name] = fn);
  /**
   * Create an approval. spec: {kind, cap, title, summary, matterId, preparedBy ("agent" or user id), body (html string or Raw),
   * cites:[{doc, page, line, quote}], checks:[{label, ok}], effect, payload, returnEffect, assignee (one named approver),
   * risk ("low"|"medium"|"high", optional), due ("YYYY-MM-DD", optional: when the decision is needed)}
   */
  A.gate = (spec) => {
    const g = A.store.put("approvals", { id: A.id("ap"), status: "pending", created: A.now(), preparedBy: spec.preparedBy || "agent", cites: [], checks: [], ...spec, body: String(spec.body || "") });
    A.audit("gate.created", { target: g.id, matterId: g.matterId || null, note: g.title, actor: g.preparedBy });
    return g;
  };
  A.canApprove = (g) => {
    const me = A.me();
    if (g.status !== "pending") return { ok: false, why: `Already ${g.status}` };
    if (!A.can(g.cap, "approve")) return { ok: false, why: `${A.roleName(me.role)} cannot approve this (${g.cap})` };
    if (g.preparedBy === me.id) return { ok: false, why: "You prepared this; someone else must approve it" };
    if (g.assignee && g.assignee !== me.id) return { ok: false, why: `Assigned to ${A.user(g.assignee).name}` };
    if (g.matterId && !A.canMatter(A.store.get("matters", g.matterId))) return { ok: false, why: "You are not on this matter" };
    if (g.cap === "trust.authorize" && !me.signatory) return { ok: false, why: "Only designated trust signatories can authorize" };
    if ((g.checks || []).some((c) => c.ok === false && c.blocking)) return { ok: false, why: "A blocking check failed" };
    return { ok: true };
  };
  /** Approvals that need a fresh sign-in (step-up) first: money movement, matter close and access changes. */
  A.needsStepUp = (g) => !!g && (/^(trust|billing)\./.test(g.cap) || /trust|bill/.test(g.kind || "") || g.cap === "matters.close" || /^admin\.(roles|users|security|break_glass_approve)$/.test(g.cap));
  let stepUpHook = null, stepUpWait = null; // stepUpWait: approvals queued behind one open step-up prompt
  /** Register the step-up prompt: fn(gate, done) calls done() once the person has re-authenticated (may be synchronous). */
  A.onStepUp = (fn) => { stepUpHook = fn; };
  A.approve = (id, note = "") => {
    const g = A.store.get("approvals", id); const c = A.canApprove(g);
    if (!c.ok) return A.toast(c.why, "bad");
    if (stepUpHook && A.needsStepUp(g)) {
      const mo = document.getElementById("modal"); if (stepUpWait && mo && mo.hidden) stepUpWait = null; // prompt dismissed without a decision
      if (stepUpWait) { stepUpWait.push([id, note]); return; }
      stepUpWait = [[id, note]];
      return stepUpHook(g, () => { const q = stepUpWait || []; stepUpWait = null; q.forEach(([i, n]) => decide(i, n)); });
    }
    decide(id, note);
  };
  function decide(id, note) {
    const g = A.store.get("approvals", id); const c = A.canApprove(g);
    if (!c.ok) return A.toast(c.why, "bad");
    A.store.put("approvals", { id, status: "approved", decidedBy: db.me, decidedAt: A.now(), note });
    A.audit("gate.approved", { target: id, matterId: g.matterId || null, note: g.title });
    try { effects[g.effect] && effects[g.effect](g.payload || {}, g); } catch (e) { console.error(e); }
    A.toast(`Approved: ${g.title}`, "ok");
  }
  /** The step-up prompt was cancelled: drop the approvals waiting on it. */
  A.cancelStepUp = () => { stepUpWait = null; };
  A.sendBack = (id, note = "") => {
    const g = A.store.get("approvals", id);
    if (!A.can(g.cap, "approve") && g.preparedBy !== db.me) return A.toast("You cannot act on this approval", "bad");
    A.store.put("approvals", { id, status: "returned", decidedBy: db.me, decidedAt: A.now(), note });
    A.audit("gate.returned", { target: id, matterId: g.matterId || null, note: note || g.title });
    if (g.preparedBy === "agent") A.agent.run({ title: `Revise: ${g.title}`, matterId: g.matterId, steps: ["Reading the reviewer's note", "Revising the draft", "Re-running checks"], done: () => { const { id: _old, ...rest } = g; A.gate({ ...rest, status: "pending", title: g.title.replace(/^(Revised: )?/, "Revised: "), created: A.now(), note: undefined, decidedBy: undefined, decidedAt: undefined }); } });
    A.toast("Sent back with your note");
  };
  A.pendingFor = () => A.store.where("approvals", (g) => g.status === "pending" && A.can(g.cap, "approve") && (!g.matterId || A.canMatter(A.store.get("matters", g.matterId))));

  /* ---------- simulated agent ---------- */
  const live = {}; // run id -> {t, i, timer}: in-memory task state so a run can pause, resume and stop
  const stepLabel = (s) => (s && typeof s === "object" ? s.label : String(s ?? ""));
  A.agent = {
    /**
     * Run a visible agent task: {title, matterId, steps:[...], ms, done(), cites, cap}.
     * Plan mode (t.plan true, or any step given as an object {label, gate, cap, title, summary, body, cites, checks}):
     * the run stores the whole plan, shows the live step, can be stopped (status "stopped", done() not called),
     * and a step with gate:true pauses the run (status "waiting") behind an approval; approving it resumes the run.
     * done() may return {title, cites:[...], outputs:[{label, go}]}; they are shown on the finished run.
     */
    run(t) {
      const held = A.agent.paused(t.matterId);
      if (held) { A.toast(held, "bad"); A.audit("agent.held", { actor: "agent", matterId: t.matterId || null, note: t.title }); return null; }
      const planned = !!t.plan || (t.steps || []).some((s) => s && typeof s === "object");
      const plan = planned ? (t.steps || []).map((s) => (typeof s === "object" ? { label: s.label, gate: !!s.gate } : { label: String(s), gate: false })) : undefined;
      const r = A.store.put("runs", { id: A.id("run"), title: t.title, matterId: t.matterId || null, steps: (t.steps || []).map(stepLabel), step: 0, status: "running", started: A.now(), by: db.me, ...(planned ? { plan, cites: t.cites || [] } : {}) });
      live[r.id] = { t, i: 0, planned, timer: null };
      if (t.onStart) { try { t.onStart(r); } catch (e) { console.error(e); } }
      schedule(r.id);
      return r;
    },
    /** Stop a running or waiting run. done() is not called; a pending step approval is withdrawn. */
    stop(id) {
      const r = A.store.get("runs", id); const L = live[id];
      if (!r || !["running", "waiting"].includes(r.status)) return;
      if (L && L.timer) clearTimeout(L.timer);
      delete live[id];
      if (r.gateId) { const g = A.store.get("approvals", r.gateId); if (g && g.status === "pending") A.store.put("approvals", { id: g.id, status: "returned", decidedBy: db.me, decidedAt: A.now(), note: "Run stopped" }); }
      A.store.put("runs", { id, status: "stopped", finished: A.now(), result: `Stopped at step ${Math.max(1, r.step)} of ${r.steps.length}`, stoppedBy: db.me });
      A.audit("agent.stopped", { target: id, note: r.title });
    },
    /** Resume a run paused at a gate step (called when its step approval is approved). */
    resume(id) {
      const r = A.store.get("runs", id); if (!r || r.status !== "waiting") return;
      if (!live[id]) { A.store.put("runs", { id, status: "stopped", finished: A.now(), result: "Step approved after the page reloaded; run it again to finish" }); return; }
      A.store.put("runs", { id, status: "running", gateId: null, plan: (r.plan || []).map((p, n) => (n === r.step - 1 ? { ...p, approved: A.now() } : p)) });
      schedule(id);
    },
    /** Post a "what the agent will do" preview in the agent panel; Run starts agent.run(t), Cancel discards it. */
    preview(t, intro) {
      openAgent(true);
      const k = A.id("pv"); previews[k] = t;
      chatAdd("agent", A.raw(String(A.h`${intro || "Here is what I will do. Nothing starts until you press Run."}`)), planHtml(t, k));
      return k;
    },
    stepLabel,
    /** Why new runs are held ("firm" pause, or a pause on this matter), else "". */
    paused(matterId) {
      const on = (id) => { const p = A.store.get("agentPauses", id); return p && p.active; };
      if (on("firm")) return "The agent is paused for the firm; resume it on Activity to run this";
      if (matterId && on(matterId)) return "The agent is paused on this matter; resume it on Activity to run this";
      return "";
    },
  };
  const previews = {};
  function planHtml(t, k) {
    return A.h`<div class="card" style="padding:8px 10px;margin-top:6px" data-preview="${k}"><b class="small">${t.title}</b>
      <ol class="small" style="margin:6px 0 8px 18px;padding:0">${(t.steps || []).map((s) => A.h`<li>${stepLabel(s)}${s && s.gate ? A.h` <span class="badge gate">needs approval</span>` : ""}</li>`)}</ol>
      <div class="row"><button class="btn small primary" data-a="agent-preview-run" data-id="${k}">Run</button><button class="btn small" data-a="agent-preview-cancel" data-id="${k}">Cancel</button></div></div>`;
  }
  function schedule(id) {
    const L = live[id]; if (!L) return;
    L.timer = setTimeout(() => tick(id), A.fast ? 1 : L.t.ms || 450);
  }
  function tick(id) {
    const L = live[id]; const r = A.store.get("runs", id);
    if (!L || !r || r.status !== "running") return;
    const t = L.t; L.i++;
    if (L.i <= r.steps.length) {
      const s = (t.steps || [])[L.i - 1];
      A.store.put("runs", { id, step: L.i });
      if (L.planned && s && typeof s === "object" && s.gate) {
        const g = A.gate({ kind: s.kind || "agent-step", cap: s.cap || t.cap || "documents.release", title: s.title || `${t.title}: ${s.label}`, summary: s.summary || `The agent paused this run at step ${L.i} of ${r.steps.length} and will continue when this is approved.`,
          matterId: t.matterId || null, preparedBy: "agent", body: s.body || "", cites: s.cites || t.cites || [], checks: s.checks || [], effect: "agent-step", payload: { runId: id, step: L.i } });
        A.store.put("runs", { id, status: "waiting", gateId: g.id });
        return;
      }
      schedule(id); return;
    }
    delete live[id];
    let out; try { out = t.done && t.done(); } catch (e) { console.error(e); }
    A.store.put("runs", { id, status: "done", finished: A.now(), result: out && out.title ? out.title : t.result || "", ...(out && out.cites ? { cites: out.cites } : {}), ...(out && out.outputs ? { outputs: out.outputs } : {}) });
    A.audit("agent.run", { actor: "agent", target: id, note: t.title });
  }
  A.effect("agent-step", (p) => A.agent.resume(p.runId));
  const intents = [], suggesters = {};
  /** Register a chat/command intent: {match: RegExp, label, run(text, ctx) -> reply|undefined}. reply: {text, cites, actions:[{label, go|run}], preview: agent task (shows the plan with Run / Cancel)} */
  A.intent = (i) => intents.push(i);
  /** Register suggested prompts for a context kind ("home", "matter", "lead", ...): fn(ctx) -> [strings] */
  A.suggest = (kind, fn) => (suggesters[kind] = suggesters[kind] || []).push(fn);
  let ctx = { kind: "home" };
  /** Set what the agent panel is "looking at": {kind, id, label} */
  A.setContext = (c) => { ctx = c || { kind: "home" }; renderAgentHead(); };
  A.context = () => ctx;
  function answer(text) {
    for (const i of intents) { if (i.match.test(text)) { const r = i.run(text, ctx); if (r) return r; } }
    const q = text.toLowerCase().split(/\W+/).filter((w) => w.length > 3);
    const hits = A.matters().filter((m) => q.some((w) => (m.name + " " + m.summary + " " + m.area).toLowerCase().includes(w))).slice(0, 4);
    if (hits.length) return { text: `I found ${hits.length} matter${hits.length > 1 ? "s" : ""} that look related. Open one and ask again, and I will answer from its documents with citations.`, actions: hits.map((m) => ({ label: `${m.number} ${m.name}`, go: `matters/${m.id}` })) };
    return { text: "I can open matters, draft letters, check conflicts, compute deadlines, prepare bills and trust requests. Everything I prepare goes to a person for approval. Try one of the suggestions below." };
  }

  /* ---------- modules, routes, matter tabs, actions ---------- */
  const mods = [], tabs = [], actions = {};
  /** Register a module: {id, label, icon, cap, order, nav:true|false, badge(), routes: {"path": fn(el, params), "path/:id": fn}} */
  A.module = (m) => { mods.push(m); mods.sort((a, b) => (a.order || 50) - (b.order || 50)); };
  /** Register a tab on the matter workspace: {id, label, cap, order, render(el, matter)} */
  A.matterTab = (t) => { tabs.push(t); tabs.sort((a, b) => (a.order || 50) - (b.order || 50)); };
  A.matterTabs = (m) => tabs.filter((t) => !t.cap || A.can(t.cap));
  /** Delegated click actions: elements with data-a="name" call fn(el, event). */
  A.action = (name, fn) => (actions[name] = fn);
  /** Delegated change/input handlers: elements with data-c="name" call fn(el, event) on change. */
  const changes = {};
  A.change = (name, fn) => (changes[name] = fn);
  /** Delegated input handlers: data-i="name" calls fn(el, event) on every keystroke. */
  const inputs = {};
  A.input = (name, fn) => (inputs[name] = fn);
  /** Live search: data-live="name" (with an id) calls fn(value, el) on each keystroke, re-renders and keeps the caret. */
  const lives = {};
  A.live = (name, fn) => (lives[name] = fn);
  let current = "home";
  function match(path) {
    for (const m of mods) for (const [pat, fn] of Object.entries(m.routes || {})) {
      const pp = pat.split("/"), xp = path.split("/"); if (pp.length !== xp.length) continue;
      const params = {}; if (pp.every((p, i) => (p[0] === ":" ? ((params[p.slice(1)] = decodeURIComponent(xp[i])), true) : p === xp[i]))) return { mod: m, fn, params };
    }
    return null;
  }
  A.canRoute = (p) => { const r = match(p); return r && (!r.mod.cap || A.can(r.mod.cap)); };
  A.route = () => current;
  const trail = []; // visited routes, newest last (for u / Back and palette "Recent")
  A.go = (path) => { path = path || "home"; if (path !== current) { trail.push(current); if (trail.length > 30) trail.shift(); kbKey = null; } current = path; render(); const main = document.getElementById("main"); if (main) main.scrollTop = 0; };
  /** Go back to the previous route (u). */
  A.back = () => { const p = trail.pop(); current = p || "home"; kbKey = null; render(); };
  A.recent = () => [...new Set(trail.slice().reverse())].filter((p) => p !== current).slice(0, 5);
  A.refresh = () => render();
  A._mods = mods;

  /* ---------- UI helpers ---------- */
  A.ui = {
    badge: (text, tone = "") => A.h`<span class="badge ${tone}">${text}</span>`,
    avatar: (uid) => { const u = A.user(uid); return A.h`<span class="av ${uid === "agent" ? "agent" : ""}" title="${u.name}">${uid === "agent" ? "✦" : u.initials}</span>`; },
    /** Empty state: title, optional hint (Raw or text), optional action {label, a|to, key}. Same look everywhere. */
    empty: (title, hint = "", action) => A.h`<div class="empty"><b>${title}</b>${hint}${action ? A.h`<div style="margin-top:10px">${A.ui.btn({ small: true, ...action })}</div>` : ""}</div>`,
    /** cols: [{key|label, render(row)}], rows, opts {onRow: action name, rowId: fn} */
    /**
     * cols: [{key|label, render(row), num}], rows, opts {onRow: action name, rowId: fn, empty, emptyHint,
     * select: selection name (adds a checkbox column; x toggles the focused row), active: id of the open row}.
     * Clickable rows join the keyboard layer (j/k, Enter).
     */
    table(cols, rows, opts = {}) {
      if (!rows.length) return A.ui.empty(opts.empty || "Nothing here yet", opts.emptyHint || "");
      const rid = (r) => String(opts.rowId ? opts.rowId(r) : r.id);
      const sel = opts.select, ids = sel ? rows.map(rid) : [], S = sel && A.selection(sel), allOn = sel && ids.length && ids.every((i) => S.has(i));
      return A.h`<div class="tblwrap"><table class="tbl"><thead><tr>${sel ? A.h`<th class="selc"><span role="checkbox" tabindex="-1" aria-checked="${allOn ? "true" : "false"}" aria-label="Select all" class="cbx ${allOn ? "on" : ""}" data-a="sel-all" data-sel="${sel}" data-ids="${ids.join(",")}" title="Select all"></span></th>` : ""}${cols.map((c) => A.h`<th class="${c.num ? "num" : ""}">${c.label}</th>`)}</tr></thead><tbody>${rows.map((r) => { const id = rid(r);
        return A.h`<tr ${opts.onRow ? A.raw(`data-a="${esc(opts.onRow)}" data-id="${esc(id)}" class="click${opts.active != null && String(opts.active) === id ? " on" : ""}${S && S.has(id) ? " picked" : ""}"`) : S && S.has(id) ? A.raw('class="picked"') : ""}>${sel ? A.h`<td class="selc">${A.ui.check(sel, id)}</td>` : ""}${cols.map((c) => A.h`<td class="${c.num ? "num" : ""}">${c.render ? c.render(r) : r[c.key]}</td>`)}</tr>`; })}</tbody></table></div>`;
    },
    tabs: (items, active, action) => A.h`<div class="tabs" role="tablist">${items.map((t) => A.h`<button role="tab" class="${t.id === active ? "on" : ""}" data-a="${action}" data-id="${t.id}">${t.label}${t.count ? A.h` <span class="cnt">${t.count}</span>` : ""}</button>`)}</div>`,
    stat: (label, value, sub = "", tone = "") => A.h`<div class="stat ${tone}"><small>${label}</small><b>${value}</b>${sub ? A.h`<span>${sub}</span>` : ""}</div>`,
    cite: (c) => A.h`<button class="cite" data-a="open-cite" data-doc="${c.doc}" data-page="${c.page || ""}" data-line="${c.line || ""}" title="${c.quote || ""}">${c.label || c.doc}${c.page ? ` p.${c.page}` : ""}${c.line ? ` l.${c.line}` : ""}</button>`,
    /** Full approval card used in the inbox and inline on records. */
    gateCard(g, opts = {}) {
      const c = A.canApprove(g); const m = g.matterId && A.store.get("matters", g.matterId);
      const ap = opts.approveAction || "gate-approve", bk = opts.backAction || "gate-back";
      const tone = g.status === "approved" ? "ok" : g.status === "returned" ? "warn" : "gate";
      return A.h`<article class="gate ${g.status}" id="gate-${g.id}">
        <header>${A.ui.status(g.status === "pending" ? "needs-approval" : g.status)}<b>${g.title}</b>
          ${g.risk ? A.ui.status(g.risk, `${g.risk[0].toUpperCase()}${g.risk.slice(1)} risk`, { high: "bad", medium: "warn" }[g.risk] || "") : ""}${g.due ? A.ui.due(g.due) : ""}
          <span class="muted">${g.preparedBy === "agent" ? A.ui.actor("agent", "Agent prepared") : A.h`Prepared by ${A.user(g.preparedBy).name}`} · ${m ? `${m.number} · ` : ""}${A.dateTime(g.created)}</span></header>
        ${g.summary ? A.h`<p>${g.summary}</p>` : ""}
        ${opts.compact ? "" : A.h`${g.body ? A.h`<div class="draft">${A.raw(g.body)}</div>` : ""}
          ${(g.checks || []).length ? A.h`<ul class="checks">${g.checks.map((k) => A.h`<li class="${k.ok ? "ok" : "bad"}">${k.ok ? "✓" : "✕"} ${k.label}</li>`)}</ul>` : ""}
          ${(g.cites || []).length ? A.h`<div class="cites"><span class="muted">Sources</span> ${g.cites.map(A.ui.cite)}</div>` : ""}`}
        <footer>${g.status === "pending"
          ? A.h`<span class="muted small">Approver needs: ${A.capName(g.cap)}</span><span class="grow"></span>
            ${c.ok ? A.h`${A.ui.actor("person", "Your decision")}<button class="btn" data-a="${bk}" data-id="${g.id}" data-key="b" title="Send back with a note (b)">Send back</button><button class="btn primary" data-a="${ap}" data-id="${g.id}" data-key="a" title="Approve (a)">Approve</button>` : A.h`<span class="muted small">${c.why}</span>`}`
          : A.h`<span class="muted small">${(A.STATUS[g.status] || [g.status])[0]} by ${A.user(g.decidedBy).name} · ${A.dateTime(g.decidedAt)}${g.note ? ` · "${g.note}"` : ""}</span>`}</footer>
      </article>`;
    },
    /** Run card. opts (optional): {full: true} shows the whole plan; the default is the compact live view. */
    runCard(r, opts) {
      const o = opts && typeof opts === "object" ? opts : {};
      const live = r.status === "running" || r.status === "waiting";
      const pct = r.steps.length ? Math.round((Math.min(r.step, r.steps.length) / r.steps.length) * 100) : 100;
      const g = r.gateId && A.store.get("approvals", r.gateId);
      const line = r.status === "running" ? (r.steps[Math.max(0, r.step - 1)] || "Starting") + "…"
        : r.status === "waiting" ? `Paused for approval: ${r.steps[r.step - 1] || ""}`
        : (r.result || (r.status === "stopped" ? "Stopped" : "Done")) + " · " + (r.finished || "");
      const plan = r.plan && (o.full || live) ? A.h`<ol class="small" style="margin:6px 0 4px 18px;padding:0">${r.plan.map((p, n) => {
        const st = n + 1 < r.step || (n + 1 === r.step && r.status === "done") || (r.status === "done") ? "done" : n + 1 === r.step ? (r.status === "waiting" ? "waiting" : r.status === "stopped" ? "stopped" : "now") : "todo";
        return A.h`<li style="${st === "now" || st === "waiting" ? "font-weight:600" : st === "todo" || st === "stopped" ? "color:var(--muted)" : ""}">${st === "done" ? "✓ " : st === "now" ? "▸ " : st === "waiting" ? "◆ " : ""}${p.label}${p.gate ? A.h` <span class="badge ${p.approved ? "ok" : "gate"}">${p.approved ? "approved" : "approval"}</span>` : ""}</li>`; })}</ol>` : "";
      const tail = r.status === "done" && ((r.cites || []).length || (r.outputs || []).length) ? A.h`<div class="cites">${(r.outputs || []).map((x) => A.h`<button class="btn small" data-a="go" data-to="${x.go}">${x.label}</button>`)}${(r.cites || []).map(A.ui.cite)}</div>` : "";
      return A.h`<div class="run ${r.status}"><span class="av agent">✦</span><div class="grow"><b>${r.title}</b>
        <div class="muted small">${line}</div>
        ${live ? A.h`<div class="bar"><i style="width:${pct}%"></i></div>` : ""}${plan}
        ${r.status === "waiting" && g && g.status === "pending" ? A.h`<div class="row wrap small">${A.ui.badge("Needs approval", "gate")}<button class="btn small" data-a="go" data-to="approvals/${g.id}">Review step</button></div>` : ""}
        ${tail}
        ${r.plan ? A.h`<div class="row wrap" style="margin-top:4px">${live ? A.h`<button class="btn small danger" data-a="agent-stop" data-id="${r.id}">Stop</button>` : ""}${o.full ? "" : A.h`<button class="btn small" data-a="go" data-to="activity/run/${r.id}">Details</button>`}</div>` : ""}</div></div>`;
    },
  };
  A.capName = (id) => (A.caps.find((c) => c.id === id) || { name: id }).name;
  A.toast = (msg, tone = "") => { const t = document.createElement("div"); t.className = `toast ${tone}`; t.textContent = msg; document.getElementById("toasts").appendChild(t); setTimeout(() => t.remove(), 3200); };
  /** Modal dialog: App.modal(title, bodyRaw, [{label, primary, run()}]) */
  A.modal = (title, body, buttons = [{ label: "Close" }]) => {
    const d = document.getElementById("modal");
    d.innerHTML = String(A.h`<div class="mbox" role="dialog" aria-modal="true" aria-label="${title}"><h3>${title}</h3><div class="mbody">${body}</div><div class="mfoot">${buttons.map((b, i) => A.h`<button class="btn ${b.primary ? "primary" : ""}" data-mb="${i}">${b.label}</button>`)}</div></div>`);
    d.hidden = false;
    d.querySelectorAll("[data-mb]").forEach((el) => (el.onclick = () => { const b = buttons[+el.dataset.mb]; const keep = b.run && b.run(d) === false; if (!keep) d.hidden = true; }));
    const f = d.querySelector("input,textarea,select"); if (f) f.focus();
  };
  A.closeModal = () => (document.getElementById("modal").hidden = true);
  /** Read form values from inside an element: {name: value} for [name] inputs; checkboxes give a boolean, a radio group gives its checked value ("" if none). */
  A.formValues = (root) => { const o = {}; root.querySelectorAll("[name]").forEach((el) => { if (el.type === "radio") { if (el.checked) o[el.name] = el.value; else if (!(el.name in o)) o[el.name] = ""; } else o[el.name] = el.type === "checkbox" ? el.checked : el.value; }); return o; };


  /* ---------- design system: App.ui.* (status chips, page header, filter bar, split view, bulk bar, inline edit) ---------- */
  /** One status vocabulary for the whole app: status word -> [label, tone]. Tones: agent (teal), gate (amber, a person must act), ok, warn, bad, "" (neutral). */
  A.STATUS = {
    "needs-approval": ["Needs approval", "gate"], pending: ["Needs approval", "gate"], "needs-you": ["Needs you", "gate"], "to-review": ["To review", "gate"], waiting: ["Waiting on approval", "gate"],
    running: ["Agent working", "agent"], "agent-working": ["Agent working", "agent"], "agent-prepared": ["Agent prepared", "agent"], suggested: ["Agent suggested", "agent"],
    draft: ["Draft", ""], new: ["New", ""], open: ["Open", ""], todo: ["To do", ""], "in-progress": ["In progress", "warn"], scheduled: ["Scheduled", ""], sent: ["Sent", ""],
    approved: ["Approved", "ok"], done: ["Done", "ok"], complete: ["Complete", "ok"], completed: ["Complete", "ok"], paid: ["Paid", "ok"], reconciled: ["Reconciled", "ok"], confirmed: ["Confirmed", "ok"], active: ["Active", "ok"], filed: ["Filed", "ok"],
    returned: ["Sent back", "warn"], unconfirmed: ["Unconfirmed", "warn"], "due-soon": ["Due soon", "warn"], partial: ["Partly paid", "warn"], hold: ["On hold", "warn"],
    overdue: ["Overdue", "bad"], blocked: ["Blocked", "bad"], failed: ["Failed", "bad"], rejected: ["Rejected", "bad"], conflict: ["Conflict", "bad"], unbalanced: ["Out of balance", "bad"],
    superseded: ["Superseded", ""], expired: ["Expired", "bad"], "in-review": ["In review", "warn"], stale: ["Stale: source changed", "bad"], reassigned: ["Reassigned", ""], escalated: ["Escalated", "bad"],
    "high-risk": ["High risk", "bad"], "medium-risk": ["Medium risk", "warn"], "low-risk": ["Low risk", ""],
    stopped: ["Stopped", ""], closed: ["Closed", ""], archived: ["Archived", ""], dismissed: ["Dismissed", ""], declined: ["Declined", ""], inactive: ["Inactive", ""],
  };
  const statusKey = (s) => String(s ?? "").trim().toLowerCase().replace(/[\s_]+/g, "-");
  /** Kind of hand-off for a record: who acts next. "agent" = the agent did it, "person" = a person must act. */
  const kbdText = (k) => String(k).split("+").map((p) => ({ mod: /Mac/.test(navigator.platform || "") ? "⌘" : "Ctrl", enter: "Enter", esc: "Esc", up: "↑", down: "↓" }[p.toLowerCase()] || p)).join(" ");
  Object.assign(A.ui, {
    /** Status chip from the shared vocabulary. ui.status("paid"), ui.status("Needs approval"), ui.status("custom", "label", "tone"). */
    status(s, label, tone) { const k = A.STATUS[statusKey(s)]; return A.h`<span class="badge chip ${tone ?? (k ? k[1] : "")}">${label || (k ? k[0] : s)}</span>`; },
    /** Who acts next: ui.actor("agent") -> "✦ Agent did this"; ui.actor("person", "Lawyer approves") -> amber "Needs a person". */
    actor(kind, text) { return kind === "agent" ? A.h`<span class="actor agent" title="Done by the agent">✦ ${text || "Agent prepared"}</span>` : A.h`<span class="actor person" title="A person must act">● ${text || "Needs a person"}</span>`; },
    /** Keyboard hint: ui.kbd("n"), ui.kbd("mod+k"). */
    kbd: (k) => A.h`<kbd>${kbdText(k)}</kbd>`,
    /** Button with optional shortcut: {label, a, id, to, key, primary, agent, danger, small, disabled, why, data:{k:v}, title}. key="n" makes the key press it; tooltip shows the key. */
    btn(o) {
      const data = Object.entries(o.data || {}).map(([k, v]) => ` data-${esc(k)}="${esc(v)}"`).join("");
      const tip = (o.disabled && o.why) || o.title || (o.key ? `${o.label} (${kbdText(o.key)})` : "");
      const cls = ["btn", o.primary && "primary", o.agent && "agent", o.danger && "danger", o.small && "small"].filter(Boolean).join(" ");
      return A.raw(`<button type="button" class="${cls}"${o.a ? ` data-a="${esc(o.a)}"` : o.to ? ` data-a="go" data-to="${esc(o.to)}"` : ""}${o.id != null ? ` data-id="${esc(o.id)}"` : ""}${o.to && o.a ? ` data-to="${esc(o.to)}"` : ""}${o.key ? ` data-key="${esc(o.key)}"` : ""}${data}${tip ? ` title="${esc(tip)}"` : ""}${o.disabled ? " disabled" : ""}>${esc(o.label)}${o.key && o.hint !== false ? `<kbd class="kh">${esc(kbdText(o.key))}</kbd>` : ""}</button>`);
    },
    /** Page header, same on every screen: {title, status, sub, back:{label,to}, meta: raw, actions:[raw|btn opts], primary: btn opts}. Primary sits on the right. */
    header(o) {
      const act = (x) => (x instanceof Raw ? x : A.ui.btn(x));
      return A.h`<header class="page-h">
        ${o.back ? A.h`<button class="crumb" data-a="go" data-to="${o.back.to}" data-key="u" title="Back (u)">← ${o.back.label}</button>` : ""}
        <div class="ph-title"><h1>${o.title}</h1>${o.status ? (o.status instanceof Raw ? o.status : A.ui.status(o.status)) : ""}${o.meta || ""}</div>
        <span class="grow"></span><div class="ph-acts">${(o.actions || []).map(act)}${o.primary ? act({ primary: true, ...(o.primary instanceof Raw ? {} : o.primary) }) : ""}</div>
        ${o.sub ? A.h`<div class="sub">${o.sub}</div>` : ""}</header>`;
    },
    /**
     * Filter bar: {search:{c, value, placeholder}, chips:[{id,label,count}], active, action, selects:[{c,value,options:[[v,label]],label}], right: raw}.
     * Search fires App.change(c) on Enter / blur ("/" focuses it). Chips fire App.action(action) with data-id.
     */
    filters(o) {
      return A.h`<div class="fbar" role="search">
        ${o.search ? A.h`<label class="fsearch"><span aria-hidden="true">⌕</span><input type="search" class="finput" data-c="${o.search.c}" data-key="/" value="${o.search.value || ""}" placeholder="${o.search.placeholder || "Filter"}" aria-label="${o.search.placeholder || "Filter"}"><kbd class="kh">/</kbd></label>` : ""}
        ${(o.chips || []).length ? A.h`<div class="fchips">${o.chips.map((c) => A.h`<button type="button" class="fchip ${c.id === o.active ? "on" : ""}" data-a="${o.action}" data-id="${c.id}" aria-pressed="${c.id === o.active ? "true" : "false"}">${c.label}${c.count != null ? A.h` <span class="n">${c.count}</span>` : ""}</button>`)}</div>` : ""}
        ${(o.selects || []).map((s) => A.h`<select class="fsel" data-c="${s.c}" aria-label="${s.label || s.c}">${s.options.map(([v, l]) => A.h`<option value="${v}" ${String(v) === String(s.value ?? "") ? A.raw("selected") : ""}>${l}</option>`)}</select>`)}
        <span class="grow"></span>${o.right || ""}</div>`;
    },
    /** List -> detail split view. list and detail are Raw; detail null shows a hint. On narrow screens the detail stacks below. */
    split(list, detail, o = {}) {
      return A.h`<div class="split ${o.wide ? "wide" : ""}"><div class="split-list">${list}</div><div class="split-detail">${detail || A.h`<div class="empty"><b>${o.emptyTitle || "Pick an item"}</b>${o.emptyHint || A.raw("Use <kbd>j</kbd> <kbd>k</kbd> to move and <kbd>Enter</kbd> to open.")}</div>`}</div></div>`;
    },
    /** Compact list rows for the left side of a split view: rows [{id, title, sub, right, status, on}], action name. j/k moves, Enter opens. */
    list(rows, action, o = {}) {
      if (!rows.length) return A.ui.empty(o.empty || "Nothing here", o.emptyHint || "");
      return A.h`<div class="slist" role="listbox">${rows.map((r) => A.h`<button type="button" role="option" class="srow kb-item ${r.on ? "on" : ""}" aria-selected="${r.on ? "true" : "false"}" data-a="${action}" data-id="${r.id}">
        ${o.select ? A.h`<span class="srow-sel">${A.ui.check(o.select, r.id)}</span>` : ""}<span class="grow"><b>${r.title}</b>${r.sub ? A.h`<small>${r.sub}</small>` : ""}</span>
        <span class="srow-r">${r.status ? A.ui.status(r.status, r.statusLabel, r.statusTone) : ""}${r.right || ""}</span></button>`)}</div>`;
    },
    /** Selection checkbox bound to App.selection(name). */
    check(name, id) { const on = A.selection(name).has(id); return A.h`<span role="checkbox" tabindex="-1" aria-checked="${on ? "true" : "false"}" class="cbx ${on ? "on" : ""}" data-a="sel-toggle" data-sel="${name}" data-id="${id}" title="Select (x)"></span>`; },
    /** Bulk action bar, shown when App.selection(name) is not empty: actions [{label, a, primary, danger, disabled, why}]. Each action gets data-ids="id1,id2". */
    bulkBar(name, actions, o = {}) {
      const s = A.selection(name); if (!s.size) return A.raw("");
      const ids = s.ids().join(",");
      return A.h`<div class="bulkbar" role="toolbar" aria-label="Bulk actions"><b>${s.size} selected</b>
        ${actions.map((x) => A.raw(String(A.ui.btn({ small: true, ...x, data: { ids, ...(x.data || {}) } }))))}
        <span class="grow"></span><button type="button" class="btn small" data-a="sel-clear" data-sel="${name}" title="Clear selection (Esc)">Clear</button>${o.note ? A.h`<span class="muted small">${o.note}</span>` : ""}</div>`;
    },
    /**
     * Inline editor for a safe field: ui.inline({name, id, value, display, type:"text"|"number"|"date"|"select"|"money", options:[[v,label]], disabled, why}).
     * Click (or e on the focused row) turns it into an input; Enter or blur saves through App.inlineEdit(name, fn(id, value)); Esc cancels.
     */
    inline(o) {
      const shown = o.display != null ? o.display : o.type === "money" ? A.money(o.value) : o.type === "date" ? A.date(o.value) : o.value;
      if (o.disabled) return A.h`<span class="inl off" title="${o.why || "Read only"}">${shown === "" || shown == null ? "—" : shown}</span>`;
      return A.h`<button type="button" class="inl" data-a="inline-edit" data-key="e" data-inl="${o.name}" data-id="${o.id}" data-type="${o.type || "text"}" data-val="${o.value ?? ""}" data-opts="${o.options ? JSON.stringify(o.options) : ""}" title="Click to edit (e)">${shown === "" || shown == null ? "—" : shown}</button>`;
    },
    /** Due-date chip relative to App.today: "Today", "Tomorrow", "In 3 days", "2 days overdue". */
    due(iso, o = {}) {
      if (!iso) return A.raw(""); const d = A.daysBetween(A.today, iso.slice(0, 10));
      const t = d < 0 ? `${-d} day${d === -1 ? "" : "s"} overdue` : d === 0 ? "Today" : d === 1 ? "Tomorrow" : d < 14 ? `In ${d} days` : A.date(iso);
      return A.h`<span class="badge chip ${o.done ? "" : d < 0 ? "bad" : d <= (o.soon ?? 2) ? "warn" : ""}" title="${A.date(iso)}">${t}</span>`;
    },
    /** Shortcut hint row for the bottom of a page or panel: [[key, label], ...]. */
    hints: (pairs) => A.h`<div class="hints">${pairs.map(([k, l]) => A.h`<span>${A.ui.kbd(k)} ${l}</span>`)}</div>`,
  });
  /** Format integer cents as CAD: App.cents(123456) -> "$1,234.56". */
  A.cents = (c) => A.money((+c || 0) / 100);
  /** Format a stored "YYYY-MM-DD HH:MM" (Toronto wall clock, as App.now() writes it) for display. */
  A.dateTime = (s) => { if (!s) return ""; const [d, t] = String(s).split(" "); return `${A.date(d)}${t ? " " + t : ""}`; };

  /* selections (bulk actions) */
  const sels = {};
  /** Named selection kept in memory across re-renders: {has, toggle, set(ids), clear, ids(), size}. */
  A.selection = (name) => {
    const s = sels[name] || (sels[name] = new Set());
    return { has: (id) => s.has(String(id)), toggle(id) { id = String(id); s.has(id) ? s.delete(id) : s.add(id); }, set(ids) { s.clear(); ids.forEach((i) => s.add(String(i))); }, clear: () => s.clear(), ids: () => [...s], get size() { return s.size; } };
  };
  A.action("sel-toggle", (el) => { A.selection(el.dataset.sel).toggle(el.dataset.id); A.refresh(); });
  A.action("sel-all", (el) => { const s = A.selection(el.dataset.sel); const ids = (el.dataset.ids || "").split(",").filter(Boolean); s.size >= ids.length ? s.clear() : s.set(ids); A.refresh(); });
  A.action("sel-clear", (el) => { A.selection(el.dataset.sel).clear(); A.refresh(); });

  /* inline edit */
  const inlines = {};
  /** Register the save handler for ui.inline({name}): fn(id, value) -> false to keep editing. */
  A.inlineEdit = (name, fn) => (inlines[name] = fn);
  A.action("inline-edit", (el) => {
    if (el.dataset.editing) return; el.dataset.editing = "1";
    const type = el.dataset.type, val = el.dataset.val; let opts = []; try { opts = el.dataset.opts ? JSON.parse(el.dataset.opts) : []; } catch (e) {}
    const box = document.createElement("span"); box.className = "inl-edit";
    box.innerHTML = type === "select" ? String(A.h`<select class="input">${opts.map(([v, l]) => A.h`<option value="${v}" ${String(v) === val ? A.raw("selected") : ""}>${l}</option>`)}</select>`)
      : String(A.h`<input class="input" type="${type === "money" ? "number" : type}" ${type === "money" ? A.raw('step="0.01"') : ""} value="${val}">`);
    el.replaceWith(box); const inp = box.firstChild; inp.focus(); if (inp.select) inp.select();
    let done = false;
    const finish = (save) => { if (done) return; done = true; const v = inp.value;
      if (save && v !== val && inlines[el.dataset.inl]) { let r; try { r = inlines[el.dataset.inl](el.dataset.id, type === "number" || type === "money" ? +v : v); } catch (e) { console.error(e); } if (r === false) { done = false; inp.focus(); return; } }
      delete el.dataset.editing; if (box.isConnected) box.replaceWith(el); A.refresh(); };
    inp.addEventListener("keydown", (e) => { if (e.key === "Enter") { e.preventDefault(); finish(true); } else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); finish(false); } });
    inp.addEventListener("blur", () => finish(true));
    if (type === "select") inp.addEventListener("change", () => finish(true));
  });

  /* ---------- quick create, shortcuts, density ---------- */
  const creators = [];
  /**
   * Register an item in the "New" menu (n, or the + New button): {id, label, cap, level ("act" default), go, key, run()}.
   * With go, the page opens and its own [data-key="n"] control is pressed, so modules keep one create flow. Same id replaces.
   */
  A.quickCreate = (c) => { const i = creators.findIndex((x) => x.id === c.id); if (i >= 0) creators[i] = c; else creators.push(c); };
  const shortcuts = [];
  /** Register a global shortcut: App.shortcut(key, label, fn). Ignored while typing. Shown in the ? sheet. */
  A.shortcut = (key, label, fn) => shortcuts.push({ key, label, fn });
  const pref = (k, v) => { try { if (v === undefined) return localStorage.getItem("counsel-ui-" + k); localStorage.setItem("counsel-ui-" + k, v); } catch (e) { return null; } };
  /** Density: "comfortable" | "compact". Compact tightens tables, lists and cards for all-day use. */
  A.density = (d) => { if (d === undefined) return document.body.classList.contains("dense") ? "compact" : "comfortable"; document.body.classList.toggle("dense", d === "compact"); pref("density", d); const b = document.getElementById("densitybtn"); if (b) b.textContent = d === "compact" ? "Comfortable" : "Compact"; };
  A.theme = (t) => { const r = document.documentElement; if (t === undefined) return r.dataset.theme || "auto"; if (t === "auto") delete r.dataset.theme; else r.dataset.theme = t; pref("theme", t); };

  function quickMenu() {
    const items = creators.filter((c) => !c.cap || A.can(c.cap, c.level || "act"));
    if (!items.length) return A.toast("Nothing you can create here");
    A.modal("Create", A.h`<div class="palres" id="qcres">${items.map((c, n) => A.h`<button class="palitem" data-n="${n}"><span>${c.label}</span><span class="muted small">${A.ui.kbd(String(n + 1))}</span></button>`)}</div>`, [{ label: "Close" }]);
    const res = document.getElementById("qcres");
    const pick = (c) => { A.closeModal(); if (c.run) return c.run(); A.go(c.go); const t = document.querySelector('#view [data-key="n"]:not([disabled])'); if (t) t.click(); };
    res.querySelectorAll(".palitem").forEach((b) => (b.onclick = () => pick(items[+b.dataset.n])));
    listNav(res, document.getElementById("modal"), (e) => { const n = +e.key; if (n >= 1 && n <= items.length) { e.preventDefault(); pick(items[n - 1]); return true; } });
    const first = res.querySelector(".palitem"); if (first) first.focus();
  }
  /** Arrow / j-k movement inside a list of .palitem buttons in a modal. */
  function listNav(res, scope, extra) {
    scope.onkeydown = (e) => {
      if (extra && extra(e)) return;
      const its = [...res.querySelectorAll(".palitem")]; if (!its.length) return;
      const i = its.indexOf(document.activeElement);
      const typing = /INPUT|TEXTAREA/.test((document.activeElement || {}).tagName || "");
      if (e.key === "ArrowDown" || (!typing && e.key === "j")) { e.preventDefault(); its[Math.min(its.length - 1, i + 1)].focus(); }
      else if (e.key === "ArrowUp" || (!typing && e.key === "k")) { e.preventDefault(); if (i <= 0) { const q = scope.querySelector("input"); (q || its[0]).focus(); } else its[i - 1].focus(); }
    };
  }

  /* ---------- keyboard layer ---------- */
  const KB_ITEMS = "#view .kb-item, #view tr.click, #view .kcard, #view article.gate";
  let kbKey = null; // identity of the focused row, kept across re-renders
  const kbId = (el) => el && (el.dataset.id || el.id || null);
  const kbAll = () => [...document.querySelectorAll(KB_ITEMS)];
  const kbCur = () => document.querySelector("#view .kb-on");
  function kbSet(el) {
    document.querySelectorAll("#view .kb-on").forEach((x) => x.classList.remove("kb-on"));
    if (!el) { kbKey = null; return; }
    el.classList.add("kb-on"); kbKey = kbId(el);
    if (el.scrollIntoView) try { el.scrollIntoView({ block: "nearest" }); } catch (e) {}
  }
  function kbRestore() { if (!kbKey) return; const el = kbAll().find((x) => kbId(x) === kbKey); if (el) el.classList.add("kb-on"); }
  function kbMove(d) { const all = kbAll(); if (!all.length) return; const i = all.indexOf(kbCur()); kbSet(all[i < 0 ? (d > 0 ? 0 : all.length - 1) : Math.max(0, Math.min(all.length - 1, i + d))]); }
  /** Press the control bound to key k: first inside the focused row, then page-level controls (not inside any row). */
  function pressKey(k) {
    const cur = kbCur(); const sel = `[data-key="${k}"]:not([disabled])`;
    let t = cur && (k === "a" ? cur.querySelector('[data-a="gate-approve"],' + sel) : cur.querySelector(sel));
    if (!t) t = [...document.querySelectorAll("#view " + sel)].find((x) => !x.closest(".kb-item, tr.click, .kcard, article.gate"));
    if (!t) return false;
    if (t.matches("input,select,textarea")) { t.focus(); if (t.select) t.select(); } else t.click();
    return true;
  }
  /* route-scoped keys: modules register page keys here; they run before the global layer and show in the ? sheet */
  const routeKeys = [];
  const routeHit = (p, r) => (p instanceof RegExp ? p.test(r) : typeof p === "function" ? p(r) : r === p || r.startsWith(p + "/"));
  /**
   * App.ui.keys(routePrefix, {key: {label, fn(e, route)} | fn}). routePrefix: "work" (matches work and work/...), a RegExp or fn(route).
   * fn returns false when it did not handle the key (the global layer then runs); anything else counts as handled.
   * Keys are ignored while typing or with a dialog open. Labelled keys appear in the ? sheet on matching pages.
   */
  A.ui.keys = (prefix, map) => { Object.entries(map).forEach(([k, v]) => routeKeys.push({ prefix, key: k, label: typeof v === "function" ? "" : v.label || "", fn: typeof v === "function" ? v : v.fn })); };
  function runRouteKey(e) {
    const r = current;
    for (const x of routeKeys) { if (x.key !== e.key || !routeHit(x.prefix, r)) continue; let res; try { res = x.fn(e, r); } catch (err) { console.error(err); res = undefined; } if (res !== false) { e.preventDefault(); return true; } }
    return false;
  }
  const GLOBAL_KEYS = [["mod+k", "Jump to anything or ask the agent"], ["/", "Search or filter this page"], ["j / k", "Move down / up a list"], ["Enter", "Open the selected item"],
    ["a", "Approve the selected item"], ["e", "Edit the selected item"], ["x", "Select the row for bulk actions"], ["n", "Create (this page, else the New menu)"], ["c", "New menu"],
    ["u", "Back"], ["g then 1–9", "Go to a section in the menu"], [".", "Show or hide the agent panel"], ["?", "This sheet"], ["Esc", "Close, or clear selection"]];
  function helpSheet() {
    const page = []; const seen = new Set(GLOBAL_KEYS.map((x) => x[0]));
    document.querySelectorAll("#view [data-key]").forEach((el) => { const k = el.dataset.key; if (seen.has(k)) return; seen.add(k); page.push([k, el.dataset.keyLabel || (el.getAttribute("title") || el.textContent || "").replace(/\s*\(.*\)\s*$/, "").trim().slice(0, 60)]); });
    const pk = new Map(); routeKeys.forEach((x) => { if (x.label && routeHit(x.prefix, current) && !pk.has(x.key)) pk.set(x.key, x.label); });
    const pageKeys = [...pk].map(([k, l]) => [({ ArrowDown: "down", ArrowUp: "up", Escape: "esc" }[k]) || k, l]);
    page.forEach((x) => { if (!pk.has(x[0])) pageKeys.push(x); }); page.length = 0; page.push(...pageKeys); pageKeys.forEach((x) => seen.add(x[0]));
    shortcuts.forEach((s) => { if (!seen.has(s.key)) { seen.add(s.key); page.push([s.key, s.label]); } });
    const rows = (l) => A.h`<dl class="keys">${l.map(([k, d]) => A.h`<dt>${k.split(" ").map((p) => (/^(\/|then|or)$/.test(p) && k.length > 1 ? A.h` ${p} ` : A.ui.kbd(p)))}</dt><dd>${d}</dd>`)}</dl>`;
    A.modal("Keyboard shortcuts", A.h`<div class="grid g2"><div><h4 class="kh4">Everywhere</h4>${rows(GLOBAL_KEYS)}</div>${page.length ? A.h`<div><h4 class="kh4">On this page</h4>${rows(page)}</div>` : ""}</div>`, [{ label: "Close" }]);
  }
  let gPending = 0;
  function onKey(e) {
    if (e.defaultPrevented) return; // a control or page handler already took this key
    const modalOpen = !document.getElementById("modal").hidden;
    if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") { e.preventDefault(); palette(); return; }
    { const t0 = e.target; const typing0 = t0 && (/INPUT|TEXTAREA|SELECT/.test(t0.tagName) || t0.isContentEditable);
      const native = (e.key === "Enter" || e.key === " ") && t0 && /BUTTON|^A$/.test(t0.tagName) && !t0.classList.contains("kb-on");
      if (!modalOpen && !typing0 && !native && !e.metaKey && !e.ctrlKey && !e.altKey && !(gPending && Date.now() - gPending < 1200) && runRouteKey(e)) return; }
    if (e.key === "Escape") { if (modalOpen) { A.closeModal(); return; } const ae = document.activeElement; if (ae && /INPUT|TEXTAREA|SELECT/.test(ae.tagName)) { ae.blur(); return; } if (kbCur()) { kbSet(null); return; } const sn = Object.keys(sels).find((n) => sels[n].size); if (sn) { sels[sn].clear(); A.refresh(); } return; }
    if (modalOpen || e.metaKey || e.ctrlKey || e.altKey) return;
    const t = e.target; if (t && (/INPUT|TEXTAREA|SELECT/.test(t.tagName) || t.isContentEditable)) return;
    const k = e.key;
    if ((k === "Enter" || k === " ") && t && /BUTTON|^A$/.test(t.tagName) && !t.classList.contains("kb-on")) return; // native activation of a focused control
    if (gPending && Date.now() - gPending < 1200) { gPending = 0; const n = +k; const navs = [...document.querySelectorAll("#navlist .nav")]; if (n >= 1 && navs[n - 1]) { e.preventDefault(); navs[n - 1].click(); } return; }
    if (k === "g") { gPending = Date.now(); return; }
    if (k === "j" || k === "ArrowDown" && kbCur()) { e.preventDefault(); kbMove(1); return; }
    if (k === "k" || k === "ArrowUp" && kbCur()) { e.preventDefault(); kbMove(-1); return; }
    if (k === "Enter" && kbCur()) { const c = kbCur(); if (pressKey("enter")) { e.preventDefault(); return; } if (c.matches("[data-a]")) { e.preventDefault(); c.click(); return; } const f = c.querySelector("[data-a]"); if (f) { e.preventDefault(); f.click(); } return; }
    if (k === "?") { e.preventDefault(); helpSheet(); return; }
    if (k === ".") { e.preventDefault(); openAgent(); return; }
    if (k === "x" && kbCur()) { const c = kbCur().querySelector('[data-a="sel-toggle"]') || (kbCur().matches('[data-a="sel-toggle"]') && kbCur()); if (c) { e.preventDefault(); c.click(); return; } }
    if (k.length === 1 && pressKey(k)) { e.preventDefault(); return; }
    const sc = shortcuts.find((s) => s.key === k); if (sc) { e.preventDefault(); sc.fn(); return; }
    if (k === "n" || k === "c") { e.preventDefault(); quickMenu(); return; }
    if (k === "/") { e.preventDefault(); palette(); return; }
    if (k === "u") { e.preventDefault(); A.back(); }
  }
  A.ui.helpSheet = helpSheet; A.ui.quickMenu = quickMenu;
  /* Default "New" menu: each opens the owning page and presses its [data-key="n"] control. Modules may replace by id. */
  [{ id: "lead", label: "Lead (new client enquiry)", cap: "intake.leads", go: "intake" },
    { id: "time", label: "Time entry", cap: "time.record", go: "time" },
    { id: "task", label: "Task", cap: "work.edit", go: "work" },
    { id: "deadline", label: "Deadline", cap: "deadlines.edit", go: "deadlines" },
    { id: "contact", label: "Contact", cap: "contacts.edit", go: "contacts" },
    { id: "invoice", label: "Draft bill", cap: "billing.draft", go: "billing" },
    { id: "trust", label: "Trust request", cap: "trust.request", go: "trust" }].forEach(A.quickCreate);

  /* ---------- shell ---------- */
  function navHtml() {
    const items = mods.filter((m) => m.nav !== false && (!m.cap || A.can(m.cap)) && (!m.visible || m.visible()));
    return A.h`${items.map((m) => { const b = m.badge ? m.badge() : 0; const on = current === m.id || current.startsWith(m.id + "/");
      return A.h`<button class="nav ${on ? "on" : ""}" data-a="go" data-to="${m.home || m.id}"><span class="ic">${m.icon || "•"}</span><span class="grow">${m.label}</span>${b ? A.h`<span class="cnt">${b}</span>` : ""}</button>`; })}`;
  }
  function render() {
    const me = A.me();
    document.getElementById("navlist").innerHTML = String(navHtml());
    document.getElementById("who").innerHTML = String(A.h`${A.ui.avatar(me.id)}<span class="grow"><b>${me.name}</b><small>${A.roleName(me.role)}</small></span><select id="userpick" aria-label="Switch user">${A.store.all("users").map((u) => A.h`<option value="${u.id}" ${u.id === me.id ? A.raw("selected") : ""}>${u.name} · ${A.roleName(u.role)}</option>`)}</select>`);
    document.getElementById("userpick").onchange = (e) => A.setUser(e.target.value);
    const el = document.getElementById("view");
    let r = match(current);
    if (!r) { current = "home"; r = match("home"); }
    if (r && r.mod.cap && !A.can(r.mod.cap)) { el.innerHTML = String(A.h`<div class="page"><div class="locked"><b>No access</b>${A.roleName(me.role)} cannot open ${r.mod.label}. The attempt is logged.</div></div>`); return; }
    ctx = { kind: "home" };
    try { r ? r.fn(el, r.params) : (el.innerHTML = "Not found"); } catch (e) { console.error(e); el.innerHTML = String(A.h`<div class="page"><div class="locked"><b>This screen failed to render</b>${e.message}</div></div>`); }
    kbRestore();
    el.querySelectorAll("[data-key]:not([title])").forEach((b) => b.setAttribute("title", `${(b.getAttribute("aria-label") || b.textContent || "").trim().slice(0, 50)} (${b.dataset.key})`));
    renderAgentHead(); renderAgentRuns();
  }
  function renderAgentHead() {
    const h = document.getElementById("agentctx"); if (!h) return;
    const sug = [].concat(...(suggesters[ctx.kind] || []).map((f) => { try { return f(ctx) || []; } catch (e) { return []; } })).slice(0, 4);
    h.innerHTML = String(A.h`<div class="muted small">Looking at: <b>${ctx.label || "your day"}</b></div><div class="sugs">${sug.map((s) => A.h`<button class="sug" data-a="ask" data-q="${s}">${s}</button>`)}</div>`);
  }
  function renderAgentRuns() {
    const el = document.getElementById("agentruns"); if (!el) return;
    const runs = A.store.where("runs", (r) => !r.matterId || A.canMatter(A.store.get("matters", r.matterId))).slice(0, 4);
    el.innerHTML = String(A.h`${runs.map(A.ui.runCard)}`);
  }
  function chatAdd(who, text, extra) {
    const log = document.getElementById("chatlog");
    const div = document.createElement("div"); div.className = `msg ${who}`;
    div.innerHTML = String(A.h`${who === "agent" ? A.ui.avatar("agent") : A.ui.avatar(db.me)}<div class="grow"><div class="bubble">${text}</div>${extra || ""}</div>`);
    log.appendChild(div); log.scrollTop = log.scrollHeight;
  }
  A.ask = (q) => {
    if (!q) return; openAgent(true); chatAdd("me", q);
    const typing = document.createElement("div"); typing.className = "msg agent typing"; typing.innerHTML = String(A.h`${A.ui.avatar("agent")}<div class="bubble">Working…</div>`);
    const log = document.getElementById("chatlog"); log.appendChild(typing); log.scrollTop = log.scrollHeight;
    setTimeout(() => {
      typing.remove();
      let r; try { r = answer(q); } catch (e) { r = { text: "That failed: " + e.message }; }
      const acts = (r.actions || []).map((a, i) => { const k = A.id("ca"); chatActs[k] = a; return A.h`<button class="btn small" data-a="chat-act" data-id="${k}">${a.label}</button>`; });
      if (r.preview) { const k = A.id("pv"); previews[k] = r.preview; chatAdd("agent", A.raw(String(A.h`${r.text}`)), planHtml(r.preview, k)); return; }
      chatAdd("agent", A.raw(String(A.h`${r.text}`)), A.h`${(r.cites || []).length ? A.h`<div class="cites">${r.cites.map(A.ui.cite)}</div>` : ""}${acts.length ? A.h`<div class="row wrap">${acts}</div>` : ""}`);
    }, A.fast ? 1 : 650);
  };
  const chatActs = {};
  function openAgent(on) { document.body.classList.toggle("agent-open", on ?? !document.body.classList.contains("agent-open")); }

  /* ---------- command palette (mod+K) ---------- */
  /** Extra palette commands: App.command({label, hint, keys, cap, run()}). */
  const commands = [];
  A.command = (c) => commands.push(c);
  function palette() {
    const items = [];
    const routeLabel = (p) => { const r = match(p); if (!r) return p; const m = p.startsWith("matters/") && A.matter(p.split("/")[1]); return m ? `${m.number} ${m.name}` : r.mod.label + (p === (r.mod.home || r.mod.id) ? "" : ` · ${p.split("/").slice(1).join(" / ")}`); };
    A.recent().filter(A.canRoute).forEach((p) => items.push({ label: routeLabel(p), hint: "Recent", go: p, rank: 0 }));
    A.pendingFor().filter((g) => A.canApprove(g).ok).slice(0, 8).forEach((g) => items.push({ label: g.title, hint: "Approve", go: `approvals/${g.id}`, rank: 1, extra: (A.store.get("matters", g.matterId) || {}).number }));
    mods.filter((m) => m.nav !== false && (!m.cap || A.can(m.cap)) && (!m.visible || m.visible())).forEach((m, n) => items.push({ label: m.label, hint: "Go to", keys: `g ${n + 1}`, go: m.home || m.id, rank: 2 }));
    A.matters().forEach((m) => items.push({ label: `${m.number} ${m.name}`, hint: "Matter", go: `matters/${m.id}`, rank: 3, extra: `${A.contact(m.clientId).name} ${m.area || ""}` }));
    if (A.can("contacts.view")) A.store.all("contacts").forEach((c) => items.push({ label: c.name, hint: "Contact", go: `contacts/${c.id}`, rank: 4, extra: c.email || "" }));
    creators.filter((c) => !c.cap || A.can(c.cap, c.level || "act")).forEach((c) => items.push({ label: `New: ${c.label}`, hint: "Create", keys: "n", rank: 5, run: () => { if (c.run) return c.run(); A.go(c.go); const t = document.querySelector('#view [data-key="n"]:not([disabled])'); if (t) t.click(); } }));
    intents.filter((i) => i.label).forEach((i) => items.push({ label: i.label, hint: "Ask the agent", ask: i.example || i.label, rank: 6 }));
    [{ label: A.density() === "compact" ? "Comfortable density" : "Compact density", hint: "View", run: () => A.density(A.density() === "compact" ? "comfortable" : "compact") },
      { label: "Theme: light / dark / system", hint: "View", run: () => A.theme({ auto: "light", light: "dark", dark: "auto" }[A.theme()]) },
      { label: "Show or hide the agent panel", hint: "View", keys: ".", run: () => openAgent() },
      { label: "Keyboard shortcuts", hint: "Help", keys: "?", run: helpSheet },
      ...A.store.all("users").filter((u) => u.id !== db.me).map((u) => ({ label: `View as ${u.name} (${A.roleName(u.role)})`, hint: "Demo", run: () => A.setUser(u.id) })),
      ...commands.filter((c) => !c.cap || A.can(c.cap))].forEach((c) => items.push({ rank: 7, ...c }));
    A.modal("Jump to or ask", A.h`<input id="palq" class="input" placeholder="Matter number, client, page, command, or a question…" autocomplete="off" aria-label="Search"><div id="palres" class="palres" role="listbox"></div>
      ${A.ui.hints([["↑↓", "move"], ["enter", "open"], ["esc", "close"]])}`, [{ label: "Close" }]);
    const q = document.getElementById("palq"), res = document.getElementById("palres");
    let list = [];
    const score = (i, words) => { const hay = `${i.label} ${i.extra || ""} ${i.hint}`.toLowerCase(); if (!words.every((w) => hay.includes(w))) return -1; return (i.label.toLowerCase().startsWith(words[0]) ? 0 : 10) + i.rank; };
    const run = (it) => { A.closeModal(); if (it.ask) return A.ask(it.ask); if (it.go) return A.go(it.go); if (it.run) return it.run(); };
    const draw = () => {
      const t = q.value.trim().toLowerCase(), words = t.split(/\s+/).filter(Boolean);
      list = words.length ? items.map((i) => [score(i, words), i]).filter((x) => x[0] >= 0).sort((a, b) => a[0] - b[0]).map((x) => x[1]).slice(0, 12)
        : items.filter((i) => i.rank <= 2).slice(0, 12);
      res.innerHTML = String(A.h`${list.map((i, n) => A.h`<button class="palitem ${n === 0 ? "on" : ""}" role="option" data-n="${n}"><span>${i.label}</span><span class="muted small">${i.hint}${i.keys ? A.h` ${A.ui.kbd(i.keys)}` : ""}</span></button>`)}${t ? A.h`<button class="palitem ask" data-n="ask"><span>✦ Ask the agent: “${q.value}”</span><span class="muted small">${list.length ? "" : "Enter"}</span></button>` : ""}`);
      res.querySelectorAll(".palitem").forEach((b) => (b.onclick = () => (b.dataset.n === "ask" ? (A.closeModal(), A.ask(q.value)) : run(list[+b.dataset.n]))));
    };
    q.oninput = draw;
    q.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); const first = res.querySelector(".palitem"); if (first) first.click(); } else if (e.key === "ArrowDown") { e.preventDefault(); const f = res.querySelector(".palitem"); if (f) f.focus(); } };
    listNav(res, document.getElementById("modal"));
    draw();
  }
  A.ui.palette = palette;

  /* ---------- built-in actions ---------- */
  A.action("go", (el) => A.go(el.dataset.to));
  A.action("ask", (el) => A.ask(el.dataset.q));
  A.action("chat-act", (el) => { const a = chatActs[el.dataset.id]; if (!a) return; a.go ? A.go(a.go) : a.run && a.run(); });
  A.action("agent-stop", (el) => A.agent.stop(el.dataset.id));
  A.action("agent-preview-run", (el) => { const t = previews[el.dataset.id]; if (!t) return; delete previews[el.dataset.id]; const box = el.closest("[data-preview]"); const r = A.agent.run({ plan: true, ...t }); if (box) box.querySelector(".row").innerHTML = r ? '<span class="muted small">Started</span>' : '<span class="muted small">Held: the agent is paused</span>'; });
  A.action("agent-preview-cancel", (el) => { delete previews[el.dataset.id]; const box = el.closest("[data-preview]"); if (box) box.querySelector(".row").innerHTML = '<span class="muted small">Cancelled; nothing was run</span>'; });
  A.action("gate-approve", (el) => A.approve(el.dataset.id));
  A.action("gate-back", (el) => A.modal("Send back", A.h`<label class="field">What should change?<textarea id="sbnote" rows="3" class="input"></textarea></label>`, [{ label: "Cancel" }, { label: "Send back", primary: true, run: () => A.sendBack(el.dataset.id, document.getElementById("sbnote").value) }]));
  A.action("open-cite", (el) => { if (A.openCite) return A.openCite(el.dataset.doc, +el.dataset.page || 0, +el.dataset.line || 0); A.toast(`${el.dataset.doc} p.${el.dataset.page} l.${el.dataset.line}`); });

  A.start = () => {
    db = load(); seeders.forEach(([c, rows]) => seedOne(c, rows)); save();
    ready = true; readyFns.forEach((f) => { try { f(); } catch (e) { console.error(e); } });
    document.addEventListener("click", (e) => { const el = e.target.closest("[data-a]"); if (!el) return; const f = actions[el.dataset.a]; if (f) { if (!e.target.matches('input[type="checkbox"],input[type="radio"]')) e.preventDefault(); f(el, e); } });
    document.addEventListener("change", (e) => { const el = e.target.closest("[data-c]"); if (el && changes[el.dataset.c]) changes[el.dataset.c](el, e); });
    document.addEventListener("input", (e) => {
      const el = e.target.closest && e.target.closest("[data-i],[data-live]"); if (!el) return;
      if (el.dataset.i && inputs[el.dataset.i]) inputs[el.dataset.i](el, e);
      if (el.dataset.live && lives[el.dataset.live]) { const id = el.id, pos = el.selectionStart; lives[el.dataset.live](el.value, el); A.refresh();
        const n = id && document.getElementById(id); if (n) { n.focus(); try { n.setSelectionRange(pos, pos); } catch (x) { /* not a text input */ } } }
    });
    document.addEventListener("keydown", onKey);
    document.getElementById("cmd").onclick = palette;
    const nb = document.getElementById("newbtn"); if (nb) nb.onclick = quickMenu;
    const hb = document.getElementById("helpbtn"); if (hb) hb.onclick = helpSheet;
    const db2 = document.getElementById("densitybtn"); if (db2) db2.onclick = () => A.density(A.density() === "compact" ? "comfortable" : "compact");
    A.density(pref("density") || "comfortable"); const th = pref("theme"); if (th && th !== "auto") A.theme(th);
    document.getElementById("agentbtn").onclick = () => openAgent();
    document.getElementById("agentclose").onclick = () => openAgent(false);
    document.getElementById("chatform").onsubmit = (e) => { e.preventDefault(); const i = document.getElementById("chatin"); A.ask(i.value.trim()); i.value = ""; };
    document.getElementById("reset").onclick = () => A.modal("Reset demo", A.h`<p>Put every record back to the starting demo data. Your changes are kept only in this browser.</p>`, [{ label: "Cancel" }, { label: "Reset", primary: true, run: () => { A.store.reset(); A.go("home"); A.toast("Demo data reset"); } }]);
    A.store.on(render);
    if (window.innerWidth > 1100) openAgent(true);
    chatAdd("agent", A.raw(String(A.h`Good morning. I work on your matters and prepare drafts; a person approves anything that reaches a client, a court, the bank or the trust ledger. Ask me anything, press <kbd>⌘K</kbd> to jump, or <kbd>?</kbd> for shortcuts.`)));
    A.go("home");
  };
})();
