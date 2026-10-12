// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { DateTime } from "./DateTime";
import { GateError, GateStatus } from "./GateStatus";
import { MatterChip } from "./MatterChip";
import { Money } from "./Money";
import { formatDate, isoDate, TimeError } from "./time";

afterEach(cleanup);

describe("Money", () => {
  it.each([
    { cents: 0, text: "$0.00", sign: "zero" },
    { cents: 5, text: "$0.05", sign: "pos" },
    { cents: 45230, text: "$452.30", sign: "pos" },
    { cents: 123456789012, text: "$1,234,567,890.12", sign: "pos" },
    { cents: -45230, text: "-$452.30", sign: "neg" },
  ])("renders $cents as $text", ({ cents, text, sign }) => {
    render(<Money cents={cents} />);
    const el = document.querySelector(".ui-money")!;
    expect(el.querySelector(".ui-money__amount")?.textContent).toBe(text);
    expect(el.getAttribute("data-sign")).toBe(sign);
    expect(el.className).toContain("ui-money");
  });

  it("trust: a negative is signed, danger-toned, labelled Overdrawn and announced", () => {
    render(<Money cents={-45230} context="trust" code="CAD" />);
    const el = document.querySelector(".ui-money")!;
    expect(el.className).toContain("ui-money--neg");
    expect(el.getAttribute("aria-label")).toBe("overdrawn by $452.30 CAD");
    expect(el.querySelector("[data-status='overdrawn']")?.textContent).toContain("Overdrawn");
    expect(el.querySelector(".ui-money__amount")?.textContent).toBe("-$452.30");
    expect(el.querySelector(".ui-money__code")?.textContent).toBe("CAD");
  });

  it("trust: a positive or zero balance has no Overdrawn label", () => {
    render(<Money cents={100} context="trust" code="CAD" />);
    expect(document.querySelector("[data-status='overdrawn']")).toBeNull();
    expect(document.querySelector(".ui-money")?.getAttribute("aria-label")).toBeNull();
  });

  it("billing: a negative is a signed credit in the danger colour, without the Overdrawn label", () => {
    render(<Money cents={-10000} context="billing" />);
    const el = document.querySelector(".ui-money")!;
    expect(el.className).toContain("ui-money--neg");
    expect(el.textContent).toBe("-$100.00");
    expect(el.querySelector("[data-status='overdrawn']")).toBeNull();
  });

  it("accepts bigint and refuses a float", () => {
    render(<Money cents={9007199254740993n} />);
    expect(document.querySelector(".ui-money__amount")?.textContent).toBe("$90,071,992,547,409.93");
    expect(() => render(<Money cents={1.5} />)).toThrow(/safe integer/);
  });

  it("size=lg adds the headline modifier", () => {
    render(<Money cents={1} size="lg" />);
    expect(document.querySelector(".ui-money")?.className).toContain("ui-money--lg");
  });
});

describe("time", () => {
  it.each([
    // 23:30 UTC on 5 Oct is 19:30 EDT: the Toronto day is still the 5th
    { v: "2026-10-05T23:30:00Z", style: "date" as const, out: "5 Oct 2026" },
    { v: "2026-10-05T23:30:00Z", style: "datetime" as const, out: "5 Oct 2026, 7:30 p.m." },
    { v: "2026-10-05T23:30:00Z", style: "time" as const, out: "7:30 p.m." },
    // 03:30 UTC on 6 Oct is 23:30 EDT on the 5th
    { v: "2026-10-06T03:30:00Z", style: "date" as const, out: "5 Oct 2026" },
    { v: "2026-10-06T03:30:00Z", style: "time" as const, out: "11:30 p.m." },
    // standard time (EST, UTC-5) in January
    { v: "2026-01-15T04:59:00Z", style: "datetime" as const, out: "14 Jan 2026, 11:59 p.m." },
    { v: "2026-01-15T05:00:00Z", style: "datetime" as const, out: "15 Jan 2026, 12:00 a.m." },
    { v: "2026-01-15T17:00:00Z", style: "time" as const, out: "12:00 p.m." },
    { v: "2026-10-05T23:30:00Z", style: "month" as const, out: "Oct 2026" },
    // calendar dates are never shifted
    { v: "2026-10-30", style: "date" as const, out: "30 Oct 2026" },
    { v: "2026-10-30", style: "month" as const, out: "Oct 2026" },
  ])("$v as $style is $out", ({ v, style, out }) => {
    expect(formatDate(v, style)).toBe(out);
  });

  it("isoDate gives the Toronto calendar day", () => {
    expect(isoDate("2026-10-06T03:30:00Z")).toBe("2026-10-05");
    expect(isoDate(new Date("2026-03-08T07:30:00Z"))).toBe("2026-03-08");
  });

  it("rejects garbage", () => {
    expect(() => formatDate("not a date")).toThrow(TimeError);
    expect(() => formatDate("2026-13-01")).toThrow(TimeError);
  });

  it("DateTime renders a <time> with the instant and shows an em dash for a missing value", () => {
    render(<DateTime value="2026-10-05T23:30:00Z" style="datetime" />);
    const t = document.querySelector("time")!;
    expect(t.getAttribute("datetime")).toBe("2026-10-05T23:30:00.000Z");
    expect(t.textContent).toBe("5 Oct 2026, 7:30 p.m.");
    cleanup();
    render(<DateTime value="2026-10-30" />);
    expect(document.querySelector("time")?.getAttribute("datetime")).toBe("2026-10-30");
    cleanup();
    render(<DateTime value={null} />);
    expect(document.querySelector("time")).toBeNull();
    expect(screen.getByText("—")).toBeTruthy();
  });
});

