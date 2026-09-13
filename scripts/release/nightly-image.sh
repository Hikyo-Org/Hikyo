#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
[ "$#" -eq 1 ] || { printf 'usage: %s prepare|resolve|promote\n' "$0" >&2; exit 2; }
: "${REPOSITORY:?}" "${VERSION:?}" "${COMMIT:?}" "${TAG:?}" "${GITHUB_OUTPUT:?}"
is_full_sha "$COMMIT"
printf '%s\n' "$VERSION" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+-nightly\.[0-9]{8}\.[1-9][0-9]*\.g[0-9a-f]{8}$'
[ "$TAG" = "v$VERSION" ]
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-image.XXXXXX")
container=
cleanup() {
	if [ -n "$container" ]; then docker rm --force "$container" >/dev/null 2>&1 || true; fi
	rm -rf "$work"
}
trap cleanup EXIT HUP INT TERM
fail() { printf 'nightly image: %s\n' "$1" >&2; exit 1; }
image=$(printf 'ghcr.io/%s' "$REPOSITORY" | tr '[:upper:]' '[:lower:]')
[ "${IMAGE:-$image}" = "$image" ] || fail 'unexpected image repository'

case "$1" in
prepare)
	# Release verification authenticates the entire inventory before extraction.
	gh api "repos/$REPOSITORY/releases/tags/$TAG" >"$work/release.json"
	jq -e --arg tag "$TAG" '.tag_name == $tag and .draft == false and .prerelease == true' "$work/release.json" >/dev/null
	[ "$(gh api "repos/$REPOSITORY/git/ref/tags/$TAG" --jq '.object.sha')" = "$COMMIT" ] || fail 'release tag commit mismatch'
	gh release download "$TAG" --repo "$REPOSITORY" --dir "$work/payloads"
	go run ./scripts/release/nightly verify --directory "$work/payloads"
	jq -e --arg version "$VERSION" --arg commit "$COMMIT" '.version == $version and .source_commit == $commit' "$work/payloads/release-manifest.json" >/dev/null
	validate_binary_provenance "$work/payloads/binary-provenance.json" "$COMMIT" "$VERSION"
	[ ! -e image-root ] || fail 'image-root must be absent'
	for arch in amd64 arm64; do
		name=$arch
		[ "$arch" != amd64 ] || name=x86_64
		mkdir -p "image-root/$arch"
		# Stream only this member, never extract archive paths or links to disk.
		tar -xOzf "$work/payloads/hikyo_${VERSION}_Linux_${name}.tar.gz" hikyo >"image-root/$arch/hikyo"
		want=$(jq -r --arg arch "$arch" '.packages[] | select(.goarch == $arch) | .archive_input.sha256' "$work/payloads/binary-provenance.json")
		[ "$(sha256_file "image-root/$arch/hikyo")" = "$want" ] || fail "linux/$arch binary provenance mismatch"
		chmod 755 "image-root/$arch/hikyo"
	done
	printf 'image=%s\n' "$image" >>"$GITHUB_OUTPUT"
	;;
resolve)
	if docker buildx imagetools inspect "$image:$VERSION" --format '{{json .Manifest}}' >"$work/manifest.json" 2>"$work/error"; then
		digest=$(jq -er '.digest' "$work/manifest.json")
		is_digest "$digest"
		printf 'exists=true\ndigest=%s\n' "$digest" >>"$GITHUB_OUTPUT"
	elif ! grep -Eiq 'unauthorized|denied|forbidden|timeout|deadline exceeded' "$work/error" &&
		{ grep -Eiq 'manifest unknown|no such manifest|unexpected status.*404 Not Found' "$work/error" ||
			grep -Fx "ERROR: $image:$VERSION: not found" "$work/error" >/dev/null; }; then
		printf 'exists=false\n' >>"$GITHUB_OUTPUT"
	else
		cat "$work/error" >&2
		fail 'cannot prove immutable image is absent'
	fi
	;;
