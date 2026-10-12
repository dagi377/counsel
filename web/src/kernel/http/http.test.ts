import { describe, expect, it } from "vitest";
import { describeError } from "./errors";
import { backoffMs, repeatable, withRetry, type FetchLike } from "./retry";

function scripted(steps: (number | "net")[]): { fetch: FetchLike; calls: () => number } {
  let i = 0;
  return {
    fetch: async () => {
      const s = steps[Math.min(i, steps.length - 1)];
      i++;
      if (s === "net") throw new TypeError("fetch failed");
      return new Response("{}", { status: s, headers: s === 429 ? { "Retry-After": "2" } : {} });
    },
    calls: () => i,
  };
}

describe("withRetry", () => {
  const cases: {
    name: string;
    steps: (number | "net")[];
    init?: RequestInit;
    want: number | "throws";
    calls: number;
    waits: number[];
  }[] = [
    { name: "ok first time", steps: [200], want: 200, calls: 1, waits: [] },
    { name: "503 then ok", steps: [503, 200], want: 200, calls: 2, waits: [150] },
    { name: "network error then ok", steps: ["net", 200], want: 200, calls: 2, waits: [150] },
    { name: "gives up after attempts", steps: [502, 502, 502, 502], want: 502, calls: 3, waits: [150, 300] },
    { name: "network error every time throws", steps: ["net"], want: "throws", calls: 3, waits: [150, 300] },
    { name: "429 honours Retry-After", steps: [429, 200], want: 200, calls: 2, waits: [2000] },
    { name: "500 is not transient", steps: [500, 200], want: 500, calls: 1, waits: [] },
    { name: "404 is an answer", steps: [404], want: 404, calls: 1, waits: [] },
    { name: "POST is not repeated", steps: [503, 200], init: { method: "POST" }, want: 503, calls: 1, waits: [] },
    {
      name: "POST with Idempotency-Key is repeated",
      steps: [503, 201],
      init: { method: "POST", headers: { "Idempotency-Key": "k" } },
      want: 201,
      calls: 2,
      waits: [150],
    },
  ];
  for (const c of cases) {
    it(c.name, async () => {
      const s = scripted(c.steps);
      const waits: number[] = [];
      const f = withRetry(s.fetch, { attempts: 3, baseMs: 300, maxMs: 4000, random: () => 0.5, sleep: async (ms) => void waits.push(ms) });
      if (c.want === "throws") {
        await expect(f("/v1/x", c.init)).rejects.toBeInstanceOf(TypeError);
      } else {
        expect((await f("/v1/x", c.init)).status).toBe(c.want);
      }
      expect(s.calls()).toBe(c.calls);
      expect(waits).toEqual(c.waits);
    });
  }

  it("does not retry an aborted request", async () => {
    const ac = new AbortController();
    ac.abort();
    const s = scripted(["net"]);
    const f = withRetry(s.fetch, { sleep: async () => undefined });
    await expect(f("/v1/x", { signal: ac.signal })).rejects.toBeInstanceOf(TypeError);
    expect(s.calls()).toBe(1);
  });

  it("backoff doubles and is capped", () => {
    expect([0, 1, 2, 3, 10].map((n) => backoffMs(n, 300, 4000, () => 1 - Number.EPSILON))).toEqual([299, 599, 1199, 2399, 3999]);
  });

  it("repeatable", () => {
    expect([repeatable(), repeatable({ method: "head" }), repeatable({ method: "PUT" }), repeatable({ method: "PUT", headers: [["idempotency-key", "k"]] })]).toEqual([
      true,
      true,
      false,
      true,
    ]);
  });
});

describe("describeError", () => {
  const cases: { name: string; err: unknown; kind: string; retryable: boolean }[] = [
    { name: "network", err: new TypeError("Failed to fetch"), kind: "offline", retryable: true },
    { name: "401", err: { status: 401, problem: { code: "unauthenticated" } }, kind: "unauthenticated", retryable: false },
    { name: "403", err: { status: 403, problem: null }, kind: "forbidden", retryable: false },
    { name: "404", err: { status: 404, problem: null }, kind: "not-found", retryable: false },
    { name: "412 stale If-Match", err: { status: 412, problem: { code: "precondition_failed" } }, kind: "conflict", retryable: false },
    { name: "409", err: { status: 409, problem: { code: "conflict" } }, kind: "conflict", retryable: false },
    { name: "429", err: { status: 429, problem: null }, kind: "rate-limited", retryable: true },
    { name: "422 shows code", err: { status: 422, problem: { code: "invalid_field" } }, kind: "invalid", retryable: false },
    { name: "503", err: { status: 503, problem: null }, kind: "server", retryable: true },
    { name: "plain error", err: new Error("boom"), kind: "unknown", retryable: true },
    { name: "undefined", err: undefined, kind: "unknown", retryable: true },
  ];
  for (const c of cases) {
    it(c.name, () => {
      const got = describeError(c.err);
      expect(got.kind).toBe(c.kind);
      expect(got.retryable).toBe(c.retryable);
      expect(got.title).not.toBe("");
    });
  }

  it("422 names the problem code", () => {
    expect(describeError({ status: 422, problem: { code: "invalid_field" } }).message).toContain("invalid_field");
  });
});
