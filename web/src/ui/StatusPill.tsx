import type { HTMLAttributes } from "react";

// Tone is the colour family. Every tone also has a glyph, so status is never colour alone.
export type Tone = "neutral" | "ok" | "warn" | "bad" | "gate" | "agent";

// STATUS is the fixed vocabulary, ported from prototype/core.js. Screens pass a key; the pill
// supplies the label and the tone. Add a key here before using it anywhere.
export const STATUS = {
  // waiting on a person (amber)
  pending: ["Needs approval", "gate"],
  "needs-approval": ["Needs approval", "gate"],
  "needs-you": ["Needs you", "gate"],
  "to-review": ["To review", "gate"],
  waiting: ["Waiting on approval", "gate"],
  // the agent (teal)
  running: ["Agent working", "agent"],
  "agent-prepared": ["Agent prepared", "agent"],
  suggested: ["Agent suggested", "agent"],
  // neutral
  draft: ["Draft", "neutral"],
  new: ["New", "neutral"],
  open: ["Open", "neutral"],
  todo: ["To do", "neutral"],
  scheduled: ["Scheduled", "neutral"],
  sent: ["Sent", "neutral"],
  superseded: ["Superseded", "neutral"],
  reassigned: ["Reassigned", "neutral"],
  stopped: ["Stopped", "neutral"],
  closed: ["Closed", "neutral"],
  archived: ["Archived", "neutral"],
  dismissed: ["Dismissed", "neutral"],
  declined: ["Declined", "neutral"],
  inactive: ["Inactive", "neutral"],
  withdrawn: ["Withdrawn", "neutral"],
  "low-risk": ["Low risk", "neutral"],
  // good
  approved: ["Approved", "ok"],
  done: ["Done", "ok"],
  complete: ["Complete", "ok"],
  paid: ["Paid", "ok"],
  reconciled: ["Reconciled", "ok"],
  confirmed: ["Confirmed", "ok"],
  active: ["Active", "ok"],
  filed: ["Filed", "ok"],
  // attention
  "in-progress": ["In progress", "warn"],
  "in-review": ["In review", "warn"],
  returned: ["Sent back", "warn"],
  unconfirmed: ["Unconfirmed", "warn"],
  "due-soon": ["Due soon", "warn"],
  partial: ["Partly paid", "warn"],
  hold: ["On hold", "warn"],
  "medium-risk": ["Medium risk", "warn"],
  // wrong
  overdue: ["Overdue", "bad"],
  overdrawn: ["Overdrawn", "bad"],
  blocked: ["Blocked", "bad"],
  failed: ["Failed", "bad"],
  rejected: ["Rejected", "bad"],
  conflict: ["Conflict", "bad"],
  unbalanced: ["Out of balance", "bad"],
  expired: ["Expired", "bad"],
  stale: ["Stale: source changed", "bad"],
  escalated: ["Escalated", "bad"],
  "high-risk": ["High risk", "bad"],
} as const satisfies Record<string, readonly [string, Tone]>;

export type Status = keyof typeof STATUS;

export const GLYPH: Record<Tone, string> = {
  neutral: "○", // ○
  ok: "✓", // ✓
  warn: "!",
  bad: "✕", // ✕
  gate: "◐", // ◐
  agent: "✦", // ✦
};

export function statusLabel(status: Status): string {
  return STATUS[status][0];
}
export function statusTone(status: Status): Tone {
  return STATUS[status][1];
}

export interface StatusPillProps extends Omit<HTMLAttributes<HTMLSpanElement>, "children"> {
  status: Status;
  // label overrides the vocabulary text for one instance (e.g. "Needs approval: Linda K").
  // It never changes the tone.
  label?: string;
}

// StatusPill renders a status from the fixed vocabulary: label text plus a tone glyph.
export function StatusPill({ status, label, className, ...rest }: StatusPillProps) {
  const [text, tone] = STATUS[status];
  return (
    <span className={["ui-pill", tone !== "neutral" && `ui-pill--${tone}`, className].filter(Boolean).join(" ")} data-status={status} {...rest}>
      <span className="ui-pill__glyph" aria-hidden="true">
        {GLYPH[tone]}
      </span>
      {label ?? text}
    </span>
  );
}
