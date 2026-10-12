// Theme preference: "system" follows the OS; "light" and "dark" force one. The choice is a
// per-browser convenience kept in localStorage; nothing else reads it.
export type ThemePref = "system" | "light" | "dark";

const KEY = "counsel.theme";

export function readThemePref(): ThemePref {
  try {
    const v = globalThis.localStorage?.getItem(KEY);
    return v === "light" || v === "dark" ? v : "system";
  } catch {
    return "system";
  }
}

// applyTheme writes the data-theme attribute tokens.css keys on, and remembers the choice.
export function applyTheme(pref: ThemePref, root: HTMLElement | undefined = globalThis.document?.documentElement): void {
  if (!root) return;
  if (pref === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", pref);
  try {
    if (pref === "system") globalThis.localStorage?.removeItem(KEY);
    else globalThis.localStorage?.setItem(KEY, pref);
  } catch {
    // storage may be blocked; the attribute still applies for this page
  }
}

// initTheme applies the stored preference at boot, before React renders, to avoid a flash.
export function initTheme(): ThemePref {
  const pref = readThemePref();
  applyTheme(pref);
  return pref;
}
