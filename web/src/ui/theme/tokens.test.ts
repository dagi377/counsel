import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { contrastPairs, renderTokensCss, themes, type ColourTokens, type ThemeName } from "./tokens";

// WCAG 2.x relative luminance and contrast ratio. Integer-free maths is not required here:
// this is colour, not money, and the file is a test.
function channel(hex: string, i: number): number {
  const v = parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16) / 255;
  return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
}
function luminance(hex: string): number {
  return 0.2126 * channel(hex, 0) + 0.7152 * channel(hex, 1) + 0.0722 * channel(hex, 2);
}
export function contrast(fg: string, bg: string): number {
  const a = luminance(fg);
  const b = luminance(bg);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

describe("tokens", () => {
  const cases = (Object.keys(themes) as ThemeName[]).flatMap((theme) =>
    contrastPairs.map((p) => ({ theme, ...p })),
  );
  it.each(cases)("$theme: $fg on $bg >= $minTenths/10 ($why)", ({ theme, fg, bg, minTenths }) => {
    const t: ColourTokens = themes[theme];
    expect(t[fg]).toMatch(/^#[0-9a-f]{6}$/);
    expect(t[bg]).toMatch(/^#[0-9a-f]{6}$/);
    expect(contrast(t[fg], t[bg])).toBeGreaterThanOrEqual(minTenths / 10);
  });

  it("every colour role is defined in both themes", () => {
    expect(Object.keys(themes.dark).sort()).toEqual(Object.keys(themes.light).sort());
  });

  it("tokens.css is generated from tokens.ts (run: node --experimental-strip-types web/tools/gen_tokens.mjs)", () => {
    const css = readFileSync(new URL("./tokens.css", import.meta.url), "utf8");
    expect(css).toBe(renderTokensCss());
  });

  it("dark mode follows the OS unless the page forces a theme", () => {
    const css = renderTokensCss();
    expect(css).toContain('@media (prefers-color-scheme: dark) {\n  :root:not([data-theme="light"])');
    expect(css).toContain(':root[data-theme="dark"]');
    expect(css).toContain("color-scheme: dark");
  });

  it("spacing is on a 4px scale", () => {
    const css = renderTokensCss();
    for (const m of css.matchAll(/--s-\d+: (\d+)px;/g)) expect(Number(m[1]) % 4).toBe(0);
  });
});
