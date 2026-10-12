<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

# Counsel build: brief for one task agent

You implement ONE task from `spec/tasks.json` in the Counsel repo (`github.com/dagi377/counsel`, local clone at `<repo>`).

## Setup (local)
Create your own worktree from origin/main. Never work in the main checkout.

    G=<repo>
    git --git-dir=$G/.git fetch origin
    git --git-dir=$G/.git worktree add <worktrees>/<ID> -b task/<ID> origin/main

If `<worktrees>/<ID>` already exists, an earlier agent was stopped partway. Continue in that worktree: review what is already there and keep what is good.
Then run every git command as `env GIT_DIR=<wt>/.git GIT_WORK_TREE=<wt> git ...`, or with the worktree path as the target. Do not use `cd`, `git -C`, `sed -i` or `cp`.

## Do
1. Read `AGENTS.md`, your task entry in `spec/tasks.json` (`files`, `accept`, `rule_ids`, `depends_on`, `flag`) and the spec sections those point to (`spec/eng`, `spec/legal`, `spec/ai`).
2. Touch ONLY the files your task lists. If you must touch another file, stop and write the reason into `docs/QUESTIONS.md` under your task id.
3. Contracts (`contract.go`, `api/openapi.yaml`, `migrations/0001_init.sql`, `spec/kernel/*.schema.json`) are frozen once on main. Changes to them must be additive and need a `QUESTIONS.md` entry.
4. Tag each rule you enforce with a `// rule: <id>` comment on the line above the code and in the test. Tests are table-driven.
5. AI: use only `kernel/ai` `ModelClient`, plus `aifake` or the mock LLM server in tests. Never call a real model.
6. Money is integer cents, rates are ppm, times are America/Toronto. The person who prepares an item never approves it, and the agent never approves.
7. Code behind a V-* item goes behind its flag, which is off by default.

## Check
Run:

    env -u GOROOT make -C <wt> check GO=/opt/homebrew/bin/go GOFMT=/opt/homebrew/bin/gofmt

It must pass. For web work, run `npm --prefix <wt>/web test`.

## Finish
Make one commit, `feat: <ID> <subject>`, with these trailers:

    Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
    Claude-Session: https://claude.ai/code/session_015cZdbw6Ruanvdp2kmpYvzn

Then:
- push: `git push origin task/<ID>`. Never push main; the integrator merges.
- remove your worktree: `git --git-dir=$G/.git worktree remove <wt>`.

Hard limit: 30 minutes. If you run out of time, push what passes `make check` and list what is left.

Return at most 4 lines: the task id, the pushed sha, the check result, and anything left or any question.
