#!/usr/bin/env bash
# Runs every e2e suite in test/e2e/suites/*.sh, in name order. Suites are discovered,
# never registered here: adding a suite means adding a file, not editing this script.
#
# Usage: test/e2e/run.sh [name-filter]
#   name-filter  run only suites whose file name contains this text
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
filter="${1:-}"

shopt -s nullglob
suites=("$here"/suites/*.sh)
shopt -u nullglob

if [ "${#suites[@]}" -eq 0 ]; then
	echo "e2e: no suites in $here/suites, nothing to run"
	exit 0
fi

passed=0
failed=()
for suite in "${suites[@]}"; do
	name="$(basename "$suite" .sh)"
	if [ -n "$filter" ] && [[ "$name" != *"$filter"* ]]; then
		continue
	fi
	echo "=== e2e: $name"
	# Each suite runs in its own shell so one suite's state never leaks into the next.
	if bash -c 'set -euo pipefail; source "$1"; source "$2"' _ "$here/lib.sh" "$suite"; then
		echo "--- PASS: $name"
		passed=$((passed + 1))
	else
		echo "--- FAIL: $name"
		failed+=("$name")
	fi
done

echo "e2e: $passed passed, ${#failed[@]} failed"
if [ "${#failed[@]}" -gt 0 ]; then
	printf '  %s\n' "${failed[@]}"
	exit 1
fi
