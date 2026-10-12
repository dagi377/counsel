import { forwardRef, type SelectHTMLAttributes } from "react";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

export interface SelectProps extends Omit<SelectHTMLAttributes<HTMLSelectElement>, "children"> {
  options: readonly SelectOption[];
  // placeholder renders a disabled first option; pair it with value="" for "nothing chosen yet".
  placeholder?: string;
}

// Select is the native element, styled. Use it inside Field so it gets a label.
export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { options, placeholder, className, ...rest },
  ref,
) {
  return (
    <span className="ui-select-wrap">
      <select ref={ref} className={["ui-select", className].filter(Boolean).join(" ")} {...rest}>
        {placeholder !== undefined && (
          <option value="" disabled>
            {placeholder}
          </option>
        )}
        {options.map((o) => (
          <option key={o.value} value={o.value} disabled={o.disabled}>
            {o.label}
          </option>
        ))}
      </select>
    </span>
  );
});
