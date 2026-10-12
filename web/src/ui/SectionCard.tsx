import { useId, type ReactNode } from "react";

export interface SectionCardProps {
  title?: ReactNode;
  description?: ReactNode;
  // actions sit in the header, right-aligned: small buttons or a link.
  actions?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  // flush removes body padding so a table or list meets the card edge.
  flush?: boolean;
  // tone adds a left border stripe for a card that needs a person (gate), is wrong (bad) or resolved (ok).
  tone?: "gate" | "bad" | "ok";
  // headingLevel keeps the outline honest: 2 on a page, 3 inside another section.
  headingLevel?: 2 | 3;
  id?: string;
  className?: string;
}

// SectionCard groups one topic on a page. It is a <section> labelled by its title.
export function SectionCard({ title, description, actions, children, footer, flush, tone, headingLevel = 2, id, className }: SectionCardProps) {
  const autoId = useId();
  const titleId = title ? `${id ?? autoId}-title` : undefined;
  const H = headingLevel === 3 ? "h3" : "h2";
  return (
    <section
      id={id}
      className={["ui-card", tone && `ui-card--tone-${tone}`, className].filter(Boolean).join(" ")}
      aria-labelledby={titleId}
    >
      {(title || actions) && (
        <header className="ui-card__head">
          <div className="ui-card__head-text">
            {title && <H id={titleId}>{title}</H>}
            {description && <p className="ui-card__desc">{description}</p>}
          </div>
          {actions && <div className="ui-card__actions">{actions}</div>}
        </header>
      )}
      <div className={["ui-card__body", flush && "ui-card__body--flush"].filter(Boolean).join(" ")}>{children}</div>
      {footer && <footer className="ui-card__foot">{footer}</footer>}
    </section>
  );
}
