import { useState } from "react";

// Access admin: users, their roles and active break-glass grants. A role change never
// writes roles directly: it opens the core.role_change gate (PUT /users/{user_id}/roles,
// 202 Gate opened) and the user's roles stay as they are until an approver decides it.

export const BUSINESS_ZONE = "America/Toronto";

export interface AccessUser {
  id: string;
  name: string;
  roles: string[];
  etag: string;
}

export interface BreakGlassGrant {
  id: string;
  userId: string;
  capability: string;
  reason: string;
  expiresAt: string; // RFC 3339 instant
}

export interface GateOpened {
  gateId: string;
}

// AccessApi is the only way this page changes access. It has no direct role write.
export interface AccessApi {
  requestRoleChange(req: {
    userId: string;
    roles: string[];
    ifMatch: string;
    idempotencyKey: string;
  }): Promise<GateOpened>;
}

export type RoleChangeResult =
  | { ok: true; gateId: string }
  | { ok: false; error: string };

// submitRoleChange opens the role change gate. It refuses an unchanged role set and never
// touches the user's displayed roles.
export async function submitRoleChange(
  api: AccessApi,
  user: AccessUser,
  roles: string[],
  idempotencyKey: string,
): Promise<RoleChangeResult> {
  const next = [...new Set(roles)].sort();
  const cur = [...user.roles].sort();
  if (next.length === cur.length && next.every((r, i) => r === cur[i])) {
    return { ok: false, error: "No change to submit." };
  }
  try {
    const g = await api.requestRoleChange({
      userId: user.id,
      roles: next,
      ifMatch: user.etag,
      idempotencyKey,
    });
    return { ok: true, gateId: g.gateId };
  } catch {
    return { ok: false, error: "The role change could not be submitted." };
  }
}

// formatExpiry renders a break-glass expiry in the business time zone, with the time left.
export function formatExpiry(expiresAt: string, now: Date): string {
  const t = new Date(expiresAt);
  if (Number.isNaN(t.getTime())) return "Unknown expiry";
  const when = new Intl.DateTimeFormat("en-CA", {
    timeZone: BUSINESS_ZONE,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
    timeZoneName: "short",
  }).format(t);
  const mins = Math.floor((t.getTime() - now.getTime()) / 60000);
  if (mins <= 0) return `Expired ${when}`;
  const left = mins >= 60 ? `${Math.floor(mins / 60)} h ${mins % 60} min` : `${mins} min`;
  return `Expires ${when} (${left} left)`;
}

function newKey(): string {
  return globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`;
}

export interface AccessAdminProps {
  api: AccessApi;
  users: AccessUser[];
  roles: string[];
  grants: BreakGlassGrant[];
  now?: Date;
}

export function AccessAdmin({ api, users, roles, grants, now = new Date() }: AccessAdminProps) {
  return (
    <section aria-labelledby="access-admin-title">
      <h1 id="access-admin-title">Access</h1>
      <h2>Users</h2>
      <table>
        <thead>
          <tr>
            <th scope="col">User</th>
            <th scope="col">Roles</th>
            <th scope="col">Change roles</th>
          </tr>
        </thead>
        <tbody>
          {users.map((u) => (
            <UserRow key={u.id} api={api} user={u} allRoles={roles} />
          ))}
        </tbody>
      </table>
      <h2>Break-glass grants</h2>
      {grants.length === 0 ? (
        <p>No active break-glass grants.</p>
      ) : (
        <ul aria-label="Break-glass grants">
          {grants.map((g) => (
            <li key={g.id} data-grant={g.id}>
              <span>{users.find((u) => u.id === g.userId)?.name ?? g.userId}</span>{" "}
              <span>{g.capability}</span> <span>{g.reason}</span>{" "}
              <time dateTime={g.expiresAt}>{formatExpiry(g.expiresAt, now)}</time>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function UserRow({ api, user, allRoles }: { api: AccessApi; user: AccessUser; allRoles: string[] }) {
  const [picked, setPicked] = useState<string[]>(user.roles);
  const [status, setStatus] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [key] = useState(newKey);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    const r = await submitRoleChange(api, user, picked, key);
    setBusy(false);
    setStatus(r.ok ? `Submitted for approval (gate ${r.gateId}).` : r.error);
  }

  return (
    <tr>
      <th scope="row">{user.name}</th>
      <td>{user.roles.join(", ") || "None"}</td>
      <td>
        <form onSubmit={onSubmit} aria-label={`Change roles for ${user.name}`}>
          {allRoles.map((r) => (
            <label key={r}>
              <input
                type="checkbox"
                checked={picked.includes(r)}
                onChange={(e) =>
                  setPicked(e.target.checked ? [...picked, r] : picked.filter((x) => x !== r))
                }
              />
              {r}
            </label>
          ))}
          <button type="submit" disabled={busy}>
            Request change
          </button>
          <p role="status">{status}</p>
        </form>
      </td>
    </tr>
  );
}
