import { describe, expect, it } from "vitest";
import { ApiError, Client, decodeSchema, enumLabel, type Bill, type Fetch } from "./client";

const bill = {
  id: "0190a0b0-0000-7000-8000-000000000001",
  version: 3,
  matter_id: "0190a0b0-0000-7000-8000-000000000002",
  client_id: "0190a0b0-0000-7000-8000-000000000003",
  status: "approved",
  period_from: "2026-09-01",
  period_to: "2026-09-30",
  fees_cents: 100000,
  taxable_disb_cents: 0,
  nontaxable_disb_cents: 0,
  hst_rate_ppm: 130000,
  hst_cents: 13000,
  discount_cents: 0,
  total_cents: 113000,
  trust_applied_cents: 0,
  balance_cents: 113000,
  created_at: "2026-10-01T09:00:00-04:00",
};

function stub(status: number, body: unknown, seen: string[] = []): Fetch {
  return async (url, init) => {
    seen.push(`${init?.method} ${url}`);
    return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  };
}

// rule: ARC-I-002
describe("TestARC_I_002_UnknownFieldsAndEnums", () => {
  const cases: { name: string; status: unknown; extra: Record<string, unknown>; want: string; label: string }[] = [
    { name: "known status", status: "approved", extra: {}, want: "approved", label: "Approved" },
    { name: "unknown status and extra field", status: "future_state", extra: { foo: 1 }, want: "other", label: "Other" },
    { name: "non-string status", status: 7, extra: {}, want: "other", label: "Other" },
  ];
  for (const c of cases) {
    it(c.name, () => {
      const got = decodeSchema<Bill>("Bill", { ...bill, ...c.extra, status: c.status });
      expect(got.status).toBe(c.want);
      expect(enumLabel(got.status)).toBe(c.label);
      for (const [k, v] of Object.entries(c.extra)) expect((got as Record<string, unknown>)[k]).toBe(v);
    });
  }

  it("getBill decodes an unknown status through the client", async () => {
    const seen: string[] = [];
    const c = new Client("https://firm.example.ca/v1", stub(200, { ...bill, foo: 1, status: "future_state" }, seen));
    const got = await c.call("getBill", { params: { bill_id: bill.id } });
    expect(seen).toEqual([`GET https://firm.example.ca/v1/bills/${bill.id}`]);
    expect(got.status).toBe("other");
    expect(enumLabel(got.status)).toBe("Other");
  });

  it("listBills decodes unknown statuses inside the page", async () => {
    const c = new Client("", stub(200, { data: [{ ...bill, status: "future_state" }, bill], next_cursor: null }));
    const got = (await c.call("listBills", { query: { limit: 2 } })) as { data: Bill[] };
    expect(got.data.map((b) => b.status)).toEqual(["other", "approved"]);
  });
});

describe("Client errors", () => {
  it("throws ApiError carrying the problem code", async () => {
    const c = new Client("", stub(404, { type: "about:blank", title: "Not found", status: 404, code: "not_found" }));
    await expect(c.call("getBill", { params: { bill_id: "x" } })).rejects.toBeInstanceOf(ApiError);
    await expect(c.call("getBill", { params: { bill_id: "x" } })).rejects.toThrow(/404 not_found/);
  });
});

describe("generated client", () => {
  it("matches api/openapi.yaml (regenerate: node tools/gen_client.mjs)", async () => {
    const gen = new URL("../../../../tools/gen_client.mjs", import.meta.url).href;
    const { generate, loadSpec, readClient } = await import(/* @vite-ignore */ gen);
    expect(readClient()).toBe(generate(loadSpec()));
  });
});
