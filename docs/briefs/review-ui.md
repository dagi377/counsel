<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

# Counsel web UI review

Reviewer: Fable (product design). Target: `web/` at `origin/main` 4c45ac3b. Reviewed at 1280 and 1600 wide, light and dark colour scheme, Chrome via Playwright. Screenshots: `counsel-ui-shots/` (47 files; `NN-<view>-<width>-<scheme>.png`).

Bar: Clio / Linear / Stripe / Google Cloud console for legal and financial software.

## Verdict: NOT YET

`web/` is a routing and data-contract scaffold, not a product UI. There is no stylesheet, no design token, no layout, no component library and no legal screen (matters, trust, billing, deadlines, conflicts, drafting are all unchecked in `docs/TASKS.md` T-045L.1 to .13). What exists renders in browser-default Times New Roman on white at every width (DOM check: `document.styleSheets.length === 0` on every page). Dark mode is not supported (page stays white under `prefers-color-scheme: dark`). The one gate flow (role change) and the one money primitive (`formatCents`) are correct in logic but have no product surface.

The `prototype/` directory holds a real design system (tokens for light and dark, status-chip vocabulary, `ui.header`/`ui.filters`/`ui.split`, gate cards with preparer/approver, step-up). None of it has reached `web/`.

What is good and should be kept: feature auto-discovery (`app/features.ts`), literal-over-parameter routing (`kernel/shell/nav.ts`), integer-cents money with a float ban (`kernel/money.ts`, `eslint.config.js`), the gate-only role write (`AccessAdmin.tsx:28-36`), portal double-filtering of foreign matters (`PortalMatters.tsx:31-43`), semantic landmarks and `aria-current`, labelled checkboxes, `role="status"`/`role="alert"` on async states.

## Findings

Ranking: blocker (ships wrong or unusable), major (below bar, visible to every user), minor (polish).

### Blockers

B1. The app never routes. `web/src/main.tsx:5-17` renders a bare `<nav>` of feature links and ignores `window.location`; `Shell` (which does route) is imported nowhere. `/portal/matters` in the real bundle shows the same one-link list as `/` (shots `01-*` vs `02-*` are identical). Fix: `main.tsx` renders `<Shell appName="Counsel" features={features} pathname={location.pathname}/>`, with a `popstate`/link-click listener so navigation does not reload.

B2. No stylesheet, tokens, font or layout exist. `web/index.html:1-12` links nothing; no `.css` file under `web/src`. Every page is 16px Times, full-width, unpadded, ~14 px gutter from UA defaults; tables have no row height, zebra, alignment or numeric columns. Fix: port `prototype/index.html:8-17` tokens (`--bg --panel --ink --muted --line --accent --gate --ok --bad --warn`, Geist / Geist Mono) into `web/src/kernel/theme/tokens.css` + `base.css`, with the dark block under `@media (prefers-color-scheme: dark) :root:not([data-theme="light"])` and `:root[data-theme="dark"]`, and `<meta name="color-scheme" content="light dark">` in `index.html`.

B3. Gate pending state is invisible after submit. `AccessAdmin.tsx:148` writes `Submitted for approval (gate 01J9Z3K8Q2R4T6V8X0Y2Z4A6B8).` into a `<p>` and leaves the checkboxes editable and diverged from the "Roles" column (shot `24-*`). Nothing says who must approve, that the current roles still apply, or how to withdraw; the raw ULID is the only handle. A second click re-sends with the same idempotency key (`AccessAdmin.tsx:141`) and reports success again. Fix: on `ok`, replace the form with a pending chip ("Pending approval · requested roles: lawyer, paralegal · approver: a firm admin other than you"), a link to `/gates/{id}`, and a "Withdraw" action (`DELETE`-style `POST /gates/{id}/withdraw` exists in `api/openapi.yaml:1111`); lock the checkboxes until the gate resolves.

B4. Approver side does not exist. `api/openapi.yaml:1023-1111` exposes `/gates`, `/gates/{id}`, `/gates/{id}/decisions`, `/gates/{id}/withdraw`; no screen lists pending gates, shows preparer vs approver, or lets an approver decide. For a product whose core promise is "agent drafts, lawyer approves" this is the first screen that must exist. Fix: `web/src/kernel/gates/` inbox + detail (preparer, what changes, checks, approve/return, step-up), reusing the `prototype/core.js` `gateCard` structure.

B5. Trust and money have no UI surface, and the one formatter cannot mark an overdraft. `kernel/money.ts:16-31` returns `-$452.30` for a negative trust balance (shot `40-*`): a leading minus in default black text, in a left-aligned proportional font. BL9 s.18(3) says a client ledger must never be overdrawn (`spec/legal/trust.json`); an overdraft must be the loudest thing on the page. Fix: keep `formatCents` as the single formatter; add `kernel/money/Money.tsx` that right-aligns, uses `font-variant-numeric: tabular-nums`, requires `code` for trust amounts, and renders negatives as `($452.30)` or `-$452.30` in `--bad` with a visible "Overdrawn" chip and `aria-label="overdrawn by $452.30"`. Never collapse or truncate it.

