import { formatCents } from "../../kernel/money";

export type MoneyContext = "trust" | "billing" | "general";

interface MoneyBase {
  // cents is the integer amount from the API (a *_cents field). Floats throw.
  cents: number | bigint;
  // size="lg" is the headline figure on a detail page or stat.
  size?: "md" | "lg";
  className?: string;
}

// In the trust context the currency code is required and a negative balance is an overdraft:
// signed, danger-coloured, labelled "Overdrawn" and announced as such. Billing and general
// amounts may omit the code; negatives there are credits and are signed but not labelled.
export type MoneyProps =
  | (MoneyBase & { context: "trust"; code: string })
  | (MoneyBase & { context?: "billing" | "general"; code?: string });

const toBig = (c: number | bigint): bigint => (typeof c === "bigint" ? c : BigInt(c));

// Money renders integer cents with tabular figures. It never rounds, truncates or hides a sign.
export function Money(props: MoneyProps) {
  const { cents, size = "md", className } = props;
  const context: MoneyContext = props.context ?? "general";
  const code = props.code;
  const text = formatCents(cents);
  const n = toBig(cents);
  const neg = n < 0n;
  const zero = n === 0n;
  const overdrawn = context === "trust" && neg;
  const cls = ["ui-money", neg && "ui-money--neg", zero && "ui-money--zero", size === "lg" && "ui-money--lg", className].filter(Boolean).join(" ");
  const label = overdrawn ? `overdrawn by ${formatCents(-n)}${code ? ` ${code}` : ""}` : undefined;
  return (
    <span className={cls} data-context={context} data-sign={neg ? "neg" : zero ? "zero" : "pos"} aria-label={label}>
      <span className="ui-money__amount">{text}</span>
      {code && <span className="ui-money__code">{code}</span>}
      {overdrawn && (
        <span className="ui-pill ui-pill--bad" data-status="overdrawn">
          <span className="ui-pill__glyph" aria-hidden="true">
            {"✕"}
          </span>
          Overdrawn
        </span>
      )}
    </span>
  );
}
