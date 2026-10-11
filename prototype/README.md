# Counsel prototype — module guide

A functional front-end of an AI-native legal practice app for Linda K Law (Toronto). No backend: all data lives in
`App.store` (memory + localStorage). Read `core.js` fully before writing; `index.html` has every CSS class you may use.

## Product rules (what "AI-native, better than Clio" means here)
- The agent does the work first; people review. Lists open on "what the agent prepared for you", not empty forms.
- Every agent output that reaches a client, court, bank, trust ledger or the firm's books is an **approval** (`App.gate`),
  approved by someone with the right capability who did not prepare it. The agent never approves.
- Every agent claim cites sources: `{doc, page, line}` into `App.store.all("docs")` (page and line are 1-based).
- Respect access: `App.can(cap, "view"|"act"|"approve")`, `App.canMatter(m, cap)`, `App.matters(filter, cap)`.
  Hide what a role cannot see; disable what it cannot do (show why in `title`). Capability ids are in `access-data.js`.
- Fast and quiet like Slack: no spinners blocking the page; agent work shows in the right panel via `App.agent.run`.
- Fictional data only. Firm "Linda K Law"; users and matters m1..m8 are in `seed.js`. Today is `App.today` (2026-10-09).
- Own words and own design. Never mention Clio in the UI.

## Core API (window.App)
- `h\`...\`` escaped template → Raw; `raw(str)`; `render(el, raw)`; `esc`, `money(n)`, `date(iso)`, `addDays`, `daysBetween`, `id(prefix)`, `now()`.
- Store: `store.all(c)`, `get(c,id)`, `where(c,fn)`, `put(c,obj)` (insert or merge; triggers re-render), `remove(c,id)`.
  `seed(collection, rows)` once per collection at module load (kept across reloads; "Reset demo" restores).
- People: `me()`, `user(id)`, `roleName(id)`, `contact(id)`, `matter(id)` (null if no access), `matters(filter, cap)`.
- Approvals: `gate({kind, cap, title, summary, matterId, preparedBy:"agent"|userId, body, cites, checks:[{label, ok, blocking}], effect, payload})`;
  `effect(name, fn(payload, gate))` runs on approve; `pendingFor()`; `ui.gateCard(g, {compact, approveAction, backAction})` renders a full
  approval with buttons (the two action names replace `gate-approve` / `gate-back` when a module adds its own rules).
- Step-up: `approve` asks for a fresh sign-in first when `needsStepUp(g)` (trust/billing money, `matters.close`, access gates), through the
  prompt registered with `onStepUp(fn(gate, done))` (home.js); approvals made while the prompt is open wait on it; `cancelStepUp()` drops them.
- Agent: `agent.run({title, matterId, steps:[...], done(){ ...create records or App.gate(...) }})`;
  `intent({match:/regex/i, label, example, run(text, ctx) → {text, cites, actions:[{label, go:"path"} | {label, run(){}}]}})`;
  `suggest(kind, ctx => [prompts])`; `setContext({kind, id, label})` in your render so the panel knows what is open.
- Routing: `module({id, label, icon, cap, order, home, nav, visible() (optional nav filter), badge(), smoke:[paths], routes:{"x": (el, params)=>{}, "x/:id": ...}})`;
  `go(path)`; `matterTab({id, label, cap, order, render(el, matter)})` adds a tab to the matter workspace.
- Clicks: put `data-a="name"` (+ `data-id`, other data-*) on elements and register `action("name", (el, ev) => {})`.
  Prefix action names with your module id (e.g. `trust-authorize`) to avoid clashes.
- Change events: `data-c="name"` on inputs/selects + `change("name", (el, ev) => {})`.
- UI: `ui.table(cols, rows, {onRow, empty})`, `ui.tabs(items, active, action)`, `ui.stat`, `ui.badge(text, tone)`,
  `ui.avatar(uid)`, `ui.empty`, `ui.cite(c)`, `ui.runCard(r)`, `modal(title, body, buttons)`, `formValues(root)` (checkbox → boolean, radio group → checked value), `toast(msg, tone)`.
- Audit: `audit(action, {target, note})` for anything a regulator would ask about.
- Grants: `setGrantOverrides({roleId: {capId: level}})` layers approved role edits over `access-data.js` for every `grant`/`can` lookup;
  Reset demo clears them. `onReady(fn)` runs fn once the store has loaded and again after Reset demo (rebuild derived state there).

## Rules for your file
- Write ONLY your module file(s) in `modules/`. Do not edit core.js, seed.js, index.html or other modules.
  If you need a core change, describe it in your final report instead.
- No external libraries, no network, no `alert/confirm/prompt`, no `<form action>`. Plain ES2020, one IIFE.
- Store re-renders the current route on every `put`; keep render functions pure and cheap; keep UI state
  (current tab, filters) in module-level variables.
- Add `smoke: [...]` paths covering every route with real ids.
- Test: `node /private/tmp/claude-501/-Users-dagmfekaduyenealem-Documents-ai-paploo/7553baff-7fef-42ae-8576-9c02d79b26a9/scratchpad/apptest/smoke.js <counsel-app dir> <your module id>`
  must end with `SMOKE OK` (it visits every path as every user and clicks every action). Also `node --check` your file.
- Quality: real legal content (Ontario), terse UI copy, no filler, no emoji in UI except the ✦ agent mark.

