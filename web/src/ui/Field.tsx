import { forwardRef, useId, type InputHTMLAttributes, type ReactNode, type TextareaHTMLAttributes } from "react";

export interface FieldProps {
  label: ReactNode;
  // hint explains the format or the consequence; it is read by screen readers via aria-describedby.
  hint?: ReactNode;
  // error replaces the hint and marks the control invalid.
  error?: ReactNode;
  required?: boolean;
  // id is forced on the child through render props so the label always targets the control.
  children: (control: FieldControlProps) => ReactNode;
  className?: string;
}

export interface FieldControlProps {
  id: string;
  "aria-describedby"?: string;
  "aria-invalid"?: true;
  "aria-required"?: true;
}

// Field wraps one control with its label, hint and error. The control is supplied as a render
// prop so Field can wire ids: <Field label="Email">{(p) => <Input {...p} />}</Field>
export function Field({ label, hint, error, required, children, className }: FieldProps) {
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;
  const describedBy = error ? errorId : hint ? hintId : undefined;
  const control: FieldControlProps = {
    id,
    "aria-describedby": describedBy,
    ...(error ? { "aria-invalid": true as const } : {}),
    ...(required ? { "aria-required": true as const } : {}),
  };
  return (
    <div className={["ui-field", className].filter(Boolean).join(" ")}>
      <label className="ui-field__label" htmlFor={id}>
        {label}
        {required && (
          <span className="ui-field__req" aria-hidden="true">
            *
          </span>
        )}
      </label>
      {children(control)}
      {error ? (
        <div className="ui-field__error" id={errorId} role="alert">
          {error}
        </div>
      ) : hint ? (
        <div className="ui-field__hint" id={hintId}>
          {hint}
        </div>
      ) : null}
    </div>
  );
}

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  // mono for identifiers (matter numbers, cheque numbers); num for right-aligned amounts.
  mono?: boolean;
  num?: boolean;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input({ mono, num, className, ...rest }, ref) {
  const cls = ["ui-input", mono && "ui-input--mono", num && "ui-input--num", className].filter(Boolean).join(" ");
  return <input ref={ref} className={cls} {...rest} />;
});

export type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement>;

export const Textarea = forwardRef<HTMLTextAreaElement, TextareaProps>(function Textarea({ className, ...rest }, ref) {
  return <textarea ref={ref} className={["ui-input", className].filter(Boolean).join(" ")} {...rest} />;
});
