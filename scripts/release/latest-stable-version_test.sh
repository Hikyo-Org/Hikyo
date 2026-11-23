#!/bin/sh
set -eu

script=$(dirname "$0")/latest-stable-version.sh

[ -z "$(printf '[]' | "$script")" ]
[ -z "$(printf '[[{"tag_name":"v0.0.1-nightly.20260824.1.g12345678","draft":false,"prerelease":true}]]' | "$script")" ]

pages='[
  [{"tag_name":"v2.0.0-nightly.20260824.2.g12345678","draft":false,"prerelease":true}],
  [{"tag_name":"v1.4.2","draft":false,"prerelease":false}]
]'
[ "$(printf '%s\n' "$pages" | "$script")" = 'v1.4.2' ]

# A desktop release listed first is skipped, whatever its order or prerelease flag.
desktop='[
  [{"tag_name":"desktop-v1.4.3","draft":false,"prerelease":false},
   {"tag_name":"v1.4.2","draft":false,"prerelease":false}],
  [{"tag_name":"v1.4.1","draft":false,"prerelease":false}]
]'
[ "$(printf '%s\n' "$desktop" | "$script")" = 'v1.4.2' ]
[ -z "$(printf '[[{"tag_name":"desktop-v1.4.3","draft":false,"prerelease":false}]]' | "$script")" ]

expect_reject() {
	label=$1
	payload=$2
	if printf '%s\n' "$payload" | "$script" >/dev/null 2>&1; then
		printf 'latest stable version fixture failed: %s accepted\n' "$label" >&2
		exit 1
	fi
}

expect_reject malformed-json '{'
expect_reject wrong-shape '{}'
expect_reject desktop-lookalike '[[{"tag_name":"desktop-1.4.3","draft":false,"prerelease":false}]]'
expect_reject missing-tag-name '[[{"draft":false,"prerelease":false}]]'
expect_reject malformed-stable-tag '[[{"tag_name":"latest","draft":false,"prerelease":false}]]'

printf 'latest stable version fixture: empty and paginated release histories are deterministic\n'
