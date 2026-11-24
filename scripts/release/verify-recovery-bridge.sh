#!/bin/sh
# Independent verifier for a recovery-bridge trust proposal (#1422).
#
#   verify-recovery-bridge.sh unsigned   BASE_TRUST STATEMENTS SOURCE_PROOFS TARGET_PROOFS
#   verify-recovery-bridge.sh proposal   BASE_TRUST NEW_TRUST  SOURCE_PROOFS TARGET_PROOFS
#   verify-recovery-bridge.sh revocation PRIOR_TRUST NEW_TRUST DIGEST...
#
# BASE_TRUST is the committed trust tree before the ceremony, NEW_TRUST the
# proposal. SOURCE_PROOFS and TARGET_PROOFS hold the published
# release-manifest.json and upgrade-compatibility.json of the two releases
# Authenticating those release proofs (Sigstore signatures, pinned root) is the
# job of the authoring and assemble steps, not of this script. What it does check
# is that both manifests are the ones the metadata authorizes and bind the
# declarations the statements are derived from.
#
# `unsigned` runs before anything is signed: it checks the manifests against the
# BASE_TRUST metadata, that the stable policy does not list the statements, and
# re-derives every statement field from the published declarations, comparing the
# statement, normalized with `jq -S`, to that derivation. `proposal` runs that on
# the signed statements and the proposal's metadata, and also checks signatures,
# digests, inventories and that nothing else in the tree changed. `revocation`
# applies the same tree, root, metadata and catalog integrity checks to a revoke
# output against the tree it revoked from. This script shares no code with the Go
# authoring tool and needs only jq, cosign and sha256. Any mismatch exits non-zero.
# shellcheck disable=SC2016,SC2086
set -eu

: "${COSIGN_BIN:=cosign}"
failures=0
fail() { printf 'FAIL %s\n' "$*"; failures=$((failures + 1)); }
ok() { printf 'ok   %s\n' "$*"; }
sha256_of() { shasum -a 256 "$1" | cut -d' ' -f1; }
check() { # DESCRIPTION COMMAND [ARGUMENT...]
	description=$1
	shift
	if "$@" >/dev/null 2>&1; then ok "$description"; else fail "$description"; fi
}
equal() { [ "$1" = "$2" ]; }
usage() {
	printf 'usage: %s unsigned|proposal|revocation ... (see the header)\n' "$0" >&2
	exit 2
}
verify_blob() { "$COSIGN_BIN" verify-blob --insecure-ignore-tlog --key "$recovery_pub" --bundle "$2" "$1"; }
finish() {
	if [ "$failures" -ne 0 ]; then
		printf '\n%s check(s) FAILED. Do not sign or merge.\n' "$failures"
		exit 1
	fi
	printf '\nAll checks passed.\n'
}

# load_releases SOURCE_PROOFS TARGET_PROOFS sets the digests the derivation binds.
load_releases() {
	source=$1 target=$2
	source_manifest=$(sha256_of "$source/release-manifest.json")
	target_manifest=$(sha256_of "$target/release-manifest.json")
	source_version=$(jq -r '.version' "$source/release-manifest.json")
	target_version=$(jq -r '.version' "$target/release-manifest.json")
	check 'manifests bind their compatibility declarations (source)' jq -e --arg d "$(sha256_of "$source/upgrade-compatibility.json")" '[.artifacts[] | select(.kind == "upgrade-compatibility" and .sha256 == $d)] | length == 1' "$source/release-manifest.json"
	check 'manifests bind their compatibility declarations (target)' jq -e --arg d "$(sha256_of "$target/upgrade-compatibility.json")" '[.artifacts[] | select(.kind == "upgrade-compatibility" and .sha256 == $d)] | length == 1' "$target/release-manifest.json"
}

