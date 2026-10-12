// @vitest-environment jsdom
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { FactsGrid } from "./FactsGrid";
import { PageHeader } from "./PageHeader";
import { SectionCard } from "./SectionCard";
import { StatusPill } from "./StatusPill";
import { DetailPage } from "./templates/DetailPage";
import { ListPage } from "./templates/ListPage";
import { SettingsPage } from "./templates/SettingsPage";
import { applyTheme, readThemePref } from "./theme/theme";

afterEach(() => {
  cleanup();
  document.documentElement.removeAttribute("data-theme");
  localStorage.clear();
});

describe("PageHeader", () => {
  it("renders the h1, status, meta, sub, actions and breadcrumbs, and sets document.title", () => {
    const { unmount } = render(
      <PageHeader
        title="Kaur v. Oduya"
        appName="Counsel"
        status={<StatusPill status="active" />}
        meta={<span>Opened 1 Oct 2026</span>}
        sub="Family law."
        actions={<button type="button">Edit</button>}
        crumbs={[{ label: "Matters", href: "/matters" }, { label: "2026-0041" }]}
      />,
    );
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Kaur v. Oduya");
    expect(document.title).toBe("Kaur v. Oduya · Counsel");
    expect(screen.getByText("Active")).toBeTruthy();
    expect(screen.getByText("Opened 1 Oct 2026")).toBeTruthy();
    expect(screen.getByText("Family law.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Edit" })).toBeTruthy();
    const nav = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(nav).getByRole("link", { name: "Matters" }).getAttribute("href")).toBe("/matters");
    expect(within(nav).getByText("2026-0041").getAttribute("aria-current")).toBe("page");
    unmount();
    expect(document.title).not.toContain("Kaur");
  });
});

describe("SectionCard", () => {
  it("is a section labelled by its heading at the given level, with actions, footer and tone", () => {
    render(
      <SectionCard title="Ledger" description="All entries" actions={<button type="button">Export</button>} footer={<span>foot</span>} tone="gate" headingLevel={3} flush>
        body
      </SectionCard>,
    );
    const s = screen.getByRole("region", { name: "Ledger" });
    expect(within(s).getByRole("heading", { level: 3 }).textContent).toBe("Ledger");
    expect(s.className).toContain("ui-card--tone-gate");
    expect(s.querySelector(".ui-card__body--flush")?.textContent).toBe("body");
    expect(within(s).getByText("All entries")).toBeTruthy();
    expect(within(s).getByRole("button", { name: "Export" })).toBeTruthy();
    expect(within(s).getByText("foot")).toBeTruthy();
  });

  it("without a title it renders no header", () => {
    render(<SectionCard>only</SectionCard>);
    expect(document.querySelector(".ui-card__head")).toBeNull();
    expect(document.querySelector("section")?.getAttribute("aria-labelledby")).toBeNull();
  });
});

describe("FactsGrid", () => {
  it("renders a dl with numeric and mono cells and an em dash for empty values", () => {
    render(
      <FactsGrid
        aria-label="Summary"
        facts={[
          { label: "Balance", value: "$1.00", num: true },
          { label: "Number", value: "2026-0041", mono: true },
          { label: "Closed", value: null },
          { label: "Blank", value: "" },
        ]}
      />,
    );
    const dl = document.querySelector("dl")!;
    expect(dl.getAttribute("aria-label")).toBe("Summary");
    const dds = dl.querySelectorAll("dd");
    expect(dds[0].className).toContain("ui-num");
    expect(dds[1].className).toContain("ui-mono");
    expect(dds[2].textContent).toBe("—");
    expect(dds[3].textContent).toBe("—");
    expect(dds[2].className).toContain("ui-muted");
  });

  it("inline switches the layout class", () => {
    render(<FactsGrid inline facts={[{ label: "A", value: "1" }]} />);
    expect(document.querySelector("dl")?.className).toContain("ui-facts--inline");
  });
});

describe("templates", () => {
  it("ListPage: header, search toolbar, banner slot and body", () => {
    render(
      <ListPage header={{ title: "Matters" }} filters={<input aria-label="Search" />} toolbarEnd={<span>4 of 4</span>} banner={<p>notice</p>}>
        <table>
          <caption>t</caption>
        </table>
      </ListPage>,
    );
    expect(screen.getByRole("heading", { level: 1 }).textContent).toBe("Matters");
    expect(screen.getByRole("search")).toBeTruthy();
    expect(screen.getByLabelText("Search")).toBeTruthy();
    expect(screen.getByText("4 of 4")).toBeTruthy();
    expect(screen.getByText("notice")).toBeTruthy();
    expect(screen.getByRole("table")).toBeTruthy();
  });

  it("DetailPage: header, banner, tabs, main and a Summary aside; no aside yields no grid", () => {
    render(
      <DetailPage header={{ title: "2026-0041" }} banner={<p>gate</p>} tabs={<div role="tablist" aria-label="x" />} aside={<p>facts</p>}>
        <p>main</p>
      </DetailPage>,
    );
    expect(screen.getByRole("complementary", { name: "Summary" }).textContent).toBe("facts");
    expect(screen.getByRole("tablist")).toBeTruthy();
    expect(screen.getByText("gate")).toBeTruthy();
    expect(document.querySelector(".ui-detail-page__grid")).not.toBeNull();
    cleanup();
    render(
      <DetailPage header={{ title: "x" }}>
        <p>main</p>
      </DetailPage>,
    );
    expect(document.querySelector(".ui-detail-page__grid")).toBeNull();
    expect(screen.queryByRole("complementary")).toBeNull();
  });

  it("SettingsPage: section nav with aria-current and the sections column", () => {
    render(
      <SettingsPage header={{ title: "Settings" }} sections={[{ id: "firm", label: "Firm" }, { id: "tax", label: "Tax" }]} current="tax">
        <SectionCard id="firm" title="Firm">
          a
        </SectionCard>
        <SectionCard id="tax" title="Tax">
          b
        </SectionCard>
      </SettingsPage>,
    );
    const nav = screen.getByRole("navigation", { name: "Settings sections" });
    const links = within(nav).getAllByRole("link");
    expect(links.map((l) => l.getAttribute("href"))).toEqual(["#firm", "#tax"]);
    expect(links[1].getAttribute("aria-current")).toBe("true");
    expect(links[0].getAttribute("aria-current")).toBeNull();
    expect(document.getElementById("tax")?.textContent).toContain("b");
  });
});

describe("theme", () => {
  it("applyTheme sets and clears data-theme and remembers the choice", () => {
    applyTheme("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
    expect(readThemePref()).toBe("dark");
    applyTheme("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
    applyTheme("system");
    expect(document.documentElement.getAttribute("data-theme")).toBeNull();
    expect(readThemePref()).toBe("system");
  });
});
