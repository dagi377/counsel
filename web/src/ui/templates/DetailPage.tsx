import type { ReactNode } from "react";
import { PageHeader, type PageHeaderProps } from "../PageHeader";

export interface DetailPageProps {
  header: PageHeaderProps;
  // banner shows under the header: a pending gate, an overdraft, a conflict.
  banner?: ReactNode;
  // tabs is a Tabs element; the active TabPanel goes in children.
  tabs?: ReactNode;
  // children is the main column: SectionCards, tables, the tab panel.
  children: ReactNode;
  // aside is the right column: FactsGrid summary, related records, actions.
  aside?: ReactNode;
  className?: string;
}

// DetailPage: header with status, optional banner and tabs, main column plus a facts aside.
// Every record screen (matter, bill, gate, trust account) uses it.
export function DetailPage({ header, banner, tabs, children, aside, className }: DetailPageProps) {
  return (
    <div className={["ui-page ui-detail-page", className].filter(Boolean).join(" ")}>
      <PageHeader {...header} />
      {banner && <div style={{ marginBottom: "var(--s-4)" }}>{banner}</div>}
      {tabs}
      {aside ? (
        <div className="ui-detail-page__grid">
          <div className="ui-detail-page__main">{children}</div>
          <aside className="ui-detail-page__aside" aria-label="Summary">
            {aside}
          </aside>
        </div>
      ) : (
        <div className="ui-detail-page__main">{children}</div>
      )}
    </div>
  );
}
