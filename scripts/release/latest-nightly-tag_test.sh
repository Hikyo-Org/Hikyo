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

[ "$(printf '%s\n' "$pages" | "$script" --limit 2)" = 'v0.0.1-nightly.20260824.3.gcf4bb563
v0.0.1-nightly.20260823.2.g12345678' ]
[ "$(printf '%s\n' "$pages" | "$script" --limit 5 | wc -l | tr -d ' ')" = 2 ]

# GitHub lists releases by source commit date, so publication order is the tag's
# date and run number whatever the listing order.
unordered='[
  [{"tag_name":"v0.0.1-nightly.20260823.9.g11111111","draft":false,"prerelease":true}],
  [{"tag_name":"v0.0.1-nightly.20260824.3.gcf4bb563","draft":false,"prerelease":true},
   {"tag_name":"v0.0.1-nightly.20260824.12.gdddddddd","draft":false,"prerelease":true},
   {"tag_name":"v0.0.1-nightly.20260824.4.gaaaaaaaa","draft":false,"prerelease":true}]
]'
[ "$(printf '%s\n' "$unordered" | "$script")" = 'v0.0.1-nightly.20260824.12.gdddddddd' ]
[ "$(printf '%s\n' "$unordered" | "$script" --limit 3)" = 'v0.0.1-nightly.20260824.12.gdddddddd
v0.0.1-nightly.20260824.4.gaaaaaaaa
v0.0.1-nightly.20260824.3.gcf4bb563' ]
# A tag that does not carry the published date, run number and commit is not a nightly.
loose='[[{"tag_name":"v0.0.1-nightly.20260825.1","draft":false,"prerelease":true},
  {"tag_name":"v0.0.1-nightly.20260824.3.gcf4bb563","draft":false,"prerelease":true}]]'
[ "$(printf '%s\n' "$loose" | "$script")" = 'v0.0.1-nightly.20260824.3.gcf4bb563' ]

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
if printf '%s\n' "$pages" | "$script" --limit 0 >/dev/null 2>&1; then
	printf 'latest nightly tag fixture failed: zero limit accepted\n' >&2
	exit 1
fi

printf 'latest nightly tag fixture: published prerelease selection is deterministic\n'
