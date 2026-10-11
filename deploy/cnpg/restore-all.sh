#!/usr/bin/env bash
# rule: ARC-U-043
# All-cells restore (region loss and the yearly drill): runs restore-drill.sh for every cell,
# in firm-priority order, at most 8 at a time, each within a 45 minute budget. A cell still
# running at its budget is stopped and reported failed.
#
# Usage: restore-all.sh <cells-file> <target-time>
#   cells-file   one cell per line, highest priority first; blank lines and # comments skipped
#   target-time  UTC, RFC 3339 with seconds, passed to every restore-drill.sh
#
# Environment (restore-drill.sh's own variables pass through):
#   PARALLEL        restore slots, 1 to 8 (default 8)
#   BUDGET_SECONDS  per-cell budget (default 2700 = 45 min)
#   POLL_SECONDS    how often slots are checked (default 5)
#   LOG_DIR         where each cell's drill output goes (default: a new temp dir)
#
# Report on stdout: "start <cell>" and "done <cell>" as they happen, then one line per cell in
# priority order, "<cell> PASS|FAIL <seconds>s[ over budget]", then
# "restore all: PASS|FAIL <passed>/<cells>". Exit 0 only when every cell passed.
set -euo pipefail

usage() {
	echo "usage: restore-all.sh <cells-file> <target-time, e.g. 2026-10-11T03:00:00Z>" >&2
	exit 2
}

[ "$#" -eq 2 ] || usage
CELLS_FILE=$1
TARGET_TIME=$2
[ -r "$CELLS_FILE" ] || { echo "restore-all: cannot read $CELLS_FILE" >&2; usage; }

PARALLEL=${PARALLEL:-8}
BUDGET_SECONDS=${BUDGET_SECONDS:-2700}
POLL_SECONDS=${POLL_SECONDS:-5}
[[ $PARALLEL =~ ^[1-8]$ ]] || { echo "restore-all: PARALLEL must be 1 to 8" >&2; exit 2; }
[[ $BUDGET_SECONDS =~ ^[1-9][0-9]*$ ]] || { echo "restore-all: bad BUDGET_SECONDS" >&2; exit 2; }
LOG_DIR=${LOG_DIR:-$(mktemp -d "${TMPDIR:-/tmp}/restore-all.XXXXXX")}
HERE=${BASH_SOURCE[0]%/*}
[ "$HERE" != "${BASH_SOURCE[0]}" ] || HERE=.
# A stopped drill gets this long to delete its namespace before it is killed outright.
GRACE_SECONDS=60

cells=()
while IFS= read -r line || [ -n "$line" ]; do
	line=${line%%#*}
	line=${line//[[:space:]]/}
	[ -n "$line" ] || continue
	for c in ${cells[@]+"${cells[@]}"}; do
		[ "$c" != "$line" ] || { echo "restore-all: cell listed twice: $line" >&2; exit 2; }
	done
	cells+=("$line")
done <"$CELLS_FILE"
[ "${#cells[@]}" -gt 0 ] || { echo "restore-all: no cells in $CELLS_FILE" >&2; exit 2; }
echo "logs in $LOG_DIR" >&2

n=${#cells[@]}
pid=()     # drill pid while running, empty otherwise
started=() # SECONDS at start
took=()    # seconds, once done
result=()  # PASS or FAIL, once done
over=()    # 1 when stopped at the budget
for ((i = 0; i < n; i++)); do pid[i]=""; over[i]=0; done

next=0
running=0
done_count=0

stop() { # $1 drill pid, $2 signal: stop the drill's current command, then the drill
	pkill "-$2" -P "$1" 2>/dev/null || true
	[ "$2" = TERM ] || kill "-$2" "$1" 2>/dev/null || true
}

while [ "$done_count" -lt "$n" ]; do
	# Fill free slots in priority order.
	while [ "$running" -lt "$PARALLEL" ] && [ "$next" -lt "$n" ]; do
		cell=${cells[next]}
		echo "start $cell"
		bash "$HERE/restore-drill.sh" "$cell" "$TARGET_TIME" >"$LOG_DIR/$cell.log" 2>&1 </dev/null &
		pid[next]=$!
		started[next]=$SECONDS
		next=$((next + 1))
		running=$((running + 1))
	done

	sleep "$POLL_SECONDS"

	for ((i = 0; i < next; i++)); do
		p=${pid[i]}
		[ -n "$p" ] || continue
		elapsed=$((SECONDS - started[i]))
		if kill -0 "$p" 2>/dev/null; then
			if [ "$elapsed" -ge "$BUDGET_SECONDS" ]; then
				over[i]=1
				if [ "$elapsed" -ge $((BUDGET_SECONDS + GRACE_SECONDS)) ]; then
					stop "$p" KILL
				else
					stop "$p" TERM
				fi
			fi
			continue
		fi
		rc=0
		wait "$p" || rc=$?
		pid[i]=""
		took[i]=$elapsed
		if [ "$rc" -eq 0 ] && [ "${over[i]}" -eq 0 ] && [ "$elapsed" -le "$BUDGET_SECONDS" ]; then
			result[i]=PASS
		else
			result[i]=FAIL
			[ "$elapsed" -le "$BUDGET_SECONDS" ] || over[i]=1
		fi
		echo "done ${cells[i]}"
		running=$((running - 1))
		done_count=$((done_count + 1))
	done
done

passed=0
for ((i = 0; i < n; i++)); do
	note=""
	[ "${over[i]}" -eq 0 ] || note=" over budget"
	echo "${cells[i]} ${result[i]} ${took[i]}s$note"
	[ "${result[i]}" != PASS ] || passed=$((passed + 1))
done
if [ "$passed" -eq "$n" ]; then
	echo "restore all: PASS $passed/$n"
	exit 0
fi
echo "restore all: FAIL $passed/$n"
exit 1
