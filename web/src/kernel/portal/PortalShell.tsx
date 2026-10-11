import { useEffect, type ComponentType } from "react";

// The portal shell: the frame every guest page renders in. It shows only the pages an app
// registered (mirroring kernel/portal PortalPage) and never renders a page for a guest who
// is not signed in; that guest is sent to sign-in instead. The server scopes every page's
// data to the guest's grants; the shell only decides which page, if any, renders.

// portalBase is where the portal is mounted; page routes are relative to it.
export const portalBase = "/portal";
// signInPath is where a signed-out guest is sent.
export const signInPath = `${portalBase}/sign-in`;

// PortalPageDef is one page an app registers: a module-prefixed id, a route under the
// portal (for example "/docs") and the component that renders it.
export interface PortalPageDef {
  id: string;
  route: string;
  title: string;
  component: ComponentType;
}

// PortalSession is the signed-in guest, or null when nobody is signed in or it expired.
export interface PortalSession {
  guestId: string;
  expiresAt: string;
}

export class PortalPageError extends Error {}

// checkPages validates registered pages: ids and routes are unique and routes start with "/".
export function checkPages(pages: readonly PortalPageDef[]): PortalPageDef[] {
  const ids = new Set<string>();
  const routes = new Set<string>();
  for (const p of pages) {
    if (!p.id || !p.title || !p.route.startsWith("/") || p.route.startsWith(`${portalBase}/`)) {
      throw new PortalPageError(`portal page "${p.id}": needs id, title and a route relative to the portal`);
    }
    if (ids.has(p.id)) throw new PortalPageError(`duplicate portal page id "${p.id}"`);
    if (routes.has(p.route)) throw new PortalPageError(`duplicate portal page route "${p.route}"`);
    ids.add(p.id);
    routes.add(p.route);
  }
  return [...pages];
}

export type PortalView =
  | { kind: "sign-in"; to: string }
  | { kind: "page"; page: PortalPageDef }
  | { kind: "not-found" };

// signedIn reports whether session is present and not expired at now. An unreadable expiry
// counts as signed out.
export function signedIn(session: PortalSession | null, now: Date): boolean {
  if (!session || !session.guestId) return false;
  const exp = Date.parse(session.expiresAt);
  return Number.isFinite(exp) && exp > now.getTime();
}

// resolvePortal picks what the shell shows for path. A signed-out guest always goes to
// sign-in; a path no registered page owns is not found.
export function resolvePortal(
  pages: readonly PortalPageDef[],
  session: PortalSession | null,
  path: string,
  now: Date,
): PortalView {
  if (!signedIn(session, now)) {
    return { kind: "sign-in", to: `${signInPath}?next=${encodeURIComponent(path)}` };
  }
  const rel = path.startsWith(`${portalBase}/`) ? path.slice(portalBase.length) : null;
  const page = rel === null ? undefined : pages.find((p) => p.route === rel);
  return page ? { kind: "page", page } : { kind: "not-found" };
}

export interface PortalShellProps {
  pages: readonly PortalPageDef[];
  session: PortalSession | null;
  path: string;
  now?: Date;
  // navigate replaces the location; the default assigns window.location.
  navigate?: (to: string) => void;
}

function defaultNavigate(to: string) {
  window.location.replace(to);
}

export default function PortalShell({ pages, session, path, now, navigate = defaultNavigate }: PortalShellProps) {
  const checked = checkPages(pages);
  const view = resolvePortal(checked, session, path, now ?? new Date());
  const redirect = view.kind === "sign-in" ? view.to : null;
  useEffect(() => {
    if (redirect) navigate(redirect);
  }, [redirect, navigate]);

  if (view.kind === "sign-in") {
    return (
      <main aria-busy="true">
        <p>
          Your session has ended. <a href={view.to}>Sign in</a> to continue.
        </p>
      </main>
    );
  }
  return (
    <div className="portal-shell">
      <nav aria-label="Portal">
        <ul>
          {checked.map((p) => (
            <li key={p.id}>
              <a href={`${portalBase}${p.route}`} aria-current={view.kind === "page" && view.page.id === p.id ? "page" : undefined}>
                {p.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>
      <main>
        {view.kind === "page" ? (
          <view.page.component />
        ) : (
          <section role="alert">
            <h1>Page not found</h1>
            <p>This page is not available in the portal.</p>
          </section>
        )}
      </main>
    </div>
  );
}
