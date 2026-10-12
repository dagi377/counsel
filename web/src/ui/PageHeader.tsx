import { useEffect, type ReactNode } from "react";

export interface Crumb {
  label: string;
  href?: string;
}

export interface PageHeaderProps {
  title: string;
  // status sits beside the title: a StatusPill or a MatterChip.
  status?: ReactNode;
  // meta is a row of short facts under the title: owner, dates, identifiers.
  meta?: ReactNode;
  // sub is one sentence of context.
  sub?: ReactNode;
  actions?: ReactNode;
  crumbs?: readonly Crumb[];
  // appName sets document.title to "<title> · <appName>" while mounted.
  appName?: string;
  className?: string;
}

// PageHeader is the one h1 on a page. It sets the document title so the tab reads like the page.
export function PageHeader({ title, status, meta, sub, actions, crumbs, appName, className }: PageHeaderProps) {
  useEffect(() => {
    if (!appName || typeof document === "undefined") return;
    const prev = document.title;
    document.title = `${title} · ${appName}`;
    return () => {
      document.title = prev;
    };
  }, [title, appName]);
  return (
    <header className={["ui-page-header", className].filter(Boolean).join(" ")}>
      {crumbs && crumbs.length > 0 && (
        <nav aria-label="Breadcrumb">
          <ol className="ui-page-header__crumbs">
            {crumbs.map((c, i) => (
              <li key={i}>{c.href ? <a href={c.href}>{c.label}</a> : <span aria-current="page">{c.label}</span>}</li>
            ))}
          </ol>
        </nav>
      )}
      <div className="ui-page-header__row">
        <div className="ui-page-header__title">
          <h1>{title}</h1>
          {status}
        </div>
        {actions && <div className="ui-page-header__actions">{actions}</div>}
      </div>
      {meta && <div className="ui-page-header__meta">{meta}</div>}
      {sub && <p className="ui-page-header__sub">{sub}</p>}
    </header>
  );
}
