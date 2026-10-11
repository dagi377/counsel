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
