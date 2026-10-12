import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";

export type ButtonVariant = "default" | "primary" | "danger" | "agent" | "ghost";

export interface ButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "type"> {
  variant?: ButtonVariant;
  size?: "md" | "sm";
  // busy shows a spinner, disables the button and announces aria-busy. Use it while a request runs.
  busy?: boolean;
  // solid fills a danger button; use it only for the confirm step of a destructive dialog.
  solid?: boolean;
  type?: "button" | "submit" | "reset";
  children: ReactNode;
}

// Button is the only clickable control that is not a link. One primary per view; danger for
// actions that destroy or move money; agent (teal) for "ask the agent" actions.
export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button(
  { variant = "default", size = "md", busy = false, solid = false, type = "button", className, disabled, children, ...rest },
  ref,
) {
  const cls = [
    "ui-btn",
    variant !== "default" && `ui-btn--${variant}`,
    size === "sm" && "ui-btn--sm",
    solid && "ui-btn--solid",
    className,
  ]
    .filter(Boolean)
    .join(" ");
  return (
    <button ref={ref} type={type} className={cls} disabled={disabled || busy} aria-busy={busy || undefined} {...rest}>
      {busy && <span className="ui-btn__spinner" aria-hidden="true" />}
      {children}
    </button>
  );
});
