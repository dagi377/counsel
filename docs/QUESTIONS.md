# Questions

Open questions from implementers. Add one entry per question and stop the task it blocks.
The owner answers in place; move answered entries to the bottom.

Template:

```

## Q-NNN <short subject>

- Task: T-0NN
- Sources: <spec files and ids that disagree or are silent>
- Reading A:
- Reading B:
- Proposed:
- Answer: (owner)
```

## Open

## Q-AI-003 Wire the AI fixture lint into make check

- Task: AI-003
- Sources: spec/ai/tasks-ai.json AI-003 accept ("missing one -> make check fails naming it"),
  spec/ai/mocks.md sec. 4 ("tools/ai_fixture_lint.py, part of make check"); AI-003 `files`
  does not list `Makefile` (owned by T-001.2).
- Reading A: AI-003 adds the Makefile target itself.
- Reading B: the Makefile owner adds it; AI-003 only ships the lint.
- Proposed: add to `Makefile` (`check` prerequisites and .PHONY):
  `ai-fixture-lint:` running `$(PYTHON) -I tools/ai_fixture_lint.py --self-test` then
  `$(PYTHON) -I tools/ai_fixture_lint.py`. Default mode requires fixtures for every feature
  whose dir exists or whose fixture task is ticked in docs/TASKS.md, so main stays green while
  wave 1 lands; switch to `--strict` (all 194 features) once wave 1 is merged. Not done in
  AI-003 because Makefile is not in its files.

## Q-001 Vite and Vitest for web/

- Task: T-001.4
- Sources: AGENTS.md "Allowed dependencies" (Front end: TypeScript, React only); T-001.4 accept.
- Reading A: the front end may use only TypeScript and React, with no build or test tool.
- Reading B: web/ needs a bundler and a unit-test runner; feature tasks (e.g. T-035L.3) name Vitest.
- Proposed: allow Vite (build, dev server, `import.meta.glob` feature discovery) and Vitest
  (unit tests) as dev tooling for web/ only, plus @vitejs/plugin-react and TypeScript types.
  T-001.4 also adds a `web-test` target to the Makefile (not in its file list) so `make check`
  runs `npm --prefix web test`; it installs web/node_modules when missing. web/package-lock.json
  is not committed by T-001.4 because it is not in the task's file list; versions are pinned exactly.
- Answer: (owner)
