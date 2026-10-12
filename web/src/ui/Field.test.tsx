// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Field, Input, Textarea } from "./Field";
import { Select } from "./Select";

afterEach(cleanup);

describe("Field", () => {
  it("labels the control and links the hint", () => {
    render(
      <Field label="Client name" hint="As on ID">
        {(p) => <Input {...p} />}
      </Field>,
    );
    const input = screen.getByLabelText("Client name");
    expect(input.tagName).toBe("INPUT");
    const hintId = input.getAttribute("aria-describedby");
    expect(hintId).toBeTruthy();
    expect(document.getElementById(hintId!)?.textContent).toBe("As on ID");
    expect(input.getAttribute("aria-invalid")).toBeNull();
  });

  it("error replaces the hint, marks invalid and is an alert", () => {
    render(
      <Field label="HST" hint="9 digits" error="Must be 9 digits">
        {(p) => <Input {...p} mono />}
      </Field>,
    );
    const input = screen.getByLabelText("HST");
    expect(input.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByRole("alert").textContent).toBe("Must be 9 digits");
    expect(screen.queryByText("9 digits")).toBeNull();
    expect(input.getAttribute("aria-describedby")).toBe(screen.getByRole("alert").id);
    expect(input.className).toContain("ui-input--mono");
  });

  it("required marks the control and shows a visual asterisk hidden from AT", () => {
    render(
      <Field label="Amount" required>
        {(p) => <Input {...p} num />}
      </Field>,
    );
    const input = screen.getByLabelText(/^Amount/);
    expect(input.getAttribute("aria-required")).toBe("true");
    expect(document.querySelector(".ui-field__req")?.getAttribute("aria-hidden")).toBe("true");
    expect(input.className).toContain("ui-input--num");
  });

  it("works with Textarea and Select", () => {
    render(
      <>
        <Field label="Reason">{(p) => <Textarea {...p} />}</Field>
        <Field label="Lawyer">
          {(p) => <Select {...p} placeholder="Choose" defaultValue="" options={[{ value: "lk", label: "Linda K" }, { value: "x", label: "Gone", disabled: true }]} />}
        </Field>
      </>,
    );
    expect(screen.getByLabelText("Reason").tagName).toBe("TEXTAREA");
    const sel = screen.getByLabelText("Lawyer") as HTMLSelectElement;
    expect(sel.tagName).toBe("SELECT");
    expect(sel.options).toHaveLength(3);
    expect(sel.options[0].disabled).toBe(true);
    expect(sel.options[0].textContent).toBe("Choose");
    expect(sel.options[2].disabled).toBe(true);
    expect(sel.value).toBe("");
  });
});
