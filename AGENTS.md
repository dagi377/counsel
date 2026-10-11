# Implementer guide (read fully before writing code)

You are building Counsel from a fixed spec. Fill in tasks in order; do not redesign.
Money, trust and deadline mistakes cost firms their licence. If anything is ambiguous, write
the question into `docs/QUESTIONS.md` and stop. Do not guess.

Source pages: `spec/eng/o1-build-guide.html` (tasks, hard rules, conflict register) and
`spec/eng/k1-kernel.html` (kernel/app boundary and interfaces).

## Stack

Go 1.26, Postgres 16 (one cluster per cell), chi, pgx/v5, River for jobs, OpenTelemetry,
`log/slog`. Front end: TypeScript and React in `web/`.

Two layers in one Go module: `kernel/` is domain-free business plumbing any app can reuse;
`app/legal/` is Counsel. The app registers with the kernel at boot through `kernel/registry`
(`app/legal/register.go`: `func Register(r registry.Registry) error`).

## Workflow (every task)

1. Open `docs/TASKS.md`. Take the next task whose `depends_on` are all done and whose
   `blocked_by` is empty or resolved.
2. Read every rule id on the task, and the OpenAPI operations and DDL tables you touch.
3. Implement only that task. Keep exported interfaces exactly as written.
4. Write the Accept tests first. Tests live next to code (`foo_test.go`).
5. Run `make check` (gofmt, go vet, go test, spec lint, rule coverage). Fix until green.
6. Tick the task (`- [x]`). One commit per task: `feat: <task id> <subject>`.

Source of truth, highest first: rule ids (b1, b2, b3, a1, a2, a3) > `api/openapi.yaml` >
`migrations/0001_init.sql` > screens and `access.json`. A conflict is never fixed silently.

## Hard rules

- **Contracts are frozen.** Files with a `CONTRACT` header (`api/openapi.yaml`, `migrations/`,
  `spec/kernel/*.schema.json`, the `Registry` interface, `spec/rules/*.json`) do not change
  shape. Changes are additive only; write why in `docs/QUESTIONS.md` and stop.
- **Kernel never imports app.** Nothing under `kernel/` imports `app/`. No legal term (matter,
  trust, lawyer, court, HST, LSO, By-Law, Clio, Ontario, ...) in kernel code, strings,
  comments, SQL or YAML. `tools/kernel_allow.txt` and `tools/kernel_bindings.txt` only shrink.
- **Money is int64 cents; every rate and percentage is int ppm.** No float, no basis points.
  Every division goes through `money.Rhu`. Round only at the five points in BIL-C-002.
- **Preparer is never the approver; the agent never approves.** An agent principal only
  drafts and proposes; its acting-for user counts as a preparer. Trust money moves only in a
  gate effect handler that re-checks the balance under `SELECT FOR UPDATE`.
- **Business time zone is America/Toronto.** Use `clock.Today(ctx)`, never `time.Now()` for dates.
- **Never log secrets or client data.** No document text, notes, client names tied to
  matters or amounts tied to a named client in logs, errors, events, audit detail or metric labels.
- **Tag rules.** Code that enforces a rule has `// rule: <id>` on the line above. Test names
  carry the id: `TestBIL_C_202_TaxShareProportional`. Problem+json errors carry `"rule"`.
- **Mock AI only in tests.** `model.mode=mock` is the default. Tests never call a real model.
- **Unverified law stays off.** A rule marked `(verify)` is tagged `// rule: <id> (verify)`
  and ships behind a feature flag that is off by default.
- **Never delete financial rows.** No DELETE or TRUNCATE on financial, trust, journal, gate or
  audit rows; corrections are reversing rows. Schema changes are expand-contract only.
- **Modules own their tables.** A module writes only its own tables and never joins another's.
- **Idempotency.** Every money or external-effect POST handles `Idempotency-Key`; every
  provider webhook dedups on `(provider, event_id)`.
- **Logging:** `log/slog`, structured fields only. **Errors:** `fmt.Errorf("context: %w", err)`;
  sentinel errors live in the owning package. **Context:** every I/O function takes `ctx` first.
- **Tests:** table-driven. Unit tests need no network or Docker. Postgres tests use
  `//go:build integration` and read `COUNSEL_TEST_DB`.

## Where things go

| Adding | Goes in |
|---|---|
| a shipped binary | `cmd/counsel*` (`counseld`, `counsel-worker`, `counsel-migrate`, `counsel-import`); `main.go` only parses flags and calls into `kernel/` and `app/legal` |
| domain-free plumbing (identity, gates, audit, events, money, ledger, docs, work, deadlines, reports, import, agent tools) | `kernel/<module>` |
| legal behaviour (parties, matters, conflicts, intake, time, trust, billing, legal registrations) | `app/legal/<module>` |
| a deadline rule pack | `app/legal/rulepacks/<jurisdiction>/` (YAML) |
| a web page | `web/src/features/<area>`; shell and generated client in `web/src/kernel` |
| a schema change | `migrations/NNNN_<subject>.sql`, expand-contract only |
| a spec copy used as test data | `spec/` |
| a fake of an external system | `test/fakes/<protocol>`, named by protocol, never by vendor |
| an e2e scenario | `test/e2e/suites/<name>.sh` |
| fixture data | the package's own `testdata/` |
| a design decision | `docs/adr/NNNN-<subject>.md` |

Never name a file, package, type or flag after a work stream or a version: name it after what it does.

## Allowed dependencies (add others only with a `docs/QUESTIONS.md` entry)

| Need | Module |
|---|---|
| Postgres | `github.com/jackc/pgx/v5` |
| HTTP router | `github.com/go-chi/chi/v5` |
| Job queue | `github.com/riverqueue/river`, `github.com/riverqueue/river/riverdriver/riverpgxv5` |
| Tracing and metrics | `go.opentelemetry.io/otel` and its `sdk` and OTLP exporters |
| YAML | `gopkg.in/yaml.v3` |
| JSON Schema validation | `github.com/santhosh-tekuri/jsonschema/v5` |
| Front end | TypeScript, React; Vite (build) and Vitest (unit tests) in `web/` |
| Everything else | Go standard library |

## Parallel agents

- Own only the files your task lists. Do not edit another task's files.
- A contract change must be additive, and needs a `docs/QUESTIONS.md` entry first.
- Scripts and Makefile targets use absolute or repo-relative paths: no `cd`, no `sed -i`.
