import { useCallback, useEffect, useState } from "react";
import { describeError } from "../../kernel/http/errors";
import { withRetry, type FetchLike } from "../../kernel/http/retry";
import { ErrorState, Loading } from "../../kernel/shell/states";
import { formatDate } from "../../kernel/time";

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

// The page lives at /portal/matters; its data comes from a separate API mount, so a dev
// server or CDN that answers every page path with index.html can never be mistaken for data.
export const portalBase = "/portal";
export const portalApiBase = "/api/portal";

// NotJSONError is a 2xx answer that is not JSON: the API is not mounted where the page
// expects it (for example the SPA fallback answered).
export class NotJSONError extends Error {}

async function getJSON<T>(fetchFn: FetchLike, url: string): Promise<T> {
  const res = await fetchFn(url, { method: "GET", credentials: "same-origin", headers: { Accept: "application/json" } });
  if (!res.ok) throw Object.assign(new Error(`portal: GET ${url}: ${res.status}`), { status: res.status, problem: null });
  const type = res.headers.get("Content-Type") ?? "";
  if (!/^application\/(problem\+)?json\b/i.test(type)) throw new NotJSONError(`portal: GET ${url}: not JSON (${type || "no content type"})`);
  return (await res.json()) as T;
}

// portalLoader fetches the guest's matters, then the shared items on each, from apiBase.
// Transient failures are retried with backoff.
export function portalLoader(apiBase = portalApiBase, fetchFn: FetchLike = withRetry((i, init) => fetch(i, init))): PortalLoader {
  const matters = `${apiBase}/matters`;
  return async () => {
    const ids = (await getJSON<string[] | null>(fetchFn, matters)) ?? [];
    const lists = await Promise.all(ids.map((m) => getJSON<PortalItem[] | null>(fetchFn, `${matters}/${encodeURIComponent(m)}`)));
    return { matters: ids, items: lists.flatMap((l) => l ?? []) };
  };
}

export const loadPortal: PortalLoader = portalLoader();

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
                  <time dateTime={it.SharedAt}>{formatDate(it.SharedAt)}</time>
                </li>
              ))}
            </ul>
          )}
        </article>
      ))}
    </section>
  );
}

type State = { status: "loading" } | { status: "error"; error: unknown } | { status: "ready"; data: PortalData };

// PortalMatters loads the guest's portal data and renders it, with a retry on failure.
// TODO(ui): table per matter, kind chips and skeleton rows once web/src/ui lands.
export default function PortalMatters({ load = loadPortal }: { load?: PortalLoader }) {
  const [state, setState] = useState<State>({ status: "loading" });
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    setState({ status: "loading" });
    load().then(
      (data) => live && setState({ status: "ready", data }),
      (error: unknown) => live && setState({ status: "error", error }),
    );
    return () => {
      live = false;
    };
  }, [load, attempt]);
  const retry = useCallback(() => setAttempt((n) => n + 1), []);
  if (state.status === "loading") return <Loading label="Loading your matters" />;
  if (state.status === "error") {
    const copy =
      state.error instanceof NotJSONError
        ? { title: "The portal is not available", message: "The portal service did not answer. Try again in a moment.", retryable: true }
        : { ...describeError(state.error), title: "Your matters could not be loaded" };
    return <ErrorState copy={copy} onRetry={retry} />;
  }
  return <PortalMattersView data={state.data} />;
}
