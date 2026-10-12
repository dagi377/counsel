import type { ReactNode } from "react";
import type { Tone } from "./StatusPill";

export type BannerTone = Exclude<Tone, "neutral" | "agent"> | "info";

export interface BannerProps {
  tone?: BannerTone;
  title?: ReactNode;
  children?: ReactNode;
  // actions sit at the right: retry, open, dismiss.
  actions?: ReactNode;
  className?: string;
}

const GLYPH: Record<BannerTone, string> = { info: "i", gate: "◐", ok: "✓", warn: "!", bad: "✕" };

// Banner is an inline message about the page or a section. A bad banner is an alert; everything
// else is a status. Use Toast for a transient confirmation and Dialog for a decision.
export function Banner({ tone = "info", title, children, actions, className }: BannerProps) {
  return (
    <div
      className={["ui-banner", `ui-banner--${tone}`, className].filter(Boolean).join(" ")}
      role={tone === "bad" ? "alert" : "status"}
    >
      <span className="ui-banner__glyph" aria-hidden="true">
        {GLYPH[tone]}
      </span>
      <div className="ui-banner__body">
        {title && <div className="ui-banner__title">{title}</div>}
        {children && <div className="ui-banner__text">{children}</div>}
      </div>
      {actions && <div className="ui-banner__actions">{actions}</div>}
    </div>
  );
}
