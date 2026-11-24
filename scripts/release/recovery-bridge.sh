#!/bin/sh
# Recovery-bridge ceremony steps for one stable release pair (#1422).
#
#   recovery-bridge.sh author      TRUST SOURCE_PROOFS TARGET_PROOFS STATEMENTS
#   recovery-bridge.sh sign        KEY STATEMENTS
#   recovery-bridge.sh reauthorize KEY TRUST STATEMENTS OUT
#   recovery-bridge.sh assemble    TRUST OUT RELEASE_PROOFS...
#   recovery-bridge.sh revoke      KEY TRUST OUT DIGEST...
#
# author derives the unsigned statements from authenticated public release
# proofs. sign and reauthorize use the recovery private key KEY and must run on
# the offline signing machine; they never print or copy it. reauthorize writes a
# new trust tree and leaves TRUST untouched. stable-policy.json is deliberately
# never re-signed: a statement binds the stable policy digest, so a policy that
# listed the statements would change that digest (see
# docs/operations/recovery-bridge-ceremony.md).
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
: "${COSIGN_BIN:=cosign}"
fail() { printf 'recovery-bridge: %s\n' "$*" >&2; exit 1; }
sha256_of() { shasum -a 256 "$1" | cut -d' ' -f1; }

cosign_sign() { # KEY FILE BUNDLE
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false \
		--use-signing-config=false --key "$1" --bundle "$3" "$2" >/dev/null
}

# stage_trust TRUST SCRATCH copies the files assemble-upgrade and the authoring
# tool accept (nothing else) into SCRATCH/snapshot and SCRATCH/keys.
stage_trust() {
	mkdir "$2/snapshot" "$2/keys"
	for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json stable-policy.json stable-policy.sigstore.json stable-trusted-root.json; do
		cp "$1/$name" "$2/snapshot/$name"
	done
	for name in $(jq -r '.primary_keys[].public_key' "$1/metadata.json"); do
		cp "$1/$name" "$2/keys/$name"
	done
}

author() {
	[ "$#" -eq 4 ] || fail 'usage: author TRUST SOURCE_PROOFS TARGET_PROOFS STATEMENTS'
	trust=$1 source=$2 target=$3 out=$4
	recovery=$(jq -r '.recovery.public_key' "$trust/root.json")
	scratch=$(mktemp -d)
	trap 'rm -rf "$scratch"' EXIT
	stage_trust "$trust" "$scratch"
	(cd "$repo_root" && go run ./scripts/release/recovery-bridge author \
		--root "$trust/root.json" --recovery-key "$trust/$recovery" \
		--snapshot "$scratch/snapshot" --keys "$scratch/keys" \
		--source "$source" --target "$target" --out "$out")
}

# assemble TRUST OUT PROOFS... builds the offline bundle an operator passes as
# HIKYO_UPGRADE_BUNDLE: the given releases, the snapshot and every catalogued bridge.
assemble() {
	[ "$#" -ge 3 ] || fail 'usage: assemble TRUST OUT RELEASE_PROOFS...'
	trust=$1 out=$2
	shift 2
	recovery=$(jq -r '.recovery.public_key' "$trust/root.json")
	scratch=$(mktemp -d)
	trap 'rm -rf "$scratch"' EXIT
	stage_trust "$trust" "$scratch"
	for proofs in "$@"; do set -- "$@" --release "$proofs"; shift; done
	for digest in $(jq -r '.bridges[]' "$trust/catalog.json"); do set -- "$@" --bridge "$trust/bridges/$digest"; done
	(cd "$repo_root" && go run ./scripts/release/assemble-upgrade \
		--root "$trust/root.json" --recovery-key "$trust/$recovery" \
		--snapshot "$scratch/snapshot" --keys "$scratch/keys" --out "$out" "$@")
}

