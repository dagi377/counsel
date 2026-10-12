// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Button } from "./Button";

afterEach(cleanup);

describe("Button", () => {
  it.each([
    { variant: undefined, cls: "ui-btn" },
    { variant: "primary" as const, cls: "ui-btn--primary" },
    { variant: "danger" as const, cls: "ui-btn--danger" },
    { variant: "agent" as const, cls: "ui-btn--agent" },
    { variant: "ghost" as const, cls: "ui-btn--ghost" },
  ])("renders variant $variant", ({ variant, cls }) => {
    render(<Button variant={variant}>Go</Button>);
    const b = screen.getByRole("button", { name: "Go" });
    expect(b.className).toContain(cls);
    expect(b.getAttribute("type")).toBe("button");
  });

  it("fires onClick and defaults to type=button so it never submits a form by accident", () => {
    const fn = vi.fn();
    render(
      <form onSubmit={(e) => e.preventDefault()}>
        <Button onClick={fn}>Go</Button>
      </form>,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(fn).toHaveBeenCalledTimes(1);
  });

  it("busy disables, announces aria-busy and shows a spinner; disabled stays plain", () => {
    const fn = vi.fn();
    render(<Button busy onClick={fn}>Save</Button>);
    const b = screen.getByRole("button");
    expect(b.hasAttribute("disabled")).toBe(true);
    expect(b.getAttribute("aria-busy")).toBe("true");
    expect(b.querySelector(".ui-btn__spinner")).not.toBeNull();
    fireEvent.click(b);
    expect(fn).not.toHaveBeenCalled();
    cleanup();
    render(<Button disabled>Save</Button>);
    expect(screen.getByRole("button").getAttribute("aria-busy")).toBeNull();
  });

  it("small and solid add their modifiers", () => {
    render(
      <Button size="sm" variant="danger" solid>
        Confirm
      </Button>,
    );
    const c = screen.getByRole("button").className;
    expect(c).toContain("ui-btn--sm");
    expect(c).toContain("ui-btn--solid");
  });
});
