#!/usr/bin/env bash
# Shared helpers for e2e suites. Sourced by run.sh before each suite runs.
# Suites call these helpers; they never source each other.

E2E_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export E2E_ROOT

# log prints a timestamped line to stderr.
log() {
	printf '%s %s\n' "$(date -u +%H:%M:%S)" "$*" >&2
}

# fail prints a message and exits the suite with status 1.
fail() {
	log "FAIL: $*"
	exit 1
}

# assert_eq fails the suite when $1 (got) differs from $2 (want); $3 names the check.
assert_eq() {
	if [ "$1" != "$2" ]; then
		fail "${3:-assert_eq}: got '$1', want '$2'"
	fi
}

# assert_contains fails the suite when $1 does not contain $2; $3 names the check.
assert_contains() {
	case "$1" in
	*"$2"*) ;;
	*) fail "${3:-assert_contains}: '$2' not found" ;;
	esac
}

# wait_for retries a command until it succeeds or the timeout in seconds ($1) passes.
wait_for() {
	local timeout="$1"
	shift
	local deadline=$((SECONDS + timeout))
	until "$@" >/dev/null 2>&1; do
		if [ "$SECONDS" -ge "$deadline" ]; then
			fail "timed out after ${timeout}s waiting for: $*"
		fi
		sleep 1
	done
}
