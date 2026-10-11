import type { ComponentType } from "react";

// A feature lives in web/src/features/<area>/feature.ts and default-exports a Feature.
// Discovery is by glob, so adding a feature never edits a shared file.
export interface Feature {
  id: string;
  title: string;
  path: string;
  component?: ComponentType;
}

export type FeatureModules = Record<string, { default?: Feature }>;

export class FeatureError extends Error {}

// collectFeatures validates discovered modules and returns them sorted by id.
export function collectFeatures(modules: FeatureModules): Feature[] {
  const byId = new Map<string, string>();
  const out: Feature[] = [];
  for (const [file, mod] of Object.entries(modules)) {
    const f = mod.default;
    if (!f || !f.id || !f.title || !f.path) {
      throw new FeatureError(`${file}: default export must be a Feature with id, title and path`);
    }
    const prev = byId.get(f.id);
    if (prev) {
      throw new FeatureError(`${file}: duplicate feature id "${f.id}" (also in ${prev})`);
    }
    byId.set(f.id, file);
    out.push(f);
  }
  return out.sort((a, b) => (a.id < b.id ? -1 : a.id > b.id ? 1 : 0));
}

export const features: Feature[] = collectFeatures(
  import.meta.glob<{ default?: Feature }>("../features/*/feature.ts", { eager: true }),
);
