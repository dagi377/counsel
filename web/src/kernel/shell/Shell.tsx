import { useEffect, type ComponentType, type ReactNode } from "react";
import { buildNav, homePath, matchRoute, type ScreenProps, type ShellFeature } from "./nav";
import { NotAvailable, NotFound } from "./states";

export interface ShellProps {
  // appName is the product name shown in the header; it comes from the app.
  appName: string;
  // features is the app's registered feature list: the only source of nav, routes and screens.
  features: readonly ShellFeature[];
  // pathname is the current location path, e.g. window.location.pathname.
  pathname: string;
  // account is the top bar's right side (signed-in user, sign out); the app supplies it.
  account?: ReactNode;
}

// Shell renders the app's chrome, its navigation and the screen of the feature that matches
// pathname. It holds no feature of its own and imports no app code.
// TODO(ui): adopt the web/src/ui rail, top bar and page header once the design system lands.
export function Shell({ appName, features, pathname, account }: ShellProps) {
  const nav = buildNav(features);
  const match = matchRoute(features, pathname);
  const home = homePath(features);
  // Every screen receives params; one declared without props ignores them.
  const Screen = match?.feature.component as ComponentType<ScreenProps> | undefined;
  const title = match ? `${match.feature.title} · ${appName}` : `Page not found · ${appName}`;
  useEffect(() => {
    document.title = title;
  }, [title]);
  return (
    <div className="shell">
      <header className="shell-top">
        <a className="shell-app" href={home ?? "/"}>
          {appName}
        </a>
        {account ? <div className="shell-account">{account}</div> : null}
      </header>
      <nav aria-label="Main" className="shell-rail">
        <ul>
          {nav.map((n) => (
            <li key={n.id}>
              <a href={n.path} aria-current={match?.feature.id === n.id ? "page" : undefined}>
                {n.title}
              </a>
            </li>
          ))}
        </ul>
      </nav>
      <main className="shell-main">
        {!match ? <NotFound path={pathname} home={home} /> : Screen ? <Screen params={match.params} /> : <NotAvailable title={match.feature.title} />}
      </main>
    </div>
  );
}
