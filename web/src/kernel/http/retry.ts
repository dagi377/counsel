// withRetry wraps a fetch so a transient failure (network error, 429, 502, 503, 504) is
// retried with exponential backoff and jitter. Only requests that are safe to repeat are
// retried: GET and HEAD, or any request that carries an Idempotency-Key header. A 500 or a
// 4xx other than 429 is a real answer and is returned at once.

export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

export interface RetryOptions {
  // attempts is the total number of tries, including the first; default 3.
  attempts?: number;
  // baseMs is the first backoff; each later one doubles, capped at maxMs. Defaults 300 / 4000.
  baseMs?: number;
  maxMs?: number;
  // sleep waits ms; tests pass a fake. It must reject when signal aborts.
  sleep?: (ms: number, signal?: AbortSignal | null) => Promise<void>;
  // random returns [0, 1); tests pass a constant.
  random?: () => number;
}

const transient = new Set([429, 502, 503, 504]);

export function isTransientStatus(status: number): boolean {
  return transient.has(status);
}

function header(init: RequestInit | undefined, name: string): string | null {
  const h = init?.headers;
  if (!h) return null;
  if (h instanceof Headers) return h.get(name);
  if (Array.isArray(h)) return h.find(([k]) => k.toLowerCase() === name.toLowerCase())?.[1] ?? null;
  for (const [k, v] of Object.entries(h)) if (k.toLowerCase() === name.toLowerCase()) return v;
  return null;
}

// repeatable reports whether a request may be sent again without a second effect.
export function repeatable(init?: RequestInit): boolean {
  const method = (init?.method ?? "GET").toUpperCase();
  return method === "GET" || method === "HEAD" || header(init, "Idempotency-Key") !== null;
}

// retryAfterMs reads a Retry-After header given in whole seconds; dates are ignored.
function retryAfterMs(res: Response): number | null {
  const v = res.headers.get("Retry-After");
  if (!v || !/^\d+$/.test(v.trim())) return null;
  return Number(v.trim()) * 1000;
}

function defaultSleep(ms: number, signal?: AbortSignal | null): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(signal.reason);
    const t = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(t);
      reject(signal.reason);
    });
  });
}

// backoffMs is the wait before retry number n (0-based): full jitter over base * 2^n, capped.
export function backoffMs(n: number, base: number, max: number, random: () => number): number {
  const ceiling = Math.min(max, base * 2 ** n);
  return Math.floor(random() * ceiling);
}

export function withRetry(fetchFn: FetchLike, o: RetryOptions = {}): FetchLike {
  const attempts = Math.max(1, o.attempts ?? 3);
  const base = o.baseMs ?? 300;
  const max = o.maxMs ?? 4000;
  const sleep = o.sleep ?? defaultSleep;
  const random = o.random ?? Math.random;
  return async (input, init) => {
    const again = repeatable(init);
    for (let n = 0; ; n++) {
      const last = !again || n + 1 >= attempts;
      let res: Response;
      try {
        res = await fetchFn(input, init);
      } catch (err) {
        // An abort is the caller's choice, never retried.
        if (last || init?.signal?.aborted) throw err;
        await sleep(backoffMs(n, base, max, random), init?.signal);
        continue;
      }
      if (last || !isTransientStatus(res.status)) return res;
      await sleep(Math.min(max, retryAfterMs(res) ?? backoffMs(n, base, max, random)), init?.signal);
    }
  };
}
