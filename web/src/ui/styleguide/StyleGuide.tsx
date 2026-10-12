import { useState } from "react";
import {
  Banner,
  Button,
  DataTable,
  DateTime,
  Dialog,
  EmptyState,
  FactsGrid,
  Field,
  GateStatus,
  Input,
  Loading,
  MatterChip,
  Menu,
  Money,
  PageHeader,
  SectionCard,
  Select,
  Skeleton,
  STATUS,
  StatusPill,
  TabPanel,
  Tabs,
  Textarea,
  ToastProvider,
  applyTheme,
  light,
  readThemePref,
  useToast,
  type Column,
  type Sort,
  type Status,
  type ThemePref,
} from "../index";

// Fixture rows: a trust ledger. Amounts are integer cents; one row is overdrawn on purpose.
interface Row {
  id: string;
  date: string;
  matter: { number: string; name: string };
  memo: string;
  cents: number;
  balanceCents: number;
  status: Status;
}

const ROWS: Row[] = [
  { id: "1", date: "2026-10-01T14:05:00Z", matter: { number: "2026-0041", name: "Kaur v. Oduya" }, memo: "Retainer received", cents: 1_250_000, balanceCents: 1_250_000, status: "reconciled" },
  { id: "2", date: "2026-10-03T18:30:00Z", matter: { number: "2026-0041", name: "Kaur v. Oduya" }, memo: "Transfer to general (invoice 1042)", cents: -452_30, balanceCents: 1_204_770, status: "approved" },
  { id: "3", date: "2026-10-05T23:30:00Z", matter: { number: "2026-0038", name: "Estate of M. Tremblay" }, memo: "Disbursement: court filing", cents: -5_200, balanceCents: -5_200, status: "pending" },
  { id: "4", date: "2026-10-07T13:00:00Z", matter: { number: "2026-0044", name: "Nguyen purchase, 12 Elm St" }, memo: "Closing funds", cents: 48_500_000, balanceCents: 48_500_000, status: "unconfirmed" },
];

function TableDemo() {
  const [sort, setSort] = useState<Sort | null>({ column: "date", dir: "desc" });
  const [state, setState] = useState<"ready" | "loading" | "error" | "empty">("ready");
  const toast = useToast();
  const columns: Column<Row>[] = [
    { id: "date", header: "Date", sortable: true, cell: (r) => <DateTime value={r.date} />, width: "120px" },
    { id: "matter", header: "Matter", cell: (r) => <MatterChip number={r.matter.number} name={r.matter.name} href="#" /> },
    { id: "memo", header: "Memo", cell: (r) => r.memo },
    { id: "status", header: "Status", cell: (r) => <StatusPill status={r.status} />, width: "150px" },
    { id: "amount", header: "Amount", num: true, sortable: true, cell: (r) => <Money cents={r.cents} context="trust" code="CAD" /> },
    { id: "balance", header: "Client balance", num: true, cell: (r) => <Money cents={r.balanceCents} context="trust" code="CAD" /> },
  ];
  const rows = [...ROWS].sort((a, b) => {
    if (!sort) return 0;
    const k = sort.column === "amount" ? "cents" : "date";
    const d = a[k] < b[k] ? -1 : a[k] > b[k] ? 1 : 0;
    return sort.dir === "asc" ? d : -d;
  });
  return (
    <>
      <div className="ui-row" style={{ marginBottom: "var(--s-3)" }}>
        {(["ready", "loading", "error", "empty"] as const).map((s) => (
          <Button key={s} size="sm" variant={state === s ? "primary" : "default"} onClick={() => setState(s)} aria-pressed={state === s}>
            {s}
          </Button>
        ))}
      </div>
      <DataTable
        caption="Trust ledger, Linda K Law mixed trust account"
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        state={state}
        sort={sort}
        onSortChange={setSort}
        rowMenu={(r) => [
          { id: "open", label: "Open entry", onSelect: () => toast.show({ text: `Opened entry ${r.id}` }) },
          { id: "receipt", label: "Print receipt", onSelect: () => toast.show({ text: "Receipt queued", tone: "ok" }) },
          { id: "reverse", label: "Propose reversal", danger: true, onSelect: () => toast.show({ text: "Reversal sent for approval", tone: "gate" }) },
        ]}
        rowMenuLabel={(r) => `Actions for ${r.memo}`}
        empty={{ title: "No ledger entries", hint: "Entries appear when funds are received or paid out.", action: <Button size="sm">Record a receipt</Button> }}
        error={{ title: "The ledger could not be loaded", hint: "Check your connection and try again.", detail: "problem: ledger.unavailable", action: <Button size="sm" onClick={() => setState("ready")}>Retry</Button> }}
        footer={
          <>
            <span>{ROWS.length} entries</span>
            <span>
              Account balance: <Money cents={ROWS.reduce((s, r) => s + r.cents, 0)} context="trust" code="CAD" />
            </span>
          </>
        }
      />
    </>
  );
}

