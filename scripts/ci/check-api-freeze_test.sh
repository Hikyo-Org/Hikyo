#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
fixture_root="$repo_root/api/testdata/freeze/delete-deprecated-endpoint-fails"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

git clone --quiet --shared --no-tags "$repo_root" "$tmp/repo"
cp "$repo_root/scripts/ci/check-api-freeze.sh" "$tmp/repo/scripts/ci/"
mkdir -p "$tmp/repo/scripts/ci/api-freeze-guard"
cp "$repo_root/scripts/ci/api-freeze-guard/main.go" \
	"$tmp/repo/scripts/ci/api-freeze-guard/"
guard="$tmp/repo/scripts/ci/check-api-freeze.sh"
cd "$tmp/repo"
git config user.name 'API freeze fixture'
git config user.email 'api-freeze-fixture@invalid.example'
git config commit.gpgsign false
git tag v1.0.0-rc.1

dormant_output=$($guard)
printf '%s\n' "$dormant_output" | grep -F \
	'API freeze guard dormant: v1.0.0 does not exist' >/dev/null

blob=$(printf '%s' 'not a release commit' | git hash-object -w --stdin)
git tag v1.0.0 "$blob"
if invalid_tag_output=$($guard 2>&1); then
	printf '%s\n' 'API freeze fixture failed: non-commit tag passed' >&2
	exit 1
fi
printf '%s\n' "$invalid_tag_output" | grep -F \
	'refs/tags/v1.0.0 does not resolve to a commit' >/dev/null
git tag --delete v1.0.0 >/dev/null

cp "$fixture_root/base.yaml" api/openapi.yaml
git add api/openapi.yaml
git commit --quiet -m 'test: install synthetic freeze base'
git tag v1.0.0

active_output=$($guard)
printf '%s\n' "$active_output" | grep -F \
	'API freeze guard passed: api/openapi.yaml is compatible with v1.0.0' >/dev/null

cp "$fixture_root/revised.yaml" api/openapi.yaml
if failure_output=$($guard 2>&1); then
	printf '%s\n' 'API freeze fixture failed: breaking change passed' >&2
	exit 1
fi
printf '%s\n' "$failure_output" | grep -F 'API freeze guard failed:' >/dev/null
printf '%s\n' "$failure_output" | grep -F \
	'[api-path-removed-with-deprecation]' >/dev/null

printf '%s\n' \
	'API freeze fixture: dormant, invalid-tag refusal, active-pass, and active-refusal paths passed'
