import { ESLint } from "eslint";
import { describe, expect, it } from "vitest";

// The float ban in eslint.config.js must actually fire on TypeScript sources (npm run lint).
describe("float ban lint rule", () => {
  const cwd = new URL("../..", import.meta.url).pathname;
  const cases: { name: string; code: string; bad: boolean }[] = [
    { name: "float literal", code: "export const r = 0.13;\n", bad: true },
    { name: "division", code: "export const f = (a: number, b: number): number => a / b;\n", bad: true },
    { name: "toFixed", code: "export const s = (n: number): string => n.toFixed(2);\n", bad: true },
    { name: "Intl.NumberFormat", code: "export const f = new Intl.NumberFormat();\n", bad: true },
    { name: "integer cents", code: "export const c: number = 113000 + 13000 * 2;\n", bad: false },
  ];
  for (const c of cases) {
    it(c.name, async () => {
      const [res] = await new ESLint({ cwd }).lintText(c.code, { filePath: "src/kernel/probe.ts" });
      const banned = res.messages.filter((m) => m.ruleId === "no-restricted-syntax");
      expect(banned.length > 0).toBe(c.bad);
    });
  }
});
