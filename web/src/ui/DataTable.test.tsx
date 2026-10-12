// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { DataTable, type Column, type Sort } from "./DataTable";
import { Menu } from "./Menu";

afterEach(cleanup);

interface Row {
  id: string;
  name: string;
  cents: number;
}
const rows: Row[] = [
  { id: "a", name: "Alpha", cents: 100 },
  { id: "b", name: "Beta", cents: -250 },
];
const columns: Column<Row>[] = [
  { id: "name", header: "Name", sortable: true, cell: (r) => r.name },
  { id: "cents", header: "Amount", num: true, sortable: true, cell: (r) => String(r.cents) },
];

describe("DataTable", () => {
  it("renders a captioned table with numeric columns right-aligned", () => {
    render(<DataTable caption="Ledger" columns={columns} rows={rows} rowId={(r) => r.id} />);
    const table = screen.getByRole("table", { name: "Ledger" });
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    const amountHeader = within(table).getByRole("columnheader", { name: /Amount/ });
    expect(amountHeader.className).toContain("ui-num");
    const cell = within(table).getByText("-250");
    expect(cell.className).toContain("ui-num");
    expect(table.closest("[data-state]")?.getAttribute("data-state")).toBe("ready");
  });

  it("sorts: uncontrolled toggles asc then desc and reports through onSortChange", () => {
    const fn = vi.fn();
    render(<DataTable caption="L" columns={columns} rows={rows} rowId={(r) => r.id} onSortChange={fn} />);
    const btn = screen.getByRole("button", { name: /Sort by Name/ });
    fireEvent.click(btn);
    expect(fn).toHaveBeenLastCalledWith({ column: "name", dir: "asc" });
    expect(screen.getByRole("columnheader", { name: /Name/ }).getAttribute("aria-sort")).toBe("ascending");
    fireEvent.click(btn);
    expect(fn).toHaveBeenLastCalledWith({ column: "name", dir: "desc" });
    expect(screen.getByRole("columnheader", { name: /Name/ }).getAttribute("aria-sort")).toBe("descending");
    fireEvent.click(screen.getByRole("button", { name: /Sort by Amount/ }));
    expect(fn).toHaveBeenLastCalledWith({ column: "cents", dir: "asc" });
    expect(screen.getByRole("columnheader", { name: /Name/ }).getAttribute("aria-sort")).toBeNull();
  });

  it("sorts: controlled reflects the sort prop only", () => {
    const sort: Sort = { column: "cents", dir: "desc" };
    const fn = vi.fn();
    render(<DataTable caption="L" columns={columns} rows={rows} rowId={(r) => r.id} sort={sort} onSortChange={fn} />);
    expect(screen.getByRole("columnheader", { name: /Amount/ }).getAttribute("aria-sort")).toBe("descending");
    fireEvent.click(screen.getByRole("button", { name: /Sort by Amount/ }));
    expect(fn).toHaveBeenCalledWith({ column: "cents", dir: "asc" });
    // still desc: the owner did not update the prop
    expect(screen.getByRole("columnheader", { name: /Amount/ }).getAttribute("aria-sort")).toBe("descending");
  });

  it("loading shows skeleton rows hidden from AT and a status", () => {
    render(<DataTable caption="L" columns={columns} rows={[]} rowId={(r) => r.id} state="loading" skeletonRows={3} />);
    expect(screen.getByRole("table").getAttribute("aria-busy")).toBe("true");
    expect(document.querySelectorAll("tbody tr[aria-hidden='true']")).toHaveLength(3);
    expect(screen.getByRole("status").textContent).toContain("Loading");
    expect(document.querySelector("[data-state]")?.getAttribute("data-state")).toBe("loading");
  });

  it("empty is inferred from no rows and shows the empty copy with its action", () => {
    render(<DataTable caption="L" columns={columns} rows={[]} rowId={(r) => r.id} empty={{ title: "No entries", hint: "Add one", action: <button type="button">Add</button> }} />);
    expect(document.querySelector("[data-state]")?.getAttribute("data-state")).toBe("empty");
    expect(screen.getByText("No entries")).toBeTruthy();
    expect(screen.getByText("Add one")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("error is an alert with detail and retry; rows are not shown", () => {
    render(
      <DataTable caption="L" columns={columns} rows={rows} rowId={(r) => r.id} state="error" error={{ title: "Failed", detail: "problem: x", action: <button type="button">Retry</button> }} />,
    );
    const alert = screen.getByRole("alert");
    expect(alert.textContent).toContain("Failed");
    expect(alert.textContent).toContain("problem: x");
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
    expect(screen.queryByText("Alpha")).toBeNull();
  });

  it("row menu opens per row, selects and does not fire the row click", () => {
    const open = vi.fn();
    const rowClick = vi.fn();
    render(
      <DataTable
        caption="L"
        columns={columns}
        rows={rows}
        rowId={(r) => r.id}
        onRowClick={rowClick}
        rowMenu={(r) => [{ id: "open", label: `Open ${r.name}`, onSelect: () => open(r.id) }]}
        rowMenuLabel={(r) => `Actions for ${r.name}`}
      />,
    );
    const trigger = screen.getByRole("button", { name: "Actions for Beta" });
    expect(trigger.getAttribute("aria-haspopup")).toBe("menu");
    fireEvent.click(trigger);
    const item = screen.getByRole("menuitem", { name: "Open Beta" });
    fireEvent.click(item);
    expect(open).toHaveBeenCalledWith("b");
    expect(rowClick).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu")).toBeNull();
    fireEvent.click(screen.getByText("Alpha"));
    expect(rowClick).toHaveBeenCalledWith(rows[0]);
  });

  it("marks the selected row and renders the footer", () => {
    render(<DataTable caption="L" columns={columns} rows={rows} rowId={(r) => r.id} selectedId="a" footer={<span>2 rows</span>} />);
    expect(document.querySelector("tr[data-row-id='a']")?.getAttribute("aria-selected")).toBe("true");
    expect(document.querySelector("tr[data-row-id='b']")?.getAttribute("aria-selected")).toBe("false");
    expect(screen.getByText("2 rows")).toBeTruthy();
  });
});

describe("Menu", () => {
  it("keyboard: arrows move, Escape closes and returns focus, disabled items are skipped", () => {
    const a = vi.fn();
    render(
      <Menu
        label="More"
        items={[
          { id: "a", label: "A", onSelect: a },
          { id: "b", label: "B", onSelect: () => undefined, disabled: true },
          { id: "c", label: "C", onSelect: () => undefined, danger: true },
        ]}
      />,
    );
    const trigger = screen.getByRole("button", { name: "More" });
    fireEvent.click(trigger);
    const menu = screen.getByRole("menu");
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    const items = screen.getAllByRole("menuitem");
    expect(document.activeElement).toBe(items[0]);
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(items[2]); // B is disabled
    expect(items[2].className).toContain("ui-menu__item--danger");
    fireEvent.keyDown(menu, { key: "ArrowDown" });
    expect(document.activeElement).toBe(items[0]);
    fireEvent.keyDown(menu, { key: "Escape" });
    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });
});
