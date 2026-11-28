#!/bin/sh
# Recovery-bridge ceremony steps for one stable release pair (#1422).
#
#   recovery-bridge.sh author      TRUST SOURCE_PROOFS TARGET_PROOFS STATEMENTS [NOT_AFTER]
#   recovery-bridge.sh sign        KEY STATEMENTS
#   recovery-bridge.sh reauthorize KEY TRUST STATEMENTS OUT [REPLACED_DIGEST...]
#   recovery-bridge.sh assemble    TRUST OUT RELEASE_PROOFS...
#   recovery-bridge.sh revoke      KEY TRUST OUT DIGEST...
#   recovery-bridge.sh revocation-list KEY TRUST SEQUENCE OUT DIGEST...   (ISSUED_AT=epoch overrides now, for rehearsals)
#   recovery-bridge.sh check-revocations TRUST STATE LIST_DIR|- [DIGEST...]
#
# author derives the unsigned v2 statements from authenticated public release
# proofs. Each carries a signed not_after, at most 30 days out (default: 29 days
# 23 hours from now; pass NOT_AFTER, canonical UTC RFC 3339, to choose). An
# expired bridge stops planning offline, so a lapsed one is re-authored and
# re-signed with the same steps (the re-sign path; see the runbook). sign and reauthorize use the recovery private key KEY and must run on
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
# sha256_of FILE prints the digest; it never prints an empty one (a catalog or list
# must not be signed over a missing digest), and works with shasum or sha256sum.
sha256_of() {
	if command -v shasum >/dev/null 2>&1; then sum=$(shasum -a 256 "$1") || fail "cannot hash $1"
	elif command -v sha256sum >/dev/null 2>&1; then sum=$(sha256sum "$1") || fail "cannot hash $1"
	else fail 'neither shasum nor sha256sum is installed'
	fi
	sum=${sum%% *}
	case "$sum" in *[!0-9a-f]* | '') fail "no SHA-256 digest for $1" ;; esac
	[ "${#sum}" -eq 64 ] || fail "no SHA-256 digest for $1"
	printf '%s\n' "$sum"
}
# abs PATH resolves a possibly relative path against the current directory, which the
# go run subshells below leave (they cd to the repository root).
abs() { case "$1" in /*) printf '%s\n' "$1" ;; *) printf '%s/%s\n' "$PWD" "$1" ;; esac; }

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
	[ "$#" -eq 4 ] || [ "$#" -eq 5 ] || fail 'usage: author TRUST SOURCE_PROOFS TARGET_PROOFS STATEMENTS [NOT_AFTER]'
	trust=$(abs "$1") source=$(abs "$2") target=$(abs "$3") out=$(abs "$4")
	not_after=${5:-$(jq -nr '(now | floor) + 2592000 - 3600 | todate')}
	recovery=$(jq -r '.recovery.public_key' "$trust/root.json")
	scratch=$(mktemp -d)
	trap 'rm -rf "$scratch"' EXIT
	stage_trust "$trust" "$scratch"
	(cd "$repo_root" && go run ./scripts/release/recovery-bridge author \
		--root "$trust/root.json" --recovery-key "$trust/$recovery" \
		--snapshot "$scratch/snapshot" --keys "$scratch/keys" \
		--source "$source" --target "$target" --not-after "$not_after" --out "$out")
}

# assemble TRUST OUT PROOFS... builds the offline bundle an operator passes as
# HIKYO_UPGRADE_BUNDLE: the given releases, the snapshot and every catalogued bridge.
assemble() {
	[ "$#" -ge 3 ] || fail 'usage: assemble TRUST OUT RELEASE_PROOFS...'
	trust=$(abs "$1") out=$(abs "$2")
	shift 2
	recovery=$(jq -r '.recovery.public_key' "$trust/root.json")
	scratch=$(mktemp -d)
	trap 'rm -rf "$scratch"' EXIT
	stage_trust "$trust" "$scratch"
	for proofs in "$@"; do set -- "$@" --release "$(abs "$proofs")"; shift; done
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
	[ "$#" -ge 4 ] || fail 'usage: reauthorize KEY TRUST STATEMENTS OUT [REPLACED_DIGEST...]'
	key=$1 trust=$2 statements=$3 out=$4
	shift 4
	# REPLACED_DIGEST... are the lapsed bridges the new statements supersede: they leave the
	# catalog and the tree, so a lapsed bridge never shares a pair with its replacement.
	replaced=$(for digest in "$@"; do printf '%s\n' "$digest"; done | jq -R . | jq -s .)
	jq -e --argjson replaced "$replaced" '($replaced - .bridges) | length == 0' "$trust/catalog.json" >/dev/null || fail 'a replaced digest is not in the catalog'
	jq -e --argjson replaced "$replaced" '(.bridges // []) as $listed | ($replaced - ($replaced - $listed)) | length == 0' "$trust/stable-policy.json" >/dev/null ||
		fail 'a replaced digest is listed by the stable policy; that needs a policy rotation'
	[ ! -e "$out" ] || fail "$out already exists"
	# Build beside the target and rename, so a failed signing leaves no partial tree.
	final=$out out=$out.partial
	rm -rf "$out"
	mkdir -p "$out"
	for path in "$trust"/*; do
		name=$(basename "$path")
		case "$name" in
		metadata.json | metadata.sigstore.json | catalog.json | catalog.sigstore.json) ;;
		*) cp -R "$trust/$name" "$out/$name" ;;
		esac
	done
	mkdir -p "$out/bridges"
	for digest in "$@"; do rm -rf "${out:?}/bridges/$digest"; done
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
	# Hash into variables first: a failure inside jq's --arg "$(...)" would only exit that
	# subshell and the script would sign a catalog over an empty digest.
	metadata_sha=$(sha256_of "$out/metadata.json") || exit 1
	policy_sha=$(sha256_of "$out/stable-policy.json") || exit 1
	jq --arg metadata "$metadata_sha" --arg policy "$policy_sha" --argjson added "$added" --argjson replaced "$replaced" \
		'.sequence += 1 | .stable_metadata_sha256 = $metadata | .stable_policy_sha256 = $policy | .bridges = (((.bridges - $replaced) + $added) | unique)' \
		"$trust/catalog.json" >"$out/catalog.json"
	cosign_sign "$key" "$out/catalog.json" "$out/catalog.sigstore.json"
	mv "$out" "$final"
	printf 'new trust tree: %s\n' "$final"
}

# revoke KEY TRUST OUT DIGEST... drops bridges from a recovery-shaped snapshot:
# the next sequences, the digests removed from the catalog, their files removed.
# Refuses digests the stable policy lists (those need a policy rotation).
revoke() {
	[ "$#" -ge 4 ] || fail 'usage: revoke KEY TRUST OUT DIGEST...'
	key=$1 trust=$2 out=$3
	shift 3
	[ ! -e "$out" ] || fail "$out already exists"
	final=$out out=$out.partial
	rm -rf "$out"
	removed=$(for digest in "$@"; do printf '%s\n' "$digest"; done | jq -R . | jq -s .)
	jq -e --argjson removed "$removed" '($removed - .bridges) | length == 0' "$trust/catalog.json" >/dev/null || fail 'a digest is not in the catalog'
	# Only recovery-only bridges can be revoked here: a bridge the stable policy lists
	# is part of the workflow-signed inventory and needs a policy rotation instead.
	jq -e --argjson removed "$removed" '(.bridges // []) as $listed | ($removed - ($removed - $listed)) | length == 0' "$trust/stable-policy.json" >/dev/null ||
		fail 'a digest is listed by the stable policy; revoking it needs a policy rotation'
	cp -R "$trust" "$out"
	for digest in "$@"; do rm -rf "${out:?}/bridges/$digest"; done
	recovery_id=$(jq -r '.recovery.id' "$trust/root.json")
	jq --arg id "$recovery_id" '.sequence += 1 | .event.signed_by = $id | del(.source_commit, .release_tag)' "$trust/metadata.json" >"$out/metadata.json"
	cosign_sign "$key" "$out/metadata.json" "$out/metadata.sigstore.json"
	metadata_sha=$(sha256_of "$out/metadata.json") || exit 1
	jq --arg metadata "$metadata_sha" --argjson removed "$removed" \
		'.sequence += 1 | .stable_metadata_sha256 = $metadata | .bridges -= $removed' "$trust/catalog.json" >"$out/catalog.json"
	cosign_sign "$key" "$out/catalog.json" "$out/catalog.sigstore.json"
	mv "$out" "$final"
	printf 'revoked; new trust tree: %s\n' "$final"
}

# revocation_list KEY TRUST SEQUENCE OUT DIGEST... writes OUT/recovery-revocations.json
# and its signature: the list the bridge guard fetches. It is bound to the pinned
# trust root and signed by the recovery key. SEQUENCE must be the published
# list's sequence plus one (1 for the first list); a client refuses anything
# lower. The list carries a signed issued_at: a client treats a list older than
# 7 days as stale (it falls back to not_after, like an unreachable endpoint, and
# the entries still apply). Sign a list when a bridge is published and whenever
# something is revoked, never on a schedule: freshness buys no security.
# Publish both files at the pinned endpoint.
revocation_list() {
	[ "$#" -ge 4 ] || fail 'usage: revocation-list KEY TRUST SEQUENCE OUT DIGEST...'
	key=$1 trust=$2 sequence=$3 out=$4
	shift 4
	case "$sequence" in '' | *[!0-9]* | 0*) fail 'SEQUENCE must be a positive integer' ;; esac
	[ ! -e "$out" ] || fail "$out already exists"
	revoked=$(for digest in "$@"; do printf '%s\n' "$digest"; done | sort -u | jq -R . | jq -s .)
	root_sha=$(sha256_of "$trust/root.json") || exit 1
	mkdir -p "$out"
	jq -n --argjson sequence "$sequence" --arg issued "$(jq -nr --argjson at "${ISSUED_AT:-$(date +%s)}" '$at | todate')" --arg root "$root_sha" --argjson revoked "$revoked" \
		'{schema: "hikyo.dev/recovery-revocations/v1", sequence: $sequence, issued_at: $issued, trust_root_sha256: $root, revoked: ($revoked | map(select(. != "")))}' \
		>"$out/recovery-revocations.json"
	cosign_sign "$key" "$out/recovery-revocations.json" "$out/recovery-revocations.sigstore.json"
	printf 'revocation list: %s\n' "$out"
}

# check_revocations TRUST STATE LIST_DIR|- DIGEST... runs the apply-time check with
# the real checker against a published list directory ("-" means the endpoint is
# unreachable) and prints which digests it revokes. STATE holds the remembered
# list and sequence floor, as an installation's state directory does.
check_revocations() {
	[ "$#" -ge 3 ] || fail 'usage: check-revocations TRUST STATE LIST_DIR|- [DIGEST...]'
	trust=$(abs "$1") state=$(abs "$2") list=$3
	[ "$list" = - ] || list=$(abs "$list")
	shift 3
	recovery=$(jq -r '.recovery.public_key' "$trust/root.json")
	scratch=$(mktemp -d)
	trap 'rm -rf "$scratch"' EXIT
	stage_trust "$trust" "$scratch"
	if [ "$list" = - ]; then set -- --unreachable "$@"; else set -- --list "$list" "$@"; fi
	(cd "$repo_root" && go run ./scripts/release/recovery-bridge check-revocations \
		--root "$trust/root.json" --recovery-key "$trust/$recovery" \
		--snapshot "$scratch/snapshot" --keys "$scratch/keys" --state "$state" "$@")
}

[ "$#" -ge 1 ] || fail 'usage: recovery-bridge.sh author|sign|reauthorize|assemble|revoke|revocation-list|check-revocations ...'
command=$1
shift
case "$command" in
author | assemble | sign | reauthorize | revoke) "$command" "$@" ;;
revocation-list) revocation_list "$@" ;;
check-revocations) check_revocations "$@" ;;
*) fail "unknown command $command" ;;
esac
