// Design tokens: the one source of truth for colour, type, spacing, radius and elevation.
// tokens.css is generated from this file (see tokens.test.ts, which fails when they drift),
// so edit here, then run `node web/tools/gen_tokens.mjs`.
//
// Colour roles come from prototype/index.html. Teal (accent) = the agent's work, amber (gate)
// = waiting on a person, ink = the firm's records, red (bad) = money or law gone wrong.

export type ThemeName = "light" | "dark";

export interface ColourTokens {
  bg: string; // page background
  panel: string; // cards, tables, dialogs
  soft: string; // table header, inset fields, hover rows
  ink: string; // body text
  muted: string; // secondary text, labels
  line: string; // borders and dividers
  rail: string; // left navigation background
  railInk: string; // text on the rail
  railMuted: string; // secondary text on the rail
  accent: string; // agent: text and borders
  accentSoft: string; // agent: fills
  gate: string; // waiting on a person: text and borders
  gateSoft: string; // waiting on a person: fills
  ok: string;
  okSoft: string;
  warn: string;
  warnSoft: string;
  bad: string; // danger: overdrafts, blocked, rejected
  badSoft: string;
  focus: string; // focus ring
  scrim: string; // dialog backdrop
}

export const light: ColourTokens = {
  bg: "#f4f5f4",
  panel: "#ffffff",
  soft: "#eef1ef",
  ink: "#18201f",
  muted: "#5f6b69",
  line: "#dfe3e1",
  rail: "#16201f",
  railInk: "#d7e0de",
  railMuted: "#8fa09c",
  accent: "#0d6b63",
  accentSoft: "#e1f0ee",
  gate: "#9a5500",
  gateSoft: "#fbf0de",
  ok: "#2c6a35",
  okSoft: "#e4f1e5",
  warn: "#7f5900",
  warnSoft: "#f8f0d8",
  bad: "#a72a2a",
  badSoft: "#f8e4e2",
  focus: "#0d6b63",
  scrim: "rgba(16, 22, 21, 0.45)",
};

export const dark: ColourTokens = {
  bg: "#101615",
  panel: "#161e1d",
  soft: "#1c2624",
  ink: "#e4ebe9",
  muted: "#97a6a3",
  line: "#27322f",
  rail: "#0b100f",
  railInk: "#d7e0de",
  railMuted: "#8fa09c",
  accent: "#4cc2b5",
  accentSoft: "#16302d",
  gate: "#e9ac56",
  gateSoft: "#33291a",
  ok: "#86c78d",
  okSoft: "#1b2e1d",
  warn: "#e3c060",
  warnSoft: "#332b17",
  bad: "#f08a80",
  badSoft: "#3a1f1d",
  focus: "#4cc2b5",
  scrim: "rgba(0, 0, 0, 0.6)",
};

export const themes: Record<ThemeName, ColourTokens> = { light, dark };

// Pairs that must meet WCAG AA (4.5:1 for text, 3:1 for the large-text and UI pairs marked so).
// Every foreground/background combination the kit renders is listed here; tokens.test.ts
// checks each one in both themes.
export interface ContrastPair {
  fg: keyof ColourTokens;
  bg: keyof ColourTokens;
  min: 3 | 4.5;
  why: string;
}

export const contrastPairs: ContrastPair[] = [
  { fg: "ink", bg: "bg", min: 4.5, why: "body text on the page" },
  { fg: "ink", bg: "panel", min: 4.5, why: "body text in a card" },
  { fg: "ink", bg: "soft", min: 4.5, why: "body text on a table header or hover row" },
  { fg: "muted", bg: "bg", min: 4.5, why: "secondary text on the page" },
  { fg: "muted", bg: "panel", min: 4.5, why: "labels and table headers" },
  { fg: "muted", bg: "soft", min: 4.5, why: "labels on a table header" },
  { fg: "railInk", bg: "rail", min: 4.5, why: "nav labels" },
  { fg: "railMuted", bg: "rail", min: 4.5, why: "nav secondary text" },
  { fg: "accent", bg: "panel", min: 4.5, why: "agent text and links" },
  { fg: "accent", bg: "bg", min: 4.5, why: "links on the page" },
  { fg: "accent", bg: "accentSoft", min: 4.5, why: "agent pill" },
  { fg: "gate", bg: "panel", min: 4.5, why: "gate text" },
  { fg: "gate", bg: "gateSoft", min: 4.5, why: "gate pill" },
  { fg: "ok", bg: "panel", min: 4.5, why: "ok text" },
  { fg: "ok", bg: "okSoft", min: 4.5, why: "ok pill" },
  { fg: "warn", bg: "panel", min: 4.5, why: "warning text" },
  { fg: "warn", bg: "warnSoft", min: 4.5, why: "warning pill" },
  { fg: "bad", bg: "panel", min: 4.5, why: "danger text, overdrafts" },
  { fg: "bad", bg: "bg", min: 4.5, why: "danger text on the page" },
  { fg: "bad", bg: "badSoft", min: 4.5, why: "danger pill" },
  { fg: "panel", bg: "ink", min: 4.5, why: "primary button label" },
  { fg: "panel", bg: "bad", min: 4.5, why: "danger button label" },
  { fg: "focus", bg: "panel", min: 3, why: "focus ring against a card" },
  { fg: "focus", bg: "bg", min: 3, why: "focus ring against the page" },
];

