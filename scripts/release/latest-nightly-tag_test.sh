#!/bin/sh
set -eu

script=$(dirname "$0")/latest-nightly-tag.sh

[ -z "$(printf '[]' | "$script")" ]
[ -z "$(printf '[[{"tag_name":"v1.0.0","draft":false,"prerelease":false}]]' | "$script")" ]

pages='[
  [{"tag_name":"v0.0.1-nightly.20260824.3.gcf4bb563","draft":false,"prerelease":true}],
  [{"tag_name":"v0.0.1-nightly.20260823.2.g12345678","draft":false,"prerelease":true}]
]'
[ "$(printf '%s\n' "$pages" | "$script")" = 'v0.0.1-nightly.20260824.3.gcf4bb563' ]

mixed='[[
  {"tag_name":"v0.0.1-nightly.20260824.4.gaaaaaaaa","draft":true,"prerelease":true},
  {"tag_name":"invalid-nightly","draft":false,"prerelease":true},
  {"tag_name":"v0.0.1-nightly.20260824.3.gcf4bb563","draft":false,"prerelease":true}
]]'
[ "$(printf '%s\n' "$mixed" | "$script")" = 'v0.0.1-nightly.20260824.3.gcf4bb563' ]

expect_reject() {
	label=$1
	payload=$2
	if printf '%s\n' "$payload" | "$script" >/dev/null 2>&1; then
		printf 'latest nightly tag fixture failed: %s accepted\n' "$label" >&2
		exit 1
	fi
}

expect_reject malformed-json '{'
expect_reject wrong-shape '{}'

printf 'latest nightly tag fixture: published prerelease selection is deterministic\n'
