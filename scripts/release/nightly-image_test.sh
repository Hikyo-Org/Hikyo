#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-image-test.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
mkdir -p "$work/bin" "$work/scripts/release" "$work/scripts/lib" "$work/assets"
cp "$script_dir/nightly-image.sh" "$work/scripts/release/"
cp "$script_dir/../lib/release.sh" "$work/scripts/lib/"
export REPOSITORY=Hikyo-Org/Hikyo COMMIT=1234567812345678123456781234567812345678
export VERSION=0.0.1-nightly.20260913.39.g12345678
TAG=v$VERSION
export TAG
export FIXTURE="$work" GITHUB_OUTPUT="$work/output" IMAGE_DIGEST=sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
export PATH="$work/bin:$PATH"
cat >"$work/bin/go" <<'MOCK'
#!/bin/sh
set -eu
printf 'go %s\n' "$*" >>"$FIXTURE/log"
[ "$*" = "run ./scripts/release/nightly verify --directory $5" ]
[ "${FAIL_VERIFY:-false}" = false ]
MOCK
cat >"$work/bin/gh" <<'MOCK'
#!/bin/sh
set -eu
printf 'gh %s\n' "$*" >>"$FIXTURE/log"
case "$1 $2" in
"release download")
	[ "$3" = "$TAG" ] && [ "$4" = --repo ] && [ "$5" = "$REPOSITORY" ] && [ "$6" = --dir ]
	mkdir "$7"
	cp "$FIXTURE/assets/"* "$7/"
	;;
