/* Settings (users, roles, walls, break-glass, agent autonomy, access review) and the client portal. */
(function () {
  "use strict";
  const A = window.App, h = A.h;
  const QUARTER = "2026 Q4";
  const LEVELS = [
    { id: "draft", name: "Draft only" },
    { id: "checks", name: "Draft + run checks" },
    { id: "act", name: "Act after approval" },
  ];
  const LOCKED = ["trust", "conflicts", "deadlines"];

  /* ---------- seeds ---------- */
  A.seed("agentSkills", [
    { id: "sk-intake", name: "Intake triage and engagement letters", area: "intake", level: "checks" },
    { id: "sk-conflicts", name: "Conflict search and clearance memo", area: "conflicts", level: "draft" },
    { id: "sk-deadlines", name: "Limitation and rule-based deadlines", area: "deadlines", level: "draft" },
    { id: "sk-docs", name: "Correspondence and pleadings drafts", area: "documents", level: "checks" },
    { id: "sk-billing", name: "Pre-bills from time entries", area: "billing", level: "checks" },
    { id: "sk-trust", name: "Trust requests and three-way reconciliation", area: "trust", level: "draft" },
    { id: "sk-portal", name: "Client portal updates and replies", area: "portal", level: "draft" },
    { id: "sk-filing", name: "Document filing and naming", area: "documents", level: "act" },
  ]);
  A.seed("accessReviews", () => window.SEED.users.map((u) => ({ id: "ar-" + u.id, userId: u.id, quarter: QUARTER, decision: null })));
  A.seed("portalTimeline", [
    { id: "pt-1", matterId: "m4", date: "2026-08-20", text: "We met, reviewed your aunt's 2019 will and confirmed you are named as estate trustee.", approvedBy: "u-linda" },
    { id: "pt-2", matterId: "m4", date: "2026-09-04", text: "We wrote to the banks and the land registry to list what the estate owns. Most replies are in.", approvedBy: "u-linda" },
    { id: "pt-3", matterId: "m4", date: "2026-10-02", text: "We are preparing the court application that confirms your authority to act (a certificate of appointment). You will need to sign an affidavit before we file.", approvedBy: "u-linda" },
  ]);
  A.seed("portalShares", [{ id: "ps-1", matterId: "m4", docId: "d-will", sharedBy: "u-linda", at: "2026-08-21" }]);
  A.seed("portalMessages", [
    { id: "pm-1", matterId: "m4", from: "u-grace", at: "2026-10-03 09:12", text: "Do I need to pay the funeral home from my own account, or can the estate pay it?" },
    { id: "pm-2", matterId: "m4", from: "u-linda", at: "2026-10-03 14:40", text: "The estate can pay reasonable funeral expenses. Keep the invoice; we will reimburse you from the estate account once it is open." },
  ]);
  A.seed("portalSign", [{ id: "es-1", matterId: "m4", title: "Affidavit of estate trustee (Form 74.4)", requested: "2026-10-08", by: "u-linda", status: "awaiting" }]);
  A.seed("portalStageLog", [{ id: "psl-1", matterId: "m4", from: "Listing the estate's assets", to: "Probate application", at: "2026-10-02", seenBy: [] }]);
  A.seed("portalParticipants", [
    { id: "pp-1", matterId: "m4", name: "Grace Mendel", rel: "Client, estate trustee", added: "2026-08-20" },
    { id: "pp-2", matterId: "m4", name: "Samuel Mendel", rel: "Spouse, helps with paperwork (client's consent on file)", added: "2026-10-05" },
  ]);
  A.seed("teams", [
    { id: "tm-lit", name: "Litigation", members: ["u-linda", "u-ayo", "u-meraf"] },
    { id: "tm-re", name: "Real estate", members: ["u-yoni", "u-kaleab"] },
    { id: "tm-acct", name: "Accounts", members: ["u-yohannes", "u-dawit"] },
  ]);
  A.seed("sessions", [
    { id: "ss-1", userId: "u-linda", device: "Chrome on macOS", place: "Toronto, ON", ip: "142.112.x.x", method: "Microsoft sign-in + MFA", started: "2026-10-09 08:02", last: "2026-10-09 10:41", active: true },
    { id: "ss-2", userId: "u-linda", device: "Safari on iPhone", place: "Toronto, ON", ip: "99.231.x.x", method: "Microsoft sign-in + MFA", started: "2026-10-07 19:15", last: "2026-10-08 22:03", active: true },
    { id: "ss-3", userId: "u-yoni", device: "Edge on Windows", place: "Mississauga, ON", ip: "70.53.x.x", method: "Microsoft sign-in + MFA", started: "2026-10-09 08:40", last: "2026-10-09 10:12", active: true },
    { id: "ss-4", userId: "u-ayo", device: "Chrome on Windows", place: "Toronto, ON", ip: "142.112.x.x", method: "Microsoft sign-in + MFA", started: "2026-10-09 09:01", last: "2026-10-09 10:38", active: true },
    { id: "ss-5", userId: "u-meraf", device: "Chrome on Windows", place: "Toronto, ON", ip: "142.112.x.x", method: "Password + authenticator app", started: "2026-10-09 08:55", last: "2026-10-09 10:30", active: true },
    { id: "ss-6", userId: "u-kaleab", device: "Firefox on Ubuntu", place: "Brampton, ON", ip: "174.88.x.x", method: "Password + authenticator app", started: "2026-10-08 13:20", last: "2026-10-08 17:44", active: true },
    { id: "ss-7", userId: "u-grace", device: "Chrome on Android", place: "Oakville, ON", ip: "64.229.x.x", method: "Portal password + email code", started: "2026-10-03 09:05", last: "2026-10-03 09:20", active: true },
  ]);
  A.seed("secSettings", [{ id: "firm", mfaRequired: true, ssoMicrosoft: true, ssoGoogle: false, ssoGeneric: false, genericIssuer: "", idleMinutes: 30, clientIdleMinutes: 15 }]);
  A.seed("supportAccess", [
    { id: "sa-1", ticket: "SUP-20418", reason: "Invoice PDFs cut off the HST line", scope: "Settings and billing screens; no documents or messages", hours: 24, status: "expired", requestedBy: "u-dawit", approvedBy: "u-linda", granted: "2026-09-12 10:05", expires: "2026-09-13 10:05", views: 14 },
  ]);
  A.seed("inferenceLog", [
    { id: "il-1", at: "2026-10-09 10:31", task: "Draft portal reply", matter: "2026-0155", model: "Drafting model", region: "Canada · Toronto", tokensIn: 3120, tokensOut: 410, kept: "Not retained" },
    { id: "il-2", at: "2026-10-09 10:12", task: "Conflict search memo", matter: "lead", model: "Drafting model", region: "Canada · Toronto", tokensIn: 5480, tokensOut: 690, kept: "Not retained" },
    { id: "il-3", at: "2026-10-09 09:47", task: "Deadline computation", matter: "2026-0142", model: "Reasoning model", region: "Canada · Montréal", tokensIn: 2210, tokensOut: 280, kept: "Not retained" },
    { id: "il-4", at: "2026-10-09 09:05", task: "Classify phone note", matter: "lead", model: "Small model", region: "Canada · Toronto", tokensIn: 640, tokensOut: 95, kept: "Not retained" },
    { id: "il-5", at: "2026-10-08 16:40", task: "Pre-bill narrative", matter: "2026-0149", model: "Drafting model", region: "Canada · Toronto", tokensIn: 4020, tokensOut: 520, kept: "Not retained" },
  ]);

  /* ---------- helpers ---------- */
  const me = () => A.me();
  const users = () => A.store.all("users");
  const active = (u) => u.status !== "inactive";
  const capArea = (cap) => cap.split(".")[0];
  /** Would user u see matter m? Mirrors App.canMatter for an arbitrary user. */
  function userSees(u, m) {
    if (u.role === "client") return m.clientId === u.contactId;
    if (!active(u)) return false;
    if ((m.wall || []).includes(u.id)) return false;
    if (A.store.where("breakglass", (b) => b.userId === u.id && b.matterId === m.id && b.active).length) return true;
    const onTeam = (m.team || []).includes(u.id);
    if (m.restricted && !onTeam) return false;
    const g = A.grant("matters.view", u.role);
    return g === "all" || g === "approve" || g === "view" || (g === "own" && onTeam);
  }
  /** Small button through the shared ui.btn (tone: primary | danger | agent). */
  const btn = (label, a, data, why, tone = "", key) => A.ui.btn({ label, a, data, key, small: true, disabled: !!why, why, primary: tone === "primary", danger: tone === "danger", agent: tone === "agent" });
  function expireBreakGlass() {
    const now = A.now();
    A.store.where("breakglass", (b) => b.active && b.expires < now).forEach((b) => { A.store.put("breakglass", { id: b.id, active: false }); A.audit("breakglass.expired", { target: b.matterId, note: A.user(b.userId).name }); });
  }
  /** "YYYY-MM-DD HH:MM" plus n hours, same format. */
  function plusHours(at, n) {
    const d = new Date(at.replace(" ", "T") + ":00"); d.setHours(d.getHours() + n);
    const p = (x) => String(x).padStart(2, "0");
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  }
  function expireSupport() {
    const now = A.now();
    A.store.where("supportAccess", (s) => s.status === "active" && s.expires < now).forEach((s) => { A.store.put("supportAccess", { id: s.id, status: "expired" }); A.audit("support.access_expired", { target: s.id, note: s.ticket }); });
  }
  const sec = () => A.store.get("secSettings", "firm") || {};
  const LAWYERS = ["owner", "lawyer", "associate"];

  /* ---------- effects ---------- */
  A.effect("settings-role", (p) => { A.store.put("users", { id: p.userId, role: p.role }); A.audit("user.role_changed", { target: p.userId, note: `${p.from} -> ${p.role}` }); });
  A.effect("settings-invite", (p) => { A.store.put("users", { id: p.userId, status: "active" }); A.audit("user.invited", { target: p.userId, note: A.roleName(p.role) }); });
  A.effect("settings-deactivate", (p) => { A.store.put("users", { id: p.userId, status: "inactive" }); A.audit("user.deactivated", { target: p.userId, note: p.reason || "" }); });
  A.effect("settings-wall", (p) => {
    const m = A.store.get("matters", p.matterId); if (!m) return;
    const wall = (m.wall || []).filter((x) => x !== p.userId); if (p.op === "add") wall.push(p.userId);
    A.store.put("matters", { id: m.id, wall, restricted: p.op === "add" ? true : m.restricted });
    if (p.op === "add") A.store.where("breakglass", (b) => b.userId === p.userId && b.matterId === m.id && b.active).forEach((b) => A.store.put("breakglass", { id: b.id, active: false }));
    A.audit(p.op === "add" ? "wall.created" : "wall.lifted", { target: m.id, note: A.user(p.userId).name });
  });
  A.effect("settings-bg-review", (p) => { A.store.put("breakglass", { id: p.bgId, reviewed: true }); A.audit("breakglass.reviewed", { target: p.bgId }); });
  A.effect("portal-send", (p) => {
    A.store.put("portalMessages", { id: A.id("pm"), matterId: p.matterId, from: p.lawyer || "u-linda", at: A.now(), text: p.text, viaAgent: true });
    if (p.replyTo) A.store.put("portalMessages", { id: p.replyTo, answered: true });
    A.audit("portal.message_sent", { target: p.matterId, note: "Agent draft released by lawyer" });
  });
  A.effect("settings-offboard", (p) => {
    (p.matters || []).forEach((x) => { const m = A.store.get("matters", x.id); if (m) A.store.put("matters", { id: m.id, responsible: x.responsible, team: x.team }); });
    (p.work || []).forEach((x) => { if (A.store.get("workItems", x.id)) A.store.put("workItems", { id: x.id, assignee: x.to }); });
    (p.deadlines || []).forEach((x) => { if (A.store.get("deadlines", x.id)) A.store.put("deadlines", { id: x.id, [x.field]: x.to }); });
    A.store.where("sessions", (s) => s.userId === p.userId && s.active).forEach((s) => A.store.put("sessions", { id: s.id, active: false, endedBy: "offboarding" }));
    A.store.where("teams", (t) => (t.members || []).includes(p.userId)).forEach((t) => A.store.put("teams", { id: t.id, members: t.members.filter((x) => x !== p.userId) }));
    A.store.put("users", { id: p.userId, status: "inactive" });
    A.audit("user.offboarded", { target: p.userId, note: `${(p.matters || []).length} matters, ${(p.work || []).length} tasks, ${(p.deadlines || []).length} deadlines reassigned; sessions revoked` });
  });
  A.effect("settings-bulk-invite", (p) => { (p.userIds || []).forEach((id) => A.store.put("users", { id, status: "active" })); A.audit("user.bulk_invited", { note: `${(p.userIds || []).length} users` }); });
  A.effect("settings-sec-set", (p) => { A.store.put("secSettings", { id: "firm", [p.key]: p.value }); A.audit("security.setting_changed", { target: p.key, note: String(p.value) }); });
  A.effect("settings-support", (p) => {
    const at = A.now();
    A.store.put("supportAccess", { id: p.id, status: "active", approvedBy: A.me().id, granted: at, expires: plusHours(at, p.hours), views: 0 });
    A.audit("support.access_granted", { target: p.id, note: `${p.hours} h; ${p.scope}` });
  });

  /* ---------- Settings ---------- */
  /* Tabs grouped by job: people and access (daily), security (rare), agent. */
  const TABS = [
    { id: "settings", label: "Users", group: "Access" },
    { id: "settings/people", label: "Onboard & offboard", group: "Access" },
    { id: "settings/audit", label: "Access review", group: "Access" },
    { id: "settings/walls", label: "Walls", group: "Access" },
    { id: "settings/break-glass", label: "Break-glass", group: "Access" },
    { id: "settings/roles", label: "Roles & duties", group: "Access" },
    { id: "settings/log", label: "Access log", group: "Access" },
    { id: "settings/security", label: "Sign-in", group: "Security" },
    { id: "settings/support", label: "Support access", group: "Security" },
    { id: "settings/residency", label: "Data residency", group: "Security" },
    { id: "settings/agent", label: "Agent autonomy", group: "Agent" },
  ];
  const ADMIN_EFFECTS = /^settings-/;
  /** Admin approvals the current user can act on now (preparer != approver enforced by App.canApprove). */
  const adminQueue = () => A.store.where("approvals", (g) => g.status === "pending" && ADMIN_EFFECTS.test(g.effect || "") && A.canApprove(g).ok);
  function tabCount(id) {
    if (id === "settings/break-glass") return A.store.where("breakglass", (b) => b.active || !b.reviewed).length;
    if (id === "settings/audit") return A.store.where("accessReviews", (r) => r.quarter === QUARTER && !r.decision).length;
    if (id === "settings") return users().filter((u) => u.status === "invited").length;
    return 0;
  }
  /** Morning strip: what needs this admin today, each one click (or one key) away. */
  function needsYou() {
    const q = adminQueue();
    const bg = A.store.where("breakglass", (b) => !b.reviewed).length;
    const ar = A.store.where("accessReviews", (r) => r.quarter === QUARTER && !r.decision).length;
    const inv = users().filter((u) => u.status === "invited").length;
    const items = [
      q.length && { n: q.length, label: `approval${q.length === 1 ? "" : "s"} for you`, to: "settings/queue", st: "needs-approval" },
      bg && { n: bg, label: "break-glass to review", to: "settings/break-glass", st: "to-review" },
      inv && { n: inv, label: `invite${inv === 1 ? "" : "s"} pending`, to: "settings", st: "waiting" },
      ar && { n: ar, label: `people to certify (${QUARTER})`, to: "settings/audit", st: "to-review" },
    ].filter(Boolean);
    if (!items.length) return h`<div class="row small muted" style="margin:0 0 12px">${A.ui.status("done", "All clear")} Nothing in settings needs you today.</div>`;
    return h`<div class="row wrap" style="margin:0 0 12px;gap:6px"><span class="small muted">Needs you:</span>${items.map((x) => h`<button class="btn small" data-a="go" data-to="${x.to}">${A.ui.status(x.st, String(x.n))} ${x.label}</button>`)}</div>`;
  }
  function shell(el, route, title, sub, body, opts = {}) {
    A.setContext({ kind: "settings", id: route, label: `Settings · ${title}` });
    const groups = [...new Set(TABS.map((t) => t.group))];
    const tabsRow = h`<div class="row wrap" style="gap:14px;align-items:flex-end;margin-bottom:12px">${groups.map((gname) => h`<div><div class="small muted" style="font-size:11px;text-transform:uppercase;letter-spacing:.05em;margin:0 0 2px 2px">${gname}</div>
      ${A.ui.tabs(TABS.filter((t) => t.group === gname).map((t) => ({ ...t, count: tabCount(t.id) })), route, "settings-tab")}</div>`)}</div>`;
    const admin = A.can("admin.users");
    A.render(el, h`<div class="page">${A.ui.header({ title, sub, status: opts.status, primary: opts.primary, actions: opts.actions })}
      ${admin ? h`${tabsRow}${needsYou()}` : ""}${body}</div>`);
  }
  A.action("settings-tab", (el) => A.go(el.dataset.id));
  const kbd = A.ui.kbd;

  /** One queue of every settings approval the current user may decide, with bulk approve. */
  function queueView(el) {
    const q = A.store.where("approvals", (g) => g.status === "pending" && ADMIN_EFFECTS.test(g.effect || ""));
    const mine = q.filter((g) => A.canApprove(g).ok);
    const body = h`${mine.length > 1 ? h`<div class="row" style="margin-bottom:10px"><span class="grow small muted">${mine.length} you can decide. Approving runs each change at once and logs it.</span>${btn(`Approve all ${mine.length}`, "settings-approve-all", { id: "" }, "", "primary")}</div>` : ""}
      ${q.length ? q.map((g) => A.ui.gateCard(g)) : A.ui.empty("No access or security changes are waiting", "Role changes, invites, walls, offboarding and security changes appear here for a second person to approve.")}`;
    shell(el, "settings/queue", "Settings approvals", "Access and security changes waiting for a second person. You never approve your own request.", body);
  }
  A.action("settings-approve-all", () => { const ids = adminQueue().map((g) => g.id); ids.forEach((id) => A.approve(id)); if (ids.length) A.toast(`Approved ${ids.length} changes`, "ok"); });

  /* ---------- Users: filter bar + list -> detail split view, keyboard j/k/Enter/e/n// ---------- */
  const uf = { q: "", role: "", status: "" };
  let selUser = null;
  const statusOf = (u) => (u.status === "inactive" ? "inactive" : u.status === "invited" ? "invited" : "active");
  /** User status in the shared chip vocabulary. */
  const USTATUS = { active: ["active", "Active"], invited: ["waiting", "Invite pending"], inactive: ["inactive", "Deactivated"] };
  const uChip = (u) => { const [w, l] = USTATUS[statusOf(u)]; return A.ui.status(w, l); };
  function filteredUsers() {
    const q = uf.q.toLowerCase();
    return users().filter((u) => (!uf.role || u.role === uf.role) && (!uf.status || statusOf(u) === uf.status)
      && (!q || `${u.name} ${u.email || ""} ${u.title || ""} ${A.roleName(u.role)}`.toLowerCase().includes(q)));
  }
  function userBlock(u, pend) {
    const canAct = A.can("admin.users", "act");
    const why = !canAct ? "Your role cannot change users" : u.id === me().id ? "You cannot change your own access" : "";
    const pendingFor = pend.some((g) => g.payload && g.payload.userId === u.id);
    return { why, roleWhy: why || (pendingFor ? "A change is already waiting for approval" : "") || (u.status === "inactive" ? "User is deactivated" : "") };
  }
  function userDetail(u, pend) {
    if (!u) return h`<div class="card">${A.ui.empty("Pick a person", h`<span class="small">${kbd("j")} ${kbd("k")} to move, ${kbd("Enter")} to open.</span>`)}</div>`;
    const { why, roleWhy } = userBlock(u, pend);
    const seen = A.store.all("matters").filter((m) => userSees(u, m));
    const walls = A.store.all("matters").filter((m) => (m.wall || []).includes(u.id));
    const bg = A.store.where("breakglass", (b) => b.userId === u.id && b.active);
    const sess = A.store.where("sessions", (s) => s.userId === u.id && s.active);
    const teams = A.store.where("teams", (t) => (t.members || []).includes(u.id));
    const work = openWork(u.id).length, dls = openDeadlines(u.id).length;
    const prepared = A.store.where("approvals", (g) => g.status === "pending" && g.preparedBy === u.id);
    const mine = pend.filter((g) => g.payload && g.payload.userId === u.id);
    const approves = A.caps.filter((c) => A.grant(c.id, u.role) === "approve").map((c) => c.name);
    return h`<div class="card"><div class="row" style="margin-bottom:8px">${A.ui.avatar(u.id)}<span class="grow"><b>${u.name}</b><br><span class="muted small">${u.email || u.title || ""}</span></span>${uChip(u)}</div>
      <div class="row wrap" style="gap:6px;margin-bottom:10px">
        ${btn("Change role", "settings-role", { id: u.id }, roleWhy, "primary", "e")}
        ${u.role !== "client" ? btn("Offboard", "settings-offboard", { id: u.id }, why || (u.status !== "active" && u.status ? "Only active staff are offboarded" : "")) : ""}
        ${btn(`End ${sess.length} session${sess.length === 1 ? "" : "s"}`, "settings-revoke-all", { id: u.id }, !A.can("admin.security", "act") ? "Your role cannot end sessions" : !sess.length ? "No open sessions" : u.id === me().id ? "Sign yourself out from your own device" : "")}
        ${btn("Deactivate", "settings-deactivate", { id: u.id }, why || (u.status === "inactive" ? "Already deactivated" : ""), "danger")}</div>
      ${mine.length ? h`<div style="margin-bottom:10px">${mine.map((g) => A.ui.gateCard(g, { compact: true }))}</div>` : ""}
      <dl class="kv">
        <dt>Role</dt><dd>${A.roleName(u.role)}${u.signatory ? h` ${A.ui.badge("Trust signatory")}` : ""}</dd>
        <dt>Can approve</dt><dd>${approves.length ? h`<span class="small">${approves.slice(0, 6).join(", ")}${approves.length > 6 ? ` +${approves.length - 6} more` : ""}</span>` : h`<span class="muted">Nothing</span>`}</dd>
        <dt>Matters</dt><dd>${seen.length} visible${seen.length ? h`<br><span class="muted small">${seen.slice(0, 5).map((m) => m.number).join(", ")}${seen.length > 5 ? "…" : ""}</span>` : ""}</dd>
        <dt>Walls</dt><dd>${walls.length ? walls.map((m) => h`${A.ui.status("hold", `Screened from ${m.number}`)} `) : h`<span class="muted">None</span>`}</dd>
        ${bg.length ? h`<dt>Break-glass</dt><dd>${bg.map((b) => A.ui.status("in-progress", `${A.store.get("matters", b.matterId)?.number || b.matterId} until ${A.dateTime(b.expires)}`))}</dd>` : ""}
        <dt>Teams</dt><dd>${teams.length ? teams.map((t) => t.name).join(", ") : h`<span class="muted">None</span>`}</dd>
        <dt>Holds</dt><dd>${work} open task${work === 1 ? "" : "s"} · ${dls} deadline${dls === 1 ? "" : "s"} · ${prepared.length} request${prepared.length === 1 ? "" : "s"} awaiting approval</dd>
        <dt>Sign-in</dt><dd>${sess.length ? sess.map((s) => h`<div class="small">${s.device} · ${s.method}<br><span class="muted">last active ${A.dateTime(s.last)}</span></div>`) : h`<span class="muted">No open sessions</span>`}</dd>
      </dl></div>`;
  }
  function usersView(el, params = {}) {
    const canAct = A.can("admin.users", "act");
    const pend = A.store.where("approvals", (g) => g.status === "pending" && /^settings-(role|invite|deactivate|offboard)$/.test(g.effect));
    if (params.id) selUser = params.id;
    const rows = filteredUsers();
    if (!rows.some((u) => u.id === selUser)) selUser = rows[0] ? rows[0].id : null;
    const roleOpts = A.roles.filter((r) => r.id !== "agent");
    const count = (st) => users().filter((u) => statusOf(u) === st).length;
    const bar = A.ui.filters({
      search: { c: "settings-uq", value: uf.q, placeholder: "Search people" },
      chips: [{ id: "", label: "Everyone", count: users().length }, ...Object.entries(USTATUS).map(([k, v]) => ({ id: k, label: v[1], count: count(k) }))], active: uf.status, action: "settings-uf-status",
      selects: [{ c: "settings-uf-role", value: uf.role, label: "Role", options: [["", "All roles"], ...roleOpts.map((r) => [r.id, r.name])] }],
      right: h`<span class="small muted">${rows.length} of ${users().length}</span>`,
    });
    const list = A.ui.list(rows.map((u) => ({ id: u.id, on: u.id === selUser, title: u.name, sub: `${A.roleName(u.role)}${u.title && u.title !== A.roleName(u.role) ? " · " + u.title : ""}`,
      right: h`${uChip(u)}${pend.some((g) => g.payload && g.payload.userId === u.id) ? A.ui.status("needs-approval", "Change pending") : ""}` })), "settings-user", { select: "settings-users", empty: "No one matches these filters" });
    const sel = A.selection("settings-users").ids().map((id) => A.store.get("users", id)).filter(Boolean);
    const offOk = sel.filter((u) => u.id !== me().id && statusOf(u) !== "inactive");
    const bulk = A.ui.bulkBar("settings-users", [
      { label: `Request deactivation (${offOk.length})`, a: "settings-bulk-deactivate", danger: true, disabled: !canAct || !offOk.length, why: !canAct ? "Your role cannot change users" : "Nobody selected can be deactivated" },
      { label: "End sessions", a: "settings-bulk-signout", disabled: !A.can("admin.security", "act"), why: "Your role cannot end sessions" },
    ], { note: "Deactivations go to an owner. You are never included." });
    const body = h`${bar}${A.ui.split(list, selUser ? userDetail(A.store.get("users", selUser), pend) : null, { emptyTitle: "Pick a person" })}${bulk}
      ${A.ui.hints([["j", "next"], ["k", "previous"], ["x", "select"], ["e", "change role"], ["n", "invite"], ["/", "search"]])}
      <p class="muted small">Role changes and invites go to an owner; nobody changes their own access.</p>`;
    shell(el, "settings", "Users", `${count("active")} active · ${count("invited")} invited · Linda K Law`, body,
      { primary: { label: "Invite user", a: "settings-invite", id: "new", key: "n", disabled: !canAct, why: "Your role cannot invite users" } });
  }
  A.action("settings-user", (el) => { selUser = el.dataset.id; A.refresh(); });
  A.change("settings-uq", (el) => { uf.q = el.value; A.refresh(); });
  A.change("settings-uf-role", (el) => { uf.role = el.value; A.refresh(); });
  A.action("settings-uf-status", (el) => { uf.status = el.dataset.id; A.refresh(); });
  A.action("settings-bulk-deactivate", (el) => {
    if (!A.can("admin.users", "act")) return;
    const ids = (el.dataset.ids || "").split(",").filter((id) => id && id !== me().id && A.store.get("users", id) && statusOf(A.store.get("users", id)) !== "inactive");
    ids.forEach((id) => A.gate({ kind: "access", cap: "admin.users", title: `Deactivate ${A.user(id).name}`, summary: `Bulk request by ${me().name}.`, preparedBy: me().id, effect: "settings-deactivate", payload: { userId: id, reason: "bulk request" },
      checks: [{ label: "Requester is not the person deactivated", ok: true }] }));
    A.selection("settings-users").clear(); if (ids.length) A.toast(`${ids.length} deactivation${ids.length === 1 ? "" : "s"} sent to an owner`);
  });
  A.action("settings-bulk-signout", (el) => {
    if (!A.can("admin.security", "act")) return;
    const ids = (el.dataset.ids || "").split(",").filter((id) => id && id !== me().id);
    let n = 0; A.store.where("sessions", (s) => s.active && ids.includes(s.userId)).forEach((s) => { n++; A.store.put("sessions", { id: s.id, active: false, endedBy: me().id }); });
    if (n) A.audit("session.revoked", { note: `${n} sessions, ${ids.length} users (bulk)` });
    A.selection("settings-users").clear(); A.toast(n ? `Ended ${n} session${n === 1 ? "" : "s"}` : "No open sessions for that selection");
  });
  A.action("settings-revoke-all", (el) => {
    const uid = el.dataset.id; if (!A.can("admin.security", "act") || uid === me().id) return;
    const ss = A.store.where("sessions", (s) => s.userId === uid && s.active); if (!ss.length) return;
    ss.forEach((s) => A.store.put("sessions", { id: s.id, active: false, endedBy: me().id }));
    A.audit("session.revoked", { target: uid, note: `${ss.length} sessions` }); A.toast(`Signed ${A.user(uid).name} out everywhere`);
  });
  A.ui.keys("settings/audit", { a: { label: "Keep everyone the agent checked", fn: (e) => { const b = document.querySelector('[data-a="settings-cert-bulk"]:not([disabled])'); if (!b) return false; b.click(); e.preventDefault(); } } });
  A.onUser(() => { selUser = null; });
  A.action("settings-invite", () => {
    A.modal("Invite user", h`<div class="form">
      <label class="field">Full name<input name="name" class="input"></label>
      <label class="field">Email<input name="email" class="input" type="email"></label>
      <label class="field">Role<select name="role">${A.roles.filter((r) => r.id !== "agent").map((r) => h`<option value="${r.id}">${r.name}</option>`)}</select></label></div>
      <p class="muted small">The invite is sent once an owner approves the role.</p>`, [{ label: "Cancel" }, { label: "Request invite", primary: true, run: (d) => {
        const v = A.formValues(d); if (!v.name.trim() || !/@/.test(v.email)) { A.toast("Name and email are required", "bad"); return false; }
        const initials = v.name.trim().split(/\s+/).map((w) => w[0]).join("").slice(0, 2).toUpperCase();
        const u = A.store.put("users", { id: A.id("u"), name: v.name.trim(), email: v.email, title: A.roleName(v.role), role: v.role, initials, status: "invited" });
        A.gate({ kind: "access", cap: "admin.users", title: `Invite ${u.name} as ${A.roleName(v.role)}`, summary: `Requested by ${me().name}. Grants ${A.roleName(v.role)} access on first sign-in.`, preparedBy: me().id, effect: "settings-invite", payload: { userId: u.id, role: v.role },
          checks: [{ label: "Requester is not the invitee", ok: true }, { label: "Email domain verified", ok: true }] });
        A.toast("Invite sent for owner approval");
      } }]);
  });
  A.action("settings-role", (el) => {
    const u = A.store.get("users", el.dataset.id); if (!u || u.id === me().id) return;
    A.modal(`Change role: ${u.name}`, h`<label class="field">New role<select name="role">${A.roles.filter((r) => r.id !== "agent").map((r) => h`<option value="${r.id}" ${r.id === u.role ? A.raw("selected") : ""}>${r.name}</option>`)}</select></label>
      <label class="field" style="margin-top:10px">Reason<input name="reason" class="input"></label><p class="muted small">Currently ${A.roleName(u.role)}. An owner other than you approves.</p>`,
    [{ label: "Cancel" }, { label: "Request change", primary: true, run: (d) => {
      const v = A.formValues(d); if (v.role === u.role) { A.toast("Pick a different role", "bad"); return false; }
      A.gate({ kind: "access", cap: "admin.users", title: `Change ${u.name}: ${A.roleName(u.role)} → ${A.roleName(v.role)}`, summary: v.reason || "No reason given", preparedBy: me().id, effect: "settings-role", payload: { userId: u.id, role: v.role, from: u.role },
        checks: [{ label: "Requester is not the person changed", ok: true }, { label: "Approver must not be the requester", ok: true }] });
      A.toast("Role change sent for approval");
    } }]);
  });
  function deactivate(uid, reason) { A.store.put("users", { id: uid, status: "inactive" }); A.audit("user.deactivated", { target: uid, note: reason || "" }); A.toast("User deactivated"); }
  A.action("settings-deactivate", (el) => {
    const u = A.store.get("users", el.dataset.id); if (!u || u.id === me().id) return;
    A.modal(`Deactivate ${u.name}`, h`<p>Sign-in stops now and open sessions end. Their matters, time and audit history stay. Reactivation needs owner approval.</p>`,
      [{ label: "Cancel" }, { label: "Deactivate", primary: true, run: () => deactivate(u.id) }]);
  });

  const CHIP = { approve: ["Approve", "gate"], all: ["All", "ok"], own: ["Own", "agent"], view: ["View", ""], none: ["—", ""] };
  let roleArea = "all";
  function rolesView(el) {
    const roles = A.roles;
    const areas = [...new Set(A.caps.map((c) => capArea(c.id)))];
    const caps = A.caps.filter((c) => roleArea === "all" || capArea(c.id) === roleArea);
    const body = h`<div class="row wrap" style="margin-bottom:10px">${["all", ...areas].map((a) => h`<button class="btn small ${a === roleArea ? "primary" : ""}" data-a="settings-area" data-id="${a}">${a === "all" ? "All areas" : a}</button>`)}</div>
      <div class="tblwrap"><table class="tbl"><thead><tr><th>Capability</th>${roles.map((r) => h`<th title="${r.desc}">${r.name}${FIXED_ROLES[r.id] ? "" : h`<br><button type="button" class="btn small" data-a="go" data-to="settings/roles/${r.id}" title="Edit ${r.name} (draft, owner approves)">Edit</button>`}</th>`)}</tr></thead><tbody>
      ${caps.map((c) => h`<tr><td><b>${c.name}</b><br><span class="muted small">${c.id}</span></td>${roles.map((r) => { const g = A.grant(c.id, r.id); const [t, tone] = CHIP[g] || [g, ""]; return h`<td>${g === "none" ? h`<span class="muted">—</span>` : A.ui.badge(t, tone)}</td>`; })}</tr>`)}
      </tbody></table></div>
      <div class="grid g2" style="margin-top:14px"><div class="card"><h2>Separation of duties</h2><ol class="small" style="margin:0;padding-left:18px">${A.sod.map((s) => h`<li style="margin-bottom:4px">${s}</li>`)}</ol></div>
      <div class="card"><h2>Current conflicts</h2>${dutyConflicts().length ? h`<ul class="small" style="margin:0;padding-left:18px">${dutyConflicts().map((c) => h`<li style="margin-bottom:6px">${A.ui.status(c.tone === "bad" ? "blocked" : "due-soon", c.tone === "bad" ? "Blocked" : "Watch")} ${c.text}</li>`)}</ul>` : h`<p class="small muted" style="margin:0">${A.ui.status("done", "None")} Every pending approval has an eligible second person, and trust has two signatories.</p>`}</div></div>`;
    shell(el, "settings/roles", "Roles", "Who can do what, the duty rules, and any live conflict between them. Edit a role to draft changes; an owner who did not make them approves.", body);
  }
  /** Live separation-of-duties problems: approvals no one else can decide, too few trust signatories. */
  function dutyConflicts() {
    const out = [];
    const staffActive = users().filter((u) => u.role !== "client" && statusOf(u) === "active");
    A.store.where("approvals", (g) => g.status === "pending").forEach((g) => {
      const m = g.matterId && A.store.get("matters", g.matterId);
      const ok = staffActive.filter((u) => u.id !== g.preparedBy && A.grant(g.cap, u.role) === "approve" && (!m || userSees(u, m)) && (g.cap !== "trust.authorize" || u.signatory));
      if (!ok.length) out.push({ tone: "bad", text: `“${g.title}” has no eligible approver other than the person who prepared it.` });
    });
    const sig = staffActive.filter((u) => u.signatory).length;
    if (sig < 2) out.push({ tone: "warn", text: `Only ${sig} active trust signator${sig === 1 ? "y" : "ies"}; a second is needed so the preparer never authorizes their own request.` });
    return out;
  }
  A.action("settings-area", (el) => { roleArea = el.dataset.id; A.refresh(); });

  function wallsView(el) {
    const canReq = A.can("conflicts.walls", "view");
    const ms = A.store.all("matters").filter((m) => m.restricted || (m.wall || []).length);
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "settings-wall");
    const body = h`<p class="muted small">Matter names only. A screened person cannot open the matter, see it in search, or receive break-glass. Creating or lifting a wall is approved by a lawyer who is neither screened nor the requester.</p>
      ${A.ui.table([
        { label: "Matter", render: (m) => h`<b>${m.number}</b> ${m.name}` },
        { label: "Status", render: (m) => m.restricted ? A.ui.status("hold", "Restricted") : A.ui.status("open", "Open to firm") },
        { label: "Team", render: (m) => h`${(m.team || []).map(A.ui.avatar)}` },
        { label: "Screened", render: (m) => (m.wall || []).length ? h`${(m.wall || []).map((uid) => h`<div class="row">${A.user(uid).name} ${btn("Lift", "settings-wall-lift", { id: m.id, user: uid }, !canReq ? "Your role cannot request wall changes" : uid === me().id ? "You cannot change your own wall status" : "")}</div>`)}` : h`<span class="muted">None</span>` },
        { label: "", render: (m) => btn("Add wall", "settings-wall-add", { id: m.id }, canReq ? "" : "Your role cannot request wall changes") },
      ], ms, { empty: "No restricted matters" })}
      <div class="row" style="margin-top:10px">${btn("Screen someone from another matter", "settings-wall-add", { id: "" }, canReq ? "" : "Your role cannot request wall changes")}</div>
      ${pend.length ? h`<h3 style="margin:18px 0 8px;font-size:14.5px">Wall changes waiting</h3>${pend.map((g) => A.ui.gateCard(g))}` : ""}`;
    shell(el, "settings/walls", "Walls", "Restricted matters and ethical walls (LSO Rules 3.4-11.1 to 3.4-11.3: screening a transferring lawyer or staff member).", body);
  }
  A.action("settings-wall-add", (el) => {
    const mid = el.dataset.id;
    A.modal("Add ethical wall", h`<div class="form">
      <label class="field">Matter<select name="matterId">${A.store.all("matters").map((m) => h`<option value="${m.id}" ${m.id === mid ? A.raw("selected") : ""}>${m.number} ${m.name}</option>`)}</select></label>
      <label class="field">Screen<select name="userId"><option value="">Choose a person</option>${users().filter((u) => u.role !== "client" && u.id !== me().id).map((u) => h`<option value="${u.id}">${u.name}</option>`)}</select></label></div>
      <label class="field" style="margin-top:10px">Reason<input name="reason" class="input" placeholder="e.g. prior retainer for the opposing party"></label>`,
    [{ label: "Cancel" }, { label: "Request wall", primary: true, run: (d) => {
      const v = A.formValues(d); const m = A.store.get("matters", v.matterId);
      if (!v.userId || !v.reason.trim()) { A.toast("Pick a person and give a reason", "bad"); return false; }
      if ((m.wall || []).includes(v.userId)) { A.toast("Already screened", "bad"); return false; }
      A.gate({ kind: "wall", cap: "conflicts.walls", title: `Screen ${A.user(v.userId).name} from ${m.number}`, summary: v.reason, preparedBy: me().id, effect: "settings-wall", payload: { matterId: m.id, userId: v.userId, op: "add" },
        checks: [{ label: "Screened person is not the requester", ok: true }, { label: "Screened person on matter team", ok: !(m.team || []).includes(v.userId), blocking: false }] });
      A.toast("Wall sent for approval");
    } }]);
  });
  A.action("settings-wall-lift", (el) => {
    const m = A.store.get("matters", el.dataset.id), uid = el.dataset.user; if (!m || uid === me().id) return;
    A.modal(`Lift wall: ${A.user(uid).name}`, h`<p>${m.number} ${m.name}. The person will see the matter as their role allows.</p><label class="field">Reason<input name="reason" class="input"></label>`,
      [{ label: "Cancel" }, { label: "Request lift", primary: true, run: (d) => {
        const v = A.formValues(d); if (!v.reason.trim()) { A.toast("Give a reason", "bad"); return false; }
        A.gate({ kind: "wall", cap: "conflicts.walls", title: `Lift wall: ${A.user(uid).name} on ${m.number}`, summary: v.reason, preparedBy: me().id, effect: "settings-wall", payload: { matterId: m.id, userId: uid, op: "remove" },
          checks: [{ label: "Conflict no longer exists (confirm with conflict check)", ok: true }] });
        A.toast("Wall lift sent for approval");
      } }]);
  });

  /** Why the current user cannot use break-glass on matter m (empty string if allowed). */
  function bgBlock(m) {
    const u = me();
    if (u.role === "admin") return "The system administrator never uses break-glass";
    if (!A.can("admin.break_glass", "act")) return `${A.roleName(u.role)} cannot use break-glass`;
    if (m && (m.wall || []).includes(u.id)) return "You are screened from this matter";
    return "";
  }
  function bgView(el) {
    expireBreakGlass();
    const list = A.store.all("breakglass");
    const block = bgBlock(null);
    const body = h`<div class="card" style="margin-bottom:14px"><h2>Emergency access</h2>
        <p class="small muted" style="margin:0 0 8px">Opens one matter for 24 hours when its team cannot be reached (e.g. a limitation period expires today). Access starts at once and a different owner or lawyer reviews it afterwards. Never allows money movement or access changes.</p>
        ${btn("Request emergency access", "settings-bg", { id: "" }, block, "primary")}</div>
      ${A.ui.table([
        { label: "Who", render: (b) => A.user(b.userId).name },
        { label: "Matter", render: (b) => { const m = A.store.get("matters", b.matterId); return m ? `${m.number} ${m.name}` : b.matterId; } },
        { label: "Reason", key: "reason" },
        { label: "Status", render: (b) => h`${b.active ? A.ui.status("in-progress", `Active until ${A.dateTime(b.expires)}`) : A.ui.status("closed", "Ended")} ${b.reviewed ? A.ui.status("approved", "Reviewed") : A.ui.status("to-review", "Review pending")}` },
        { label: "", render: (b) => b.active ? btn("End now", "settings-bg-end", { id: b.id }, b.userId === me().id || A.can("admin.break_glass_approve", "approve") ? "" : "Only the user or a reviewer can end this") : "" },
      ], list, { empty: "No break-glass use on record" })}`;
    shell(el, "settings/break-glass", "Break-glass", "Every use is logged and reviewed.", body);
  }
  function openBreakGlass(matterId) {
    const block = bgBlock(null); if (block) return A.toast(block, "bad");
    const opts = A.store.all("matters").filter((m) => !A.canMatter(m) && !(m.wall || []).includes(me().id));
    if (!opts.length) return A.toast("You can already open every matter you are allowed to", "ok");
    A.modal("Request emergency access", h`<label class="field">Matter<select name="matterId">${opts.map((m) => h`<option value="${m.id}" ${m.id === matterId ? A.raw("selected") : ""}>${m.number} ${m.name}</option>`)}</select></label>
      <label class="field" style="margin-top:10px">Reason (shown to the reviewer)<textarea name="reason" rows="3" class="input" placeholder="e.g. Responsible lawyer unreachable; motion record due at 4 p.m. today"></textarea></label>`,
    [{ label: "Cancel" }, { label: "Open access now", primary: true, run: (d) => {
      const v = A.formValues(d); const m = A.store.get("matters", v.matterId);
      if (v.reason.trim().length < 10) { A.toast("Give a specific reason", "bad"); return false; }
      if (bgBlock(m)) { A.toast(bgBlock(m), "bad"); return false; }
      const t = A.now().slice(11);
      const b = A.store.put("breakglass", { id: A.id("bg"), userId: me().id, matterId: m.id, reason: v.reason.trim(), active: true, started: A.now(), expires: `${A.addDays(A.today, 1)} ${t}`, reviewed: false });
      A.audit("breakglass.used", { target: m.id, note: v.reason.trim() });
      A.gate({ kind: "review", cap: "admin.break_glass_approve", title: `Review break-glass: ${me().name} on ${m.number}`, summary: v.reason.trim(), preparedBy: me().id, effect: "settings-bg-review", payload: { bgId: b.id },
        checks: [{ label: "User not screened from matter", ok: true }, { label: "Reviewer is a different owner or lawyer", ok: true }, { label: "No money or access changes during access", ok: true }] });
      A.toast(`Access open on ${m.number} until ${A.dateTime(b.expires)}`, "ok");
    } }]);
  }
  A.action("settings-bg", (el) => openBreakGlass(el.dataset.id));
  A.action("settings-bg-end", (el) => { A.store.put("breakglass", { id: el.dataset.id, active: false }); A.audit("breakglass.ended", { target: el.dataset.id }); A.toast("Emergency access ended"); });

  function agentView(el) {
    const canEdit = A.can("platform.agent_settings", "act");
    const skills = A.store.all("agentSkills");
    const runs = A.store.all("runs").length;
    const body = h`<div class="stats">${A.ui.stat("Data residency", "Canada", "Models and storage in ca-central; no training on firm data")}
        ${A.ui.stat("Model budget, October", `${A.cents(budgetUsed())} / ${A.cents(acct().budgetCents)}`, `${Math.round((100 * budgetUsed()) / acct().budgetCents)}% used · alerts at 80%`)}
        ${A.ui.stat("Agent runs this session", runs, "every run is in the audit log")}</div>
      <p class="muted small">Every level keeps a person in the loop for anything reaching a client, court, bank or the trust ledger. Trust, conflicts and deadlines are locked at draft for approval.</p>
      ${A.ui.table([
        { label: "Skill", render: (s) => h`<b>${s.name}</b><br><span class="muted small">${s.area}</span>` },
        { label: "Autonomy", render: (s) => { const lock = LOCKED.includes(s.area);
          return h`<div class="row wrap">${LEVELS.map((l) => { const why = !canEdit ? "Your role cannot change agent settings" : lock && l.id !== "draft" ? "Hard lock: this area never goes past draft for approval" : "";
            return h`<button class="btn small ${s.level === l.id ? "primary" : ""}" data-a="settings-skill" data-id="${s.id}" data-level="${l.id}" ${why ? A.raw(`disabled title="${A.esc(why)}"`) : ""}>${l.name}</button>`; })}
            ${lock ? A.ui.status("hold", "Locked") : ""}</div>`; } },
      ], skills)}`;
    shell(el, "settings/agent", "Agent", "How far the agent may go on its own, per skill.", body);
  }
  A.action("settings-skill", (el) => {
    const s = A.store.get("agentSkills", el.dataset.id); const lvl = el.dataset.level;
    if (!s || s.level === lvl || !A.can("platform.agent_settings", "act")) return;
    if (LOCKED.includes(s.area) && lvl !== "draft") return A.toast("This area is locked at draft for approval", "bad");
    A.store.put("agentSkills", { id: s.id, level: lvl });
    A.audit("agent.autonomy_changed", { target: s.id, note: `${s.level} -> ${lvl}` });
  });

  /** Agent pre-check for the access review: a suggestion with a reason; a person still decides. */
  function reviewHint(u) {
    if (u.status === "inactive") return { s: "remove", why: "Deactivated; access should be closed in the review" };
    if (u.role === "client") { const open = A.store.where("matters", (m) => m.clientId === u.contactId && m.status !== "closed").length; return open ? { s: "keep", why: `${open} open matter${open === 1 ? "" : "s"}` } : { s: "remove", why: "No open matters; portal access no longer needed" }; }
    const n = A.store.all("matters").filter((m) => userSees(u, m)).length;
    if (u.status === "invited") return { s: "check", why: "Invite never accepted" };
    return { s: "keep", why: `Active; sees ${n} matter${n === 1 ? "" : "s"} through ${A.roleName(u.role)}` };
  }
  let arFilter = "open";
  function auditView(el) {
    const canCert = A.can("admin.access_review", "act");
    const all = A.store.where("accessReviews", (r) => r.quarter === QUARTER);
    const done = all.filter((r) => r.decision).length;
    const recs = arFilter === "open" ? all.filter((r) => !r.decision) : all;
    const certWhy = (r) => (!canCert ? "Your role cannot certify access" : r.userId === me().id ? "Someone else certifies your access" : "");
    const bulk = all.filter((r) => !r.decision && !certWhy(r) && reviewHint(A.user(r.userId)).s === "keep");
    const body = h`<div class="stats">${A.ui.stat("Certified", `${done} / ${all.length}`, QUARTER, done === all.length ? "" : "gate")}${A.ui.stat("Due", A.date("2026-12-31"), "quarterly")}${A.ui.stat("✦ Agent suggests keep", bulk.length, "you confirm; the agent never certifies")}</div>
      <div class="row wrap" style="gap:8px;margin-bottom:10px">
        ${A.ui.tabs([{ id: "open", label: "To decide", count: all.length - done }, { id: "all", label: "All" }], arFilter, "settings-ar-filter")}
        <span class="grow"></span>
        ${btn(`Keep ${bulk.length} the agent checked`, "settings-cert-bulk", { id: "" }, !canCert ? "Your role cannot certify access" : bulk.length ? "" : "Nothing left the agent marked keep", "primary")}
        <span class="small muted">${kbd("a")}</span></div>
      ${A.ui.table([
        { label: "User", render: (r) => { const u = A.user(r.userId); return h`<b>${u.name}</b><br><span class="muted small">${A.roleName(u.role)}${u.status === "inactive" ? " · deactivated" : ""}</span>`; } },
        { label: "Sees", render: (r) => { const u = A.user(r.userId); const n = A.store.all("matters").filter((m) => userSees(u, m)).length; return `${n} matter${n === 1 ? "" : "s"}`; } },
        { label: "Last sign-in", render: (r) => { const s = A.store.where("sessions", (x) => x.userId === r.userId).map((x) => x.last).sort().pop(); return s ? A.date(s.slice(0, 10)) : h`<span class="muted">Never</span>`; } },
        { label: "✦ Agent", render: (r) => { const x = reviewHint(A.user(r.userId)); return h`${A.ui.status("suggested", x.s === "keep" ? "Keep" : x.s === "remove" ? "Remove" : "Check")}<br><span class="muted small">${x.why}</span>`; } },
        { label: "Decision", render: (r) => r.decision ? h`${A.ui.status(r.decision === "keep" ? "approved" : "rejected", r.decision === "keep" ? "Kept" : "Removed")} <span class="muted small">${A.user(r.by).name} · ${A.dateTime(r.at)}</span>`
          : h`<div class="row">${btn("Keep", "settings-cert", { id: r.id, d: "keep" }, certWhy(r))}${btn("Remove", "settings-cert", { id: r.id, d: "remove" }, certWhy(r), "danger")}</div>` },
      ], recs, { empty: "Every person is certified for " + QUARTER })}
      <p class="muted small" style="margin-top:8px">Remove sends a deactivation to an owner for approval. Signatories and walls are reviewed on their own tabs.</p>`;
    shell(el, "settings/audit", "Access review", "Confirm each person still needs the access they have.", body);
  }
  A.action("settings-ar-filter", (el) => { arFilter = el.dataset.id; A.refresh(); });
  A.action("settings-cert-bulk", () => {
    if (!A.can("admin.access_review", "act")) return;
    const rs = A.store.where("accessReviews", (r) => r.quarter === QUARTER && !r.decision && r.userId !== me().id && reviewHint(A.user(r.userId)).s === "keep");
    rs.forEach((r) => { A.store.put("accessReviews", { id: r.id, decision: "keep", by: me().id, at: A.now() }); A.audit("access_review.certified", { target: r.userId, note: "keep (bulk, agent pre-checked)" }); });
    if (rs.length) A.toast(`Kept ${rs.length} people`, "ok");
  });
  A.action("settings-cert", (el) => {
    const r = A.store.get("accessReviews", el.dataset.id); if (!r || r.decision || r.userId === me().id || !A.can("admin.access_review", "act")) return;
    A.store.put("accessReviews", { id: r.id, decision: el.dataset.d, by: me().id, at: A.now() });
    A.audit("access_review.certified", { target: r.userId, note: el.dataset.d });
    if (el.dataset.d === "remove") A.gate({ kind: "access", cap: "admin.users", title: `Deactivate ${A.user(r.userId).name} (access review)`, summary: `Marked Remove in the ${QUARTER} access review by ${me().name}.`, preparedBy: me().id, effect: "settings-deactivate", payload: { userId: r.userId, reason: "access review" } });
  });

  /* ---------- People: offboarding, bulk invite, teams ---------- */
  const staff = () => users().filter((u) => u.role !== "client" && active(u) && u.status !== "invited");
  const openWork = (uid) => A.store.where("workItems", (w) => w.assignee === uid && !/^(done|closed)$/i.test(w.status || ""));
  const dlField = (d) => ("responsible" in d ? "responsible" : "owner");
  const openDeadlines = (uid) => A.store.where("deadlines", (d) => d[dlField(d)] === uid && !/^(done|met|closed|cancelled)$/i.test(d.status || ""));
  /** What the departing user holds and who should take each item. */
  function offboardPlan(uid, succ) {
    const matters = A.store.where("matters", (m) => m.status !== "closed" && (m.responsible === uid || (m.team || []).includes(uid))).map((m) => {
      const peer = (m.team || []).find((x) => x !== uid && active(A.user(x)) && LAWYERS.includes(A.user(x).role));
      const responsible = m.responsible === uid ? peer || succ : m.responsible;
      let team = (m.team || []).filter((x) => x !== uid); if (!team.includes(responsible)) team.push(responsible);
      return { id: m.id, label: `${m.number} ${m.name}`, wasResponsible: m.responsible === uid, responsible, team };
    });
    const newResp = (mid) => { const x = matters.find((y) => y.id === mid); return x ? x.responsible : succ; };
    const work = openWork(uid).map((w) => ({ id: w.id, label: `${w.key || w.id} ${w.title}`, to: newResp(w.matterId) }));
    const deadlines = openDeadlines(uid).map((d) => ({ id: d.id, label: `${d.due} ${d.title}`, field: dlField(d), to: newResp(d.matterId) }));
    const approvals = A.store.where("approvals", (g) => g.status === "pending" && g.preparedBy === uid).map((g) => ({ id: g.id, label: g.title }));
    const sessions = A.store.where("sessions", (s) => s.userId === uid && s.active).length;
    return { matters, work, deadlines, approvals, sessions };
  }
  function planBody(p) {
    const list = (title, rows, f) => h`<p><b>${title}</b> (${rows.length})</p>${rows.length ? h`<ul>${rows.map((r) => h`<li>${f(r)}</li>`)}</ul>` : h`<p class="muted small">None.</p>`}`;
    return String(h`${list("Matters", p.matters, (r) => h`${r.label}: ${r.wasResponsible ? `responsible → ${A.user(r.responsible).name}` : "removed from team"}`)}
      ${list("Open tasks", p.work, (r) => h`${r.label} → ${A.user(r.to).name}`)}
      ${list("Deadlines", p.deadlines, (r) => h`${r.label} → ${A.user(r.to).name}`)}
      ${list("Approvals they prepared (stay in the queue; another person still approves)", p.approvals, (r) => r.label)}
      <p><b>Sign-in.</b> ${p.sessions} open session${p.sessions === 1 ? "" : "s"} end on approval; the account is deactivated. Time entries and audit history stay.</p>`);
  }
  function peopleView(el) {
    const canAct = A.can("admin.users", "act");
    const why = canAct ? "" : "Your role cannot manage users";
    const pend = A.store.where("approvals", (g) => g.status === "pending" && /^settings-(offboard|bulk-invite)$/.test(g.effect));
    const teams = A.store.all("teams");
    const body = h`<div class="grid g2">
        <section class="card"><h2>Offboarding</h2><p class="small muted" style="margin:0 0 8px">Pick who is leaving. The agent lists their open matters, tasks, deadlines and approvals and proposes who takes each one. An owner approves; then work moves and sessions end.</p>
          ${btn("✦ Offboard someone", "settings-offboard", { id: "" }, why, "agent")}</section>
        <section class="card"><h2>Invite from a list</h2><p class="small muted" style="margin:0 0 8px">Paste CSV rows (name, email, role). Check the preview, then send all invites for one owner approval.</p>
          ${btn("Invite from CSV", "settings-csv", { id: "" }, why)}</section></div>
      ${pend.length ? h`<h3 style="margin:18px 0 8px;font-size:14.5px">Waiting for an owner</h3>${pend.map((g) => A.ui.gateCard(g))}` : ""}
      <div class="row" style="margin:18px 0 8px"><h3 class="grow" style="margin:0;font-size:14.5px">Teams</h3>${btn("New team", "settings-team-new", { id: "" }, why)}</div>
      ${A.ui.table([
        { label: "Team", render: (t) => h`<b>${t.name}</b>` },
        { label: "Members", render: (t) => (t.members || []).length ? h`${t.members.map(A.ui.avatar)} <span class="muted small">${t.members.map((x) => A.user(x).name).join(", ")}</span>` : h`<span class="muted">No members</span>` },
        { label: "", render: (t) => h`<div class="row">${btn("Members", "settings-team-edit", { id: t.id }, why)}${btn("Delete", "settings-team-del", { id: t.id }, why, "danger")}</div>` },
      ], teams, { empty: "No teams yet" })}
      <p class="muted small">Teams group people for assignment and matter staffing. They do not grant access; roles and matter teams do.</p>`;
    shell(el, "settings/people", "People", "Leavers, bulk invites and teams.", body);
  }
  A.action("settings-offboard", (el) => {
    if (!A.can("admin.users", "act")) return;
    const pre = el && el.dataset ? el.dataset.id : "";
    const leave = staff().filter((u) => u.id !== me().id);
    if (!leave.length) return A.toast("Nobody else to offboard", "bad");
    A.modal("Offboard a user", h`<div class="form">
      <label class="field">Who is leaving<select name="userId">${leave.map((u) => h`<option value="${u.id}" ${u.id === pre ? A.raw("selected") : ""}>${u.name} · ${A.roleName(u.role)}</option>`)}</select></label>
      <label class="field">Default successor<select name="succ">${staff().filter((u) => LAWYERS.includes(u.role)).map((u) => h`<option value="${u.id}" ${u.id === "u-linda" ? A.raw("selected") : ""}>${u.name}</option>`)}</select></label>
      <label class="field">Last day<input name="last" type="date" class="input" value="${A.today}"></label></div>
      <p class="muted small">Where another lawyer is already on a matter team, they take it; otherwise the successor does. You can send the proposal back with changes.</p>`,
    [{ label: "Cancel" }, { label: "Prepare plan", primary: true, run: (d) => {
      const v = A.formValues(d);
      if (v.userId === v.succ) { A.toast("The successor cannot be the person leaving", "bad"); return false; }
      const u = A.user(v.userId), by = me().id;
      A.agent.run({ title: `Offboarding plan: ${u.name}`, steps: ["Finding matters, tasks and deadlines", "Matching each item to a team lawyer", "Listing sessions and pending approvals"], done: () => {
        const p = offboardPlan(v.userId, v.succ);
        return A.gate({ kind: "access", cap: "admin.users", title: `Offboard ${u.name} (last day ${v.last})`, summary: `${p.matters.length} matters, ${p.work.length} tasks, ${p.deadlines.length} deadlines to reassign; ${p.sessions} sessions to end.`,
          preparedBy: by, body: planBody(p), effect: "settings-offboard", payload: { userId: v.userId, matters: p.matters, work: p.work, deadlines: p.deadlines },
          checks: [{ label: "Requester is not the person leaving", ok: true }, { label: "Every open matter keeps a responsible lawyer", ok: p.matters.every((m) => m.responsible && m.responsible !== v.userId), blocking: true }, { label: "Every deadline has a new owner", ok: p.deadlines.every((x) => x.to !== v.userId), blocking: true }] });
      } });
      A.toast("The agent is preparing the offboarding plan");
    } }]);
  });

  const SAMPLE_CSV = "name,email,role\nHana Tesfaye,hana.tesfaye@lindaklaw.example.ca,paralegal\nOmar Siddiqui,omar.siddiqui@lindaklaw.example.ca,associate\nRuth Abera,ruth@lindaklaw,clerk";
  function parseCsv(text) {
    const known = new Set(users().map((u) => (u.email || "").toLowerCase()));
    const roles = A.roles.filter((r) => r.id !== "agent" && r.id !== "client");
    const seen = new Set();
    return text.split(/\r?\n/).map((l) => l.trim()).filter((l) => l && !/^name\s*,/i.test(l)).map((line) => {
      const [name = "", email = "", role = ""] = line.split(",").map((x) => x.trim());
      const r = roles.find((x) => x.id === role.toLowerCase() || x.name.toLowerCase() === role.toLowerCase());
      const e = email.toLowerCase();
      const err = !name ? "Missing name" : !/^[^@\s]+@[^@\s]+\.[a-z]{2,}$/i.test(email) ? "Email is not valid" : !r ? `Unknown role “${role}”` : known.has(e) ? "Already a user" : seen.has(e) ? "Duplicate in list" : "";
      seen.add(e);
      return { name, email, role: r ? r.id : role, err };
    });
  }
  const csvPreview = (rows) => h`${A.ui.table([{ label: "Name", key: "name" }, { label: "Email", key: "email" }, { label: "Role", render: (r) => A.roleName(r.role) }, { label: "Check", render: (r) => (r.err ? A.ui.status("failed", r.err) : A.ui.status("done", "OK")) }], rows.map((r, i) => ({ ...r, id: i })), { empty: "No rows" })}
    <p class="muted small">${rows.filter((r) => !r.err).length} of ${rows.length} rows will be invited. Rows with problems are skipped.</p>`;
  A.action("settings-csv", () => {
    if (!A.can("admin.users", "act")) return;
    A.modal("Invite from CSV", h`<label class="field">Rows: name, email, role<textarea name="csv" rows="6" class="input">${SAMPLE_CSV}</textarea></label><div id="csv-prev" style="margin-top:10px"></div>`,
      [{ label: "Cancel" }, { label: "Preview", run: (d) => { A.render(d.querySelector("#csv-prev"), csvPreview(parseCsv(A.formValues(d).csv))); return false; } },
        { label: "Request invites", primary: true, run: (d) => {
          const rows = parseCsv(A.formValues(d).csv).filter((r) => !r.err);
          if (!rows.length) { A.toast("No valid rows", "bad"); return false; }
          const ids = rows.map((r) => A.store.put("users", { id: A.id("u"), name: r.name, email: r.email, title: A.roleName(r.role), role: r.role, initials: r.name.split(/\s+/).map((w) => w[0]).join("").slice(0, 2).toUpperCase(), status: "invited" }).id);
          A.gate({ kind: "access", cap: "admin.users", title: `Invite ${rows.length} users from a list`, summary: rows.map((r) => `${r.name} (${A.roleName(r.role)})`).join("; "), preparedBy: me().id,
            body: String(csvPreview(rows)), effect: "settings-bulk-invite", payload: { userIds: ids }, checks: [{ label: "Every row has a valid email and role", ok: true }, { label: "No existing users duplicated", ok: true }] });
          A.toast(`${rows.length} invites sent for owner approval`);
        } }]);
  });

  function teamModal(t) {
    A.modal(t ? `Members: ${t.name}` : "New team", h`<label class="field">Team name<input name="name" class="input" value="${t ? t.name : ""}"></label>
      <div style="margin-top:10px;display:flex;flex-direction:column;gap:4px">${staff().map((u) => h`<label class="row"><input type="checkbox" name="m_${u.id}" ${t && t.members.includes(u.id) ? A.raw("checked") : ""}> ${u.name} <span class="muted small">${A.roleName(u.role)}</span></label>`)}</div>`,
    [{ label: "Cancel" }, { label: t ? "Save" : "Create team", primary: true, run: (d) => {
      const v = A.formValues(d); const name = (v.name || "").trim() || (t ? t.name : "");
      if (!name) { A.toast("Name the team", "bad"); return false; }
      const members = Object.keys(v).filter((k) => k.startsWith("m_") && v[k]).map((k) => k.slice(2));
      const x = A.store.put("teams", { id: t ? t.id : A.id("tm"), name, members });
      A.audit(t ? "team.updated" : "team.created", { target: x.id, note: `${name}: ${members.length} members` });
    } }]);
  }
  A.action("settings-team-new", () => { if (A.can("admin.users", "act")) teamModal(null); });
  A.action("settings-team-edit", (el) => { const t = A.store.get("teams", el.dataset.id); if (t && A.can("admin.users", "act")) teamModal(t); });
  A.action("settings-team-del", (el) => { const t = A.store.get("teams", el.dataset.id); if (!t || !A.can("admin.users", "act")) return; A.store.remove("teams", t.id); A.audit("team.deleted", { target: t.id, note: t.name }); A.toast(`Deleted team ${t.name}`); });

  /* ---------- Sign-in & security ---------- */
  function securityView(el) {
    const s = sec(), canSec = A.can("admin.security", "act");
    const dis = canSec ? "" : A.raw(`disabled title="${A.esc(A.roleName(me().role) + " cannot change security settings (admin.security)")}"`);
    const chk = (key, label, hint) => h`<label class="row" style="margin:6px 0"><input type="checkbox" data-c="settings-sec" data-key="${key}" ${s[key] ? A.raw("checked") : ""} ${dis}><span class="grow"><b>${label}</b>${hint ? h`<br><span class="muted small">${hint}</span>` : ""}</span></label>`;
    const sel = (key, opts) => h`<select data-c="settings-sec" data-key="${key}" ${dis}>${opts.map((n) => h`<option value="${n}" ${+s[key] === n ? A.raw("selected") : ""}>${n < 60 ? `${n} minutes` : `${n / 60} hour${n === 60 ? "" : "s"}`}</option>`)}</select>`;
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "settings-sec-set");
    const sess = A.store.all("sessions");
    const body = h`<div class="grid g2">
        <section class="card"><h2>Multi-factor sign-in</h2>${chk("mfaRequired", "Require a second factor for everyone", "Authenticator app, security key or the identity provider's MFA. Turning this off needs another owner's approval.")}
          ${s.mfaRequired ? A.ui.status("active", "Required for all staff") : A.ui.status("blocked", "Not required")}</section>
        <section class="card"><h2>Single sign-on</h2>
          ${chk("ssoMicrosoft", "Microsoft work accounts", "Staff sign in with their firm Microsoft account.")}
          ${chk("ssoGoogle", "Google Workspace accounts", "")}
          ${chk("ssoGeneric", "Other identity provider (SAML 2.0 or OpenID Connect)", s.genericIssuer ? `Issuer: ${s.genericIssuer}` : "Not configured")}
          ${btn("Configure provider", "settings-sso", { id: "" }, canSec ? "" : "Your role cannot change security settings")}</section>
        <section class="card"><h2>Idle timeout</h2><dl class="kv"><dt>Staff</dt><dd>${sel("idleMinutes", [15, 30, 60, 120, 480])}</dd><dt>Client portal</dt><dd>${sel("clientIdleMinutes", [5, 10, 15, 30])}</dd></dl>
          <p class="muted small">After this long without activity the session locks and the user signs in again.</p></section></div>
      ${pend.length ? h`<h3 style="margin:18px 0 8px;font-size:14.5px">Waiting for approval</h3>${pend.map((g) => A.ui.gateCard(g))}` : ""}
      <h3 style="margin:18px 0 8px;font-size:14.5px">Open sessions</h3>
      ${A.ui.table([
        { label: "User", render: (x) => h`<b>${A.user(x.userId).name}</b><br><span class="muted small">${A.roleName(A.user(x.userId).role)}</span>` },
        { label: "Device", render: (x) => h`${x.device}<br><span class="muted small">${x.place} · ${x.ip}</span>` },
        { label: "Sign-in", key: "method" },
        { label: "Last active", render: (x) => h`${A.dateTime(x.last)}<br><span class="muted small">since ${A.dateTime(x.started)}</span>` },
        { label: "", render: (x) => (x.active ? btn("Revoke", "settings-revoke", { id: x.id }, canSec ? "" : "Your role cannot end sessions", "danger") : A.ui.status("stopped", x.endedBy === "offboarding" ? "Ended (offboarding)" : "Revoked")) },
      ], sess, { empty: "No sessions" })}`;
    shell(el, "settings/security", "Sign-in & security", "How people sign in and how long sessions stay open.", body);
  }
  A.change("settings-sec", (el) => {
    if (!A.can("admin.security", "act")) return A.toast("Your role cannot change security settings", "bad");
    const key = el.dataset.key, value = el.type === "checkbox" ? el.checked : +el.value;
    if (key === "mfaRequired" && !value) {
      if (A.store.where("approvals", (g) => g.status === "pending" && g.effect === "settings-sec-set").length) { A.toast("A change is already waiting for approval", "bad"); return A.refresh(); }
      A.gate({ kind: "security", cap: "admin.security", title: "Stop requiring multi-factor sign-in", summary: `Requested by ${me().name}. Staff could sign in with a password alone.`, preparedBy: me().id, effect: "settings-sec-set", payload: { key, value: false },
        checks: [{ label: "Approver is not the requester", ok: true }, { label: "Weakens sign-in for every user", ok: false }] });
      A.toast("Sent for approval; MFA stays on until approved"); return A.refresh();
    }
    A.store.put("secSettings", { id: "firm", [key]: value });
    A.audit("security.setting_changed", { target: key, note: String(value) });
  });
  A.action("settings-sso", () => {
    if (!A.can("admin.security", "act")) return;
    const s = sec();
    A.modal("Other identity provider", h`<div class="form"><label class="field">Issuer or metadata URL<input name="issuer" class="input" value="${s.genericIssuer || "https://id.lindaklaw.example.ca/realms/staff"}"></label>
      <label class="field">Client ID<input name="client" class="input" value="counsel-app"></label></div><p class="muted small">The client secret is entered once and never shown again.</p>`,
    [{ label: "Cancel" }, { label: "Save", primary: true, run: (d) => {
      const v = A.formValues(d); if (!/^https:\/\//.test(v.issuer)) { A.toast("Use an https URL", "bad"); return false; }
      A.store.put("secSettings", { id: "firm", genericIssuer: v.issuer, ssoGeneric: true }); A.audit("security.sso_configured", { note: v.issuer });
    } }]);
  });
  A.action("settings-revoke", (el) => {
    const x = A.store.get("sessions", el.dataset.id); if (!x || !x.active || !A.can("admin.security", "act")) return;
    A.store.put("sessions", { id: x.id, active: false, endedBy: me().id }); A.audit("session.revoked", { target: x.userId, note: x.device }); A.toast(`Signed ${A.user(x.userId).name} out of ${x.device}`);
  });

  /* ---------- Support access ---------- */
  function supportView(el) {
    expireSupport();
    const canReq = A.can("admin.users", "act");
    const list = A.store.all("supportAccess");
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "settings-support");
    const body = h`<div class="card" style="margin-bottom:14px"><h2>Vendor support viewing your data</h2>
        <p class="small muted" style="margin:0 0 8px">Our support staff cannot see your firm's data unless an owner allows it. Access is read-only, limited to the scope you set, ends on its own, and every screen they open is logged here.</p>
        ${btn("Log a support request", "settings-support-req", { id: "" }, canReq ? "" : "Your role cannot request support access", "primary")}</div>
      ${pend.length ? h`<h3 style="margin:0 0 8px;font-size:14.5px">Waiting for an owner</h3>${pend.map((g) => A.ui.gateCard(g))}` : ""}
      ${A.ui.table([
        { label: "Ticket", render: (s) => h`<b>${s.ticket}</b><br><span class="muted small">${s.reason}</span>` },
        { label: "Scope", key: "scope" },
        { label: "Status", render: (s) => h`${A.ui.status(SUPPORT_ST[s.status] || s.status, s.status === "active" ? `Active until ${A.dateTime(s.expires)}` : undefined)}${s.granted ? h`<br><span class="muted small">granted ${A.dateTime(s.granted)} by ${A.user(s.approvedBy).name}</span>` : ""}` },
        { label: "Screens viewed", num: true, render: (s) => s.views ?? 0 },
        { label: "", render: (s) => (s.status === "active" ? btn("End now", "settings-support-end", { id: s.id }, canReq ? "" : "Your role cannot end support access", "danger") : "") },
      ], list, { empty: "Support has never had access" })}`;
    shell(el, "settings/support", "Support access", "Time-limited, logged consent for vendor support.", body);
  }
  A.action("settings-support-req", () => {
    if (!A.can("admin.users", "act")) return;
    A.modal("Support access request", h`<div class="form">
      <label class="field">Support ticket<input name="ticket" class="input" value="SUP-${20500 + A.store.all("supportAccess").length}"></label>
      <label class="field">What support needs to look at<input name="reason" class="input" value="Calendar sync shows deadlines a day late"></label>
      <label class="field">Scope<select name="scope"><option>Settings screens only</option><option>Settings and billing screens; no documents or messages</option><option>One matter, read-only, including documents</option></select></label>
      <label class="field">Ends after<select name="hours"><option value="4">4 hours</option><option value="24" selected>24 hours</option><option value="72">72 hours</option></select></label></div>
      <p class="muted small">An owner approves. Access starts at approval and ends on its own.</p>`,
    [{ label: "Cancel" }, { label: "Send to owner", primary: true, run: (d) => {
      const v = A.formValues(d); if (!v.reason.trim() || !v.ticket.trim()) { A.toast("Ticket and reason are required", "bad"); return false; }
      const s = A.store.put("supportAccess", { id: A.id("sa"), ticket: v.ticket.trim(), reason: v.reason.trim(), scope: v.scope, hours: +v.hours, status: "requested", requestedBy: me().id, views: 0 });
      A.gate({ kind: "security", cap: "admin.security", title: `Let vendor support view firm data (${s.ticket})`, summary: `${s.scope}; ${s.hours} hours; read-only. ${s.reason}.`, preparedBy: me().id, effect: "settings-support", payload: { id: s.id, hours: s.hours, scope: s.scope },
        checks: [{ label: "Read-only", ok: true }, { label: "Ends automatically", ok: true }, { label: "Every screen viewed is logged", ok: true }] });
      A.audit("support.access_requested", { target: s.id, note: s.ticket }); A.toast("Sent to an owner");
    } }]);
  });
  A.action("settings-support-end", (el) => { const s = A.store.get("supportAccess", el.dataset.id); if (!s || s.status !== "active" || !A.can("admin.users", "act")) return; A.store.put("supportAccess", { id: s.id, status: "revoked", expires: A.now() }); A.audit("support.access_revoked", { target: s.id, note: s.ticket }); A.toast("Support access ended"); });

  /* ---------- Data residency ---------- */
  function residencyView(el) {
    const live = A.store.where("runs", (r) => r.status === "done").slice(0, 5).map((r) => { const m = r.matterId && A.store.get("matters", r.matterId);
      return { id: r.id, at: r.finished || r.started, task: r.title, matter: m ? m.number : "firm", model: "Drafting model", region: "Canada · Toronto", tokensIn: 1200 + r.title.length * 37, tokensOut: 180 + r.title.length * 5, kept: "Not retained" }; });
    const rows = [...live, ...A.store.all("inferenceLog")];
    const body = h`<div class="stats">${A.ui.stat("Stored data", "Canada", "Montréal primary, Toronto backup; encrypted at rest")}
        ${A.ui.stat("Model inference", "Canada", "Toronto and Montréal endpoints only")}
        ${A.ui.stat("Calls outside Canada", "0", "since the firm opened its account", "ok")}
        ${A.ui.stat("Training on firm data", "Never", "prompts and outputs are not retained")}</div>
      <p class="muted small">Supports the Law Society's guidance on cloud computing and PIPEDA accountability: you can show where every agent call ran. Sample of recent calls below; the full log exports from the audit trail.</p>
      ${A.ui.table([
        { label: "When", render: (r) => A.dateTime(r.at) },
        { label: "Task", render: (r) => h`<b>${r.task}</b><br><span class="muted small">${r.matter}</span>` },
        { label: "Model", key: "model" },
        { label: "Inference region", render: (r) => A.ui.status("confirmed", r.region) },
        { label: "Tokens in / out", num: true, render: (r) => `${r.tokensIn.toLocaleString("en-CA")} / ${r.tokensOut.toLocaleString("en-CA")}` },
        { label: "Retention", key: "kept" },
      ], rows)}`;
    shell(el, "settings/residency", "Data residency", "Where firm data is stored and where the agent's models run.", body);
  }



  /* ---------- Role editor (draft -> duty check -> owner approval) ---------- */
  const LEVEL_IDS = ["none", "view", "own", "all", "approve"];
  const LICENSEE = ["owner", "lawyer", "associate"];
  const LEGAL_AREAS = ["intake", "conflicts", "documents", "matters", "deadlines", "trust", "billing"];
  const FIXED_ROLES = { agent: "The agent's grants are fixed; it never approves.", client: "Client access comes from the portal settings." };
  A.seed("roleGrants", []);
  /** Apply approved role edits on top of access-data.js as core grant overrides (Reset demo clears them and re-runs this). */
  const applyGrants = () => A.setGrantOverrides(Object.fromEntries(A.store.all("roleGrants").map((r) => [r.role, r.changes || {}])));
  A.onReady(applyGrants);
  A.effect("settings-role-grants", (p) => {
    const cur = A.store.get("roleGrants", "rg-" + p.role) || { changes: {} };
    A.store.put("roleGrants", { id: "rg-" + p.role, role: p.role, changes: { ...cur.changes, ...p.changes } });
    applyGrants();
    A.audit("role.grants_changed", { target: p.role, note: `${Object.keys(p.changes).length} capabilities: ${p.reason}` });
  });
  const roleDraft = {}; let roleEdArea = "intake"; const roleCheckRun = {};
  /** Duty rules run against a role draft: hard problems block submit. */
  function dutyCheck(roleId, draft) {
    const lvl = (c) => (c in draft ? draft[c] : A.grant(c, roleId));
    const out = [];
    A.caps.forEach((c) => {
      const g = lvl(c.id), area = capArea(c.id);
      if (g === "approve" && !LICENSEE.includes(roleId) && LEGAL_AREAS.includes(area) && !(roleId === "bookkeeper" && /^(billing|trust)\./.test(c.id) && A.grant(c.id, roleId) === "approve"))
        out.push({ hard: true, text: `${c.name}: approve on legal or trust work needs a licensee role.` });
      if (roleId === "admin" && g === "approve") out.push({ hard: true, text: `${c.name}: the system administrator never approves.` });
      if (roleId === "auditor" && RANK_OF(g) > 1) out.push({ hard: true, text: `${c.name}: auditors are read-only.` });
    });
    if (lvl("trust.authorize") === "approve" && lvl("trust.request") && RANK_OF(lvl("trust.request")) >= 2) out.push({ hard: false, text: "This role can both request and authorize trust payments; each request still needs a different signatory." });
    return out;
  }
  const RANK_OF = (g) => LEVEL_IDS.indexOf(g);
  function roleEditView(el, params) {
    const r = A.roles.find((x) => x.id === params.id);
    if (!r) return shell(el, "settings/roles", "Role not found", "", A.ui.empty("No such role", "Pick a role from Roles & duties."));
    const draft = roleDraft[r.id] || (roleDraft[r.id] = {});
    const fixed = FIXED_ROLES[r.id];
    const canEdit = A.can("admin.roles", "act");
    const why = fixed || (!canEdit ? "Your role cannot edit roles" : me().role === r.id ? "Nobody edits the role they hold" : "");
    const areas = [...new Set(A.caps.map((c) => capArea(c.id)))];
    const changed = Object.keys(draft).filter((c) => draft[c] !== A.grant(c, r.id));
    const left = A.ui.list(areas.map((a) => { const n = changed.filter((c) => capArea(c) === a).length; return { id: a, on: a === roleEdArea, title: a[0].toUpperCase() + a.slice(1), sub: `${A.caps.filter((c) => capArea(c.id) === a).length} capabilities`, right: n ? A.ui.status("draft", `${n} changed`) : "" }; }), "settings-roled-area");
    const rows = A.caps.filter((c) => capArea(c.id) === roleEdArea);
    const centre = h`<div class="card"><h2>${roleEdArea[0].toUpperCase() + roleEdArea.slice(1)}</h2>${A.ui.table([
      { label: "Capability", render: (c) => h`<b>${c.name}</b><br><span class="muted small">${c.id}${LEGAL_AREAS.includes(capArea(c.id)) ? " · approve needs a licensee" : ""}</span>` },
      { label: "Level", render: (c) => { const v = c.id in draft ? draft[c.id] : A.grant(c.id, r.id);
        return h`<select data-c="settings-roled-level" data-cap="${c.id}" aria-label="Level for ${c.name}" ${why ? A.raw(`disabled title="${A.esc(why)}"`) : ""}>${LEVEL_IDS.map((l) => h`<option value="${l}" ${l === v ? A.raw("selected") : ""}>${l}</option>`)}</select>${v !== A.grant(c.id, r.id) ? h` ${A.ui.status("draft", "Changed")}` : ""}`; } },
    ], rows)}</div>`;
    const probs = dutyCheck(r.id, draft); const hard = probs.filter((p) => p.hard);
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "settings-role-grants" && g.payload && g.payload.role === r.id);
    const ran = roleCheckRun[r.id];
    const right = h`<div class="card"><h2>Changes</h2>${changed.length ? h`<ul class="small" style="margin:0 0 8px;padding-left:18px">${changed.map((c) => h`<li>${A.capName(c)}: ${A.grant(c, r.id)} → <b>${draft[c]}</b></li>`)}</ul>` : h`<p class="muted small">No changes yet. Pick a level to start a draft.</p>`}
      <h2 style="margin-top:12px">Duty check</h2>${ran ? h`<p class="small" style="margin:0 0 6px">${A.ui.actor("agent", `Checked ${A.dateTime(ran)}`)}</p>` : ""}
      ${probs.length ? h`<ul class="small" style="margin:0;padding-left:18px">${probs.map((p) => h`<li style="margin-bottom:4px">${A.ui.status(p.hard ? "blocked" : "due-soon", p.hard ? "Blocks submit" : "Watch")} ${p.text}</li>`)}</ul>` : h`<p class="small">${A.ui.status("done", "No conflicts")} with the duty rules.</p>`}
      <div class="row wrap" style="gap:6px;margin-top:10px">${btn("✦ Check duties", "settings-roled-check", { id: r.id }, fixed || "", "agent")}
        ${btn("Discard", "settings-roled-discard", { id: r.id }, why || (changed.length ? "" : "Nothing to discard"))}
        ${btn("Submit for owner approval", "settings-roled-submit", { id: r.id }, why || (!changed.length ? "No changes" : hard.length ? "Fix the blocking duty problems first" : pend.length ? "A change to this role is already waiting" : ""), "primary")}</div>
      ${pend.length ? h`<div style="margin-top:10px">${pend.map((g) => A.ui.gateCard(g, { compact: true }))}</div>` : ""}</div>`;
    shell(el, "settings/roles", `Role: ${r.name}`, r.desc || "", h`<div style="display:grid;grid-template-columns:minmax(160px,220px) minmax(0,1fr) minmax(240px,320px);gap:14px;align-items:start" class="roled">${left}${centre}${right}</div>
      <p class="muted small">Edits are a draft until a firm owner who did not make them approves. Every change is in the access log with its reason.</p>`,
      { status: changed.length ? "draft" : pend.length ? "needs-approval" : "", actions: [{ label: "All roles", to: "settings/roles", small: true }] });
  }
  A.action("settings-roled-area", (el) => { roleEdArea = el.dataset.id; A.refresh(); });
  A.change("settings-roled-level", (el) => { const rid = (A.route().split("/")[2] || ""); if (!rid || FIXED_ROLES[rid] || !A.can("admin.roles", "act") || me().role === rid) return; (roleDraft[rid] || (roleDraft[rid] = {}))[el.dataset.cap] = el.value; A.refresh(); });
  A.action("settings-roled-discard", (el) => { roleDraft[el.dataset.id] = {}; A.refresh(); });
  A.action("settings-roled-check", (el) => {
    const rid = el.dataset.id;
    A.agent.run({ title: `Duty check: ${A.roleName(rid)} draft`, steps: ["Reading the draft", "Running separation-of-duties rules", "Listing conflicts"], done: () => { roleCheckRun[rid] = A.now(); A.refresh(); } });
  });
  A.action("settings-roled-submit", (el) => {
    const rid = el.dataset.id, draft = roleDraft[rid] || {};
    if (FIXED_ROLES[rid] || !A.can("admin.roles", "act") || me().role === rid) return;
    const changes = Object.fromEntries(Object.entries(draft).filter(([c, v]) => v !== A.grant(c, rid)));
    if (!Object.keys(changes).length || dutyCheck(rid, draft).some((p) => p.hard)) return;
    A.modal(`Submit changes to ${A.roleName(rid)}`, h`<p class="small">${Object.keys(changes).length} capabilit${Object.keys(changes).length === 1 ? "y" : "ies"} change for everyone with this role.</p><label class="field">Reason (shown in the access log)<input name="reason" class="input" value="Align grants with current duties"></label>`,
      [{ label: "Cancel" }, { label: "Send to an owner", primary: true, run: (d) => {
        const v = A.formValues(d); if (!v.reason.trim()) { A.toast("Give a reason", "bad"); return false; }
        A.gate({ kind: "access", cap: "admin.roles", title: `Change ${A.roleName(rid)} role: ${Object.keys(changes).length} capabilities`, summary: v.reason.trim(), preparedBy: me().id,
          body: String(h`<ul>${Object.entries(changes).map(([c, l]) => h`<li>${A.capName(c)}: ${A.grant(c, rid)} → ${l}</li>`)}</ul>`), effect: "settings-role-grants", payload: { role: rid, changes, reason: v.reason.trim() },
          checks: [{ label: "Duty rules pass", ok: true, blocking: true }, { label: "Approver is a firm owner who did not make the change", ok: true }, { label: "Requester does not hold this role", ok: me().role !== rid, blocking: true }] });
        roleDraft[rid] = {}; A.toast("Role change sent to an owner");
      } }]);
  });

  /* ---------- Firm & account: profile, portal, notifications, email, API, subscription, escrow, status ---------- */
  const demo = h`<span class="badge chip" title="No backend in this prototype; the screen runs on sample data">demo</span>`;
  A.seed("firmProfile", [{ id: "firm", name: "Linda K Law", legal: "Linda K Law Professional Corporation", address: "220 Bay St, Suite 1400, Toronto, ON M5J 2W4", phone: "416-555-0100", lso: "", hst: "78412 3305 RT0001", fye: "12-31", numFormat: "YYYY-####", taxRate: 13,
    accounts: [{ id: "ac-trust", kind: "Mixed trust account", bank: "TD Canada Trust", transit: "10452", last4: "4417" }, { id: "ac-gen", kind: "General account", bank: "TD Canada Trust", transit: "10452", last4: "9025" }] }]);
  A.seed("practiceAreas", [
    { id: "pa-lit", name: "Civil litigation", stages: ["Intake complete", "Pleadings", "Discoveries", "Mediation", "Trial"], retention: "10 years after close", skills: "Deadlines, pleadings drafts", rateCents: 45000, status: "active" },
    { id: "pa-est", name: "Estates and probate", stages: ["Intake complete", "Listing the estate's assets", "Probate application", "Administration", "Final accounts"], retention: "Permanent (wills)", skills: "Probate forms, asset letters", rateCents: 39500, status: "active" },
    { id: "pa-re", name: "Residential real estate", stages: ["Intake complete", "Requisitions", "Closing", "Reporting"], retention: "15 years after close", skills: "Requisition letters, statements of adjustments", rateCents: 0, status: "active" },
    { id: "pa-emp", name: "Employment", stages: ["Intake complete", "Demand", "Negotiation", "Settlement"], retention: "10 years after close", skills: "Severance calculations, demand letters", rateCents: 42500, status: "active" },
  ]);
  A.seed("portalSettings", [{ id: "firm", clientMfa: "required", signIn: "password_mfa", sessionMaxHours: 12, can: { message: true, upload: true, pay: true, sign: true, calendar: false, stage: true }, uploadMaxMb: 25, uploadTypes: "PDF, JPG, PNG, DOCX", notice: "You have a new item from Linda K Law. Sign in to the portal to read it.", afterCloseDays: 90, autoReply: "We reply within one business day. Office hours are 9 a.m. to 5 p.m., Monday to Friday.", colour: "#1f4f46" }]);
  A.seed("notifPrefs", []);
  A.seed("emailSettings", [{ id: "firm", syncScope: "contacts", neverFile: ["*@alerts.td.example.ca", "Folder: Personal"], domain: "lindaklaw.example.ca", dns: { spf: true, dkim: true, dmarc: false }, aliases: ["intake@lindaklaw.example.ca", "accounts@lindaklaw.example.ca"] }]);
  A.seed("mailboxes", [
    { id: "mb-1", userId: "u-linda", address: "linda@lindaklaw.example.ca", status: "active", signature: "Linda Kassim\nPrincipal lawyer, Linda K Law\n416-555-0100" },
    { id: "mb-2", userId: "u-meraf", address: "meraf@lindaklaw.example.ca", status: "active", signature: "" },
  ]);
  A.seed("apiKeys", [
    { id: "ak-1", name: "Accounting export", owner: "u-dawit", scopes: ["billing:read", "time:read"], created: "2026-07-02", expires: "2027-01-02", last: "2026-10-09 06:00", status: "active", tail: "a91f" },
    { id: "ak-2", name: "Old intake form", owner: "u-dawit", scopes: ["intake:write"], created: "2025-11-10", expires: "2026-05-10", last: "2026-05-09 17:22", status: "expired", tail: "03bc" },
  ]);
  A.seed("webhooks", [{ id: "wh-1", url: "https://hooks.lindaklaw.example.ca/counsel", events: ["matter.created", "matter.closed", "bill.issued"], expires: "2027-04-01", status: "active" }]);
  A.seed("webhookDeliveries", [
    { id: "wd-1", hook: "wh-1", event: "bill.issued", at: "2026-10-08 16:41", code: 200, tries: 1 },
    { id: "wd-2", hook: "wh-1", event: "matter.created", at: "2026-10-06 11:02", code: 503, tries: 3 },
    { id: "wd-3", hook: "wh-1", event: "matter.created", at: "2026-10-06 11:20", code: 200, tries: 1 },
  ]);
  A.seed("account", [{ id: "acct", plan: "Practice", status: "active", budgetCents: 120000, card: "Visa ending 4242", costNote: "" }]);
  A.seed("usageMonths", [["2026-05", 28430], ["2026-06", 33110], ["2026-07", 30980], ["2026-08", 36420], ["2026-09", 39870], ["2026-10", 41260]].map(([m, c]) => ({ id: "um-" + m, month: m, cents: c })));
  A.seed("serviceInvoices", [["2026-08", 9], ["2026-09", 9], ["2026-10", 9]].map(([m, seats]) => ({ id: "si-" + m, number: `S-${m}`, date: `${m}-01`, seats, cents: seats * 9900 })));
  A.seed("escrow", [{ id: "escrow", dest: "lindaklaw-escrow", region: "ca-central-1 (Montréal)", schedule: "daily", key: "Firm-held key: lkl-escrow-2026", status: "healthy" }]);
  A.seed("escrowRuns", [["2026-10-09", 3.42, "3f9c…e21a"], ["2026-10-08", 3.41, "b07d…91c4"], ["2026-10-07", 3.41, "e5a2…0d7f"]].map(([d, gb, hash], i) => ({ id: "er-" + i, at: `${d} 02:00`, gb, hash, check: "match", status: "done" })));
  A.seed("restoreTests", [{ id: "rt-1", at: "2026-07-14 03:10", result: "Counts and hashes match: 8 matters, 412 documents, 1,206 ledger rows", status: "filed" }]);
  A.seed("statusComponents", [
    { id: "sc-app", name: "App", up: 99.96, status: "operational", down: [41] }, { id: "sc-agent", name: "Agent", up: 99.9, status: "operational", down: [12, 63] },
    { id: "sc-email", name: "Email filing", up: 99.93, status: "operational", down: [28] }, { id: "sc-bank", name: "Bank feed", up: 99.7, status: "degraded", down: [5, 33, 89] },
    { id: "sc-pay", name: "Payments", up: 99.98, status: "operational", down: [] },
  ]);
  A.seed("statusIncidents", [
    { id: "si-1", title: "Bank feed delayed for one bank", comp: "sc-bank", start: "2026-10-09 07:40", end: "", impact: "Transactions from one bank arrive up to 4 hours late. Trust balances are unaffected; reconciliation waits for the feed.", touchesData: false },
    { id: "si-2", title: "Agent runs queued", comp: "sc-agent", start: "2026-08-18 14:05", end: "2026-08-18 14:52", impact: "Agent drafts took up to 20 minutes. No data was exposed.", touchesData: false },
    { id: "si-3", title: "Email filing sent copies to the wrong matter inbox", comp: "sc-email", start: "2026-07-29 10:15", end: "2026-07-29 11:02", impact: "Six messages were filed to another firm matter in the same firm cell before filing paused.", touchesData: true },
  ]);
  A.seed("statusSubs", []);

  const firmDoc = () => A.store.get("firmProfile", "firm") || {};
  const acct = () => A.store.get("account", "acct") || { budgetCents: 1 };
  const budgetUsed = () => { const m = A.store.all("usageMonths").slice().sort((a, b) => (a.month < b.month ? -1 : 1)).pop(); return m ? m.cents : 0; };
  const SUPPORT_ST = { active: "in-progress", requested: "needs-approval", expired: "closed", revoked: "stopped", declined: "declined" };
  /** Invoice totals in integer cents: fees + disbursements + HST on taxable items, less trust applied. */
  const toC = (d) => Math.round((+d || 0) * 100);
  const invTotal = (i) => { const fees = (i.lines || []).reduce((s, l) => s + toC(l.amount), 0); const dis = (i.disb || []).reduce((s, x) => s + toC(x.amount), 0); const tax = Math.round(((fees + (i.disb || []).filter((x) => x.taxable).reduce((s, x) => s + toC(x.amount), 0)) * (firmDoc().taxRate || 13)) / 100); return fees + dis + tax; };
  const invOwing = (i) => Math.max(0, invTotal(i) - toC(i.trustApplied) - toC(i.paid));

  const FIRM_TABS = [
    { id: "firm", label: "Firm profile", can: () => A.can("platform.firm_profile") },
    { id: "firm/portal", label: "Client portal", can: () => A.can("platform.portal") || A.can("admin.security") },
    { id: "firm/notifications", label: "Notifications", can: () => true },
    { id: "firm/email", label: "Email", can: () => true },
    { id: "firm/api", label: "API & webhooks", can: () => A.can("platform.integrations") },
    { id: "firm/subscription", label: "Subscription", can: () => A.can("platform.agent_settings") },
    { id: "firm/escrow", label: "Data escrow", can: () => A.can("admin.users") || A.can("admin.export") },
    { id: "firm/status", label: "Status", can: () => true },
  ];
  function firmShell(el, route, title, sub, body, opts = {}) {
    A.setContext({ kind: "settings", id: route, label: `Firm · ${title}` });
    const tab = FIRM_TABS.find((t) => t.id === route);
    const tabs = A.ui.tabs(FIRM_TABS.filter((t) => t.can()), route, "firm-tab");
    const inner = tab && !tab.can() ? h`<div class="locked"><b>No access</b>${A.roleName(me().role)} cannot open ${tab.label}.</div>` : body;
    A.render(el, h`<div class="page">${A.ui.header({ title, sub, status: opts.status, meta: opts.demo ? demo : "", primary: opts.primary, actions: opts.actions })}${tabs}${inner}</div>`);
  }
  A.action("firm-tab", (el) => A.go(el.dataset.id));
  const canFirm = () => A.can("platform.firm_profile", "act");
  const sect = (title, body, right) => h`<section class="card" style="margin-bottom:14px"><div class="row" style="margin-bottom:6px"><h2 class="grow" style="margin:0">${title}</h2>${right || ""}</div>${body}</section>`;

  /* Firm profile */
  let selArea = "pa-lit";
  A.inlineEdit("firm-field", (key, v) => { if (!canFirm()) return; const val = String(v).trim(); if (!val && ["name", "address", "hst", "numFormat"].includes(key)) { A.toast("This field is required", "bad"); return false; }
    if (key === "numFormat" && !/#/.test(val)) { A.toast("Include # for the running number, e.g. YYYY-####", "bad"); return false; }
    A.store.put("firmProfile", { id: "firm", [key]: val }); A.audit("firm.profile_changed", { target: key, note: val }); });
  A.inlineEdit("firm-area-rate", (id, v) => { if (!canFirm()) return; if (!(v >= 0)) return false; A.store.put("practiceAreas", { id, rateCents: Math.round(v * 100) }); A.audit("firm.rate_changed", { target: id, note: A.money(v) }); });
  A.effect("firm-account", (p) => { const f = firmDoc(); A.store.put("firmProfile", { id: "firm", accounts: (f.accounts || []).map((a) => (a.id === p.id ? { ...a, bank: p.bank, transit: p.transit, last4: p.last4 } : a)) }); A.audit("firm.account_changed", { target: p.id, note: `${p.bank} …${p.last4}` }); });
  function firmView(el) {
    const f = firmDoc(), ed = canFirm(), why = ed ? "" : "Your role can view the firm profile only";
    const fld = (key, label, note) => h`<dt>${label}</dt><dd>${A.ui.inline({ name: "firm-field", id: key, value: f[key] || "", disabled: !ed, why })}${note ? h`<br><span class="muted small">${note}</span>` : ""}</dd>`;
    const year = A.today.slice(0, 4); const next = (f.numFormat || "YYYY-####").replace("YYYY", year).replace("YY", year.slice(2)).replace(/#+/, (m) => String(160).padStart(m.length, "0"));
    const areas = A.store.all("practiceAreas");
    const a = areas.find((x) => x.id === selArea) || areas[0];
    const pendAcc = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "firm-account");
    const areaDetail = a ? h`<div class="card"><div class="row" style="margin-bottom:8px"><b class="grow">${a.name}</b>${A.ui.status(a.status)}${a.status === "draft" ? A.ui.actor("agent", "Agent drafted") : ""}</div>
      <dl class="kv"><dt>Stages</dt><dd>${a.stages.join(" → ")}</dd><dt>Retention</dt><dd>${a.retention}</dd><dt>Default skills</dt><dd>${a.skills}</dd>
      <dt>Default rate</dt><dd>${A.ui.inline({ name: "firm-area-rate", id: a.id, type: "money", value: (a.rateCents || 0) / 100, display: a.rateCents ? `${A.cents(a.rateCents)}/h` : "Flat fee", disabled: !ed, why })}</dd></dl>
      ${a.status === "draft" ? h`<div class="row" style="margin-top:10px;gap:6px">${A.ui.actor("person", "Review, then publish")}${btn("Publish", "firm-area-publish", { id: a.id }, why, "primary")}${btn("Discard", "firm-area-discard", { id: a.id }, why, "danger")}</div>` : ""}</div>` : null;
    const body = h`<div class="grid g2">
      ${sect("Firm details", h`<dl class="kv">${fld("name", "Firm name")}${fld("legal", "Legal name")}${fld("address", "Address")}${fld("phone", "Phone")}${fld("lso", "LSO firm number", "Verify whether the LSO issues firm-level numbers")}${fld("hst", "HST number", "Shown on every bill")}</dl>`)}
      ${sect("Billing, tax and numbering", h`<dl class="kv"><dt>Tax</dt><dd>HST ${f.taxRate || 13}% (Ontario)</dd>${fld("fye", "Fiscal year end (MM-DD)")}${fld("numFormat", "Matter number format", `Next matter: ${next}`)}</dl>`)}</div>
      ${sect("Bank and trust accounts", h`${A.ui.table([{ label: "Account", key: "kind" }, { label: "Bank", key: "bank" }, { label: "Transit", key: "transit" }, { label: "Number", render: (x) => `…${x.last4}` },
        { label: "", render: (x) => btn("Change", "firm-account-edit", { id: x.id }, ed ? (pendAcc.some((g) => g.payload.id === x.id) ? "A change is waiting for an owner" : "") : why) }], f.accounts || [])}
        <p class="muted small">Reference only: no credentials or full account numbers are stored. Changes go to an owner who did not request them.</p>${pendAcc.map((g) => A.ui.gateCard(g, { compact: true }))}`)}
      <div class="row" style="margin:4px 0 8px"><h3 class="grow" style="margin:0;font-size:14.5px">Practice areas</h3>${btn("✦ Draft a practice area", "firm-area-new", { id: "" }, why, "agent")}</div>
      ${A.ui.split(A.ui.list(areas.map((x) => ({ id: x.id, on: a && x.id === a.id, title: x.name, sub: `${x.stages.length} stages · ${x.retention}`, status: x.status === "draft" ? "agent-prepared" : "" })), "firm-area"), areaDetail)}`;
    firmShell(el, "firm", "Firm profile", "Identity, tax and numbering defaults, accounts, and the practice areas that drive stages, retention and skills.", body);
  }
  A.action("firm-area", (el) => { selArea = el.dataset.id; A.refresh(); });
  A.action("firm-area-publish", (el) => { if (!canFirm()) return; A.store.put("practiceAreas", { id: el.dataset.id, status: "active" }); A.audit("firm.practice_area_published", { target: el.dataset.id }); A.toast("Practice area published", "ok"); });
  A.action("firm-area-discard", (el) => { if (!canFirm()) return; A.store.remove("practiceAreas", el.dataset.id); selArea = "pa-lit"; A.toast("Draft discarded"); });
  A.action("firm-area-new", () => {
    if (!canFirm()) return;
    A.modal("Draft a practice area", h`<label class="field">Practice area<select name="kind"><option value="fam">Family law</option><option value="corp">Corporate and commercial</option><option value="imm">Immigration</option></select></label><p class="muted small">The agent drafts stages, retention and skills. You review and publish.</p>`,
      [{ label: "Cancel" }, { label: "✦ Draft", primary: true, run: (d) => {
        const k = A.formValues(d).kind;
        const T = { fam: ["Family law", ["Intake complete", "Financial disclosure", "Case conference", "Settlement conference", "Order"], "10 years after close", "Form 13.1 financial statements, separation agreements"],
          corp: ["Corporate and commercial", ["Intake complete", "Due diligence", "Drafting", "Closing", "Post-closing"], "10 years after close", "Minute books, share purchase agreements"],
          imm: ["Immigration", ["Intake complete", "Document gathering", "Application filed", "Decision"], "10 years after close", "Document checklists, submission letters"] }[k];
        A.agent.run({ title: `Draft practice area: ${T[0]}`, steps: ["Reading firm templates", "Drafting stages and checklists", "Setting retention class"], done: () => {
          const x = A.store.put("practiceAreas", { id: A.id("pa"), name: T[0], stages: T[1], retention: T[2], skills: T[3], rateCents: 40000, status: "draft" }); selArea = x.id;
          return { title: `${T[0]} drafted for review`, outputs: [{ label: "Review", go: "firm" }] }; } });
      } }]);
  });
  A.action("firm-account-edit", (el) => {
    if (!canFirm()) return; const a = (firmDoc().accounts || []).find((x) => x.id === el.dataset.id); if (!a) return;
    A.modal(`Change ${a.kind}`, h`<div class="form"><label class="field">Bank<input name="bank" class="input" value="${a.bank}"></label><label class="field">Transit (5 digits)<input name="transit" class="input" value="${a.transit}"></label><label class="field">Last 4 digits<input name="last4" class="input" value="${a.last4}"></label></div><p class="muted small">An owner who did not request this approves before bills and trust receipts show the new account.</p>`,
      [{ label: "Cancel" }, { label: "Send to an owner", primary: true, run: (d) => {
        const v = A.formValues(d); if (!/^\d{5}$/.test(v.transit) || !/^\d{4}$/.test(v.last4) || !v.bank.trim()) { A.toast("Bank, a 5-digit transit and 4 digits are required", "bad"); return false; }
        A.gate({ kind: "security", cap: "admin.security", title: `Change ${a.kind} to ${v.bank.trim()} …${v.last4}`, summary: `Requested by ${me().name}. Was ${a.bank} …${a.last4}.`, preparedBy: me().id, effect: "firm-account", payload: { id: a.id, bank: v.bank.trim(), transit: v.transit, last4: v.last4 },
          checks: [{ label: "Approver is not the requester", ok: true }, { label: "Bank letter on file (confirm before approving)", ok: true }] });
        A.toast("Account change sent to an owner");
      } }]);
  });

  /* Client portal settings */
  const ps = () => A.store.get("portalSettings", "firm") || { can: {} };
  const canPortal = () => A.can("platform.portal", "all") || A.can("admin.security", "act");
  A.effect("firm-portal-set", (p) => { A.store.put("portalSettings", { id: "firm", [p.key]: p.value }); A.audit("portal.setting_changed", { target: p.key, note: String(p.value) }); });
  const CLIENT_CAN = [["message", "Send messages"], ["upload", "Upload documents"], ["pay", "Pay invoices"], ["sign", "Sign documents"], ["calendar", "See key dates"], ["stage", "See the current step"]];
  function portalSettingsView(el) {
    const s = ps(), ed = canPortal(), why = ed ? "" : "Your role cannot change portal settings";
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "firm-portal-set");
    const dis = ed ? "" : A.raw(`disabled title="${A.esc(why)}"`);
    const num = (key, label, unit) => h`<dt>${label}</dt><dd><input class="input" type="number" min="1" style="max-width:90px" data-c="firm-portal" data-key="${key}" value="${s[key]}" ${dis}> <span class="muted small">${unit}</span></dd>`;
    const body = h`<div class="grid g2">
      ${sect("Branding", h`<dl class="kv"><dt>Name shown</dt><dd>${firmDoc().name}</dd><dt>Colour</dt><dd><input type="color" data-c="firm-portal" data-key="colour" value="${s.colour}" ${dis}></dd><dt>Logo</dt><dd>${btn("Upload logo", "firm-portal-logo", { id: "" }, why)}</dd></dl>`)}
      ${sect("Sign-in", h`<dl class="kv"><dt>Second factor</dt><dd><select data-c="firm-portal" data-key="clientMfa" ${dis}><option value="required" ${s.clientMfa === "required" ? A.raw("selected") : ""}>Required</option><option value="optional" ${s.clientMfa === "optional" ? A.raw("selected") : ""}>Optional</option></select> ${s.clientMfa === "required" ? A.ui.status("active", "Required") : A.ui.status("due-soon", "Optional")}</dd>
        <dt>Method</dt><dd><select data-c="firm-portal" data-key="signIn" ${dis}><option value="password_mfa" ${s.signIn === "password_mfa" ? A.raw("selected") : ""}>Password + code</option><option value="magic_link_plus_device" ${s.signIn !== "password_mfa" ? A.raw("selected") : ""}>Email link + known device</option></select></dd>
        <dt>Idle timeout</dt><dd>${sec().clientIdleMinutes || 15} minutes <span class="muted small">(set under Settings · Sign-in)</span></dd>${num("sessionMaxHours", "Session lifetime", "hours, absolute")}</dl>
        <p class="muted small">Making the second factor optional goes to an owner who did not request it.</p>`)}
      ${sect("What clients may do", h`${CLIENT_CAN.map(([k, l]) => h`<label class="row" style="margin:4px 0"><input type="checkbox" data-c="firm-portal-can" data-key="${k}" ${s.can[k] ? A.raw("checked") : ""} ${dis}> ${l}</label>`)}
        <dl class="kv" style="margin-top:8px">${num("uploadMaxMb", "Largest upload", "MB")}<dt>File types</dt><dd>${A.ui.inline({ name: "firm-portal-text", id: "uploadTypes", value: s.uploadTypes, disabled: !ed, why })}</dd></dl>`)}
      ${sect("Notices and after close", h`<dl class="kv"><dt>New-item email</dt><dd>${A.ui.inline({ name: "firm-portal-text", id: "notice", value: s.notice, disabled: !ed, why })}<br><span class="muted small">Content-free: never names the matter or quotes a message.</span></dd>
        <dt>Auto-reply</dt><dd>${A.ui.inline({ name: "firm-portal-text", id: "autoReply", value: s.autoReply, disabled: !ed, why })}</dd>${num("afterCloseDays", "Read-only after close", "days")}</dl>
        <div class="row" style="margin-top:8px">${btn("Preview notice email", "firm-portal-preview", { id: "" }, "")}</div>`)}</div>
      ${pend.length ? h`<h3 style="margin:4px 0 8px;font-size:14.5px">Waiting for an owner</h3>${pend.map((g) => A.ui.gateCard(g))}` : ""}`;
    firmShell(el, "firm/portal", "Client portal settings", "Firm-wide portal behaviour: branding, sign-in, what clients may do, notices and access after close.", body,
      { actions: [{ label: "Preview as client", to: "portal", small: true }] });
  }
  A.change("firm-portal", (el) => {
    if (!canPortal()) return A.toast("Your role cannot change portal settings", "bad");
    const key = el.dataset.key, value = el.type === "number" ? Math.max(1, +el.value || 1) : el.value;
    if (key === "clientMfa" && value === "optional") {
      if (A.store.where("approvals", (g) => g.status === "pending" && g.effect === "firm-portal-set" && g.payload.key === "clientMfa").length) { A.toast("Already waiting for an owner", "bad"); return A.refresh(); }
      A.gate({ kind: "security", cap: "admin.security", title: "Make the client second factor optional", summary: `Requested by ${me().name}. Clients could sign in with a password alone.`, preparedBy: me().id, effect: "firm-portal-set", payload: { key, value },
        checks: [{ label: "Approver is not the requester", ok: true }, { label: "Weakens client sign-in", ok: false }] });
      A.toast("Sent to an owner; the second factor stays required until approved"); return A.refresh();
    }
    A.store.put("portalSettings", { id: "firm", [key]: value }); A.audit("portal.setting_changed", { target: key, note: String(value) });
  });
  A.change("firm-portal-can", (el) => { if (!canPortal()) return; A.store.put("portalSettings", { id: "firm", can: { ...ps().can, [el.dataset.key]: el.checked } }); A.audit("portal.setting_changed", { target: "client_can." + el.dataset.key, note: String(el.checked) }); });
  A.inlineEdit("firm-portal-text", (key, v) => { if (!canPortal()) return; const val = String(v).trim();
    if (key === "notice" && /\{|matter|file \d|\d{4}-\d{4}/i.test(val)) { A.toast("Keep the notice content-free: no matter names, numbers or placeholders", "bad"); return false; }
    A.store.put("portalSettings", { id: "firm", [key]: val }); A.audit("portal.setting_changed", { target: key }); });
  A.action("firm-portal-logo", () => { if (!canPortal()) return; A.store.put("portalSettings", { id: "firm", logo: "logo-lkl.svg" }); A.audit("portal.setting_changed", { target: "logo" }); A.toast("Logo uploaded (demo)"); });
  A.action("firm-portal-preview", () => { const s = ps(); A.modal("Notice email preview", h`<div class="card" style="border-left:4px solid ${s.colour}"><p class="small muted">From: ${firmDoc().name} &lt;portal@${(A.store.get("emailSettings", "firm") || {}).domain || "lindaklaw.example.ca"}&gt;<br>Subject: New item in your portal</p><p>${s.notice}</p><p class="small muted">Sign in: portal link. ${s.clientMfa === "required" ? "You will be asked for a one-time code." : ""}</p></div>`, [{ label: "Close" }]); });

  /* Notification settings */
  const EVENTS = ["approval waiting", "approval overdue", "deadline approaching", "deadline changed", "client message", "client upload", "payment received", "trust exception", "mention", "work item assigned", "sync failure", "export done", "agent paused"];
  const CHANNELS = [["inapp", "In-app"], ["email", "Email"], ["digest", "Daily digest"], ["push", "Mobile push"], ["text", "Text"]];
  const LOCKED_EV = { "deadline approaching": ["inapp", "email"], "approval overdue": ["inapp", "email"], "trust exception": ["inapp", "email"] };
  const baseGrid = () => Object.fromEntries(EVENTS.map((e) => [e, { inapp: true, email: /approval|deadline|trust|client message/.test(e), digest: !/approval waiting|mention/.test(e), push: /deadline approaching|approval overdue/.test(e), text: false }]));
  const firmNp = () => A.store.get("notifPrefs", "np-firm") || { id: "np-firm", grid: baseGrid() };
  const myNp = () => A.store.get("notifPrefs", "np-" + me().id) || { id: "np-" + me().id, grid: {}, quiet: "20:00-07:00", ooo: "", delegate: "" };
  let npScope = "mine";
  function notificationsView(el) {
    const firmMode = npScope === "firm" && A.can("admin.users", "act");
    const f = firmNp().grid, mine = myNp();
    const val = (e, c) => (LOCKED_EV[e] || []).includes(c) || (firmMode ? f[e][c] : (mine.grid[e] && c in mine.grid[e] ? mine.grid[e][c] : f[e][c]));
    const delegates = users().filter((u) => u.role !== "client" && u.id !== me().id && statusOf(u) === "active");
    const body = h`${A.ui.filters({ chips: [{ id: "mine", label: "My notifications" }, ...(A.can("admin.users", "act") ? [{ id: "firm", label: "Firm defaults" }] : [])], active: firmMode ? "firm" : "mine", action: "firm-np-scope",
        right: h`<span class="small muted">Notification emails never contain client content, only a link.</span>` })}
      <div class="tblwrap"><table class="tbl"><thead><tr><th>Event</th>${CHANNELS.map(([, l]) => h`<th>${l}</th>`)}</tr></thead><tbody>
      ${EVENTS.map((e) => h`<tr><td>${e[0].toUpperCase() + e.slice(1)}${LOCKED_EV[e] ? h` ${A.ui.status("hold", "Firm-locked")}` : ""}</td>${CHANNELS.map(([c]) => { const lock = (LOCKED_EV[e] || []).includes(c);
        return h`<td><input type="checkbox" aria-label="${e} by ${c}" data-c="firm-np" data-ev="${e}" data-ch="${c}" ${val(e, c) ? A.raw("checked") : ""} ${lock ? A.raw('disabled title="Always delivered: firm rule"') : ""}></td>`; })}</tr>`)}
      </tbody></table></div>
      ${firmMode ? h`<p class="muted small">Firm defaults apply to everyone who has not changed a row. Locked rows cannot be turned off.</p>` : h`<div class="grid g2" style="margin-top:14px">
        ${sect("Quiet hours", h`<select data-c="firm-np-quiet" aria-label="Quiet hours">${[["", "None"], ["20:00-07:00", "8 p.m. to 7 a.m."], ["18:00-08:00", "6 p.m. to 8 a.m."], ["22:00-06:00", "10 p.m. to 6 a.m."]].map(([v, l]) => h`<option value="${v}" ${mine.quiet === v ? A.raw("selected") : ""}>${l}</option>`)}</select>
          <p class="muted small">Non-urgent items wait until morning. Deadlines inside 48 hours still go through.</p>`)}
        ${sect("Out of office", h`<div class="row wrap" style="gap:6px"><label class="small">Until <input type="date" class="input" data-c="firm-np-ooo" value="${mine.ooo || ""}"></label>
          <select data-c="firm-np-delegate" aria-label="Delegate"><option value="">No delegate</option>${delegates.map((u) => h`<option value="${u.id}" ${mine.delegate === u.id ? A.raw("selected") : ""}>${u.name}</option>`)}</select></div>
          <p class="muted small">${mine.ooo && mine.delegate ? `Until ${A.date(mine.ooo)}, approvals assigned to you go to ${A.user(mine.delegate).name}, who still needs the right role to decide.` : "Pick a date and a delegate to route your approvals while you are away."}</p>`)}</div>`}`;
    firmShell(el, "firm/notifications", "Notifications", firmMode ? "Firm defaults and rows nobody can turn off." : `What reaches ${me().name} and how.`, body);
  }
  A.action("firm-np-scope", (el) => { npScope = el.dataset.id; A.refresh(); });
  A.change("firm-np", (el) => {
    const e = el.dataset.ev, c = el.dataset.ch; if ((LOCKED_EV[e] || []).includes(c)) return;
    if (npScope === "firm" && A.can("admin.users", "act")) { const f = firmNp(); f.grid[e] = { ...f.grid[e], [c]: el.checked }; A.store.put("notifPrefs", { id: "np-firm", grid: f.grid }); A.audit("notifications.firm_default_changed", { target: e, note: `${c}: ${el.checked}` }); return; }
    const m = myNp(); A.store.put("notifPrefs", { ...m, grid: { ...m.grid, [e]: { ...(m.grid[e] || {}), [c]: el.checked } } });
  });
  A.change("firm-np-quiet", (el) => A.store.put("notifPrefs", { ...myNp(), quiet: el.value }));
  A.change("firm-np-ooo", (el) => A.store.put("notifPrefs", { ...myNp(), ooo: el.value }));
  A.change("firm-np-delegate", (el) => { A.store.put("notifPrefs", { ...myNp(), delegate: el.value }); if (el.value) A.audit("notifications.delegate_set", { target: el.value }); });

  /* Email filing and sending */
  const es = () => A.store.get("emailSettings", "firm") || { neverFile: [], dns: {}, aliases: [] };
  const SCOPES = [["contacts", "Mail with matter contacts"], ["contacts_and_staff", "Contacts and staff-to-staff mail"], ["everyone", "All mail in connected inboxes"]];
  const SCOPE_N = { contacts: 214, contacts_and_staff: 371, everyone: 1288 };
  let emailTab = "filing", scopePreview = null;
  function emailView(el) {
    const s = es(), adm = A.can("admin.users", "act"), why = adm ? "" : "Only the administrator changes firm email settings";
    const mine = A.store.where("mailboxes", (b) => b.userId === me().id);
    const tabs = A.ui.tabs([{ id: "filing", label: "Filing" }, { id: "sending", label: "Sending" }, { id: "signatures", label: "Signatures" }], emailTab, "firm-email-tab");
    let body;
    if (emailTab === "filing") body = h`${sect("What is filed", h`<select data-c="firm-email-scope" aria-label="Filing scope" ${adm ? "" : A.raw(`disabled title="${A.esc(why)}"`)}>${SCOPES.map(([v, l]) => h`<option value="${v}" ${(scopePreview || s.syncScope) === v ? A.raw("selected") : ""}>${l}</option>`)}</select>
        ${scopePreview ? h`<div class="card" style="margin-top:10px"><p class="small" style="margin:0 0 8px">${A.ui.status("draft", "Preview")} Over the last 30 days this would file <b>${SCOPE_N[scopePreview]}</b> messages instead of ${SCOPE_N[s.syncScope]}. Never-file rules still win.</p>
          <div class="row" style="gap:6px">${btn("Apply", "firm-email-scope-apply", { id: scopePreview }, why, "primary")}${btn("Cancel", "firm-email-scope-cancel", { id: "" }, "")}</div></div>` : h`<p class="muted small">Changing the scope shows a preview before anything applies.</p>`}`)}
      ${sect("Never file", h`${A.ui.table([{ label: "Rule", key: "rule" }, { label: "", render: (r) => btn("Remove", "firm-email-nf-del", { id: r.id }, why, "danger") }], s.neverFile.map((rule, i) => ({ id: String(i), rule })), { empty: "No rules" })}`, btn("Add rule", "firm-email-nf-add", { id: "" }, why))}
      ${sect("Matter inbound addresses", h`<p class="small">Each matter has an address like <code>2026-0142@in.${s.domain}</code>. Only these registered addresses may forward to it:</p><p class="small">${s.aliases.join(", ")}</p>`)}`;
    else if (emailTab === "sending") body = h`${sect("Sender domain", h`<dl class="kv"><dt>Domain</dt><dd>${s.domain}</dd>${["spf", "dkim", "dmarc"].map((k) => h`<dt>${k.toUpperCase()}</dt><dd>${s.dns[k] ? A.ui.status("confirmed", "Found") : A.ui.status("failed", "Missing")}</dd>`)}</dl>
        <p class="muted small">Until all three records are found, bills and reminders go from the platform address.</p>`, h`${demo} ${btn("Recheck DNS", "firm-email-dns", { id: "" }, why)}`)}
      ${sect("My connected inboxes", h`${A.ui.table([{ label: "Address", key: "address" }, { label: "Status", render: (b) => A.ui.status(b.status) }, { label: "", render: (b) => btn("Disconnect", "firm-email-mb-del", { id: b.id }, "", "danger") }], mine, { empty: "No inbox connected" })}
        <p class="muted small">Connected through the generic mail connector. Bill shares and reminders use your address; template mail uses the firm sender.</p>`, btn("Connect inbox", "firm-email-mb-add", { id: "" }, ""))}`;
    else body = sect("My signature", h`${mine.length ? mine.map((b) => h`<div style="margin-bottom:12px"><b class="small">${b.address}</b>
        <textarea class="input" rows="4" id="sig-${b.id}" style="width:100%;margin-top:4px">${b.draftSig || b.signature || ""}</textarea>
        <div class="row" style="gap:6px;margin-top:6px">${b.draftSig ? A.ui.actor("agent", "Agent drafted; edit and save") : ""}<span class="grow"></span>${btn("✦ Draft signature", "firm-email-sig-draft", { id: b.id }, "", "agent")}${btn("Save", "firm-email-sig-save", { id: b.id }, "", "primary")}</div></div>`) : A.ui.empty("No inbox connected", "Connect an inbox under Sending first.")}
      <p class="muted small">Firm templates (engagement letters, reminders, reporting letters) add the firm footer with the HST number automatically.</p>`);
    firmShell(el, "firm/email", "Email", "What is filed, which addresses send, and signatures. Filing changes are previewed before they apply.", h`${tabs}${body}`);
  }
  A.action("firm-email-tab", (el) => { emailTab = el.dataset.id; A.refresh(); });
  A.change("firm-email-scope", (el) => { if (!A.can("admin.users", "act")) return; scopePreview = el.value === es().syncScope ? null : el.value; A.refresh(); });
  A.action("firm-email-scope-apply", (el) => { if (!A.can("admin.users", "act")) return; A.store.put("emailSettings", { id: "firm", syncScope: el.dataset.id }); A.audit("email.scope_changed", { note: el.dataset.id }); scopePreview = null; A.toast("Filing scope changed", "ok"); });
  A.action("firm-email-scope-cancel", () => { scopePreview = null; A.refresh(); });
  A.action("firm-email-nf-del", (el) => { if (!A.can("admin.users", "act")) return; const s = es(); const r = s.neverFile[+el.dataset.id]; A.store.put("emailSettings", { id: "firm", neverFile: s.neverFile.filter((_, i) => i !== +el.dataset.id) }); A.audit("email.never_file_removed", { note: r }); });
  A.action("firm-email-nf-add", () => {
    if (!A.can("admin.users", "act")) return;
    A.modal("Never file", h`<label class="field">Sender, domain or folder<input name="rule" class="input" value="*@newsletters.example.ca"></label><p class="muted small">Never-file rules always win over the filing scope.</p>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d).rule.trim(); if (!v) { A.toast("Enter a rule", "bad"); return false; } A.store.put("emailSettings", { id: "firm", neverFile: [...es().neverFile, v] }); A.audit("email.never_file_added", { note: v }); } }]);
  });
  A.action("firm-email-dns", () => { if (!A.can("admin.users", "act")) return; A.store.put("emailSettings", { id: "firm", dns: { spf: true, dkim: true, dmarc: true } }); A.toast("DMARC record found; the firm domain now sends", "ok"); });
  A.action("firm-email-mb-del", (el) => { const b = A.store.get("mailboxes", el.dataset.id); if (!b || b.userId !== me().id) return; A.store.remove("mailboxes", b.id); A.audit("email.inbox_disconnected", { note: b.address }); A.toast("Inbox disconnected"); });
  A.action("firm-email-mb-add", () => {
    A.modal("Connect inbox", h`<label class="field">Address<input name="addr" class="input" value="${(me().email || me().name.split(" ")[0].toLowerCase() + "@lindaklaw.example.ca")}"></label><p class="muted small">You sign in with the mail provider; the password is never seen or stored here.</p>`,
      [{ label: "Cancel" }, { label: "Connect", primary: true, run: (d) => { const v = A.formValues(d).addr.trim(); if (!/^[^@\s]+@[^@\s]+\.[a-z]{2,}$/i.test(v)) { A.toast("Enter a valid address", "bad"); return false; }
        if (A.store.where("mailboxes", (b) => b.address === v).length) { A.toast("Already connected", "bad"); return false; }
        A.store.put("mailboxes", { id: A.id("mb"), userId: me().id, address: v, status: "active", signature: "" }); A.audit("email.inbox_connected", { note: v }); A.toast("Inbox connected (demo)", "ok"); } }]);
  });
  A.action("firm-email-sig-draft", (el) => {
    const b = A.store.get("mailboxes", el.dataset.id); if (!b || b.userId !== me().id) return; const u = me(), f = firmDoc();
    A.agent.run({ title: "Draft email signature", steps: ["Reading your profile", "Applying the firm style"], done: () => { A.store.put("mailboxes", { id: b.id, draftSig: `${u.name}\n${u.title || A.roleName(u.role)}, ${f.name}\n${f.address}\n${f.phone}\nThis message may be privileged and confidential.` }); } });
  });
  A.action("firm-email-sig-save", (el) => { const b = A.store.get("mailboxes", el.dataset.id); if (!b || b.userId !== me().id) return; const t = document.getElementById("sig-" + b.id); A.store.put("mailboxes", { id: b.id, signature: t ? t.value : b.signature, draftSig: "" }); A.toast("Signature saved", "ok"); });

  /* API access and webhooks */
  const API_AREAS = ["matters", "contacts", "documents", "time", "billing", "trust", "intake"];
  let apiTab = "keys";
  const canApi = () => A.can("platform.integrations", "all");
  A.effect("firm-api-key", (p) => { A.store.put("apiKeys", { id: p.id, status: "active" }); A.audit("api.key_approved", { target: p.id, note: p.scopes.join(", ") }); });
  function apiView(el) {
    expireApi();
    const why = canApi() ? "" : "Your role can view API access only";
    const tabs = A.ui.tabs([{ id: "apps", label: "Connected apps" }, { id: "keys", label: "API keys" }, { id: "hooks", label: "Webhooks" }, { id: "docs", label: "Docs" }], apiTab, "firm-api-tab");
    const keys = A.store.all("apiKeys"), hooks = A.store.all("webhooks"), dels = A.store.all("webhookDeliveries").slice().sort((a, b) => (a.at < b.at ? 1 : -1));
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "firm-api-key");
    const KEY_ST = { active: "active", pending: "needs-approval", revoked: "stopped", expired: "closed" };
    let body;
    if (apiTab === "apps") body = A.ui.empty("No OAuth apps yet", "OAuth apps arrive in a later phase. Use an API key with read scopes for now.");
    else if (apiTab === "keys") body = h`${A.ui.table([
        { label: "Key", render: (k) => h`<b>${k.name}</b><br><span class="muted small">…${k.tail} · ${A.user(k.owner).name}</span>` },
        { label: "Scopes", render: (k) => h`<span class="small">${k.scopes.join(", ")}</span>` },
        { label: "Expires", render: (k) => A.ui.due(k.expires, { done: k.status !== "active", soon: 30 }) },
        { label: "Last used", render: (k) => (k.last ? A.dateTime(k.last) : h`<span class="muted">Never</span>`) },
        { label: "Status", render: (k) => A.ui.status(KEY_ST[k.status] || k.status, k.status === "expired" ? "Expired" : undefined) },
        { label: "", render: (k) => (k.status === "active" || k.status === "pending" ? btn("Revoke", "firm-api-revoke", { id: k.id }, why, "danger") : "") },
      ], keys, { empty: "No API keys" })}${pend.map((g) => A.ui.gateCard(g, { compact: true }))}
      <p class="muted small">Keys are shown once at creation and stored hashed. Write scopes need an owner's approval; trust write is never offered.</p>`;
    else if (apiTab === "hooks") body = h`${A.ui.table([
        { label: "Endpoint", render: (w) => h`<code class="small">${w.url}</code>` }, { label: "Events", render: (w) => h`<span class="small">${w.events.join(", ")}</span>` },
        { label: "Expires", render: (w) => A.ui.due(w.expires, { soon: 30 }) }, { label: "Status", render: (w) => A.ui.status(w.status) },
        { label: "", render: (w) => h`<div class="row">${btn("Send test", "firm-hook-test", { id: w.id }, why)}${btn("Delete", "firm-hook-del", { id: w.id }, why, "danger")}</div>` },
      ], hooks, { empty: "No webhooks" })}
      <h3 style="margin:14px 0 8px;font-size:14.5px">Delivery log</h3>${A.ui.table([{ label: "When", render: (d) => A.dateTime(d.at) }, { label: "Event", render: (d) => h`<code class="small">${d.event}</code>` }, { label: "Response", render: (d) => A.ui.status(d.code < 300 ? "done" : "failed", `HTTP ${d.code}`) }, { label: "Tries", num: true, key: "tries" }], dels.slice(0, 20), { empty: "No deliveries yet" })}`;
    else body = h`<div class="card"><p class="small">OpenAPI 3.1 description of the v1 API: read endpoints for matters, contacts, documents, time and billing; write endpoints for intake and contacts. Version: 2026-10.</p>${btn("Download openapi.json", "firm-api-spec", { id: "" }, "")}</div>`;
    firmShell(el, "firm/api", "API access and webhooks", "Let the firm's own tools read or write data with scoped, expiring keys. Every grant is visible and revocable.", h`<div class="card" style="margin-bottom:12px;padding:8px 12px"><span class="small">${A.ui.status("active", "Live")} API keys and webhooks · ${A.ui.status("scheduled", "Later")} OAuth apps and wider write scopes</span></div>${tabs}${body}`,
      { demo: true, primary: apiTab === "hooks" ? { label: "Add webhook", a: "firm-hook-add", key: "n", disabled: !canApi(), why } : { label: "Create API key", a: "firm-api-new", key: "n", disabled: !canApi(), why } });
  }
  function expireApi() { A.store.where("apiKeys", (k) => k.status === "active" && k.expires < A.today).forEach((k) => A.store.put("apiKeys", { id: k.id, status: "expired" })); }
  A.action("firm-api-tab", (el) => { apiTab = el.dataset.id; A.refresh(); });
  A.action("firm-api-spec", () => { A.audit("api.spec_downloaded"); A.toast("openapi.json downloaded (demo)"); });
  A.action("firm-api-new", () => {
    if (!canApi()) return;
    A.modal("Create API key", h`<div class="form"><label class="field">Name<input name="name" class="input" value="Practice dashboard"></label>
      <label class="field">Expires<select name="days"><option value="30">30 days</option><option value="90" selected>90 days</option><option value="365">1 year (firm maximum)</option></select></label></div>
      <table class="tbl" style="margin-top:8px"><thead><tr><th>Area</th><th>Read</th><th>Write</th></tr></thead><tbody>${API_AREAS.map((a) => h`<tr><td>${a}</td><td><input type="checkbox" name="r_${a}" ${a === "matters" ? A.raw("checked") : ""}></td><td>${a === "trust" ? h`<span class="muted small">Never</span>` : h`<input type="checkbox" name="w_${a}">`}</td></tr>`)}</tbody></table>`,
      [{ label: "Cancel" }, { label: "Create", primary: true, run: (d) => {
        const v = A.formValues(d); const scopes = [...API_AREAS.filter((a) => v["r_" + a]).map((a) => a + ":read"), ...API_AREAS.filter((a) => a !== "trust" && v["w_" + a]).map((a) => a + ":write")];
        if (!v.name.trim() || !scopes.length) { A.toast("Name the key and pick at least one scope", "bad"); return false; }
        const write = scopes.some((x) => /:write$/.test(x));
        const secret = "lkl_" + A.id("k").replace(/[^a-z0-9]/gi, "") + "x7Qm2Rv9";
        const k = A.store.put("apiKeys", { id: A.id("ak"), name: v.name.trim(), owner: me().id, scopes, created: A.today, expires: A.addDays(A.today, +v.days), last: "", status: write ? "pending" : "active", tail: secret.slice(-4) });
        A.audit("api.key_created", { target: k.id, note: scopes.join(", ") });
        if (write) A.gate({ kind: "security", cap: "platform.integrations", title: `API key with write access: ${k.name}`, summary: scopes.join(", "), preparedBy: me().id, effect: "firm-api-key", payload: { id: k.id, scopes }, checks: [{ label: "No trust write scope", ok: true, blocking: true }, { label: "Expires within the firm maximum", ok: true }] });
        setTimeout(() => A.modal("Copy your key now", h`<p class="small">This is the only time the key is shown.${write ? " It works once an owner approves the write scopes." : ""}</p><code style="display:block;padding:8px;word-break:break-all">${secret}</code>`, [{ label: "Done" }]), 0);
      } }]);
  });
  A.action("firm-api-revoke", (el) => { if (!canApi()) return; const k = A.store.get("apiKeys", el.dataset.id); if (!k) return; A.store.put("apiKeys", { id: k.id, status: "revoked" });
    A.store.where("approvals", (g) => g.status === "pending" && g.effect === "firm-api-key" && g.payload.id === k.id).forEach((g) => A.store.put("approvals", { id: g.id, status: "dismissed" }));
    A.audit("api.key_revoked", { target: k.id, note: k.name }); A.toast("Key revoked"); });
  A.action("firm-hook-add", () => {
    if (!canApi()) return;
    const EV = ["matter.created", "matter.closed", "bill.issued", "payment.received", "document.filed"];
    A.modal("Add webhook", h`<label class="field">HTTPS endpoint<input name="url" class="input" value="https://hooks.lindaklaw.example.ca/reports"></label><div style="margin-top:8px">${EV.map((e) => h`<label class="row"><input type="checkbox" name="e_${e}" ${e === "bill.issued" ? A.raw("checked") : ""}> <code class="small">${e}</code></label>`)}</div><p class="muted small">Payloads carry ids and the event, not document text. Expires after one year.</p>`,
      [{ label: "Cancel" }, { label: "Add", primary: true, run: (d) => { const v = A.formValues(d); const events = EV.filter((e) => v["e_" + e]);
        if (!/^https:\/\/[^\s/]+\.[^\s]+/.test(v.url)) { A.toast("HTTPS endpoints only", "bad"); return false; } if (!events.length) { A.toast("Pick at least one event", "bad"); return false; }
        const w = A.store.put("webhooks", { id: A.id("wh"), url: v.url.trim(), events, expires: A.addDays(A.today, 365), status: "active" }); A.audit("api.webhook_added", { target: w.id, note: w.url }); } }]);
  });
  A.action("firm-hook-test", (el) => { if (!canApi()) return; A.store.put("webhookDeliveries", { id: A.id("wd"), hook: el.dataset.id, event: "test.ping", at: A.now(), code: 200, tries: 1 }); A.toast("Test delivered: HTTP 200 (demo)", "ok"); });
  A.action("firm-hook-del", (el) => { if (!canApi()) return; A.store.remove("webhooks", el.dataset.id); A.audit("api.webhook_deleted", { target: el.dataset.id }); A.toast("Webhook deleted"); });

  /* Subscription and usage */
  const PLANS = [["Solo", 6900], ["Practice", 9900], ["Firm", 14900]];
  A.effect("firm-cancel", () => { A.store.put("account", { id: "acct", status: "cancelling" }); A.audit("account.cancel_approved", { note: "Exit flow started: full export, 60-day notice, deletion certificate" }); });
  A.inlineEdit("firm-budget", (_, v) => { if (!canFirm()) return; if (!(v > 0)) { A.toast("Enter a monthly amount", "bad"); return false; } A.store.put("account", { id: "acct", budgetCents: Math.round(v * 100) }); A.audit("account.budget_changed", { note: A.money(v) }); });
  function subscriptionView(el) {
    const a = acct(), ed = canFirm(), why = ed ? "" : "Your role can view the subscription only";
    const seats = users().filter((u) => u.role !== "client" && statusOf(u) === "active").length;
    const price = (PLANS.find((p) => p[0] === a.plan) || PLANS[1])[1];
    const months = A.store.all("usageMonths").slice().sort((x, y) => (x.month < y.month ? -1 : 1));
    const max = Math.max(a.budgetCents, ...months.map((m) => m.cents));
    const used = budgetUsed(), pct = Math.round((100 * used) / a.budgetCents);
    const chart = h`<div style="display:flex;align-items:flex-end;gap:10px;height:140px;position:relative;border-bottom:1px solid var(--line);margin:8px 0 4px">
      <div style="position:absolute;left:0;right:0;bottom:${(100 * a.budgetCents) / max}%;border-top:1px dashed var(--bad)" title="Budget ${A.cents(a.budgetCents)}"></div>
      ${months.map((m) => h`<div style="flex:1;display:flex;flex-direction:column;justify-content:flex-end;height:100%" title="${A.cents(m.cents)}"><div style="background:var(--agent,#2a7f78);height:${(100 * m.cents) / max}%;border-radius:3px 3px 0 0"></div></div>`)}</div>
      <div style="display:flex;gap:10px">${months.map((m) => h`<span class="muted small" style="flex:1;text-align:center">${A.date(m.month + "-01").replace(/ \d+,?/, "")}</span>`)}</div>`;
    const pend = A.store.where("approvals", (g) => g.status === "pending" && g.effect === "firm-cancel");
    const body = h`<div class="stats">${A.ui.stat("Plan", a.plan, `${A.cents(price)} per seat per month + HST`)}${A.ui.stat("Seats", seats, "active staff; clients are free")}
        ${A.ui.stat("Agent usage, October", A.cents(used), `${pct}% of budget`, pct >= 80 ? "gate" : "")}${A.ui.stat("Next invoice", A.cents(Math.round(seats * price * 1.13)), `${A.date(A.addDays(A.today.slice(0, 8) + "01", 31).slice(0, 8) + "01")} incl. HST`)}</div>
      <div class="grid g2">
        ${sect("Agent usage vs budget", h`${chart}<dl class="kv" style="margin-top:10px"><dt>Monthly budget</dt><dd>${A.ui.inline({ name: "firm-budget", id: "acct", type: "money", value: a.budgetCents / 100, display: A.cents(a.budgetCents), disabled: !ed, why })}</dd></dl>
          <p class="muted small">Above the budget the agent asks before costly jobs. Alerts at 80%.</p>
          ${a.costNote ? h`<div class="card" style="margin-top:8px">${A.ui.actor("agent", "Agent draft")}<p class="small" style="margin:6px 0 0">${a.costNote}</p></div>` : ""}`, btn("✦ What drives cost", "firm-cost-explain", { id: "" }, "", "agent"))}
        ${sect("Payment", h`<dl class="kv"><dt>Status</dt><dd>${A.ui.status(a.status === "cancelling" ? "stopped" : a.status, a.status === "cancelling" ? "Cancelling" : undefined)}</dd><dt>Method</dt><dd>${a.card} <span class="muted small">held by the payment processor; never stored here</span></dd></dl>
          <div class="row wrap" style="gap:6px;margin-top:10px">${btn("Change plan", "firm-plan", { id: "" }, why)}${btn("Update card", "firm-card", { id: "" }, why)}${btn("Cancel service", "firm-cancel-req", { id: "" }, why || (a.status === "cancelling" ? "Already cancelling" : pend.length ? "Waiting for an owner" : ""), "danger")}</div>
          ${pend.map((g) => A.ui.gateCard(g, { compact: true }))}`)}</div>
      <h3 style="margin:4px 0 8px;font-size:14.5px">Invoices from us</h3>
      ${A.ui.table([{ label: "Invoice", key: "number" }, { label: "Date", render: (i) => A.date(i.date) }, { label: "Seats", num: true, key: "seats" }, { label: "Subtotal", num: true, render: (i) => A.cents(i.cents) }, { label: "HST", num: true, render: (i) => A.cents(Math.round(i.cents * 0.13)) }, { label: "Total", num: true, render: (i) => A.cents(i.cents + Math.round(i.cents * 0.13)) }, { label: "", render: (i) => btn("Download", "firm-inv-dl", { id: i.id }, "") }], A.store.all("serviceInvoices").slice().reverse())}`;
    firmShell(el, "firm/subscription", "Subscription and usage", "The firm's own account: plan, seats, invoices from us and agent usage against budget.", body, { demo: true, status: a.status === "cancelling" ? "stopped" : "" });
  }
  A.action("firm-cost-explain", () => {
    A.agent.run({ title: "Explain agent cost this month", steps: ["Reading usage by skill and matter", "Comparing with last month"], done: () => {
      A.store.put("account", { id: "acct", costNote: "Most of October's increase is pre-bill narratives for 2026-0149 and conflict memos for three new leads. Moving pre-bills to weekly instead of daily would save about $40 a month." }); } });
  });
  A.action("firm-plan", () => { if (!canFirm()) return; A.modal("Change plan", h`<label class="field">Plan<select name="plan">${PLANS.map(([p, c]) => h`<option value="${p}" ${p === acct().plan ? A.raw("selected") : ""}>${p} · ${A.cents(c)} per seat</option>`)}</select></label><p class="muted small">Takes effect on the next invoice.</p>`,
    [{ label: "Cancel" }, { label: "Change", primary: true, run: (d) => { const p = A.formValues(d).plan; A.store.put("account", { id: "acct", plan: p }); A.audit("account.plan_changed", { note: p }); A.toast(`Plan changed to ${p}`); } }]); });
  A.action("firm-card", () => { if (!canFirm()) return; A.audit("account.payment_method_opened"); A.toast("Opens the payment processor's secure page (demo)"); });
  A.action("firm-inv-dl", (el) => { A.audit("account.invoice_downloaded", { target: el.dataset.id }); A.toast("Invoice PDF downloaded (demo)"); });
  A.action("firm-cancel-req", () => {
    if (!canFirm()) return;
    A.modal("Cancel service", h`<p class="small">Cancelling starts the exit flow: a full export of every matter, document, ledger and audit row; a 60-day notice period; then deletion with a certificate. An owner who did not request it signs off.</p><label class="field">Reason<input name="reason" class="input"></label>`,
      [{ label: "Keep service" }, { label: "Send to an owner", primary: true, run: (d) => { const v = A.formValues(d); if (!v.reason.trim()) { A.toast("Give a reason", "bad"); return false; }
        A.gate({ kind: "security", cap: "admin.export", title: "Cancel the firm's subscription", summary: v.reason.trim(), preparedBy: me().id, effect: "firm-cancel", payload: {}, checks: [{ label: "Full export runs before deletion", ok: true }, { label: "Trust records retained as the Law Society requires", ok: true }] }); A.toast("Sent to an owner"); } }]);
  });

  /* Data escrow */
  const escDoc = () => A.store.get("escrow", "escrow") || {};
  const canEscrow = () => A.can("admin.users", "act") || A.can("admin.export", "approve");
  const CA_REGIONS = ["ca-central-1 (Montréal)", "ca-west-1 (Calgary)", "northamerica-northeast2 (Toronto)"];
  A.effect("firm-escrow-dest", (p) => { A.store.put("escrow", { id: "escrow", dest: p.dest, region: p.region }); A.audit("escrow.destination_changed", { note: `${p.dest} · ${p.region}` }); });
  function escrowView(el) {
    const e = escDoc(), why = canEscrow() ? "" : "Your role can view escrow only";
    const runs = A.store.all("escrowRuns").slice().sort((a, b) => (a.at < b.at ? 1 : -1));
    const tests = A.store.all("restoreTests").slice().sort((a, b) => (a.at < b.at ? 1 : -1));
    const pend = A.store.where("approvals", (g) => g.status === "pending" && /^firm-escrow/.test(g.effect));
    const body = h`<div class="stats">${A.ui.stat("Destination", e.dest, e.region)}${A.ui.stat("Schedule", e.schedule === "daily" ? "Daily, 02:00" : "Weekly, Sunday 02:00", "Toronto time")}
        ${A.ui.stat("Last run", runs[0] ? A.dateTime(runs[0].at) : "Never", runs[0] ? `${runs[0].gb} GB · hash ${runs[0].check}` : "")}${A.ui.stat("Last restore test", tests[0] ? A.date(tests[0].at.slice(0, 10)) : "Never", "quarterly")}</div>
      <div class="row wrap" style="gap:6px;margin-bottom:12px">${btn("Run now", "firm-escrow-run", { id: "" }, why, "primary")}${btn("✦ Restore test", "firm-escrow-test", { id: "" }, why, "agent")}${btn("Change destination", "firm-escrow-dest", { id: "" }, why || (pend.length ? "A change is waiting for an owner" : ""))}
        <select data-c="firm-escrow-sched" aria-label="Schedule" ${why ? A.raw(`disabled title="${A.esc(why)}"`) : ""}><option value="daily" ${e.schedule === "daily" ? A.raw("selected") : ""}>Daily</option><option value="weekly" ${e.schedule !== "daily" ? A.raw("selected") : ""}>Weekly</option></select></div>
      ${pend.map((g) => A.ui.gateCard(g))}
      <div class="grid g2">${sect("Runs", A.ui.table([{ label: "Run", render: (r) => A.dateTime(r.at) }, { label: "Size", num: true, render: (r) => `${r.gb} GB` }, { label: "Hash", render: (r) => h`<code class="small">${r.hash}</code>` }, { label: "Status", render: (r) => A.ui.status(r.check === "match" ? "done" : "failed", r.check === "match" ? "Hash match" : "Mismatch") }], runs.slice(0, 10)))}
        ${sect("Restore tests", h`${A.ui.table([{ label: "When", render: (t) => A.dateTime(t.at) }, { label: "Result", render: (t) => h`<span class="small">${t.result}</span>` }, { label: "", render: (t) => t.status === "agent-prepared" ? h`<div class="row">${A.ui.status("agent-prepared")}${btn("File as evidence", "firm-escrow-file", { id: t.id }, why, "primary")}</div>` : A.ui.status("filed", "Filed") }], tests)}
          <p class="muted small">Restores into a scratch cell and compares counts and hashes. Filed results show in the trust center.</p>`)}</div>
      <p class="muted small">Encryption key: ${e.key}. The key never leaves the firm; we cannot read the export. Destinations outside Canada are refused.</p>`;
    firmShell(el, "firm/escrow", "Data escrow", "Scheduled encrypted export of the whole firm to storage the firm owns in Canada, with restore-test evidence.", body, { demo: true, status: e.status === "healthy" ? "active" : e.status });
  }
  A.action("firm-escrow-run", () => { if (!canEscrow()) return; const n = A.store.all("escrowRuns").length; A.store.put("escrowRuns", { id: A.id("er"), at: A.now(), gb: 3.43, hash: ["9a1e…44b0", "c3d8…7f12", "51fa…b9e3"][n % 3], check: "match", status: "done" }); A.audit("escrow.run", { note: "manual" }); A.toast("Export finished; hash verified (demo)", "ok"); });
  A.action("firm-escrow-test", () => {
    if (!canEscrow()) return;
    A.agent.run({ title: "Escrow restore test", steps: ["Restoring the latest export into a scratch cell", "Comparing record counts", "Comparing file hashes"], done: () => {
      const docs = A.store.all("docs").length, ms = A.store.all("matters").length;
      A.store.put("restoreTests", { id: A.id("rt"), at: A.now(), result: `Counts and hashes match: ${ms} matters, ${docs} documents`, status: "agent-prepared" });
      return { title: "Restore test passed; ready to file", outputs: [{ label: "Review", go: "firm/escrow" }] }; } });
  });
  A.action("firm-escrow-file", (el) => { if (!canEscrow()) return; A.store.put("restoreTests", { id: el.dataset.id, status: "filed", filedBy: me().id }); A.audit("escrow.restore_test_filed", { target: el.dataset.id }); A.toast("Filed in the trust center", "ok"); });
  A.change("firm-escrow-sched", (el) => { if (!canEscrow()) return; A.store.put("escrow", { id: "escrow", schedule: el.value }); A.audit("escrow.schedule_changed", { note: el.value }); });
  A.action("firm-escrow-dest", () => {
    if (!canEscrow()) return;
    A.modal("Change escrow destination", h`<div class="form"><label class="field">Bucket<input name="dest" class="input" value="${escDoc().dest}"></label><label class="field">Region<select name="region">${[...CA_REGIONS, "us-east-1 (Virginia)"].map((r) => h`<option ${r === escDoc().region ? A.raw("selected") : ""}>${r}</option>`)}</select></label></div><p class="muted small">An owner approves where firm data goes.</p>`,
      [{ label: "Cancel" }, { label: "Send to an owner", primary: true, run: (d) => { const v = A.formValues(d);
        if (!CA_REGIONS.includes(v.region)) { A.toast("Refused: escrow must stay in a Canadian region", "bad"); return false; } if (!v.dest.trim()) { A.toast("Name the bucket", "bad"); return false; }
        A.gate({ kind: "security", cap: "admin.export", title: `Escrow destination: ${v.dest.trim()} (${v.region})`, summary: `Requested by ${me().name}.`, preparedBy: me().id, effect: "firm-escrow-dest", payload: { dest: v.dest.trim(), region: v.region }, checks: [{ label: "Region is in Canada", ok: true, blocking: true }, { label: "Bucket owned by the firm", ok: true }] }); A.toast("Sent to an owner"); } }]);
  });

  /* Status page */
  function statusView(el) {
    const comps = A.store.all("statusComponents"), incs = A.store.all("statusIncidents").slice().sort((a, b) => (a.start < b.start ? 1 : -1));
    const subbed = A.store.where("statusSubs", (s) => s.userId === me().id).length;
    const ST = { operational: ["done", "Operational"], degraded: ["due-soon", "Degraded"], outage: ["failed", "Outage"], maintenance: ["scheduled", "Maintenance"] };
    const overall = comps.some((c) => c.status === "outage") ? "outage" : comps.some((c) => c.status !== "operational") ? "degraded" : "operational";
    const bars = (c) => h`<div style="display:flex;gap:1px;height:22px" aria-label="90-day uptime">${Array.from({ length: 90 }, (_, i) => h`<span style="flex:1;border-radius:1px;background:${c.down.includes(i) ? "var(--warn,#c98a1b)" : "var(--ok,#2f8a4c)"}" title="${A.date(A.addDays(A.today, i - 89))}${c.down.includes(i) ? ": degraded" : ""}"></span>`)}</div>`;
    const body = h`${comps.map((c) => h`<div class="card" style="margin-bottom:8px;padding:10px 14px"><div class="row" style="margin-bottom:6px"><b class="grow">${c.name}</b><span class="muted small">${c.up.toFixed(2)}% · 90 days</span> ${A.ui.status(...ST[c.status])}</div>${bars(c)}</div>`)}
      <h3 style="margin:16px 0 8px;font-size:14.5px">Incidents</h3>
      ${incs.map((i) => { const br = A.store.where("approvals", (g) => g.effect === "firm-breach-review" && g.payload && g.payload.id === i.id)[0];
        return h`<div class="card" style="margin-bottom:8px"><div class="row"><b class="grow">${i.title}</b>${A.ui.status(i.end ? "done" : "in-progress", i.end ? "Resolved" : "Ongoing")}</div>
        <p class="muted small" style="margin:4px 0">${(comps.find((c) => c.id === i.comp) || {}).name} · ${A.dateTime(i.start)}${i.end ? ` to ${A.dateTime(i.end).slice(-5)}` : ""}</p><p class="small" style="margin:0">${i.impact}</p>
        ${i.touchesData ? h`<div class="row" style="margin-top:8px;gap:6px">${A.ui.actor("agent", "Flagged as a possible privacy breach")}${br ? A.ui.status(br.status) : btn("Start breach review", "firm-breach", { id: i.id }, A.can("admin.security", "act") ? "" : "The owner or administrator starts the review")}</div>` : ""}</div>`; })}`;
    firmShell(el, "firm/status", "Status", "Uptime and incidents for the firm's cell. Also public at the status address.", body,
      { demo: true, status: ST[overall][0], primary: { label: subbed ? "Unsubscribe" : "Email me updates", a: "firm-status-sub", id: subbed ? "off" : "on" } });
  }
  A.effect("firm-breach-review", (p) => { A.store.put("statusIncidents", { id: p.id, breachReview: "approved" }); A.audit("privacy.breach_review_closed", { target: p.id, note: "PIPEDA real risk of significant harm assessed" }); });
  A.action("firm-status-sub", (el) => { const on = el.dataset.id === "on"; if (on) A.store.put("statusSubs", { id: "sub-" + me().id, userId: me().id, email: me().email || me().name }); else A.store.remove("statusSubs", "sub-" + me().id); A.toast(on ? "You will get status emails" : "Unsubscribed"); });
  A.action("firm-breach", (el) => {
    if (!A.can("admin.security", "act")) return; const i = A.store.get("statusIncidents", el.dataset.id); if (!i) return;
    A.agent.run({ title: `Breach review: ${i.title}`, steps: ["Reading the incident timeline", "Listing affected records", "Assessing real risk of significant harm"], done: () => A.gate({ kind: "security", cap: "admin.security", title: `Privacy breach review: ${i.title}`, preparedBy: "agent",
      summary: "Six messages reached another matter inside the firm; no one outside the firm. Draft assessment: low risk of significant harm; record the breach, notify affected clients by letter, no report to the Privacy Commissioner.",
      body: String(h`<p>${i.impact}</p><p>PIPEDA requires a breach record kept for 24 months, and a report and notice only where there is a real risk of significant harm.</p>`), effect: "firm-breach-review", payload: { id: i.id },
      checks: [{ label: "Affected records listed", ok: true }, { label: "Owner decides on notification", ok: true }] }) });
  });

  /* ---------- Access log (append-only view of access events) ---------- */
  const LOG_KINDS = [
    { id: "", label: "All access events" }, { id: "user.", label: "Users" }, { id: "wall.", label: "Walls" }, { id: "breakglass.", label: "Break-glass" },
    { id: "session.", label: "Sessions" }, { id: "security.", label: "Security" }, { id: "support.", label: "Support" }, { id: "access_review.", label: "Access review" }, { id: "team.", label: "Teams" }, { id: "role.", label: "Role grants" }, { id: "api.", label: "API keys" },
  ];
  const ACCESS_RX = /^(user|wall|breakglass|session|security|support|access_review|team|role|api|escrow|agent\.autonomy)/;
  let logKind = "", logQ = "";
  function logView(el) {
    const canSee = A.can("admin.audit_log", "view") || A.can("admin.users", "view");
    const rows = A.store.where("audit", (x) => ACCESS_RX.test(x.action) && (!logKind || x.action.startsWith(logKind)) && (!logQ || `${x.action} ${x.note || ""} ${A.user(x.actor).name} ${x.target || ""}`.toLowerCase().includes(logQ.toLowerCase())));
    const body = canSee ? h`<div class="row wrap" style="gap:8px;margin-bottom:10px">
        <select data-c="settings-log-kind" aria-label="Event type">${LOG_KINDS.map((k) => h`<option value="${k.id}" ${k.id === logKind ? A.raw("selected") : ""}>${k.label}</option>`)}</select>
        <input class="input" data-c="settings-log-q" value="${logQ}" placeholder="Search who, what, note" style="max-width:260px">
        <span class="grow"></span><span class="small muted">${rows.length} events · append-only; nobody edits or deletes entries</span></div>
      ${A.ui.table([
        { label: "When", render: (x) => A.dateTime(x.at) },
        { label: "Who", render: (x) => h`${A.ui.avatar(x.actor)} ${A.user(x.actor).name}` },
        { label: "Event", render: (x) => h`<code class="small">${x.action}</code>` },
        { label: "Target", render: (x) => { const u = A.store.get("users", x.target), m = A.store.get("matters", x.target); return u ? u.name : m ? m.number : x.target || ""; } },
        { label: "Detail", render: (x) => h`<span class="small">${x.note || ""}</span>` },
      ], rows.slice(0, 200), { empty: "No access events match" })}` : A.ui.empty("No access", "Your role cannot read the access log.");
    shell(el, "settings/log", "Access log", "Every access change, emergency access, session end and security setting, newest first.", body);
  }
  A.change("settings-log-kind", (el) => { logKind = el.value; A.refresh(); });
  A.change("settings-log-q", (el) => { logQ = el.value; A.refresh(); });
  A.module({
    id: "settings", label: "Settings", icon: "⚙", order: 80, cap: "admin.users",
    smoke: [...TABS.map((t) => t.id), "settings/queue", "settings/users/u-ayo", "settings/roles/paralegal", "settings/roles/agent"],
    badge: () => adminQueue().length,
    routes: { "settings": usersView, "settings/users/:id": usersView, "settings/queue": queueView, "settings/log": logView, "settings/people": peopleView, "settings/security": securityView, "settings/support": supportView, "settings/residency": residencyView, "settings/roles": rolesView, "settings/roles/:id": roleEditView, "settings/walls": wallsView, "settings/break-glass": bgView, "settings/agent": agentView, "settings/audit": auditView },
  });

  A.module({ id: "firm", label: "Firm & account", icon: "▦", order: 82, cap: "platform.firm_profile", smoke: FIRM_TABS.map((t) => t.id),
    routes: { "firm": firmView, "firm/portal": portalSettingsView, "firm/notifications": notificationsView, "firm/email": emailView, "firm/api": apiView, "firm/subscription": subscriptionView, "firm/escrow": escrowView, "firm/status": statusView } });
  A.module({ id: "emergency", label: "Emergency access", icon: "!", order: 85, cap: "admin.break_glass", visible: () => !A.can("admin.users"), smoke: ["emergency"], routes: { "emergency": bgView } });

  /* ---------- Client portal ---------- */
  let previewClient = "c-grace";
  function portalMatters() {
    if (me().role === "client") return A.matters();
    return A.matters((m) => m.clientId === previewClient && m.status === "open");
  }
  function portalView(el) {
    const client = me().role === "client";
    const clients = [...new Set(A.matters((m) => m.status === "open").map((m) => m.clientId))];
    if (!client && !clients.includes(previewClient)) previewClient = clients[0];
    const ms = portalMatters(); const m = ms[0];
    const who = client ? me().name : A.contact(previewClient).name;
    A.setContext({ kind: "portal", id: m && m.id, label: client ? "Your portal" : `Portal preview · ${who}` });
    if (!m) return A.render(el, h`<div class="page">${A.ui.empty("No open matters to show", "")}</div>`);
    const tl = A.store.where("portalTimeline", (t) => t.matterId === m.id).sort((a, b) => (a.date < b.date ? -1 : 1));
    const shares = A.store.where("portalShares", (s) => s.matterId === m.id);
    const uploads = A.store.where("docs", (d) => d.matterId === m.id && d.fromPortal);
    const msgs = A.store.where("portalMessages", (x) => x.matterId === m.id).slice().sort((a, b) => (a.at < b.at ? -1 : 1));
    const inv = A.store.where("invoices", (i) => i.matterId === m.id);
    const signs = A.store.where("portalSign", (s) => s.matterId === m.id);
    const pendingReplies = A.store.where("approvals", (g) => g.effect === "portal-send" && g.status === "pending" && g.matterId === m.id);
    const ro = client ? "" : "Preview only: the client acts here";
    const lawyer = A.user(m.responsible);
    const plain = stagePlain(m.stage);
    const log = A.store.where("portalStageLog", (x) => x.matterId === m.id).sort((a, b) => (a.at < b.at ? 1 : -1))[0];
    let stageNote = null;
    if (log && log.to !== m.stage) stageNote = { from: log.to, to: m.stage, at: A.today };
    else if (log && A.daysBetween(log.at, A.today) <= 30 && !(log.seenBy || []).includes(me().id)) stageNote = log;
    const parts = A.store.where("portalParticipants", (x) => x.matterId === m.id).sort((a, b) => (a.added < b.added ? -1 : 1));
    A.render(el, h`<div class="page">
      ${client ? "" : h`<div class="card" style="margin-bottom:14px;border-style:dashed"><div class="row wrap"><b>Client portal preview</b><span class="muted small grow">Exactly what the client sees. Buttons are disabled here.</span>
        ${clients.map((c) => h`<button class="btn small ${c === previewClient ? "primary" : ""}" data-a="portal-pick" data-id="${c}">${A.contact(c).name}</button>`)}</div></div>`}
      <div class="page-h"><h1>${client ? `Hello, ${who.split(" ")[0]}` : who}</h1><span class="sub">${m.name} · file ${m.number} · your lawyer: ${lawyer.name}</span></div>
      ${(() => { const todo = [
          ...signs.filter((x) => x.status !== "signed").map((x) => ({ t: `Sign: ${x.title}`, a: "portal-sign", id: x.id, st: "needs-you" })),
          ...(stageNote ? [{ t: "Your file moved to a new step", st: "new" }] : []),
          ...(inv.filter((i) => i.status && !/paid|draft/i.test(i.status)).map((i) => ({ t: `Invoice ${i.number || i.id}: ${A.cents(invOwing(i))} due ${A.date(i.dueDate || i.date)}`, st: A.daysBetween(A.today, i.dueDate || i.date) < 0 ? "overdue" : "waiting" }))),
        ];
        return todo.length ? h`<div class="card" style="margin-bottom:14px;padding:10px 14px"><div class="row wrap" style="gap:8px"><b class="small">${client ? "Waiting for you" : "Waiting for the client"}</b>
            ${todo.map((x) => x.a ? btn(x.t, x.a, { id: x.id }, ro, "primary") : A.ui.status(x.st || "waiting", x.t))}
            ${pendingReplies.length ? A.ui.status("agent-prepared", client ? `${lawyer.name.split(" ")[0]} is reviewing a reply to you` : "Agent reply waiting for lawyer approval") : ""}</div></div>`
          : h`<p class="small muted" style="margin:0 0 12px">${A.ui.status("done", "Nothing needed from you")} We will post here when we need something.</p>`; })()}
      <div class="grid g2">
        <div class="card"><h2>Where things stand</h2>
          <p style="margin:0 0 8px">Current step: <b>${plain[0]}</b><br><span class="muted small">${plain[1]}</span></p>
          ${stageNote ? h`<div class="card" style="margin:0 0 10px;padding:8px 10px"><div class="row"><span class="grow small"><b>Your file moved to a new step</b> on ${A.date(stageNote.at)}: ${stagePlain(stageNote.from)[0]} → ${stagePlain(stageNote.to)[0]}.</span>${btn("Got it", "portal-stage-ack", { id: stageNote.id || "" }, ro || !stageNote.id ? ro || "New step not logged yet" : "")}</div></div>` : ""}
          ${tl.length ? h`<ol style="margin:0;padding-left:18px">${tl.map((t) => h`<li style="margin-bottom:8px"><span class="muted small">${A.date(t.date)}</span><br>${t.text}</li>`)}</ol>
            <p class="muted small" style="margin:6px 0 0">✦ Written by our assistant in plain language and checked by ${A.user(tl[tl.length - 1].approvedBy).name}.</p>`
          : h`<p class="muted small">We will post updates here.</p>`}</div>
        <div class="card"><h2>Messages</h2>
          <div style="display:flex;flex-direction:column;gap:8px;max-height:280px;overflow:auto">${msgs.length ? msgs.map((x) => { const mine = x.from === (client ? me().id : "u-grace") || A.user(x.from).role === "client";
            return h`<div class="msg ${mine ? "me" : ""}">${A.ui.avatar(x.from)}<div class="grow"><div class="bubble">${x.text}</div><span class="muted small">${A.user(x.from).name} · ${A.dateTime(x.at)}</span></div></div>`; }) : h`<span class="muted small">No messages yet.</span>`}</div>
          ${pendingReplies.length ? h`<p class="muted small">${client ? `${lawyer.name.split(" ")[0]} is reviewing a reply to you.` : "A reply drafted by the agent is waiting for lawyer approval (below)."}</p>` : ""}
          ${parts.length > 1 ? h`<p class="muted small" style="margin:6px 0 0">In this conversation: ${parts.map((x) => `${x.name} (${x.rel.split(",")[0]})`).join(", ")}. ${parts.filter((x) => x.added > parts[0].added).map((x) => `${x.name} joined ${A.date(x.added)} and sees only messages from that date on.`).join(" ")}</p>` : ""}
          <div class="row" style="margin-top:8px"><input id="portal-msg" class="input" placeholder="Write to your lawyer" ${ro ? A.raw("disabled") : ""}>${btn("Send", "portal-send", { id: m.id }, ro, "primary")}</div></div>
        <div class="card"><h2>Documents</h2>
          ${shares.length || uploads.length ? h`<ul style="margin:0;padding-left:18px">${shares.map((s) => { const d = A.store.get("docs", s.docId); return d ? h`<li>${A.ui.cite({ doc: d.id, label: d.name, page: 1 })} <span class="muted small">shared ${A.date(s.at)}</span></li>` : ""; })}
            ${uploads.map((d) => h`<li>${d.name} <span class="muted small">uploaded ${A.date(d.added)}</span></li>`)}</ul>` : h`<span class="muted small">Nothing shared yet.</span>`}
          <div class="row" style="margin-top:10px">${btn("Upload securely", "portal-upload", { id: m.id }, ro)}<span class="muted small">Encrypted, stored in Canada.</span></div></div>
        <div class="card"><h2>Invoices</h2>
          ${inv.length ? A.ui.table([{ label: "Invoice", render: (i) => i.number || i.id }, { label: "Date", render: (i) => A.date(i.date) }, { label: "Total", num: true, render: (i) => A.cents(invTotal(i)) }, { label: "Owing", num: true, render: (i) => A.cents(i.status === "paid" ? 0 : invOwing(i)) }, { label: "Status", render: (i) => A.ui.status(i.status || "sent") }], inv)
            : h`<span class="muted small">No invoices yet. Fees on this file are ${m.fee}.</span>`}</div>
        <div class="card"><h2>To sign</h2>
          ${signs.length ? signs.map((s) => h`<div class="row" style="margin-bottom:6px"><span class="grow"><b>${s.title}</b><br><span class="muted small">Requested by ${A.user(s.by).name} · ${A.date(s.requested)}</span></span>
            ${s.status === "signed" ? A.ui.status("done", `Signed ${A.date(s.signed)}`) : btn("Review and sign", "portal-sign", { id: s.id }, ro, "primary")}</div>`) : h`<span class="muted small">Nothing to sign.</span>`}
          <p class="muted small" style="margin:6px 0 0">A sworn affidavit is commissioned by video with your lawyer after you sign.</p></div>
      </div>
      <p class="muted small" style="margin-top:12px">For your security, the portal signs you out after ${sec().clientIdleMinutes || 15} minutes without activity. Anything you were typing is not sent.</p>
      ${!client && pendingReplies.length ? h`<h3 style="margin:18px 0 8px;font-size:14.5px">Replies waiting for lawyer approval</h3>${pendingReplies.map((g) => A.ui.gateCard(g))}` : ""}
    </div>`);
  }
  const PLAIN = {
    "Intake complete": ["File opened", "We have opened your file and are collecting what we need to get started."],
    "Pleadings": ["Exchanging court papers", "Each side files documents with the court that set out its position."],
    "Requisitions": ["Checking the title", "We are checking the property's title and asking the seller's lawyer to fix any problems before closing."],
    "Listing the estate's assets": ["Listing what the estate owns", "We are writing to banks and the land registry to find everything the estate owns."],
    "Probate application": ["Asking the court to confirm your authority", "We are preparing the papers that ask the court to confirm you can act for the estate."],
    "Demand": ["Demand sent", "We have written to the other side setting out the claim and asking them to respond."],
    "Review": ["Reviewing documents", "We are reviewing the documents and will report to you with our advice."],
    "Discoveries": ["Exchanging evidence", "Both sides share relevant documents and answer questions under oath."],
    "Closed": ["File closed", "Your matter is complete. We keep your file as the Law Society requires."],
  };
  const stagePlain = (s) => PLAIN[s] || [s, "We will explain this step in our next update."];
  A.action("portal-stage-ack", (el) => {
    if (me().role !== "client") return;
    const x = A.store.get("portalStageLog", el.dataset.id); if (!x) return;
    A.store.put("portalStageLog", { id: x.id, seenBy: [...(x.seenBy || []), me().id] });
  });
  A.action("portal-pick", (el) => { previewClient = el.dataset.id; A.refresh(); });
  function draftReply(m, q) {
    const t = q.toLowerCase();
    if (m.id === "m4" && /(how long|when|timeline|probate|certificate)/.test(t))
      return { text: "Thank you, Grace. Once you sign the affidavit we file the application with the Superior Court in Toronto. Toronto applications are currently taking about 8 to 12 weeks. We will post an update as soon as the certificate is issued.", cites: [{ doc: "d-will", page: 1, line: 3, quote: "I appoint my niece GRACE MENDEL as Estate Trustee." }] };
    if (m.id === "m4" && /(house|willow|erik|residence|sell)/.test(t))
      return { text: "The will leaves the house at 41 Willow Cres to Erik. It should not be sold or listed; we will arrange the transfer to him once the certificate is issued.", cites: [{ doc: "d-will", page: 1, line: 4, quote: "I give my residence at 41 Willow Cres to my nephew ERIK LINDQVIST." }] };
    return { text: `Thank you for your message. ${A.user(m.responsible).name.split(" ")[0]} has it and will reply within one business day. If something is urgent, call the office at 416-555-0100.`, cites: [] };
  }
  A.action("portal-send", (el) => {
    if (me().role !== "client") return;
    const m = A.matter(el.dataset.id); const inp = document.getElementById("portal-msg"); const text = inp ? inp.value.trim() : "";
    if (!m || !text) return A.toast("Write a message first", "bad");
    const msg = A.store.put("portalMessages", { id: A.id("pm"), matterId: m.id, from: me().id, at: A.now(), text });
    A.audit("portal.message_received", { target: m.id });
    A.agent.run({ title: `Draft reply to ${me().name}`, matterId: m.id, steps: ["Reading the message", "Checking the file and documents", "Drafting a plain-language reply"], done: () => {
      const r = draftReply(m, text);
      return A.gate({ kind: "portal-reply", cap: "documents.release", title: `Reply to ${A.contact(m.clientId).name} in the portal`, summary: `Client asked: “${text.slice(0, 140)}”`, matterId: m.id, preparedBy: "agent",
        body: String(h`<p>${r.text}</p>`), cites: r.cites, effect: "portal-send", payload: { matterId: m.id, text: r.text, replyTo: msg.id, lawyer: m.responsible },
        checks: [{ label: "No legal advice beyond the file", ok: true }, { label: "Cites the file where it states facts", ok: true }, { label: "Client identity verified", ok: true }] });
    } });
    A.toast("Sent. Your lawyer will reply here.", "ok");
  });
  A.action("portal-upload", (el) => {
    if (me().role !== "client") return;
    const m = A.matter(el.dataset.id); if (!m) return;
    A.modal("Upload a document", h`<label class="field">File name<input name="name" class="input" placeholder="e.g. Funeral home invoice.pdf"></label><label class="field" style="margin-top:10px">Note for your lawyer<input name="note" class="input"></label>`,
      [{ label: "Cancel" }, { label: "Upload", primary: true, run: (d) => {
        const v = A.formValues(d); if (!v.name.trim()) { A.toast("Name the file", "bad"); return false; }
        A.store.put("docs", { id: A.id("doc"), matterId: m.id, name: v.name.trim(), kind: "client upload", added: A.today, by: me().id, fromPortal: true, pages: [[`Uploaded by ${me().name} through the client portal.`, v.note || ""]] });
        A.audit("portal.upload", { target: m.id, note: v.name.trim() }); A.toast("Uploaded. Your lawyer has been notified.", "ok");
      } }]);
  });
  A.action("portal-sign", (el) => {
    if (me().role !== "client") return;
    const s = A.store.get("portalSign", el.dataset.id); if (!s || s.status === "signed") return;
    A.modal(`Sign: ${s.title}`, h`<p>You confirm you are the estate trustee named in the will and that the information in the affidavit is true.</p><label class="field">Type your full name to sign<input name="sig" class="input"></label>`,
      [{ label: "Cancel" }, { label: "Sign", primary: true, run: (d) => {
        const v = A.formValues(d); if (v.sig.trim().toLowerCase() !== me().name.toLowerCase()) { A.toast("Type your name exactly as shown", "bad"); return false; }
        A.store.put("portalSign", { id: s.id, status: "signed", signed: A.today }); A.audit("portal.esign", { target: s.matterId, note: s.title }); A.toast("Signed. Your lawyer will book the video commissioning.", "ok");
      } }]);
  });

  A.module({ id: "portal", label: "Portal", icon: "◐", order: 5, cap: "platform.portal", visible: () => A.me().role === "client", smoke: ["portal"], routes: { "portal": portalView } });

  /* ---------- agent intents ---------- */
  A.suggest("settings", () => ["Who can see this matter", "Why can't I approve this", "Give me access to 2025-0311"]);
  A.suggest("portal", () => (me().role === "client" ? ["How long will probate take?"] : ["Who can see this matter"]));
  function findMatter(text, ctx) {
    const all = A.store.all("matters"); const t = text.toLowerCase();
    return all.find((m) => t.includes(m.number.toLowerCase()) || t.includes(m.id + " ") || t.endsWith(m.id) || m.name.toLowerCase().split(/\W+/).some((w) => w.length > 4 && t.includes(w)))
      || (ctx && ctx.id && A.store.get("matters", ctx.id)) || null;
  }
  A.intent({ match: /who can (see|access|open) (this|the)? ?matter/i, label: "Who can see this matter", example: "Who can see this matter", run(text, ctx) {
    const m = findMatter(text, ctx); if (!m) return { text: "Open a matter first, or name it (e.g. 2026-0142)." };
    if (!A.canMatter(m) && me().role !== "admin") return { text: "You cannot see that matter, so I cannot list who can." };
    const see = users().filter((u) => userSees(u, m));
    const walled = (m.wall || []).map((id) => A.user(id).name);
    return { text: `${m.number} ${m.name}: ${see.length} people can open it — ${see.map((u) => `${u.name} (${A.roleName(u.role)}${(m.team || []).includes(u.id) ? ", team" : A.store.where("breakglass", (b) => b.userId === u.id && b.matterId === m.id && b.active).length ? ", break-glass" : ""})`).join("; ")}.${m.restricted ? " It is restricted to its team." : ""}${walled.length ? ` Screened: ${walled.join(", ")}.` : ""} The system administrator sees the name and number only.`,
      actions: A.can("admin.users") ? [{ label: "Open walls", go: "settings/walls" }] : [] };
  } });
  A.intent({ match: /why can'?t i approve/i, label: "Why can't I approve this", example: "Why can't I approve this", run() {
    const g = A.store.where("approvals", (x) => x.status === "pending")[0];
    if (!g) return { text: "Nothing is waiting for approval right now." };
    const c = A.canApprove(g);
    if (c.ok) return { text: `You can approve “${g.title}”.`, actions: [{ label: "Approve now", run: () => A.approve(g.id) }] };
    const rule = /prepared/.test(c.why) ? A.sod[0] : /signator/.test(c.why) ? A.sod[4] : /not on this matter/.test(c.why) ? A.sod[9] : A.sod[2];
    return { text: `“${g.title}”: ${c.why}. Rule: ${rule} It needs someone with “${A.capName(g.cap)}” at approve level${g.preparedBy !== "agent" ? ` other than ${A.user(g.preparedBy).name}` : ""}.` };
  } });
  A.intent({ match: /give me access to (.+)/i, label: "Request emergency access to a matter", example: "Give me access to 2025-0311", run(text, ctx) {
    const m = findMatter(text.replace(/give me access to/i, ""), null);
    if (!m) return { text: "I could not find that matter. Give me its number, e.g. 2025-0311." };
    if (A.canMatter(m)) return { text: `You already have access to ${m.number}.`, actions: [{ label: "Open it", go: `matters/${m.id}` }] };
    const why = bgBlock(m);
    if (why) return { text: `I cannot give access. ${why}. Normal access is added by the responsible lawyer (${A.user(m.responsible).name}) changing the team, approved under “${A.capName("matters.team")}”.` };
    return { text: `I cannot grant access myself. For an emergency you can use break-glass: ${m.number} opens to you for 24 hours at once, the use is logged, and another owner or lawyer reviews it. Otherwise ask ${A.user(m.responsible).name} to add you to the team.`,
      actions: [{ label: "Request break-glass", run: () => openBreakGlass(m.id) }] };
  } });
})();
