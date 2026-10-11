import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import PortalShell, {
  PortalPageError,
  checkPages,
  resolvePortal,
  signInPath,
  type PortalPageDef,
  type PortalSession,
} from "./PortalShell";

const now = new Date("2026-10-11T14:00:00Z");
const session: PortalSession = { guestId: "g-1", expiresAt: "2026-10-11T15:00:00Z" };
const expired: PortalSession = { guestId: "g-1", expiresAt: "2026-10-11T13:00:00Z" };

const page = (id: string, route: string): PortalPageDef => ({
  id,
  route,
  title: `Title ${id}`,
  component: () => <p>body of {id}</p>,
});
const pages = [page("files.list", "/files"), page("invoices.list", "/invoices")];

describe("resolvePortal", () => {
  const cases: { name: string; session: PortalSession | null; path: string; want: string }[] = [
    { name: "registered page renders", session, path: "/portal/files", want: "page:files.list" },
    { name: "second registered page", session, path: "/portal/invoices", want: "page:invoices.list" },
    { name: "unregistered page not found", session, path: "/portal/admin", want: "not-found" },
    { name: "path outside the portal not found", session, path: "/files", want: "not-found" },
    { name: "portal root not found", session, path: "/portal", want: "not-found" },
    { name: "signed-out guest goes to sign-in", session: null, path: "/portal/files", want: "sign-in" },
    { name: "expired session goes to sign-in", session: expired, path: "/portal/files", want: "sign-in" },
    { name: "bad expiry goes to sign-in", session: { guestId: "g-1", expiresAt: "soon" }, path: "/portal/files", want: "sign-in" },
    { name: "no guest goes to sign-in", session: { guestId: "", expiresAt: session.expiresAt }, path: "/portal/files", want: "sign-in" },
    { name: "signed out on unknown page still goes to sign-in", session: null, path: "/portal/admin", want: "sign-in" },
  ];
  for (const c of cases) {
    it(c.name, () => {
      const v = resolvePortal(pages, c.session, c.path, now);
      expect(v.kind === "page" ? `page:${v.page.id}` : v.kind).toBe(c.want);
      if (v.kind === "sign-in") expect(v.to).toBe(`${signInPath}?next=${encodeURIComponent(c.path)}`);
    });
  }
});

describe("checkPages", () => {
  const cases: { name: string; pages: PortalPageDef[]; ok: boolean }[] = [
    { name: "unique pages", pages, ok: true },
    { name: "duplicate id", pages: [page("a", "/a"), page("a", "/b")], ok: false },
    { name: "duplicate route", pages: [page("a", "/a"), page("b", "/a")], ok: false },
    { name: "route not relative", pages: [page("a", "a")], ok: false },
    { name: "route repeats the mount", pages: [page("a", "/portal/a")], ok: false },
  ];
  for (const c of cases) {
    it(c.name, () => {
      if (c.ok) expect(checkPages(c.pages)).toHaveLength(c.pages.length);
      else expect(() => checkPages(c.pages)).toThrow(PortalPageError);
    });
  }
});

describe("PortalShell", () => {
  const cases: {
    name: string;
    session: PortalSession | null;
    path: string;
    has: string[];
    lacks: string[];
  }[] = [
    {
      name: "renders the active registered page and nav of registered pages only",
      session,
      path: "/portal/files",
      has: ["body of files.list", 'href="/portal/files"', 'href="/portal/invoices"', 'aria-current="page"'],
      lacks: ["body of invoices.list", "Page not found"],
    },
    {
      name: "unregistered page shows not found, no page body",
      session,
      path: "/portal/admin",
      has: ["Page not found"],
      lacks: ["body of", "/portal/admin"],
    },
    {
      name: "signed-out guest sees only the sign-in link",
      session: null,
      path: "/portal/files",
      has: [`href="${signInPath}?next=%2Fportal%2Ffiles"`],
      lacks: ["body of", "Title files.list", "Title invoices.list"],
    },
  ];
  for (const c of cases) {
    it(c.name, () => {
      const html = renderToStaticMarkup(
        <PortalShell pages={pages} session={c.session} path={c.path} now={now} navigate={() => {}} />,
      );
      for (const s of c.has) expect(html).toContain(s);
      for (const s of c.lacks) expect(html).not.toContain(s);
    });
  }
});