// Type scale: px sizes on a 4px-friendly leading. Body is 13px for density; 14px for prose.
export const type = {
  family: '"Geist", -apple-system, "Segoe UI", Roboto, "Helvetica Neue", sans-serif',
  mono: '"Geist Mono", ui-monospace, Menlo, Consolas, monospace',
  size: { xs: "11px", sm: "12px", md: "13px", base: "14px", lg: "16px", xl: "18px", "2xl": "22px", "3xl": "28px" },
  leading: { tight: "1.25", snug: "1.35", normal: "1.5" },
  weight: { regular: "400", medium: "500", semibold: "600", bold: "700" },
  tracking: { caps: "0.04em", tightHeading: "-0.01em" },
} as const;

// Spacing on a 4px scale.
export const space = { 1: "4px", 2: "8px", 3: "12px", 4: "16px", 5: "20px", 6: "24px", 8: "32px", 10: "40px", 12: "48px" } as const;

export const radius = { sm: "4px", md: "6px", lg: "8px", xl: "10px", "2xl": "12px", pill: "999px" } as const;

// Elevation: level 0 is a border only; 1 raises a popover; 2 raises a dialog.
export const elevation = {
  1: "0 1px 2px rgba(16, 22, 21, 0.08), 0 4px 12px rgba(16, 22, 21, 0.10)",
  2: "0 20px 50px rgba(16, 22, 21, 0.28)",
} as const;

export const motion = { fast: "120ms", base: "180ms", ease: "cubic-bezier(0.2, 0, 0, 1)" } as const;

export const layout = { rail: "232px", pageMax: "1240px", asideMin: "280px" } as const;

const kebab = (s: string): string => s.replace(/[A-Z]/g, (c) => "-" + c.toLowerCase());

function colourBlock(t: ColourTokens): string {
  return (Object.keys(t) as (keyof ColourTokens)[]).map((k) => `  --c-${kebab(k)}: ${t[k]};`).join("\n");
}

function scaleBlock(prefix: string, o: Record<string, string>): string {
  return Object.entries(o)
    .map(([k, v]) => `  --${prefix}-${k}: ${v};`)
    .join("\n");
}

// renderTokensCss produces the full contents of tokens.css. Light is the default; dark applies
// when the OS prefers it (unless the page forces light) or when the page forces dark.
export function renderTokensCss(): string {
  const shared = [
    `  --font: ${type.family};`,
    `  --font-mono: ${type.mono};`,
    scaleBlock("fs", type.size),
    scaleBlock("lh", type.leading),
    scaleBlock("fw", type.weight),
    scaleBlock("tracking", type.tracking),
    scaleBlock("s", space),
    scaleBlock("r", radius),
    scaleBlock("shadow", elevation),
    scaleBlock("motion", motion),
    scaleBlock("layout", layout),
  ].join("\n");
  return [
    "/* GENERATED from web/src/ui/theme/tokens.ts by web/tools/gen_tokens.mjs. Do not edit. */",
    ":root {",
    shared,
    "  color-scheme: light;",
    colourBlock(light),
    "}",
    "@media (prefers-color-scheme: dark) {",
    '  :root:not([data-theme="light"]) {',
    "    color-scheme: dark;",
    colourBlock(dark).replace(/^ {2}/gm, "    "),
    "  }",
    "}",
    ':root[data-theme="dark"] {',
    "  color-scheme: dark;",
    colourBlock(dark),
    "}",
    "",
  ].join("\n");
}
