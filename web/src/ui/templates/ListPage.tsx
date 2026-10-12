import type { ReactNode } from "react";
import { PageHeader, type PageHeaderProps } from "../PageHeader";

export interface ListPageProps {
  header: PageHeaderProps;
  // filters are Fields (search, Select) shown in a toolbar above the table.
  filters?: ReactNode;
  // toolbarEnd holds view toggles or a count, right-aligned in the toolbar.
  toolbarEnd?: ReactNode;
  // banner shows above the table: a Banner for a page-level problem or notice.
  banner?: ReactNode;
  // children is the DataTable (or an EmptyState when the whole page has nothing).
  children: ReactNode;
  className?: string;
}

// ListPage: header, filter toolbar, one table. Every list screen (matters, bills, deadlines)
// uses it so they line up and behave alike.
export function ListPage({ header, filters, toolbarEnd, banner, children, className }: ListPageProps) {
  return (
    <div className={["ui-page ui-list-page", className].filter(Boolean).join(" ")}>
      <PageHeader {...header} />
      {banner && <div className="ui-list-page__banner" style={{ marginBottom: "var(--s-3)" }}>{banner}</div>}
      {(filters || toolbarEnd) && (
        <div className="ui-list-page__toolbar" role="search">
          {filters}
          {toolbarEnd && <div className="ui-list-page__toolbar-end">{toolbarEnd}</div>}
        </div>
      )}
      {children}
    </div>
  );
}
