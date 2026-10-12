import type { ReactNode } from "react";

export interface Fact {
  label: ReactNode;
  value: ReactNode;
  // num right-aligns and uses tabular figures: amounts, counts, rates.
  num?: boolean;
  // mono for identifiers.
  mono?: boolean;
}

export interface FactsGridProps {
  facts: readonly Fact[];
  // inline lays the facts out as label/value rows (for an aside); the default is a card grid.
  inline?: boolean;
  // emptyValue is shown when a fact's value is null, undefined or "".
  emptyValue?: ReactNode;
  className?: string;
  "aria-label"?: string;
}

// FactsGrid is a definition list of label/value pairs: the summary block on a detail page.
export function FactsGrid({ facts, inline, emptyValue = "—", className, ...rest }: FactsGridProps) {
  return (
    <dl className={["ui-facts", inline && "ui-facts--inline", className].filter(Boolean).join(" ")} {...rest}>
      {facts.map((f, i) => {
        const empty = f.value === null || f.value === undefined || f.value === "";
        return (
          <div className="ui-facts__item" key={i}>
            <dt>{f.label}</dt>
            <dd className={[f.num && "ui-num", f.mono && "ui-mono", empty && "ui-muted"].filter(Boolean).join(" ") || undefined}>
              {empty ? emptyValue : f.value}
            </dd>
          </div>
        );
      })}
    </dl>
  );
}