sign() {
	[ "$#" -eq 2 ] || fail 'usage: sign KEY STATEMENTS'
	key=$1 statements=$2
	for directory in "$statements"/*/; do
		[ -f "${directory}statement.json" ] || fail "no statement.json in $directory"
		cosign_sign "$key" "${directory}statement.json" "${directory}statement.sigstore.json"
		printf 'signed %s\n' "$(basename "$directory")"
	done
}

reauthorize() {
	[ "$#" -eq 4 ] || fail 'usage: reauthorize KEY TRUST STATEMENTS OUT'
	key=$1 trust=$2 statements=$3 out=$4
	[ ! -e "$out" ] || fail "$out already exists"
	mkdir -p "$out"
	for path in "$trust"/*; do
		name=$(basename "$path")
		case "$name" in
		metadata.json | metadata.sigstore.json | catalog.json | catalog.sigstore.json) ;;
		*) cp -R "$trust/$name" "$out/$name" ;;
		esac
	done
	mkdir -p "$out/bridges"
	recovery_id=$(jq -r '.recovery.id' "$trust/root.json")
	digests=
	for directory in "$statements"/*/; do
		digest=$(basename "$directory")
		[ "$(sha256_of "${directory}statement.json")" = "$digest" ] || fail "$digest: directory name is not the statement digest"
		[ -f "${directory}statement.sigstore.json" ] || fail "$digest: unsigned"
		mkdir -p "$out/bridges/$digest"
		cp "${directory}statement.json" "${directory}statement.sigstore.json" "$out/bridges/$digest/"
		digests="$digests $digest"
	done
	# Recovery-shaped metadata: the next sequence, no delegated identity, every other field kept.
	jq --arg id "$recovery_id" '.sequence += 1 | .event.signed_by = $id | del(.source_commit, .release_tag)' \
		"$trust/metadata.json" >"$out/metadata.json"
	cosign_sign "$key" "$out/metadata.json" "$out/metadata.sigstore.json"
	# The catalog lists the bridges; stable-policy.json (and its digest) stay as they were.
	added=$(for digest in $digests; do printf '%s\n' "$digest"; done | jq -R . | jq -s .)
	jq --arg metadata "$(sha256_of "$out/metadata.json")" --arg policy "$(sha256_of "$out/stable-policy.json")" --argjson added "$added" \
		'.sequence += 1 | .stable_metadata_sha256 = $metadata | .stable_policy_sha256 = $policy | .bridges = ((.bridges + $added) | unique)' \
		"$trust/catalog.json" >"$out/catalog.json"
	cosign_sign "$key" "$out/catalog.json" "$out/catalog.sigstore.json"
	printf 'new trust tree: %s\n' "$out"
}

# revoke KEY TRUST OUT DIGEST... drops bridges from a recovery-shaped snapshot:
# the next sequences, the digests removed from the catalog, their files removed.
revoke() {
	[ "$#" -ge 4 ] || fail 'usage: revoke KEY TRUST OUT DIGEST...'
	key=$1 trust=$2 out=$3
	shift 3
	[ ! -e "$out" ] || fail "$out already exists"
	removed=$(for digest in "$@"; do printf '%s\n' "$digest"; done | jq -R . | jq -s .)
	jq -e --argjson removed "$removed" '($removed - .bridges) | length == 0' "$trust/catalog.json" >/dev/null || fail 'a digest is not in the catalog'
	cp -R "$trust" "$out"
	for digest in "$@"; do rm -rf "${out:?}/bridges/$digest"; done
	recovery_id=$(jq -r '.recovery.id' "$trust/root.json")
	jq --arg id "$recovery_id" '.sequence += 1 | .event.signed_by = $id | del(.source_commit, .release_tag)' "$trust/metadata.json" >"$out/metadata.json"
	cosign_sign "$key" "$out/metadata.json" "$out/metadata.sigstore.json"
	jq --arg metadata "$(sha256_of "$out/metadata.json")" --argjson removed "$removed" \
		'.sequence += 1 | .stable_metadata_sha256 = $metadata | .bridges -= $removed' "$trust/catalog.json" >"$out/catalog.json"
	cosign_sign "$key" "$out/catalog.json" "$out/catalog.sigstore.json"
	printf 'revoked; new trust tree: %s\n' "$out"
}

[ "$#" -ge 1 ] || fail 'usage: recovery-bridge.sh author|sign|reauthorize|assemble|revoke ...'
command=$1
shift
case "$command" in
author | assemble | sign | reauthorize | revoke) "$command" "$@" ;;
*) fail "unknown command $command" ;;
esac