## Daily-use design system (App.ui.*, all additive; the API above is unchanged)
Every list page uses the same shape: `ui.header` → `ui.filters` → list or `ui.split(list, detail)` → `ui.bulkBar`.
- `ui.header({title, status, sub, back:{label,to}, meta, actions:[btn opts|Raw], primary:{label, a|to, id, key:"n"}})`: title + status chip left, primary action right. `back` binds `u`.
- `ui.status(word, label?, tone?)`: the one chip vocabulary (`App.STATUS`): needs-approval/pending/waiting (amber, a person must act), running/agent-prepared/suggested (teal, agent), draft/new/open, in-progress, approved/done/paid/reconciled/confirmed, returned/unconfirmed/due-soon, overdue/blocked/failed/conflict/unbalanced/expired/stale/escalated, in-review, superseded/reassigned, high-/medium-/low-risk, closed/archived. Unknown words render neutral. Selected cards: `.card.on` (same look as `.srow.on`).
- `ui.actor("agent"|"person", text?)`: "✦ Agent prepared" vs "● Needs a person". Show both on anything the agent touched.
- `ui.btn({label, a | to, id, key, primary, agent, danger, small, disabled, why, data:{}})`: `key` binds a shortcut (pressed when no row is focused, or inside the focused row) and shows it in the tooltip and as a small kbd.
- `ui.filters({search:{c, value, placeholder}, chips:[{id,label,count}], active, action, selects:[{c, value, options:[[v,label]]}], right})`. Search fires your `App.change(c)` on Enter/blur; `/` focuses it.
- `ui.split(listRaw, detailRaw|null, {emptyTitle, emptyHint, wide})` and `ui.list(rows:[{id,title,sub,right,status,statusLabel,statusTone,on}], action, {select, empty})` for the left pane. Stacks under 1100px.
- `ui.table(cols, rows, {onRow, rowId, empty, emptyHint, select: "name", active: id})`: `select` adds checkboxes bound to `App.selection(name)`; `active` highlights the open row.
- `App.selection(name)` → `{has, toggle, set, clear, ids(), size}` (memory, survives re-render). `ui.check(name, id)`; built-in actions `sel-toggle`, `sel-all` (data-ids), `sel-clear`.
- `ui.bulkBar(name, [{label, a, primary, danger, disabled, why}])`: shows when the selection is not empty; each button gets `data-ids="id1,id2"`.
- `ui.inline({name, id, value, display, type:"text"|"number"|"date"|"select"|"money", options, disabled, why})` + `App.inlineEdit(name, (id, value) => {...})` (return false to keep editing). Enter/blur saves, Esc cancels, `e` edits on the focused row. Use only for safe fields (never amounts that reach trust or a bill after approval).
- `ui.due(iso, {done, soon})`: "Today", "Tomorrow", "In 3 days", "2 days overdue". `ui.empty(title, hint, action?)`, `ui.kbd(key)`, `ui.hints([[key,label]])`.
- Money and time: `App.cents(c)` formats integer cents as CAD; `App.money(n)` dollars; `App.date` reads dates as Toronto calendar days; `App.now()` uses Toronto wall-clock time (`App.tz`); `App.dateTime(s)` formats a stored "YYYY-MM-DD HH:MM".

### Keyboard layer (core; nothing to wire beyond `data-key`)
j/k (or ↓/↑ once a row is focused) move over `.kb-item`, clickable table rows, `.kcard` and approval cards; Enter opens; `a` approves the focused approval (never page-wide); `b` sends it back; `e` edits; `x` selects for bulk; `n` presses the page's `[data-key="n"]`, else opens the New menu; `c` New menu; `/` page filter (else palette); `u` back (`App.back()`); `g` then 1–9 nav; `.` agent panel; `?` shortcut sheet (lists page keys from `[data-key]`, label from `data-key-label` or `title`); Esc closes, blurs, unfocuses, then clears selection. Keys are ignored while typing.
- `App.quickCreate({id, label, cap, level, go, run})`: New menu item; with `go` it opens the page and presses its `[data-key="n"]`. Defaults: lead, time entry, task, deadline, contact, draft bill, trust request (replace by id).
- `App.shortcut(key, label, fn)` global key; `App.command({label, hint, keys, cap, run})` palette command.
- Palette (⌘K): recent pages, approvals you can decide, sections (with `g n`), matters by number/name/client/area, contacts, create items, agent intents, view commands (density, theme, agent panel, shortcuts) and "view as" users. Multi-word match, ↑↓/j k, Enter.
- `App.density("compact"|"comfortable")`, `App.theme("light"|"dark"|"auto")`, remembered per browser. `ui.palette()`, `ui.quickMenu()`, `ui.helpSheet()` open the overlays.
- `App.ui.keys(routePrefix, {key: {label, fn(e, route)}})`: page keys for a route (prefix string, RegExp or fn). They run before the global layer (return false to pass the key on) and labelled ones show in the `?` sheet. Do not add your own `keydown` listeners; the global layer ignores events that are already `defaultPrevented`.
- `App.live(name, fn(value, el))` with `data-live="name"` and an `id` on the input: live search (re-renders, keeps the caret). `App.input(name, fn(el, e))` with `data-i` for raw `input` events.
- `App.onUser(fn(userId))`: runs after "view as" switches user; core clears `App.selection`s, modules clear their own cursors and picks.
- Approvals: `gate({..., assignee, risk, due})`; with `assignee` only that person may approve. Gate audit rows carry `matterId`.
- `App.agent.paused(matterId)`: why runs are held. `agent.run` returns null and toasts while `agentPauses` "firm" or the matter's pause is active.
