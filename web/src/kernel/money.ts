// Money arrives from the API as integer cents (fields ending _cents). This is the one
// formatter for displaying it. It works on the decimal digits of the integer, so no
// floating-point arithmetic ever touches an amount.

export class MoneyError extends Error {}

export interface MoneyFormat {
  // currency code shown after the amount when set, e.g. "CAD".
  code?: string;
  // symbol before the digits; defaults to "$".
  symbol?: string;
}

// formatCents renders integer cents as "-$1,234.56". It throws MoneyError on a non-integer
// or a value outside the safe integer range, rather than rounding it.
export function formatCents(cents: number | bigint, f: MoneyFormat = {}): string {
  let n: bigint;
  if (typeof cents === "bigint") {
    n = cents;
  } else {
    if (!Number.isSafeInteger(cents)) throw new MoneyError(`money: cents must be a safe integer, got ${String(cents)}`);
    n = BigInt(cents);
  }
  const neg = n < 0n;
  const digits = (neg ? -n : n).toString().padStart(3, "0");
  const whole = digits.slice(0, -2).replace(/\B(?=(\d{3})+$)/g, ",");
  const frac = digits.slice(-2);
  const sym = f.symbol ?? "$";
  const out = `${neg ? "-" : ""}${sym}${whole}.${frac}`;
  return f.code ? `${out} ${f.code}` : out;
}

// MoneyText is formatCents' result with the sign kept apart from the digits, so a screen can
// never lose it: an overdraft must render signed, in the danger tone and labelled
// "Overdrawn" (BL9 s.18(3)). The design system's Money component renders this; a screen
// never re-derives the sign from the text.
export interface MoneyText {
  // text is the full signed amount, e.g. "-$452.30 CAD"; the one form for plain text.
  text: string;
  // magnitude is the unsigned amount, e.g. "$452.30 CAD", for a "(...)" or chip rendering.
  magnitude: string;
  // sign is -1, 0 or 1.
  sign: -1 | 0 | 1;
  // negative is sign === -1. On a balance it means overdrawn.
  negative: boolean;
  // label is the accessible reading: "minus $452.30", or "overdrawn by $452.30" for a balance.
  label: string;
}

export interface MoneyTextFormat extends MoneyFormat {
  // balance marks a ledger or account balance: a negative balance is an overdraft.
  balance?: boolean;
}

// formatMoney formats cents like formatCents and returns the sign separately. It throws
// MoneyError on the same inputs formatCents does.
export function formatMoney(cents: number | bigint, f: MoneyTextFormat = {}): MoneyText {
  const text = formatCents(cents, f);
  const n = typeof cents === "bigint" ? cents : BigInt(cents);
  const sign: -1 | 0 | 1 = n < 0n ? -1 : n > 0n ? 1 : 0;
  const magnitude = sign < 0 ? text.slice(1) : text;
  const label = sign < 0 ? `${f.balance ? "overdrawn by" : "minus"} ${magnitude}` : magnitude;
  return { text, magnitude, sign, negative: sign < 0, label };
}

// overdrawn reports whether a balance in cents is an overdraft.
export function overdrawn(cents: number | bigint): boolean {
  return formatMoney(cents).negative;
}
