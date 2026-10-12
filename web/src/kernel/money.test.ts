import { describe, expect, it } from "vitest";
import { formatCents, formatMoney, MoneyError, overdrawn, type MoneyFormat, type MoneyText, type MoneyTextFormat } from "./money";

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

describe("formatMoney keeps the sign apart", () => {
  const cases: { name: string; cents: number | bigint; f?: MoneyTextFormat; want: MoneyText }[] = [
    {
      name: "overdrawn balance",
      cents: -45230,
      f: { code: "CAD", balance: true },
      want: { text: "-$452.30 CAD", magnitude: "$452.30 CAD", sign: -1, negative: true, label: "overdrawn by $452.30 CAD" },
    },
    {
      name: "negative amount, not a balance",
      cents: -5,
      want: { text: "-$0.05", magnitude: "$0.05", sign: -1, negative: true, label: "minus $0.05" },
    },
    { name: "zero", cents: 0, f: { balance: true }, want: { text: "$0.00", magnitude: "$0.00", sign: 0, negative: false, label: "$0.00" } },
    {
      name: "positive",
      cents: 1250000,
      want: { text: "$12,500.00", magnitude: "$12,500.00", sign: 1, negative: false, label: "$12,500.00" },
    },
    {
      name: "bigint overdraft",
      cents: -9007199254740993n,
      f: { balance: true },
      want: {
        text: "-$90,071,992,547,409.93",
        magnitude: "$90,071,992,547,409.93",
        sign: -1,
        negative: true,
        label: "overdrawn by $90,071,992,547,409.93",
      },
    },
  ];
  for (const c of cases) {
    it(c.name, () => {
      expect(formatMoney(c.cents, c.f)).toEqual(c.want);
    });
  }

  it("overdrawn", () => {
    expect([overdrawn(-1), overdrawn(0), overdrawn(1), overdrawn(-1n)]).toEqual([true, false, false, true]);
  });

  it("rejects what formatCents rejects", () => {
    expect(() => formatMoney(12.5)).toThrow(MoneyError);
  });
});
