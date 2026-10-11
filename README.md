# Counsel

Counsel is an AI-native practice app for Ontario law firms: matters, conflicts, time, trust
accounting and billing, with an agent that drafts and proposes while people approve.
It is built as a reusable app kernel with the legal app on top.

**Status: spec only.** No code exists yet.

## Spec

| Path | Holds |
|---|---|
| `spec/eng` | engineering spec: build guide (`o1-build-guide.html`), kernel (`k1-kernel.html`), interfaces and OpenAPI, data model and DDL, billing law, tax, billing engine, uptime, migration |
| `spec/legal` | product spec by area: screens, gates, entities, access matrix |
| `spec/ai` | agent spec: tools and skills |
| `spec/blockers.json` | open conflicts and verify blockers |

Before writing code, read `AGENTS.md`. Open owner decisions: `docs/DECISIONS-NEEDED.md`.
Tasks: `docs/TASKS.md`. Implementer questions: `docs/QUESTIONS.md`.

## Layout

```
cmd/          one directory per shipped binary (counseld, counsel-worker, counsel-migrate, counsel-import)
kernel/       domain-free plumbing: registry, identity, gate, audit, outbox, money, ledger, docs, work, deadline, report, importer, agenttools, server
app/legal/    the legal app; register.go is its one entry point
api/          openapi.yaml (CONTRACT)
migrations/   SQL migrations; 0001_init.sql is the CONTRACT DDL
spec/         the spec, and CONTRACT copies used as test data
web/          TypeScript and React front end (src/kernel, src/features/<area>)
deploy/       cell deploy (Helm, CloudNativePG)
test/         fixtures (helpdesk: a non-legal app), fakes by protocol, e2e suites
tools/        spec lint, rule coverage, kernel ratchet lists
docs/         ADRs, tasks, questions, decisions needed, research
```