# check_statement DIGEST FILE POLICY_DIGEST re-derives the statement from the declarations.
check_statement() {
	digest=$1 statement=$2 policy_digest=$3
	check "$digest: statement bytes hash to its digest" equal "$(sha256_of "$statement")" "$digest"
	engine=$(jq -r '.source_migrations.engine' "$statement")
	expected=$(jq -S -n \
		--slurpfile sm "$source/release-manifest.json" --slurpfile tm "$target/release-manifest.json" \
		--slurpfile sc "$source/upgrade-compatibility.json" --slurpfile tc "$target/upgrade-compatibility.json" \
		--arg sd "$(sha256_of "$source/upgrade-compatibility.json")" --arg td "$(sha256_of "$target/upgrade-compatibility.json")" \
		--arg smd "$source_manifest" --arg tmd "$target_manifest" --arg policy "$policy_digest" --arg engine "$engine" '
		def identity($c; $cd; $md): {profile: $c.profile, version: $c.version, sequence: $c.sequence, commit: $c.commit, compatibility_sha256: $cd, manifest_sha256: $md};
		($sc[0].engines[] | select(.migrations.engine == $engine)) as $s | ($tc[0].engines[] | select(.migrations.engine == $engine)) as $t |
		{schema: "hikyo.dev/recovery-bridge/v1",
		 source: identity($sc[0]; $sd; $smd), target: identity($tc[0]; $td; $tmd),
		 source_policy_sha256: $policy, target_policy_sha256: $policy,
		 source_migrations: $s.migrations, target_migrations: $t.migrations,
		 source_schema_sha256: $s.schema_sha256, target_schema_sha256: $t.schema_sha256, mode: "maintenance"}' 2>/dev/null) || expected=
	check "$digest ($engine): the statement has no duplicate keys" no_duplicate_keys "$statement"
	check "$digest ($engine): the independent derivation produced a statement" test -n "$expected"
	check "$digest ($engine): every field equals the independent derivation" equal "$(jq -S . "$statement")" "$expected"
	check "$digest ($engine): source migrations are a strict prefix of the target's" jq -e '(.source_migrations.entries | length) < (.target_migrations.entries | length) and (.target_migrations.entries[:(.source_migrations.entries | length)] == .source_migrations.entries)' "$statement"
	check "$digest ($engine): target is a newer stable release" jq -e '.target.sequence > .source.sequence and .source.profile == "stable/v1" and .target.profile == "stable/v1"' "$statement"
}

engines_of() { # FILE...
	for file in "$@"; do jq -r '.source_migrations.engine' "$file"; done | sort | tr '\n' ,
}

json_list() { for item in "$@"; do printf '%s\n' "$item"; done | jq -R . | jq -s -c .; }

# check_authorized METADATA: the published releases are the ones the metadata authorizes.
check_authorized() {
	check "metadata authorizes the source manifest ($source_version)" equal "$(jq -r --arg v "$source_version" --arg d "$source_manifest" '[.releases[] | select(.version == $v and .manifest_sha256 == $d)] | length' "$1")" 1
	check "metadata authorizes the target manifest ($target_version)" equal "$(jq -r --arg v "$target_version" --arg d "$target_manifest" '[.releases[] | select(.version == $v and .manifest_sha256 == $d)] | length' "$1")" 1
}

# Tree hygiene helpers. find is run so that any error is visible to the caller.
bad_names() { (cd "$1" && find . -name '*[!A-Za-z0-9._-]*'); }
readable() { (cd "$1" && find . >/dev/null); }
empty_dirs() { (cd "$1" && find . -mindepth 1 -type d -empty); }
# no_duplicate_keys FILE: jq keeps the last of two equal keys, the runtime refuses both.
no_duplicate_keys() { [ -z "$(jq -c --stream 'select(length == 2) | .[0]' "$1" | sort | uniq -d)" ]; }

# unexpected_files BASE NEW ADDED lists changed or new files that no recovery
# re-authorization may touch (everything but metadata, catalog and added bridges).
unexpected_files() {
	(cd "$2" && find . ! -type d | sort) | while IFS= read -r file; do
		cmp -s "$2/$file" "$1/$file" 2>/dev/null && continue
		case $file in
		./metadata.json | ./metadata.sigstore.json | ./catalog.json | ./catalog.sigstore.json) continue ;;
		esac
		for digest in $3; do
			case $file in
			./bridges/"$digest"/statement.json | ./bridges/"$digest"/statement.sigstore.json) continue 2 ;;
			esac
		done
		printf '%s\n' "$file"
	done
}

