#!/usr/bin/env python3
"""Generate spec/tasks.json and docs/TASKS.md from spec/eng/o1-tasks.json,
spec/ai/tasks-ai.json and spec/blockers.json. Deterministic: same inputs, same bytes.
Usage: python3 -I tools/gen_tasks.py [repo_root]"""
import json
import os
import re
import sys
from collections import Counter

R = sys.argv[1] if len(sys.argv) > 1 else os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
O = json.load(open(R + '/spec/eng/o1-tasks.json'))
A = json.load(open(R + '/spec/ai/tasks-ai.json'))
B = json.load(open(R + '/spec/blockers.json'))
OT = {t['id']: t for t in O['tasks']}

FLAGS = {'V-TRUST': 'trust.bylaw9_verified', 'V-BILL': 'billing.solicitors_act_verified',
         'V-TAX': 'tax.verify_rules', 'V-CFA': 'billing.contingency_fees',
         'V-DEADLINES': 'deadline.ontario_pack', 'V-ID': 'conflict.id_verification',
         'V-SURCHARGE': 'billing.card_surcharge'}
DECIDED = {b['id'] for b in B if b['status'] in ('decided', 'assumed')}

def C(title, files, a=(), x=(), d=(), **o):
    return dict(title=title, files=list(files), a=list(a), x=list(x), d=list(d), o=o)

def kt(p, names):  # files with test next to code
    out = []
    for n in names:
        out.append(f'{p}/{n}')
    return out

