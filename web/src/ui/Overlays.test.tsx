// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Dialog } from "./Dialog";
import { TabPanel, Tabs } from "./Tabs";
import { ToastProvider, useToast } from "./Toast";

afterEach(cleanup);

describe("Dialog", () => {
  function Host({ dismissible = true }: { dismissible?: boolean }) {
    const [open, setOpen] = useState(false);
    return (
      <>
        <button type="button" onClick={() => setOpen(true)}>
          Open
        </button>
        <Dialog open={open} onClose={() => setOpen(false)} title="Reverse entry" description="Nothing moves until approved." dismissible={dismissible} footer={<button type="button">Confirm</button>}>
          <input aria-label="Reason" />
        </Dialog>
      </>
    );
  }

  it("renders nothing when closed; when open it is a labelled modal that focuses its first control", () => {
    render(<Host />);
    expect(screen.queryByRole("dialog")).toBeNull();
    fireEvent.click(screen.getByText("Open"));
    const d = screen.getByRole("dialog", { name: "Reverse entry" });
    expect(d.getAttribute("aria-modal")).toBe("true");
    expect(d.getAttribute("aria-describedby")).toBeTruthy();
    expect(document.activeElement).toBe(screen.getByLabelText("Reason"));
    expect(document.body.style.overflow).toBe("hidden");
  });

  it("Escape closes and returns focus to the opener; focus wraps on Tab", () => {
    render(<Host />);
    const opener = screen.getByText("Open");
    opener.focus();
    fireEvent.click(opener);
    const d = screen.getByRole("dialog");
    const confirm = screen.getByText("Confirm");
    confirm.focus();
    fireEvent.keyDown(d, { key: "Tab" });
    expect(document.activeElement).toBe(screen.getByLabelText("Reason"));
    fireEvent.keyDown(d, { key: "Tab", shiftKey: true });
    expect(document.activeElement).toBe(confirm);
    fireEvent.keyDown(d, { key: "Escape" });
    expect(screen.queryByRole("dialog")).toBeNull();
    expect(document.activeElement).toBe(opener);
    expect(document.body.style.overflow).toBe("");
  });

  it("scrim click closes; dismissible=false ignores both", () => {
    render(<Host />);
    fireEvent.click(screen.getByText("Open"));
    fireEvent.mouseDown(document.querySelector(".ui-dialog-scrim")!);
    expect(screen.queryByRole("dialog")).toBeNull();
    cleanup();
    render(<Host dismissible={false} />);
    fireEvent.click(screen.getByText("Open"));
    fireEvent.mouseDown(document.querySelector(".ui-dialog-scrim")!);
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" });
    expect(screen.getByRole("dialog")).toBeTruthy();
  });
});

describe("Tabs", () => {
  function Host() {
    const [v, setV] = useState("a");
    return (
      <>
        <Tabs idPrefix="t" label="Sections" value={v} onChange={setV} tabs={[{ id: "a", label: "A", count: 2 }, { id: "b", label: "B", count: 1, countTone: "gate" }, { id: "c", label: "C", disabled: true }]} />
        <TabPanel idPrefix="t" tabId={v}>
          panel {v}
        </TabPanel>
      </>
    );
  }

  it("is a tablist with one selected tab wired to its panel", () => {
    render(<Host />);
    const list = screen.getByRole("tablist", { name: "Sections" });
    const tabs = screen.getAllByRole("tab");
    expect(tabs).toHaveLength(3);
    expect(tabs[0].getAttribute("aria-selected")).toBe("true");
    expect(tabs[0].getAttribute("tabindex")).toBe("0");
    expect(tabs[1].getAttribute("tabindex")).toBe("-1");
    const panel = screen.getByRole("tabpanel");
    expect(panel.id).toBe(tabs[0].getAttribute("aria-controls"));
    expect(panel.getAttribute("aria-labelledby")).toBe(tabs[0].id);
    expect(list).toBeTruthy();
    expect(tabs[1].querySelector(".ui-tab__count--gate")?.textContent).toBe("1");
  });

  it("click and arrow keys change the tab, skipping disabled ones", () => {
    render(<Host />);
    const list = screen.getByRole("tablist");
    fireEvent.click(screen.getByRole("tab", { name: /B/ }));
    expect(screen.getByRole("tabpanel").textContent).toBe("panel b");
    fireEvent.keyDown(list, { key: "ArrowRight" });
    expect(screen.getByRole("tabpanel").textContent).toBe("panel a"); // C is disabled, wraps to A
    fireEvent.keyDown(list, { key: "End" });
    expect(screen.getByRole("tabpanel").textContent).toBe("panel b");
    fireEvent.keyDown(list, { key: "Home" });
    expect(screen.getByRole("tabpanel").textContent).toBe("panel a");
  });
});

describe("Toast", () => {
  function Host() {
    const t = useToast();
    return (
      <>
        <button type="button" onClick={() => t.show({ text: "Saved", tone: "ok" })}>
          ok
        </button>
        <button type="button" onClick={() => t.show({ text: "Broke", tone: "bad", action: { label: "Reload", onClick: () => undefined } })}>
          bad
        </button>
      </>
    );
  }

  it("ok toasts are status and auto-dismiss; bad toasts are alerts and stay until dismissed", () => {
    vi.useFakeTimers();
    try {
      render(
        <ToastProvider>
          <Host />
        </ToastProvider>,
      );
      fireEvent.click(screen.getByText("ok"));
      const s = screen.getByRole("status");
      expect(s.textContent).toContain("Saved");
      expect(s.className).toContain("ui-toast--ok");
      fireEvent.click(screen.getByText("bad"));
      const a = screen.getByRole("alert");
      expect(a.textContent).toContain("Broke");
      expect(screen.getByRole("button", { name: "Reload" })).toBeTruthy();
      act(() => {
        vi.advanceTimersByTime(6001);
      });
      expect(screen.queryByRole("status")).toBeNull();
      expect(screen.getByRole("alert")).toBeTruthy();
      fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
      expect(screen.queryByRole("alert")).toBeNull();
    } finally {
      vi.useRealTimers();
    }
  });

  it("useToast outside a provider throws a clear error", () => {
    function Bad() {
      useToast();
      return null;
    }
    const err = vi.spyOn(console, "error").mockImplementation(() => undefined);
    expect(() => render(<Bad />)).toThrow(/ToastProvider/);
    err.mockRestore();
  });
});
