import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { Button, type ButtonProps } from "./Button";

export interface MenuItem {
  id: string;
  label: ReactNode;
  onSelect: () => void;
  danger?: boolean;
  disabled?: boolean;
}

export interface MenuProps {
  items: readonly MenuItem[];
  // label names the trigger for assistive tech, e.g. "Actions for invoice 1042".
  label: string;
  trigger?: ReactNode;
  triggerProps?: Partial<ButtonProps>;
}

// Menu is a button that opens a list of actions. Arrow keys move, Enter or Space selects,
// Escape closes and returns focus. The DataTable row menu is built on it.
export function Menu({ items, label, trigger = "⋯", triggerProps }: MenuProps) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const root = useRef<HTMLSpanElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const button = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;
    const first = list.current?.querySelector<HTMLButtonElement>("button:not(:disabled)");
    first?.focus();
    const onDoc = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    return () => document.removeEventListener("mousedown", onDoc);
  }, [open]);

  const close = () => {
    setOpen(false);
    button.current?.focus();
  };

  const onKey = (e: KeyboardEvent<HTMLUListElement>) => {
    const buttons = Array.from(list.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? []);
    const i = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (e.key === "Escape") {
      e.preventDefault();
      close();
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      buttons[(i + 1) % buttons.length]?.focus();
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      buttons[(i - 1 + buttons.length) % buttons.length]?.focus();
    } else if (e.key === "Home") {
      buttons[0]?.focus();
    } else if (e.key === "End") {
      buttons[buttons.length - 1]?.focus();
    } else if (e.key === "Tab") {
      setOpen(false);
    }
  };

  return (
    <span className="ui-menu-anchor" ref={root}>
      <Button
        ref={button}
        size="sm"
        variant="ghost"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen((o) => !o)}
        {...triggerProps}
      >
        {trigger}
      </Button>
      {open && (
        <ul className="ui-menu" role="menu" id={id} aria-label={label} ref={list} onKeyDown={onKey}>
          {items.map((it) => (
            <li key={it.id} role="none">
              <button
                type="button"
                role="menuitem"
                className={["ui-menu__item", it.danger && "ui-menu__item--danger"].filter(Boolean).join(" ")}
                disabled={it.disabled}
                onClick={() => {
                  close();
                  it.onSelect();
                }}
              >
                {it.label}
              </button>
            </li>
          ))}
        </ul>
      )}
    </span>
  );
}
