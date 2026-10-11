import type { ComponentType } from "react";
import { buildNav, matchRoute, type ScreenProps, type ShellFeature } from "./nav";

export interface ShellProps {
  // appName is the product name shown in the header; it comes from the app.
  appName: string;
  // features is the app's registered feature list: the only source of nav, routes and screens.
  features: readonly ShellFeature[];
  // pathname is the current location path, e.g. window.location.pathname.
  pathname: string;
}

// Shell renders the app's navigation and the screen of the feature that matches pathname.
// It holds no feature of its own and imports no app code.
export function Shell({ appName, features, pathname }: ShellProps) {
  const nav = buildNav(features);
  const match = matchRoute(features, pathname);
  // Every screen receives params; one declared without props ignores them.
  const Screen = match?.feature.component as ComponentType<ScreenProps> | undefined;
  return (
    <div className="shell">
      <header>
        <span className="shell-app">{appName}</span>
      </header>
      <nav aria-label="Main">
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
      <main>
        {!match ? (
          <section role="alert">
            <h1>Page not found</h1>
            <p>No screen is registered for this address.</p>
          </section>
        ) : Screen ? (
          <Screen params={match.params} />
        ) : (
          <section>
            <h1>{match.feature.title}</h1>
          </section>
        )}
      </main>
    </div>
  );
}
