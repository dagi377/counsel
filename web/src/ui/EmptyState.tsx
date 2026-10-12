import type { ReactNode } from "react";

export type EmptyKind = "empty" | "error" | "forbidden" | "not-found";

export interface EmptyStateProps {
  kind?: EmptyKind;
  title: ReactNode;
  // hint says what to do next; keep it to one sentence.
  hint?: ReactNode;
  // detail is a machine handle (an address, a problem code) shown in mono.
  detail?: string;
  // action is usually a Button (Retry, New matter) or a link (Go to home).
  action?: ReactNode;
  className?: string;
}

// EmptyState fills a region that has nothing to show: no rows, a load error, no access or an
// unknown address. error and forbidden are alerts; the others are plain regions.
export function EmptyState({ kind = "empty", title, hint, detail, action, className }: EmptyStateProps) {
  const alert = kind === "error" || kind === "forbidden";
  return (
    <div
      className={["ui-empty", kind !== "empty" && `ui-empty--${kind}`, className].filter(Boolean).join(" ")}
      role={alert ? "alert" : undefined}
      data-kind={kind}
    >
      <div className="ui-empty__title">{title}</div>
      {hint && <div className="ui-empty__hint">{hint}</div>}
      {detail && <code className="ui-empty__detail">{detail}</code>}
      {action && <div className="ui-empty__action">{action}</div>}
    </div>
  );
}
