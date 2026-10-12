<!-- Copied from the build session's scratchpad so the path survives; paths are generalised. -->

# Counsel web: building the product UI

Read Fable's review first: `docs/briefs/review-ui.md` (its screenshots were not kept; re-take them with Playwright).

## The bar
The web UI is today a scaffold. It needs to become product-grade legal and financial software, at the level of Clio, Linear, Stripe and the Google Cloud console.
- Desktop first: check at 1280px and 1600px, in light and dark.
- Money is integer cents end to end. An overdraft is ALWAYS visible: signed, in the danger colour, and labelled "Overdrawn".
- Gates (approvals) show the preparer, the approver and a pending state. Agents never approve.
- Ontario: dates are shown in America/Toronto.

## Repo
- Repo: <repo>, under `web/`.
- Read `AGENTS.md` and `web/README` if present, and the spec tasks in spec/tasks.json (T-045L.*, T-045.*, T-035.5, T-039.4).
- The prototype at prototype/index.html, if present, holds design tokens to port.

## Git
- Create the worktree at `<worktrees>/web-<pkg>`, on branch `web/<pkg>`, from origin/main or from the branch your launch message names.
- Run git as `git --git-dir=<wt>/.git --work-tree=<wt>`. Do not use cd, git -C, sed -i or cp.

## Check
- `npm --prefix <wt>/web test`, plus tsc, plus lint.
- Also `env -u GOROOT make -C <wt> check GO=/opt/homebrew/bin/go GOFMT=/opt/homebrew/bin/gofmt` if you touch Go.
- Screenshots: use Playwright with system Chrome, against a dev server with mock data. Never call real services or a real model.

## Rules
- Touch only your package's files.
- Contract changes (openapi) must be additive, each with a QUESTIONS entry.
- Tests: every state is covered (loading, empty, error, partial and forbidden). The pages are in an axe sweep. Behaviour assertions are never weakened.

## Commit
Subject: `feat: web <pkg> <subject>`, with these trailers:

    Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
    Claude-Session: https://claude.ai/code/session_015cZdbw6Ruanvdp2kmpYvzn

Push `web/<pkg>`, never main. Remove the worktree.

Report in at most 5 lines.
