import { withRetry, type FetchLike } from "../../kernel/http/retry";
import { Client } from "./client";

// apiBase is where the versioned API is mounted on the cell host (openapi servers: /v1).
export const apiBase = "/v1";

// apiFetch is the browser fetch with retry and backoff on transient failures.
export const apiFetch: FetchLike = withRetry((input, init) => fetch(input, init));

// api is the one client screens use.
export const api = new Client(apiBase, apiFetch);

// sessionProbeUrl is a cheap authenticated read: its status tells signed in from signed out.
// TODO(api): switch to a session endpoint once the API has one (docs/QUESTIONS.md W2).
export const sessionProbeUrl = `${apiBase}/roles`;
