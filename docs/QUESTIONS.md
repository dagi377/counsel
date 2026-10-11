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

## Q-004 POST /hooks/{provider} lacks problem responses ARC-I-051 asks for

- Task: T-009.1
- Sources: spec/rules/a2.json ARC-I-051 ("every operation ... mutating operations 400, 401, 403,
  404, 409, 412, 422, 428, 429"); api/openapi.yaml `receiveProviderEvent` documents no 403, 404,
  409 or 422.
- Reading A: ARC-I-051 covers the session-less webhook receiver too; the contract needs an
  additive change adding the four responses.
- Reading B: the receiver is not a client operation; ARC-I-051 excludes it.
- Proposed: B for now. `TestARC_I_051_OperationsDocumentProblemResponses` skips only
  `/hooks/{provider}`; if A, add the responses additively and drop the skip.
- Answer: (owner)

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

## Q-T-007.1 registry.GateKind has no SpecID field for gate.approval_request.spec_gate

- Task: T-007.1
- Sources: migrations/0001_init.sql (approval_request.spec_gate NOT NULL); Q-003 added
  `spec_id` to spec/kernel/gatekind.schema.json only, not to the Go `registry.GateKind`.
- Reading A: add an optional `SpecID string` to `registry.GateKind` (additive contract change).
- Reading B: keep the contract; the engine takes the label separately.
- Done for now: B. `gate.Kinds.Label(slug, specID)` records it, and `Open` refuses a kind
  with no label (ErrInvalidKind). Proposed: A, so T-007L.1 can pass it through the Registry.
- Answer: (owner)

## Answered

## Q-005 BIL-C-082 over-credit: which status and problem code

- Task: T-023.1
- Sources: spec/rules/b3.json BIL-C-082 test ("-> 409 credit_exceeds_bill"); api/openapi.yaml
  requestCreditNote documents 409 but names no code.
- Reading A: 409 with problem code `credit_exceeds_bill`, rule BIL-C-082, as the rule's test says.
- Reading B: 422 validation error.
- Answer: owner (2026-10-11): A. `credit.CheckBucket` wraps `ErrExceedsBalance` together with a
  409 `credit_exceeds_bill` problem (rule BIL-C-082), so `server.WriteError` writes it as is.

## Q-004 Audit chain hashes `at` in the session time zone

- Task: kernel/db (T-043), kernel/audit (T-005)
- Sources: migrations/0001_init.sql audit.chain trigger (hashes `jsonb_build_object(..., 'at', NEW.at, ...)::text`); kernel/audit verification rebuilds the same text.
- Problem: jsonb renders a timestamptz in the session's TimeZone, so a caller whose session zone is America/Toronto makes the trigger hash a different string than verification rebuilds, and Verify reports a broken chain.
- Answer: owner (2026-10-11): kernel/db sets `SET LOCAL TIME ZONE 'UTC'` in every transaction (InTx and Bind), so the trigger and Verify hash `at` identically. Business dates stay America/Toronto through `clock`. Done in kernel/db/pool.go; integration test kernel/db/pool_test.go runs with session zone America/Toronto.

## Q-003 GateKind has no field for the spec gate id

- Task: T-002L.2
- Sources: spec/kernel/gatekind.schema.json (no spec id field); a2-interfaces.json gate_kinds[].spec_gate; DECISIONS-NEEDED CF-04 ("gate code = slug ... plus a spec id column").
- Reading A: drop the spec id from spec/gates.json and trace by slug only.
- Reading B: add an optional `spec_id` string to the GateKind schema (additive).
- Proposed: B.
- Answer: owner (2026-10-11): add `spec_id` additively and record it here. Done in T-002L.2.

## Q-002 GateKind cannot express dual mode or a sole-owner delay path

- Task: T-002L.2
- Sources: a2-interfaces.json gate_kinds[].dual_mode_min_approvers (2 on G-B4, G-B6, G6, G6c, G11, G11t, G12, G-TSIG, G-TREL); access.json GA9 ("two owners, or owner plus 24h delay for a sole owner") and GA11 (sole owner: identity check plus 24h delay).
- Reading A: the gate engine reads a firm setting (dual mode) and raises min_approvers at runtime; spec/gates.json carries the base value.
- Reading B: add `dual_mode_min_approvers` and a sole-owner `cooling_off` path to the GateKind schema.
- Proposed: A for dual mode (firm setting, T-007/T-007L). For GA9 spec/gates.json sets 2 approvers, so a sole-owner firm cannot export until the delay path exists. See docs/gates-defaults.md.
- Answer: owner (2026-10-11, as the firm's lawyer): B, with two additive optional GateKind
  fields. `dual_when` (e.g. `"signatories>=2"`): require 2 distinct approvers when the firm has
  at least 2 eligible approvers holding the gate's capability. `sole_owner_delay` (Go duration,
  e.g. `"24h"`): when only one eligible approver exists, that person may approve alone only after
  the delay, with notification. Applied: the nine dual-mode trust gates get
  `dual_when: "signatories>=2"`; GA9 and GA11 get `min_approvers: 2` with
  `sole_owner_delay: "24h"`. Done in T-002L.2.