S = {}
S['T-001'] = [
    C('go.mod with allowed deps and cmd main stubs', ['go.mod', 'go.sum', 'cmd/counseld/main.go', 'cmd/counsel-worker/main.go', 'cmd/counsel-migrate/main.go', 'cmd/counsel-import/main.go'],
      x=['go build ./... succeeds; each main.go only parses flags and calls kernel/boot (no logic)', 'go.mod requires only modules in the AGENTS.md allowed list'], w0=True),
    C('Makefile check, rule coverage and layering test', ['Makefile', 'tools/rule_coverage.py', 'tools/layering_test.go'], a=[0, 1, 2], w0=True),
    C('CI workflow and e2e runner with suite auto-discovery', ['.github/workflows/ci.yaml', 'test/e2e/run.sh', 'test/e2e/lib.sh'], a=[3],
      x=['test/e2e/run.sh runs every test/e2e/suites/*.sh it finds (no per-suite registration), so suite tasks never edit run.sh', 'CI runs make check on every push and e2e on the wave branch'], w0=True),
    C('web/ scaffold: Vite, Vitest, React, feature auto-discovery', ['web/package.json', 'web/tsconfig.json', 'web/vite.config.ts', 'web/vitest.config.ts', 'web/index.html', 'web/src/main.tsx', 'web/src/app/features.ts', 'web/src/app/features.test.ts', 'AGENTS.md', 'docs/QUESTIONS.md'],
      x=['npm run test (vitest) passes and make check runs it', 'web/src/app/features.ts discovers web/src/features/*/feature.ts with import.meta.glob, so feature tasks never edit a shared file',
         'AGENTS.md allowed deps row Front end lists Vite and Vitest; docs/QUESTIONS.md has a Q entry recording why (build and unit-test tooling for web/)'], w0=True),
]
S['T-001b'] = [C('Kernel boundary ratchet tests', ['kernel/boundary_test.go', 'tools/kernel_terms.txt', 'tools/kernel_allow.txt', 'tools/kernel_bindings.txt'], a=[0, 1, 2, 3, 4], w0=True)]
S['T-002'] = [
    C('Import api/openapi.yaml with contract-diff test', ['api/openapi.yaml', 'kernel/contract/openapi_test.go'], a=[0], w0=True),
    C('Import migrations/0001_init.sql with smoke test', ['migrations/0001_init.sql', 'migrations/smoke.sql', 'kernel/contract/ddl_integration_test.go'], a=[1], w0=True),
    C('spec/kernel JSON Schemas for registered extension data', ['spec/kernel/', 'kernel/contract/schema_test.go'], a=[2], w0=True),
    C('kernel/registry: frozen Registry interface (k1)', ['kernel/registry/registry.go', 'kernel/registry/types.go', 'kernel/registry/registry_test.go'],
      x=['Registry interface matches k1-kernel.html exactly and carries a CONTRACT header', 'A recording fake Registry (registrytest) captures every call for module tests'], w0=True),
    C('Migration numbering scheme and per-module blocks', ['migrations/README.md', 'tools/migration_blocks.txt', 'tools/migration_blocks_test.go'],
      x=['Each module owns a 100-number block (e.g. 0100-0199 kernel/audit); a migration file outside its module block fails make check', '0001_init.sql stays the only CONTRACT DDL; later files are additive expand-contract only'], w0=True),
    C('kernel/registry boot validation against spec/kernel schemas', ['kernel/registry/validate.go', 'kernel/registry/validate_test.go'], d=[3, 4],
      x=['Boot fails on duplicate ids, on a kernel-prefixed app id, and on data that fails its spec/kernel schema']),
]
S['T-002L'] = [
    C('spec/rules/*.json union of b1,b2,b3,a1,a2,a3', ['spec/rules/', 'tools/rules_union_test.go'], a=[0], w0=True),
    C('spec/gates.json and spec/access.json', ['spec/gates.json', 'spec/access.json', 'tools/gates_access_test.go'], a=[1], w0=True),
]
S['T-003'] = [
    C('kernel/money: Cents, PPM, Rhu', ['kernel/money/money.go', 'kernel/money/money_test.go'], a=[0, 1, 2, 3, 5]),
    C('kernel/money: Allocate largest remainder', ['kernel/money/allocate.go', 'kernel/money/allocate_test.go'], a=[6], d=[1]),
    C('No-float analyzer for kernel/ and app/', ['tools/nofloat_test.go', 'tools/nofloat_allow.txt'], a=[4]),
]
S['T-004'] = [
    C('kernel/ids: monotonic ids', ['kernel/ids/ids.go', 'kernel/ids/ids_test.go'], a=[0]),
    C('kernel/clock: business date in cell zone', ['kernel/clock/clock.go', 'kernel/clock/clock_test.go'], a=[1]),
    C('kernel/numseq: gapless number series', ['kernel/numseq/numseq.go', 'kernel/numseq/numseq_integration_test.go'], a=[2]),
]
S['T-005'] = [
    C('kernel/audit: Append with hash chain', ['kernel/audit/audit.go', 'kernel/audit/chain.go', 'kernel/audit/chain_test.go'], a=[0]),
    C('kernel/audit: Verify and immutability triggers', ['kernel/audit/verify.go', 'kernel/audit/immutable_integration_test.go'], a=[1], d=[1]),
    C('kernel/audit: detail masking', ['kernel/audit/mask.go', 'kernel/audit/mask_test.go'], a=[2], d=[1]),
]
S['T-006'] = [
    C('kernel/identity: roles, capabilities, Can()', ['kernel/identity/can.go', 'kernel/identity/roles.go', 'kernel/identity/can_test.go'], a=[0, 3]),
    C('kernel/identity: SubjectResolver visibility', ['kernel/identity/resolver.go', 'kernel/identity/resolver_test.go'], a=[1], d=[1]),
    C('kernel/identity: AgentPrincipal capability intersection', ['kernel/identity/agent.go', 'kernel/identity/agent_test.go'], a=[2], d=[1]),
    C('kernel/identity: per-request RLS session', ['kernel/identity/session.go', 'kernel/identity/session_integration_test.go'], a=[4], d=[1]),
]
S['T-008'] = [
    C('kernel/idem: Idempotency-Key store and replay', ['kernel/idem/idem.go', 'kernel/idem/idem_test.go'], a=[0]),
    C('Money POST requires Idempotency-Key (conformance)', ['kernel/server/middleware/require_idem.go', 'kernel/server/middleware/require_idem_test.go'], a=[1], d=[1]),
    C('kernel/outbox: Emit in tx and relay', ['kernel/outbox/outbox.go', 'kernel/outbox/relay.go', 'kernel/outbox/relay_integration_test.go'], a=[2]),
    C('kernel/outbox: gap-free subject_seq', ['kernel/outbox/seq.go', 'kernel/outbox/seq_integration_test.go'], a=[3], d=[3]),
]
S['T-008b'] = [
    C('kernel/inbound: provider registration and dedup', ['kernel/inbound/inbound.go', 'kernel/inbound/inbound_test.go'], a=[0, 2]),
    C('kernel/inbound: signature and timestamp check', ['kernel/inbound/verify.go', 'kernel/inbound/verify_test.go'], a=[1], d=[1]),
]
S['T-007'] = [
    C('kernel/gate: kind registration and Open', ['kernel/gate/kind.go', 'kernel/gate/open.go', 'kernel/gate/kind_test.go'], a=[0]),
    C('kernel/gate: JCS payload hash and versions', ['kernel/gate/version.go', 'kernel/gate/jcs.go', 'kernel/gate/version_test.go'], a=[1], d=[1]),
    C('kernel/gate: Decide with SoD and authority limits', ['kernel/gate/decide.go', 'kernel/gate/decide_test.go'], a=[2, 6], d=[1]),
    C('kernel/gate: quorum and expiry', ['kernel/gate/quorum.go', 'kernel/gate/expiry.go', 'kernel/gate/quorum_test.go'], a=[3, 4], d=[3]),
    C('kernel/gate: effect runs exactly once', ['kernel/gate/effect.go', 'kernel/gate/effect_integration_test.go'], a=[5], d=[3]),
    C('kernel/gate: SelfApprovalPolicy hook (default deny)', ['kernel/gate/selfapproval.go', 'kernel/gate/selfapproval_test.go'], a=[7], d=[3]),
]
S['T-009'] = [
    C('kernel/server: problem+json and log scan', ['kernel/server/problem.go', 'kernel/server/problem_test.go'], a=[1]),
    C('kernel/server: ETag and If-Match', ['kernel/server/etag.go', 'kernel/server/etag_test.go'], a=[0]),
    C('kernel/api: cursor paging', ['kernel/api/cursor.go', 'kernel/api/cursor_test.go'], x=['Opaque cursor round-trips; tampered cursor -> 400 problem+json; limit capped at the OpenAPI maximum']),
    C('kernel/server: route registration and OpenAPI conformance', ['kernel/server/routes.go', 'kernel/server/conformance_test.go'], a=[2, 3], d=[1]),
]
S['T-010'] = [
    C('kernel/ledger: balanced idempotent Post', ['kernel/ledger/post.go', 'kernel/ledger/post_integration_test.go', 'kernel/ledger/post_test.go'], a=[0, 3]),
    C('kernel/ledger: Reverse', ['kernel/ledger/reverse.go', 'kernel/ledger/reverse_test.go'], a=[1], d=[1]),
    C('kernel/ledger: periods and ClosePeriod', ['kernel/ledger/period.go', 'kernel/ledger/period_integration_test.go'], a=[2], d=[1]),
    C('kernel/ledger: chart registration and Balance', ['kernel/ledger/chart.go', 'kernel/ledger/balance.go', 'kernel/ledger/chart_test.go'], a=[4], d=[1]),
]
S['T-036'] = [
    C('kernel/webhook: Standard Webhooks signing', ['kernel/webhook/sign.go', 'kernel/webhook/sign_test.go'], a=[0]),
    C('kernel/webhook: ordered delivery per endpoint and subject', ['kernel/webhook/deliver.go', 'kernel/webhook/deliver_integration_test.go'], a=[1], d=[1]),
    C('kernel/webhook: registered event types with schema', ['kernel/webhook/types.go', 'kernel/webhook/types_test.go'], a=[2]),
]
S['T-032'] = [
    C('kernel/docs: document kinds and upload', ['kernel/docs/kind.go', 'kernel/docs/upload.go', 'kernel/docs/kind_test.go'], a=[0]),
    C('kernel/docs: page/line text and citations', ['kernel/docs/text.go', 'kernel/docs/cite.go', 'kernel/docs/cite_test.go'], a=[1], d=[1]),
    C('kernel/docs: blob storage, no text in logs', ['kernel/docs/blob.go', 'kernel/docs/blob_test.go'], a=[2], d=[1]),
    C('kernel/retention: retention classes', ['kernel/retention/class.go', 'kernel/retention/class_test.go'], a=[3]),
    C('kernel/retention: purge proposals via destruction gate', ['kernel/retention/purge.go', 'kernel/retention/purge_test.go'], d=[4],
      x=['A purge run only inserts proposals; rows are destroyed only in the destruction gate effect; a never_purge class is never proposed']),
]
S['T-034'] = [
    C('kernel/work: work items on registered subjects', ['kernel/work/item.go', 'kernel/work/item_test.go'], a=[1]),
    C('kernel/work: boards and workflows', ['kernel/work/board.go', 'kernel/work/workflow.go', 'kernel/work/workflow_test.go'], d=[1],
      x=['Transition not in the workflow definition -> 409; board columns map 1:1 to workflow states']),
    C('kernel/work: append-only automation', ['kernel/work/automation.go', 'kernel/work/automation_test.go'], a=[0], d=[2]),
]
S['T-031'] = [
    C('kernel/deadline: rule pack loader', ['kernel/deadline/pack.go', 'kernel/deadline/pack_test.go'], a=[0]),
    C('kernel/deadline: day-count computation', ['kernel/deadline/compute.go', 'kernel/deadline/compute_test.go'], d=[1],
      x=['Golden dates for clear, business and calendar modes with a non-legal test pack and holiday calendar (test/fixtures/helpdesk)']),
    C('kernel/deadline: human confirmation', ['kernel/deadline/confirm.go', 'kernel/deadline/confirm_test.go'], a=[1], d=[2]),
    C('kernel/deadline: reminder worker', ['kernel/deadline/remind.go', 'kernel/deadline/remind_test.go'], a=[2], d=[3]),
]
S['T-035'] = [
    C('kernel/notify: templates and channel preferences', ['kernel/notify/notify.go', 'kernel/notify/notify_test.go'], a=[0]),
    C('kernel/notify: delivery worker', ['kernel/notify/worker.go', 'kernel/notify/worker_test.go'], d=[1],
      x=['Email goes through the outbox and is retried; a channel the user turned off is never sent']),
    C('kernel/portal: guest identity and sign-in', ['kernel/portal/auth.go', 'kernel/portal/auth_test.go'], a=[1]),
    C('kernel/portal: page registration', ['kernel/portal/pages.go', 'kernel/portal/pages_test.go'], d=[3],
      x=['PortalPage loaders registered by the app; an unregistered page -> 404; guest session scoped to its grants']),
    C('web portal shell', ['web/src/kernel/portal/PortalShell.tsx', 'web/src/kernel/portal/PortalShell.test.tsx'], d=[4],
      x=['Vitest: shell renders registered portal pages only; signed-out guest is redirected to sign-in']),
]
S['T-037'] = [
    C('kernel/agenttools: Tool registration and input schema', ['kernel/agenttools/tool.go', 'kernel/agenttools/tool_test.go'], a=[0]),
    C('kernel/agenttools: write tools draft or open gates only', ['kernel/agenttools/write.go', 'kernel/agenttools/write_test.go'], a=[1], d=[1]),
    C('kernel/agenttools: injection returned as quoted data', ['kernel/agenttools/quote.go', 'kernel/agenttools/quote_test.go'], a=[2], d=[1]),
    C('kernel/agenttools: citation shape', ['kernel/agenttools/cite.go', 'kernel/agenttools/cite_test.go'], a=[3], d=[1]),
    C('kernel/agenttools: plans and steps', ['kernel/agenttools/plan.go', 'kernel/agenttools/plan_test.go'], d=[1],
      x=['Plan with steps is persisted; each step links its tool calls; a run resumes at the first unfinished step after a crash']),
]
S['T-038'] = [
    C('kernel/report: ReportDef registration and Run', ['kernel/report/def.go', 'kernel/report/run.go', 'kernel/report/def_test.go'], a=[0]),
    C('kernel/report: River schedules', ['kernel/report/schedule.go', 'kernel/report/schedule_test.go'], d=[1], x=['A scheduled run is a River job; rerun of the same schedule slot creates no second run']),
    C('kernel/report: CSV/PDF export with manifest', ['kernel/report/export.go', 'kernel/report/export_test.go'], a=[1], d=[1]),
]
S['T-039'] = [
    C('kernel/identity: role change and activation gates', ['kernel/identity/admin_gates.go', 'kernel/identity/admin_gates_test.go'], a=[0]),
    C('kernel/identity: break-glass grants', ['kernel/identity/breakglass.go', 'kernel/identity/breakglass_test.go'], a=[1]),
    C('kernel/identity: access reviews and offboarding', ['kernel/identity/review.go', 'kernel/identity/review_test.go'], a=[2]),
    C('web access admin pages', ['web/src/kernel/access/AccessAdmin.tsx', 'web/src/kernel/access/AccessAdmin.test.tsx'], d=[1, 2, 3],
      x=['Vitest: role change submits a gate (never a direct write); break-glass shows its expiry']),
]
S['T-040'] = [
    C('kernel/importer: source registration and checkpoints', ['kernel/importer/source.go', 'kernel/importer/checkpoint.go', 'kernel/importer/checkpoint_test.go'], a=[0]),
    C('kernel/importer: id map and idempotent transform', ['kernel/importer/idmap.go', 'kernel/importer/idmap_test.go'], a=[1], d=[1]),
    C('kernel/importer: dry run and import gate', ['kernel/importer/dryrun.go', 'kernel/importer/dryrun_test.go'], a=[2], d=[2]),
    C('kernel/importer: UTC instants, unshifted dates', ['kernel/importer/timefix.go', 'kernel/importer/timefix_test.go'], a=[3], d=[1]),
    C('cmd/counsel-import CLI', ['cmd/counsel-import/main.go', 'kernel/importer/cli.go', 'kernel/importer/cli_test.go'], d=[3],
      x=['counsel-import dry-run|run|resume parse flags only and call kernel/importer; run without an approved dry run exits non-zero']),
]
S['T-043'] = [
    C('Helm chart per cell', ['deploy/helm/counsel/Chart.yaml', 'deploy/helm/counsel/values.yaml', 'deploy/helm/counsel/templates/app.yaml', 'tools/helm_render_test.go'], a=[0]),
    C('CloudNativePG backups and restore drill', ['deploy/cnpg/', 'docs/runbooks/restore.md'], a=[1]),
    C('DR bucket month-end export ordering', ['kernel/backup/export.go', 'kernel/backup/export_test.go'], a=[2]),
    C('Migration job and expand-contract lint', ['deploy/helm/counsel/templates/migrate-job.yaml', 'tools/migration_lint.py', 'tools/migration_lint_test.go'], a=[3]),
    C('Pooled connection session reset', ['kernel/db/pool.go', 'kernel/db/pool_integration_test.go'], a=[4]),
    C('Control plane config and WAF', ['deploy/helm/counsel/templates/waf.yaml', 'deploy/controlplane/', 'tools/controlplane_test.go'], a=[5]),
    C('Rollout rings', ['deploy/rings.yaml', 'docs/runbooks/rollout.md', 'tools/rings_test.go'], d=[1],
      x=['rings.yaml defines canary, early, general rings; a release cannot reach general before canary has run 24 h (lint test)']),
]
S['T-044'] = [
    C('SLO rules, alerts and synthetic check', ['deploy/observability/', 'kernel/metrics/synthetic.go', 'kernel/metrics/synthetic_test.go'], a=[0]),
    C('Degraded-mode smoke checks', ['kernel/metrics/smoke.go', 'kernel/metrics/smoke_test.go'], a=[1]),
    C('kernel/flags: owner and expiry validation', ['kernel/flags/config.go', 'kernel/flags/config_test.go'], a=[2]),
    C('Dead letter paging and replay', ['kernel/outbox/deadletter.go', 'kernel/outbox/deadletter_test.go'],
      x=['Dead letter older than 1 business day pages; replay is an explicit admin action and idempotent (ARC-U-024)']),
    C('Purge guard for never_purge classes', ['kernel/retention/guard.go', 'kernel/retention/guard_test.go'], a=[3], ext=['T-032']),
    C('Status page', ['kernel/metrics/status.go', 'kernel/metrics/status_test.go'], d=[2], x=['Status endpoint reports each smoke check; a failing check shows degraded, never leaks customer data']),
]
S['T-045'] = [
    C('web generated API client', ['web/src/kernel/api/client.ts', 'web/src/kernel/api/client.test.ts', 'tools/gen_client.mjs'], a=[0]),
    C('web money formatter and float lint', ['web/src/kernel/money.ts', 'web/src/kernel/money.test.ts', 'web/eslint.config.js'], a=[1]),
    C('web shell navigation from feature list', ['web/src/kernel/shell/Shell.tsx', 'web/src/kernel/shell/nav.ts', 'web/src/kernel/shell/Shell.test.tsx'], a=[2], d=[1]),
]
L = 'app/legal'
S['T-006L'] = [
    C('Legal roles and capabilities from access.json', [f'{L}/roles/roles.go', f'{L}/roles/access_matrix_test.go'], a=[0]),
    C('Agent limits and module naming', [f'{L}/roles/agent.go', f'{L}/roles/agent_test.go'], a=[1], d=[1]),
    C('Walls SubjectResolver', [f'{L}/roles/walls.go', f'{L}/roles/walls_test.go'], a=[2], d=[1]),
]
S['T-007L'] = [
    C('Legal gate kinds from gates.json', [f'{L}/gates/kinds.go', f'{L}/gates/kinds_test.go'], a=[0]),
    C('Sole-practitioner self-approval policy', [f'{L}/gates/selfapproval.go', f'{L}/gates/selfapproval_test.go'], a=[1], d=[1]),
    C('Trust cooling-off periods', [f'{L}/gates/coolingoff.go', f'{L}/gates/coolingoff_test.go'], a=[2], d=[1]),
    C('Human non-preparer decision check', [f'{L}/gates/decision.go', f'{L}/gates/decision_test.go'], a=[3], d=[1]),
]
S['T-010L'] = [
    C('Operating and trust chart of accounts', [f'{L}/books/chart.go', f'{L}/books/chart_test.go'], a=[0]),
    C('AR and client credit invariants', [f'{L}/books/invariants.go', f'{L}/books/invariants_test.go'], a=[1], d=[1]),
    C('Vendor bills in gl', [f'{L}/books/vendorbill.go', f'{L}/books/vendorbill_test.go'], a=[2], d=[1]),
]
S['T-010b'] = [
    C('Matter core create API', [f'{L}/matter/core.go', f'{L}/matter/core_test.go'], a=[0]),
    C('Minimal party contact', [f'{L}/party/contact.go', f'{L}/party/contact_test.go'], x=['party.contact rows can be created and read through the module API (no conflicts UI)']),
    C('Matter query API and api_matter view', [f'{L}/matter/query.go', f'{L}/matter/query_test.go'], a=[1], d=[1]),
]
S['T-028'] = [
    C('Merged contact mapping', [f'{L}/party/merge.go', f'{L}/party/merge_test.go'], a=[0], ext=['T-010b']),
    C('Party fields and relationships', [f'{L}/party/relationships.go', f'{L}/party/fields_test.go'], a=[1], ext=['T-010b']),
]
TR = f'{L}/trust'
S['T-011'] = [
    C('Trust accounts and signatories', [f'{TR}/account.go', f'{TR}/account_test.go'], a=[4]),
    C('Trust receipts and client ledgers', [f'{TR}/receipt.go', f'{TR}/receipt_integration_test.go'], a=[0], d=[1]),
    C('Cash receipt limit', [f'{TR}/cashlimit.go', f'{TR}/cashlimit_test.go'], a=[1], d=[2]),
    C('NSF reversal and replenishment', [f'{TR}/nsf.go', f'{TR}/nsf_test.go'], a=[2], d=[2]),
    C('Ledger lock under concurrency', [f'{TR}/ledgerlock.go', f'{TR}/ledgerlock_integration_test.go'], a=[3], d=[2]),
]
W = f'{TR}/withdraw'
S['T-012'] = [
    C('Withdrawal request validation', [f'{W}/request.go', f'{W}/request_test.go'], a=[0, 4]),
    C('Withdrawal gate effect with balance re-check', [f'{W}/effect.go', f'{W}/effect_integration_test.go'], a=[1, 2], d=[1]),
    C('Execution recording and bank match', [f'{W}/execute.go', f'{W}/execute_test.go'], a=[3, 6], d=[2]),
    C('Withdrawal effect replay once', [f'{W}/replay_integration_test.go'], a=[5], d=[2]),
    C('Transfers and EFT authorization', [f'{W}/transfer.go', f'{W}/eft.go', f'{W}/eft_test.go'], d=[2],
      x=['EFT needs an authorization record (Form 9A) before execution; client-to-client transfer posts two balanced trust lines']),
]
RC = f'{TR}/recon'
S['T-013'] = [
    C('Bank statement import', [f'{RC}/bankimport.go', f'{RC}/bankimport_test.go'], x=['Statement import dedups lines by (account, bank line id); re-import adds zero rows']),
    C('Three-way reconciliation', [f'{RC}/threeway.go', f'{RC}/threeway_test.go'], a=[0], d=[1]),
    C('Reconciliation sign-off locks month', [f'{RC}/signoff.go', f'{RC}/signoff_integration_test.go'], a=[1], d=[2]),
    C('Month-end immutable export', [f'{RC}/export.go', f'{RC}/export_test.go'], a=[2], d=[3]),
]
TX = f'{L}/billing/tax'
S['T-014'] = [
    C('Tax rates seed and lookup', [f'{TX}/rates.go', f'{TX}/rates_test.go'], a=[0], flag=None),
    C('Place-of-supply decision table', [f'{TX}/pos.go', f'{TX}/decision_table_test.go'], a=[1], d=[1], flag=None),
    C('Tax computation, no tax on tax', [f'{TX}/compute.go', f'{TX}/compute_test.go'], a=[2], d=[1], flag=None),
    C('Business number check digit', [f'{TX}/bn.go', f'{TX}/bn_test.go'], a=[3], flag=None),
    C('Verify-flagged tax rules', [f'{TX}/verify_rules.go', f'{TX}/verify_rules_test.go'], a=[4], d=[2]),
]
RT = f'{L}/timekeeping/rates'
S['T-015'] = [
    C('Rate resolution chain', [f'{RT}/resolve.go', f'{RT}/resolve_test.go'], a=[0]),
    C('Effective-dated re-pricing', [f'{RT}/reprice.go', f'{RT}/reprice_test.go'], a=[1], d=[1]),
    C('Time rounding', [f'{RT}/rounding.go', f'{RT}/rounding_test.go'], a=[2]),
    C('Matter move rate recheck', [f'{RT}/move.go', f'{RT}/move_test.go'], a=[3], d=[1]),
]
TK = f'{L}/timekeeping'
S['T-016'] = [
    C('Time entry state machine and suggestions', [f'{TK}/entry.go', f'{TK}/state_test.go'], a=[0]),
    C('Timers', [f'{TK}/timer.go', f'{TK}/timer_test.go'], d=[1], x=['Timer start/stop yields raw minutes; at most one running timer per user']),
    C('Write-downs and billed-entry lock', [f'{TK}/writedown.go', f'{TK}/writedown_test.go'], a=[1, 2], d=[1]),
    C('Paralegal scope check', [f'{TK}/paralegal.go', f'{TK}/paralegal_test.go'], a=[3], d=[1]),
]
DR = f'{L}/billing/draft'
S['T-017'] = [
    C('Bill draft lines and b3 golden examples', [f'{DR}/lines.go', f'{DR}/examples_test.go'], a=[0]),
    C('Bill discounts', [f'{DR}/discount.go', f'{DR}/discount_test.go'], a=[1, 2], d=[1]),
    C('Disbursements and markup', [f'{DR}/disbursement.go', f'{DR}/disbursement_test.go'], a=[3], d=[1]),
    C('Itemized disbursement delivery block', [f'{DR}/itemize.go', f'{DR}/itemize_test.go'], a=[4], d=[3]),
    C('Line reductions', [f'{DR}/reduction.go', f'{DR}/reduction_test.go'], d=[1], x=['Reduction lowers line amount only, never the source time entry; reduction below zero -> 422']),
]
TT = f'{L}/billing/total'
S['T-018'] = [
    C('Tax per tax line', [f'{TT}/tax.go', f'{TT}/tax_test.go'], a=[0]),
    C('Bill totals and server recompute', [f'{TT}/total.go', f'{TT}/total_test.go'], a=[1, 2], d=[1]),
    C('Tax point', [f'{TT}/taxpoint.go', f'{TT}/taxpoint_test.go'], a=[3]),
]
BB = f'{L}/billing/bill'
S['T-019'] = [
    C('Bill approval effect and numbering', [f'{BB}/approve.go', f'{BB}/approve_integration_test.go'], a=[0], flag=None),
    C('Approved bill guard', [f'{BB}/guard.go', f'{BB}/guard_integration_test.go'], a=[1], d=[1], flag=None),
    C('Bill signature and delivery', [f'{BB}/deliver.go', f'{BB}/deliver_test.go'], a=[2, 5], d=[1], flag=None),
    C('Delivered bill content', [f'{BB}/pdf.go', f'{BB}/pdf_test.go'], a=[3], d=[1]),
    C('Bill approval journal', [f'{BB}/journal.go', f'{BB}/journal_integration_test.go'], a=[4], d=[1], flag=None),
    C('Paralegal bills and fee basis', [f'{BB}/paralegal.go', f'{BB}/paralegal_test.go'], a=[6], d=[1]),
]
S['T-020'] = [
    C('Bill void', [f'{BB}/void.go', f'{BB}/void_integration_test.go'], a=[0, 1]),
    C('Place-of-supply change after delivery', [f'{BB}/pos_change.go', f'{BB}/pos_change_test.go'], a=[2]),
]
PY = f'{L}/billing/payment'
S['T-021'] = [
    C('Payment capture and allocation', [f'{PY}/allocate.go', f'{PY}/allocate_test.go'], a=[0, 7]),
    C('Payment provider webhook dedup', [f'{PY}/provider.go', f'{PY}/provider_test.go'], a=[1], d=[1]),
    C('Overpayment and client credit', [f'{PY}/overpay.go', f'{PY}/overpay_test.go'], a=[2, 3], d=[1]),
    C('Operating receipts journal', [f'{PY}/receipts.go', f'{PY}/receipts_test.go'], a=[4], d=[1]),
    C('NSF reversal of payments', [f'{PY}/nsf.go', f'{PY}/nsf_test.go'], a=[5], d=[1]),
    C('Refunds, disputes and chargebacks', [f'{PY}/dispute.go', f'{PY}/dispute_test.go'], a=[6], d=[1]),
]
TA = f'{L}/billing/trustapply'
S['T-022'] = [
    C('Apply trust to bill', [f'{TA}/apply.go', f'{TA}/apply_test.go'], a=[0, 1]),
    C('Disbursement paid from trust', [f'{TA}/disbursement.go', f'{TA}/disbursement_test.go'], a=[2], d=[1]),
    C('Auto-apply proposals', [f'{TA}/auto.go', f'{TA}/auto_test.go'], a=[3], d=[1]),
    C('Retainer tax timing', [f'{TA}/retainer.go', f'{TA}/retainer_test.go'], a=[4], d=[1]),
]
CR = f'{L}/billing/credit'
S['T-023'] = [
    C('Credit notes with source tax proration', [f'{CR}/credit.go', f'{CR}/credit_test.go'], a=[0, 1]),
    C('Write-offs and bad-debt adjustment', [f'{CR}/writeoff.go', f'{CR}/writeoff_test.go'], a=[2], d=[1]),
]
IN = f'{L}/billing/interest'
S['T-024'] = [
    C('Interest accrual start', [f'{IN}/start.go', f'{IN}/start_test.go'], a=[0], d=[3]),
    C('Simple interest calculation', [f'{IN}/calc.go', f'{IN}/calc_test.go'], a=[1], flag=None),
    C('months_after calendar months', [f'{IN}/months.go', f'{IN}/months_test.go'], a=[2]),
    C('Interest posting job', [f'{IN}/post.go', f'{IN}/post_test.go'], a=[3, 4], d=[1, 2], flag=None),
]
AS = f'{L}/billing/assessment'
S['T-024b'] = [
    C('Collection hold', [f'{AS}/collection.go', f'{AS}/collection_test.go'], a=[0]),
    C('Assessment requisition', [f'{AS}/requisition.go', f'{AS}/requisition_test.go'], a=[1]),
    C('Assessment result repayment', [f'{AS}/result.go', f'{AS}/result_test.go'], a=[2], d=[2]),
]
S['T-025'] = [
    C('Split bills', [f'{L}/billing/split/split.go', f'{L}/billing/split/split_test.go'], a=[0]),
    C('Batch billing', [f'{L}/billing/batch/batch.go', f'{L}/billing/batch/batch_test.go'], a=[1]),
    C('Combined client bills', [f'{L}/billing/batch/combined.go', f'{L}/billing/batch/combined_test.go'], d=[2], x=['Combined bill for one client over 3 matters keeps per-matter subtotals and tax lines; rerun creates no duplicate']),
    C('Unbilled WIP holding flag', [f'{L}/billing/batch/wipflag.go', f'{L}/billing/batch/wipflag_test.go'], a=[2]),
]
SU = f'{L}/billing/surcharge'
S['T-026'] = [
    C('Card surcharge rules and cap', [f'{SU}/surcharge.go', f'{SU}/surcharge_test.go'], a=[0, 1]),
    C('Surcharge tax and refunds', [f'{SU}/tax.go', f'{SU}/tax_test.go'], a=[2], d=[1]),
]
CT = f'{L}/billing/contingency'
S['T-027'] = [
    C('Contingency agreements and fee', [f'{CT}/agreement.go', f'{CT}/agreement_test.go'], a=[0, 1]),
    C('Settlement statement', [f'{CT}/settlement.go', f'{CT}/settlement_test.go'], a=[2], d=[1]),
    C('Fee agreement templates', [f'{CT}/template.go', f'{CT}/template_test.go'], a=[3]),
    C('Fee splitting and referral fees', [f'{L}/billing/feeshare/feeshare.go', f'{L}/billing/feeshare/feeshare_test.go'], a=[4]),
]
CF = f'{L}/conflict'
S['T-029'] = [
    C('Conflict check runs and hits', [f'{CF}/check.go', f'{CF}/check_test.go'], a=[0], flag=None),
    C('G2 clearance', [f'{CF}/clearance.go', f'{CF}/clearance_test.go'], d=[1], flag=None, x=['Clearance only through G2 decided by a lawyer who is not the preparer; check run rows stay append-only']),
    C('Ethical walls', [f'{CF}/walls.go', f'{CF}/walls_test.go'], a=[1], d=[1], flag=None),
    C('Client ID verification', [f'{CF}/idverify.go', f'{CF}/idverify_test.go'], a=[2]),
]
MT = f'{L}/matter'
S['T-030'] = [
    C('Matter lifecycle', [f'{MT}/lifecycle.go', f'{MT}/lifecycle_test.go'], a=[1]),
    C('Matter team and stages', [f'{MT}/team.go', f'{MT}/stages.go', f'{MT}/stages_test.go'], d=[1], x=['Stage moves follow the stage list; removing the last responsible lawyer -> 409']),
    C('Matter notes and facts', [f'{MT}/notes.go', f'{MT}/notes_test.go'], d=[1], x=['Notes and facts are append-only versions; note text never in logs or audit detail']),
    C('Matter RLS policies', [f'{MT}/rls.go', f'{MT}/rls_integration_test.go'], a=[0], d=[1]),
]
RP = f'{L}/rulepacks/ontario'
S['T-031L'] = [
    C('Ontario holiday calendar pack data', [f'{RP}/holidays.yaml', f'{RP}/holidays_test.go'], x=['Ontario 2026 holidays load as rule-pack data; a date lookup for 2026-12-28 is a holiday']),
    C('Rules of Civil Procedure rule pack', [f'{RP}/civil-procedure.yaml', f'{RP}/golden_test.go'], a=[0], d=[1]),
    C('Limitation assessment', [f'{L}/limitation/assess.go', f'{L}/limitation/assess_test.go'], a=[1], d=[2]),
]
LD = f'{L}/docs'
S['T-032L'] = [
    C('Legal document kinds and retention classes', [f'{LD}/kinds.go', f'{LD}/kinds_test.go'], a=[0]),
    C('Matter folders', [f'{LD}/folders.go', f'{LD}/folders_test.go'], d=[1], x=['New matter gets the default folder tree; a document lives in exactly one folder']),
    C('File destruction proposals via G14', [f'{LD}/destruction.go', f'{LD}/destruction_test.go'], a=[1], d=[1]),
]
IK = f'{L}/intake'
S['T-033'] = [
    C('Leads and intake forms', [f'{IK}/lead.go', f'{IK}/form.go', f'{IK}/lead_test.go'], x=['Published form creates a lead with its party.contact; duplicate submission (same form, idempotency key) creates one lead']),
    C('Consultations', [f'{IK}/consult.go', f'{IK}/consult_test.go'], d=[1], x=['Consultation books against a lead; outcome is required before conversion']),
    C('Lead to matter conversion', [f'{IK}/convert.go', f'{IK}/convert_test.go'], a=[0], d=[1]),
]
CM = f'{L}/comms'
S['T-035L'] = [
    C('Client portal page loaders', [f'{CM}/portal.go', f'{CM}/portal_test.go'], a=[0]),
    C('Court filings and message templates', [f'{CM}/filings.go', f'{CM}/templates.go', f'{CM}/filings_test.go'], a=[1]),
    C('Portal web feature', ['web/src/features/portal/feature.ts', 'web/src/features/portal/PortalMatters.tsx', 'web/src/features/portal/PortalMatters.test.tsx'], d=[1],
      x=['Vitest: guest sees own shared documents and bills only; no import from another feature folder']),
]
AG = f'{L}/agent'
S['T-037L'] = [
    C('Counsel read tools', [f'{AG}/tools_read.go', f'{AG}/tools_read_test.go'], x=['Read tools respect walls: search_matters omits walled matters; every result quoting a document carries a citation']),
    C('Counsel write tools propose only', [f'{AG}/tools_write.go', f'{AG}/tools_write_test.go'], a=[2, 3]),
    C('Paploo plugin manifest', ['paploo/counsel-plugin/paploo.yaml', 'paploo/counsel-plugin/manifest_test.go'], a=[1]),
    C('Tool schema validation for all 25 tools', [f'{AG}/schemas/', f'{AG}/schemas_test.go'], a=[0], d=[1, 2]),
]
RE = f'{L}/reports'
S['T-038L'] = [
    C('HST report', [f'{RE}/hst.go', f'{RE}/hst_test.go'], a=[0], flag=None),
    C('Monthly client trust listing', [f'{RE}/trustlisting.go', f'{RE}/trustlisting_test.go'], a=[1]),
    C('AR aging and WIP reports', [f'{RE}/aging.go', f'{RE}/wip.go', f'{RE}/aging_test.go'], flag=None, x=['AR aging buckets 0-30/31-60/61-90/90+ sum to OP-1100; WIP equals approved unbilled time at resolved rates']),
]
S['T-039L'] = [C('Trust signatory change gate', [f'{TR}/signatory/change.go', f'{TR}/signatory/change_test.go'], a=[0])]
CL = f'{L}/clio'
S['T-040L'] = [
    C('Practice-management REST fake server', ['test/fakes/practiceapi/server.go', 'test/fakes/practiceapi/server_test.go'], x=['Fake serves paged v4-shaped resources from testdata and records every request method; no network beyond localhost']),
    C('Source client, region and token', [f'{CL}/client.go', f'{CL}/client_test.go'], a=[0], d=[1]),
    C('Rate limit and decimal money', [f'{CL}/ratelimit.go', f'{CL}/decimal.go', f'{CL}/ratelimit_test.go'], a=[1], d=[2]),
    C('Resource extract and transform', [f'{CL}/transform.go', f'{CL}/transform_test.go'], d=[2], x=['Contacts, matters, time and bills transform into module APIs; rerun on the same snapshot -> zero changes']),
    C('Document version import', [f'{CL}/documents.go', f'{CL}/documents_test.go'], a=[2], d=[2]),
    C('Source dry-run report', [f'{CL}/dryrun.go', f'{CL}/dryrun_test.go'], a=[3], d=[4]),
]
SY = f'{CL}/sync'
S['T-040b'] = [
    C('Alongside mode read-only', [f'{SY}/alongside.go', f'{SY}/alongside_test.go'], a=[0]),
    C('Source subscription renewal job', [f'{SY}/renew.go', f'{SY}/renew_test.go'], a=[1]),
    C('Outbound endpoint expiry events', ['kernel/webhook/expiry.go', 'kernel/webhook/expiry_test.go'], a=[2]),
    C('Polling fallback', [f'{SY}/poll.go', f'{SY}/poll_test.go'], a=[3], d=[1]),
]
S['T-041'] = [
    C('Cut-over validation checks', [f'{CL}/validate.go', f'{CL}/validate_test.go'], a=[0], d=[2, 3, 4, 5]),
    C('Imported bills keep source tax', [f'{CL}/bills_import.go', f'{CL}/bills_import_test.go'], a=[1]),
    C('Opening balances after G-IMP', [f'{CL}/opening.go', f'{CL}/opening_test.go'], a=[2]),
    C('Historical trust lines', [f'{CL}/trust_history.go', f'{CL}/trust_history_test.go'], a=[3]),
    C('Imported reconciliations', [f'{CL}/recon_import.go', f'{CL}/recon_import_test.go'], a=[4]),
]
EX = f'{L}/export'
S['T-042'] = [
    C('Firm export layout and manifest', [f'{EX}/export.go', f'{EX}/manifest.go', f'{EX}/export_test.go'], a=[0]),
    C('Export trust balances reconcile', [f'{EX}/trust.go', f'{EX}/trust_test.go'], a=[1], d=[1]),
]
AREAS = ['matters', 'conflicts', 'intake', 'time', 'billing', 'trust', 'documents', 'deadlines', 'work', 'reports', 'access', 'settings']
S['T-045L'] = [C(f'Legal screens: {ar}', [f'web/src/features/{ar}/'], x=[f'Every {ar} screen in legal-spec has a route guarded by its access-map capability (Vitest)', f'web/src/features/{ar} registers through feature.ts and imports no other feature folder']) for ar in AREAS] + \
    [C('Screen coverage check across features', ['web/src/app/screens.test.ts'], a=[0, 1], d=list(range(1, len(AREAS) + 1)))]

