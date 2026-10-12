<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

# Counsel integrator brief

You are the Counsel integrator. Repo: <repo> (github.com/dagi377/counsel). Read all of this before you start.

## Setup
1. Fetch.
2. Make a worktree at <worktrees>/<pass>, on branch integ/<pass>, from origin/main.
3. Run git as `git --git-dir=<wt>/.git --work-tree=<wt>`. Do not use cd, git -C, sed -i or cp.

## Merging
- Merge the branches you are given in the order given. Skip a branch only if it is not on origin, and list every skipped branch.
- Before you start, read the decisions in docs/QUESTIONS.md (owner decisions are recorded there per task) and apply the ones that touch your branches.
- docs/QUESTIONS.md will conflict. Keep every entry.
- Migration numbers must be unique and pass the lint. If two branches take the same number, renumber within the block.
- If branches carry duplicate copies of a file (for example several scope.go copies), keep the one on main or the most complete one. Do not end up with two definitions.

## After every merge
Run:

    env -u GOROOT make -C <wt> check GO=/opt/homebrew/bin/go GOFMT=/opt/homebrew/bin/gofmt

It must be green before you merge the next branch.
- Never weaken a test, and never relax a contract test.
- Contract changes must be additive, each with a QUESTIONS.md entry.

## Landing on main
1. Push integ/<pass>.
2. Run `git fetch`, then merge origin/main, because another integrator may have landed first. Re-run check.
3. Push `integ/<pass>:main` as a fast-forward. Never force.
4. Delete the merged remote task branches and remove the worktree.

## Commits
Every commit carries these trailers:

    Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
    Claude-Session: https://claude.ai/code/session_015cZdbw6Ruanvdp2kmpYvzn

Hard limit: 50 minutes.

## Report
At most 8 lines: the main sha, what was merged, what was skipped, and any open questions.
