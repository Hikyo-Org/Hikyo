#!/bin/sh
# GitHub expressions below are literal fixture text.
# shellcheck disable=SC2016
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
ci_workflow="$repo_root/.github/workflows/ci.yml"
release_workflow="$repo_root/.github/workflows/release.yml"
dockerfile="$repo_root/Dockerfile.release"
smoke_script="$repo_root/scripts/release/smoke-image-ui.sh"

fail() {
	printf 'release binary reuse fixture failed: %s\n' "$1" >&2
	exit 1
}

require_text() {
	text=$1
	expected=$2
	printf '%s\n' "$text" | grep -F -- "$expected" >/dev/null || fail "missing $expected"
}

release_block=$(sed -n '/args: release --clean --skip=publish/,/uses: docker\/setup-buildx-action/p' "$release_workflow")
release_image_block=$(sed -n '/name: Publish unsigned multi-arch image/,/name: Published image SBOM/p' "$release_workflow")
snapshot_block=$(sed -n '/^  release-snapshot:/,/^  generated:/p' "$ci_workflow")
[ -n "$release_block" ] || fail 'release packaging block is missing'
[ -n "$release_image_block" ] || fail 'release image block is missing'
[ -n "$snapshot_block" ] || fail 'release snapshot job is missing'

require_text "$release_block" 'args: release --clean --skip=publish'
require_text "$release_block" './scripts/release/prepare-image-root.sh'
require_text "$release_block" './scripts/release/check-candidate.sh "$CANDIDATE" "$CANDIDATE_SHA256"'
require_text "$release_block" 'commit=$(jq -r '\''.commit'\'' "$CANDIDATE")'
require_text "$release_block" 'dist image-root "$commit" .goreleaser.yaml'
if printf '%s\n' "$release_block" | grep -E 'go build|CGO_ENABLED=|GOARCH=' >/dev/null; then
	fail 'release workflow still has a second Go build path'
fi

require_text "$snapshot_block" './scripts/release/prepare-image-root.sh'
require_text "$snapshot_block" 'name: Smoke the exact candidate image'
require_text "$snapshot_block" 'image_id=$(docker image inspect --format '\''{{.Id}}'\'' "$image")'
require_text "$snapshot_block" 'docker run --rm "$image_id" version'
require_text "$snapshot_block" './scripts/release/smoke-image-ui.sh "$image_id"'
require_text "$release_image_block" 'name: Smoke the published candidate image'
require_text "$release_image_block" './scripts/release/smoke-image-ui.sh "$IMAGE@$IMAGE_DIGEST"'
require_text "$(cat "$dockerfile")" 'image-root/${TARGETARCH}/hikyo'
require_text "$(cat "$smoke_script")" 'Accept: text/html'
require_text "$(cat "$smoke_script")" '^/assets/[^/?#]+-[A-Za-z0-9_-]{8,}\.js$'
require_text "$(cat "$smoke_script")" '^content-type: text/javascript'

printf 'release binary reuse fixture: archives and OCI images share GoReleaser outputs; candidate images serve the SPA\n'
