import { useCallback, useEffect, useState, type ComponentType } from "react";
import { describeError } from "../kernel/http/errors";
import type { FetchLike } from "../kernel/http/retry";
import { loginHref, logoutPath, probeSession, rememberSession, sawSession, type SessionState } from "../kernel/session/session";
import { navigate, useLocation } from "../kernel/shell/location";
import { homePath, matchRoute } from "../kernel/shell/nav";
import { Shell } from "../kernel/shell/Shell";
import { ErrorState, Loading, NotFound } from "../kernel/shell/states";
import { apiFetch, sessionProbeUrl } from "./api/api";
import { appFeatures, features as registered, portalFeatures, type Feature } from "./features";

export const appName = "Counsel";

// TODO(ui): the signed-out, checking and error pages use plain elements; adopt the
// web/src/ui page and button components once the design system lands.

// SignedOut is the member sign-in page. A first visit is greeted; "Your session has ended"
// is shown only when this tab had a session before.
export function SignedOut({ reason, returnTo }: { reason: "none" | "expired"; returnTo: string }) {
  return (
    <main className="signed-out">
      <h1>{reason === "expired" ? "Your session has ended" : `Sign in to ${appName}`}</h1>
      <p>{reason === "expired" ? "Sign in again to pick up where you left off." : "Use your firm account to continue."}</p>
      <a href={loginHref(returnTo)}>Sign in</a>
    </main>
  );
}

// Account is the signed-in top bar control. Sign-out is a POST, so it is a form.
export function Account() {
  return (
    <form method="post" action={logoutPath}>
      <button type="submit">Sign out</button>
    </form>
  );
}

// PortalApp renders a guest page under /portal. It never probes the member session: guest
// pages carry their own data access, and a guest is never sent to member sign-in.
// TODO(portal): mount PortalShell once the server serves guest sessions (docs/QUESTIONS.md W2).
export function PortalApp({ features, pathname }: { features: readonly Feature[]; pathname: string }) {
  const match = matchRoute(features, pathname);
  // Guest pages take no route params.
  const Page = match?.feature.component as ComponentType | undefined;
  useEffect(() => {
    document.title = `${match ? match.feature.title : "Page not found"} · ${appName}`;
  }, [match]);
  return <main className="portal">{Page ? <Page /> : <NotFound path={pathname} home={null} />}</main>;
}

export interface AppProps {
  features?: readonly Feature[];
  // fetch is used for the session probe; tests pass a stub.
  fetch?: FetchLike;
}

// App is the member web app: it checks the session, then mounts the Shell on the current URL.
export function App({ features = registered, fetch = apiFetch }: AppProps) {
  const { pathname, search } = useLocation();
  const portal = pathname === "/portal" || pathname.startsWith("/portal/");
  const members = appFeatures(features);
  const [session, setSession] = useState<SessionState>({ status: "checking" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (portal) return;
    let live = true;
    setSession({ status: "checking" });
    probeSession({ url: sessionProbeUrl, fetch }, sawSession()).then((s) => {
      if (!live) return;
      if (s.status === "signed-in") rememberSession();
      setSession(s);
    });
    return () => {
      live = false;
    };
  }, [fetch, attempt, portal]);

  // "/" leads to the first area the member can open.
  const home = homePath(members);
  useEffect(() => {
    if (!portal && session.status === "signed-in" && pathname === "/" && home && home !== "/") navigate(home, true);
  }, [portal, session.status, pathname, home]);

  const retry = useCallback(() => setAttempt((n) => n + 1), []);

  if (portal) return <PortalApp features={portalFeatures(features)} pathname={pathname} />;
  switch (session.status) {
    case "checking":
      return (
        <main>
          <Loading label="Checking your session" />
        </main>
      );
    case "signed-out":
      return <SignedOut reason={session.reason} returnTo={pathname + search} />;
    case "error":
      return (
        <main>
          <ErrorState copy={{ ...describeError(session.error), title: `${appName} could not check your session` }} onRetry={retry} />
        </main>
      );
    case "signed-in":
      return <Shell appName={appName} features={members} pathname={pathname} account={<Account />} />;
  }
}