function DialogDemo() {
  const [open, setOpen] = useState(false);
  const toast = useToast();
  return (
    <>
      <Button variant="danger" onClick={() => setOpen(true)}>
        Propose reversal
      </Button>
      <Dialog
        open={open}
        onClose={() => setOpen(false)}
        title="Propose a reversal"
        description="A reversing entry is drafted for a trust signatory to approve. Nothing moves until they do."
        footer={
          <>
            <Button onClick={() => setOpen(false)}>Cancel</Button>
            <Button
              variant="danger"
              solid
              onClick={() => {
                setOpen(false);
                toast.show({ text: "Reversal sent for approval", tone: "gate" });
              }}
            >
              Send for approval
            </Button>
          </>
        }
      >
        <Field label="Reason" required hint="Shown to the approver and kept in the audit trail.">
          {(p) => <Textarea {...p} placeholder="Why this entry is wrong" />}
        </Field>
      </Dialog>
    </>
  );
}

function ToastDemo() {
  const toast = useToast();
  return (
    <div className="ui-row">
      <Button onClick={() => toast.show({ text: "Matter saved" })}>Neutral</Button>
      <Button onClick={() => toast.show({ text: "Invoice 1042 sent to the client", tone: "ok" })}>Ok</Button>
      <Button onClick={() => toast.show({ text: "Transfer sent to a trust signatory", tone: "gate" })}>Gate</Button>
      <Button onClick={() => toast.show({ text: "The bill could not be posted (stale version)", tone: "bad", action: { label: "Reload", onClick: () => undefined } })}>Bad</Button>
    </div>
  );
}

function TabsDemo() {
  const [tab, setTab] = useState("ledger");
  return (
    <>
      <Tabs
        idPrefix="sg"
        label="Trust account sections"
        value={tab}
        onChange={setTab}
        tabs={[
          { id: "ledger", label: "Ledger", count: 4 },
          { id: "gates", label: "Approvals", count: 1, countTone: "gate" },
          { id: "recon", label: "Reconciliation" },
          { id: "closed", label: "Closed matters", disabled: true },
        ]}
      />
      <TabPanel idPrefix="sg" tabId={tab}>
        <p className="ui-muted">{tab === "ledger" ? "Ledger entries for this account." : tab === "gates" ? "One transfer awaits a signatory." : "Last reconciled 30 Sep 2026."}</p>
      </TabPanel>
    </>
  );
}

function ThemeSwitch() {
  const [pref, setPref] = useState<ThemePref>(() => readThemePref());
  return (
    <Field label="Theme">
      {(p) => (
        <Select
          {...p}
          value={pref}
          options={[
            { value: "system", label: "Follow the system" },
            { value: "light", label: "Light" },
            { value: "dark", label: "Dark" },
          ]}
          onChange={(e) => {
            const v = e.target.value as ThemePref;
            setPref(v);
            applyTheme(v);
          }}
        />
      )}
    </Field>
  );
}

const SWATCHES: (keyof typeof light)[] = ["bg", "panel", "soft", "ink", "muted", "line", "accent", "accentSoft", "gate", "gateSoft", "ok", "okSoft", "warn", "warnSoft", "bad", "badSoft"];

