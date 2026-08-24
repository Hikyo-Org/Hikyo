#!/bin/sh
# GitHub expressions below are literal fixture text.
# shellcheck disable=SC2016
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
workflow="$repo_root/.github/workflows/nightly.yml"
official="$repo_root/.github/workflows/release.yml"
goreleaser="$repo_root/.goreleaser.yaml"
stable_resolver="$repo_root/scripts/release/latest-stable-version.sh"

fail() {
	printf 'nightly release fixture failed: %s\n' "$1" >&2
	exit 1
}

[ -f "$workflow" ] || fail 'nightly workflow is missing'
grep -F 'cron: "0 2 * * *"' "$workflow" >/dev/null || fail '02:00 UTC schedule is missing'
grep -F 'ref: refs/heads/main' "$workflow" >/dev/null || fail 'nightly checkout is not pinned to main'
grep -F 'COMMIT: ${{ steps.source.outputs.commit }}' "$workflow" >/dev/null || fail 'nightly does not use the checked-out main SHA'
grep -F 'select(.draft == false and .prerelease == true' "$workflow" >/dev/null || fail 'draft nightlies can suppress publication'
grep -F './scripts/release/require-green-main.sh "$REPOSITORY" "$COMMIT"' "$workflow" >/dev/null || fail 'nightly does not require exact-head green main CI'
grep -F 'INITIAL_NIGHTLY_VERSION: 0.0.1' "$workflow" >/dev/null || fail 'first nightly version is not explicit'
grep -F 'set -o pipefail' "$workflow" >/dev/null || fail 'paginated release discovery can hide API failures'
grep -F './scripts/release/latest-stable-version.sh' "$workflow" >/dev/null || fail 'stable release discovery is missing'
grep -F 'select(.draft == false and .prerelease == false' "$stable_resolver" >/dev/null || fail 'stable resolver admits prereleases'
grep -F './scripts/release/nightly-version.sh "$INITIAL_NIGHTLY_VERSION"' "$workflow" >/dev/null || fail 'first nightly does not use the initial version'
if grep -F 'releases/latest' "$workflow" >/dev/null; then
	fail 'no-release bootstrap still depends on the latest-release endpoint'
fi
grep -F 'HIKYO_RELEASE_VERSION: ${{ steps.identity.outputs.version }}' "$workflow" >/dev/null || fail 'GoReleaser does not receive nightly version'
grep -F 'gh release create "$TAG"' "$workflow" >/dev/null || fail 'nightly prerelease publication is missing'
grep -F -- '--prerelease' "$workflow" >/dev/null || fail 'nightly is not marked prerelease'
grep -F 'dist/hikyo_*_Darwin_*.tar.gz' "$workflow" >/dev/null || fail 'macOS assets are missing'
grep -F 'dist/hikyo_*_Linux_*.tar.gz' "$workflow" >/dev/null || fail 'Linux assets are missing'
grep -F 'dist/hikyo_*_Windows_*.zip' "$workflow" >/dev/null || fail 'Windows assets are missing'
grep -F '"!v*-nightly.*"' "$official" >/dev/null || fail 'official ceremony still accepts nightly tags'
grep -F 'index .Env "HIKYO_RELEASE_VERSION"' "$goreleaser" >/dev/null || fail 'snapshot archive version is not injectable'
grep -F 'release/repository/nightly-tag-creation.json' "$repo_root/scripts/release/configure-repository.sh" >/dev/null || fail 'nightly tag creation policy is not applied'
jq -e '
	.conditions.ref_name.include == ["refs/tags/v*-nightly.*"] and
	.bypass_actors == [{actor_id: 15368, actor_type: "Integration", bypass_mode: "always"}]
' "$repo_root/release/repository/nightly-tag-creation.json" >/dev/null || fail 'nightly tag creation is not restricted to GitHub Actions'
jq -e '.conditions.ref_name.exclude == ["refs/tags/v*-nightly.*"]' \
	"$repo_root/release/repository/release-tag-creation.json" >/dev/null || fail 'stable creation policy still captures nightlies'

if grep -E 'sign-bundle|bind-manifest|ceremony\.sh|COSIGN_PASSWORD|\.key\.age' "$workflow" >/dev/null; then
	fail 'nightly workflow reaches offline signing ceremony material'
fi

"$repo_root/scripts/release/next-nightly-version_test.sh"
"$repo_root/scripts/release/latest-stable-version_test.sh"
"$repo_root/scripts/release/require-green-main_test.sh"
printf 'nightly release fixture: CI-gated six-platform prereleases stay outside official signing\n'
