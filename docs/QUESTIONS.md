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

## Q-DRAFT-001 Draft rules enforced only in draftfake (gap record)

- Task: T-017.2 (BIL-C-021), T-017.3 (BIL-C-041), T-017.4 (BIL-L-002)
- Sources: review L3; `app/legal/billing/draft/draftfake/fake.go` was the only code tagged
  `// rule:` for these ids. BIL-C-022 and BIL-C-023 are already tagged in `draft/lines.go`
  and `draft/examples_test.go`.
- Gap: no production code enforces the bill discount total (BIL-C-021), the no-markup-on-agent
  disbursement rule (BIL-C-041) or the itemized-disbursement delivery block (BIL-L-002). The
  fake keeps its logic for callers' tests but no longer carries `// rule:` tags, so a grep for
  rule enforcement no longer counts it.
- Proposed: the three open tasks add `discount.go`, `disbursement.go`, `itemize.go` with the
  tags and `TestBIL_C_021_*`, `TestBIL_C_041_*`, `TestBIL_L_002_*`; then delete the matching
  fake logic.
- Answer: (owner)

## Q-AGENT-002 F-BIL-03 gate kind differs from the real trust withdrawal tool

- Task: T-037L.2 (agent write tools), F-BIL-03 acceptance test
- Sources: the F-BIL-03 acceptance test expected gate `billing.trust_fee_withdrawal` and
  capability `trust.prepare`; `agent.WriteTools` (app/legal/agent/tools_write.go) opens
  `trust.withdrawal` and needs `trust.request`. The fixture input also did not match the real
  `prepare_trust_withdrawal` schema.
- Change made: the F-BIL-03 test now runs the real `agent.WriteTools` through
  `app/legal/agent/agenttest`; fixtures send the real schema and the test asserts the real
  gate kind and capability.
- Reading A: a trust-to-fees transfer is a trust withdrawal; keep `trust.withdrawal`.
- Reading B: add a separate fee-transfer write tool over `billing/trustapply` with gate
  `billing.trust_fee_withdrawal`.
- Answer: (owner)

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

## Q-AI-005 New fake-LLM endpoints must register routes in main.go

- Task: AI-005 (same for AI-006 and AI-007)
- Sources: spec/tasks.json AI-005/006/007 `files` list only the new protocol files; routes are
  registered in `server.handler()` in `test/fakes/llm/main.go`, which none of them lists.
- Reading A: touching `main.go` is implied; each task adds its `mux.HandleFunc` lines (and
  AI-006 hooks the fault layer and journal into `route`).
- Reading B: the tasks' `files` must be extended to list `main.go` first.
- Proposed: A. AI-005 adds two route lines to `handler()` plus `chat_test.go`. The AI-006 and
  AI-007 branches add their lines next to them; merging gives a trivial conflict in `handler()`.

## Q-AI-006 Fault layer and journal need main.go and messages_test.go

- Task: AI-006
- Sources: spec/tasks.json AI-006 `files` lists only `test/fakes/llm/faults.go` and
  `journal.go`; the server struct, flags and routes live in `test/fakes/llm/main.go`, and
  `TestFaultFixtureRefused` in `messages_test.go` asserted the pre-AI-006 501 refusal.
- Reading A: those edits are implied by the task.
- Reading B: the task's `files` must list them first.
- Proposed: A. `main.go` gains `-dump-bodies`, `-speed`, three server fields, the
  `/_journal` and `/_reset` routes and wraps `/v1/messages` in `s.instrument(...)`;
  `TestFaultFixtureRefused` is replaced by `faults_test.go`. When AI-005 merges, wrap
  `/v1/chat/completions` and `/v1/responses` the same way (a `decodeFunc` each, with
  `writeOpenAIError`).

## Q-AI-007 /v1/embeddings route registered in main.go

- Task: AI-007
- Sources: spec/tasks.json AI-007 `files` lists only `test/fakes/llm/embeddings.go`; routes are
  registered in `server.handler()` in `test/fakes/llm/main.go` (see Q-AI-005).
- Proposed: one `mux.HandleFunc("POST /v1/embeddings", ...)` line in `main.go`; the
  configured dimension is an `-embed-dim` flag (default 1024) declared in `embeddings.go`, so
  the server struct is untouched. When AI-006 merges, wrap the route in `s.instrument` if
  embeddings calls should be journalled.
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

## Q-031.1 Rule pack schema cannot express clear days or months_after

- Task: T-031.1
- Sources: T-031.1 accept (day-count mode clear/business/calendar, months_after, validated by
  spec/kernel/rulepack.schema.json); kernel/deadline/contract.go (`Clear`, `Rule.MonthsAfter`,
  `Roll` none/forward only); spec/kernel/rulepack.schema.json (`day_count` enum calendar/business,
  no `months_after`, `roll` also allows `backward`; rules have `additionalProperties: false`).
- Reading A: the schema is the contract; packs cannot use clear days or months_after.
- Reading B: the schema is missing them; T-031.2 golden dates and the Ontario packs (T-031L.2)
  need both.
- Proposed: additive schema change: add `"clear"` to `day_count.enum` and an optional
  `"months_after": {"type": "integer", "minimum": 0}` to rule properties. kernel/deadline/pack.go
  already maps both, so no code change follows. Until then the loader rejects `roll: backward`
  (the contract has no `Roll` for it) as ErrInvalidPack; either add `RollBackward` to the
  contract or drop `backward` from the schema.
- Answer: (owner)

## Answered

## Q-T-023.2bb No gate kind for the managing-lawyer write-off approval

- Task: T-023.2bb (blocks T-023.2bc)
- Sources: spec/eng/b3-billing-engine.json BIL-C-085 ("G-B5 ... owner role above threshold",
  config writeoff_second_approval_cents 100000); spec/gates.json has only G-B5
  `billing.credit_note` (approver `billing.credit`, min_approvers 1); spec/legal/access.json
  has no managing-lawyer capability. `app/legal/gates/kinds_test.go` requires every registered
  kind to be in spec/gates.json, so the effect cannot open a kind declared only in Go.
- The task says: if no second-gate kind exists, ask for an additive one and stop.
- Reading A: add a gate kind to spec/gates.json, e.g. slug `billing.writeoff_second_approval`,
  spec_id `G-B5`, label "G-B5 (managing-lawyer second approval)", risk high, min_approvers 1,
  ttl 168h, payload `{write_off_id (uuid), bill_id (uuid), amount_cents (integer >= 0),
  first_approver (uuid)}`, approver = a new capability `billing.writeoff_second_approve`
  granted at level approve to the owner role only in access.json. app/legal/gates registers
  it; T-023.2bb opens it (preparer = the G-B5 approver) and T-023.2bc applies it.
- Reading B: no new kind; raise G-B5 to two approvers (dual_when amount > threshold) and check
  the owner role in the G-B5 effect. This needs an amount-conditional dual mode the gate
  engine does not have (Q-002).
- Proposed: A. T-023.2ba already ships `NeedsSecondApproval` and
  `SecondApproval.CheckSecondApprover` (managing lawyer = owner role, resolved by the caller).
- Answer: owner (2026-10-11): A. Additive gate kind `billing.writeoff_second_approval`
  (spec_id G-B5) in spec/gates.json; approver capability `billing.writeoff_second_approve`,
  held only by the owner role. The two approvers are different people, and neither is the
  preparer or an agent.

## Q-T-045.1 Generated API client cannot live in web/src/kernel

- Task: T-045.1
- Sources: spec/tasks.json T-045.1 files (`web/src/kernel/api/client.ts`); AGENTS.md "Where things go" (generated client in `web/src/kernel`); kernel/boundary_test.go `termRoots` includes `web/src/kernel`; api/openapi.yaml is the legal API (`matter_id`, `/trust/...`, `hst_cents`).
- Problem: a client generated from api/openapi.yaml carries matter, trust and HST names, so TestKernelHasNoLegalTerms fails; `tools/kernel_allow.txt` may only shrink.
- Reading A: generate into `web/src/app/api/client.ts` (app layer); `web/src/kernel` keeps only the domain-free runtime (fetch, problem+json, extensible-enum decode, enumLabel).
- Reading B: keep the path and add `web/src/kernel/api/client.ts` as an exemption in kernel/boundary_test.go (scanner change, not an allowlist line).
- Proposed: A. A working generator (tools/gen_client.mjs, python3+PyYAML for YAML, no new npm deps) and ARC-I-002 tests are drafted and pass vitest; they ship once the path is decided.
- Answer: owner (2026-10-11): A. Generate into `web/src/app/api` (the kernel web stays free of legal terms), with a Node YAML parser (`yaml` devDependency), not python3. T-045.1 is requeued.
- Status: done in T-045.1: `tools/gen_client.mjs` writes `web/src/app/api/client.ts`; `yaml` is a web devDependency.

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
- Answer: owner (2026-10-11): A. 403, 404, 409 and 422 problem responses added to `receiveProviderEvent` in api/openapi.yaml (additive); the ARC-I-051 skip is gone and the contract test accepts additive changes over the baseline.

## Q-004 trust.Accounts cannot fill NOT NULL columns of trust_account and signatory

- Task: T-011.1
- Sources: app/legal/trust/contract.go (`Account` has only `AccountLast4`; `AddSignatory` takes
  only a user id); migrations/0001_init.sql (`trust_account.account_no_ciphertext NOT NULL`,
  `signatory.approval_id NOT NULL`, `signatory.is_licensee NOT NULL`).
- Reading A: the contract methods store placeholders (empty ciphertext, no approval id).
- Reading B: the contract needs additive input: the full account number (sealed before
  storage) and the GA1 approval id.
- Proposed: B. T-011.1 ships additive `AccountStore.OpenAccountNumber` and
  `AddApprovedSignatory`; the contract `OpenAccount` and `AddSignatory` validate and return
  `ErrNeedsApproval` until the owner picks a shape (e.g. `Account.AccountNo` and an
  `approvalID` argument).
- Answer: owner (2026-10-11): B. `Account.AccountNo` added (additive, input only). `OpenAccountNumber` and `AddApprovedSignatory` are the supported API; `OpenAccount` and `AddSignatory` stay as deprecated stubs returning ErrNeedsApproval, so no signature changes.

## Q-005 app/legal register test forbids any registration

- Task: T-010L.1
- Sources: T-010L.1 accept ("Chart OP-*/TR-* registered with the ledger engine");
  app/legal/books/register.go (features `add` a registrar from init());
  app/legal/register_test.go `TestRegisterWithNoFeaturesRecordsNothing` (owned by T-001.5)
  fails once any feature registers anything.
- Reading A: wave-1 features wire themselves with `add` from init(); the scaffold test is
  replaced by one that checks the expected calls.
- Reading B: features stay unwired until a later task.
- Proposed: A. T-010L.1 ships `books.RegisterCharts` unwired; once the owner of
  app/legal/register_test.go relaxes it, add `func init() { add(RegisterCharts) }` to chart.go.
- Answer: owner (2026-10-11): registration is split into an "always" set (chart of accounts, core gate kinds and capabilities) and a feature-gated set. `books.RegisterCharts` is wired from init(); `TestRegisterWithNoFeaturesRecordsOnlyAlwaysSet` asserts only always-set calls are recorded.

## Q-T-024b.2 Special circumstances after one month or after 12 months

- Task: T-024b.2
- Sources: spec/rules BIL-L-005; Solicitors Act ss.3, 4; the T-024b.2 accept line asked for a note after one month.
- Answer: owner (2026-10-11): follow BIL-L-005, not the accept line. Within one month assessment is available as of right; then the court may refer the bill ("court referral available"); special circumstances are needed only after 12 months. Tagged `// rule: BIL-L-005 (verify)`, behind `billing.solicitors_act_verified` (off by default).

## Q-GATES-001 spec/gates.json spec_id values that fail the spec_gate CHECK

- Task: T-007.1, T-007L.1
- Sources: migrations/0001_init.sql approval_request.spec_gate CHECK `^G(-?[A-Z0-9]+)+[a-z]?$`; spec/gates.json had 10 ids such as "G14 (conflicts)" and "G-form".
- Answer: owner (2026-10-11): the 10 ids are normalised (e.g. "G14 (conflicts)" -> "G14", "G-form" -> "G-FORM") and the long text kept in a new optional `label` field (additive in spec/kernel/gatekind.schema.json). `gates.ParseKinds` enforces the CHECK pattern; the frozen 0001 CHECK is unchanged.

## Q-INBOX-001 Two inbox Claim implementations

- Task: T-008.1, T-008.3
- Sources: kernel/idem `PG.Claim` and kernel/outbox `PG.Claim` both inserted into platform.inbox.
- Answer: owner (2026-10-11): keep `outbox.PG.Claim`; the idem copy is removed and its ARC-U-022/ARC-I-030 tests moved to kernel/outbox/inbox_test.go.

## Q-GATE-002 Fractional numbers in gate payloads

- Task: T-007.1
- Sources: kernel/gate payload canonicalisation rejects non-integer numbers; no test covered it.
- Answer: owner (2026-10-11): keep the rejection; `TestARC_I_014_PayloadHashRejectsFractionalNumbers` covers fractions, exponents and out-of-range integers.

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

## Q-T032L1 app/legal register test expects modules to register nothing

- Task: T-032L.1
- `app/legal/register_test.go` `TestRegisterWithNoFeaturesRecordsNothing` asserts that
  `legal.Register` makes zero registry calls. T-032L.1 must register document kinds and
  retention classes, so that test fails (16 calls). The file is outside this task's list.
- Proposed: the integrator replaces the zero-call check with a check that `Register` twice on a
  `registryfake` and `Seal` reports only `ErrDuplicateID`-free results for one call, or drops
  it. Every wave 1 task that registers anything hits the same failure.
- Answer: owner (2026-10-11): same as Q-005. Registration tests assert only the "always" set;
  feature-gated kinds (docs kinds, RC-* retention) register only when their flag is on. Done:
  `docs.RegisterKinds` is exported and not in the always-run registrars, so
  `TestRegisterWithNoFeaturesRecordsOnlyAlwaysSet` stays as is.

## T-035L.2 Court filing contract does not match comms.comm_filing; no registry kind for message templates

- Task: T-035L.2
- Sources: app/legal/comms/contract.go (`Filing`: Court, Reference, DocumentID, status
  draft/filed/rejected, FiledOn); migrations/0001_init.sql `comms.comm_filing` (key
  `(comm_item_id, matter_id)`, `rule`, `confidence_ppm`, status proposed/filed/rejected,
  `filed_by`, `filed_at`): the table files a communication item to a matter, it is not a court
  filing. `comms.message_template` is versioned (`key`, `version`, `channel`, `body_ref`,
  approval columns) while the contract `Template` holds Subject and Body inline. The task says
  templates are "registered by the app", but `kernel/registry.Registry` has no message-template
  kind (only EventType, DocumentKind, PortalPage, ...).
- Reading A: comm_filing is "file this message to a matter" (agent proposes with rule and
  confidence, a user confirms). The contract `Filing` gets reshaped additively to match and a
  court-filing record moves to a later task with its own table.
- Reading B: court filings are a new additive table (e.g. `comms.court_filing`) matching the
  contract; comm_filing stays as is for message-to-matter filing.
- Templates: the app either seeds `comms.message_template` rows at boot through its own
  store, or `Registry` gains an additive `MessageTemplate` kind.
- Proposed: B for filings (contract already describes court filings), and app-side seeding
  for templates (no Registry change).
- Answer: owner (2026-10-11): B. A separate additive `legal.court_filing` table (court,
  reference, draft/filed/rejected); `comms.comm_filing` stays as message-to-matter filing. The
  app seeds its own message templates (versioned `body_ref`) at registration; no registry kind
  is added. T-035L.2 is requeued.

## AI-008: /healthz model field

- No `/healthz` handler exists on main yet, and AI-008 lists no server file. AI-008 ships
  `ai.Config.Check(env)` and `httpclient.FromConfig(...)`, whose `ModelClient.Mode()` is the
  value `/healthz` must report as `model: mock|live`. Proposed: the task that adds `/healthz`
  (kernel/server) reads `Mode()` from the client built at boot, and boot passes `COUNSEL_ENV`
  as `env` so a live config in test or ci fails with `ErrLiveModelForbidden`.

## Q-FLAGS-001 Verify flags that modules define but app/legal does not register

- Tasks: T-041.x (export), T-044.x (reports), T-037L.x/T-038L.x (clio), T-035L.1 (conflict)
- Sources: `export.FlagPostCancelWindow` and `reports.FlagBillingRecordsVerified` were read
  with `flags.Enabled` but missing from `app/legal/flags.go`; several packages repeated
  flag names as private copies.
- Answer: owner (2026-10-11): the integrator registers every new flag in app/legal/flags.go,
  default off. Done: `billing.records_verified` and `export.post_cancel_window` added
  (`conflict.id_verification` and `trust.bylaw9_verified` were already there). Module packages
  cannot import app/legal (it imports them), so each keeps one exported mirror per package;
  the private copies in clio and billing/assessment were folded into it, and
  `TestFlagMirrorsMatchCanonicalNames` checks every mirror against app/legal.

## Q-GL-DIM: generic line dimensions on kernel/ledger.Line

- `gl.journal_line` carries optional columns beyond the core ones (`party`, `tax_code`,
  `bank_line_id`, and the app's `matter_id`). Reverse copied only the three kernel ones, so a
  reversal lost the app's link; the kernel may not name it.
- Done (additive): `ledger.Line.Dimensions map[string]string`, keyed by column name. Post
  writes any listed column the table has (unknown name is `ErrInvalidEntry`); Reverse reads
  the optional columns from the catalog and copies every one unchanged, with sides swapped.
  Post also calls `CheckOpen`, so a closed period is `ErrPeriodClosed` before any write.

## Q-JCS-FLOAT: canonical JSON numbers without floats (resolved)

- Resolved: no float formatting. Gate payload numbers are integers within +-(2^53-1), and
  the ES6 form of such an integer is its decimal form, so the one canonical writer
  (`kernel/canon`) formats numbers with integer math only and rejects a fraction, an
  exponent or an out-of-range integer (`canon.ErrNumber`). `kernel/gate` (`JCS`,
  `PayloadHash`, API unchanged) and `kernel/ai/fixture` (`PromptHash`) both use it, and
  neither is in `tools/nofloat_allow.txt`. RFC 8785 Appendix B float vectors are tested as
  rejected inputs.
- Amended (decided): prompts may hold decimals, so `canon.MarshalLiteral` / `canon.LiteralNumber`
  add a literal-number mode that keeps a number's own digits, normalised (zero sign, leading
  and trailing zeros, exponent form), with no float parsing. `fixture.PromptHash` uses it;
  gate payloads stay on the integer-only `canon.Marshal`.

## Q-INTAKE-PARTY: intake/party import cycle

- intake imports party (Leads create party contacts); party/merge.go imported intake for
  `Submission` and `Lead`. Done: party declares two small interfaces, `party.Submission`
  (`SubmittedContactName`) and `party.Lead` (`LeadContact`), which `intake.Submission` and
  `intake.Lead` implement (additive methods in `app/legal/intake/party.go`). party no longer
  imports intake.

## Q-T-007.3 Decide inputs the gate schema does not hold

