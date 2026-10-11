import { describe, expect, it } from "vitest";
import { collectFeatures, features, FeatureError, type FeatureModules } from "./features";

describe("collectFeatures", () => {
  const cases: { name: string; modules: FeatureModules; want?: string[]; err?: RegExp }[] = [
    { name: "empty", modules: {}, want: [] },
    {
      name: "sorted by id",
      modules: {
        "../features/b/feature.ts": { default: { id: "b", title: "B", path: "/b" } },
        "../features/a/feature.ts": { default: { id: "a", title: "A", path: "/a" } },
      },
      want: ["a", "b"],
    },
    {
      name: "missing default export",
      modules: { "../features/x/feature.ts": {} },
      err: /default export must be a Feature/,
    },
    {
      name: "missing path",
      modules: { "../features/x/feature.ts": { default: { id: "x", title: "X", path: "" } } },
      err: /id, title and path/,
    },
    {
      name: "duplicate id",
      modules: {
        "../features/a/feature.ts": { default: { id: "a", title: "A", path: "/a" } },
        "../features/a2/feature.ts": { default: { id: "a", title: "A2", path: "/a2" } },
      },
      err: /duplicate feature id "a"/,
    },
  ];
  for (const c of cases) {
    it(c.name, () => {
      if (c.err) {
        expect(() => collectFeatures(c.modules)).toThrow(FeatureError);
        expect(() => collectFeatures(c.modules)).toThrow(c.err);
        return;
      }
      expect(collectFeatures(c.modules).map((f) => f.id)).toEqual(c.want);
    });
  }
});

describe("features", () => {
  it("is discovered from web/src/features/*/feature.ts", () => {
    expect(Array.isArray(features)).toBe(true);
  });
});
