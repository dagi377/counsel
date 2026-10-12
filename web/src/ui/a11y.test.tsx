// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import axe from "axe-core";
import { afterEach, describe, expect, it } from "vitest";
import { StyleGuide } from "./styleguide/StyleGuide";

afterEach(cleanup);

// The axe sweep runs over the style guide, which renders every component in every state.
// jsdom has no layout, so colour contrast is checked separately in theme/tokens.test.ts.
async function sweep(root: Element): Promise<string[]> {
  const r = await axe.run(root, {
    rules: { "color-contrast": { enabled: false }, region: { enabled: false } },
  });
  return r.violations.map((v) => `${v.id}: ${v.help}\n  ${v.nodes.map((n) => n.target.join(" ")).join("\n  ")}`);
}

describe("accessibility", () => {
  it("the style guide has no axe violations", async () => {
    const { container } = render(<StyleGuide />);
    expect(await sweep(container)).toEqual([]);
  }, 30_000);

  it.each(["loading", "error", "empty"] as const)("the table in its %s state has no axe violations", async (state) => {
    const { container } = render(<StyleGuide />);
    fireEvent.click(screen.getByRole("button", { name: state }));
    expect(container.querySelector(`[data-state="${state}"]`)).not.toBeNull();
    expect(await sweep(container)).toEqual([]);
  }, 30_000);

  it("an open dialog and an open row menu have no axe violations", async () => {
    const { container } = render(<StyleGuide />);
    fireEvent.click(screen.getByRole("button", { name: "Propose reversal" }));
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(await sweep(document.body)).toEqual([]);
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    fireEvent.click(screen.getByRole("button", { name: "Actions for Retainer received" }));
    expect(screen.getByRole("menu")).toBeTruthy();
    expect(await sweep(container)).toEqual([]);
  }, 30_000);
});
