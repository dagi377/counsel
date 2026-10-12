// The member session is the OIDC session cookie set by kernel/identity/login (AUTH-WIRE).
// The browser cannot read the cookie (HttpOnly), so the web app learns whether it is signed
// in by asking the API: 401 means signed out, any other answer means the cookie is live.
// Sign-in and sign-out are browser navigations to /auth/*, outside the versioned API.

import type { FetchLike } from "../http/retry";

export const loginPath = "/auth/login";
export const logoutPath = "/auth/logout";

export type SessionState =
  | { status: "checking" }
  | { status: "signed-in" }
  // reason "expired" is used when this tab saw a live session before; "none" otherwise.
  | { status: "signed-out"; reason: "none" | "expired" }
  | { status: "error"; error: unknown };

// loginHref is the sign-in URL that brings the user back to returnTo afterwards. The server
// only honours a same-site path, so anything else falls back to "/".
export function loginHref(returnTo: string): string {
  const safe = returnTo.startsWith("/") && !returnTo.startsWith("//") ? returnTo : "/";
  return `${loginPath}?return_to=${encodeURIComponent(safe)}`;
}

// SessionProbe is a cheap authenticated GET; its status tells signed in from signed out.
export interface SessionProbe {
  url: string;
  fetch: FetchLike;
}

// probeSession asks the server whether the session cookie is live. 401 is signed out;
// 2xx, 403 and 404 prove the cookie authenticated; anything else is an error to retry.
export async function probeSession(p: SessionProbe, sawSession: boolean): Promise<SessionState> {
  let res: Response;
  try {
    res = await p.fetch(p.url, { method: "GET", credentials: "include", headers: { Accept: "application/json" } });
  } catch (error) {
    return { status: "error", error };
  }
  if (res.status === 401) return { status: "signed-out", reason: sawSession ? "expired" : "none" };
  if (res.ok || res.status === 403 || res.status === 404) return { status: "signed-in" };
  return { status: "error", error: { status: res.status, problem: null } };
}

// The tab remembers that it once had a session, so a later 401 reads "Your session has
// ended" rather than greeting a first-time visitor that way. Storage can be unavailable
// (private mode, blocked site data); then every signed-out state reads as a first visit.
const seenKey = "counsel.session.seen";

export function rememberSession(): void {
  try {
    sessionStorage.setItem(seenKey, "1");
  } catch {
    // storage unavailable: nothing to remember
  }
}

export function sawSession(): boolean {
  try {
    return sessionStorage.getItem(seenKey) === "1";
  } catch {
    return false;
  }
}
