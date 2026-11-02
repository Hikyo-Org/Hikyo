#!/usr/bin/env bash
# Download and authenticate all release bytes before exposing an executable.
set -euo pipefail
[[ $# == 4 ]] || { echo 'usage: verify-release.sh REPOSITORY TAG COMMIT WORK' >&2; exit 2; }
repository=$1 tag=$2 commit=$3 work=$4
[[ "$repository" == "$GITHUB_REPOSITORY" ]]
[[ "$commit" =~ ^[0-9a-f]{40}$ ]]
[[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z][0-9A-Za-z.-]*)?$ ]]
mkdir -m 700 "$work"
mkdir "$work/bundle"
gh api "repos/$repository/releases/tags/$tag" >"$work/release.json"
jq -e --arg tag "$tag" '.tag_name == $tag and .draft == false and .published_at != null' "$work/release.json" >/dev/null
# Reject tag movement and unpublished data, independently of release metadata.
test "$(git rev-parse "refs/tags/$tag^{commit}")" = "$commit"
git merge-base --is-ancestor "$commit" HEAD
gh release download "$tag" --repo "$repository" --dir "$work/bundle"
case "$tag" in
  *-nightly.*)
    go run ./scripts/release/nightly verify --trust release/trust \
      --directory "$work/bundle" >"$work/identity.json"
    ;;
  *)
    go run ./scripts/release/stable verify --trust release/trust \
      --directory "$work/bundle" --version "${tag#v}" --commit "$commit" \
      --historical --published >"$work/stable-identity.json"
    jq '.identity' "$work/stable-identity.json" >"$work/identity.json"
    ;;
esac
go run ./scripts/security/release-evidence prepare --directory "$work/bundle" \
  --identity "$work/identity.json" --tag "$tag" --source "$commit" \
  --binary "$work/hikyo" --out "$work/prepared.json"
case "$tag" in
  *-nightly.*) ./scripts/release/assemble-nightly.sh release/trust "$work/runtime-bundle" "$work/bundle" ;;
  *) ./scripts/security/assemble-release.sh release/trust "$work/bundle" "$work/runtime-bundle" ;;
esac
