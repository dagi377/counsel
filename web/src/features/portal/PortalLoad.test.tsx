// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import PortalMatters, { NotJSONError, PortalMattersView, portalLoader, type PortalData } from "./PortalMatters";

afterEach(cleanup);

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("portal API is not the page URL", () => {
  it("loads from /api/portal", async () => {
    const seen: string[] = [];
    const load = portalLoader(undefined, async (url) => {
      seen.push(url);
      return url.endsWith("/matters") ? json(["m 1"]) : json([{ ID: "d1", MatterID: "m 1", Kind: "document", Title: "t", SharedAt: "2026-10-06T00:30:00Z" }]);
    });
    const got = await load();
    expect(seen).toEqual(["/api/portal/matters", "/api/portal/matters/m%201"]);
    expect(got.items.map((i) => i.ID)).toEqual(["d1"]);
  });

  it("an HTML answer is an error, not data", async () => {
    const load = portalLoader("/api/portal", async () => new Response("<!doctype html>", { status: 200, headers: { "Content-Type": "text/html" } }));
    await expect(load()).rejects.toBeInstanceOf(NotJSONError);
  });
});

describe("portal states", () => {
  it("shared date is the Toronto day", () => {
    const data: PortalData = { matters: ["m1"], items: [{ ID: "d1", MatterID: "m1", Kind: "bill", Title: "Invoice", SharedAt: "2026-10-06T00:30:00Z" }] };
    render(<PortalMattersView data={data} />);
    expect(screen.getByText("5 Oct 2026").getAttribute("datetime")).toBe("2026-10-06T00:30:00Z");
  });

  it("error shows a retry that reloads", async () => {
    let fail = true;
    const load = async (): Promise<PortalData> => {
      if (fail) throw new TypeError("offline");
      return { matters: [], items: [] };
    };
    await act(async () => {
      render(<PortalMatters load={load} />);
    });
    expect(screen.getByRole("alert").textContent).toContain("Your matters could not be loaded");
    fail = false;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    });
    expect(screen.getByText("Nothing has been shared with you yet.")).toBeTruthy();
  });

  it("non-JSON answer has its own copy", async () => {
    await act(async () => {
      render(<PortalMatters load={() => Promise.reject(new NotJSONError("x"))} />);
    });
    expect(screen.getByRole("alert").textContent).toContain("The portal is not available");
  });
});