# ---- build ----
# Dependency model (contract-first):
#  * wave 0 freezes every contract: go.mod/CI, OpenAPI, DDL, spec/kernel schemas, the Registry
#    and its extension points, ModelClient + aifake, the AI fixture format, the mock LLM server
#    core, and one "contract" task per Go package that another o1 task depends on. A contract
#    task writes the package's exported interface (contract.go) and an in-memory fake
#    (<pkg>fake/) so dependants build and test against interface + fake.
#  * a dependency on another o1 task (T-id) resolves to the contract tasks of the packages
#    that o1 task owns, never to all of its subtasks. Only o1 tasks that publish no Go
#    package (spec, web, yaml) resolve to their concrete subtasks.
#  * dependencies inside one o1 task stay as declared (`d`), so each subtask waits only for
#    the sibling whose code it extends.
assert set(S) == set(OT), set(OT) ^ set(S)
W0_PARENTS = {pid for pid, kids in S.items() if all(k['o'].get('w0') for k in kids)}
children = {pid: [f'{pid}.{i}' for i in range(1, len(kids) + 1)] for pid, kids in S.items()}


def gopkgs(files):
    return sorted({os.path.dirname(f) for f in files if f.endswith('.go') and not f.endswith('_test.go')})


# owner of each Go package = first o1 task (by id) that writes a non-test .go file in it
owner = {}
w0_dirs = {d for pid in W0_PARENTS for k in S[pid] for d in gopkgs(k['files'])} | {'kernel/ai'}
for pid in sorted(S):
    for k in S[pid]:
        for d in gopkgs(k['files']):
            if d not in w0_dirs:
                owner.setdefault(d, pid)