- Task: T-007.3
- Sources: migrations/0001_init.sql (gate.approval_request stores no payload; gate.gate_decision.decision allows only approve/reject/changes_requested; core.authority_limit.kind values are app terms); ARC-I-016, ARC-I-020.
- Done for now: `PG.SetDecideHooks(DecideHooks{Preparers, Payload, LimitKinds})` in decide.go supplies editors of the underlying item, the payload (re-hashed against payload_hash) and the gate kind -> authority_limit kind map. The hooks sit in a package-level map keyed by *PG, because the PG struct lives in open.go (not this task's file).
- Proposed: (1) move the hooks into a `PG` field at integration; (2) an approval above the limit returns `ErrRecommendOnly` and stores nothing, since the decision CHECK has no 'recommend' value and the trigger counts every 'approve'. Recording recommendations needs an additive table or decision value.

## Q-T035.4 PortalPage.Capability is registered but not enforced

- Task: T-035.4 (kernel/portal page registration)
- Source: `PortalPage.Capability` says "capability the guest's grant must carry", but
  `portal.Guest` (frozen contract) has only `Grants []string` (subject ids), so the shell
  has nothing to check the capability against.
- Done: `RegisterPage` requires a module-prefixed capability (portalpage schema); `Load`
  enforces the session and the subject grant only.
- Question: add `Guest.Capabilities []string` (additive) so `Load` refuses a page whose
  capability the guest does not carry, failing closed when the list is empty?

## T-011.2: trust book posting and files beyond the task list

- Files: added `app/legal/trust/receipt_test.go` (unit tests against an in-memory store, as
  the owner asked) and this entry; neither is in the task's file list.
- Trust book: the migration header (CF-02) says `trust.trust_transaction` is the book of
  record, TR-1500/TR-2500 are derived from it and `gl.*` holds the operating book only, and
  the `gl.journal_entry.source_type` CHECK has no trust kind. The task asks to post
  TR-1500/TR-2500 through the GL, so `LedgerStore` takes a `ledger.Ledger` for the trust book
  (book `trust`, source kind `trust_transaction`, source id = txn id, TR-1500 lines carry the
  trust account subject and TR-2500 lines the client ledger). Proposed: wire a trust-book
  ledger, or approve an additive migration adding `trust_transaction` to the source_type
  CHECK plus seeding TR-1500/TR-2500 in gl.account. Until then the integration test uses
  ledgerfake for the trust book and checks BIL-I-009 on the derived views
  (`LedgerStore.CheckJournal`).
- `Balance.AvailableCents` subtracts authorized-not-executed withdrawal holds (ARC-I-025),
  which the contract comment ("Cleared - Protected - Minimum") leaves out. Proposed: amend the
  comment additively.
- Flag: nothing in this task is marked (verify), so no code sits behind
  `trust.bylaw9_verified`; the balance checks always apply.

## T-012.2: withdrawal effect files and split with T-012.3

- Files: added `app/legal/trust/withdraw/effect_test.go` (unit tests on trustfake and an
  in-memory store, as the owner asked) and this entry; neither is in the task's file list.
- `Effect.Apply` is the trust.withdrawal EffectFunc (authorisation by a signatory, never the
  preparer or an agent, balance re-checked). `Effect.Execute` posts the disbursement through
  `Disburse` (ledger lock + re-check) when a different user records execution, with the
  sole-practitioner exception audited. T-012.3 (execute.go, RecordExecution) should call
  `Effect.Execute` rather than post on its own.
- `Apply` re-checks with `Ledgers.Balance`, which reads outside the caller's tx; the locked
  re-check happens at `Execute` and in the DB guard (DM-064). Proposed: an additive
  `Ledgers.BalanceTx(ctx, tx, id)` if the authorisation check must run under the lock too.
- Branch: task/T-012.2 is cut from task/T-011.2 (it uses `trust.LedgerStore`); merge T-011.2
  first.

## T-024.1: interest start day (day after months_after, or months_after itself)

- The frozen contract (`Calendar.AccrualStart`) and the T-024.1 accept line start interest on
  the day after `months_after(delivery, 1)` (delivered 2026-01-15 -> accrues from 2026-02-16).
  BIL-C-073 and BIL-L-008 say interest accrues for days `>= months_after(d, 1)` (2026-02-15).
- Done: `interest.Start` follows the contract and accept line, tagged (verify), behind
  `billing.solicitors_act_verified`. Counsel must confirm which day is correct.
- Integrator: add `interest.FlagSolicitorsActVerified` to `TestFlagMirrorsMatchCanonicalNames`.

## Wave 2a integration: additive contract changes (decided)

- app/legal/billing/draft contract: new sentinel `ErrInvalidReduction`. A reduction must be >= 0;
  a write-up is its own explicit action (T-017.5 decision). Additive.
- kernel/docs contract: the `Citation.QuoteHash` comment now states the ARC-I-037 form
  ("sha256:" + hex SHA-256 of the normalised quote). docsfake hashes the same way. Comment only.
- Resolved at wave 2a integration: interest runs from `months_after(delivery, 1)` itself
  (Solicitors Act s.33). The contract comment, `Months.AccrualStart` and tests now say so.

## Q-CLEARANCE-BOOT: no boot place to wire conflict.Clearance with identity capabilities

- Decision (T-029.2): the only `conflict.Clearer` is `Clearance` (gated), and its
  `Capabilities` must come from the identity registry at boot.
- Blocker: there is no runtime boot wiring yet. `app/legal.Register(registry.Registry)` only
  declares kinds, gates and capabilities; it receives no identity service, store or clock, and
  `cmd/counseld/main.go` only parses flags. `kernel/identity` also has no per-user lookup
  (`Capabilities(ctx, userID) ([]string, error)`); `Access` answers only `Can(...)`.
- Question: where does service construction live (a `legal.Wire(deps)` called from counseld, or
  a kernel boot container), and should `kernel/identity` gain an additive per-user capability
  lookup, or should `conflict.Capabilities` be satisfied by an adapter over `Access.Can`?
  Until then `NewClearance` is wired only in tests.
- Answer: owner (2026-10-11): `app/legal/boot` builds Clearance with Capabilities from a kernel
  `identity.CapabilityLookup`. The lookup is generic: it reads the registered roles and the role
  assignments and returns the capabilities a user holds. It fails closed for unknown users.
  `core.*` kinds are registered by kernel/identity's own admin gates in kernel boot. Done in
  BOOT-17: `kernel/identity/capabilities.go` (additive: `CapabilityLookup`, `ErrUnknownUser` for a
  user with no roles, `ErrUnknownRole` for an unregistered role) and `app/legal/boot/clearance.go`
  (`Options.Clearance` builds `conflict.NewClearance(checks, catalog, lookup, clock.Now)` over the
  catalog BOOT-16 keeps). `kernel/identity` is outside BOOT-17's file list; the owner's answer
  names it.

## Q-GL-BOOK Trust book in gl: book column, trust source kind, per-book close

- Task: T-011.2, T-010 follow-up; CF-02.
- Sources: 0001 header ("gl.* holds the operating book only"); By-law 9 (trust records kept
  separately); `ledger.Entry.Book`; `books.BookOperating`, `books.BookTrust`.
- Answer: owner (2026-10-11): additive migration `0400_ledger_book.sql` (kernel/ledger).
  `gl.account.book` and `gl.journal_entry.book` (default `operating`), source kind `trust`
  appended to the `source_type` CHECK, and a per-book close in the new `gl.book_period`
  (book, month). `gl.period` stays and keeps meaning the operating book; `gl.period_closed`
  and the DM-062 trigger check the entry's book. The trust ledger posts with book `trust` and
  source kind `trust`. CF-02 is reread as "operating views show the operating book only":
  `gl.operating_journal_entry` and `gl.operating_journal_line`. The book CHECK is a format
  check, not `IN ('operating','trust')`, because the kernel engine knows no book name (the
  app's chart registers them). This supersedes "a period close covers all books".

## Q-T-007.3-SCHEMA Gate payload bytes and the recommend decision

- Task: T-007.3 (see Q-T-007.3 above).
- Answer: owner (2026-10-11): additive migration `0300_gate_payload_recommend.sql`
  (kernel/gate). `gate.approval_request.payload_jcs` (JCS bytes) and `payload_sha256`
  (= sha256(payload_jcs) = payload_hash, checked in the DB). `gate.gate_decision.decision`
  gains `recommend`; `gate.check_decision` records it and leaves the request pending. Open
  stores the payload; Decide reads the stored payload (the app hook only serves rows opened
  before the migration) and records an approval above the authority limit as `recommend`
  instead of returning ErrRecommendOnly and storing nothing. A recommendation carries a fixed
  reason because `reason` is required for every non-approve decision.

## Q-T-008.4 Gap-free subject_seq on the outbox

- Task: T-008.4.
- Answer: owner (2026-10-11): additive migration `0500_outbox_subject_seq.sql`
  (kernel/outbox): `platform.outbox.subject_seq bigint`, unique per (aggregate, aggregate_id)
  where set. T-008.4 is requeued; its integration test stays on hold.

## Q-T-036.2 Webhook delivery subject, message and acknowledgement

- Task: T-036.2.
- Answer: owner (2026-10-11): additive migration `1400_webhook_delivery_subject.sql`
  (kernel/webhook): `platform.webhook_delivery.subject`, `message_id`, `payload_ref` (the
  outbox row id; no raw payload) and `acked_at`. T-036.2 is requeued.

## Q-T-034.1 Work items on generic subjects

- Task: T-034.1.
- Answer: owner (2026-10-11): additive migrations `1000_work_item_subject.sql` (kernel/work:
  nullable `work.work_item.subject_kind`, `subject_id`) and
  `6100_work_item_subject_backfill.sql` (app/legal/work: backfill `('matter', matter_id)` in
  batches of 10000). The backfill sits in the app block so no legal term enters a kernel
  migration. `matter_id` stays for app/legal. T-034.1 is requeued.

## Q-T-035L.2 Court filing table

- Task: T-035L.2 (see T-035L.2 above).
- Answer: owner (2026-10-11): additive migration `6000_court_filing.sql` (app/legal/comms):
  schema `legal`, table `legal.court_filing` (court, reference, status draft|filed|rejected,
  document, filed_by, filed_on), RLS forced like every matter table. T-035L.2 is requeued.

## Q-T-008.2 Idempotency-Key on PATCH /webhook-endpoints/{webhook_id}

- Task: T-008.2.
- Sources: every `x-external-effect` operation must take `Idempotency-Key`; renewWebhookEndpoint
  had only `If-Match`.
- Answer: owner (2026-10-11): option A. Add the `IdempotencyKey` parameter (additive; listed in
  kernel/contract/additive.txt). T-008.2 is requeued with its draft.

## Migration numbering for the 2026-10-11 additive schema

- The changes above were asked for as one `0002_additive.sql`. migrations/README.md and
  tools/migration_blocks_test.go reserve 0001-0099 for `0001_init.sql` alone and require each
  file in its owning module's block, so they ship as one file per owner: 0300 (kernel/gate),
  0400 (kernel/ledger), 0500 (kernel/outbox), 1000 (kernel/work), 1400 (kernel/webhook),
  6000 (app/legal/comms), 6100 (app/legal/work). The Go package `migrations` embeds them
  in order for counsel-migrate and the integration tests.

## T-003.2: Allocate moved out of money.go

- T-003.1 already shipped `Allocate` in `kernel/money/money.go`. T-003.2 moves the body,
  unchanged, into `allocate.go` (where the contract header says it lives), so it also touches
  `money.go` (deletion only) and this file. New tests are in `allocate_test.go`; the existing
  `TestAllocate` in `money_test.go` is kept as is.

## T-014.2: place of supply (additive contract change; decisions to confirm)

- Additive change to the frozen `app/legal/billing/tax` contract: `Supply` gains
  `PropertyProvince`, `CourtProvince`, `RecipientResident`, `RecipientBusiness`, `MetInCanada`,
  `AgentRole`, `RegisteredProvinces`; `Line` gains `Label` ("RST" for MB PST). Existing fields
  and callers are unchanged. `RecipientBusiness` is not in the T-014.2a accept list: s.23(a)
  zero-rates a non-resident business in Canadian litigation but taxes an individual, so the
  engine must know which. Its zero value (individual) is the taxed, fail-closed side.
- T-014.2a sets `Line.Label` in `compute.go` (not in its file list): Compute is the only place a
  line is built.
- Order (verify): BIL-T-010 and SOR/2010-117 s.27 (Div. 5 applies despite Divs. 2-3) put
  litigation before real property, so a court province beats a property province. The
  T-014.2b accept line lists property first; the engine follows BIL-T-010.
- Fail closed (verify): non-resident treatment needs `RecipientResident == false` and a
  non-Canadian `ClientProvince` code (foreign address on file, BIL-T-015 enforcement). A
  recipient that is not flagged resident but has a Canadian address is taxed by that address;
  no address at all is `ErrUnknownProvince`. Counsel to confirm.
- Rows marked (verify) in b2 (BC/MB/SK/QC places, federal courts, agent role) return
  `ErrUnverified` while `tax.verify_rules` is off.
- T-014.2c and T-014.2d each replace a fail-closed placeholder in `pos.go` with a call into
  their own file (pos.go is not in their file lists).

## T-019.1d-f: bill approval effect wiring, fees book storage, BN fail-closed

- Fees book storage: 0001_init.sql has no fees book table. `ApproveEffect` writes the row
  through `FeesBookWriter` in the gate tx, but there is no Postgres writer yet. Options:
  (a) an additive `billing.fees_book` table (append-only, voids kept), or (b) treat the
  numbered `billing.bill` rows as the register (bill_guard already refuses deleting a
  non-draft bill and keeps voids) and drop the separate write. The integration test uses a
  test-only table to prove the row commits and rolls back with the gate tx.
- Registration: gate kinds are registered centrally by `gates.RegisterKinds`, so
  `bill.Effects(*ApproveEffect)` returns the `billing.bill_approval` handler (as
  `signatory.Effects` does) instead of a second GateKind registration in bill/register.go.
  Integrator: pass it into the RegisterKinds effects map at boot.
- Integrator: add `bill.FlagBillingRecordsVerified` to `TestFlagMirrorsMatchCanonicalNames`.
- `CheckDeliverable` uses `tax.BNChecker`, which fails closed: while `tax.bn_check_verified`
  is off, a GST/HST registrant cannot deliver any bill (tax.ErrUnverified). Confirm this is
  intended before the deliver route is wired, or turn the flag on once BIL-T-061 is verified.
- BIL-I-008 has its text in spec/eng/b3-billing-engine.json; `CheckNumbering` implements it.
  It also lets a void bill have no number (voided before approval), as the 0001 CHECK does.

## T-043.1c: app.yaml touched beyond the task file list

- Task: T-043.1c
- Sources: ARC-U-005, ARC-U-041; T-043.1c lists quota.yaml and values.yaml only.
- Reading A: the app pods' priority class, node pool and per-container requests/limits live in templates/app.yaml, so the accept lines cannot pass without touching it.
- Proposed: done. app.yaml Deployments get priorityClassName counsel-<cell>-app, the app pool nodeSelector and toleration, and resources from values. No other change.
- Answer: (owner)

## T-043.4b: lock_timeout limit differs between sources

- Task: T-043.4b (also T-043.4c)
- Sources: MIG-061 says lock_timeout = '3s'; ARC-U-018 and the task say 2 s.
- Decision taken (owner instruction, strictest value, as T-043.1f did): tools/migration_lint.py
  accepts lock_timeout > 0 and <= 2s, and the migration Job sets PGOPTIONS lock_timeout=2s.
  Any value that passes also meets MIG-061 (<= 3s). The task's accept line says the lint
  accepts <= 3s; that is looser than the owner's rule, so the lint uses 2s.
- Answer: (owner)

## AI-034.0a: Embed moved out of aifake/fake.go

The task lists only `kernel/ai/aifake/embed.go`. `Fake.Embed` was defined in `fake.go`, and a
method cannot be defined twice, so it was deleted there and rewritten in `embed.go`. `Vector`
stays in `fake.go`; no other change to it.
`tools/nofloat_allow.txt` gains `embed.go` and `embed_test.go`: embedding vectors are float32
by the `ai.EmbedResponse` contract, the same reason `fake.go` is listed.

## AI-034.0b: Embed journal field on aifake.Fake

The Embed journal needs a slice on the `Fake` struct (guarded by its existing mutex), which is
declared in `fake.go`; that one field is the only change outside the task's files.

## Q-INTEREST-CJA: prejudgment rate when no rate was agreed (integrator, wave 3a)

The decision log says interest with no agreed rate should run at the Courts of Justice Act
prejudgment rate for the quarter of delivery. The frozen rules say the opposite: BIL-C-071
("the Act supplies no default rate. Default: off"), BIL-L-009 (the rate must be printed on the
bill) and BIL-L-010 ("the Act now sets no default rate"). Rule ids outrank decisions, so the
integrator did not change the no-rate behaviour (ErrNoAgreedRate stays). It seeded the
quarterly CJA prejudgment table (2020 Q1 to 2026 Q4, from ontario.ca, verify) as
`interest.PrejudgmentRate`, usable as the agreed rate's CJA benchmark. Needed: a lawyer reads
s.33 as amended and either the rules or the decision change.
- Answer (lawyer, 2026-10-11): Solicitors Act s.33 ties interest to the CJA s.127 prejudgment
  rate, so the frozen rule text is incomplete. Rule behaviour stays the default. New flag
  `interest.cja_default_rate` (verify, default off): when on, a bill with no agreed rate uses
  `interest.PrejudgmentRate` for the Toronto quarter of delivery (`interest.EffectiveRate`).
  Answered.


## T-020.1c: bill void effect

- Approval entry: the billing.bill_approval effect posts no BIL-J-003 journal entry yet, and
  kernel/ledger has no lookup by source. `VoidEffect` finds the entry to reverse through the
  `ApprovalEntries` port, which has no Postgres implementation. Options: (a) the approval
  effect posts BIL-J-003 and stores its entry id on the bill (additive column), or (b) an
  additive `ledger.FindBySource(ctx, tx, kind, id)` over the existing source index. The
  integration test posts the approval entry itself.
- Extra file: `void_effect_test.go` holds the unit tests and the route-table guard
  (TestBIL_C_084_OnlyGateVoids), so they run in `make check`; the listed
  `void_integration_test.go` is integration-only.
- Integrator: pass `bill.VoidEffects(*VoidEffect)` into the gates.RegisterKinds effects map
  at boot, as for `bill.Effects`.

## BIL-J-003: bill approval journal entry

- Resolves the T-020.1c approval-entry question with option (b) inside the bill package:
  `ApproveEffect` now posts BIL-J-003 through `Ledger` (book operating, source `bill`/bill id,
  dated the bill date) in the gate tx, and `PGStore.ApprovalEntry` finds it by that source
  for the void. No contract or migration change; `TaxLine` reads `billing.bill.tax_breakdown`.
- Lines posted: Dr OP-1100 total; Dr OP-4900 discount and negative fee adjustments; Cr OP-4000
  per fee line; Cr OP-4010 taxable (own-cost) disbursements; Cr OP-1210 agency disbursements at
  cost (they were debited to OP-1210 when paid, BIL-J-002); Cr OP-2100 (GST/HST) or OP-2110
  (PST/QST) per tax line. Lines and tax lines must equal the bill's frozen totals or the
  approval fails.
- Not yet posted: the WIP relief pair (Dr OP-1290 / Cr OP-1200 at recorded value) and
  write-downs to OP-4900. Both need the time entries' recorded value, which the bill does not
  carry. Until then OP-1200/OP-1290 keep billed time; the pair nets to zero, so AR, revenue
  and tax are right.
- Integrator: `ApproveEffect` refuses to run without `Journal` (PGStore{}) and `Ledger`
  (*ledger.PG); `VoidEffect.Entries` is PGStore{}. The chart (books) must be synced into
  gl.account before the first approval.


## fix/trust-auth-race: withdrawal authorisation locks the client ledger

- Effect.Apply read `Balance` in its own transaction without a lock, so two concurrent
  authorisations could both pass and together overdraw a client ledger (By-law 9). Apply now
  locks the withdrawal, then the client ledger row FOR UPDATE in the gate's tx (the order
  Execute/Disburse use), re-checks `trust.AvailableCents` net of authorised holds and records
  its hold in the same tx.
- Files outside withdraw/: `withdraw.TrustLedgers` swaps `Balance` for
  `LockLedger(ctx, tx, id) (Balance, held, error)`, so `trust.LedgerStore` (receipt.go) and
  `trustfake` gain that method (additive; the frozen `trust.Ledgers` contract is unchanged).

## T-012.5: EFT check wired into the withdrawal effect

- The Form 9A check (eft.go `CheckEFT`) must run before an EFT posts, which happens in
  `Effect.Execute` (effect.go, not in T-012.5's files). effect.go gains an `EFT EFTRecords`
  field and one call to `CheckEFT` before `Disburse`. While `EFT` is nil every EFT execution
  fails closed with `eft_authorization_missing`.
- Follow-ups: a Postgres `EFTRecords` over `form_9a_doc_id`/docs.document; `problemFor`
  (execute.go) should map `ErrNoEFTAuthorization` to 409 `eft_authorization_missing`; the
  trust.transfer gate effect should call `Transfers.Post`; and `sumsSQL` (trust/receipt.go)
  leaves out authorised `trust_transfer` holds that `trust.available_cents` subtracts.


## Q-T-011.4 Trust exception table for NSF reversals

- Task: T-011.4. Sources: b3 BIL-C-065 ("a reversal of a trust deposit always opens a trust
  exception"), task accept line ("trust exception opened"). 0001 has no table to hold one.
- Answer: owner (2026-10-11): additive migration `5500_trust_exception.sql` (app/legal/trust,
  first file in its block): `trust.exception` (kind `nsf`, receipt id, ledger and matter link,
  amount cents, shortfall cents, opened_at/by, resolved_at/by, resolution), one row per
  (kind, receipt), no delete. `migrations/migrations_test.go` lists the new file. ReverseNSF
  opens the row in the reversal's tx; the replenishment's operating leg is
  Dr OP-1100 / Cr OP-1000 with source kind `nsf_replenishment`. Resolving an exception is a
  later task.


## Q-T-011.5 Ledger balance guard mapping

- Task: T-011.5 (BIL-I-010, DM-052, DM-064).
- DM-052 is a DEFERRABLE INITIALLY DEFERRED constraint trigger, so it fails at COMMIT, after
  any insert-level mapping. `ledgerlock.go` adds `LedgerStore.InLedgerTx` and `ledgerCheck`:
  SQLSTATE 23514 whose message starts with `DM-052:` or `DM-064:` becomes
  ErrInsufficientFunds (409 insufficient_trust_funds); deadlocks and other CHECK violations
  pass through unchanged.
- Follow-up for the integrator (receipt.go is outside T-011.5's file list): `insertTxn` maps
  every 23514 to ErrInsufficientFunds, so an unrelated CHECK on trust_transaction (cash payout,
  kind/sign) reads as insufficient funds. Switch it to `ledgerCheck`.
- The 32-goroutine integration test seeds 32 withdrawals, each authorised under its own
  approved trust.withdrawal gate. It is vetted with `-tags integration` and not yet run
  (integration runs are on hold).


## Q-T-031L.4 Limitations Act calculator: additive contract

- Task: T-031L.4 (gap task: the real calculator behind app/legal/limitation).
- Additive to `app/legal/limitation/contract.go`: Assessment fields `DiscoveryPresumed`
  (s.5(2)), `NoLimit` (s.16/s.17) and `ContributionServedDate` (s.18); sentinels
  `ErrUnverified` (flag off) and `ErrSuspended` (open-ended suspension); types `NoLimitKind`,
  `SchedulePeriod` and the `Schedule` lookup hook (s.19, no data yet).
- For the lawyer (all marked (verify) in `app/legal/limitation/statute.go`):
  (a) a Feb 29 anniversary falls on Feb 28 (Legislation Act, 2006 s.89, month-end rule);
  (b) the due date is not moved off a weekend or holiday (conservative; Legislation Act s.88
  may allow the next business day); (c) undiscovered environmental claims cite s.17;
  (d) an s.22 agreement suspends both periods; (e) a Schedule period replaces the basic and
  ultimate periods and ignores suspensions.
- No Assessor or production wiring exists yet, so nothing used the fake outside tests. The
  BOOT task set should construct `limitation.NewStatute(schedule)` as the Calculator.

## Q-BOOT-13-PORTS: trust services need ports no module implements yet

- Task: BOOT-13 (app/legal/boot trust services).
- Gap: on main there is no production `signatory.Service`, `signatory.Directory`,
  `trust.Sealer`, `withdraw.SignatoryCheck`, `withdraw.FirmPolicy` or `withdraw.Auditor`
  (only fakes and test types), and `*ledger.PG` has no `Balance`, so it is not a
  `ledger.Ledger` on its own (`ledger.Balances` has it).
- Decision taken: `trustServices(k, TrustPorts)` builds `trust.AccountStore`,
  `trust.LedgerStore` and `*withdraw.Effect` (store `withdraw.PGStore{}`) from the module
  constructors and takes the missing ports from `TrustPorts`; each nil port fails boot naming
  it. The AccountStore is the LedgerStore's `SignatoryGuard`.
- Needed: tasks for the identity directory adapter, the account-number sealer, a Postgres
  signatory service, the firm-policy reader, the withdrawal auditor and a kernel ledger value
  that joins `PG` and `Balances`. BOOT-16 fills `TrustPorts` from them.
- Answer: (owner)

## Q-PORT-19: the conflict index version must not be a snapshot xmin

- Task: PORT-19 (app/legal/conflict PGSearcher). The first pass reported
  `pg_snapshot_xmin(pg_current_snapshot())` as `IndexVersion`; that moves with any
  transaction in the cluster and says nothing about the name index.
- Answer (owner, answered): add a real conflict index version, a sequence bumped on party and
  alias writes, additively. Done in `migrations/5201_conflict_index_version.sql`
  (app/legal/conflict): `conflict.index_version_seq` and a one-row `conflict.index_version`
  set to `nextval` by a statement-level SECURITY DEFINER trigger on writes to
  `party.contact` names and aliases, `matter.party_role` and `conflict.sealed_prospect` (all
  three change search results). The row is written in the writer's transaction, so a search
  sees committed versions only; it reads the version first, so the hits are never older than
  it. Cost: name writes serialize on that one row.

## Q-PORT-20: cal.deadline cannot hold a kernel deadline without an additive migration

- Task: PORT-20 (kernel/deadline Postgres store). Stopped; no code written.
- Problem: `kernel/deadline.Proposed` is (PackID, PackVersion, RuleID, Trigger date, Due
  date, Subject{Kind, ID}). `cal.deadline` (0001) has none of PackID-as-text, PackVersion,
  RuleID, Trigger date or a generic subject pair. Its link is `matter_id` XOR `lead_id`
  (a CHECK), `title` and a legal `kind` enum are NOT NULL, and the pack is a uuid FK to
  `cal.rule_pack` (whose `province` and `kind` columns are legal). Kernel code may not name
  `matter` in SQL (tools/kernel_terms.txt), so a kernel store cannot satisfy the CHECK.
  Repack also needs the trigger date and rule id to recompute, which the table does not keep
  (`trigger_id` points at `cal.trigger_event`, itself matter-only).
- Proposal (additive, same pattern as Q-T-034.1 work_item subject):
  1. Kernel migration: `cal.deadline ADD COLUMN subject_kind text, subject_id uuid,
     pack_code text, pack_version text, rule_key text, trigger_on date` (all nullable,
     pair CHECKs NOT VALID then VALIDATE), and `cal.deadline_confirmation` unchanged.
  2. App-block migration: a BEFORE INSERT trigger that sets `matter_id`/`lead_id` from
     `(subject_kind, subject_id)` when the kind is the app's matter or lead kind, and fills
     `title`/`kind` defaults (`'other'`) when the kernel leaves them empty; plus a backfill
     of the new columns from existing rows.
  3. PORT-20 then reads/writes only the generic columns; `rule_pack_id` stays NULL for
     kernel-saved rows (the 0001 CHECK allows it when `rule_cite` is NULL).
- Also needed: the Engine contract's Save/Confirm take a tx but Get does not; PGBook.Get
  would run its own InTx. Confirmations/Repack are Book methods, not Engine methods, so
  they would take a tx too (additive, PGBook only).
- Answer (owner, answered): approved. Additive cal.deadline migration with a generic subject
  (kind, id), pack code and version, rule id and trigger date; an app-block trigger sets
  matter_id/lead_id from the subject. Done in PORT-20: `migrations/1100_deadline_subject.sql`
  and `1101_deadline_subject_validate.sql` (kernel/deadline: nullable `subject_kind`,
  `subject_id`, `pack_code`, `pack_version`, `rule_key`, `trigger_on`; pair CHECKs NOT VALID,
  validated in the later file), and `5900_deadline_subject_link.sql` (app/legal/deadlines:
  BEFORE INSERT trigger mapping the pair to matter_id/lead_id and back, `title` from the rule
  key, `kind` 'other'; batched backfill of the pair). No index on the new columns yet: the
  lint needs a separate no-transaction CONCURRENTLY file, which `migrations.Apply` (one
  batch per file) cannot run; add one when Repack volume needs it. `PGBook` takes a tx for
  Save, Confirm, Confirmations and Repack; Get runs its own transaction.

## BOOT progress

BOOT tasks are not in docs/TASKS.md, so their progress is tracked here.

- Merged to main (wave 4a): BOOT-01, BOOT-02, BOOT-03, BOOT-04, BOOT-05, BOOT-06, BOOT-08,
  BOOT-10, BOOT-11, BOOT-12, BOOT-13, BOOT-14, BOOT-15.
- Merged to main (wave 4b): T-009.4, BOOT-07, BOOT-09, BOOT-16, BOOT-17, BOOT-19, BOOT-20,
  BOOT-22, BOOT-23; with PORT-01, PORT-02, FEAT-CORE-01, FEAT-CORE-04, FEAT-TOOL-02,
  FEAT-TOOL-03, FEAT-TOOL-04, FEAT-TOOL-07, fix/w3-gates and fix/w3-legal.
- Applied at integration (logged decisions):
  - `identity.NewAdminGates(gates, store, opts...)` with `identity.WithCoreEffect(kind,
    effect)`: the app supplies handlers for core.* kinds the kernel owns.
  - Kernel boot builds them: `(*boot.Kernel).AdminGates(opts...)` calls
    `identity.NewAdminGates` on the kernel gate engine and `Deps.AdminStore` and keeps the
    result in `Kernel.Admin`. The legal app calls it with `boot.CoreGateEffects(svc)` when it
    builds its effect table, so the core.trust_signatory effect is the kernel admin gates' one.
  - `boot.LimitationStatute(schedule)` attaches `ontario.Holidays()` (embedded
    holidays.yaml) with `WithHolidays`, so the s.88 roll applies; tested on 2027 holidays.
    `boot.LimitationAssessor(schedule, store, deadlines)` builds the Assessor on it.
  - `import.source_sync` is defined in app/legal/flags.go (`legalflags.ImportSourceSync`),
    default off; the renewal job reads it.
  - `report.SlotWorker` takes a `report.Engine`; the app's report.slot worker is
    `report.SlotWorker` on the kernel Engine and DB, and its own copy is gone.
  - `withdraw.PGEFTRecords` has a unit test on a fake DB. Its integration test is still
    to be written (vet only).
  - Kernel boot `Run` now uses the real kernel job adder (gate.run_due, outbox.relay); the
    BOOT-09 stub is gone.
- Merged to main (wave 4c): fix/w3-trust, fix/w3-deploy, fix/migrate-role (migrate role
  expand/contract and wait-version), PORT-03, PORT-04, PORT-05, PORT-06, PORT-08, PORT-09,
  PORT-13, PORT-14, HTTP-00, HTTP-01, HTTP-02. Applied at integration:
  - `Run` builds the outbox relay over the request pool and calls `addNotifyJobs` and
    `addWebhookJobs` after every app's Register.
  - The legal app schedules `trust.withdrawal_release_expired` (PGStore.ReleaseExpired)
    every 15 minutes when gate services are built.
  - `billing.bill_delivery` is in spec/gates.json (unwired); idem problems carry `type`;
    the billing/bill integration helper applies every migration.
- Still open: register the kernel admin kinds (core.role_change, core.user_activation) with
  the admin gates' effects once a Postgres AdminStore exists; build the Assessor from the
  app's Register once limitation ports have production implementations.


## Q-MIG-LINT-SHIPPED Migration lint on migrations/: edits in place on unreleased files

- Review item 19 (migrations) and 18 (deploy). `make migration-lint` and
  `TestMIG_061_ShippedMigrationsPass` now lint every numbered file in migrations/. Only the
  frozen `0001_init.sql` is baselined (`tools/migration_lint_baseline.txt`, two lines: no
  timeouts; it builds an empty database). The lint itself is unchanged except `--baseline` and
  skipping non-numbered files (smoke.sql).
- The lint is per file, so a forward migration cannot make an earlier failing file pass. No
  release has shipped and no cell has run the migrate Job (boot's migrate role is not
  implemented yet), so these unreleased files were edited in place, each with its fix in a
  new forward file:
  - 0300, 0400, 0500, 1000, 1400, 5500, 5600, 6000, 6100: add `SET lock_timeout = '2s'` and
    `SET statement_timeout = '5min'` (RESET at the end).
  - 0300, 0400, 0500, 1000, 1400: VALIDATE CONSTRAINT moved to 0301, 0401, 0501, 1001, 1401.
  - 0400, 0500, 1000, 1400: CREATE INDEX moved to no-transaction files 0402, 0502, 1002, 1402
    (`CREATE INDEX CONCURRENTLY IF NOT EXISTS`).
  - 1400: `payload_ref ... REFERENCES platform.outbox(id)` inline became
    `webhook_delivery_payload_ref_fk ... NOT VALID`, validated in 1401.
  - Forward only: 5501 narrows the trust.exception UPDATE grant to (resolved_at, resolved_by,
    resolution).
- A dev database migrated before this change has no `schema_meta.applied` and would re-run
  0001; recreate it.
- Still open: 6100 is a backfill in a migration (ARC-U-018 says backfills never run there);
  move it to a kernel/migrate backfill job. The migrate Job still uses the app DB secret;
  give it a dedicated migrator role secret.

## Q-HELM-NAMES Cell namespaces use Counsel names (ARC-U-001)

- ARC-U-001 (spec/eng/a1-uptime.html) lists namespaces counsel-<cell>, paploo-<cell>,
  paploo-<cell>-agents. The chart now creates counsel-<cell>, counsel-<cell>-runtime (agent
  runtime) and counsel-<cell>-agents (agent containers), plus counsel-<cell>-agents quota,
  limit range and counsel-<cell>-agent-scheduling. Owner decision (W3 review item 18): no other
  product name in this repo. The spec text needs the same rename; it was not edited here.

## Q-HELM-MIGRATE First install and the migrate Job

- Decision (autopilot): the migrate Job is a `pre-upgrade` hook only. On install it is an
  ordinary release resource (`counsel-<cell>-migrate-install`), because a pre-install hook runs
  before its namespace, service account, priority class and database secret exist. The chart
  fails on any other hook value.
- Every Deployment has a `wait-schema` init container: `counsel-migrate -mode=wait-version`
  waits, with the app credentials, until `schema_meta.applied` holds the newest migration
  built into the image.

## T-009.4: concrete server, two-way OpenAPI check, unserved list (owner decision)

The owner unblocked T-009.4 before every openapi path has a handler. Files outside the task:

- `api/unserved.txt`: the openapi operations no handler serves yet. The owner asked for
  `kernel/contract/unserved.txt`, but its lines are openapi paths with legal terms and the
  kernel term lint (TestKernelHasNoLegalTerms) scans every file under kernel/; the allow list
  only shrinks, so the file sits next to `api/openapi.yaml` instead. It only shrinks; the conformance tests fail when a listed operation is mounted, when it leaves
  openapi.yaml, and when an operation is neither mounted nor listed.
- `app/legal/conformance_test.go`: the full two-way check has to see app routes, and kernel
  code cannot import app/. It mounts every handler on main (kernel inbound hooks, trust
  withdrawal execution) plus every `Registry.Routes` call `legal.Register` makes.
  `kernel/server/conformance_test.go` checks the kernel routes one way.

Decision taken: `server.New` mounts module routes under `server.BasePath` ("/v1", the openapi
server URL path), and `Routes()` reports the full served pattern. Modules keep mounting
openapi-relative paths, as `kernel/inbound` and `withdraw.Execution` already do.

Left open: the second accept line (refuse a route that sets state owned by a gate effect
unless it is the gate decision endpoint) needs a per-route "gate-owned state" marker that no
contract declares yet. Not built.

## HTTP-00: gate-owned-state guard (files outside the task, decisions taken)

- Write declarations live next to the route: `r.Method(m, path, server.Declare(h, keys...))`.
  `server.DeclaredWrites` walks the mounted router (unwrapping `chi.ChainHandler`) and
  `server.CheckWrites` refuses an undeclared POST/PUT/PATCH/DELETE route and any declaration
  naming a `legal.GateOwned` key, except on `server.DecisionRoute`. Kernel mounters that do not
  use Declare (inbound hooks) are declared in `app/legal/conformance_test.go` (`kernelWrites`).
- `kernel/gate/decide.go` (not in the task's files): `runDecidedEffect` now hands the effect
  `WithEffect(ctx, g.ID)`, so "set only by the effect runner" holds. One line.
- `app/legal/register_test.go`: `MethodRoutes` joins the "always" set. Routes always mount;
  each handler fails closed per request (the withdrawal execution route answers 503 until boot
  wiring supplies its dependencies, and trust_rules_unverified while its flag is off). The
  OpenAPI conformance test needs every route mounted with no features on, so routes cannot be
  feature-gated at registration.
- Fakes: `matterfake` (Create, AddMember) and `withdrawfake` (Authorize) gain
  `GuardEffects(tb)`: effect-owned methods fail tb and return `gate.ErrOutsideEffect` outside
  an effect. Other module fakes gain it when their handler task lands.
- Q-HTTP-00 (open): `bill.delivered_at` is effect-owned (openapi deliverBill: "set only by the
  delivery effect on evidence"; 0001 trigger RV1-008 mentions an approved delivery_correction),
  but spec/gates.json has no delivery or delivery-correction kind. It is mapped to
  `billing.bill_delivery`, listed pending and in the test's `unspecifiedKinds`. Needed: add the
  kind to spec/gates.json (additive) or name the existing kind that owns it.
- Mapping choices to confirm: write_off.status=approved -> billing.credit_note (the openapi op
  "Request a credit note or write-off; opens gate billing.credit_note"); matter.team and
  matter.restricted -> matter.widen_access; matter.status=closed -> platform.closing;
  trust_account.signatories -> core.trust_signatory; trust_txn.posted is split into
  `=receipt` (trust.receipt_classification) and `=transfer` (trust.transfer).

## HTTP-01 / HTTP-02: matter routes (decisions taken, open items)

- Routes register a zero-value `matter.HTTP` / `matter.Writes` through `Registry.Routes`;
  every route answers 503 `unavailable` and calls nothing until boot wiring supplies the
  dependencies (same as the withdrawal execution route). The BOOT tasks must inject them.
- GET /matters: one capability check per request (ARC-I-042); walled and restricted matters
  are left out by Query under the caller's RLS session, not by per-row checks. openapi
  declares no `cursor`/`limit` parameters and no 200 schema for /matters; the handler takes
  both (api.FromRequest) and the 200 bodies are not schema-checked.
- POST /matters: the rule wins over the task text. Same key + different body returns 422
  `idempotency_key_reused` (ARC-I-008), not 409. kernel/idem's own problem bodies omit the
  required `type` field of the Problem schema; the handler checks the missing key itself so
  that 400 validates, but the 422/409 bodies from `idem.New` do not (fix in kernel/idem).
- POST /matters opens `matter.opening` with subject `party.contact`/client_id: the matter
  row does not exist until the effect creates it. Needed: the matter.opening effect
  (service gap) and a confirmation of the subject.
- PATCH: plain-field writes go through a new `matter.Updater` port (declared in
  http_write.go, not the frozen contract); its Postgres implementation is a service gap.
  status=open opens matter.opening; closing/closed/archived open platform.closing (verify);
  team and restricted open matter.widen_access. One gated change per request; mixing plain
  and gated fields is 400.

## BOOT-09: Run wiring gaps

- Additive `Config` fields beyond `Zone`: `DirectDatabaseURL` (db.Open validates both DSNs and
  the worker needs the direct one, ARC-U-011) and `MaxWorkers` (default 10). Server and worker
  need `DatabaseURL`, `DirectDatabaseURL` and `Zone`. `Kernel` and `Deps` gain `Notify`.
- `gate.PG` does not implement `gate.Engine` yet (no Get, Withdraw, RunDue), so the production
  server and worker roles stop with `ErrNotImplemented` at the gate engine until it does.
- Registry schemas load from `spec/kernel` relative to the working directory; the images
  must ship it, or the schemas get embedded.
- Reports use a deny-all authorizer and an audit log with no Verify querier until identity
  is wired at boot. No kernel module owns a River job yet, so `addKernelJobs` adds none.
- `TestRunFlags` now runs the import role and declares its flag in `Define`: Run builds
  through BuildWith, which turns flags on before Register (ARC-I-040).

## Q-BOOT-16-FILES: BOOT-16 touches app/legal/boot/boot.go

- Task: BOOT-16. Files listed: `effects.go`, `unwired_gates.txt`, `effects_test.go`.
- Reason: "Register calls gates.RegisterWired" needs a Register hook, and the hook list and
  `Options` live in `boot.go`. The change is additive: `Options.Gates *GatePorts` (nil keeps
  the old behaviour: no app gate kind registered), and `App` builds a `wiring` value that
  Register fills with the gate Catalog so a later step (BOOT-17 Clearance) can read it.
- Wired: signatory (core.trust_signatory is ignored by RegisterWired, the kernel owns it),
  trust.withdrawal, billing.bill_approval, billing.bill_void (bill.VoidEffects, per the
  integrator decision) and the two write-off kinds. CreditPorts come from the caller
  (no Postgres write-off store yet, Q-BOOT-13-PORTS).

## Q-BOOT-22 notification templates: Options.Sender and the app template list

- Task: BOOT-22 (app/legal/boot notification templates).
- Outside the task's files: `app/legal/boot/boot.go` gains `Options.Sender notify.Sender`
  (additive; the accept line names it) and `opts.registerNotify` in `registerHooks`.
- No app module defines kernel notification templates yet (the comms message templates are
  a separate store, seeded by BOOT-23). The app list in `notify.go` holds one template,
  `sync.subscription_renewal_failed`, for the cliosync renewal alert (ids and times only).
  Modules that need notifications add theirs to `notifyTemplates`.
- Answer: (owner)

## Q-BOOT-23 template seed: Start hook and test fake

- Task: BOOT-23 (app/legal/boot seed message templates at Start).
- Outside the task's files: `app/legal/boot/boot.go` `startHooks` now returns `seedTemplates`.
- A kernel with no DB (`kboot.Build` with no deps, used by `Run` today) skips the seed, so
  `TestAppShape`'s empty-kernel Start stays valid; with a DB, a seed error stops Start.
- `commsfake` has no versioned seed (versions live in `TemplateStore.Seed`'s SQL), so
  `seed_test.go` drives the real `TemplateStore.Seed` over a test-local in-memory
  `comms.message_template` DB instead of commsfake.
- Answer: (owner)

## Q-FEAT-TOOL agent handler set (FEAT-TOOL-03, FEAT-TOOL-04, FEAT-TOOL-07)

- The tool tasks say each handler registers itself from init() into "the agent handler set"
  that `agenttest.FromFeature` (FEAT-CORE-14) reads, but no handler set exists on main. Each
  of these tasks adds the same file, `app/legal/agent/handlerset.go` (identical in all three
  branches, so they merge cleanly): `Handler{Name, Build(Deps)}`, `addHandler` (init only),
  `HandlerFor`, `HandlerNames`, typed `Deps`/`DepOf`, and shared helpers (`handlerTool`,
  `visibleTo`, cursor paging, `MatterDocuments`). FEAT-CORE-14/15 should build on it.
- app/legal/docs has no "documents of a matter" query and kernel/docs has no text search, so
  list_documents and search_documents read through a narrow `MatterDocuments` interface
  (document + current version) that the docs module must implement when wired at boot.
- list_work_items: a work item title is user text with no document citation. It is returned
  as an untrusted `agenttools.Quoted` envelope without a source; CheckCitations passes because
  only the "quotes" array needs citations. Matter-scoped items are those whose subject kind is
  "matter".

## Q-GATE-AMOUNT gate amount path, authority-limit kind and editor lookup per kind

- Source: review of waves 2-3, items 4 and 5 (ARC-I-016, ARC-I-020, BIL-C-085).
- Additive to `kernel/registry/types.go` GateKind: `AmountPath`, `LimitKind`, `Preparers`
  (new type `PreparersFunc`). Additive to `spec/kernel/gatekind.schema.json`: optional
  `amount_path` and `limit_kind`. `spec/gates.json` declares `amount_path` on every kind whose
  payload has a `*_cents` field (bill_approval reads `total_cents`) and `limit_kind`
  `trust_withdrawal_max` on trust.withdrawal and billing.trust_fee_withdrawal.
- Decide no longer depends on SetDecideHooks for these checks: the limit and the kind's editor
  lookup run on every Decide. A money kind with no amount path fails registration and Decide.
- Open (verify): core.authority_limit.kind allows only writedown_pct, trust_withdrawal_max and
  practice_area, so bills have no cents cap kind yet; adding one needs an additive migration.
- Write-offs: `WriteOffEffect.Totals` (nil fails closed) sums write-offs on the same bill plus
  the client's other bills in the last 30 days (verify the window with the lawyer). The
  Postgres implementation must lock the bill row FOR UPDATE.

## Q-W3-LEGAL Wave-3 legal corrections (review items 6 to 9)

Changed expectations, each with its citation. Every rule stays (verify) and behind its flag.

- **RCP 7-day period** (`app/legal/rulepacks/ontario/civil-procedure.yaml`). Rules of Civil
  Procedure, R.R.O. 1990, Reg. 194, r. 3.01(1)(b): holidays are not counted only where the
  period is *less than* seven days. The 7-day rule (renamed `short_period_7_days` ->
  `period_7_days`) now counts calendar days, rolling forward only off a holiday last day
  (r. 3.01(1)(c)). Golden dates: 2026-03-02 -> 03-09 (was 03-11); 2026-04-01 -> 04-08 (was
  04-14); 2026-05-14 -> 05-21 (was 05-26); new 2026-05-11 -> 05-19 (Victoria Day roll).
- **Notice of intent to defend.** r. 18.02(1): the notice is delivered within the time for the
  statement of defence, i.e. 20 days after service in Ontario (r. 18.01(a)), not 10 days.
  Golden dates: 2026-03-02 -> 03-23 (was 03-12). New rule `statement_of_defence_after_notice`
  (r. 18.02(2)): a notice delivered in time gives 10 more days, so 30 days from service of
  the claim; trigger `claim_served_notice_of_intent_delivered` is raised only when the
  notice was delivered in time. Golden: 2026-03-02 -> 04-01; 2026-03-04 -> 04-07 (Good
  Friday and Easter Monday roll). Not modelled: 40/60-day service outside Ontario
  (r. 18.01(b), (c)) and r. 3.01(1)(d) deemed-service times.
- **r. 1.03 holidays.** Unchanged: "holiday" includes every Saturday and Sunday, so the pack
  skips both in business counting and the forward roll.
- **Limitations Act, 2002 s.22** (`app/legal/limitation/statute.go`). An agreement suspension
  now reaches the s.15 ultimate period only when it starts on or after discovery. The
  instruction for this fix said "except business agreements under s.22(5) and (6)". The
  statute as I read it says otherwise: s.22(4) lets *any* agreement made after the effective
  date suspend or extend the s.15 period, but only if the claim has been discovered; s.22(5)
  para. 2 lets a business agreement *vary* the s.15 period but says it may be suspended or
  extended "only in accordance with subsection (4)". So no business-agreement exception was
  added (no `BusinessAgreement` field); the discovery condition applies to all agreements.
  The suspension's start date stands in for the date the agreement was made. (verify s.22(3)
  to (6) and the 2006-10-19 effective date.) New tests: agreement after discovery suspends
  both (basic 2024-01-11, ultimate 2035-01-11); agreement before discovery leaves the
  ultimate at 2035-01-01.
- **Legislation Act, 2006 s.87/s.88 roll.** The limitation roll now uses the s.87 "holiday"
  definition: Sunday plus the dated holidays. Saturday is not an s.87 holiday, so a Saturday
  last day is no longer moved (fail closed). Changed: ultimate 2041-09-28 (Sat) stays (was
  09-30); basic 2026-02-28 (Sat) stays (was 03-02). New: a Sunday last day rolls to Monday;
  a Saturday Boxing Day rolls past Sunday to Monday. (verify) Whether a court office closed
  on Saturday extends the time under the Legislation Act's closed-office provision, and
  whether s.87 makes the r. 1.03 substitute Mondays (e.g. 2026-12-28) holidays; the
  calendar passed in is the r. 1.03 list, which includes them.
- **Interest due date** (`app/legal/billing/interest`). Additive contract field
  `interest.Bill.DueDate` (zero = none agreed). `Posting.Post` passes it to `Start.Clip`, so
  interest starts on the later of the agreed due date and one month after delivery
  (BIL-C-070, Solicitors Act s.33). New tests: due 2026-03-20 -> 10 days in March; a period
  ending on the due date -> ErrNotDue; an earlier due date never starts before 02-15.
- **ETA Sch. VI Part V s.23: NOT changed; needs the tax lawyer.** The instruction asked to add
  an "individual in Canada when the service is performed" exclusion and to apply the
  litigation exclusion to all recipients. The provision does not say that: s.23(a) excludes
  "a service rendered to an individual in connection with criminal, civil or administrative
  litigation in Canada, other than a service rendered before the commencement of such
  litigation" (individuals only), and s.23 has no presence test. The presence exclusions
  are in s.7(a)/(a.1), and s.7(b) takes advisory, professional and consulting services out
  of s.7. The frozen rule BIL-T-015 (spec/rules/b2.json) quotes this text and has test
  cases for both points, and rule ids outrank the code. Changing the behaviour would mean
  changing a frozen rule against the statute, so the code and tests are left as they are.
  (verify against the current s.23 text and GST/HST Memorandum 4.5.3.)

## PORT-01 (kernel/gate: *PG implements gate.Engine)

- Branch is cut from origin/main with origin/task/BOOT-09 merged in, because the run.go
  ErrNotImplemented assert this task removes exists only on that branch.
- `kernel/gate/open.go` (outside the task's file list) gains one field on `PG`: `db TxRunner`,
  set with `SetDB`. `Engine.Get` takes no tx, so the engine needs its own read handle; the
  field is additive. Boot's `pgGates` now takes the pool and wires it.
- Withdraw sets `counsel.gate_tx` itself (there is no `gate.withdraw()` SQL function in
  0001); `counsel_app` holds UPDATE on gate.approval_request. A SECURITY DEFINER
  `gate.withdraw(uuid, uuid)` in a later additive migration would be tighter.
- RunDue skips a gate whose effect failed (recorded as failed), whose kind is unknown or whose
  stored payload fails its hash, logging a warning, so one bad gate does not block the rest.

## PORT-02 (kernel/registry/schemas: embedded spec/kernel schemas)

- Branch is cut from origin/main with origin/task/BOOT-09 merged in (run.go lives only there).
  The merge resolves two conflicts against main's wave-4a boot work: Kernel keeps main's
  fields (BOOT-09 had a second `Notify`), and run.go drops its stub `addKernelJobs` in favour
  of main's real one in jobs_kernel.go.
- `kernel/registry/schemas/embed_integration_test.go` is added beyond the task's file list to
  meet the "integration test compiles" line; it needs no database.

## FEAT-TOOL-02 search_contacts

- Additive contract change (approved in the FEAT plan answers): `party.Contacts` gains
  `Search(ctx, query)` and `party.SearchLimit`; Store and partyfake implement it. The Store
  query uses LIKE on lower(legal_name/display_name) with wildcards escaped; it has no
  integration test yet.
- Visibility: a contact is returned only when it is the client of at least one matter the
  acting user can see (matter.Query List by ClientID, then walls). A contact of walled
  matters only is omitted, not refused with ErrNotVisible: the input has no matter id, and a
  404 on a name search would tell the user a walled client exists. Parties other than the
  client (matter.party_role) are not reachable through matter.Query, so they are not found.
- Citations: a contact name is client text with no document version. It is returned as an
  untrusted Quoted envelope cited to the contact record (`contact:<id>`, page 1, line 1) so
  CheckCitations validates it; quotes[i] is the name of contacts[i].
- `app/legal/agent/handlerset.go` is byte-identical to the file on task/FEAT-TOOL-03 (see
  Q-FEAT-TOOL agent handler set), so the branches merge cleanly.

## Q-W3-TRUST Trust review fixes: additive contract changes

- Source: wave 2-3 review, trust items 2, 3, 10 and 11 (By-Law 9).
- `migrations/5501_withdrawal_authority.sql` (app/legal/trust block, additive):
  - `trust.withdrawal_request.authorized_at` / `expires_at` (nullable). `MarkAuthorized` sets
    `expires_at = authorized_at + 72h` (`withdraw.AuthorizationTTL`, the G11 gate TTL).
    `PGStore.Lock` fails closed once it has passed, and on any authorised row with no expiry
    (rows authorised before this migration must be re-authorised). Expired holds are released
    to `cancelled`: on the same ledger at the next authorisation, and by
    `PGStore.ReleaseExpired`, which the boot-wiring task set must schedule as a periodic job.
  - Trigger `withdrawal_terms_frozen`: amount, payee, ledger, method, bill, purpose, preparer
    and receiving account never change once a withdrawal is proposed; editing is a new
    request. `form_9a_doc_id` may be attached once while awaiting authorisation.
  - Trigger `transfer_terms_frozen` on `trust.trust_transfer` once it leaves draft.
  - Table `trust.form_9a_signature` (append-only): the Form 9A signer and signing time.
    `PGEFTRecords` reads the signer from it, never the document uploader; no row, no record.
    `PGEFTRecords.RecordSignature` writes it. (verify the Form 9A signing requirements)
- `spec/gates.json` (additive): `trust.withdrawal` payload gains `withdrawal_id` and requires
  it and `method`; `trust.transfer` gains and requires `transfer_id`.
- `withdraw.EffectPayload` / `TransferPayload` carry the terms (amount, payee, method, matter;
  for transfers amount and both matters). Apply and Execute fail closed on any mismatch with
  the row. Execute re-reads the approved payload through `Effect.Approvals`
  (`PGApprovals`: stored `payload_jcs` of the approved gate); nil means no execution.
- `trust.Receipt.CashGateID`, `payment.Capture.Method/MatterID/CashExceptionCode/CashGateID`
  (additive): an exception code passes the $7,500 cash limit only with an approved
  `trust.cash_over_limit` (G6c) gate for the same matter, amount and code.
  `trust.LedgerStore.CheckOperatingCash` is the operating-path check (same per-matter lock);
  `payment.CheckCash` must be called by the Postgres Capture when it lands.
- Not fixed here: `billing.cash_received_cents` still counts operating cash only through
  allocations (billing.payment has no matter_id); needs its own additive change.

## fix/migrate-role: migrate role, gate verdict order, test role lock

- `counsel-migrate -mode=expand` applies the files without a `-- contract: <release>` mark;
  `-mode=contract` applies only the marked files and refuses while any expand file is
  unapplied (`migrations.ErrExpandPending`). Both record in `schema_meta.applied` and use the
  direct DSN when given (ARC-U-011). A failure exits non-zero.
- 0302 re-creates `gate.check_decision` so an unknown verdict fails as
  `gate_decision_verdict_ck` before any DM rule: a BEFORE trigger runs ahead of CHECK
  constraints, so DM-070 used to win (TestGatePayloadAndRecommend).
- Integration helpers hold `migrationstest.LockClusterRoles` while building their database:
  the frozen 0001 names the cluster roles, so they cannot be unique per package; schema setup
  is serialised instead.
- Still open: move the 6100 backfill into a `kernel/migrate` Backfill step run by a worker job
  (needs a durable Checkpoints store and an app hook for the step), and the migrator role secret.

## PORT-03 (kernel/boot: identity-backed report Authorizer)

- Built on task/PORT-02's merge of origin/task/BOOT-09 into main (run.go lives only there).
- run.go gains an optional `env.identity` (Access + PrincipalResolver from the pool). Production
  leaves it nil until the identity store is wired (BOOT-20), so reports stay on denyAll there;
  `reportAuthorizer` switches to `reportAuth` as soon as an Access is supplied.
- "Firm subject" is the zero identity.Subject (organisation-wide); the kernel names it
  `orgSubject` so no app term enters kernel code.
- There is no Postgres RoleAssignments in the kernel yet; the integration test reads
  core.app_user/core.role through a test-local one.

## HTTP-00: gate-owned-state guard (files outside the task, decisions taken)

## HTTP-03: contacts routes (files outside the task)

## HTTP-01 / HTTP-02: matter routes (decisions taken, open items)

- Routes register a zero-value `matter.HTTP` / `matter.Writes` through `Registry.Routes`;
  every route answers 503 `unavailable` and calls nothing until boot wiring supplies the
  dependencies (same as the withdrawal execution route). The BOOT tasks must inject them.
- GET /matters: one capability check per request (ARC-I-042); walled and restricted matters
  are left out by Query under the caller's RLS session, not by per-row checks. openapi
  declares no `cursor`/`limit` parameters and no 200 schema for /matters; the handler takes
  both (api.FromRequest) and the 200 bodies are not schema-checked.
- POST /matters: the rule wins over the task text. Same key + different body returns 422
  `idempotency_key_reused` (ARC-I-008), not 409. kernel/idem's own problem bodies omit the
  required `type` field of the Problem schema; the handler checks the missing key itself so
  that 400 validates, but the 422/409 bodies from `idem.New` do not (fix in kernel/idem).
- POST /matters opens `matter.opening` with subject `party.contact`/client_id: the matter
  row does not exist until the effect creates it. Needed: the matter.opening effect
  (service gap) and a confirmation of the subject.
- PATCH: plain-field writes go through a new `matter.Updater` port (declared in
  http_write.go, not the frozen contract); its Postgres implementation is a service gap.
  status=open opens matter.opening; closing/closed/archived open platform.closing (verify);
  team and restricted open matter.widen_access. One gated change per request; mixing plain
  and gated fields is 400.
- Q-HTTP-00 (answered, wave 4c merge): `billing.bill_delivery` is added to spec/gates.json and
  the a2 gate_kinds table (additive; G-B2, billing.approve, medium, 168h, effect
  `billing.deliver_bill`, ARC-I-052). It leaves `unspecifiedKinds`; its delivery effect is not
  built yet, so it is listed unwired.
- Wave 4c merge notes: migrations 1401_webhook_expiry_notice and 5501_withdrawal_authority
  clashed with same-numbered files and are renumbered 1403 and 5502; the withdrawal expiry
  index moves to 5503_withdrawal_authority_index.sql (CONCURRENTLY, MIG-061). Run wires the
  outbox relay over the request pool (db.Pool gains plain Exec/Query/QueryRow) and calls
  addNotifyJobs and addWebhookJobs. The legal app schedules
  trust.withdrawal_release_expired every 15 minutes when gate services are built. kernel/idem
  problem bodies carry `type` (urn:counsel:problem:<code>, now api.ProblemTypePrefix).

## FEAT-CORE-02 answers carry citations; explicit tool HandlerSet

- Owner decision (2026-10-11): tool handlers do not register through init(). Outside the
  task's files, `app/legal/agent/handlerset.go` now holds an explicit `HandlerSet`
  (`NewHandlerSet` rejects duplicates, `For`, `Names`) and `Handlers()` lists every real
  handler; the four tool files drop their init(). `HandlerFor`/`HandlerNames` remain as
  wrappers over `Handlers()`, so existing callers keep working. A new tool adds its
  `Handler` to the list in `Handlers()`.
- Additive to the frozen `kernel/agenttools/run` contract: `Spec.AnswerMayBeUncited bool`
  (default false, so an answer must cite). `CheckAnswer` reads `{answer, citations[]}` with
  snake_case citation keys (`version_id`, `page`, `from_line`, `to_line`); `AnswerSchema` is the
  matching `Spec.OutputSchema`. A cited range may be a sub-range of a citation a tool result of
  the same run returned (same version and page).

## FEAT-CORE-06 run scope (gate-kind allowlist and drafts made in this run)

- Additive to the frozen kernel/agenttools contract (new file scope.go): `Scope{RunID,
  GateKinds, Drafts}`, `NewScope`, `WithScope`, `ScopeOf`, `CheckGateKind`, `RecordDraft`,
  `ScopedOpener`, `ErrGateKindNotAllowed` (403 gate_kind_not_allowed), and the kernel
  `submit_for_approval` tool (`SubmitForApprovalTool`, capability `agent.ask`).
- Outside the task's files, each a few lines: `run/runner.go` stores one Scope per run in the
  context (Spec.GateKinds) and maps ErrGateKindNotAllowed to its problem code;
  `tool.go` Registry.Call records a write tool's draft outcome in the scope; `write.go`
  OpenGateTool opens through ScopedOpener.
- Fail closed: an agent principal with no scope for its run id opens no gate. User
  principals (the manual path) have no run scope and are not checked. The existing
  open_gate test now runs its agent cases inside a scope declaring both kinds it uses; its
  assertions are unchanged.
- Drafts are keyed by the write output ref (the subject id); submit_for_approval on any other
  subject is ErrNotFound (404). The gate payload carries subject ids only; the model's note is
  not stored (AI-R-005).


## FEAT-TOOL-21 prepare_trust_withdrawal amount provenance

- Closed by FEAT-FIX-A: `WriteDeps.Provenance` is checked only when set. Now nil fails closed
  (ErrMissingDep, nothing recorded). `agenttest.New` builds one Provenance per driver, and a
  read stand-in whose text is JSON records its figures for the run, as the real read would.
  Boot wiring (BOOT-FEAT-WIRE) must pass the run's Provenance.
- `WriteDeps.Withdrawals` is now the narrow `agent.Withdrawals` interface (Request, Available).
  `withdraw.Service` and `withdrawfake.Fake` satisfy it, so callers are unchanged.
- Closed by FEAT-FIX-A: payee was free text. Now `WriteDeps.Contacts` (the FEAT-TOOL-12
  `MatterContacts` port, required, nil fails closed) and payee must be one of the matter's
  contact ids (`ErrPayeeNotContact`, 422, rule BIL-L-037). The F-TRU-02, F-TRU-10 and
  F-BIL-03 fixtures now pay `agenttest.PayeeContact`.

## FEAT-CORE-10

- Q-FEAT-CORE-10.1: `prompts/rules.md` is embedded and exposed as `feature.Rules()`, but
  `Def.Spec()` (feature.go, FEAT-CORE-08, outside this task's files) still builds
  `System = Preamble(id) + prompt`. Proposed one-line change at merge: `Preamble(id) + Rules() +
  "\n" + prompt`. `feature.PromptHash` hashes `Spec()`, so it follows that change by itself;
  no fixture pins a prompt_hash yet, so nothing needs rehashing.

## FEAT-TOOL-11 draft_document

- Depends on FEAT-CORE-06 (run Scope), which has no branch yet. The handler takes a
  `RunScope` dep (`Returned` for "citations must come from this run", `RecordDraft` for the
  run's drafts); boot adapts the CORE-06 Scope to it once that lands. On the manual path (a
  user principal, no run) citations are only shape-checked.
- No init(): per the owner decision, `DraftDocumentHandler()` returns the Handler and boot
  adds it to the explicit HandlerSet. Until boot does, agenttest.FromFeature keeps its
  stand-in for draft_document.
- The docs module has no draft API yet; the handler writes through `DocumentDrafts`
  (`CreateDraft`), which app/legal/docs implements at boot.

## FEAT-TOOL-12 draft_email

- No init(): `DraftEmailHandler()` returns the Handler and boot adds it to the explicit
  HandlerSet (owner decision); agenttest.FromFeature keeps its stand-in until boot does.
- No email-draft or matter-contact API exists yet. The handler writes through `EmailDrafts`
  (`CreateEmailDraft`, deliberately without a send method) and checks recipients with
  `MatterContacts` (`MatterContactIDs`: client and parties of the matter); boot wires both.
  submit=true opens `platform.outgoing_email` through `agenttools.GateOpener` with subject
  kind `email_draft`; the caller is the preparer.

## FEAT-TOOL-13 draft_note

- No init(): `DraftNoteHandler()` returns the Handler and boot adds it to the explicit
  HandlerSet (owner decision); agenttest.FromFeature keeps its stand-in until boot does.
- No note-draft API exists yet. The handler writes through `NoteDrafts` (`CreateNoteDraft`)
  and opens `matter.agent_note` through `agenttools.GateOpener` with subject kind
  `agent_note`; the caller is the preparer (an agent's acting-for user).

## Q-PORT-15 write-off store has no table for draft write-offs (answered)

PORT-15 asks for `PGWriteOffs` over `billing.write_off`, adding only a second-gate column in
`6220_write_off_second_gate.sql`. The 0001 table cannot hold what `WriteOffStore` needs:

- `billing.write_off` is the approved record only: `approval_id` is NOT NULL, the row is
  immutable (`write_off_immutable` trigger, UPDATE/DELETE revoked), and `billing.bill_balance`
  subtracts every row's `amount_cents` at once (`bill_not_overpaid_wo` fires on INSERT). A
  draft row would reduce the bill's balance before G-B5 is approved, and `MarkAwaitingSecond`
  could never set a column on it.
- It has no `status`, `prepared_by`, `approved_by` or `approved_at`, which `Lock` must return
  (`ApproveWriteOff` checks the preparer and the draft status).
- Its CHECK needs `fee_cents + dtx_cents + dnt_cents + tax_cents + interest_cents =
  amount_cents`, but `SaveApproved(w WriteOff)` only carries `AmountCents`; the tax part is in
  the `BadDebtAdjustment` that `PostBadDebt` gets, not in `SaveApproved`.
- `WriteOffBills` needs `Bill.ResponsibleUser`, which lives on `matter.matter`; billing may not
  join another module's table.

Proposal (needs approval; all additive):
A. `6220_write_off_draft.sql` adds `billing.write_off_draft` (id uuid PK, bill_id, amount_cents
   > 0, reason, prepared_by uuid, credit_gate_id uuid NULL, second_gate_id uuid NULL,
   created_at), append-only except a one-time `second_gate_id` NULL -> value guarded by a
   trigger. `Lock` = draft row FOR UPDATE LEFT JOIN `billing.write_off` on the same id
   (approved when that row exists; approved_by from the gate decision through the gate
   engine, not a join). `SaveApproved` inserts `billing.write_off` with the draft's id and
   `approval_id` = the G-B5 gate id, `ON CONFLICT (id) DO NOTHING` for idempotency.
B. Bucket split: additive `SaveApprovedSplit(ctx, tx, w, adj)` (or `WriteOff.TaxCents`) so
   `tax_cents` = adj.TaxCents and `fee_cents` = amount - tax; or approve putting the whole
   amount in `fee_cents` (loses the tax split in the row; the journal still has it).
C. `ResponsibleUser`: `PGWriteOffBills` takes a `matter.Query`-style lookup port (matter id ->
   responsible lawyer) instead of joining `matter.matter`.

Answer (owner): approve an additive `billing.write_off_draft` table (status, preparer,
approvers, total). The fee, disbursement and tax split is computed at draft time with
`money.Allocate` in proportion to the bill's outstanding components (largest remainder, then
verified). Only the effect writes the approved row to `billing.write_off`. The responsible
lawyer is read through a matter port, never a SQL join.

Done in PORT-15:
- `migrations/5610_write_off_draft.sql` (billing block 5600-5699; the task text named 6220,
  which is the app/legal/agent block): the draft row with its five-bucket split,
  `credit_gate_id` (G-B5) and `second_gate_id`, each set once, and draft -> approved once,
  guarded by a trigger; no deletes.
- `PGWriteOffs` implements `WriteOffStore` and `WriteOffBills`. `Draft` locks the bill, reads
  its outstanding buckets from billing tables only and fails closed when they do not add up to
  the bill balance net of reserved fee transfers (a trust fee transfer has no bucket split).
  `Submit` records the G-B5 gate id; `SaveApproved` refuses a draft without it and inserts
  `billing.write_off` with `approval_id` = that gate, `ON CONFLICT (id) DO NOTHING`.
- `ResponsibleLawyers` is the matter port `WriteOffBill` reads the lawyer through; nil or an
  empty lawyer fails closed. The matter-side adapter is wired at boot (BOOT-15/16).

## Q-PORT-18 conflict run model does not map onto conflict.check_run / conflict.hit (answered)

PORT-18 asks for `PGRuns` over `conflict.check_run` / `conflict.hit` behind an additive
`RunStore`. The 0001 tables need values the conflict domain never has, so a store would have
to invent regulator-facing conflict records:

- `check_run` hangs off `conflict.conflict_check` (`check_id` NOT NULL, which itself needs
  `trigger`, `settings_version` -> `conflict_settings` and `requested_by`), plus
  `index_version` and `complete` NOT NULL. `Request`/`Run` carry only a matter id, terms and
  the preparer: no check id, no version, no index version.
- `conflict.hit` needs `check_party_id` NOT NULL (a `check_party` row with `kind`
  person/organization and `role`), `matched_kind`, `matched_id`, `match_type` and `score_ppm`.
  `Hit` is `{Term, PartyID, MatterID, Role}`: the term is not a check party, and the match
  kind, match type and score are not known. `Hit.MatterID`/`Role` have no column.
- G2 decisions and hit dispositions live on `conflict.clearance` (approval_id, signed_by) and
  on `hit.disposition*`, which is UPDATEd in place; the in-memory `Checks` keeps both as
  append-only logs. Replacing only the run slice leaves `Dispose`/`decide` needing a run
  lookup by id, which `RunStore{Append, Runs}` does not offer.

Proposal (needs approval; additive):
A. `RunStore` = `Append(ctx, Run) (Run, error)`, `Runs(ctx, matterID)`, `Run(ctx, id)`; memory
   default keeps `NewChecks` as is; `NewChecksWith(s, store, now)`.
B. Additive fields: `Request.CheckID`, `Run.CheckID`, `Run.Version`, `Run.IndexVersion`,
   `Run.Complete`; `Hit.CheckPartyID`, `Hit.MatchedKind`, `Hit.MatchType`, `Hit.ScorePPM`,
   filled by the Searcher (the party index knows them). `PGRuns.Append` refuses
   (ErrInvalid) a run or hit missing any of them, never defaults them; version is
   `max(version)+1` under `SELECT ... FOR UPDATE` on the conflict_check row, and a 23505 on
   UNIQUE(check_id, version) maps to a new `ErrRunExists`.
C. Dispositions and G2 decisions stay in memory in this task; a follow-up maps them onto
   `conflict.hit` dispositions and `conflict.clearance` (or an additive append-only
   disposition table, since `hit.disposition` is updated in place).

Answer (owner): approve the additive domain fields on conflict Run/Hit (check record, party,
match type, score ppm), filled by the searcher. Missing values are refused. Add
RunStore.Get(id).

Done in PORT-18:
- `contract.go` (additive): `Request.CheckID`; `Run.CheckID`, `Version`, `IndexVersion`,
  `Complete`; `Hit.CheckPartyID`, `MatchedKind`, `MatchType`, `ScorePPM` (`Hit.PartyID` is
  `conflict.hit.matched_id`).
- `RunStore{Append, Runs, Get}`, `ErrRunExists`, `ErrInvalidRun` and the optional
  `IndexVersioner` searcher in `check.go`; `NewChecks` keeps the in-memory store,
  `NewChecksWith(s, store, now)` takes `PGRuns`. `clearance.go` reads a run through the store
  (one helper call, needed because the run slice is gone).
- `migrations/5200_check_run_detail.sql` (conflict block): nullable `check_run.prepared_by`,
  `check_run.terms`, `hit.ordinal`, `hit.term`, `hit.party_matter_id`, `hit.party_role`, so a
  stored run reads back whole. `PGRuns` refuses (ErrInvalidRun) a run or hit missing any value,
  a hit whose check party is on another check, and on read a run without a preparer or with a
  gap in hit order. Dispositions and G2 decisions stay in memory (proposal C).

## Q-INTEG-12 Pass 12 integration decisions

- Run scope, one concept (additive): the kernel `agenttools.Scope` (FEAT-CORE-06) now also
  holds the citations the run's tool results returned (`AddCitations`, `Cites`).
  `Registry.Call` records them (`RecordCitations`) after `CheckCitations`. The app's
  `agent.RunScope` port (FEAT-TOOL-11..13) stays as the consumer view; its implementation is
  the stateless `agenttools.RunScopes{}`, which reads the Scope the Runner put in ctx and fails
  closed with `ErrNoRunScope` when the ctx has no scope for that run.
- Provenance (FEAT-CORE-07) keeps its own per-run value set keyed by `Principal.RunID`; it is
  built at registration time (`ReadTool`/`SourcedWrite` wrappers), before any run exists, so
  moving it into Scope is not a natural fit. Open: the Runner should call `Provenance.Forget`
  when a run ends (boot task).
- `agent.Handlers()` now lists draft_document, draft_email, draft_note and the
  `ReportHandlers()` pair. No init() registration.
- Q-FEAT-CORE-10.1 applied: `Def.Spec()` builds `Preamble(id) + Rules() + "\n" + prompt`.
- `PGWriteOffTotals` reads pending write-offs from `billing.write_off_draft` (status draft
  with a credit gate set), never the kernel gate table. A submitted draft whose gate was
  rejected stays `draft` and still counts; that only raises the total toward the second
  approval (fails closed). Open: add a `rejected`/`withdrawn` draft status set by the gate
  outcome so rejected drafts stop counting.
- Migrations: PORT-19's `5200_conflict_index_version.sql` renumbered to 5201 (collided with
  PORT-18's 5200). 5200 and 5610 brought to MIG-061 (timeouts, NOT VALID constraints); the
  hit ordinal unique index moved to `5202_check_run_detail_index.sql` (CONCURRENTLY).
- Not done in pass 12 (requeue to the BOOT/PORT task set): filling `feature.Set` at boot and
  building the registry from `agent.Handlers()` (app/legal/boot has no agent wiring yet); the
  report_step step-id map in the PlanStore (needs a step id on `agenttools.Step`, a kernel
  contract change; PORT-25); the matter-side `credit.ResponsibleLawyers` adapter at boot; and
  write-off drafting on bills with trust fee transfers (split the fee transfer first, PORT-15
  note). Q-INTEG-12.1 (lawyer): when a bill was paid partly by a trust fee transfer, is the
  write-off split computed on the outstanding balance after the transfer (fee portion only),
  or must the transfer itself be apportioned across fee, disbursement and tax first?
- `party.Contacts` gains `List(ctx, Filter)` in contract.go (additive, as the task's accept
  line asks), so `partyfake` gains `List` too. Pages run by id (uuidv7, so creation order);
  `ListLimit` is 201, one more than the openapi page maximum, so the handler can see a next page.
- `ErrDuplicate` (409 duplicate_contact, proposed code) maps a unique violation on insert;
  party.contact has no unique key yet, so it fires only once one is added.
- A malformed contact_id answers 400 validation_failed after the capability check (accept
  line), unlike GET /matters/{id}, which answers 404.
- app/legal/boot TestRegisterNoFlagsRecordsOnlyAlwaysSet failed on HTTP-02 merged with main
  (main's test predates the matter routes): Routes calls join the "always" set, since
  HTTP-00 mounts every handler route unconditionally (unwired routes answer 503). The exact
  comparison with legal.Register's calls is unchanged.

## HTTP-04: time-entry routes (files outside the task)

- Additive contract changes in timekeeping/contract.go: `TimeEntry.RowVersion` (the ETag) and
  `Entries.List(ctx, EntryFilter)`; timekeepingfake implements both. No Postgres `Entries`
  exists yet (service gap), so the routes answer 503 until boot wires one.
- `time_entry_id` is a plain string in openapi; ids outside `[0-9A-Za-z-]{1,64}` are 400.
- ErrOutOfScope maps to 422 activity_out_of_scope, but the service flags out-of-scope
  paralegal entries instead of refusing them (BIL-L-046), so no route returns it today.
- A reused Idempotency-Key with a different body answers 422 (rule ARC-I-008, kernel/idem),
  not the 409 in the task text, as in HTTP-02.

## HTTP-05: expense routes (approved scope)

- No expense service existed; the owner approved a minimal one. `timekeeping/expense.go` adds
  `Expense`, `Expenses` (Record, Get, List) and the Postgres `ExpenseStore` on
  timekeeping.expense (draft rows only; approval and billing set the other statuses). Boot
  wiring supplies the store; until then the routes answer 503. Its integration test is on hold.
- An agent principal never records an expense (403), as with time entries.
- `paid_from: trust` is accepted as a draft record only (the DDL allows it while draft); the
  trust transaction link and any trust movement stay with the trust gates.
- A reused Idempotency-Key with a different body answers 422 (rule ARC-I-008, kernel/idem),
  not the 409 in the task text, as in HTTP-02 and HTTP-04.

## HTTP-06: bill read routes (files outside the task)

- Additive contract changes in billing/bill/contract.go: `Service.List(ctx, Filter)`, `Filter`,
  and the stored fields the openapi Bill and BillLine require (`Bill.Version`, `PeriodFrom`,
  `PeriodTo`, `TaxRatePPM`, `TrustAppliedCents`, `BalanceCents`, `CreatedAt`; `Line.ID`,
  `Line.Taxable`). billfake implements List and bumps Version on each write. No Postgres
  bill.Service exists yet, so the routes answer 503 until boot wires one.
- Stored statuses map to the openapi BillStatus: preflight_failed -> draft,
  pending_approval -> in_review, void -> voided; written_off and credited pass through
  (x-extensible-enum). `status=returned` matches nothing: no stored status is "returned".
  Delivery channels mail and hand show as "print".
- List pages use the openapi `{data, next_cursor}` shape. Bills on matters hidden from the
  caller are left out of the list (pages can be short).
- Line actions are not stored on bill.Line yet; every line reports action "bill".

## HTTP-07: bill draft routes (files outside the task)

- No single-draft entry point exists (billing/draft has Calc+Store, billing/batch builds
  drafts), so http_draft.go adds the narrow port `bill.Drafts` (Create, Edit) with
  `DraftRequest`, `DraftEdit`, `LineAction` and the sentinels `ErrNotEditable` (409
  bill_not_editable) and `ErrNoUnbilledWIP` (422 no_unbilled_wip). Boot wiring must supply a
  Drafts built on billing/draft; until then the routes answer 503.
- PATCH edits only stored draft statuses (draft, preflight_failed). A bill in review is 409;
  there is no stored "returned" status yet.
- A body carrying status, number or totals is 400 validation_failed (accept line), not the
  422 read_only_field in the BillPatch description.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-08: bill submit, void and deliver routes

- Submit opens billing.bill_approval (202, Gate body) and never approves; void opens
  billing.bill_void. Neither route changes the bill row: no service method moves a draft to
  pending_approval yet, so the status stays draft until a submit service exists (follow-up).
- Submit has no request body in openapi, but the approval payload requires
  delivery_method. The route accepts an optional `{"delivery_method": email|portal|mail}`
  body, default email. Please confirm or add the field to openapi (additive).
- Void requires billing.approve (the requester must hold the approving capability);
  submit and deliver require billing.draft.
- Deliver emits one outbox message `billing.bill.delivery_requested` and returns 202 with
  the bill; delivered_at stays unset until the delivery effect records evidence. The
  consumer that calls bill.Service.Deliver is a boot follow-up.
- Gate subject kind for bills is `billing.bill` (bill.SubjectBill).
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## Q-FEAT-CORE-12 screen-action route (additive OpenAPI operation)

- Owner approved the additive screen-action operation (FEAT plan answers, 2026-10-11). Proposed
  addition to `api/openapi.yaml`, not yet made (the task does not list that file):
  `POST /api/agent/actions/{feature_id}`, body `{screen: string, record_ids: string[1..100]}`
  (no other fields, so no free text reaches the run). Responses: 200 `{answer, citations[]}`
  (chat features, run inline, nothing persisted; citations use the `run.AnswerSchema` shape),
  202 `{run_id}` (every other feature, queued), problem+json 400, 401, 403
  (`missing_capability`, rule ARC-I-036), 404 (unknown feature or a walled record), 422
  (screen not an action of the feature, or bad record ids), 503 (model busy, Retry-After).
- `app/legal/agent/trigger.Actions` serves it. Boot wires `Screens` (screen id -> subject kind
  and read capability), `Access` (kernel identity), the runner and an `ActionQueue` that inserts
  the ids-only run job. The route is not mounted until the OpenAPI operation lands, so the
  route conformance test stays green.
- Branch base: task/FEAT-CORE-11 (shared `SubjectRef`) with task/FEAT-CORE-02 (`CheckAnswer`)
  merged in; both are on top of task/FEAT-CORE-08.

## FEAT-TOOL-05 list_deadlines

- Additive contract change (named in the task's accept text): `kernel/deadline` gains
  `Filter` and a separate `Lister` interface (`List(ctx, Filter)`), so `Engine` and its
  implementations are unchanged. deadlinefake implements it; the Postgres store has no List
  yet and must implement `Lister` before boot wires list_deadlines.
- Visibility: a named walled matter is refused with ErrNotVisible (404). Without a matter id,
  deadlines of walled matters are omitted, and deadlines on a subject that is not a matter
  are omitted too (walls cannot be checked on them: fail closed).
- The result carries ids, rule references and dates only, no document or client text, so it
  has no quoted envelope. Due dates are YYYY-MM-DD strings, which Provenance.ReadTool records.
- Registered through `ListDeadlinesHandler()` for the boot HandlerSet, not init().

## FEAT-TOOL-06 compute_deadline

- "Published pack": kernel/deadline has no publish state, so the tool reads a new
  consumer-side `agent.PublishedPacks` (wired at boot; a pack still behind its flag, such as
  the unverified Ontario pack, must not be listed). The tool computes only on a published pack
  and refuses with ErrUnknownPack when the engine holds another version than the published one.
- `rule_id` is a bare rule id found in exactly one published pack, or `<pack>/<rule>`; a bare
  id in several packs is refused (invalid input) rather than guessed.
- Walls: the a2 input has no matter id and the computation reads no matter (empty subject,
  nothing saved), so there is no walled-matter case; the accept line on ErrNotVisible does not
  apply.
- No write: `Compute` only; `Save`/`Confirm` are never called. Result has no document or client
  text, so no quoted envelope; due dates are YYYY-MM-DD and recorded by Provenance.ReadTool.
- Not yet in `agent.Handlers()`: `ComputeDeadlineHandler()` is added there (and to its test)
  when boot wires PublishedPacks.

## FEAT-TOOL-08 list_unbilled

- `timekeeping.Entries` has no list by matter and carries no amounts, so the tool reads a new
  consumer-side `agent.MatterTime` (wired at boot over timekeeping entries priced with
  timekeeping/rates, as reports.WIPReporter prices WIP). No timekeeping contract change.
- Unbilled = state approved, billable, no BillID (b3 WIP). `period_to` is inclusive on the
  work date read in America/Toronto. Amounts and the total are int64 cents; a negative amount
  or an overflowing total fails closed. Provenance.ReadTool records every amount and the total.
- The narrative is client text: returned as an untrusted Quoted envelope cited to the entry
  record (`time_entry:<id>`, page 1, line 1), as search_contacts does for contact names.
- Not yet in `agent.Handlers()`: `ListUnbilledHandler()` is added there (and to its test)
  when boot wires MatterTime.

## FEAT-TOOL-09 get_bill

- The frozen app/legal/billing/bill contract has no balance: `Bill` carries totals only and no
  module returns what is still owed on a bill. get_bill reads it through a new
  `agent.BillBalances` port (`BalanceCents(ctx, bill.ID)`), to be wired at boot from billing
  payments, credits and write-offs. Proposed fix: an additive balance read in billing.
- Registered through the explicit `GetBillHandler()`, not init().
- An unknown bill id is `ErrNotVisible` (404), the same as a bill on a walled matter.
- Line narratives are quoted as untrusted text cited to the bill line (`bill:<id>`, page 1,
  line = ordinal); a bill has no document version.
- The tool wraps itself with `Provenance.ReadTool`, so the cents it returns are what a later
  write in the run may carry; it needs the run's `*agenttools.Provenance` in Deps.

## FEAT-TOOL-10 get_trust_balance

- withdraw.Available takes a trust account id and a matter id; nothing on main lists the
  accounts that hold a matter's ledger. The tool reads them through a new
  `agent.MatterTrustAccounts` port (`TrustAccountsOf(ctx, matterID)`), wired at boot from the
  trust ledgers, and returns one figure per account plus their sum.
- Available has no history, so an `as_of` other than today (America/Toronto) is refused as
  invalid input rather than answered with today's balance.
- Registered through the explicit `GetTrustBalanceHandler()`, not init(). The result holds ids
  and cents only, so it quotes nothing; it wraps itself with `Provenance.ReadTool` and needs
  the run's `*agenttools.Provenance` in Deps.

## FEAT-TOOL-15 propose_deadline

- Built on origin/task/FEAT-CORE-06 (run Scope) with origin/task/FEAT-CORE-07 (provenance)
  merged in. Registered through the explicit `ProposeDeadlineHandler()`, not init().
- The frozen kernel/deadline `Proposed` has no title and no pack id, and the a2 input has no
  pack. The deadline is saved with rule id, trigger and due dates and the matter subject; the
  title and the source citation travel in the cal.confirm gate payload the confirming person
  sees. Proposed fix: an additive `Proposed.Title` (and pack id in the a2 input).
- `due_on` and `trigger_date` go through `Provenance.SourcedWrite`: an agent may only carry a
  date a read tool returned in the same run (a compute_deadline result, or a date a document
  read returned with its citation). The manual path is not checked.
- The gate kind is checked against the run scope before the deadline is saved, so a run whose
  feature does not declare cal.confirm leaves no proposed deadline behind.

## FEAT-TOOL-16 create_task

- The a2 `create_task` input has `description_markdown`, but the frozen kernel/work `Item`
  has no description field (the `work.work_item.description` column exists). The tool accepts
  the field (it is schema-valid) and does not store it. Proposed fix: an additive
  `Item.Description` in kernel/work, then the tool passes it through.
- The proposed status is config, not a constant: the tool reads the task workflow and its
  proposed status through a new `agent.ProposedTasks` port, wired at boot from work config.
- An assignee walled from the matter is refused as `ErrNotVisible` (404), like the acting user.

## FEAT-TOOL-19 propose_conflict_parties

- The a2 input names a `check_id`, but the frozen conflict Checker runs by matter
  (`Request{MatterID, Terms}`) and has no check entity or check parties. The tool reads the
  check through a new `agent.ConflictCheckParties` port (CheckMatter, ProposeParties, wired at
  boot over `conflict.conflict_check` / `conflict.check_party`), then runs
  `conflict.Checker.Check` over every party name on the check.
- The result is outcome `proposal` with the run id as ref, not `gate`: G2 is decided on the run
  through `conflict.Clearer`, not a kernel gate the runner could park on. The run stays open.


## FEAT-FIX-A trust withdrawal fails closed; draft_bill checks sources first


## FEAT-FIX-A trust withdrawal fails closed; draft_bill checks sources first

- Closed by FEAT-FIX-B: a fee transfer to the firm's own general account (spec trust.json
  "Payee is firm general account (known)") was refused because it is not a matter contact.
  Decision: a separate firm-payee id. See the FEAT-FIX-B entry below.
- draft_bill reads a new `agent.UnbilledWIP` port (matter's unbilled entry ids up to
  period_to) from Deps; boot (BOOT-FEAT-WIRE) must wire it. Every line action source is
  checked against it before `batch.Drafts.Create`, so a bad source creates no draft.

## FEAT-FIX-B firm general account as a fee payee (By-Law 9 s.9(1))

- Decision: `WriteDeps.FirmAccount` (the firm general account's payee id) and `WriteDeps.Bills`
  (`agent.FeeBills`, returns `withdraw.FeeBill` for a bill on the matter) are new required
  ports; nil fails closed (ErrMissingDep). No BillBalances port existed, so FeeBills is the
  equivalent.
- The firm account is a payee only when purpose_category is fees, bill_id is a bill on the
  same matter, the bill is delivered and amount_cents <= its balance due (rules BIL-L-037,
  BIL-L-035). Otherwise `ErrFirmPayeeNeedsBill` (422), `withdraw.ErrBillNotDelivered` or
  `withdraw.ErrExceedsBillBalance`. Every other payee must still be a matter contact id.
- `withdraw.Request` has no bill field, so the bill id is checked by the agent but not carried
  into the trust request. Boot (BOOT-FEAT-WIRE) must wire FirmAccount and Bills; the trust
  module's own ValidateFee remains the authoritative check at authorization.
- `WriteDeps.Provenance` is checked only when set. `agenttest.New` (not this task's file) builds
  WriteTools without one, and its read stand-ins return quoted text with no numbers to observe,
  so a fail-closed nil would break the F-TRU/F-BIL/F-ACC feature tests. Boot wiring
  (BOOT-FEAT-WIRE) must pass the run's Provenance, and agenttest should wrap its read stand-ins
  with `Provenance.ReadTool`; after that, nil can fail closed.
- `WriteDeps.Withdrawals` is now the narrow `agent.Withdrawals` interface (Request, Available).
  `withdraw.Service` and `withdrawfake.Fake` satisfy it, so callers are unchanged.
- The title says "payee from matter contacts", but the accept list does not cover it and there
  is no matter-contact dependency in WriteDeps. Payee is still free text. Open question.

## Q-INTEG-13b Pass 13b integration decisions

- Feature list: `feature.AppSet`/`appDefs` could not list the deadline Defs, because
  app/legal/deadlines imports app/legal/agent/feature (an import cycle). The list moved to a
  new package app/legal/agent/featureset (`featureset.App()`, `appDefs()`), which imports both;
  `go generate ./app/legal/agent/featureset` now regenerates the plugin skills, and
  TestExportSkillsCheckedIn moved with it. F-DL-01/02/03/04/06 are listed; the three tool-use
  ones (F-DL-02/03/06) generated skills under paploo/counsel-plugin/skills. Boot builds its
  feature.Set from `featureset.App()` (BOOT-FEAT-WIRE).
- Event type names in app/legal/deadlines (DL-05, DL-06) are kept: `deadline.trigger_date_changed`,
  `rulepack.published`, `holiday.added`, `filing.receipt_received`, `claim.issued`,
  `service.affidavit_received`. No module or spec/eng/a2-interfaces.json defines these yet. The
  a2 `deadline.changed` event (cal) is a confirmed due-date change, not a trigger-date change, so
  it is not reused. Open: the owning modules (cal, rulepacks, comms) should publish these types,
  or the triggers rebind to whatever names those modules choose.
- Handler set: `agent.Handlers()` now lists every tool handler (list_deadlines, compute_deadline,
  list_unbilled, get_bill, get_trust_balance, propose_time_entries, propose_deadline,
  create_task, propose_conflict_parties, submit_for_approval, draft_bill,
  propose_payment_allocation). Their new ports (BillBalances, MatterTrustAccounts,
  PublishedPacks, MatterTime, SubjectMatters, ProposedTasks, ConflictCheckParties) come through
  `agent.Deps`; until boot wires them, the handler's Build fails with ErrMissingDep.
- scope.go: FEAT-TOOL-15/17/18 each carried the CORE-06 kernel/agenttools/scope.go; the merge
  kept main's single copy (the pass-12 RunScope version). No duplicate definitions remain.
- Name clash: FEAT-TOOL-09 (get_bill) and FEAT-TOOL-17 (draft_bill) both declared `civilDate`
  and test fixtures `billMatter`, `billRun`, `billWorld`. The draft-bill ones are renamed
  (`parseCivilDate`, `draftBill*`).
- 6200_agent_flag.sql (FEAT-CORE-13) gained the MIG-061 `SET lock_timeout` / `statement_timeout`.
- Not done here: the FEAT-TOOL-16 `Item.Description` additive field (follow-up).


## Q-PORT-11: signatory change log, migration number and files outside the list

- Task: PORT-11. The task names `migrations/6210_signatory_change.sql`, but 6200-6299 is the
  app/legal/agent block in `tools/migration_blocks.txt`; trust owns 5500-5599. The file is
  `migrations/5510_signatory_change.sql` (additive: one new table).
- `PGService` satisfying `Service` needs the PORT-10 `Propose`/`Approve` stubs (and their
  `ErrNotImplemented` test) removed from `service_pg.go` and `service_pg_test.go`. Branch is
  based on `task/PORT-10`, so PORT-10 must merge first.
- `signatory.Effects` calls `Service` without a tx. `TxEffects(*PGService)` runs the same
  effect on the gate tx (`PGService.OnTx`) and records the gate id as
  `trust.signatory.approval_id`; boot should register `TxEffects`, not `Effects`. Outside a
  gate, `Approve` records the change id as the approval.
- One change adds or removes exactly one user (the GA6 payload is one user); a bonded
  non-licensee grant stores bond_cents 0 with no expiry and keeps the evidence on the change
  (verify: the real bond amount and expiry need their own input).

## Q-PORT-12: shared signatory validity helper

- Task: PORT-12. "Shared helper in signatory" needs one file outside the task list:
  `app/legal/trust/signatory/validity.go` adds `CanSignTx(ctx, tx, account, user)`, which runs
  PGService.CanSign's query and validity rule in the caller's tx. `withdraw.PGSigners` calls
  it, so the withdrawal gate and the signatory list use one rule. Branch is based on
  `task/PORT-10`, so PORT-10 must merge first. Boot wires `withdraw.PGSigners{}` into
  `TrustPorts.Signers` with no new SQL.
- Q-PORT-22 (open): the Clio client (app/legal/clio) has no webhook-subscription renew call,
  so nothing implements sync.SubscriptionRenewer yet. PORT-22 ships only OutboxNotifier.
  Needed: a `Renew(ctx, remoteID) (time.Time, error)` PATCH on the Clio webhook endpoint in
  app/legal/clio/client.go (with cliofake support). Until then BOOT-19 wires the renewal job
  only behind its flag, which is off by default.

## Q-PORT-24-SOLE: sole practitioner signs their own trust reconciliation

- Task: PORT-24-SOLE, answering Q-PORT-24 (decided as counsel): By-law 9 needs a licensee to
  sign the monthly reconciliation and a sole practitioner has no second person.
  `recon.PGSignOff.Sign` lets the preparer sign only when the firm has exactly one active
  licensee (core.app_user, same definition as the signatory directory) and the preparer is
  that licensee. It records audit action `sign_off.sole_practitioner` in the sign-off tx
  through `PGSignOff.WithAudit`; without an audit log the preparer is refused. Every other
  preparer is refused (ErrSelfApproval) and an agent never signs (ErrAgentApproval). Tagged
  `rule: ARC-I-021`.
- The database keeps its own guard: `core.check_sod` (0001) also needs
  `core.firm.sole_practitioner` true and writes its `sod_solo_exception` row, so boot must keep
  that setting in step with the licensee count. Showing the exception in the UI is a separate
  web task.
- PORT-26 (logged, autopilot): `kernel/identity/breakglass.go` is edited outside the task's file
  list, because `NewBreakGlassWith` and the `GrantStore` field must live on `BreakGlass`. The
  in-memory store is the default. `audit.break_glass` (frozen) has no expiry column, so
  `PGGrants` treats a grant as expired once its `core.break_glass_expire` audit event with
  detail `grant_id` exists. The boot audit masker must allow `grant_id` as `audit.KindID`;
  otherwise ExpireDue audits the same grant again on every run. The other option is an
  additive `expired_at` column in the kernel/audit block (needs approval). The table holds
  one subject kind in an app-named column, so boot sets `PGGrants.SubjectKind` and
  `SubjectColumn` (the kernel names no legal term); other kinds are refused.
- PORT-26 expired_at (approved by the owner): additive migration
  `0200_break_glass_expired_at.sql` (kernel/identity block) adds nullable
  `audit.break_glass.expired_at`. `PGGrants` now reads expiry from that column and
  `ExpireDue` sets it, in the caller's transaction, after each grant's expiry audit event; a
  grant it cannot mark fails the run (`ErrGrantNotMarked`) so the audit event rolls back too.
  Expiry no longer depends on `audit.audit_event`, so the boot audit masker need not keep
  `grant_id` for ExpireDue to stay idempotent. Grants expired before this migration have no
  `expired_at` and are audited once more on the first run after it.
- PORT-27 (logged, autopilot): `kernel/portal/auth.go` is edited outside the task's file list,
  because Shell has to use the new `SessionStore` (field `Shell.Store`; NewShell sets an
  in-memory store). The migration is `1500_portal_token.sql` in the kernel/portal block, not
  `6240` (an app block). `core.user_session` has no hash or expiry column, so
  `platform.portal_token` holds the SHA-256 keys of sign-in tokens and session handles, the
  grants and the expiry, and links each session to its `core.user_session` row
  (platform 'portal'). Ending that row (logout or revocation) ends the portal session. Guest
  ids must be `core.app_user` ids.

## HTTP-PAGE list routes return the openapi page shape

- Decided (autopilot counsel): openapi declares every list page as `{data, next_cursor}`
  (next_cursor string or null); `api.Page` encoded `{items, next}`. kernel/api/contract.go
  `Page` keeps its Go fields (Items, Next) so callers compile unchanged, but its JSON tags are
  now `data`/`next_cursor` and a `MarshalJSON` method writes `data: []` for no items and
  `next_cursor: null` on the last page. This is a wire change, not additive; it fixes
  GET /matters, /contacts, /time-entries, /expenses, /credit-notes (and the trust lists on the
  HTTP-10/10b/11 branches, whose tests still decode `items`/`next` and must switch to
  `data`/`next_cursor` when merged). GET /bills (HTTP-06) already used this shape.
- openapi gives no 200 schema for GET /matters, /contacts, /time-entries, /expenses; their
  tests check the whole body against the openapi list page envelope. Declaring those 200
  schemas is an open additive openapi change.

## HTTP-10b trust ledger overdraft fields

- Trust ledger overdraft is never hidden (By-law 9; BIL-I-010, ARC-I-025). GET /trust/ledgers
  always sends `overdrawn` (bool) and `shortfall_cents` (>= 0) on each ledger. openapi `Cents`
  is non-negative, so a negative posted or available balance is reported as 0 with
  `overdrawn: true` and `shortfall_cents` = the amount below zero of the lower of the two; the
  true signed figure is that amount minus shortfall_cents. The web must show `overdrawn` as an
  error state. spec/legal names the By-law 9 no-overdraft rule only in prose (billing.json
  "reg"); the code cites BIL-I-010 from spec/eng/b3.
- Closed by Q-HTTP-10c (the fields are now declared). Was open: the two fields were not yet declared on openapi `TrustLedger`. The
  additive change needs a line in kernel/contract/additive.txt, and any pointer under
  `/components/schemas/TrustLedger` puts the legal term "trust" in kernel/, which
  TestKernelHasNoLegalTerms rejects; tools/kernel_allow.txt is a ratchet that may not grow
  (TestKernelRatchet). `TrustLedger` has no `additionalProperties: false`, so the bodies still
  validate. Proposed: exempt kernel/contract/additive.txt (openapi pointers, not kernel code)
  from the term scan, then add `overdrawn: {type: boolean}` and
  `shortfall_cents: {$ref: Cents}` to TrustLedger under this entry.

## HTTP-12: gate inbox and detail routes

- kernel/gate.Engine has no List; the routes take a `gates.Lister` (ListFilter: after, limit,
  states, kinds, not-prepared-by). Boot wires a lister over core.gate under the caller's RLS.
- Reading gates needs at least one kind's approver capability ("any"); GET /gates/{gate_id}
  then needs that kind's approver capability ("the") or to be the gate's preparer. A walled
  subject is 404. The kind check runs after the gate is loaded (one Get call).
- Inbox default (decidable_by_me=true): pending gates of held kinds, minus the caller's own
  (an agent counts as its acting-for user). A malformed gate_id answers 404.
- Each detail view appends one `gate.viewed` audit event in its own transaction.
- `decisions` is always empty: the engine does not expose per-decision records yet.

## HTTP-13: gate decision and withdraw routes

- Decisions need a human principal (agent -> 403 agent_cannot_decide), at least one approver
  capability (403 before any engine call), then the kind's approver capability on the
  subject (re-checked at decision time; walled -> 404). Body `version` and If-Match both
  must equal the gate version (412). The preparer -> 403 self_approval unless approving
  with a sole_practitioner_declaration, which the engine's self-approval policy then decides.
- Step-up comes from `DecideHTTP.StepUp` (when the session last passed MFA). Missing, or
  older than 15 min for high/irreversible kinds -> 401 step_up_required with
  `WWW-Authenticate: Bearer error="insufficient_user_authentication"` (RFC 9470), matching
  kernel/gate's problem table and ARC-I-017's test. An engine ErrMFARequired answers the
  same. Decided in HTTP-13b (the accept line's 403 is superseded).
- Approval runs the effect inside Engine.Decide in the route's transaction; the PG engine
  wraps it in gate.WithEffect. gatefake does not set WithEffect, so the test asserts the
  effect ran once per approval, not the context mark.
- Withdraw by anyone but the preparer (or an agent acting for them) -> 403 not_preparer
  (proposed code). Problem codes self_approval, cooling_off, gate_not_open,
  payload_changed, already_decided, not_preparer are proposed. trust.ErrInsufficientFunds
  from an effect maps to 409 insufficient_trust_funds.

## HTTP-14: payment list, record and get routes (files outside the task)

- `payment.Service` gains `List(ctx, ListFilter)` and `ListFilter` in contract.go (additive,
  as the accept line asks); `paymentfake` gains `List` and an optional `NewID` so tests get
  uuid payment ids. Pages run by id.
- A body with a `trust_ledger` allocation, or a service ErrRetainerToOperating /
  ErrTrustReceipt, answers 422 `use_trust_receipts` (proposed code) with detail and instance
  pointing at /v1/trust/receipts; PaymentCreate has no account field, so "trust-account
  payment body" is read as a trust_ledger target.
- POST /payments captures to at most one bill; more than one bill allocation answers 422
  validation_failed pointing at POST /payments/{payment_id}/allocations.
- Payment has no row version or allocation ids yet: `version` is 1, ETag "1", and each
  allocation id is a stable uuid derived from (payment id, index). received_at echoes the
  business date (midnight America/Toronto), since Capture keeps only ReceivedOn.
  processor_fee_cents is stored and returned since HTTP-14b (below).
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## Q-HTTP-08b Submit moves the bill and adds delivery_method to the submit body

- `bill.Service` gains `Submit(ctx, tx, id)` and `bill.ErrNotSubmittable` (contract.go,
  additive); billfake implements it. Submit moves a draft to pending_approval and bumps its
  version. POST /bills/{bill_id}/submit now calls it in the same transaction as the
  billing.bill_approval gate open: a failed open rolls the move back, and a bill that left
  draft between the read and the tx answers 409 bill_not_submittable with no gate.
- api/openapi.yaml: POST /bills/{bill_id}/submit gains an optional request body
  `{"delivery_method": "email" | "portal" | "mail"}` (default email, no other fields), the
  value the bill approval payload needs. Optional request bodies are additive under
  ARC-I-001; listed in kernel/contract/additive.txt as
  `/paths/~1bills~1{bill_id}~1submit/post/requestBody`.
- Only billfake implements bill.Service today; the PG bill service (boot follow-up) must
  implement Submit with `UPDATE ... SET status='pending_approval' WHERE status='draft'`.

## Q-HTTP-10c TrustLedger declares overdrawn and shortfall_cents

- Closes the open item under HTTP-10b. Owner decision: the approved-additions list moves
  from kernel/contract/additive.txt to api/contract-additive.txt, because it lists openapi
  pointers and describes the api contract, not kernel code. kernel/contract/openapi_test.go
  reads it from the new path with the same checks (every addition listed with a
  QUESTIONS id, no stale lines, no duplicates). TestKernelHasNoLegalTerms and
  tools/kernel_allow.txt are unchanged; nothing is exempted.
- api/openapi.yaml: `TrustLedger` gains `overdrawn: {type: boolean}` and
  `shortfall_cents: {$ref: Cents}` (>= 0), as GET /trust/ledgers already sends (BIL-I-010,
  By-law 9). Optional response fields are additive under ARC-I-001; listed in
  api/contract-additive.txt as `/components/schemas/TrustLedger/properties/overdrawn` and
  `/components/schemas/TrustLedger/properties/shortfall_cents`.
- The trust ledger list tests validate the whole page against the GET /trust/ledgers 200
  schema and fail on any ledger field TrustLedger does not declare.

## HTTP-14b: store processor_fee_cents on the payment

- `payment.Capture` gains `ProcessorFeeCents` (additive). POST /payments passes it through
  and the Payment body returns it on create and get. A fee below 0 or above amount_cents
  answers 400 validation_failed; paymentfake refuses the same with ErrInvalid.
- No migration: `billing.payment.processor_fee_cents` already exists in the frozen
  0001_init.sql (`bigint NOT NULL DEFAULT 0`, `CHECK (deposit_to = 'operating' OR
  processor_fee_cents = 0)`), so an additive nullable column would duplicate it. The
  Postgres payment service, when built, writes Capture.ProcessorFeeCents to that column.

## Q-HTTP-16 Direct trust receipt posting versus the gate-owned receipt key

- HTTP-16 accept: an unambiguous, under-limit POST /trust/receipts answers 201 and posts the
  receipt (Ledgers.Receive) in the route; openapi says the same ("Posts directly when
  classification is unambiguous and cash is under limit").
- app/legal/gateowned.go lists `trust_txn.posted=receipt` as owned by
  trust.receipt_classification (ARC-I-012). The route must declare what it writes, so the
  201 path would declare that key and TestRoutesConformToOpenAPI's gate-owned-state guard
  fails. Declaring a different key for the same write would hide the conflict.
- Options: (A) split the key: `trust_txn.posted=receipt_classified` stays gate-owned and the
  route declares `trust_txn.posted=receipt_unambiguous` as route-owned, with the
  unambiguous test written down (ledger found for account+matter, no payment_id, method not
  cash or cash under the limit); (B) every receipt goes through trust.receipt_classification
  (always 202), which contradicts the openapi summary and the HTTP-16 accept line.
  Proposed: A. HTTP-16 is not started until this is answered.

## HTTP-17: trust withdrawal request and cancel routes

- POST /trust/withdrawals returns 202 with the openapi WithdrawalRequest body (status
  awaiting_authorization, gate_id), not the Gate body the accept line names: openapi wins.
- The route records the request through withdraw.Service.Request and opens the gate itself
  (trust.withdrawal, or billing.trust_fee_withdrawal when bill_id is set). A real Service
  must not also open a gate; please confirm.
- New ports in http_request.go (Service stays frozen): LedgerReader (ledger_id -> account and
  matter), Cancels (row version, ledger, bill; records the cancellation), Gates (Open, Withdraw).
- Method wire answers 422 (no wire method in the module); direction_document_id carries the
  Form 9A EFT authorization id. Only the preparer can withdraw the gate (403 otherwise).

## HTTP-18: trust reconciliation import, prepare and submit routes

- New ports in http_write.go (Service stays frozen): Parser (CSV/OFX file -> Statement,
  ErrUnparseable), Versions (row version for If-Match), Gates (Open, Get). No parser exists on
  main yet; boot wiring must supply one.
- Import answers 202 `{statement_id, lines}` (openapi has no body schema) and posts no journal
  entry. A statement whose lines do not sum to its closing balance answers 422
  unparseable_statement.
- Prepare: the imported statement's closing balance is the bank balance (answered, HTTP-18b).
  A bank_balance_cents that differs from it answers 422 `bank_balance_mismatch` before
  anything is prepared; it is never ignored. The check reads the closing balance through a new
  `Statements` port (Closing), since Service is frozen; reconfake implements it and boot
  wiring must supply the Postgres one (until then the routes answer 503).
- Submit is refused 422 unexplained_difference unless the three-way totals agree after
  reconciling items; a signed-off reconciliation answers 409 period_locked.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-19: trust request routes (approved scope)

- No trust-request service existed; the owner approved a minimal one because the operations
  are in the frozen openapi (same rule as HTTP-05). `trust/request.go` adds `Request`,
  `Requests` (Create, Get, List) and the Postgres `RequestStore` on a new table
  `trust.trust_request` (migration `5504_trust_request.sql`, trust block). `trustfake.Requests`
  is the in-memory fake. Boot wiring supplies the store; until then the routes answer 503.
- Create writes a draft only. The agent may draft for the person it acts for
  (`prepared_by_agent`), but never approves; the preparer never approves their own request.
  Both are enforced by `CheckApprover` and the table's CHECK (approved_by <> prepared_by).
  There is no approve route yet: approval belongs to a later gate step.
- GET needs trust.view, POST needs trust.request; walls and RLS limit rows. openapi has no
  body or response schema for these operations; the views are documented here until it does.
- `billing.retainer_request` (0001) is left untouched; reconciling the two is a follow-up.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008), as in HTTP-05.

## HTTP-20: document list, upload and page text routes

- openapi gives GET/POST /documents no body schema. POST /documents takes JSON
  `{matter_id, folder_id, kind, filename?, content}` with `content` base64 (32 MiB cap); it
  answers 201 `{id, matter_id, kind, folder_id, version_id}`. The page route answers
  `{document_id, version_id, page, lines:[{no, text}]}`.
- The routes read and write through a `DocumentStore` port in http.go (List, Document,
  Upload, HasVersion) plus kernel/docs `Store.Lines`; boot implements Upload as
  kernel/docs `Store.Put` and `Folders.File` in one tx. No contract.go change.
- A version that belongs to another document answers 404, as does a page out of range
  (kernel/docs ErrBadRange). An unknown kind answers 422 `docs.unknown_kind` before any
  service call. Upload checks access on the matter subject (`matter.matter`), so a walled
  matter answers 404.
- Routes register from http.go's init through `add`, so register.go is unchanged.
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-21: document release and agent draft routes

- Built on task/HTTP-20 (same package; it reuses that branch's helpers). http.go gains one
  split: `authorize` now calls `check`, so the release routes share the principal and
  capability checks.
- POST /documents/{document_id}/release takes `{channel, version_ids}` (channel per the
  docs.outgoing payload schema; each version must belong to the document, else 422) and
  opens `docs.outgoing` with `{matter_id, document_version_ids, channel}`. It answers 202
  with the Gate body and declares `document.release_requested`; `document.released` stays
  gate-owned. The release effect itself is boot work (docs.outgoing is still in
  app/legal/boot/unwired_gates.txt).
- The route checks `documents.release` as the accept line says; the gate still refuses the
  preparer as approver.
- GET /drafts reads a `Drafts` port in http_release.go (List by DraftFilter); boot
  implements it over docs.document and the agent run log. Item shape:
  `{document_id, version_id, matter_id, kind, run_id, acting_for, created_at}`.
- The Gate body copies bill's gate rendering (min_approvers 1 per spec/gates.json); a
  shared renderer in kernel/gate would be a follow-up.

## HTTP-22: deadline list, propose and get routes (kernel/deadline)

- `deadline.Engine` has no List: the route reads a `Lister` port in kernel/deadline/http.go
  (List by ListFilter, ordered by id). PGBook implementing it is a boot follow-up. No
  contract.go change.
- The kernel handler takes every app name from `deadline.HTTPConfig` (capabilities, the
  parent subject kind and its body/payload key, the deadline subject kind, the confirm
  gate). app/legal/register.go supplies `DeadlineRoutes` (deadlines.view, deadlines.edit,
  matter.matter, matter_id, deadline.deadline, cal.confirm) and mounts it from Register
  after the modules loop: modules.go is one entry per package with a register.go, and
  kernel/deadline is not an app package.
- POST /deadlines takes `{matter_id, title, pack_id, rule_id, trigger_date}`; it computes,
  saves the deadline proposed and opens cal.confirm with the gates.json payload
  (`source` = pack id and version) in one tx, then answers 202 with the Gate body. A rule
  that yields more than one deadline answers 422. It declares `deadline.status=proposed`;
  Engine.Confirm is only the gate effect's (still listed in boot/unwired_gates.txt).
- ErrUnknownRule and ErrUnknownPack answer 422 `unknown_rule` (proposed code).
- A reused Idempotency-Key with a different body answers 422 (ARC-I-008, kernel/idem).

## HTTP-23: work-item routes (approved scope; files outside the task)

- The routes serve over the existing kernel `work.Service` port (Create, Get, List, Move,
  SetApprover) and its `workfake`. There is no Postgres work.Service yet: boot must wire one
  (PORT follow-up); until then the routes answer 503.
- Additive contract change: `kernel/work.Filter.After` (page cursor, id order), implemented in
  workfake, so GET /work-items can page with the opaque cursor.
- New package `app/legal/work`: one line in `app/legal/modules.go` and the module count in
  `app/legal/register_test.go` (36 -> 37).
- PATCH changes `status_id` (a workflow move) and/or `approver_id`; other fields are not
  editable until the service has an update method. If-Match is checked against the item's
  version; Move runs before SetApprover (which bumps the version). The approver is never the
  assignee or an agent (422 self_approval); a missing transition is 409 transition_not_allowed.
- A matter-scoped item uses subject kind "matter" (as the 6100 backfill and agent tools do);
  wall checks use the matter registry subject (matter.matter) and answer 404.
- openapi has no body or response schema for these operations; the views are documented here.

- Open: a fee transfer to the firm's own general account (spec trust.json "Payee is firm
  general account (known)") is not a matter contact, so prepare_trust_withdrawal now refuses
  it. Boot must decide whether `MatterContacts` includes the firm's general account for fee
  withdrawals, or whether a separate firm-payee id is allowed.

- Closed by FEAT-FIX-B: a fee transfer to the firm's own general account (spec trust.json
  "Payee is firm general account (known)") was refused because it is not a matter contact.
  Decision: a separate firm-payee id. See the FEAT-FIX-B entry below.
- draft_bill reads a new `agent.UnbilledWIP` port (matter's unbilled entry ids up to
  period_to) from Deps; boot (BOOT-FEAT-WIRE) must wire it. Every line action source is
  checked against it before `batch.Drafts.Create`, so a bad source creates no draft.

## FEAT-FIX-B firm general account as a fee payee (By-Law 9 s.9(1))

- Decision: `WriteDeps.FirmAccount` (the firm general account's payee id) and `WriteDeps.Bills`
  (`agent.FeeBills`, returns `withdraw.FeeBill` for a bill on the matter) are new required
  ports; nil fails closed (ErrMissingDep). No BillBalances port existed, so FeeBills is the
  equivalent.
- The firm account is a payee only when purpose_category is fees, bill_id is a bill on the
  same matter, the bill is delivered and amount_cents <= its balance due (rules BIL-L-037,
  BIL-L-035). Otherwise `ErrFirmPayeeNeedsBill` (422), `withdraw.ErrBillNotDelivered` or
  `withdraw.ErrExceedsBillBalance`. Every other payee must still be a matter contact id.
- `withdraw.Request` has no bill field, so the bill id is checked by the agent but not carried
  into the trust request. Boot (BOOT-FEAT-WIRE) must wire FirmAccount and Bills; the trust
  module's own ValidateFee remains the authoritative check at authorization.

## WO-FIX: write-off draft outcomes and the split after a trust fee transfer

- Additive in `app/legal/billing/credit/contract.go`: `StatusRejected` and `StatusWithdrawn`.
  `PGWriteOffs.Close(ctx, tx, id, status)` moves a submitted draft to one of them; both are
  final. Migration 5620 widens the `billing.write_off_draft` status check (NOT VALID,
  validated in 5621) and replaces the guard so a row that has left `draft` never changes.
  Only `status = 'draft'` drafts count toward the second-approval total (BIL-C-085).
  Open (BOOT): call `Close(..., StatusRejected)` from the G-B5 gate rejection outcome and
  `Close(..., StatusWithdrawn)` from a preparer withdraw route.
- Q-INTEG-12.1 applied: on a bill with trust fee transfers (authorized, executed or applied,
  net of excess) the draft split uses `DraftOpenBuckets`: the bucket outstanding less the
  transfer must equal the balance, and the balance less open interest is apportioned over
  fees, disbursements and tax in the bill's issued proportions (BIL-C-205, ETA s.231). The
  transfer itself is never re-split. An apportioned bucket above its outstanding fails closed.

## Q-FEAT-CORE-15 Read and trust withdrawal handlers join the explicit handler set

- `handlerset.go` `Handlers()` lists four more handlers (search_matters, get_matter,
  read_document, prepare_trust_withdrawal) so the set covers all 25 a2 tools;
  `handlerset_test.go` lists them too. The handlers live in `handlers.go`.
- prepare_trust_withdrawal builds from the `*agent.WriteTools` boot passes in Deps, so the
  run it parks is the one that WriteTools resumes on gate.decided.
- `agenttest/seed.go`: `SeedStore.Document` implements `agent.DocumentLookup`, so
  read_document runs its real handler in feature tests instead of a stand-in.

## Q-FEAT-DL-08 Deadline feature tests: stand-ins and files beyond the task list

- Task: FEAT-DL-08 (F-DL-07 Post-event follow-up)
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (F-DL-07 in
  appDefs, as FEAT-CORE-17 asks) and the generated skill
  `paploo/counsel-plugin/skills/f-dl-07-post-event-follow-up/SKILL.md`.
- The agenttest Seed's Deps carry no deadline engine, published packs, subject lookup or
  proposed-task status, so every `agent.Handlers()` tool those need ran as a stand-in. The
  deadline feature tests build them in a test helper (`toolEnv` in
  `ai_f_dl_07_feature_test.go`, reused by F-DL-08 and F-DL-09) and wrap each read tool in
  the run's Provenance. Proposed: move these ports into `agenttest.Seed.Deps`.
- `get_matter` and `read_document` have no `Handler` (only the older `ReadTools`), so they
  still run as stand-ins and the tests assert exactly those two. Proposed: a `Handler` for
  each. `agenttest.QuotedOnly` then also needs to read the real `read_document` output,
  whose quoted text sits under `pages[].lines[]`, not in a top-level `agenttools.Quoted`.
- Defs set `Grant` to the capabilities of their tools: an empty grant refuses every call.
- The event type `calendar.event_ended` is named here; no calendar module publishes it yet.

## FEAT-TOOL-05 list_deadlines

- Additive contract change (named in the task's accept text): `kernel/deadline` gains
  `Filter` and a separate `Lister` interface (`List(ctx, Filter)`), so `Engine` and its
  implementations are unchanged. deadlinefake implements it; the Postgres store has no List
  yet and must implement `Lister` before boot wires list_deadlines.
- Visibility: a named walled matter is refused with ErrNotVisible (404). Without a matter id,
  deadlines of walled matters are omitted, and deadlines on a subject that is not a matter
  are omitted too (walls cannot be checked on them: fail closed).
- The result carries ids, rule references and dates only, no document or client text, so it
  has no quoted envelope. Due dates are YYYY-MM-DD strings, which Provenance.ReadTool records.
- Registered through `ListDeadlinesHandler()` for the boot HandlerSet, not init().

## FEAT-DL-11 F-DL-05 daily digest (also FEAT-DL-12, FEAT-DL-13)

- appDefs (FEAT-CORE-17) lives in `app/legal/agent/feature`, and every feature Def imports
  that package, so `feature.appDefs()` cannot list `deadlines.DigestDef()` without an import
  cycle. The Defs are not in appDefs yet; the list has to move to a package that imports the
  modules (app/legal/boot) or take the Defs as a parameter. Decision needed before the skill
  export covers deadline features.
- agenttest `Seed.Deadline` files deadlines on subject kind `"matter"`, but list_deadlines
  (FEAT-TOOL-05) keeps only `roles.SubjectMatter` (`"matter.matter"`), so a seeded deadline
  never lists. The F-DL feature tests save their deadline on `roles.SubjectMatter` directly;
  Seed.Deadline should use `roles.SubjectMatter`.
- get_matter, read_document and compute_deadline are not in `agent.Handlers()`. The tests run
  list_deadlines as its real handler and read_document as a quoting handler over the seed's
  document text (so CheckAnswer has real citations); get_matter and compute_deadline run as
  stand-ins.

## Q-FEAT-DL-04 F-DL-02 cannot reach its tool-gap kind cal.second_check

- spec/ai/features.json gives F-DL-02 the tool_gap kind `cal.second_check` (G7b), but its tool
  list has no drafting tool: `propose_deadline` opens cal.confirm and records no draft, and
  `submit_for_approval` only submits a draft this run created (404 otherwise). So the production
  Def can never open G7b. `TestFDL02_FromFeature` pins that (second check without a draft ->
  ErrNotFound); `TestFDL02_SecondCheckViaDraft` adds `draft_document` and `documents.edit` to the
  Def and shows draft_document -> submit_for_approval opens G7b pending with the acting user as
  preparer.
- Proposal: add `draft_document` to F-DL-02's tools in spec/ai/features.json and to
  `LimitationAssessmentDef` (Tools and Grant `documents.edit`), then regenerate the skill.
- Shared: `get_matter`, `read_document` and `search_matters` have no `agent.Handler`
  (agent.ReadTools only), and `agenttest.Seed.Deps` carries no deadline engine, PublishedPacks,
  Provenance, DocumentLookup or SubjectMatters, and does not wrap read tools in
  `Provenance.ReadTool`. The deadline feature tests add these in-test (dlWorld). `agenttest.QuotedOnly`
  only knows the `agenttools.Quoted` envelope, while read_document returns
  `{"untrusted": true, pages}`; the tests check that envelope instead (docQuotedOnly).
- Event names: F-DL-01 binds `document.uploaded`; F-DL-02 binds `lead.created` (intake) and
  `matter.updated` (claim facts), all a2 events.

## Q-FEAT-DL-09 propose_deadline opens only cal.confirm, so F-DL-08 cannot open cal.change

- Task: FEAT-DL-09 (F-DL-08 Adjournment watcher)
- F-DL-08 declares gate kind `cal.change` (spec/ai/features.json). `propose_deadline` saves the
  deadline and opens `cal.confirm` itself, after `CheckGateKind`; `cal.confirm` is outside the
  F-DL-08 run scope, so a correct run is refused with `ErrGateKindNotAllowed` before anything
  is saved, and `submit_for_approval` has no draft to submit (a propose result is a gate, not
  a draft). The run fails closed; no `cal.change` gate can open, so the pending-gate, 403 and
  ErrSelfApproval checks cannot run for F-DL-08 yet. `TestFDL08_FromFeature` pins this.
- Proposed: `propose_deadline` opens the deadline gate kind the run scope allows
  (`cal.change` when the feature declares it, else `cal.confirm`), or returns a draft
  proposal that `submit_for_approval` opens under the run's kind. Either changes
  `app/legal/agent/tools_propose_deadline.go` (FEAT-TOOL-15), not this task's files.
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (F-DL-08 in
  appDefs). F-DL-08 is an extraction feature, so it gets no plugin skill.
- Event types `calendar.event_moved` and `calendar.event_cancelled` are named here; no calendar
  module publishes them yet.

- Resolved by TOOL-FIX-C: `propose_deadline` with a `deadline_id` opens `cal.change` on that
  stored deadline and holds the new date in the gate payload without applying it; without one
  it saves a new deadline under `cal.confirm`. The run scope's gate-kind check still applies.
  `TestFDL08_FromFeature` now checks the pending gate, the 403 for the agent, `ErrSelfApproval`
  for the acting user and another lawyer's approval; a Def without `cal.change` is refused.

## Q-FEAT-DL-10 create_task returns a proposal, so F-DL-09 cannot open work.absence

- Task: FEAT-DL-10 (F-DL-09 Absence coverage check)
- `create_task` creates the cover task in the proposed status and returns
  `OutcomeProposal`; only an `OutcomeDraft` result is recorded as a draft of the run, so
  `submit_for_approval` refuses `work.absence` on the cover proposal with `ErrNotFound`, and
  no write tool in the Def opens `work.absence` itself. The run fails closed (the task stays
  proposed, no gate), and the pending-gate, 403 and ErrSelfApproval checks cannot run for
  F-DL-09 yet. `TestFDL09_FromFeature` pins this and checks a kind outside the Def is
  `ErrGateKindNotAllowed`.
- Proposed: `create_task` records its proposed task as a draft of the run when the run scope
  allows `work.absence` (or returns `OutcomeDraft`), so `submit_for_approval` can open the
  gate. That changes `app/legal/agent/tools_task.go` (FEAT-TOOL-16), not this task's files.
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (F-DL-09 in
  appDefs) and the generated skill
  `paploo/counsel-plugin/skills/f-dl-09-absence-coverage-check/SKILL.md`.
- Event type `work.absence_approved` is named here; work/team-availability does not publish
  it yet.

## Q-FEAT-CON-01 F-CON-01 party extraction

- Files: the task lists `app/legal/conflicts/ai_f_con_01_test.go`; the landed AI-033 test is
  `app/legal/conflict/ai_f_con_01_test.go` (package `conflict`), so that file was moved to
  FromFeature in place. `app/legal/agent/featureset/featureset.go` gained
  `conflict.PartyExtractionDef()` (FEAT-CORE-17 registration); F-CON-01 is an extraction Def,
  so no plugin skill is generated for it.
- G2 has no kernel gate here: propose_conflict_parties runs the conflict check, and the open
  check run is the pending conflict.clearance (G2), decided through `conflict.Clearance`
  (gates.CheckDecision). So the tests assert the run is open with the acting user as preparer,
  an agent decider is 403 (`gates.ErrAgentApproves`) and the acting user is refused as preparer
  (`gates.ErrPreparerApproves`, the self-approval refusal of app/legal/gates), not
  `agent.ErrSelfApproval` (stand-in gates only).
- `get_matter`/`search_matters`/`read_document` have no `agent.Handler` and `agenttest.Seed.Deps`
  carries no `ConflictCheckParties`, `conflict.Checker`, `DocumentLookup` or `SubjectMatters`; the
  test adds them (con01World). `agenttest.QuotedOnly` does not know read_document's
  `{"untrusted": true, pages}` envelope; the tests check it (docQuotedOnly).
- Events: `lead.created` (intake submitted) and `document.uploaded` (document added), a2 events.
- The landed test retried on `ai.ErrTimeout`; FromFeature's default RetryOn does not, so the
  test sets it as the landed test did.

## Q-FEAT-CON-02 F-CON-02 opens G2 through propose_conflict_parties

- No F-CON tool drafts, so the run scope never holds a draft and an agent's
  submit_for_approval is refused (404). The run's G2 gate is the conflict run that
  propose_conflict_parties starts: conflict.Clearance decides it with gate kind
  conflict.clearance, and the run's preparer is the acting user. The feature test checks that
  run is open, that an agent principal deciding is 403 (ErrAgentApproves) and that the
  acting user deciding is 403 (conflict.ErrPreparerApproves, the conflict module's
  self-approval refusal).
- The Def starts from the a2 event `conflict.check_completed` ("event: check run").
- No F-CON-02 write carries an amount or a date, so ErrUnsourcedValue (ARC-U-027) has no
  case here. propose_conflict_parties checks a party source's shape but not that the run
  read it (Scope.Cites); that is a handler follow-up.

## Q-FEAT-CON-03 Clearance memo is the run's answer; new event conflict.hits_disposed

- As in Q-FEAT-CON-02, the G2 gate is the conflict run propose_conflict_parties starts; no
  F-CON tool drafts, so the memo is the run's final answer, not a stored draft. A memo draft
  store with a draft tool would let submit_for_approval carry it.
- a2 lists no event for "all hits disposed"; F-CON-03 binds `conflict.hits_disposed`, which
  the conflict module does not publish yet.
- The AI-035 test (app/legal/conflict/ai_f_con_03_test.go; the task's
  app/legal/conflicts/ path is a typo) now runs through agenttest.FromFeature with no
  stand-ins. The f-con-03 fixtures use a2 inputs, the injection variant no longer makes an
  extra tool call, and ok pins match.prompt_hash.
- search_matters and get_matter return matter short names unquoted (not as
  agenttools.Quoted), so an instruction in a matter name reaches the model outside quoted
  data (ARC-I-038). That is a handler follow-up.

## PORT-WORK: Postgres kernel work.Service

- `work.PG` (kernel/work/pg.go) implements `work.Service` over work.work_item, work.status and
  work.transition. Boot must wire it (with the app's `SubjectKinds`) for the HTTP-23 routes.
- Shared contract tests live in `kernel/work/worktest`; workfake runs them in unit tests and
  work.PG runs them in `pg_integration_test.go` (vet-only for now).
- Carries the HTTP-23 additive `work.Filter.After` cursor (same hunk as task/HTTP-23, so the
  merge is clean). PG pages in id order (uuidv7, creation order).
- Keys: an empty Item.Key becomes `W-` plus the random 12-hex tail of the uuidv7 id; rank
  starts as the id. A readable per-firm series (kernel/numseq) is a later choice.
- Merges task/TOOL16-DESC: work.PG writes and reads `Item.Description` (work_item.description,
  NULL for none; the 1003 check bounds it).
- Automations (work.Automations) have no Postgres implementation yet.
## TOOL16-DESC: work item description

- Additive in `kernel/work/contract.go`: `Item.Description *string` (Markdown, nil for none),
  answering the FEAT-TOOL-16 note above. `PrepareItem` refuses one longer than
  `MaxDescriptionLen` (10000, the create_task schema maximum) and turns a blank one into nil.
  `create_task` now stores `description_markdown` on the proposed item.
- `work.work_item.description` already exists as a nullable column in 0001_init.sql, so the
  migration (1003, validated in 1004) adds only the matching length/non-blank check, NOT VALID.
  There is no Postgres `work.Service` yet; when one is written it reads and writes the column.

## WD-BILL: trust withdrawal names its bill

- Additive in `app/legal/trust/withdraw/contract.go`: `Request.BillID`
  (`trust.withdrawal_request.bill_id`, already in 0001_init.sql, so no migration). The agent's
  prepare_trust_withdrawal passes its `bill_id` on (FEAT-FIX-B). The trust.withdrawal gate
  payload gains `bill_id` (omitempty), so an approval binds the bill.
- `Effect.Apply` re-checks a fee withdrawal at authorization (By-Law 9 s.9(1), BIL-L-035/037):
  it names a bill, the bill is on the same matter, delivered on or before now, and the amount
  is no more than its unpaid balance. A withdrawal with a bill id, or purpose `fees` /
  `fees_to_operating`, is a fee withdrawal. `Effect.Bills` (new port `withdraw.FeeBills`, read
  in the gate tx) nil fails closed. Open (BOOT): wire a billing adapter returning the bill's
  matter, delivery date and balance net of reserved fee transfers.
- Branch is based on task/FEAT-FIX-B (not yet on main) with origin/main merged in; the merge
  resolved the draft_bill test rename (billWorld -> draftBillWorld) and QUESTIONS.md.

## PORT-16b: written-off interest to OP-1110

- Additive in `app/legal/billing/credit/contract.go`: `WriteOff.InterestCents`, read by
  `PGWriteOffs.Lock` from the draft's `interest_cents` bucket. `BadDebtEntry` now follows
  BIL-J-009 in full: Cr OP-1100 the amount less interest, Cr OP-1110 the interest; Dr OP-6100
  still takes the pre-tax amount plus interest. Tax plus interest above the amount fails.
- Open: `ApproveWriteOff` computes the tax adjustment on the whole amount, interest included
  (`BadDebtTax(amount, billTax, billTotal)`). Interest carries no HST, so the relief should
  probably be on the amount less interest, or simply the draft's tax bucket. Not changed here.

## DL-EVENTS: the owning modules publish the deadline trigger events

- Published now, each in the writer's tx through `outbox.Emitter`:
  - `rulepack.published` (`deadline.EventPackPublished`): kernel/deadline `PGBook.Repack`,
    subject `cal.rule_pack`/pack id, body pack id, version and recomputed count. New
    `PGBook.Events` field; nil refuses Repack before any write.
  - `filing.receipt_received` (`comms.EventFilingReceiptReceived`): `FilingStore.RecordFiling`
    for a filing recorded as filed, subject `legal.court_filing`/filing id. New
    `FilingStore.Events` field; nil refuses a filed filing before any tx.
  - `holiday.added` (`ontario.EventHolidayAdded`): `ontario.PublishAddedHolidays(ctx, tx, em,
    known)` emits one event per calendar holiday not in `known`. Boot calls it when it loads
    the calendar, passing the holidays the engine used before.
  - `app/legal/deadlines/events_test.go` pins the bound names to the published constants.
- Open (no write point exists yet, so nothing publishes them):
  - `deadline.trigger_date_changed`: cal has no operation that changes a stored trigger date
    (`cal.deadline.trigger_on` is set once by Save). It belongs to that operation when added.
  - `claim.issued` and `service.affidavit_received`: comms records court filings only; there is
    no issued-claim or affidavit-of-service record to publish from.
- BOOT: set `PGBook.Events` and `FilingStore.Events` to the outbox emitter.

## PORT-STMT: Postgres recon Statements.Closing

- `recon.PGStatements` (statements_pg.go) implements the HTTP-18b `Statements` port: the
  closing balance, in cents, of the latest trust.bank_import whose period covers the month's
  last day. No statement (or only imports without a stored balance) is `ErrNotFound`.
- trust.bank_import had no balance columns, so `5530_bank_import_balances.sql` (app/legal/trust
  block) adds nullable `opening_cents` and `closing_cents`. The Postgres ImportStatement (boot
  follow-up; none on main) must write both.
- Built on main without HTTP-18b, so the interface check is in the test; after HTTP-18b merges,
  add `var _ Statements = (*PGStatements)(nil)` and wire it as `WriteHTTP.Statements` at boot.

## Q-HTTP-15 Gated payment routes: proposed allocations and refund gate

- POST /payments/{payment_id}/allocations: the sum must equal the payment (422
  allocation_sum_mismatch, ARC-I-029). A set of operating bills only, each named once, is
  proposed directly (200, allocation state `proposed`) through a new `payment.Proposals`
  port; the route declares `payment.allocation=proposed` and never
  `payment.allocation=applied`. Unapplied money (client credit) or a trust_ledger row opens
  billing.payment_allocation (202). The payment version is 1, as GET reports it.
- POST /payments/{payment_id}/refunds opens billing.refund (202) with payee = the payment's
  payer of record and reason = reason_code; an amount above the payment answers 409
  refund_exceeds_payment. The billing.refund effect (provider call keyed <gate_id>:<version>)
  and its registration are not built yet: an approved refund pays nothing until they are.
- Boot must wire `payment.Gated` (DB, Gates, Proposals and the HTTP dependencies); until it
  does, both routes answer 503.

## HTTP-25: agent runs, start and get (approved scope)

- agent.Service has no start or run query, so http.go adds the narrow `RunService` port
  (SkillGrant, Start, Run) with `ErrUnknownSkill` (422 unknown_skill) and `ErrRunNotFound`.
  Boot must wire the Paploo starter and a run store over agent.agent_run; until then the
  routes answer 503.
- POST /agent/runs takes `{skill_id, inputs?: object, matter_id?}`. The route mints the run id,
  asks `Access.AgentPrincipal(run, caller, skill grant)` (ARC-I-036 intersection) and hands
  that principal to Start; it answers 202 with the run view. A walled matter answers 404.
- GET /agent/runs/{run_id} answers 404 for a run another member started or one on a walled
  matter. Neither op lists, so the cursor and limit accept lines do not apply.
- A reused Idempotency-Key with a different body answers 422 (kernel/idem, ARC-I-008).

## HTTP-24: admin users, role change, roles, audit events (files outside the task)

- New package `app/legal/access` routes: one line in `app/legal/modules.go` and the module
  count in `app/legal/register_test.go` (36 -> 37; 38 once HTTP-23's work module also merges).
- Narrow read ports in http.go (the service gaps): `Directory` (Users, User, CheckRoleChange),
  `RoleCatalog` (Roles) and `AuditQuery` (Events, after a decimal sequence). Boot must wire
  Postgres ones; until then the routes answer 503. Audit detail leaves through `audit.Masker`.
- PUT /users/{user_id}/roles takes `{"role_id": uuid}` (core.app_user holds one role) and opens
  core.role_change with payload `{role_id, change: {user_id, from_role_id, user_version}}`
  (the kinds.json schema). It assigns nothing; the gate effect does. If-Match is the user's
  row version. `last_admin` (409) and `unknown_role` (422) come from CheckRoleChange.
- A reused Idempotency-Key with a different body answers 422 (kernel/idem, ARC-I-008), not
  the 409 the accept line names; same as the other HTTP routes.
- openapi has no body schemas for these operations; the views are documented here.
## Q-CON-03-MEMO Clearance memo kept on file; conflict.hits_disposed published

- Decided as counsel (LSO Rule 3.4 records): F-CON-03 files the memo as a draft document on
  the check's matter through draft_document (DocumentDrafts), not only as the run's answer.
  F-CON-03 gains draft_document in its Def and in spec/ai/features.json, and the grant
  documents.edit.
- The a2 draft_document input has no field that links a draft to a conflict run, so the
  memo is titled "Conflict clearance memo, run <ref>" with the ref propose_conflict_parties
  returned, and the draft carries the agent run id. A structured link (an additive subject on
  NewDraftDocument, or the run storing the memo draft id) is a follow-up. A check on a lead
  with no matter has nowhere to file the memo yet.
- conflict.Checks publishes conflict.hits_disposed (subject conflict.run) through its
  Events outbox.Emitter once, on the disposition that leaves every hit of the run disposed.
  The body is ids and a count, with acting_for set to the run's preparer. Dispositions are
  kept in memory (Q-PORT-18 C), so the event is emitted with no tx; boot must set Events, and
  while it is nil no event is published.
- Resolved by TOOL-FIX-C: when the run scope declares a gate kind on work items
  (`agent.WorkItemGateKinds`: `work.absence`, `work.output`), `create_task` returns its task
  (still proposed, no approver) as an `OutcomeDraft` of the run, so `submit_for_approval` can
  open the gate on it. `TestFDL09_FromFeature` now checks the pending gate, the 403 for the
  agent, `ErrSelfApproval` for the acting user and another lawyer's approval; a Def without
  `work.absence` still gets a proposal and the submit is refused.

## Q-TOOL-FIX-C propose_deadline input gains deadline_id (additive a2 change)

- Task: TOOL-FIX-C (FEAT-TOOL-15, FEAT-TOOL-16 follow-up; closes Q-FEAT-DL-09, Q-FEAT-DL-10)
- `propose_deadline`'s input schema gains an optional `deadline_id` (string) in
  `spec/eng/a2-interfaces.json` and its copy `app/legal/agent/schemas/tools.json`. Additive:
  every input valid before is still valid. The cal.change payload's `reason` is the input
  `title`; there is no separate reason field.
- `propose_deadline` now also reads a `DeadlineGetter` dep (kernel/deadline `Get`), which the
  deadline engine and its fakes already implement.
- `work.output` is counted as a work-item gate kind beside `work.absence` because its payload
  names an `item_id`; confirm when the F-WRK features that use it are built.
## Q-FEAT-CON-04 Conflict feature tests: stand-ins, test helpers and files beyond the task list

- Tasks: FEAT-CON-04, FEAT-CON-05, FEAT-CON-06 (F-CON-04 to F-CON-06).
- Files beyond the list: `app/legal/agent/featureset/featureset.go` and its test (each Def in
  appDefs, as FEAT-CORE-17 asks) and the generated plugin skills under
  `paploo/counsel-plugin/skills/f-con-0N-*/SKILL.md`.
- The task lists `app/legal/conflicts/ai_f_con_04_test.go`; the landed AI-036 test lives in
  `app/legal/conflict/` (package `conflict`), so it was moved to FromFeature there.
- The agenttest Seed's Deps carry no `agent.SubjectMatters`, so `submit_for_approval` would run
  as a stand-in, and its draft ids are not a2 uuids. The conflict feature tests add both through
  `conHandlers` / `conDrafts` in `ai_f_con_04_feature_test.go` (reused by F-CON-05 and F-CON-06).
  Proposed: a `SubjectMatters` and uuid draft ids in `agenttest.Seed`.
- `search_matters` and `read_document` have no `Handler`, so they still register as stand-ins.
  The ok scripts call neither; the injection scripts call `read_document` to carry the injected
  text, since `agenttest.QuotedOnly` reads only a top-level `agenttools.Quoted` result and
  `search_contacts` returns its quotes under `quotes[]`.
- Defs set `Grant` to the capabilities of their tools: an empty grant refuses every call.
- The event types named here (`conflict.clearance_subject_to_consent`, ...) have no publisher yet.
- F-CON-05: `propose_conflict_parties` opens no kernel gate. Its G2 is the open conflict run
  that `conflict.Clearance` decides through app/legal/gates, so the F-CON-05 test checks the
  run is open and prepared by the acting user, and that the agent (`conflict.ErrAgentApproves`)
  and the acting user (`conflict.ErrPreparerApproves`, the G2 self-approval refusal; kernel
  `gate.ErrSelfApproval` does not apply) are refused with 403. `submit_for_approval` cannot open
  conflict.clearance on a run: the run scope only records drafts, not proposals.

## Q-FEAT-CORE-15 Read and trust withdrawal handlers join the explicit handler set

- `handlerset.go` `Handlers()` lists four more handlers (search_matters, get_matter,
  read_document, prepare_trust_withdrawal) so the set covers all 25 a2 tools;
  `handlerset_test.go` lists them too. The handlers live in `handlers.go`.
- prepare_trust_withdrawal builds from the `*agent.WriteTools` boot passes in Deps, so the
  run it parks is the one that WriteTools resumes on gate.decided.
- `agenttest/seed.go`: `SeedStore.Document` implements `agent.DocumentLookup`, so
  read_document runs its real handler in feature tests instead of a stand-in.

## Q-FEAT-CON-07 Conflict features F-CON-07 to F-CON-09 on top of FEAT-CORE-15

- Tasks: FEAT-CON-07, FEAT-CON-08, FEAT-CON-09. Same layout as Q-FEAT-CON-04: each Def joins
  `featureset.appDefs` and its test; tool-use Defs get their generated plugin skill.
- The branches merge `task/FEAT-CORE-15`, which gives `read_document` and `search_matters` real
  handlers. The conflict scripts and the F-CON-04 fixtures read a fixed doc id without
  `version_id` through `SetRead`, so `conHandlers` (in `ai_f_con_04_feature_test.go`) now leaves
  both tools out of the handler set and they stay stand-ins. Proposed follow-up: seed the
  documents with `agenttest.Seed.Document`, pass `version_id`, and check the injection with a
  helper that accepts read_document's `{"untrusted": true}` envelope (F-CON-02 has one,
  `conQuotedOnly`).
- `task/FEAT-CON-02`/`03` and `task/FEAT-CON-04`..`06` both declare `conFeature`, `conScript`,
  `conCall` and others in package `conflict_test` with different signatures; merging both
  chains will not compile until one set is renamed.
- F-CON-07's model call is extraction (spec): its Def sets `Kind: ai.KindExtraction`, so it
  exports no plugin skill.
- Event types with no publisher yet: `identity.portal_submission` (F-CON-07),
  `party.organization_client_created` (F-CON-08), `roles.walled_matter_accessed` (F-CON-09).
- F-CON-09's "schedule: review dates" is a per-wall date, so its binding checks daily
  (`trigger.ParseCadence`) and needs the acting user boot names, like F-CON-05. The F-CON-07
  and F-CON-08 gate-kind tests now use `conflict.GateWall` for the out-of-Def kind.
- Event types with no publisher yet: `identity.portal_submission` (F-CON-07).

## Q-FEAT-CON-10 Conflict features F-CON-10, F-CON-15 and F-CON-16

- Tasks: FEAT-CON-10, FEAT-CON-11, FEAT-CON-12. Same layout as Q-FEAT-CON-07: each Def joins
  `featureset.appDefs` and its test, and gets its generated plugin skill. The branches stack on
  `task/FEAT-CON-07` (which carries FEAT-CORE-15); `featureset.go` and its test take one line
  each per task, so they merge with FEAT-CON-08/09 by keeping both lines.
- F-CON-15 and F-CON-16 are screen actions. The spec words the button in `trigger`
  ("on request: ethical-walls > Propose wall") and leaves `on_request` empty, while the screen
  action route reads `Def.Actions`. The Defs set `Actions` to the trigger's button; their spec
  test checks that and compares the rest of the Def with `Actions` cleared. Proposed: fill
  `on_request` for screen-action features in `spec/ai/features.json`.
- Gate kind constants `GateIDDestruction` (G14), `GateWall` (G2b) and `GateLateral` (G2c)
  live in the Def files. `conflict.id_destruction` needs two approvers; the feature test checks only
  that another lawyer may decide, not that the batch is fully approved.
- F-CON-10's weekly binding needs an acting user from boot (the records officer); boot wiring
  is not in these tasks.
  `party.organization_client_created` (F-CON-08).

## Q-FEAT-CON-13 Read-only conflict features F-CON-11 to F-CON-14 and F-CON-17

- Tasks: FEAT-CON-13 (F-CON-11), FEAT-CON-14 (F-CON-12), FEAT-CON-15 (F-CON-13, F-CON-14,
  F-CON-17). Each Def joins `featureset.appDefs` (outside the task's file list); chat and
  embedding Defs export no plugin skill.
- The shared read-only harness lives in `ai_f_con_11_feature_test.go`: `search_contacts` and
  `read_document` run their real handlers over `agenttest.Seed`, with read_document's first
  page re-quoted as `agenttools.Quoted` so `agenttest.QuotedOnly` and `run.CheckAnswer` see
  its citation. `search_matters` stays a stand-in the scripts never call (the Seed has no
  `matter.Query`).
- None of the three read tools returns a date as a JSON value, so the unsourced-date check
  counts a date inside a quoted document line as read in the run. Proposed follow-up: have
  read_document return `agenttools.Quoted` itself so the harness needs no re-quote.
- The agent-never-approves check goes through `conflict.Clearance` on a check the agent
  prepared for the acting user: agent principal -> `ErrAgentApproves`, acting user ->
  `ErrPreparerApproves`, both 403.
## Q-FEAT-DOC-03 QuotedOnly accepts a real read handler's untrusted output

- The document drafting features run read_document's real handler over Seed documents (no
  stand-ins). Its output is `{"untrusted": true, "doc_id", "version_id", "pages": [...]}`, not
  an `agenttools.Quoted`, so `agenttest.QuotedOnly` reported the quoted lines as unquoted.
- `agenttest/agenttest.go`: QuotedOnly now also accepts a tool_result marked untrusted at its
  top level whose string values hold the text (additive; the Quoted path is unchanged).
  `agenttest/quoted_test.go` covers both shapes and the refusals.
- F-DOC-02 binds both `conflict.cleared` and `lead.converted` (the spec trigger names G2 and
  G1); the prompt has the agent draft only when the matter shows both done.
## Q-FEAT-DOC-02 F-DOC-01 drafts its filing review: draft_document added to the Def and spec

- FEAT-DOC-02 asks that the tool-gap kind docs.filing_privilege (G-D1) be opened "via
  draft_document then submit_for_approval". submit_for_approval opens a gate only for a
  subject the run drafted, and F-DOC-01 listed no draft tool, so its run could not open G-D1.
- As decided for F-DL-02 (Q-FEAT-DL-04), draft_document is added to F-DOC-01 in
  spec/ai/features.json and in IngestDocumentDef (app/legal/docs/ai_f_doc_01.go), with its
  capability documents.edit in the Grant. The change is additive; the extraction pipeline
  (IngestPipeline and its IngestSink) is unchanged.
- The four intake channels (upload, email-in, portal upload, scan) all file a document
  version, and a2 names one event for that, document.uploaded, so the binding has one event.
