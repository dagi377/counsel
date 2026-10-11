#!/usr/bin/env bash
# rule: ARC-U-043
# Restore drill for one cell: point-in-time restore of the cell's database into a scratch
# namespace, wait until the restored cluster is Ready, run the registered restore checks,
# then delete the namespace (also on failure).
#
# Usage: restore-drill.sh <cell> <target-time>
#   cell         lower-case DNS label, as in the cell chart (`--set cell=<id>`)
#   target-time  UTC, RFC 3339 with seconds, e.g. 2026-10-11T03:00:00Z
#
# Environment:
#   KUBE_CONTEXT      kube context of the DR cluster (required unless DRY_RUN=1)
#   DRY_RUN=1         rehearsal: KUBE_CONTEXT is not required and every kubectl command is
#                     printed to stderr before it runs; put a fake kubectl first on PATH
#   DRILL_ID          suffix of the scratch namespace (default: UTC timestamp)
#   DR_REGION         region of the DR bucket (default ca-west-1)
#   IMAGE             Counsel image that runs the restore checks
#   READY_TIMEOUT     how long to wait for the restored cluster (default 45m)
#   RESTORE_CHECK_CMD command run in IMAGE against the restored database
#                     (default: counseld restore-check)
#
# Exit status: 0 when the restore and every check passed, 1 when a step failed, 2 on bad
# usage. The last line on stdout is "restore drill <cell>: PASS|FAIL <seconds>s".
set -euo pipefail

usage() {
	echo "usage: restore-drill.sh <cell> <target-time, e.g. 2026-10-11T03:00:00Z>" >&2
	exit 2
}

[ "$#" -eq 2 ] || usage
CELL=$1
TARGET_TIME=$2
[[ $CELL =~ ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$ ]] || { echo "restore-drill: bad cell: $CELL" >&2; usage; }
[[ $TARGET_TIME =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || { echo "restore-drill: bad target time: $TARGET_TIME" >&2; usage; }

DRY_RUN=${DRY_RUN:-0}
if [ "$DRY_RUN" != 1 ] && [ -z "${KUBE_CONTEXT:-}" ]; then
	echo "restore-drill: KUBE_CONTEXT is required (the DR cluster)" >&2
	exit 2
fi

HERE=${BASH_SOURCE[0]%/*}
[ "$HERE" != "${BASH_SOURCE[0]}" ] || HERE=.
DRILL_ID=${DRILL_ID:-$(date -u +%Y%m%d%H%M%S)}
NS=scratch-$CELL-$DRILL_ID
CLUSTER=counsel-$CELL-pg-restore
DR_REGION=${DR_REGION:-ca-west-1}
IMAGE=${IMAGE:-ghcr.io/dagi377/counsel:0.1.0}
READY_TIMEOUT=${READY_TIMEOUT:-45m}
RESTORE_CHECK_CMD=${RESTORE_CHECK_CMD:-counseld restore-check}
START=$SECONDS

kc() {
	local ctx=()
	[ -z "${KUBE_CONTEXT:-}" ] || ctx=(--context "$KUBE_CONTEXT")
	[ "$DRY_RUN" != 1 ] || echo "+ kubectl $*" >&2
	kubectl ${ctx[@]+"${ctx[@]}"} "$@"
}

result=FAIL
finish() {
	# The scratch namespace goes whatever happened; the restored data must not linger.
	kc delete namespace "$NS" --ignore-not-found --wait=false >/dev/null || true
	echo "restore drill $CELL: $result $((SECONDS - START))s"
}
trap finish EXIT

# Placeholders are fixed tokens; values are validated above, so no escaping is needed.
render() {
	local line
	while IFS= read -r line || [ -n "$line" ]; do
		line=${line//@CELL@/$CELL}
		line=${line//@NAMESPACE@/$NS}
		line=${line//@TARGET_TIME@/$TARGET_TIME}
		line=${line//@DR_REGION@/$DR_REGION}
		printf '%s\n' "$line"
	done <"$HERE/restore-cluster.yaml"
}

# The check pod reads the restored database through the secret CloudNativePG creates.
overrides() {
	local cmd="" w
	for w in $RESTORE_CHECK_CMD; do cmd="$cmd${cmd:+,}\"$w\""; done
	printf '{"apiVersion":"v1","spec":{"containers":[{"name":"restore-check","image":"%s","command":[%s],"env":[{"name":"COUNSEL_DATABASE_URL","valueFrom":{"secretKeyRef":{"name":"%s-app","key":"uri"}}}]}]}}' \
		"$IMAGE" "$cmd" "$CLUSTER"
}

kc create namespace "$NS"
kc label namespace "$NS" "counsel.io/cell=$CELL" counsel.io/drill=restore
render | kc apply -n "$NS" -f -
kc wait -n "$NS" --for=condition=Ready "cluster/$CLUSTER" --timeout="$READY_TIMEOUT"
kc run restore-check -n "$NS" --image="$IMAGE" --restart=Never --rm -i --overrides="$(overrides)"
result=PASS