// StyleGuide is the living reference for the kit: every component in every state, with the
// fixture data a legal screen would show. It is served at /styleguide.html in dev.
export function StyleGuide() {
  return (
    <ToastProvider>
      <div className="ui-page" style={{ maxWidth: 1240 }}>
        <PageHeader
          title="Counsel UI kit"
          status={<StatusPill status="active" label="Design system" />}
          sub="Tokens, components, legal and finance primitives, page templates. Light and dark. Money is integer cents; dates are America/Toronto; the agent never approves."
          actions={<ThemeSwitch />}
          crumbs={[{ label: "Counsel", href: "/" }, { label: "Style guide" }]}
        />
        <div className="ui-stack">
          <SectionCard id="colour" title="Colour" description="Teal is the agent's work, amber is waiting on a person, ink is the firm's records, red is money or law gone wrong.">
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(120px, 1fr))", gap: "var(--s-3)" }}>
              {SWATCHES.map((k) => (
                <div key={k} style={{ border: "1px solid var(--c-line)", borderRadius: "var(--r-lg)", overflow: "hidden" }}>
                  <div style={{ height: 40, background: `var(--c-${k.replace(/[A-Z]/g, (c) => "-" + c.toLowerCase())})` }} />
                  <div style={{ padding: "var(--s-1) var(--s-2)", fontSize: "var(--fs-sm)" }}>
                    <code>{k}</code>
                  </div>
                </div>
              ))}
            </div>
          </SectionCard>

          <SectionCard id="type" title="Type and spacing" description="Geist, 13px dense body and 14px prose, on a 4px grid. Numbers are always tabular.">
            <div className="ui-stack" style={{ gap: "var(--s-2)" }}>
              <h1>Page title 22/600</h1>
              <h2>Section title 16/600</h2>
              <h3>Card title 14/600</h3>
              <p>Body 14/400. The agent drafts; a lawyer approves. Trust money moves only after a signatory decides.</p>
              <p style={{ fontSize: "var(--fs-md)" }}>Dense 13/400 for tables and facts.</p>
              <p className="ui-muted" style={{ fontSize: "var(--fs-sm)" }}>Caption 12/400, muted.</p>
              <p className="ui-mono">Mono: 2026-0041 · CHQ 000417 · 01J9Z3K8Q2R4T6V8X0Y2Z4A6B8</p>
            </div>
          </SectionCard>

          <SectionCard id="status" title="Status vocabulary" description="Fixed keys; every pill has a glyph as well as a colour.">
            <div className="ui-row">
              {(Object.keys(STATUS) as Status[]).map((s) => (
                <StatusPill key={s} status={s} />
              ))}
            </div>
          </SectionCard>

          <SectionCard id="buttons" title="Buttons">
            <div className="ui-stack" style={{ gap: "var(--s-3)" }}>
              <div className="ui-row">
                <Button variant="primary">Approve</Button>
                <Button>Send back</Button>
                <Button variant="danger">Reverse entry</Button>
                <Button variant="danger" solid>
                  Confirm reversal
                </Button>
                <Button variant="agent">{"✦"} Ask the agent</Button>
                <Button variant="ghost">Cancel</Button>
              </div>
              <div className="ui-row">
                <Button size="sm">Small</Button>
                <Button size="sm" variant="primary">
                  Small primary
                </Button>
                <Button busy>Submitting</Button>
                <Button disabled>Disabled</Button>
                <Menu label="More actions" items={[{ id: "a", label: "Export CSV", onSelect: () => undefined }, { id: "b", label: "Archive", danger: true, onSelect: () => undefined }]} />
              </div>
            </div>
          </SectionCard>

          <SectionCard id="fields" title="Fields">
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: "var(--s-3)" }}>
              <Field label="Client name" required hint="As it appears on identification.">
                {(p) => <Input {...p} defaultValue="Priya Kaur" />}
              </Field>
              <Field label="Matter number">{(p) => <Input {...p} mono defaultValue="2026-0041" />}</Field>
              <Field label="Amount (cents)" hint="Whole cents; no decimals.">
                {(p) => <Input {...p} num inputMode="numeric" defaultValue="1250000" />}
              </Field>
              <Field label="Responsible lawyer">
                {(p) => <Select {...p} placeholder="Choose a lawyer" defaultValue="" options={[{ value: "lk", label: "Linda K" }, { value: "jo", label: "J. Oduya", disabled: true }]} />}
              </Field>
              <Field label="HST number" error="Must be 9 digits followed by RT0001.">
                {(p) => <Input {...p} mono defaultValue="12345" />}
              </Field>
              <Field label="Closed matter">{(p) => <Input {...p} disabled defaultValue="Read only" />}</Field>
            </div>
          </SectionCard>

          <SectionCard id="money" title="Money, dates, matters, gates" description="Integer cents end to end. An overdraft is signed, red and labelled. Dates are Toronto time.">
            <FactsGrid
              facts={[
                { label: "Trust balance", value: <Money cents={1_204_770} context="trust" code="CAD" size="lg" /> },
                { label: "Overdrawn client ledger", value: <Money cents={-5_200} context="trust" code="CAD" size="lg" /> },
                { label: "Invoice total (billing)", value: <Money cents={452_30} context="billing" /> },
                { label: "Credit note (billing)", value: <Money cents={-10_000} context="billing" /> },
                { label: "Zero", value: <Money cents={0} /> },
                { label: "Large", value: <Money cents={123_456_789_012} context="trust" code="CAD" /> },
                { label: "Received (instant)", value: <DateTime value="2026-10-05T23:30:00Z" style="datetime" /> },
                { label: "Due (calendar date)", value: <DateTime value="2026-10-30" /> },
                { label: "Period", value: <DateTime value="2026-10-01" style="month" /> },
                { label: "Missing date", value: <DateTime value={null} /> },
                { label: "Matter", value: <MatterChip number="2026-0041" name="Kaur v. Oduya" href="#" /> },
                { label: "Closed matter", value: <MatterChip number="2025-0112" name="Singh incorporation" closed /> },
              ]}
            />
            <div className="ui-stack" style={{ marginTop: "var(--s-5)", gap: "var(--s-3)" }}>
              <GateStatus state="pending" preparer={{ name: "Agent", kind: "agent" }} actingFor={{ name: "Linda K", kind: "person" }} approver={{ name: "M. Tremblay", kind: "person" }} />
              <GateStatus state="pending" preparer={{ name: "Linda K", kind: "person" }} />
              <GateStatus state="approved" preparer={{ name: "Linda K", kind: "person" }} approver={{ name: "M. Tremblay", kind: "person" }} decidedAt="2026-10-06T15:12:00Z" />
              <GateStatus state="rejected" preparer={{ name: "Agent", kind: "agent" }} actingFor={{ name: "Linda K", kind: "person" }} approver={{ name: "M. Tremblay", kind: "person" }} decidedAt="2026-10-06T15:12:00Z" />
              <GateStatus state="withdrawn" preparer={{ name: "Linda K", kind: "person" }} decidedAt="2026-10-06T15:12:00Z" />
            </div>
          </SectionCard>

          <SectionCard id="table" title="DataTable" description="Sortable headers, right-aligned tabular numbers, row menu, and loading, empty and error states inside the frame." flush>
            <div style={{ padding: "var(--s-4)" }}>
              <TableDemo />
            </div>
          </SectionCard>

          <SectionCard id="banners" title="Banners and empty states">
            <div className="ui-stack" style={{ gap: "var(--s-3)" }}>
              <Banner tone="gate" title="Waiting on a trust signatory" actions={<Button size="sm">Open approval</Button>}>
                Transfer of $452.30 CAD to general was prepared by the agent for Linda K.
              </Banner>
              <Banner tone="bad" title="Client ledger overdrawn by $52.00 CAD" actions={<Button size="sm" variant="danger">Review</Button>}>
                Estate of M. Tremblay. By-Law 9 s.18(3): a client trust ledger must never be overdrawn.
              </Banner>
              <Banner tone="warn" title="Reconciliation is 12 days overdue">Last reconciled 30 Sep 2026.</Banner>
              <Banner tone="ok" title="Invoice 1042 paid in full" />
              <Banner tone="info">Dates are shown in Toronto time.</Banner>
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(240px, 1fr))", gap: "var(--s-3)" }}>
                <EmptyState title="No open matters" hint="New matters appear here once intake is complete." action={<Button size="sm" variant="primary">New matter</Button>} />
                <EmptyState kind="error" title="Could not load deadlines" hint="Try again in a moment." detail="problem: deadlines.unavailable" action={<Button size="sm">Retry</Button>} />
                <EmptyState kind="forbidden" title="You do not have access" hint="Ask a firm admin for the trust.ledger.view capability." />
                <EmptyState kind="not-found" title="Page not found" detail="/matters/2026-9999" action={<a href="/">Go to home</a>} />
              </div>
            </div>
          </SectionCard>

          <SectionCard id="overlays" title="Dialog, tabs, toast, skeleton">
            <div className="ui-stack" style={{ gap: "var(--s-4)" }}>
              <TabsDemo />
              <div className="ui-row">
                <DialogDemo />
              </div>
              <ToastDemo />
              <Loading label="Loading the matter">
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: "var(--s-4)" }}>
                  <Skeleton lines={3} />
                  <Skeleton height={64} />
                </div>
              </Loading>
            </div>
          </SectionCard>
        </div>
      </div>
    </ToastProvider>
  );
}
