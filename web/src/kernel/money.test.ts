import { describe, expect, it } from "vitest";
import { formatCents, MoneyError, type MoneyFormat } from "./money";

describe("formatCents", () => {
  const cases: { name: string; cents: number | bigint; f?: MoneyFormat; want?: string; err?: boolean }[] = [
    { name: "zero", cents: 0, want: "$0.00" },
    { name: "one cent", cents: 1, want: "$0.01" },
    { name: "ten cents", cents: 10, want: "$0.10" },
    { name: "one dollar", cents: 100, want: "$1.00" },
    { name: "thousands", cents: 123456, want: "$1,234.56" },
    { name: "millions", cents: 100000000, want: "$1,000,000.00" },
    { name: "negative", cents: -1234, want: "-$12.34" },
    { name: "negative cent", cents: -5, want: "-$0.05" },
    { name: "with code", cents: 113000, f: { code: "CAD" }, want: "$1,130.00 CAD" },
    { name: "max cents field", cents: 100000000000, want: "$1,000,000,000.00" },
    { name: "bigint past safe range", cents: 9007199254740993n, want: "$90,071,992,547,409.93" },
    { name: "fraction rejected", cents: 12.5, err: true },
    { name: "NaN rejected", cents: Number.NaN, err: true },
    { name: "unsafe integer rejected", cents: 2 ** 53, err: true },
  ];
  for (const c of cases) {
    it(c.name, () => {
      if (c.err) {
        expect(() => formatCents(c.cents, c.f)).toThrow(MoneyError);
        return;
      }
      expect(formatCents(c.cents, c.f)).toBe(c.want);
    });
  }
});
