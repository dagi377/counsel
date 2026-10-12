<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

# GATE-EFFECTS-2: real per-kind approval effects

Base your branch on origin/task/GATE-EFFECTS (e134e8b8). The brief is docs/briefs/task-agent.md.

GATE-EFFECTS registered every gate kind with one shared boot.ApprovalEffect. For many subjects that effect only records the decision. Approving a gate must DO the approved thing.

## What to do
1. Find every gate kind owned by your module group that falls into the record-only branch (spec/gates.json has the owner and the effect description).
2. Write a real effect for each, in the owning module, as described by spec/gates.json and spec/legal. Each effect must:
   - apply exactly what was approved;
   - be idempotent (keyed by gate id);
   - refuse an agent approver and preparer = approver;
   - use integer cents;
   - follow By-law 9 for trust;
   - be tagged with `// rule:`.
3. Register each effect in boot through a per-kind map, adding one line per kind. Put the map in a NEW file named effects_<group>.go, so the three parallel groups don't conflict.
4. Write table tests for each effect: approval applies, a repeat is a no-op, and an agent or the preparer is refused.

## Rules
- If a kind's effect genuinely needs an owner decision, leave it on the record-only path and add a QUESTIONS entry naming it.
- Use a private GOCACHE at scratchpad/gocache-ge2-<group>, deleted when done.
- Run make check at the end.
- Hard limit: 50 minutes.

Report at most 3 lines: the kinds given real effects, and the kinds left with the reason.
