# Counsel UI kit (`web/src/ui`)

The design system every screen under `web/src/features` and `web/src/kernel` is built on.
Import from `../../ui` (the `index.ts` barrel) and nothing deeper. Importing it loads the
stylesheet. The living reference is the style guide: `npm --prefix web run dev`, then open
`/styleguide.html`.

## Rules

1. **Money is integer cents.** Render every amount with `<Money cents={...}>`. Never do
   arithmetic on money in the browser beyond adding cents; never format with `toFixed` or
   `Intl.NumberFormat` (the lint bans them). Trust amounts need `context="trust"` and a
   `code`; a negative trust balance then renders signed, red, labelled "Overdrawn" and is
   announced as `overdrawn by $X`. Never hide, collapse or truncate it.
2. **Dates are America/Toronto.** Render with `<DateTime value={...}>` or `formatDate()`.
   A `YYYY-MM-DD` string is a calendar date and is never shifted; an RFC 3339 instant is
   converted to Toronto wall time. Never call `toLocaleDateString` without the zone.
3. **Gates show both sides.** `<GateStatus>` shows the preparer and the approver (or who
   is awaited). It throws if the approver is an agent or the same person as the preparer.
   An agent preparer is marked "Agent prepared" with who it acted for.
4. **Status is never colour alone.** Use `<StatusPill status="...">` with a key from
   `STATUS`. Each tone has a glyph and a label. Add a key to the vocabulary before using
   it; do not invent a one-off badge.
5. **Numbers are tabular and right-aligned.** Mark money and count columns `num: true`.
   Identifiers (matter numbers, cheque numbers, ULIDs) use `mono`.
6. **Every async region has every state.** Lists use `DataTable` with `state`
   `loading | error | empty | ready` and give `empty` and `error` copy with an action
   (Retry, New ...). Pages that fail use `EmptyState kind="error" | "forbidden" |
   "not-found"`. A load error always has a retry.
7. **One `h1` per page**, from `PageHeader`, which also sets `document.title`.
   `SectionCard` headings are `h2` on a page, `h3` inside another section.
8. **Tokens only.** No hex colours, pixel sizes or font names in screens. Use the
   `--c-*`, `--s-*`, `--fs-*`, `--r-*` variables from `theme/tokens.css`. Edit tokens in
   `theme/tokens.ts`, then `node --experimental-strip-types web/tools/gen_tokens.mjs`;
   the test fails if the CSS drifts, or if any listed pair falls below AA.
9. **Dark mode is automatic.** Tokens switch on `prefers-color-scheme` unless
   `data-theme` is set on `<html>`; `applyTheme()` sets it. Never branch on theme in a
   component.
10. **Desktop first.** Check at 1280 and 1600. Nothing here is tuned for phones.

## Colour roles

| Role | Means |
|---|---|
| `accent` (teal) | the agent's work: drafts, suggestions, "ask the agent" |
| `gate` (amber) | waiting on a person: approvals, needs-you, pending counts |
| `ink` | the firm's records |
| `ok` | resolved, paid, reconciled, approved |
| `warn` | attention soon: due soon, partly paid, sent back |
| `bad` | money or law gone wrong: overdrawn, rejected, conflict, overdue |

## API summary

```tsx
// Controls
<Button variant="default|primary|danger|agent|ghost" size="md|sm" busy solid />
<Field label hint error required>{(p) => <Input {...p} mono num />}</Field>
<Field label>{(p) => <Select {...p} options={[{value,label,disabled?}]} placeholder />}</Field>
<Textarea />

// Data
<DataTable<T> caption columns={[{id, header, cell, num?, sortable?, width?}]} rows rowId
  state="ready|loading|error|empty" sort|defaultSort onSortChange
  rowMenu={(row) => MenuItem[]} rowMenuLabel onRowClick selectedId
  empty={{title, hint, action}} error={{title, hint, detail, action}} footer />
<Menu label items={[{id, label, onSelect, danger?, disabled?}]} />
<FactsGrid facts={[{label, value, num?, mono?}]} inline />

// Structure
<PageHeader title status meta sub actions crumbs appName />
<SectionCard title description actions footer flush tone headingLevel id />
<Tabs label value onChange tabs={[{id, label, count?, countTone?, disabled?}]} idPrefix />
<TabPanel idPrefix tabId />

// Feedback
<StatusPill status={Status} label? />
<Banner tone="info|gate|ok|warn|bad" title actions />
<EmptyState kind="empty|error|forbidden|not-found" title hint detail action />
<Dialog open onClose title description footer wide dismissible />
<ToastProvider /> + useToast().show({text, tone, action, durationMs})
<Skeleton width height lines /> inside <Loading label>

// Legal and finance primitives
<Money cents context="trust" code="CAD" size="md|lg" />
<DateTime value style="date|datetime|time|month" empty />
formatDate(value, style); isoDate(value); ZONE
<GateStatus state="pending|approved|rejected|withdrawn" preparer actingFor approver decidedAt compact />
<MatterChip number name href closed />

// Templates
<ListPage header filters toolbarEnd banner>{table}</ListPage>
<DetailPage header banner tabs aside>{main}</DetailPage>
<SettingsPage header sections current>{SectionCards with ids}</SettingsPage>

// Theme
applyTheme("system|light|dark"); initTheme(); readThemePref()
```

## Testing

Component tests run in jsdom (`// @vitest-environment jsdom` at the top of the file) with
Testing Library. `a11y.test.tsx` runs axe over the whole style guide, including the
table's loading, error and empty states, an open dialog and an open menu. Colour
contrast is checked numerically in `theme/tokens.test.ts` because jsdom has no layout.

Screenshots: `node web/tools/styleguide_shots.mjs [outDir]` (system Chrome via Playwright,
1280px, light and dark).
