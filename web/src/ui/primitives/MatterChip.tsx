export interface MatterChipProps {
  // number is the firm's matter number, e.g. "2026-0041".
  number: string;
  // name is the short matter name; it is truncated, the number never is.
  name?: string;
  href?: string;
  closed?: boolean;
  className?: string;
}

// MatterChip identifies a matter wherever it appears outside its own page: a mono number and
// the short name, as a link when href is given.
export function MatterChip({ number, name, href, closed, className }: MatterChipProps) {
  const cls = ["ui-matter", closed && "ui-matter--closed", className].filter(Boolean).join(" ");
  const body = (
    <>
      <span className="ui-matter__number">{number}</span>
      {name && <span className="ui-matter__name">{name}</span>}
    </>
  );
  const label = name ? `Matter ${number}, ${name}` : `Matter ${number}`;
  if (href) {
    return (
      <a className={cls} href={href} aria-label={label} data-closed={closed || undefined}>
        {body}
      </a>
    );
  }
  return (
    <span className={cls} aria-label={label} data-closed={closed || undefined}>
      {body}
    </span>
  );
}
