import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import feature from "./feature";
import PortalMatters, { PortalMattersView, visibleItems, type PortalData, type PortalItem } from "./PortalMatters";

const item = (ID: string, MatterID: string, Kind: string, SharedAt = "2026-10-01T14:00:00Z"): PortalItem => ({
  ID,
  MatterID,
  Kind,
  Title: `title-${ID}`,
  SharedAt,
});

describe("portal guest sees own shared documents and bills only", () => {
  const cases: { name: string; data: PortalData; want: Record<string, string[]> }[] = [
    { name: "nothing shared", data: { matters: [], items: [] }, want: {} },
    {
      name: "own documents and bills",
      data: { matters: ["m1"], items: [item("d1", "m1", "document"), item("b1", "m1", "bill")] },
      want: { m1: ["d1", "b1"] },
    },
    {
      name: "foreign matter dropped",
      data: { matters: ["m1"], items: [item("d1", "m1", "document"), item("x1", "m2", "bill")] },
      want: { m1: ["d1"] },
    },
    {
      name: "unknown kind dropped",
      data: { matters: ["m1"], items: [item("n1", "m1", "note"), item("t1", "m1", "trust_ledger")] },
      want: { m1: [] },
    },
    {
      name: "grouped by matter, newest first",
      data: {
        matters: ["m1", "m2"],
        items: [
          item("old", "m1", "document", "2026-09-01T14:00:00Z"),
          item("b2", "m2", "bill"),
          item("new", "m1", "bill", "2026-10-05T14:00:00Z"),
        ],
      },
      want: { m1: ["new", "old"], m2: ["b2"] },
    },
  ];
  for (const c of cases) {
    it(c.name, () => {
      const got = Object.fromEntries([...visibleItems(c.data)].map(([m, its]) => [m, its.map((i) => i.ID)]));
      expect(got).toEqual(c.want);

      const html = renderToStaticMarkup(<PortalMattersView data={c.data} />);
      const shown = [...html.matchAll(/data-item="([^"]+)"/g)].map((m) => m[1]);
      expect(shown.sort()).toEqual(Object.values(c.want).flat().sort());
      for (const it of c.data.items) {
        if (!shown.includes(it.ID)) expect(html).not.toContain(it.Title);
      }
      if (Object.keys(c.want).length === 0) expect(html).toContain("Nothing has been shared with you yet.");
    });
  }

  it("shows a loading state before data arrives", () => {
    const html = renderToStaticMarkup(<PortalMatters load={() => new Promise(() => {})} />);
    expect(html).toContain("Loading your matters");
  });
});

describe("portal feature", () => {
  it("default-exports a feature with the portal component", () => {
    expect(feature).toMatchObject({ id: "portal", path: "/portal/matters", component: PortalMatters });
  });

  const sources = import.meta.glob<string>(["./*.ts", "./*.tsx", "!./*.test.tsx"], {
    query: "?raw",
    import: "default",
    eager: true,
  });
  const cases = Object.entries(sources);
  it("has sources to scan", () => {
    expect(cases.length).toBeGreaterThanOrEqual(2);
  });
  for (const [file, src] of cases) {
    it(`${file} imports no other feature folder`, () => {
      const specs = [...src.matchAll(/from\s+["']([^"']+)["']/g)].map((m) => m[1]);
      for (const s of specs) {
        expect(s, `${file} imports ${s}`).not.toMatch(/(^|\/)features\//);
        expect(s, `${file} imports ${s}`).not.toMatch(/^\.\.\/(?!\.\.\/)/);
      }
    });
  }
});
