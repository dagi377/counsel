<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

Implement the Counsel tasks named in your launch message, one after another. Each task gets its own worktree, branch task/<ID>, commit and push.
- The brief: docs/briefs/task-agent.md. Follow it exactly.
- The task definitions are in spec/ai/features.json and spec/ai/tasks-ai.json; decisions are in docs/QUESTIONS.md.
- Dependencies may be pushed but not merged yet. Check `git branch -r | grep task/FEAT` first, and if a task needs one, base the branch on it (e.g. origin/task/FEAT-CORE-06 for run Scope). Never redefine an interface that already exists on such a branch.
- Register tools through explicit *Handler() functions, never init(), even where the accept text says init().
- The agent never approves. Gates are opened with the acting user as preparer.
- Use the mock model only (aifake or the mock server).
Hard limit: 15 minutes per task. Report at most 3 lines per task.

Feature tasks (DL, CON, and so on): the end-to-end feature tests need `agenttest.FromFeature` (FEAT-CORE-14) and the tool handlers.
- If `git log origin/main --oneline | grep FEAT-CORE-14` shows CORE-14 is on main, base on origin/main.
- If not, merge origin/task/FEAT-CORE-14 plus the tool branches you need into your branch.
- Write the full FromFeature tests (403, ErrSelfApproval, ErrUnsourcedValue, injection). Do not skip them.
- Register each feature Def in appDefs() (FEAT-CORE-17) so its skill file is generated.
- Every FEAT agent: register Defs in app/legal/agent/featureset (featureset.App()), not feature.appDefs.
