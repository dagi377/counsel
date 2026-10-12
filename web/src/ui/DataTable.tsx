import { useState, type ReactNode } from "react";
import { EmptyState } from "./EmptyState";
import { Menu, type MenuItem } from "./Menu";
import { Skeleton } from "./Skeleton";

export type SortDir = "asc" | "desc";

export interface Sort {
  column: string;
  dir: SortDir;
}

export interface Column<T> {
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  // num right-aligns the column and uses tabular figures. Every money column is num.
  num?: boolean;
  // sortable adds a sort button to the header. The table does not sort rows itself: the owner
  // sorts (or asks the server to) and passes rows back in order.
  sortable?: boolean;
  width?: string;
  // headerLabel is the plain text of the header when header is not a string (for aria-label).
  headerLabel?: string;
}

export type TableState = "ready" | "loading" | "error" | "empty";

export interface DataTableProps<T> {
  columns: readonly Column<T>[];
  rows: readonly T[];
  rowId: (row: T) => string;
  // caption names the table for assistive tech; it is visually hidden.
  caption: string;
  // state overrides what rows imply: "loading" and "error" are explicit; "empty" is inferred
  // from rows.length === 0 when state is omitted.
  state?: TableState;
  // sort and onSortChange make sorting controlled. Without them the table keeps its own sort
  // and still reports it through onSortChange, starting from defaultSort.
  sort?: Sort | null;
  defaultSort?: Sort;
  onSortChange?: (sort: Sort) => void;
  // rowMenu gives each row an actions menu in a trailing column.
  rowMenu?: (row: T) => MenuItem[];
  rowMenuLabel?: (row: T) => string;
  onRowClick?: (row: T) => void;
  selectedId?: string;
  empty?: { title: ReactNode; hint?: ReactNode; action?: ReactNode };
  error?: { title?: ReactNode; hint?: ReactNode; detail?: string; action?: ReactNode };
  // skeletonRows is how many placeholder rows the loading state shows.
  skeletonRows?: number;
  footer?: ReactNode;
  className?: string;
}

// DataTable is the list view of the product: dense rows, uppercase headers, right-aligned
// numbers, a sort control per sortable column and one row menu. It renders its own loading,
// empty and error states inside the table frame so the page does not jump.
export function DataTable<T>({
  columns,
  rows,
  rowId,
  caption,
  state,
  sort,
  defaultSort,
  onSortChange,
  rowMenu,
  rowMenuLabel,
  onRowClick,
  selectedId,
  empty,
  error,
  skeletonRows = 5,
  footer,
  className,
}: DataTableProps<T>) {
  const [ownSort, setOwnSort] = useState<Sort | null>(defaultSort ?? null);
  const current = sort !== undefined ? sort : ownSort;
  const effective: TableState = state ?? (rows.length === 0 ? "empty" : "ready");
  const colCount = columns.length + (rowMenu ? 1 : 0);

  const toggle = (col: Column<T>) => {
    const next: Sort = current?.column === col.id && current.dir === "asc" ? { column: col.id, dir: "desc" } : { column: col.id, dir: "asc" };
    if (sort === undefined) setOwnSort(next);
    onSortChange?.(next);
  };

  return (
    <div className={["ui-table-wrap", className].filter(Boolean).join(" ")} data-state={effective}>
      <table className="ui-table" aria-busy={effective === "loading" || undefined}>
        <caption className="ui-sr-only">{caption}</caption>
        <thead>
          <tr>
            {columns.map((c) => {
              const sorted = current?.column === c.id ? current.dir : null;
              return (
                <th
                  key={c.id}
                  scope="col"
                  className={c.num ? "ui-num" : undefined}
                  style={c.width ? { width: c.width } : undefined}
                  aria-sort={sorted ? (sorted === "asc" ? "ascending" : "descending") : undefined}
                >
                  {c.sortable ? (
                    <button
                      type="button"
                      className="ui-table__sort"
                      onClick={() => toggle(c)}
                      aria-label={`Sort by ${c.headerLabel ?? (typeof c.header === "string" ? c.header : c.id)}${sorted === "asc" ? ", descending" : ", ascending"}`}
                    >
                      {c.header}
                      <span className="ui-table__sort-glyph" aria-hidden="true">
                        {sorted === "asc" ? "▲" : sorted === "desc" ? "▼" : "▴▾"}
                      </span>
                    </button>
                  ) : (
                    c.header
                  )}
                </th>
              );
            })}
            {rowMenu && (
              <th scope="col" className="ui-table__menu-cell">
                <span className="ui-sr-only">Actions</span>
              </th>
            )}
          </tr>
        </thead>
        <tbody>
          {effective === "loading" &&
            Array.from({ length: skeletonRows }, (_, i) => (
              <tr key={`sk-${i}`} aria-hidden="true">
                {columns.map((c) => (
                  <td key={c.id} className={c.num ? "ui-num" : undefined}>
                    <Skeleton width={c.num ? "60%" : i % 2 ? "70%" : "85%"} />
                  </td>
                ))}
                {rowMenu && <td />}
              </tr>
            ))}
          {effective === "loading" && (
            <tr className="ui-sr-only">
              <td colSpan={colCount} role="status">
                {"Loading…"}
              </td>
            </tr>
          )}
          {effective === "empty" && (
            <tr className="ui-table__state">
              <td colSpan={colCount}>
                <EmptyState title={empty?.title ?? "Nothing here yet"} hint={empty?.hint} action={empty?.action} />
              </td>
            </tr>
          )}
          {effective === "error" && (
            <tr className="ui-table__state">
              <td colSpan={colCount}>
                <EmptyState kind="error" title={error?.title ?? "Could not load"} hint={error?.hint} detail={error?.detail} action={error?.action} />
              </td>
            </tr>
          )}
          {effective === "ready" &&
            rows.map((r) => {
              const id = rowId(r);
              return (
                <tr
                  key={id}
                  data-row-id={id}
                  aria-selected={selectedId === undefined ? undefined : selectedId === id}
                  onClick={onRowClick ? () => onRowClick(r) : undefined}
                  style={onRowClick ? { cursor: "pointer" } : undefined}
                >
                  {columns.map((c) => (
                    <td key={c.id} className={c.num ? "ui-num" : undefined}>
                      {c.cell(r)}
                    </td>
                  ))}
                  {rowMenu && (
                    <td className="ui-table__menu-cell" onClick={(e) => e.stopPropagation()}>
                      <Menu items={rowMenu(r)} label={rowMenuLabel ? rowMenuLabel(r) : `Actions for row ${id}`} />
                    </td>
                  )}
                </tr>
              );
            })}
        </tbody>
      </table>
      {footer && <div className="ui-table__footer">{footer}</div>}
    </div>
  );
}
