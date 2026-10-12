// describeError turns any failure from an API call into copy a user can act on. It reads
// the HTTP status and the problem code (RFC 9457 + code) by shape, so the kernel needs no
// import of the generated client.

export interface ErrorCopy {
  // title is a short headline, message says what to do next.
  title: string;
  message: string;
  // retryable is true when trying again can help (network, 429, 5xx).
  retryable: boolean;
  // kind sorts the failure for screens that branch on it.
  kind: "offline" | "unauthenticated" | "forbidden" | "not-found" | "conflict" | "rate-limited" | "server" | "invalid" | "unknown";
}

interface HttpLike {
  status?: unknown;
  problem?: { code?: unknown; title?: unknown } | null;
}

export function describeError(err: unknown): ErrorCopy {
  const e = (err ?? {}) as HttpLike;
  const status = typeof e.status === "number" ? e.status : null;
  if (status === null) {
    if (err instanceof TypeError) {
      return { kind: "offline", title: "Counsel could not reach the server", message: "Check your connection, then try again.", retryable: true };
    }
    return { kind: "unknown", title: "Something went wrong", message: "Try again. If it keeps happening, contact your firm administrator.", retryable: true };
  }
  const code = typeof e.problem?.code === "string" ? e.problem.code : "";
  if (status === 401) {
    return { kind: "unauthenticated", title: "You are signed out", message: "Sign in again to continue.", retryable: false };
  }
  if (status === 403) {
    return { kind: "forbidden", title: "You do not have access", message: "Ask a firm administrator if you need this.", retryable: false };
  }
  if (status === 404) {
    return { kind: "not-found", title: "Not found", message: "It may have been removed, or you may not have access to it.", retryable: false };
  }
  if (status === 409 || status === 412) {
    return { kind: "conflict", title: "Someone else changed this", message: "Reload to see the latest version, then try again.", retryable: false };
  }
  if (status === 429) {
    return { kind: "rate-limited", title: "Too many requests", message: "Wait a moment, then try again.", retryable: true };
  }
  if (status === 400 || status === 422) {
    return { kind: "invalid", title: "The request was not accepted", message: code ? `The server said: ${code}.` : "Check the form and try again.", retryable: false };
  }
  if (status >= 500) {
    return { kind: "server", title: "Counsel had a problem", message: "This is not your fault. Try again in a moment.", retryable: true };
  }
  return { kind: "unknown", title: "Something went wrong", message: `The server answered ${status}.`, retryable: true };
}