pkgs_of = {pid: sorted({d for k in S[pid] for d in gopkgs(k['files']) if d in owner}) for pid in S}

# which o1 tasks are depended upon across parents (o1 deps, ext, AI deps)
needed = set()
for pid, kids in S.items():
    needed.update(OT[pid]['depends_on'])
    for k in kids:
        needed.update(k['o'].get('ext', []))
for a in A['tasks']:
    needed.update(d for d in a['depends_on'] + a['blocked_by'] if d.startswith('T-'))
needed -= W0_PARENTS
contract_dirs = sorted({d for p in needed for d in pkgs_of[p]})
contract_id = {}
contract_tasks = []
by_owner = {}
for d in contract_dirs:
    by_owner.setdefault(owner[d], []).append(d)
for pid in sorted(by_owner):
    dirs = by_owner[pid]
    tid = f'{pid}.0'
    files = []
    for d in dirs:
        base = os.path.basename(d)
        files += [f'{d}/contract.go', f'{d}/{base}fake/fake.go', f'{d}/{base}fake/fake_test.go']
        contract_id[d] = tid
    contract_tasks.append(dict(
        id=tid, wave=None, parent=pid, layer=OT[pid]['layer'],
        title=f'Contract and in-memory fake: {", ".join(dirs)}', depends_on=[], files=files, rule_ids=[], size='S',
        accept=[f'{d}/contract.go declares, with a CONTRACT header, every exported type, interface and sentinel error that '
                f'{pid} subtasks implement and other tasks call; bodies live in the {pid} subtasks' for d in dirs] +
               [f'{d}/{os.path.basename(d)}fake is an in-memory implementation of the {d} interfaces, table-driven tested, '
                'with no network, no Postgres and no Docker' for d in dirs] +
               ['A compile-time assertion (var _ Iface = (*Fake)(nil)) pins the fake to the contract; changes after wave 0 are additive only']))