describe("GateStatus", () => {
  const linda = { name: "Linda K", kind: "person" as const };
  const marc = { name: "M. Tremblay", kind: "person" as const };
  const agent = { name: "Agent", kind: "agent" as const };

  it("pending shows the preparer and the expected approver", () => {
    render(<GateStatus state="pending" preparer={linda} approver={marc} />);
    const el = document.querySelector(".ui-gate")!;
    expect(el.getAttribute("data-state")).toBe("pending");
    expect(el.querySelector("[data-status='pending']")?.textContent).toContain("Needs approval");
    expect(el.textContent).toContain("Prepared by Linda K");
    expect(el.textContent).toContain("Awaiting M. Tremblay");
  });

  it("pending with no named approver says someone other than the preparer must decide", () => {
    render(<GateStatus state="pending" preparer={linda} />);
    expect(document.querySelector(".ui-gate")?.textContent).toContain("other than the preparer");
  });

  it("agent preparer is marked and shows who it acted for", () => {
    render(<GateStatus state="approved" preparer={agent} actingFor={linda} approver={marc} decidedAt="2026-10-06T15:12:00Z" />);
    const el = document.querySelector(".ui-gate")!;
    expect(el.textContent).toContain("Agent prepared");
    expect(el.textContent).toContain("for Linda K");
    expect(el.textContent).toContain("Approved by M. Tremblay");
    expect(el.querySelector("time")?.textContent).toBe("6 Oct 2026, 11:12 a.m.");
  });

  it.each([
    { state: "rejected" as const, text: "Rejected by M. Tremblay", pill: "Rejected" },
    { state: "withdrawn" as const, text: "Withdrawn by the preparer", pill: "Withdrawn" },
  ])("$state", ({ state, text, pill }) => {
    render(<GateStatus state={state} preparer={linda} approver={state === "rejected" ? marc : undefined} />);
    const el = document.querySelector(".ui-gate")!;
    expect(el.textContent).toContain(text);
    expect(el.querySelector(".ui-pill")?.textContent).toContain(pill);
  });

  it("refuses an agent approver and a self-approval", () => {
    // the type forbids it; the runtime check is for data that arrives untyped
    const bad = agent as unknown as typeof marc;
    expect(() => render(<GateStatus state="approved" preparer={linda} approver={bad} />)).toThrow(GateError);
    expect(() => render(<GateStatus state="approved" preparer={linda} approver={linda} />)).toThrow(GateError);
  });

  it("compact renders the pill only with a title", () => {
    render(<GateStatus state="pending" preparer={linda} approver={marc} compact />);
    expect(document.querySelector(".ui-gate")).toBeNull();
    expect(document.querySelector("[title]")?.getAttribute("title")).toContain("approver M. Tremblay");
  });
});

describe("MatterChip", () => {
  it("renders number and name, as a link when href is set", () => {
    render(<MatterChip number="2026-0041" name="Kaur v. Oduya" href="/matters/1" />);
    const a = screen.getByRole("link", { name: "Matter 2026-0041, Kaur v. Oduya" });
    expect(a.getAttribute("href")).toBe("/matters/1");
    expect(a.querySelector(".ui-matter__number")?.textContent).toBe("2026-0041");
    expect(a.querySelector(".ui-matter__name")?.textContent).toBe("Kaur v. Oduya");
  });

  it("renders a plain chip with a closed marker", () => {
    render(<MatterChip number="2025-0112" closed />);
    expect(screen.queryByRole("link")).toBeNull();
    const el = document.querySelector(".ui-matter")!;
    expect(el.getAttribute("aria-label")).toBe("Matter 2025-0112");
    expect(el.className).toContain("ui-matter--closed");
    expect(el.querySelector(".ui-matter__name")).toBeNull();
  });
});