B6. Generated API client is stale; the repo's own test fails on main. `web/src/app/api/client.test.ts` "matches api/openapi.yaml" fails: `ProvinceCode`, `PlaceOfSupplyPatch`, `FirmTaxRegistrations` are missing from `client.ts`. The web layer cannot be trusted to match the contract. Fix: `node tools/gen_client.mjs`, commit, and gate CI on `npm test`.

### Major

M1. `npm ci` cannot run: no `web/package-lock.json`. Builds are not reproducible. Fix: commit the lockfile.

M2. The float ban never executes. `eslint.config.js` bans float literals, `/`, `toFixed`, `Intl.NumberFormat`, but `eslint` is not in `package.json` devDependencies and there is no `lint` script. Fix: add `eslint@9.x` and `"lint": "eslint src"`, run it in `make check`.

M3. Shell has no product chrome. `Shell.tsx:22-24` renders the app name as a `<span>`; there is no sidebar, no signed-in user, no firm name, no search, no gate badge. Nav order is alphabetical by id (`features.ts:32`), not product order. Fix: add `order` to `Feature`, a left rail (`--rail` from the prototype) with icon + label, a top bar with user menu and pending-gate count.

M4. A registered feature with no component renders a title only (`Shell.tsx:45-47`, shot `12-*`), with no "coming soon" or "no access" message. Fix: a `NotAvailable` state ("This area is not part of this release" vs "You do not have access"), using the access matrix.

M5. Not-found pages are text-only (`Shell.tsx:38-41`, `PortalShell.tsx:122-125`). No way back, no address shown. Fix: empty-state component with the attempted path and a "Go to home" link.

M6. Portal signed-out copy is wrong for first-time guests. `PortalShell.tsx:96-103` says "Your session has ended" to a guest who never signed in, and sets `aria-busy="true"` on `<main>` while showing static text. Fix: "Sign in to see your matters" when there was no session; keep "Your session has ended" only after an expiry. Drop `aria-busy` or render a real redirecting state.

M7. Portal API and page share one URL. `PortalMatters.tsx:46-47` fetches `/portal/matters` (same path the page is served on), relying on `Accept` to disambiguate. In dev the fetch returns `index.html`, `res.ok` is true, and the JSON parse throws into the generic error (shot `06-*`, `10-*`). Fix: fetch `/api/portal/matters` (or whatever the server mounts), and treat a non-JSON content type as an error with a distinct message.

M8. Portal list is unstructured. `PortalMatters.tsx:82-97` renders `<h2>Matter 2026-0041</h2>` + `<li>Bill Invoice 1042 ... 2026-10-05</li>`: kind, title and date are three unseparated spans (shot `30-*`). Matter shows its id, not its name. Fix: a table per matter (Kind chip, Title as link to the document or bill, Shared date, Action: View / Pay), matter header with short name and id.

M9. Dates are UTC and raw. `PortalMatters.tsx:92` shows `SharedAt.slice(0,10)` (UTC day, can be a day off for an Ontario client at 8 pm). `AccessAdmin.tsx:72-81` formats expiry correctly in `America/Toronto`; the portal does not. Fix: one `kernel/time.ts` formatter used by both, Toronto zone, `en-CA` locale, "5 Oct 2026" style.

M10. Error states have no retry. `PortalMatters.tsx:119` "Your matters could not be loaded. Try again later." with no button; `AccessAdmin.tsx:64` "The role change could not be submitted." with no reason (409 on stale `If-Match`, 403, network are all collapsed). Fix: retry action on load errors; map `Problem.code` (`client.ts:26-39`) to copy, show "Someone changed this user; reload" for 412/409.

M11. Break-glass grants are a string soup. `AccessAdmin.tsx:123-131` joins name, capability id, reason and expiry in one `<li>` (shot `20-*`); expired grants sit in the same list as active ones under the heading "Break-glass grants" with only the word "Expired" to tell them apart; an unknown user shows its raw id (`u-unknown`). Fix: table (Person, Capability label, Reason, Expires, Status chip Active / Expired), expired collapsed under "Recently expired"; human-readable capability labels.

M12. Role and capability identifiers are shown raw. `AccessAdmin.tsx:154,166` show `firm_admin`, `trust.ledger.view`. Fix: a `roles` prop of `{id,label}` and a capability label map from the access matrix.

M13. Dark mode absent. Browser `colorScheme: dark` renders the same white page (shots `*-dark.png`). Fix: part of B2.

### Minor

m1. `index.html` has no favicon (404 on every page load), no `theme-color`, `lang="en"` rather than `en-CA`.

m2. Loading state is a sentence (`PortalMatters.tsx:118` "Loading your matters...") with ASCII dots. Fix: skeleton rows in the table and a single ellipsis character; keep `role="status"`.

