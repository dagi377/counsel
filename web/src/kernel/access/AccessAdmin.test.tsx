import { describe, expect, it, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
import { AccessAdmin, formatExpiry, submitRoleChange, type AccessApi, type AccessUser } from "./AccessAdmin";

const user: AccessUser = { id: "u1", name: "Ada", roles: ["staff"], etag: "W/\"3\"" };

describe("submitRoleChange", () => {
  const cases: {
    name: string;
    roles: string[];
    fail?: boolean;
    want: { ok: boolean; gateId?: string; calls: number };
  }[] = [
    { name: "opens the role change gate", roles: ["staff", "admin"], want: { ok: true, gateId: "g-1", calls: 1 } },
    { name: "unchanged set is not submitted", roles: ["staff"], want: { ok: false, calls: 0 } },
    { name: "gate error is reported", roles: ["admin"], fail: true, want: { ok: false, calls: 1 } },
  ];
  for (const c of cases) {
    it(c.name, async () => {
      const requestRoleChange = vi.fn(async () => {
        if (c.fail) throw new Error("409");
        return { gateId: "g-1" };
      });
      const api: AccessApi = { requestRoleChange };
      const u = { ...user, roles: [...user.roles] };
      const r = await submitRoleChange(api, u, c.roles, "k-1");
      expect(r.ok).toBe(c.want.ok);
      if (r.ok) expect(r.gateId).toBe(c.want.gateId);
      expect(requestRoleChange).toHaveBeenCalledTimes(c.want.calls);
      if (c.want.calls > 0) {
        expect(requestRoleChange).toHaveBeenCalledWith({
          userId: "u1",
          roles: [...new Set(c.roles)].sort(),
          ifMatch: user.etag,
          idempotencyKey: "k-1",
        });
      }
      // Never a direct write: the user's roles are unchanged until the gate is decided.
      expect(u.roles).toEqual(["staff"]);
    });
  }

  it("the api exposes no direct role write", () => {
    const api: AccessApi = { requestRoleChange: vi.fn() };
    expect(Object.keys(api)).toEqual(["requestRoleChange"]);
  });
});

describe("break-glass expiry", () => {
  const now = new Date("2026-03-08T04:00:00Z");
  const cases: { name: string; at: string; want: RegExp }[] = [
    { name: "future, business zone", at: "2026-03-08T04:30:00Z", want: /^Expires 2026-03-07,? 23:30 EST \(30 min left\)$/ },
    { name: "hours left", at: "2026-03-08T06:15:00Z", want: /\(2 h 15 min left\)$/ },
    { name: "past", at: "2026-03-08T03:00:00Z", want: /^Expired 2026-03-07/ },
    { name: "bad value", at: "soon", want: /^Unknown expiry$/ },
  ];
  for (const c of cases) {
    it(c.name, () => {
      expect(formatExpiry(c.at, now)).toMatch(c.want);
    });
  }

  it("the page shows each grant's expiry", () => {
    const html = renderToStaticMarkup(
      <AccessAdmin
        api={{ requestRoleChange: vi.fn() }}
        users={[user]}
        roles={["staff", "admin"]}
        grants={[{ id: "b1", userId: "u1", capability: "records.read", reason: "incident 7", expiresAt: "2026-03-08T04:30:00Z" }]}
        now={now}
      />,
    );
    expect(html).toContain('dateTime="2026-03-08T04:30:00Z"');
    expect(html).toMatch(/Expires 2026-03-07,? 23:30 EST \(30 min left\)/);
    expect(html).toContain("Ada");
  });

  it("no grants shows the empty state", () => {
    const html = renderToStaticMarkup(
      <AccessAdmin api={{ requestRoleChange: vi.fn() }} users={[]} roles={[]} grants={[]} />,
    );
    expect(html).toContain("No active break-glass grants.");
  });
});
