#!/bin/sh
# Called only by the tag build job. No persistent Cosign private key is required.
set -eu
[ "$#" -eq 2 ] || { printf 'usage: %s TRUST DIRECTORY\n' "$0" >&2; exit 2; }
trust=$1 directory=$2
: "${COSIGN_BIN:=cosign}"
: "${GITHUB_ACTIONS:?}" "${GITHUB_WORKFLOW_REF:?}" "${GITHUB_REPOSITORY:?}" "${GITHUB_REF:?}" "${GITHUB_SHA:?}"
[ "$GITHUB_ACTIONS" = true ] || exit 1
[ "$GITHUB_WORKFLOW_REF" = "$GITHUB_REPOSITORY/.github/workflows/release.yml@$GITHUB_REF" ] || exit 1
version=$(jq -er '.version' "$directory/release-manifest.json")
commit=$(jq -er '.source_commit' "$directory/release-manifest.json")
[ "$GITHUB_REF" = "refs/tags/v$version" ] && [ "$commit" = "$GITHUB_SHA" ] || exit 1
go run ./scripts/release/stable prepare --trust "$trust" --directory "$directory"
# Every artifact, including provenance, has its own transparency-backed bundle.
# The recovery signature on the delegation policy must never be overwritten.
for file in "$directory"/*; do
  [ -f "$file" ] && [ ! -L "$file" ] || { printf 'stable signing: regular files required\n' >&2; exit 1; }
  case "$file" in */stable-policy.json | *.sigstore.json) continue ;; esac
  case "$file" in
    */release-manifest.json | */metadata.json | */catalog.json) signature="${file%.json}.sigstore.json" ;;
    *) signature="$file.sigstore.json" ;;
  esac
  [ ! -e "$signature" ] || { printf 'stable signing: existing signature refused\n' >&2; exit 1; }
  "$COSIGN_BIN" sign-blob --yes --use-signing-config=false --new-bundle-format=true \
    --rekor-url=https://rekor.sigstore.dev --trusted-root="$trust/stable-trusted-root.json" \
    --bundle "$signature" "$file"
done
for subject in image chart; do
  if [ "$subject" = image ]; then
    reference=$(jq -er '.artifacts[] | select(.kind == "image") | .image+"@"+.digest' "$directory/release-manifest.json")
  else
    reference=$(jq -er '.artifacts[] | select(.kind == "chart-digest") | .chart+"@"+.digest' "$directory/release-manifest.json")
  fi
  "$COSIGN_BIN" sign --yes --use-signing-config=false \
    --rekor-url=https://rekor.sigstore.dev --trusted-root="$trust/stable-trusted-root.json" "$reference"
done
go run ./scripts/release/stable verify --trust "$trust" --directory "$directory" --version "$version" --commit "$commit" --published
