#!/bin/sh
set -eu

script=$(dirname "$0")/next-nightly-version.sh
commit=176e6e67379d0675e6211f0491dc965cee4f1c5c

[ "$("$script" 1.0.0 20260824 42 "$commit")" = \
	'1.1.0-nightly.20260824.42.g176e6e67' ]
[ "$("$script" v1.0.9 20261231 7 "$commit")" = \
	'1.1.0-nightly.20261231.7.g176e6e67' ]
[ "$("$script" 2.14.3 20260824 1001 "$commit")" = \
	'2.15.0-nightly.20260824.1001.g176e6e67' ]

expect_reject() {
	label=$1
	shift
	if "$script" "$@" >/dev/null 2>&1; then
		printf 'next nightly version fixture failed: %s accepted\n' "$label" >&2
		exit 1
	fi
}

expect_reject prerelease 1.1.0-rc.1 20260824 42 "$commit"
expect_reject invalid-date 1.0.0 2026824 42 "$commit"
expect_reject invalid-run 1.0.0 20260824 run-42 "$commit"
expect_reject short-commit 1.0.0 20260824 42 deadbeef

printf 'next nightly version fixture: stable next-minor tags are deterministic\n'
