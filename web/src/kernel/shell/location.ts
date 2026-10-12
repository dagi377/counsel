import { useEffect, useState } from "react";

// Client-side navigation without a router dependency. A click on a same-origin link to an
// app path is turned into history.pushState, and back/forward (popstate) re-renders, so
// moving between screens never reloads the page. Links the app does not own (/auth/*,
// the API, other origins, new-tab and download clicks) are left to the browser.

export interface Loc {
  pathname: string;
  search: string;
}

// external are path prefixes the server answers itself; they must be full navigations.
const external = ["/auth/", "/v1/", "/api/"];

export function ownsPath(path: string): boolean {
  return path.startsWith("/") && !external.some((p) => path === p.slice(0, -1) || path.startsWith(p));
}

const changeEvent = "counsel:navigate";

function current(): Loc {
  return { pathname: window.location.pathname, search: window.location.search };
}

// navigate moves to an app path; replace swaps the history entry instead of adding one.
export function navigate(to: string, replace = false): void {
  if (replace) window.history.replaceState(null, "", to);
  else window.history.pushState(null, "", to);
  window.dispatchEvent(new Event(changeEvent));
}

// linkTarget returns the app path a click should navigate to, or null to let the browser
// handle it.
export function linkTarget(e: MouseEvent, origin: string): string | null {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return null;
  const a = (e.target as Element | null)?.closest?.("a");
  if (!a || !a.hasAttribute("href") || a.hasAttribute("download")) return null;
  if (a.target && a.target !== "_self") return null;
  const url = new URL(a.href, origin);
  if (url.origin !== origin || !ownsPath(url.pathname)) return null;
  return url.pathname + url.search + url.hash;
}

// useLocation returns the current location and keeps it in step with link clicks and
// back/forward.
export function useLocation(): Loc {
  const [loc, setLoc] = useState<Loc>(current);
  useEffect(() => {
    const sync = () => setLoc(current());
    const onClick = (e: MouseEvent) => {
      const to = linkTarget(e, window.location.origin);
      if (to === null) return;
      e.preventDefault();
      navigate(to);
    };
    window.addEventListener("popstate", sync);
    window.addEventListener(changeEvent, sync);
    document.addEventListener("click", onClick);
    return () => {
      window.removeEventListener("popstate", sync);
      window.removeEventListener(changeEvent, sync);
      document.removeEventListener("click", onClick);
    };
  }, []);
  return loc;
}