def resolve(dep):
    """Precise tasks a dependant needs from o1 task or AI task `dep`."""
    if dep in W0_PARENTS:
        return children[dep]
    if dep in S:
        if pkgs_of[dep]:
            return sorted({contract_id[d] for d in pkgs_of[dep]})
        return children[dep]
    return [dep]


tasks = list(contract_tasks)
for pid, kids in S.items():
    p = OT[pid]
    vflags = [b for b in p['blocked_by'] if b.startswith('V-')]
    pflag = FLAGS[vflags[0]] if vflags else None
    built = []
    for i, k in enumerate(kids, 1):
        acc = [p['accept'][j] for j in k['a']] + k['x']
        rules = [r for r in p['rule_ids'] if any(r in s for s in acc + [k['title']])]
        built.append((i, k, acc, rules))
    used = {r for _, _, _, rs in built for r in rs}
    left = [r for r in p['rule_ids'] if r not in used]
    for idx, (i, k, acc, rules) in enumerate(built):
        if idx == 0 and left:
            rules = rules + left
            acc = acc + [f'Each of {", ".join(left)} has a // rule: tag in this task\'s code and a test named after it']
        if not acc:
            acc = [f'Tests cover: {k["title"]}']
        tid = f'{pid}.{i}'
        deps = []
        if not k['o'].get('w0'):
            for d in p['depends_on'] + k['o'].get('ext', []):
                deps += resolve(d)
            deps += [f'{pid}.{j}' for j in k['d']]
            deps += [contract_id[d] for d in gopkgs(k['files']) if d in contract_id]
        t = dict(id=tid, wave=None, parent=pid, layer=p['layer'], title=k['title'], depends_on=sorted(set(deps) - {tid}),
                 files=k['files'], rule_ids=rules, accept=acc, size='S')
        fl = k['o'].get('flag', pflag) if 'flag' in k['o'] else pflag
        if fl:
            t['flag'] = fl
        tasks.append(t)