"api --paginate") cat "$FIXTURE/releases.json" ;;
*)
	case "$2" in
	*/git/ref/tags/*) printf '%s\n' "${TAG_COMMIT:-$COMMIT}" ;;
	*/releases/tags/*) jq -n --arg tag "$TAG" '{tag_name:$tag,draft:false,prerelease:true}' ;;
	*) exit 91 ;;
	esac
	;;
esac
MOCK
cat >"$work/bin/docker" <<'MOCK'
#!/bin/sh
set -eu
printf 'docker %s\n' "$*" >>"$FIXTURE/log"
case "$1 $2" in
"buildx imagetools")
	case "$3" in
	inspect)
		if [ "$4" = "ghcr.io/hikyo-org/hikyo:$VERSION" ]; then
			case "${LOOKUP:-existing}" in
			missing) echo 'manifest unknown' >&2; exit 1 ;;
			missing-buildx) echo "ERROR: ghcr.io/hikyo-org/hikyo:$VERSION: not found" >&2; exit 1 ;;
			denied) echo 'denied: unauthorized' >&2; exit 1 ;;
			network) echo 'network not found' >&2; exit 1 ;;
			esac
		fi
		if [ "$5" = --raw ]; then
			jq -n --arg arm "${ARM_ARCH:-arm64}" '{manifests:[{digest:("sha256:"+("b"*64)),platform:{os:"linux",architecture:"amd64"}},{digest:("sha256:"+("c"*64)),platform:{os:"linux",architecture:$arm}}]}'
		else
			jq -n --arg digest "$IMAGE_DIGEST" '{digest:$digest}'
		fi
		;;
	create) [ "$4" = --tag ] && [ "$5" = ghcr.io/hikyo-org/hikyo:nightly ] && [ "$6" = "ghcr.io/hikyo-org/hikyo@$IMAGE_DIGEST" ] ;;
	*) exit 92 ;;
	esac
	;;
"pull --platform") printf '%s\n' "${3#linux/}" >"$FIXTURE/arch" ;;
"image inspect")
	case "$(cat "$FIXTURE/arch")" in
	amd64) expected=sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb ;;
	arm64) expected=sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc ;;
	esac
	[ "$3" = "ghcr.io/hikyo-org/hikyo@$expected" ]
	jq -n --arg arch "$(cat "$FIXTURE/arch")" --arg version "${IMAGE_VERSION:-$VERSION}" --arg commit "$COMMIT" --arg source "https://github.com/$REPOSITORY" '
	[{Os:"linux",Architecture:$arch,Config:{User:"65532:65532",Entrypoint:["/usr/local/bin/hikyo"],Cmd:["server"],Labels:{"org.opencontainers.image.source":$source,"org.opencontainers.image.revision":$commit,"org.opencontainers.image.version":$version}}}]'
	;;
"create --platform") printf 'test-container\n' ;;
"cp test-container:/usr/local/bin/hikyo")
	if [ "${WRONG_BINARY:-false}" = true ]; then printf bad >"$3"; else cp "$FIXTURE/binary-$(cat "$FIXTURE/arch")" "$3"; fi
	;;
"rm test-container" | "rm --force") ;;
*) exit 93 ;;
esac
MOCK
cat >"$work/bin/cosign" <<'MOCK'
#!/bin/sh
set -eu
printf 'cosign %s\n' "$*" >>"$FIXTURE/log"
case "$1" in
sign)
	[ "$2" = --yes ] && [ "$3" = --use-signing-config=false ] && [ "$4" = --rekor-url=https://rekor.sigstore.dev ]
	[ "$5" = "ghcr.io/hikyo-org/hikyo@$IMAGE_DIGEST" ]
	[ "${FAIL_SIGN:-false}" = false ]
	;;
verify)
	[ "$2" = --certificate-identity ] && [ "$3" = "https://github.com/$REPOSITORY/.github/workflows/nightly.yml@refs/heads/main" ]
	[ "$4" = --certificate-oidc-issuer ] && [ "$5" = https://token.actions.githubusercontent.com ]
	[ "$6" = "ghcr.io/hikyo-org/hikyo@$IMAGE_DIGEST" ]
	[ "${FAIL_SIGNATURE:-false}" = false ]
	printf '[]\n'
	;;
*) exit 94 ;;
esac
MOCK
cat >"$work/scripts/release/smoke-image-ui.sh" <<'MOCK'
#!/bin/sh
set -eu
printf 'smoke %s\n' "$*" >>"$FIXTURE/log"
[ "$1" = "ghcr.io/hikyo-org/hikyo@$IMAGE_DIGEST" ]
[ "${FAIL_SMOKE:-false}" = false ]
MOCK
chmod +x "$work/bin/"* "$work/scripts/release/"*.sh
for arch in amd64 arm64; do
	printf 'canonical binary %s\n' "$arch" >"$work/binary-$arch"
	mkdir "$work/archive-$arch"
	cp "$work/binary-$arch" "$work/archive-$arch/hikyo"
	name=$arch
	[ "$arch" != amd64 ] || name=x86_64
	tar -czf "$work/assets/hikyo_${VERSION}_Linux_${name}.tar.gz" -C "$work/archive-$arch" hikyo
done
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
jq -n --arg version "$VERSION" --arg commit "$COMMIT" \
	--arg amd "$(sha256_file "$work/binary-amd64")" --arg arm "$(sha256_file "$work/binary-arm64")" '
{schema:"hikyo.dev/release-binaries/v1",version:$version,source_commit:$commit,
producer:{name:"goreleaser",build_id:"hikyo",config:".goreleaser.yaml",config_sha256:("a"*64)},
packages:(["amd64","arm64"] | map(. as $arch | (if $arch == "amd64" then $amd else $arm end) as $sha |
{goos:"linux",goarch:$arch,archive_input:{build_id:"hikyo",sha256:$sha},oci_input:{path:("image-root/"+$arch+"/hikyo"),sha256:$sha}}))}
' >"$work/assets/binary-provenance.json"
jq -n --arg version "$VERSION" --arg commit "$COMMIT" '{version:$version,source_commit:$commit}' >"$work/assets/release-manifest.json"
jq -n --arg tag "$TAG" '[[{tag_name:$tag,draft:false,prerelease:true}]]' >"$work/releases.json"
cd "$work"
run() { "$work/scripts/release/nightly-image.sh" "$@"; }
reject() {
	if run "$@" >"$work/rejected.log" 2>&1; then printf 'unexpected success: %s\n' "$*" >&2; exit 1; fi
}
not_promoted() { if grep -F 'imagetools create' "$work/log" >/dev/null; then echo 'unexpected moving tag mutation' >&2; exit 1; fi; }
( FAIL_VERIFY=true reject prepare )
[ ! -e image-root ]
( TAG_COMMIT=0000000000000000000000000000000000000000 reject prepare )
[ ! -e image-root ]
run prepare
cmp image-root/amd64/hikyo "$work/binary-amd64"
cmp image-root/arm64/hikyo "$work/binary-arm64"
rm -rf image-root
cp "$work/assets/binary-provenance.json" "$work/provenance-good"
jq '.packages[0].archive_input.sha256 = ("b"*64) | .packages[0].oci_input.sha256 = ("b"*64)' "$work/provenance-good" >"$work/assets/binary-provenance.json"
reject prepare
rm -rf image-root
cp "$work/provenance-good" "$work/assets/binary-provenance.json"
run prepare
: >"$GITHUB_OUTPUT"
( LOOKUP=missing run resolve )
( LOOKUP=missing-buildx run resolve )
grep -Fx 'exists=false' "$GITHUB_OUTPUT"
( LOOKUP=denied reject resolve )
( LOOKUP=network reject resolve )
: >"$GITHUB_OUTPUT"
run resolve
grep -Fx 'exists=true' "$GITHUB_OUTPUT"
grep -Fx "digest=$IMAGE_DIGEST" "$GITHUB_OUTPUT"
for failure in ARM_ARCH IMAGE_VERSION WRONG_BINARY FAIL_SMOKE FAIL_SIGN FAIL_SIGNATURE; do
	: >"$work/log"
	case "$failure" in
	ARM_ARCH) ( ARM_ARCH=amd64 reject promote ) ;;
	IMAGE_VERSION) ( IMAGE_VERSION=wrong reject promote ) ;;
	WRONG_BINARY) ( WRONG_BINARY=true reject promote ) ;;
	FAIL_SMOKE) ( FAIL_SMOKE=true reject promote ) ;;
	FAIL_SIGN) ( FAIL_SIGN=true reject promote ) ;;
	FAIL_SIGNATURE) ( FAIL_SIGNATURE=true reject promote ) ;;
	esac
	not_promoted
done
: >"$work/log"
run promote
grep -F 'imagetools create --tag ghcr.io/hikyo-org/hikyo:nightly' "$work/log"
# An interrupted run reuses immutable digest and completes promotion.
run resolve
run promote
# API ordering cannot make a republished old release downgrade the moving tag.
jq -n --arg tag "$TAG" '[[{tag_name:$tag,draft:false,prerelease:true},{tag_name:"v0.0.1-nightly.20260914.40.g12345678",draft:false,prerelease:true}]]' >"$work/releases.json"
: >"$work/log"
run promote
not_promoted
printf 'nightly image fixtures passed\n'
