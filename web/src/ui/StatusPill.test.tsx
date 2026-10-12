// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Banner } from "./Banner";
import { EmptyState } from "./EmptyState";
import { Loading, Skeleton } from "./Skeleton";
import { GLYPH, STATUS, StatusPill, statusLabel, statusTone, type Status, type Tone } from "./StatusPill";

afterEach(cleanup);

describe("StatusPill", () => {
  const all = Object.keys(STATUS) as Status[];

  it.each(all)("%s renders its label, tone class and a glyph (never colour alone)", (s) => {
    render(<StatusPill status={s} />);
    const el = document.querySelector(`[data-status="${s}"]`)!;
    expect(el.textContent).toContain(statusLabel(s));
    const tone: Tone = statusTone(s);
    if (tone !== "neutral") expect(el.className).toContain(`ui-pill--${tone}`);
    const glyph = el.querySelector(".ui-pill__glyph")!;
    expect(glyph.getAttribute("aria-hidden")).toBe("true");
    expect(glyph.textContent).toBe(GLYPH[tone]);
  });

  it("covers the gate vocabulary with the amber tone and the agent with teal", () => {
    expect(statusTone("pending")).toBe("gate");
    expect(statusLabel("pending")).toBe("Needs approval");
    expect(statusTone("approved")).toBe("ok");
    expect(statusTone("rejected")).toBe("bad");
    expect(statusTone("withdrawn")).toBe("neutral");
    expect(statusTone("overdrawn")).toBe("bad");
    expect(statusTone("running")).toBe("agent");
  });

  it("label overrides text but keeps the tone", () => {
    render(<StatusPill status="pending" label="Needs Linda K" />);
    const el = screen.getByText("Needs Linda K");
    expect(el.className).toContain("ui-pill--gate");
  });
});

describe("Banner", () => {
  it.each([
    { tone: "info" as const, role: "status" },
    { tone: "gate" as const, role: "status" },
    { tone: "ok" as const, role: "status" },
    { tone: "warn" as const, role: "status" },
    { tone: "bad" as const, role: "alert" },
  ])("$tone is a $role with title, text and actions", ({ tone, role }) => {
    render(
      <Banner tone={tone} title="Title" actions={<button type="button">Act</button>}>
        Text
      </Banner>,
    );
    const el = screen.getByRole(role);
    expect(el.className).toContain(`ui-banner--${tone}`);
    expect(el.textContent).toContain("Title");
    expect(el.textContent).toContain("Text");
    expect(screen.getByRole("button", { name: "Act" })).toBeTruthy();
    expect(el.querySelector(".ui-banner__glyph")?.getAttribute("aria-hidden")).toBe("true");
  });
});

describe("EmptyState", () => {
  it.each([
    { kind: undefined, alert: false },
    { kind: "empty" as const, alert: false },
    { kind: "not-found" as const, alert: false },
    { kind: "error" as const, alert: true },
    { kind: "forbidden" as const, alert: true },
  ])("kind $kind alert=$alert", ({ kind, alert }) => {
    render(<EmptyState kind={kind} title="T" hint="H" detail="/x" action={<a href="/">Home</a>} />);
    const el = document.querySelector(".ui-empty")!;
    expect(el.getAttribute("role")).toBe(alert ? "alert" : null);
    expect(el.getAttribute("data-kind")).toBe(kind ?? "empty");
    expect(el.textContent).toContain("T");
    expect(el.textContent).toContain("H");
    expect(el.querySelector("code")?.textContent).toBe("/x");
    expect(screen.getByRole("link", { name: "Home" })).toBeTruthy();
  });
});

describe("Skeleton", () => {
  it("is hidden from AT; Loading announces the region", () => {
    render(
      <Loading label="Loading matters">
        <Skeleton lines={3} />
        <Skeleton width={40} height={40} />
      </Loading>,
    );
    const status = screen.getByRole("status");
    expect(status.getAttribute("aria-busy")).toBe("true");
    expect(status.textContent).toBe("Loading matters");
    for (const el of document.querySelectorAll(".ui-skeleton, .ui-skeleton-lines")) {
      expect(el.closest("[aria-hidden='true']")).not.toBeNull();
    }
    expect(document.querySelectorAll(".ui-skeleton-lines .ui-skeleton")).toHaveLength(3);
  });
});