promote)
	: "${IMAGE_DIGEST:?}"
	is_digest "$IMAGE_DIGEST"
	ref=$image@$IMAGE_DIGEST
	# Apply the same checks to a fresh push and an interrupted earlier push.
	docker buildx imagetools inspect "$ref" --raw >"$work/index.json"
	jq -e '([.manifests[].platform | .os + "/" + .architecture] | sort) == ["linux/amd64", "linux/arm64"]' "$work/index.json" >/dev/null
	for arch in amd64 arm64; do
		child_digest=$(jq -er --arg arch "$arch" '.manifests[] | select(.platform.architecture == $arch) | .digest' "$work/index.json")
		is_digest "$child_digest"
		child_ref=$image@$child_digest
		docker pull --platform "linux/$arch" "$child_ref"
		docker image inspect "$child_ref" >"$work/config.json"
		jq -e --arg arch "$arch" --arg version "$VERSION" --arg commit "$COMMIT" --arg source "https://github.com/$REPOSITORY" '
			length == 1 and all(.[]; .Os == "linux" and .Architecture == $arch and
			.Config.User == "65532:65532" and .Config.Entrypoint == ["/usr/local/bin/hikyo"] and
			.Config.Cmd == ["server"] and .Config.Labels["org.opencontainers.image.source"] == $source and
			.Config.Labels["org.opencontainers.image.version"] == $version and
			.Config.Labels["org.opencontainers.image.revision"] == $commit)
		' "$work/config.json" >/dev/null
		container=$(docker create --platform "linux/$arch" "$child_ref")
		docker cp "$container:/usr/local/bin/hikyo" "$work/hikyo"
		cmp "image-root/$arch/hikyo" "$work/hikyo" || fail "published linux/$arch binary differs"
		docker rm "$container" >/dev/null
		container=
	done
	# Restore host architecture after checking arm64 without executing it.
	docker pull --platform linux/amd64 "$ref"
	"$script_dir/smoke-image-ui.sh" "$ref"
	cosign sign --yes --use-signing-config=false --rekor-url=https://rekor.sigstore.dev "$ref"
	cosign verify --certificate-identity "https://github.com/$REPOSITORY/.github/workflows/nightly.yml@refs/heads/main" \
		--certificate-oidc-issuer https://token.actions.githubusercontent.com "$ref" >"$work/signatures.json"
	printf 'digest=%s\n' "$IMAGE_DIGEST" >>"$GITHUB_OUTPUT"
	if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
		printf '### Verified nightly container\n\nImage: %s:%s\n\nDigest: %s\n\n' \
			"$image" "$VERSION" "$IMAGE_DIGEST" >>"$GITHUB_STEP_SUMMARY"
	fi
	# Workflow concurrency serializes promotion. An old rerun must never roll back nightly.
	gh api --paginate --slurp "repos/$REPOSITORY/releases?per_page=100" >"$work/releases.json"
	latest=$(jq -er '
		[.[][] | select(.draft == false and .prerelease == true) |
		 .tag_name | . as $tag | try (capture("^v[0-9]+\\.[0-9]+\\.[0-9]+-nightly\\.(?<date>[0-9]{8})\\.(?<sequence>[1-9][0-9]*)\\.g[0-9a-f]{8}$") | .tag = $tag)] |
		sort_by([(.date | tonumber), (.sequence | tonumber)]) | last.tag // empty
	' "$work/releases.json")
	[ -n "$latest" ] || fail 'no published nightly release remains'
	if [ "$latest" != "$TAG" ]; then
		printf 'nightly image: %s verified; newer release %s keeps moving tag\n' "$ref" "$latest"
		exit 0
	fi
	docker buildx imagetools create --tag "$image:nightly" "$ref"
	docker buildx imagetools inspect "$image:nightly" --format '{{json .Manifest}}' >"$work/promoted.json"
	[ "$(jq -er '.digest' "$work/promoted.json")" = "$IMAGE_DIGEST" ] || fail 'moving tag digest mismatch'
	printf 'nightly image: %s and %s:nightly verified\n' "$image:$VERSION" "$image"
	;;
*) fail 'unknown operation' ;;
esac
