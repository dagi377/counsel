// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import type { FetchLike } from "../kernel/http/retry";
import { App } from "./App";
import type { Feature } from "./features";

function Matters() {
  return <p>matters screen</p>;
}
function MatterDetail({ params }: { params: Record<string, string> }) {
  return <p>{`matter ${params.id}`}</p>;
}
function Guest() {
  return <p>guest page</p>;
}

const features: Feature[] = [
  { id: "trust", title: "Trust", path: "/trust", order: 2 },
  { id: "matters", title: "Matters", path: "/matters", component: Matters, order: 1 },
  { id: "matter", title: "Matter", path: "/matters/{id}", component: MatterDetail as Feature["component"] },
  { id: "portal", title: "Client portal", path: "/portal/matters", component: Guest, surface: "portal" },
];

function answering(status: number | "net", calls: string[] = []): FetchLike {
  return async (url) => {
    calls.push(url);
    if (status === "net") throw new TypeError("offline");
    return new Response("{}", { status });
  };
}

async function mount(path: string, fetch: FetchLike) {
  window.history.replaceState(null, "", path);
  await act(async () => {
    render(<App features={features} fetch={fetch} />);
  });
}

beforeEach(() => sessionStorage.clear());
afterEach(cleanup);

describe("routing", () => {
  it("renders the screen for the URL with the nav in order", async () => {
    await mount("/matters", answering(200));
    expect(screen.getByText("matters screen")).toBeTruthy();
    const links = screen.getByRole("navigation", { name: "Main" }).querySelectorAll("a");
    expect([...links].map((a) => a.textContent)).toEqual(["Matters", "Trust"]);
    expect(screen.getByRole("link", { name: "Matters" }).getAttribute("aria-current")).toBe("page");
    expect(document.title).toBe("Matters · Counsel");
    expect(screen.queryByText("Client portal")).toBeNull();
  });

  it("passes route params", async () => {
    await mount("/matters/2026-0041", answering(200));
    expect(screen.getByText("matter 2026-0041")).toBeTruthy();
  });

  it("a nav click changes screen without a reload, and back returns", async () => {
    await mount("/matters", answering(200));
    await act(async () => {
      fireEvent.click(screen.getByRole("link", { name: "Trust" }));
    });
    expect(window.location.pathname).toBe("/trust");
    expect(screen.getByText("This area is not part of this release yet.")).toBeTruthy();
    await act(async () => {
      window.history.replaceState(null, "", "/matters");
      window.dispatchEvent(new PopStateEvent("popstate"));
    });
    expect(screen.getByText("matters screen")).toBeTruthy();
  });

  it("unknown path shows not found with the path and a way home", async () => {
    await mount("/nowhere", answering(200));
    expect(screen.getByRole("heading", { name: "Page not found" })).toBeTruthy();
    expect(screen.getByText("/nowhere")).toBeTruthy();
    expect(screen.getByRole("link", { name: "Go to home" }).getAttribute("href")).toBe("/matters");
    expect(document.title).toBe("Page not found · Counsel");
  });

  it("/ leads to the first area", async () => {
    await mount("/", answering(200));
    expect(window.location.pathname).toBe("/matters");
    expect(screen.getByText("matters screen")).toBeTruthy();
  });

  it("portal pages skip the member session and chrome", async () => {
    const calls: string[] = [];
    await mount("/portal/matters", answering(401, calls));
    expect(screen.getByText("guest page")).toBeTruthy();
    expect(calls).toEqual([]);
    expect(screen.queryByRole("navigation", { name: "Main" })).toBeNull();
  });
});

describe("auth states", () => {
  it("signed in: shell with sign out", async () => {
    const calls: string[] = [];
    await mount("/matters", answering(403, calls));
    expect(calls).toEqual(["/v1/roles"]);
    const out = screen.getByRole("button", { name: "Sign out" }).closest("form");
    expect(out?.getAttribute("method")).toBe("post");
    expect(out?.getAttribute("action")).toBe("/auth/logout");
  });

  it("first visit: sign in, returning to the page", async () => {
    await mount("/trust?x=1", answering(401));
    expect(screen.getByRole("heading", { name: "Sign in to Counsel" })).toBeTruthy();
    expect(screen.queryByText(/session has ended/)).toBeNull();
    expect(screen.getByRole("link", { name: "Sign in" }).getAttribute("href")).toBe("/auth/login?return_to=%2Ftrust%3Fx%3D1");
    expect(screen.queryByRole("navigation", { name: "Main" })).toBeNull();
  });

  it("expired: says the session ended only after one was seen", async () => {
    sessionStorage.setItem("counsel.session.seen", "1");
    await mount("/matters", answering(401));
    expect(screen.getByRole("heading", { name: "Your session has ended" })).toBeTruthy();
  });

  it("probe failure: error with retry, retry recovers", async () => {
    let status: number | "net" = "net";
    const fetch: FetchLike = async (url, init) => answering(status)(url, init);
    await mount("/matters", fetch);
    expect(screen.getByRole("alert").textContent).toContain("could not check your session");
    status = 200;
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    });
    expect(screen.getByText("matters screen")).toBeTruthy();
  });

  it("checking: a status while the probe runs", async () => {
    window.history.replaceState(null, "", "/matters");
    render(<App features={features} fetch={() => new Promise(() => {})} />);
    expect(screen.getByRole("status").textContent).toContain("Checking your session");
  });
});
