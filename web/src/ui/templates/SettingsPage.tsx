import type { ReactNode } from "react";
import { PageHeader, type PageHeaderProps } from "../PageHeader";

export interface SettingsSection {
  id: string;
  label: string;
}

export interface SettingsPageProps {
  header: PageHeaderProps;
  // sections drive the sticky left nav; each id must match a SectionCard id in children.
  sections: readonly SettingsSection[];
  // current is the section id in view (the owner tracks the hash or scroll position).
  current?: string;
  children: ReactNode;
  className?: string;
}

// SettingsPage: header, a sticky section nav on the left and a column of SectionCards. Firm
// settings, tax registrations, access and integrations use it.
export function SettingsPage({ header, sections, current, children, className }: SettingsPageProps) {
  return (
    <div className={["ui-page ui-settings-page", className].filter(Boolean).join(" ")}>
      <PageHeader {...header} />
      <div className="ui-settings-page__grid">
        <nav aria-label="Settings sections">
          <ul className="ui-settings-page__nav">
            {sections.map((s) => (
              <li key={s.id}>
                <a href={`#${s.id}`} aria-current={current === s.id ? "true" : undefined}>
                  {s.label}
                </a>
              </li>
            ))}
          </ul>
        </nav>
        <div className="ui-settings-page__sections">{children}</div>
      </div>
    </div>
  );
}