# wave-0 extension-point stubs derived from all app/kernel packages
app_pk, ker_pk = set(), set()
for t in tasks:
    for f in t['files']:
        if f.endswith('.go') and f.startswith('app/legal/') and 'fake/' not in f:
            app_pk.add(os.path.dirname(f))
        if f.endswith('.go') and f.startswith('kernel/') and f.count('/') >= 2 and 'fake/' not in f:
            ker_pk.add(os.path.dirname(f))
app_pk.add('app/legal/rulepacks/ontario')
app_pk.discard('app/legal')
ker_pk -= {'kernel/registry', 'kernel/contract'}
tasks.append(dict(id='T-001.5', wave=None, parent='T-001', layer='app', title='app/legal registration extension points (per-package register.go)',
                  depends_on=[], files=['app/legal/register.go', 'app/legal/modules.go', 'app/legal/flags.go', 'app/legal/register_test.go'] + sorted(f'{d}/register.go' for d in app_pk),
                  rule_ids=[], size='S', accept=[
                      'app/legal/register.go is func Register(r registry.Registry) error and calls every package in modules.go; each package register.go keeps a registrars list that feature files append to from init(), so feature tasks add files instead of editing a shared one',
                      'app/legal/flags.go defines every verify flag (' + ', '.join(sorted(FLAGS.values())) + ') with owner and expiry, all off by default; a test asserts each is off',
                      'Register against registrytest fake succeeds with zero registrations']))