m3. `AccessAdmin.tsx:169` only disables the button while busy; checkboxes stay live and the label does not change. Fix: `aria-busy` on the form, "Submitting..." label, disable inputs.

m4. The `<p role="status">` per row (`AccessAdmin.tsx:172`) is always in the DOM; three empty live regions on load. Fix: render it only with content.

m5. `Shell.tsx:36-49` has no `<h1>` when a feature screen omits one; the only page title is the `<title>` "Counsel". Fix: screens set `document.title` ("Access · Counsel").

m6. Relative time "(1 h 34 min left)" (`AccessAdmin.tsx:85`) never updates while the page is open. Fix: tick every minute or drop the relative part.

m7. `money.ts:30` renders the code after the amount with a space (`$12,500.00 CAD`). Fine, but make it the only form and use it on every trust amount; billing may omit it.

m8. `PortalShell` nav shows every registered page, including ones the guest has no items for (shot `30-*`); the shell has no firm name, no sign-out, no "Signed in as".

## Checks that pass

- Keyboard focus is visible (UA ring) on links, checkboxes and buttons (shot `25-*`); tab order follows the DOM.
- All `<input>` elements have labels (`unlabeled === 0` on every page); forms carry `aria-label` with the user's name.
- Default text and link colours meet contrast (black and `#0000EE` on white).
- `formatCents` is cents-exact: `5 -> $0.05`, `0 -> $0.00`, `123456789012 -> $1,234,567,890.12`, bigint accepted, non-integers throw rather than round.
- Unknown portal item kinds (`memo`) never render (`PortalMatters.tsx:36`).
- `typecheck` passes; 90 of 91 tests pass (B6 is the one failure).

## Fix packages (disjoint files)

P1. Design system foundation. `web/index.html`, new `web/src/kernel/theme/tokens.css`, `web/src/kernel/theme/base.css`, new `web/src/kernel/ui/` (Button, Chip/Status, Table, EmptyState, PageHeader). Port prototype tokens light + dark, Geist, 8px grid, 13/14px body for density, right-aligned tabular numerics. Closes B2, M13, m1.

P2. App boot, routing and shell chrome. `web/src/main.tsx`, `web/src/app/features.ts` (add `order`), `web/src/kernel/shell/Shell.tsx`, `web/src/kernel/shell/nav.ts`, their tests. Mount Shell, client-side navigation, left rail, top bar with user and pending-gate badge, not-found and not-available states, `document.title`. Closes B1, M3, M4, M5, m5.

P3. Gates: approver inbox and preparer pending state. New `web/src/kernel/gates/GatesInbox.tsx`, `GateDetail.tsx`, `gates.test.tsx`; `web/src/kernel/access/AccessAdmin.tsx` + test. Pending chip, lock inputs, withdraw, link to gate; inbox with preparer / approver / checks / decide, step-up prompt hook, `Problem.code` to copy. Closes B3, B4, M10 (access half), M11, M12, m3, m4, m6.

P4. Portal. `web/src/kernel/portal/PortalShell.tsx`, `web/src/features/portal/PortalMatters.tsx`, their tests, new `web/src/kernel/time.ts`. Separate API path, Toronto dates, table per matter, kind chips, skeleton loading, retry, first-visit vs expired copy, firm name and sign-out. Closes M6, M7, M8, M9, M10 (portal half), m2, m8.

P5. Money and tooling. New `web/src/kernel/money/Money.tsx` (+ test), `web/package.json`, `web/package-lock.json`, regenerated `web/src/app/api/client.ts`, `Makefile` web target. Overdraft tone + chip + aria text, tabular numerics, `code` required for trust; add eslint devDependency and `lint` script; commit lockfile; regenerate client and make `npm test` + `npm run lint` part of `make check`. Closes B5, B6, M1, M2, m7.

P1 lands first; P2 to P5 can run in parallel on top of it. After these five the UI is a styled scaffold with one real gate flow; it is still NOT YET until the legal screens (T-045L.1 to .12, trust ledger with overdraft, billing, deadlines, conflicts, drafting review) exist and go through this same review.

## Screenshot index

| Shot | What it shows |
|---|---|
| `01-root-feature-list-*` | `/` in the real bundle: one link, no shell |
| `02..06-portal-matters-*` | `/portal/matters` in the real bundle with ready / loading / error / empty / no-mock API: all identical to `01` (B1) |
| `10-shell-portal-*` | `Shell` mounted via harness; portal error state inside it |
| `11-shell-404-*`, `12-shell-noscreen-*` | not-found and no-component states |
| `20..25-access-admin-*` | access admin: default, no grants, no-change error, busy, submitted + failed, keyboard focus |
| `30..33-portal-shell-*` | portal shell page, not-found, signed-out, empty |
| `40-money-formatter-*` | `formatCents` on trust balance, overdraft, invoice, zero, five cents, large |
| `*-1600-light`, `*-1280-dark` | same views at 1600 wide and under dark colour scheme |

Harness used to reach unmounted components: `web/review.html` + `web/src/review-main.tsx` in the review worktree only (removed with it).
