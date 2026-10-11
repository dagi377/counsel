# Restore runbook

Rule: ARC-U-043 (restore capacity and drills). Scripts: `deploy/cnpg/restore-drill.sh` (one
cell) and `deploy/cnpg/restore-all.sh` (every cell). Run both from the repo root, against the
DR cluster.

## Budgets

Every cell has 45 minutes, split into three 15-minute stages:

| Stage | Budget | Where it shows |
|---|---|---|
| Base restore (about 50 GB) | 15 min | recovery Cluster created until WAL replay starts |
| WAL replay to the target time | 15 min | base backups are at most 24 h old, so replay fits |
| Restore checks | 15 min | the `restore-check` pod |

The scripts enforce the 45-minute total, not each stage. A cell over 45 minutes is a failed
restore. In a drill, a cell over 60 minutes, or all cells over 4 hours, is a failed drill and
blocks onboarding new cells until it is fixed.

## Before you start

- `kubectl` points at the DR cluster. Set `KUBE_CONTEXT` to that context; the scripts refuse
  to run without it.
- Pick the target time: UTC, RFC 3339 with seconds, for example `2026-10-11T03:00:00Z`. For a
  drill, use a time from the last hour. For a real restore, use the last moment before the
  incident.
- Optional: `IMAGE` is the Counsel image that runs the checks (the cell's current release),
  `DR_REGION` is the DR bucket's region (default `ca-west-1`).

## Single-cell PITR

    KUBE_CONTEXT=<dr-context> deploy/cnpg/restore-drill.sh <cell> <target-time>

The script:

1. creates the scratch namespace `scratch-<cell>-<timestamp>`, labelled with the cell;
2. applies `deploy/cnpg/restore-cluster.yaml`: a one-instance CloudNativePG Cluster that
   bootstraps from the cell's base backup and WAL archive (`s3://counsel-<cell>-wal-dr/`) and
   replays to the target time;
3. waits up to 45 minutes for the Cluster to be Ready;
4. runs the restore checks in a `restore-check` pod against the restored database;
5. deletes the scratch namespace, also when a step failed.

The last line is `restore drill <cell>: PASS <n>s` or `restore drill <cell>: FAIL <n>s`. Exit
status 0 is a pass, 1 a failed step, 2 bad arguments.

To rehearse the steps without a cluster, set `DRY_RUN=1`: `KUBE_CONTEXT` is not needed and
every `kubectl` command is printed before it runs. Put a fake `kubectl` first on `PATH`;
`tools/restore_drill_test.go` does this.

## All cells (region loss, yearly drill)

Write the cells in firm-priority order, one per line, priority-1 cells first (blank lines and
`#` comments are skipped):

    # cells.txt
    firm-a
    firm-b

Then:

    KUBE_CONTEXT=<dr-context> deploy/cnpg/restore-all.sh cells.txt <target-time>

It starts cells in file order and runs at most 8 at a time (`PARALLEL`, 1 to 8). When a cell
finishes, the next one starts. A cell still running at 45 minutes (`BUDGET_SECONDS`) is stopped,
its namespace is deleted, and it is reported failed. Each cell's drill output is in `LOG_DIR`
(printed at the start), in `<cell>.log`.

Timing for 30 cells: cluster bring-up 30 min, then 4 waves of 45 min, for 3 h 30 min in total,
inside the 4 h target. The first priority-1 cell must serve writes within 60 min. Above 32 cells
in a region, more parallel slots are needed: ceil(cells / slots) × 45 min + 30 min must stay at or
under 4 h.

## The restore check

The restore check is the trust three-way reconciliation, run on the restored database: for
every trust account, the sum of the client ledgers equals the reconciled bank balance (statement
closing balance plus deposits in transit minus outstanding cheques) equals the trust book
balance. Any difference fails the restore, even a single cent.

Counsel registers it at boot with the kernel registry (`RestoreCheck`). The kernel's
`kernel/restorecheck` runs every registered check, and a failing check never stops the rest. The
`restore-check` pod runs `counseld restore-check` (override with `RESTORE_CHECK_CMD`).

Not built yet: the `counseld restore-check` subcommand and the app's registration of the
three-way check. Until both land, a real drill restores and reports the database but its check
step fails.

## Reading the report

The `restore-check` pod prints one line per check, then a summary:

    PASS <check> 412ms
    FAIL <check> 3.1s: <reason>
    restore check: FAIL 1/2

`restore check: PASS n/n` means every check passed. With FAIL, the lines that start with `FAIL`
name each failed check and its reason. An empty run (`0/0`) is a failure: nothing was proved.

`restore-all.sh` prints `start <cell>` and `done <cell>` as cells run, then one line per cell in
priority order, then the verdict:

    firm-a PASS 2210s
    firm-b FAIL 2700s over budget
    restore all: FAIL 1/2

- `PASS`: the restore and every check passed within budget.
- `FAIL` without `over budget`: a step or a check failed; read `<cell>.log` in `LOG_DIR`.
- `FAIL ... over budget`: the cell hit 45 minutes and was stopped.

Exit status 0 only when every cell passed.

## After a failure

1. Read the cell's log; the last error shows the step that failed.
2. For a failed check, rerun the single cell with `restore-drill.sh` once the cause is fixed.
3. Record the failed drill. New cells are not onboarded until a rerun passes.
