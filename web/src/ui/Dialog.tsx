import { useEffect, useId, useRef, type ReactNode } from "react";
import { createPortal } from "react-dom";

export interface DialogProps {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: ReactNode;
  children?: ReactNode;
  // footer holds the buttons: cancel on the left of the primary, destructive confirm as danger solid.
  footer?: ReactNode;
  wide?: boolean;
  // dismissible=false keeps the dialog up on Escape and scrim click: a step-up prompt or a
  // decision that must be made.
  dismissible?: boolean;
}

const FOCUSABLE = 'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])';

// Dialog is a modal for one decision. It traps focus, closes on Escape, restores focus on close
// and renders into document.body so it escapes any overflow clipping.
export function Dialog({ open, onClose, title, description, children, footer, wide, dismissible = true }: DialogProps) {
  const id = useId();
  const box = useRef<HTMLDivElement>(null);
  const opener = useRef<Element | null>(null);

  useEffect(() => {
    if (!open) return;
    opener.current = document.activeElement;
    const first = box.current?.querySelector<HTMLElement>(FOCUSABLE);
    (first ?? box.current)?.focus();
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = prevOverflow;
      (opener.current as HTMLElement | null)?.focus?.();
    };
  }, [open]);

  if (!open || typeof document === "undefined") return null;

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Escape" && dismissible) {
      e.stopPropagation();
      onClose();
      return;
    }
    if (e.key !== "Tab" || !box.current) return;
    const nodes = Array.from(box.current.querySelectorAll<HTMLElement>(FOCUSABLE));
    if (nodes.length === 0) {
      e.preventDefault();
      return;
    }
    const first = nodes[0];
    const last = nodes[nodes.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  };

  return createPortal(
    <div
      className="ui-dialog-scrim"
      onMouseDown={(e) => {
        if (dismissible && e.target === e.currentTarget) onClose();
      }}
    >
      <div
        ref={box}
        className={["ui-dialog", wide && "ui-dialog--wide"].filter(Boolean).join(" ")}
        role="dialog"
        aria-modal="true"
        aria-labelledby={`${id}-title`}
        aria-describedby={description ? `${id}-desc` : undefined}
        tabIndex={-1}
        onKeyDown={onKeyDown}
      >
        <div className="ui-dialog__head">
          <h2 id={`${id}-title`}>{title}</h2>
          {description && (
            <p className="ui-dialog__desc" id={`${id}-desc`}>
              {description}
            </p>
          )}
        </div>
        {children && <div className="ui-dialog__body">{children}</div>}
        {footer && <div className="ui-dialog__foot">{footer}</div>}
      </div>
    </div>,
    document.body,
  );
}