tasks.append(dict(id='T-001.6', wave=None, parent='T-001', layer='kernel', title='kernel boot, flags and package stubs',
                  depends_on=[], files=['kernel/boot/boot.go', 'kernel/boot/boot_test.go', 'kernel/all/all.go', 'kernel/flags/flags.go', 'kernel/flags/flags_test.go'] + sorted(f'{d}/doc.go' for d in ker_pk),
                  rule_ids=[], size='S', accept=[
                      'kernel/boot builds the kernel and calls app Register; kernel/all blank-imports every kernel package so modules self-register routes via kernel/server Mount from their own files',
                      'kernel/flags.Enabled(ctx, name) is false for any flag not explicitly turned on; flags are defined by the app at boot',
                      'Every kernel package has a doc.go stub so wave tasks only add files']))
children['T-001'] = children['T-001'] + ['T-001.5', 'T-001.6']

# ---- AI tasks ----
# AI-001 (ModelClient), AI-002 (aifake), AI-003 (fixture format) and AI-004 (mock LLM server)
# are contracts: wave 0. AI-004 reads fixtures through the format AI-003 freezes in
# spec/ai/mocks.md, so the two are built side by side.
AI_W0 = {'AI-001', 'AI-002', 'AI-003', 'AI-004'}
rm_files = {'test/e2e/run.sh'}
for a in A['tasks']:
    deps = []
    if a['id'] not in AI_W0:
        for d in a['depends_on'] + [b for b in a['blocked_by'] if b.startswith('T-')]:
            deps += resolve(d)
    m = re.search(r'F-[A-Z]+-\d+', a['title'])
    acc = list(a['accept'])
    if a['id'] == 'AI-002':
        acc.append('Implements the ModelClient interface AI-001 freezes in the same wave; a compile-time assertion pins aifake to it')
    if a['id'] == 'AI-004':
        acc.append('Serves replies from fixtures in the format of spec/ai/mocks.md (frozen by AI-003 in the same wave); a format test loads testdata/ai/README.md examples')
    t = dict(id=a['id'], wave=None, parent=a['id'], layer=a['layer'], title=a['title'], depends_on=sorted(set(deps)),
             files=[f for f in a['files'] if f not in rm_files], rule_ids=a['rule_ids'], accept=acc, size='S')
    if a['size'] != 'S':
        t['accept'] = t['accept'] + ['Fits one agent session: if it does not, stop and split in docs/QUESTIONS.md']
    if m:
        t['ai_feature'] = m.group(0)
    tasks.append(t)

# ---- waves ----
byid = {t['id']: t for t in tasks}
for t in tasks:
    for d in t['depends_on']:
        assert d in byid, (t['id'], d)
memo = {}


