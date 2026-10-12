import { describe, expect, it } from "vitest";
import { loginHref, probeSession } from "./session";

describe("probeSession", () => {
  const cases: { name: string; answer: number | "net"; saw: boolean; want: string }[] = [
    { name: "200 is signed in", answer: 200, saw: false, want: "signed-in" },
    { name: "403 still proves the cookie", answer: 403, saw: false, want: "signed-in" },
    { name: "401 on a first visit", answer: 401, saw: false, want: "signed-out:none" },
    { name: "401 after a session in this tab", answer: 401, saw: true, want: "signed-out:expired" },
    { name: "503 is an error", answer: 503, saw: false, want: "error" },
    { name: "network failure is an error", answer: "net", saw: false, want: "error" },
  ];
  for (const c of cases) {
    it(c.name, async () => {
      const seen: string[] = [];
      const got = await probeSession(
        {
          url: "/v1/roles",
          fetch: async (url, init) => {
            seen.push(`${init?.method} ${url} ${init?.credentials}`);
            if (c.answer === "net") throw new TypeError("offline");
            return new Response("{}", { status: c.answer });
          },
        },
        c.saw,
      );
      expect(seen).toEqual(["GET /v1/roles include"]);
      expect(got.status === "signed-out" ? `signed-out:${got.reason}` : got.status).toBe(c.want);
    });
  }
});

describe("loginHref", () => {
  const cases: { name: string; to: string; want: string }[] = [
    { name: "keeps the path", to: "/tickets/42?x=1", want: "/auth/login?return_to=%2Ftickets%2F42%3Fx%3D1" },
    { name: "protocol-relative is refused", to: "//evil.example", want: "/auth/login?return_to=%2F" },
    { name: "absolute URL is refused", to: "https://evil.example/", want: "/auth/login?return_to=%2F" },
  ];
  for (const c of cases) it(c.name, () => expect(loginHref(c.to)).toBe(c.want));
});
