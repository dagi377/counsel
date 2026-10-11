import { useEffect, useState } from "react";

// The client portal: a guest sees the documents and bills a firm user shared on the guest's
// own matters, and nothing else. The server loaders (app/legal/comms portal pages) already
// scope every response to the guest; this page filters again, so a response that carries a
// foreign matter or an unknown kind still never renders it.

export type PortalItemKind = "document" | "bill";

// PortalItem is one shared item as the comms.portal_items page returns it.
export interface PortalItem {
  ID: string;
  MatterID: string;
  Kind: string;
  Title: string;
  SharedAt: string;
}

// PortalData is what the page renders: the guest's matter ids and the items loaded for them.
export interface PortalData {
  matters: string[];
  items: PortalItem[];
}

export type PortalLoader = () => Promise<PortalData>;

const kinds: readonly string[] = ["document", "bill"];

// visibleItems keeps only documents and bills on the guest's own matters, grouped by matter
// in the order of matters, newest first within a matter.
export function visibleItems(data: PortalData): Map<string, PortalItem[]> {
  const out = new Map<string, PortalItem[]>();
  for (const m of data.matters) out.set(m, []);
  for (const it of data.items) {
    const list = out.get(it.MatterID);
    if (!list || !kinds.includes(it.Kind)) continue;
    list.push(it);
  }
  for (const list of out.values()) {
    list.sort((a, b) => (a.SharedAt < b.SharedAt ? 1 : a.SharedAt > b.SharedAt ? -1 : 0));
  }
  return out;
}

// Portal routes, as registered by the comms portal pages under the portal mount.
export const portalBase = "/portal";
const mattersRoute = `${portalBase}/matters`;

async function getJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { credentials: "same-origin", headers: { Accept: "application/json" } });
  if (!res.ok) throw new Error(`portal: GET ${url}: ${res.status}`);
  return (await res.json()) as T;
}

// loadPortal fetches the guest's matters, then the shared items on each.
export const loadPortal: PortalLoader = async () => {
  const matters = (await getJSON<string[] | null>(mattersRoute)) ?? [];
  const lists = await Promise.all(
    matters.map((m) => getJSON<PortalItem[] | null>(`${mattersRoute}/${encodeURIComponent(m)}`)),
  );
  return { matters, items: lists.flatMap((l) => l ?? []) };
};

function kindLabel(kind: string): string {
  return kind === "bill" ? "Bill" : "Document";
}

// PortalMattersView renders loaded portal data.
export function PortalMattersView({ data }: { data: PortalData }) {
  const groups = visibleItems(data);
  if (groups.size === 0) {
    return (
      <section aria-label="Your matters">
        <h1>Your matters</h1>
        <p>Nothing has been shared with you yet.</p>
      </section>
    );
  }
  return (
    <section aria-label="Your matters">
      <h1>Your matters</h1>
      {[...groups].map(([matter, items]) => (
        <article key={matter} data-matter={matter}>
          <h2>Matter {matter}</h2>
          {items.length === 0 ? (
            <p>Nothing shared on this matter yet.</p>
          ) : (
            <ul>
              {items.map((it) => (
                <li key={it.ID} data-item={it.ID} data-kind={it.Kind}>
                  <span>{kindLabel(it.Kind)}</span> <span>{it.Title}</span>{" "}
                  <time dateTime={it.SharedAt}>{it.SharedAt.slice(0, 10)}</time>
                </li>
              ))}
            </ul>
          )}
        </article>
      ))}
    </section>
  );
}

type State = { status: "loading" } | { status: "error" } | { status: "ready"; data: PortalData };

// PortalMatters loads the guest's portal data and renders it.
export default function PortalMatters({ load = loadPortal }: { load?: PortalLoader }) {
  const [state, setState] = useState<State>({ status: "loading" });
  useEffect(() => {
    let live = true;
    load().then(
      (data) => live && setState({ status: "ready", data }),
      () => live && setState({ status: "error" }),
    );
    return () => {
      live = false;
    };
  }, [load]);
  if (state.status === "loading") return <p role="status">Loading your matters...</p>;
  if (state.status === "error") return <p role="alert">Your matters could not be loaded. Try again later.</p>;
  return <PortalMattersView data={state.data} />;
}