def wave(i, stack=()):
    if i in memo:
        return memo[i]
    if i in stack:
        raise SystemExit('cycle ' + ' -> '.join(stack + (i,)))
    t = byid[i]
    w = 0 if not t['depends_on'] else 1 + max(wave(d, stack + (i,)) for d in t['depends_on'])
    memo[i] = w
    return w


for t in tasks:
    t['wave'] = wave(t['id'])
tasks.sort(key=lambda t: (t['wave'], t['id']))
key = ['id', 'wave', 'parent', 'layer', 'title', 'depends_on', 'files', 'rule_ids', 'accept', 'size', 'flag', 'ai_feature']
tasks = [{k: t[k] for k in key if k in t} for t in tasks]
waves = Counter(t['wave'] for t in tasks)


def critical_path():
    byid2 = {t['id']: t for t in tasks}
    end = max(tasks, key=lambda t: (t['wave'], t['id']))
    path = [end['id']]
    while byid2[path[-1]]['depends_on']:
        cur = byid2[path[-1]]
        path.append(max(cur['depends_on'], key=lambda d: (byid2[d]['wave'], d)))
    return path[::-1]


out = dict(id='tasks', summary='Parallel build plan: every o1 task split into S tasks plus the AI tasks; wave 0 freezes '
           'contracts and fakes, later tasks depend only on the contract or sibling subtask they use; waves computed from depends_on. '
           'Generated by tools/gen_tasks.py; do not edit by hand.',
           sources=['spec/eng/o1-tasks.json', 'spec/ai/tasks-ai.json', 'spec/blockers.json'],
           flags=dict(FLAGS),
           counts=dict(total=len(tasks), per_wave={str(k): waves[k] for k in sorted(waves)}, max_width=max(waves.values()),
                       critical_path=critical_path()),
           tasks=tasks)
with open(R + '/spec/tasks.json', 'w') as fh:
    json.dump(out, fh, indent=1)
    fh.write('\n')


# ---- docs/TASKS.md (a view of spec/tasks.json) ----
c = out['counts']
L = ['# Tasks\n',
     'Generated by `tools/gen_tasks.py` from `spec/eng/o1-tasks.json`, `spec/ai/tasks-ai.json` and `spec/blockers.json`. '
     '`spec/tasks.json` is the source of truth; this page is a view. Regenerate with `python3 -I tools/gen_tasks.py`, '
     'validate with `python3 -I tools/check_tasks.py`.\n',
     f"**{c['total']} tasks, {len(c['per_wave'])} waves, max parallel width {c['max_width']}, critical path {len(c['critical_path'])} tasks.** "
     'Every task is size S (one agent, about 25 minutes). `parent` traces each task to its o1 task (T-id) or AI task (AI-id); '
     '`rule_ids` and `accept` are the tests to write first.\n',
     '## Contract first\n',
     'Wave 0 freezes every contract so the rest of the plan builds against interfaces and fakes, not against finished code:\n',
     '- `go.mod`, `Makefile`, CI, e2e runner, `web/` scaffold, boot and per-package extension points (T-001.*).',
     '- `api/openapi.yaml`, `migrations/0001_init.sql`, `spec/kernel/*.schema.json`, the `Registry` interface, `spec/rules`, gates and access (T-002.*, T-002L.*).',
     '- `ModelClient` (AI-001), its in-process fake `aifake` (AI-002), the AI fixture format (AI-003) and the mock LLM server core (AI-004).',
     '- One `<id>.0` task per Go package that another task uses: `contract.go` (CONTRACT header: exported types, interfaces, sentinel errors) and `<pkg>fake/`, an in-memory fake.\n',
     'A task that uses another o1 task depends on that task\'s `.0` contract, not on all its subtasks. Inside one o1 task, a subtask '
     'depends only on the sibling whose code it extends. Tasks that publish no Go package (spec, web, YAML) are depended on directly.\n',
     '## How to work a task\n',
     '1. **Claim**: one agent per task. Take a task from the lowest open wave whose `depends_on` are all merged. Mark it claimed in the wave tracking issue before starting.',
     '2. **Branch**: `task/<id>` from the current wave branch `wave/<n>` (wave 0 branches from `main`).',
     '3. **Own only your files.** The `files` list is exclusive within a wave: no other task in the same wave touches them. Need another file? Stop and write a `docs/QUESTIONS.md` entry.',
     '4. **Build against the contract.** Implement the interface in your package\'s `contract.go`; test callers of other packages with their `<pkg>fake`. Never edit a `contract.go` after wave 0 except additively, with a `docs/QUESTIONS.md` entry.',
     '5. **Extension points, not hot files.** Routes, gate kinds, tools and the rest register from your own file via the package `register.go` registrar list (T-001.5/T-001.6); migrations go in your module block (`migrations/README.md`); web features are discovered from `web/src/features/*/feature.ts`; e2e suites are discovered by `test/e2e/run.sh`. Never edit a shared registry file.',
     '6. **Flags**: a task with `flag` codes a `(verify)` rule behind that flag, off by default (`app/legal/flags.go`). Its tests run with the flag on and assert it is off by default. It is not blocked.',
     '7. **Done**: `make check` green; one commit `feat: <id> <subject>`; open a PR into `wave/<n>`; tick the box below.\n',
     '## How the integrator merges a wave\n',
     '1. Merge task PRs into `wave/<n>` in any order: files are disjoint inside a wave. Wave 0 merges T-001.* first, then T-002.*, then the rest in id order.',
     '2. On `wave/<n>` run `make check` (gofmt, go vet, go test, spec lint, rule coverage, `tools/check_tasks.py`). Fix forward on the wave branch, never in `main`.',
     '3. Run the per-wave e2e with mock AI: start the stack with `model.mode=mock` and `test/fakes/llm`, then `test/e2e/run.sh`. No live model, no real provider; a failing suite blocks the wave.',
     '4. Check every `<pkg>fake` still satisfies its `contract.go` (the compile-time assertions do this) and that no `contract.go` changed shape.',
     '5. Merge `wave/<n>` into `main` (fast-forward), tag `wave-<n>`, delete the task branches, open `wave/<n+1>` from `main`.\n',
     '## Critical path\n',
     ' -> '.join(c['critical_path']) + '\n',
     '## Verify flags (off by default)\n',
     '| Blocker | Flag |', '|---|---|']
for k, v in out['flags'].items():
    L.append(f'| {k} | `{v}` |')
L += ['', '## Waves\n', f"Max width {c['max_width']}.\n", '| Wave | Tasks |', '|---|---|']
for w, n in c['per_wave'].items():
    L.append(f'| {w} | {n} |')
L.append('')
cur = None
for t in out['tasks']:
    if t['wave'] != cur:
        cur = t['wave']
        L.append(f"\n### Wave {cur} ({c['per_wave'][str(cur)]} tasks)\n")
        L.append('| Done | Id | Parent | Layer | Title | Depends on | Files | Flag |\n|---|---|---|---|---|---|---|---|')
    dl = t['depends_on']
    dep = ', '.join(dl) if len(dl) <= 6 else ', '.join(dl[:6]) + f' +{len(dl) - 6}'
    fs = t['files']
    files = '<br>'.join(f'`{f}`' for f in fs[:4]) + (f'<br>+{len(fs) - 4} more' if len(fs) > 4 else '')
    L.append(f"| [ ] | {t['id']} | {t['parent']} | {t['layer']} | {t['title'].replace('|', '/')} | {dep} | {files} | {t.get('flag', '')} |")
with open(R + '/docs/TASKS.md', 'w') as fh:
    fh.write('\n'.join(L) + '\n')
print(json.dumps({k: c[k] for k in ('total', 'per_wave', 'max_width')}), 'critical_path', len(c['critical_path']))
