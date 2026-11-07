#!/usr/bin/env bash
# Fixture: the Pi rehearsal's cleanup claims exactly the image references its own archives created.
# ctr names an import by the digest of the archive's index.json, which is not a blob, and the CRI adds
# bare sha256: image IDs; a ref may also resolve to one of the archives' blobs. A ref pointing anywhere
# else belongs to someone else on the shared node.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=scripts/ci/security-pi-rehearsal.sh
source "$script_dir/security-pi-rehearsal.sh"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
sha() { python3 -c 'import hashlib, sys; print(hashlib.sha256(sys.stdin.buffer.read()).hexdigest())'; }
for label in a b c; do
	mkdir -p "$WORK/oci-$label/blobs/sha256"
	: >"$WORK/oci-$label/blobs/sha256/$(sha <<<"layer $label")"
	printf '{"schemaVersion":2,"label":"%s"}' "$label" >"$WORK/oci-$label/index.json"
	tar -C "$WORK/oci-$label" -cf "$WORK/image-$label.tar" .
done
cp "$WORK/image-a.tar" "$WORK/standin.tar"
blob_a=$(sha <<<"layer a")
index_b=$(sha <"$WORK/oci-b/index.json")
blob_c=$(sha <<<"layer c") # stands in for image c's manifest blob
other=$(printf 'f%.0s' {1..64})

# ctr on the node: its image list, and the target of each ref the claim has to resolve.
ctr() {
	case "$*" in
	'images ls -q') printf '%s\n' "${NODE_IMAGES[@]}" ;;
	"images ls name==sha256:$blob_a") printf 'REF TYPE DIGEST\nx index sha256:%s\n' "$index_b" ;;
	"images ls name==import-2026-11-05@sha256:$index_b") printf 'REF TYPE DIGEST\nx index sha256:%s\n' "$index_b" ;;
	"images ls name==import-2026-11-05@sha256:$blob_c") printf 'REF TYPE DIGEST\nx manifest sha256:%s\n' "$blob_c" ;;
	"images ls name==import-2026-10-01@sha256:$other") printf 'REF TYPE DIGEST\nx index sha256:%s\n' "$other" ;;
	"images ls name==sha256:$other") return 1 ;;
	*) echo "unexpected ctr $*" >&2; return 1 ;;
	esac
}
NODE_IMAGES=(docker.io/library/busybox:1)
ctr images ls -q | sort >"$WORK/images-before"
archive_digests >"$WORK/imported-blobs"

NODE_IMAGES=(docker.io/library/busybox:1 "import-2026-11-05@sha256:$index_b" "sha256:$blob_a" "import-2026-10-01@sha256:$other" "sha256:$other" "import-2026-11-05@sha256:$blob_c" ghcr.io/hikyo-1068-pi/hikyo:1)
got=$(added_images | rehearsal_images)
want=$(printf '%s\n' ghcr.io/hikyo-1068-pi/hikyo:1 "import-2026-11-05@sha256:$index_b" "sha256:$blob_a" "import-2026-11-05@sha256:$blob_c" | sort)
[ "$(sort <<<"$got")" = "$want" ] || { printf 'FAIL: claimed\n%s\nwant\n%s\n' "$got" "$want" >&2; exit 1; }
[ "$(cat "$WORK/unresolved-images")" = "sha256:$other" ] || { echo 'FAIL: an unresolvable ref was not recorded' >&2; exit 1; }
echo 'security-pi-rehearsal_test: ok'
