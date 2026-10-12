import { StatusPill, type Status } from "../StatusPill";
import { DateTime } from "./DateTime";

export type GateState = "pending" | "approved" | "rejected" | "withdrawn";

// A principal is a person or the agent. Only a person can approve.
export interface Principal {
  name: string;
  kind: "person" | "agent";
}

export interface PersonPrincipal extends Principal {
  kind: "person";
}

export class GateError extends Error {}

export interface GateStatusProps {
  state: GateState;
  // preparer is who proposed the change: a person or the agent (on behalf of a person).
  preparer: Principal;
  // actingFor names the person an agent preparer acted for.
  actingFor?: PersonPrincipal;
  // approver is the person who decided (approved or rejected); for pending it is who is
  // expected to decide, or omitted when any eligible approver may. Never the agent.
  approver?: PersonPrincipal;
  // decidedAt is the instant of the decision or withdrawal.
  decidedAt?: string | Date;
  // compact shows the pill only, with the full text as a title.
  compact?: boolean;
  className?: string;
}

const PILL: Record<GateState, Status> = { pending: "pending", approved: "approved", rejected: "rejected", withdrawn: "withdrawn" };

// GateStatus shows where an approval stands and who is on each side. It refuses to render an
// agent as the approver, because an agent never approves.
export function GateStatus({ state, preparer, actingFor, approver, decidedAt, compact, className }: GateStatusProps) {
  if ((approver as Principal | undefined)?.kind === "agent") throw new GateError("gate: an agent cannot be the approver");
  if (preparer.kind === "person" && approver && approver.name === preparer.name) throw new GateError("gate: the preparer cannot be the approver");

  const prepared =
    preparer.kind === "agent" ? (
      <>
        <span className="ui-gate__agent">{"✦"} Agent prepared</span>
        {actingFor && <> for <b>{actingFor.name}</b></>}
      </>
    ) : (
      <>
        Prepared by <b>{preparer.name}</b>
      </>
    );

  let decided: React.ReactNode;
  switch (state) {
    case "pending":
      decided = approver ? (
        <>
          Awaiting <b>{approver.name}</b>
        </>
      ) : (
        <>Awaiting an approver other than the preparer</>
      );
      break;
    case "approved":
      decided = (
        <>
          Approved by <b>{approver?.name ?? "—"}</b>
        </>
      );
      break;
    case "rejected":
      decided = (
        <>
          Rejected by <b>{approver?.name ?? "—"}</b>
        </>
      );
      break;
    case "withdrawn":
      decided = <>Withdrawn by the preparer</>;
      break;
  }

  const pill = <StatusPill status={PILL[state]} />;
  if (compact) {
    return (
      <span className={className} title={`${state}: prepared by ${preparer.name}${approver ? `, approver ${approver.name}` : ""}`}>
        {pill}
      </span>
    );
  }
  return (
    <div className={["ui-gate", className].filter(Boolean).join(" ")} data-state={state}>
      {pill}
      <span className="ui-gate__when">{decidedAt ? <DateTime value={decidedAt} style="datetime" /> : null}</span>
      <div className="ui-gate__who">
        <span>{prepared}</span>
        <span aria-hidden="true">{"·"}</span>
        <span>{decided}</span>
      </div>
    </div>
  );
}
