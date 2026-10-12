import type { CSSProperties } from "react";

export interface SkeletonProps {
  width?: number | string;
  height?: number | string;
  // lines renders a stack of text-height bars; the last one is shorter.
  lines?: number;
  className?: string;
}

// Skeleton is a loading placeholder for one block or a few lines. Wrap the loading region in
// an element with role="status" and a visually hidden "Loading…" (see Loading below).
export function Skeleton({ width = "100%", height = 14, lines, className }: SkeletonProps) {
  if (lines && lines > 1) {
    return (
      <div className={["ui-skeleton-lines", className].filter(Boolean).join(" ")} aria-hidden="true">
        {Array.from({ length: lines }, (_, i) => (
          <span key={i} className="ui-skeleton" style={{ height, width: i === lines - 1 ? "60%" : width }} />
        ))}
      </div>
    );
  }
  const style: CSSProperties = { width, height };
  return <span className={["ui-skeleton", className].filter(Boolean).join(" ")} style={style} aria-hidden="true" />;
}

export interface LoadingProps {
  // label is announced to assistive tech; the visual is the skeleton children.
  label?: string;
  children: React.ReactNode;
  className?: string;
}

// Loading announces a region as busy while showing skeleton children.
export function Loading({ label = "Loading…", children, className }: LoadingProps) {
  return (
    <div role="status" aria-live="polite" aria-busy="true" className={className}>
      <span className="ui-sr-only">{label}</span>
      {children}
    </div>
  );
}
