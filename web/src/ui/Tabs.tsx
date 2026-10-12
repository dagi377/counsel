import { useId, useRef, type KeyboardEvent, type ReactNode } from "react";

export interface Tab {
  id: string;
  label: ReactNode;
  count?: number;
  // countTone="gate" makes the count amber: things waiting on a person.
  countTone?: "gate";
  disabled?: boolean;
}

export interface TabsProps {
  tabs: readonly Tab[];
  value: string;
  onChange: (id: string) => void;
  // label names the tab list, e.g. "Matter sections".
  label: string;
  // idPrefix lets a TabPanel elsewhere on the page link back to its tab.
  idPrefix?: string;
}

// Tabs is a controlled tab list with roving focus: arrow keys move and select, Home/End jump.
// Render the active content in a TabPanel with the same idPrefix and tab id.
export function Tabs({ tabs, value, onChange, label, idPrefix }: TabsProps) {
  const autoId = useId();
  const prefix = idPrefix ?? autoId;
  const list = useRef<HTMLDivElement>(null);

  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    const enabled = tabs.filter((t) => !t.disabled);
    const i = enabled.findIndex((t) => t.id === value);
    let next: Tab | undefined;
    if (e.key === "ArrowRight") next = enabled[(i + 1) % enabled.length];
    else if (e.key === "ArrowLeft") next = enabled[(i - 1 + enabled.length) % enabled.length];
    else if (e.key === "Home") next = enabled[0];
    else if (e.key === "End") next = enabled[enabled.length - 1];
    if (!next) return;
    e.preventDefault();
    onChange(next.id);
    document.getElementById(`${prefix}-tab-${next.id}`)?.focus();
  };

  return (
    <div className="ui-tabs" role="tablist" aria-label={label} ref={list} onKeyDown={onKey}>
      {tabs.map((t) => {
        const selected = t.id === value;
        return (
          <button
            key={t.id}
            type="button"
            role="tab"
            id={`${prefix}-tab-${t.id}`}
            className="ui-tab"
            aria-selected={selected}
            aria-controls={`${prefix}-panel-${t.id}`}
            tabIndex={selected ? 0 : -1}
            disabled={t.disabled}
            onClick={() => onChange(t.id)}
          >
            {t.label}
            {t.count !== undefined && (
              <span className={["ui-tab__count", t.countTone === "gate" && "ui-tab__count--gate"].filter(Boolean).join(" ")}>{t.count}</span>
            )}
          </button>
        );
      })}
    </div>
  );
}

export interface TabPanelProps {
  idPrefix: string;
  tabId: string;
  children: ReactNode;
  className?: string;
}

export function TabPanel({ idPrefix, tabId, children, className }: TabPanelProps) {
  return (
    <div role="tabpanel" id={`${idPrefix}-panel-${tabId}`} aria-labelledby={`${idPrefix}-tab-${tabId}`} tabIndex={0} className={className}>
      {children}
    </div>
  );
}