# dropped_files BASE NEW REMOVED lists base files missing from NEW other than revoked bridges.
dropped_files() {
	(cd "$1" && find . -type f | sort) | while IFS= read -r file; do
		[ -f "$2/$file" ] && continue
		for digest in $3; do
			case $file in
			./bridges/"$digest"/*) continue 2 ;;
			esac
		done
		printf '%s\n' "$file"
	done
}

# check_chain BASE NEW ADDED REMOVED: the integrity checks shared by a proposal and a
# revocation. ADDED and REMOVED are space-separated bridge digests. The new tree may
# differ from BASE only in the recovery-shaped metadata, the catalog, their signatures,
# the ADDED bridge directories and the absence of the REMOVED ones.
check_chain() {
	base=$1 new=$2 added=$3 removed=$4
	recovery_pub="$new/$(jq -r '.recovery.public_key' "$new/root.json")"
	check 'every path in the new tree is readable' readable "$new"
	check 'every path in the new tree has a plain name (letters, digits, dot, dash, underscore)' equal "$(bad_names "$new")" ''
	check 'the new tree has no empty directories' equal "$(empty_dirs "$new")" ''
	check 'metadata has no duplicate keys' no_duplicate_keys "$new/metadata.json"
	check 'catalog has no duplicate keys' no_duplicate_keys "$new/catalog.json"
	unexpected=$(unexpected_files "$base" "$new" "$added")
	dropped=$(dropped_files "$base" "$new" "$removed")
	check 'only metadata, catalog, their signatures and the two files of each added bridge changed' equal "$unexpected" ''
	check 'the new tree holds only regular files and directories (no symlinks)' equal "$(find "$new" ! -type f ! -type d)" ''
	check 'no base trust file was removed except revoked bridge directories' equal "$dropped" ''
	check 'stable-policy.json is byte-identical (statements bind its digest)' cmp -s "$base/stable-policy.json" "$new/stable-policy.json"
	check 'stable-policy.sigstore.json is byte-identical' cmp -s "$base/stable-policy.sigstore.json" "$new/stable-policy.sigstore.json"
	check 'root.json is byte-identical' cmp -s "$base/root.json" "$new/root.json"
	# Backstop behind the tree-diff allowlist; no negative can pin it alone.
	check 'the recovery public key is byte-identical' cmp -s "$base/$(jq -r '.recovery.public_key' "$base/root.json")" "$recovery_pub"
	check 'metadata signature verifies with the pinned recovery key' verify_blob "$new/metadata.json" "$new/metadata.sigstore.json"
	check 'catalog signature verifies with the pinned recovery key' verify_blob "$new/catalog.json" "$new/catalog.sigstore.json"
	check 'metadata sequence is the base sequence plus one' equal "$(jq '.sequence' "$new/metadata.json")" "$(($(jq '.sequence' "$base/metadata.json") + 1))"
	check 'catalog sequence is the base sequence plus one' equal "$(jq '.sequence' "$new/catalog.json")" "$(($(jq '.sequence' "$base/catalog.json") + 1))"
	check 'metadata is recovery-signed without delegated identity' equal "$(jq -r '[.event.signed_by, (has("source_commit")), (has("release_tag"))] | @csv' "$new/metadata.json")" "\"$(jq -r '.recovery.id' "$new/root.json")\",false,false"
	check 'metadata differs from the base only in sequence, signer and delegated identity' equal \
		"$(jq -S 'del(.sequence, .event.signed_by, .source_commit, .release_tag)' "$new/metadata.json")" \
		"$(jq -S 'del(.sequence, .event.signed_by, .source_commit, .release_tag)' "$base/metadata.json")"
	check 'catalog differs from the base only in sequence, metadata digest and bridges' equal \
		"$(jq -S 'del(.sequence, .stable_metadata_sha256, .bridges)' "$new/catalog.json")" \
		"$(jq -S 'del(.sequence, .stable_metadata_sha256, .bridges)' "$base/catalog.json")"
	# Backstop behind the bridge-directory-to-catalog check; no negative can pin it alone.
	check 'catalog bridges are the base bridges plus the added minus the removed' equal \
		"$(jq -c --argjson a "$(json_list $added)" --argjson r "$(json_list $removed)" '((.bridges + $a) - $r) | sort' "$base/catalog.json")" \
		"$(jq -c '.bridges | sort' "$new/catalog.json")"
	check 'catalog binds the new metadata digest' equal "$(jq -r '.stable_metadata_sha256' "$new/catalog.json")" "$(sha256_of "$new/metadata.json")"
	check 'catalog binds the unchanged stable policy digest' equal "$(jq -r '.stable_policy_sha256' "$new/catalog.json")" "$(sha256_of "$new/stable-policy.json")"
	check 'every catalogued bridge has a directory and no directory is uncatalogued' equal "$(find "$new/bridges" -mindepth 1 -maxdepth 1 -exec basename {} \; | sort | tr '\n' ' ')" "$(jq -r '.bridges[]' "$new/catalog.json" | sort | tr '\n' ' ')"
}

mode=${1:-}
[ -n "$mode" ] || usage
shift
case "$mode" in
unsigned)
	[ "$#" -eq 4 ] || usage
	base=$1 statements=$2
	load_releases "$3" "$4"
	check_authorized "$base/metadata.json"
	policy_digest=$(sha256_of "$base/stable-policy.json")
	count=0
	for directory in "$statements"/*/; do
		count=$((count + 1))
		digest=$(basename "$directory")
		check "$digest: the stable policy does not list it (it would change the digest the statement binds)" jq -e --arg d "$digest" '(.bridges // []) | index($d) | not' "$base/stable-policy.json"
		check_statement "$digest" "${directory}statement.json" "$policy_digest"
	done
	check 'exactly two statements, one per engine' equal "$count:$(engines_of "$statements"/*/statement.json)" '2:postgres,sqlite,'
	finish
	;;
proposal)
	[ "$#" -eq 4 ] || usage
	base=$1 new=$2
	new_bridges=$(jq -r --slurpfile base "$base/catalog.json" '.bridges - $base[0].bridges | .[]' "$new/catalog.json")
	check_chain "$base" "$new" "$new_bridges" ''
	load_releases "$3" "$4"
	check_authorized "$new/metadata.json"
	policy_digest=$(sha256_of "$new/stable-policy.json")
	count=0
	listed=
	for digest in $new_bridges; do
		count=$((count + 1))
		statement="$new/bridges/$digest/statement.json"
		listed="$listed $statement"
		check "$digest: the stable policy does not list it (it would change the digest the statement binds)" jq -e --arg d "$digest" '(.bridges // []) | index($d) | not' "$new/stable-policy.json"
		check "$digest: recovery signature verifies" verify_blob "$statement" "$new/bridges/$digest/statement.sigstore.json"
		check_statement "$digest" "$statement" "$policy_digest"
	done
	check 'exactly two new bridges, one per engine' equal "$count:$(engines_of $listed)" '2:postgres,sqlite,'
	finish
	;;
revocation)
	[ "$#" -ge 3 ] || usage
	prior=$1 new=$2
	shift 2
	check_chain "$prior" "$new" '' "$*"
	removed=$(json_list "$@")
	check 'every revoked digest was catalogued before' jq -e --argjson r "$removed" '($r - .bridges) | length == 0' "$prior/catalog.json"
	check 'no revoked digest is listed by the stable policy' jq -e --argjson r "$removed" '(.bridges // []) as $b | ($r - ($r - $b)) | length == 0' "$new/stable-policy.json"
	for digest in "$@"; do
		check "$digest: its files are gone" test ! -e "$new/bridges/$digest"
	done
	finish
	;;
*) usage ;;
esac
