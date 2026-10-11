import type { ComponentType } from "react";

// The shell knows nothing about the app. The app passes its registered feature list in; every
// nav entry, route and screen comes from that list.

// ScreenProps are handed to the screen of the matched route.
export interface ScreenProps {
  params: Record<string, string>;
}

// ShellFeature is the part of an app feature the shell reads. A path segment written as
// {name} is a parameter: "/tickets/{id}" matches "/tickets/42" with params {id: "42"}.
// A screen may ignore the params, so a component without props is accepted too.
export interface ShellFeature {
  id: string;
  title: string;
  path: string;
  component?: ComponentType<ScreenProps> | ComponentType;
}

export interface NavItem {
  id: string;
  title: string;
  path: string;
}

export interface RouteMatch {
  feature: ShellFeature;
  params: Record<string, string>;
}

export class ShellError extends Error {}

const segments = (path: string): string[] => path.split("/").filter((s) => s !== "");
const paramName = (seg: string): string | null => (/^\{([A-Za-z_][A-Za-z0-9_]*)\}$/.exec(seg)?.[1] ?? null);

// routeKey normalises parameter names away, so "/t/{id}" and "/t/{ticket}" collide.
const routeKey = (path: string): string => "/" + segments(path).map((s) => (paramName(s) ? "{}" : s)).join("/");

// validateFeatures rejects a list the shell cannot route: a path that does not start with "/"
// or two features on the same route.
export function validateFeatures(features: readonly ShellFeature[]): void {
  const seen = new Map<string, string>();
  for (const f of features) {
    if (!f.path.startsWith("/")) throw new ShellError(`feature "${f.id}": path "${f.path}" must start with "/"`);
    const key = routeKey(f.path);
    const prev = seen.get(key);
    if (prev) throw new ShellError(`feature "${f.id}": route "${f.path}" is already taken by "${prev}"`);
    seen.set(key, f.id);
  }
}

// buildNav lists the features a user can link to, in registration order. A route with a
// parameter is reachable only from another screen, so it gets no nav entry.
export function buildNav(features: readonly ShellFeature[]): NavItem[] {
  validateFeatures(features);
  return features
    .filter((f) => !segments(f.path).some((s) => paramName(s) !== null))
    .map((f) => ({ id: f.id, title: f.title, path: f.path }));
}

// matchRoute finds the feature whose path matches pathname. A literal segment beats a
// parameter, so "/tickets/new" wins over "/tickets/{id}" whatever the registration order.
export function matchRoute(features: readonly ShellFeature[], pathname: string): RouteMatch | null {
  const want = segments(pathname);
  let best: { match: RouteMatch; literals: number } | null = null;
  for (const f of features) {
    const pat = segments(f.path);
    if (pat.length !== want.length) continue;
    const params: Record<string, string> = {};
    let literals = 0;
    let ok = true;
    for (let i = 0; i < pat.length && ok; i++) {
      const name = paramName(pat[i]);
      if (name) {
        try {
          params[name] = decodeURIComponent(want[i]);
        } catch {
          ok = false;
        }
      } else if (pat[i] === want[i]) {
        literals++;
      } else {
        ok = false;
      }
    }
    if (ok && (!best || literals > best.literals)) best = { match: { feature: f, params }, literals };
  }
  return best?.match ?? null;
}
