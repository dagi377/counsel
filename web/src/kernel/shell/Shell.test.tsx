import { describe, expect, it } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { Shell } from "./Shell";
import { buildNav, matchRoute, ShellError, type ScreenProps, type ShellFeature } from "./nav";

// The helpdesk fixture (test/fixtures/helpdesk) is a tiny non-legal app. Its registered portal
// pages become the feature list the shell is tested against; no app/legal code is involved.
interface PortalPage {
  id: string;
  route: string;
  capability: string;
}

const pages = Object.values(
  import.meta.glob<PortalPage[]>("../../../../test/fixtures/helpdesk/portalpage.json", {
    eager: true,
    import: "default",
  }),
)[0];

const titles: Record<string, string> = { "ticket.my_tickets": "My tickets", "ticket.detail": "Ticket" };

function MyTickets() {
  return <p>ticket list</p>;
}

function TicketDetail({ params }: ScreenProps) {
  return <p>{`ticket ${params.id}`}</p>;
}

const screens: Record<string, ShellFeature["component"]> = {
  "ticket.my_tickets": MyTickets,
  "ticket.detail": TicketDetail,
};

const helpdesk: ShellFeature[] = (pages ?? []).map((p) => ({
  id: p.id,
  title: titles[p.id] ?? p.id,
  path: p.route,
  component: screens[p.id],
}));

describe("helpdesk fixture", () => {
  it("loads the registered pages", () => {
    expect(helpdesk.map((f) => [f.id, f.path])).toEqual([
      ["ticket.my_tickets", "/tickets"],
      ["ticket.detail", "/tickets/{id}"],
    ]);
  });
});

describe("buildNav", () => {
  const cases: { name: string; features: ShellFeature[]; want?: string[]; err?: RegExp }[] = [
    { name: "helpdesk: parameter routes get no entry", features: helpdesk, want: ["ticket.my_tickets"] },
    { name: "keeps registration order", features: [f("b", "/b"), f("a", "/a")], want: ["b", "a"] },
    { name: "empty list", features: [], want: [] },
    { name: "duplicate route", features: [f("a", "/x"), f("b", "/x")], err: /already taken by "a"/ },
    { name: "duplicate route by parameter", features: [f("a", "/t/{id}"), f("b", "/t/{key}")], err: /already taken/ },
    { name: "relative path", features: [f("a", "tickets")], err: /must start with "\/"/ },
  ];
  for (const c of cases) {
    it(c.name, () => {
      if (c.err) {
        expect(() => buildNav(c.features)).toThrow(ShellError);
        expect(() => buildNav(c.features)).toThrow(c.err);
        return;
      }
      expect(buildNav(c.features).map((n) => n.id)).toEqual(c.want);
    });
  }
});

describe("matchRoute", () => {
  const withNew = [...helpdesk, f("ticket.new", "/tickets/new")];
  const cases: { name: string; features: ShellFeature[]; path: string; id: string | null; params?: Record<string, string> }[] = [
    { name: "list", features: helpdesk, path: "/tickets", id: "ticket.my_tickets", params: {} },
    { name: "trailing slash", features: helpdesk, path: "/tickets/", id: "ticket.my_tickets", params: {} },
    { name: "parameter", features: helpdesk, path: "/tickets/42", id: "ticket.detail", params: { id: "42" } },
    { name: "parameter is decoded", features: helpdesk, path: "/tickets/a%20b", id: "ticket.detail", params: { id: "a b" } },
    { name: "bad escape does not match", features: helpdesk, path: "/tickets/%E0%A4%A", id: null },
    { name: "literal beats parameter", features: withNew, path: "/tickets/new", id: "ticket.new", params: {} },
    { name: "parameter still matches others", features: withNew, path: "/tickets/7", id: "ticket.detail", params: { id: "7" } },
    { name: "unknown path", features: helpdesk, path: "/billing", id: null },
    { name: "too deep", features: helpdesk, path: "/tickets/1/x", id: null },
    { name: "root with no root feature", features: helpdesk, path: "/", id: null },
  ];
  for (const c of cases) {
    it(c.name, () => {
      const m = matchRoute(c.features, c.path);
      expect(m?.feature.id ?? null).toBe(c.id);
      if (c.params) expect(m?.params).toEqual(c.params);
    });
  }
});

describe("Shell", () => {
  const cases: { name: string; features: ShellFeature[]; path: string; has: string[]; not: string[] }[] = [
    {
      name: "helpdesk list screen, nav marks it current",
      features: helpdesk,
      path: "/tickets",
      has: ["Helpdesk", '<a href="/tickets" aria-current="page">My tickets</a>', "<p>ticket list</p>"],
      not: ["Page not found", "/tickets/{id}"],
    },
    {
      name: "helpdesk detail screen gets its parameter",
      features: helpdesk,
      path: "/tickets/42",
      has: ['<a href="/tickets">My tickets</a>', "<p>ticket 42</p>"],
      not: ["aria-current", "Page not found"],
    },
    {
      name: "unknown path shows not found with the nav",
      features: helpdesk,
      path: "/nope",
      has: ['role="alert"', "Page not found", '<a href="/tickets">My tickets</a>'],
      not: ["ticket list"],
    },
    {
      name: "feature without a component shows its title",
      features: [f("reports", "/reports")],
      path: "/reports",
      has: ["<h1>Reports</h1>"],
      not: ["Page not found"],
    },
    {
      name: "no features: empty nav, not found",
      features: [],
      path: "/",
      has: ["<ul></ul>", "Page not found"],
      not: ["<li>"],
    },
  ];
  for (const c of cases) {
    it(c.name, () => {
      const html = renderToStaticMarkup(<Shell appName="Helpdesk" features={c.features} pathname={c.path} />);
      for (const s of c.has) expect(html).toContain(s);
      for (const s of c.not) expect(html).not.toContain(s);
    });
  }

  it("throws on a feature list it cannot route", () => {
    expect(() => renderToStaticMarkup(<Shell appName="x" features={[f("a", "/x"), f("b", "/x")]} pathname="/x" />)).toThrow(
      ShellError,
    );
  });
});

describe("shell imports no app code", () => {
  const sources = import.meta.glob<string>(["./*.ts", "./*.tsx", "!./*.test.ts", "!./*.test.tsx"], {
    eager: true,
    query: "?raw",
    import: "default",
  });
  it("found the shell sources", () => {
    expect(Object.keys(sources).sort()).toEqual(["./Shell.tsx", "./nav.ts"]);
  });
  for (const [file, src] of Object.entries(sources)) {
    it(file, () => {
      const imports = [...src.matchAll(/from\s+"([^"]+)"/g)].map((m) => m[1]);
      for (const spec of imports) expect(spec).toMatch(/^(react|\.\/[A-Za-z]+)$/);
    });
  }
});

function f(id: string, path: string): ShellFeature {
  return { id, title: id.charAt(0).toUpperCase() + id.slice(1), path };
}
