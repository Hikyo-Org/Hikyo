#!/bin/sh
set -eu

script=$(dirname "$0")/nightly-run-tag.sh

[ -z "$(printf '[]' | "$script" 3)" ]

refs='[[
  {"ref":"refs/tags/v0.0.1-nightly.20260824.3.gcf4bb563"},
  {"ref":"refs/tags/v1.1.0-nightly.20260825.4.g12345678"},
  {"ref":"refs/tags/v1.0.0"}
]]'
[ "$(printf '%s\n' "$refs" | "$script" 3)" = 'v0.0.1-nightly.20260824.3.gcf4bb563' ]

changed_date_and_version='[[
  {"ref":"refs/tags/v1.1.0-nightly.20260825.3.gcf4bb563"}
]]'
[ "$(printf '%s\n' "$changed_date_and_version" | "$script" 3)" = \
	'v1.1.0-nightly.20260825.3.gcf4bb563' ]

expect_reject() {
	label=$1
	run=$2
	payload=$3
	if printf '%s\n' "$payload" | "$script" "$run" >/dev/null 2>&1; then
		printf 'nightly run tag fixture failed: %s accepted\n' "$label" >&2
		exit 1
	fi
}

expect_reject invalid-run 0 '[]'
expect_reject malformed-json 3 '{'
expect_reject wrong-shape 3 '{}'
expect_reject duplicate-run 3 '[[
  {"ref":"refs/tags/v0.0.1-nightly.20260824.3.gcf4bb563"},
  {"ref":"refs/tags/v1.1.0-nightly.20260825.3.g12345678"}
]]'

printf 'nightly run tag fixture: reruns reuse one durable tag identity\n'
