#!/bin/sh
# Independent verifier for a recovery-bridge trust proposal (#1422).
#
#   verify-recovery-bridge.sh BASE_TRUST NEW_TRUST SOURCE_PROOFS TARGET_PROOFS
#
# BASE_TRUST is the committed trust tree before the ceremony, NEW_TRUST the
# proposal. SOURCE_PROOFS and TARGET_PROOFS hold the published
# release-manifest.json and upgrade-compatibility.json of the two releases
# (authenticate them first with `stable verify --historical --published`).
#
# This script shares no code with the Go authoring tool. It re-derives every
# statement field from the published declarations with jq, compares the signed
# statement bytes to that derivation, and checks the signatures, digests and
# inventories with cosign and sha256 only. Any mismatch exits non-zero.
set -eu

: "${COSIGN_BIN:=cosign}"
failures=0
fail() { printf 'FAIL %s\n' "$*"; failures=$((failures + 1)); }
ok() { printf 'ok   %s\n' "$*"; }
sha256_of() { shasum -a 256 "$1" | cut -d' ' -f1; }
check() { # DESCRIPTION CONDITION-COMMAND...
	description=$1
	shift
	if "$@" >/dev/null 2>&1; then ok "$description"; else fail "$description"; fi
}
equal() { [ "$1" = "$2" ]; }

[ "$#" -eq 4 ] || { printf 'usage: %s BASE_TRUST NEW_TRUST SOURCE_PROOFS TARGET_PROOFS\n' "$0" >&2; exit 2; }
base=$1 new=$2 source=$3 target=$4
recovery_pub="$new/$(jq -r '.recovery.public_key' "$new/root.json")"

verify_blob() { "$COSIGN_BIN" verify-blob --insecure-ignore-tlog --key "$recovery_pub" --bundle "$2" "$1"; }

# 1. The trust tree changed only where a recovery re-authorization may change it.
unexpected=$(
	(cd "$new" && find . -type f | sort) | while IFS= read -r file; do
		cmp -s "$new/$file" "$base/$file" 2>/dev/null || printf '%s\n' "$file"
	done | grep -vE '^\./(bridges/.*|catalog\.json|catalog\.sigstore\.json|metadata\.json|metadata\.sigstore\.json)$' || true
)
dropped=$(cd "$base" && find . -type f | sort | while IFS= read -r file; do [ -f "$new/$file" ] || printf '%s\n' "$file"; done)
check 'only metadata, catalog, their signatures and bridges/ changed' equal "$unexpected" ''
check 'no base trust file was removed' equal "$dropped" ''
check 'stable-policy.json and its signature are byte-identical (statements bind its digest)' cmp -s "$base/stable-policy.json" "$new/stable-policy.json"
check 'root and recovery key are byte-identical' sh -c "cmp -s '$base/root.json' '$new/root.json' && cmp -s '$base/$(jq -r '.recovery.public_key' "$base/root.json")' '$recovery_pub'"

# 2. Metadata and catalog are the recovery re-authorization shape and bind each other.
check 'metadata signature verifies with the pinned recovery key' verify_blob "$new/metadata.json" "$new/metadata.sigstore.json"
check 'catalog signature verifies with the pinned recovery key' verify_blob "$new/catalog.json" "$new/catalog.sigstore.json"
check 'metadata sequence is the base sequence plus one' equal "$(jq '.sequence' "$new/metadata.json")" "$(($(jq '.sequence' "$base/metadata.json") + 1))"
check 'catalog sequence is the base sequence plus one' equal "$(jq '.sequence' "$new/catalog.json")" "$(($(jq '.sequence' "$base/catalog.json") + 1))"
check 'metadata is recovery-signed without delegated identity' equal "$(jq -r '[.event.signed_by, (has("source_commit")), (has("release_tag"))] | @csv' "$new/metadata.json")" "\"$(jq -r '.recovery.id' "$new/root.json")\",false,false"
check 'metadata differs from the base only in sequence, signer and delegated identity' equal \
	"$(jq -S 'del(.sequence, .event.signed_by, .source_commit, .release_tag)' "$new/metadata.json")" \
	"$(jq -S 'del(.sequence, .event.signed_by, .source_commit, .release_tag)' "$base/metadata.json")"
check 'catalog binds the new metadata digest' equal "$(jq -r '.stable_metadata_sha256' "$new/catalog.json")" "$(sha256_of "$new/metadata.json")"
check 'catalog binds the unchanged stable policy digest' equal "$(jq -r '.stable_policy_sha256' "$new/catalog.json")" "$(sha256_of "$new/stable-policy.json")"
check 'catalog keeps the base nightly policies' equal "$(jq -c '.nightly_policies' "$new/catalog.json")" "$(jq -c '.nightly_policies' "$base/catalog.json")"

# 3. The published releases are the ones the metadata authorizes.
source_manifest=$(sha256_of "$source/release-manifest.json")
target_manifest=$(sha256_of "$target/release-manifest.json")
source_version=$(jq -r '.version' "$source/release-manifest.json")
target_version=$(jq -r '.version' "$target/release-manifest.json")
check "metadata authorizes the source manifest ($source_version)" equal "$(jq -r --arg v "$source_version" --arg d "$source_manifest" '[.releases[] | select(.version == $v and .manifest_sha256 == $d)] | length' "$new/metadata.json")" 1
check "metadata authorizes the target manifest ($target_version)" equal "$(jq -r --arg v "$target_version" --arg d "$target_manifest" '[.releases[] | select(.version == $v and .manifest_sha256 == $d)] | length' "$new/metadata.json")" 1
check 'manifests bind their compatibility declarations' sh -c "
	jq -e --arg d '$(sha256_of "$source/upgrade-compatibility.json")' '[.artifacts[] | select(.kind == \"upgrade-compatibility\" and .sha256 == \$d)] | length == 1' '$source/release-manifest.json' &&
	jq -e --arg d '$(sha256_of "$target/upgrade-compatibility.json")' '[.artifacts[] | select(.kind == \"upgrade-compatibility\" and .sha256 == \$d)] | length == 1' '$target/release-manifest.json'"

# 4. Every new bridge is catalogued, signed and exactly the derivation of the published evidence.
policy_digest=$(sha256_of "$new/stable-policy.json")
new_bridges=$(jq -r --slurpfile base "$base/catalog.json" '.bridges - $base[0].bridges | .[]' "$new/catalog.json")
count=0
for digest in $new_bridges; do
	count=$((count + 1))
	statement="$new/bridges/$digest/statement.json"
	check "$digest: statement bytes hash to the catalog digest" equal "$(sha256_of "$statement")" "$digest"
	# shellcheck disable=SC2016
	check "$digest: the stable policy does not list it (it would change the digest the statement binds)" jq -e --arg d "$digest" '.bridges | index($d) | not' "$new/stable-policy.json"
	check "$digest: recovery signature verifies" verify_blob "$statement" "$new/bridges/$digest/statement.sigstore.json"
	engine=$(jq -r '.source_migrations.engine' "$statement")
	expected=$(jq -S -n \
		--slurpfile sm "$source/release-manifest.json" --slurpfile tm "$target/release-manifest.json" \
		--slurpfile sc "$source/upgrade-compatibility.json" --slurpfile tc "$target/upgrade-compatibility.json" \
		--arg sd "$(sha256_of "$source/upgrade-compatibility.json")" --arg td "$(sha256_of "$target/upgrade-compatibility.json")" \
		--arg smd "$source_manifest" --arg tmd "$target_manifest" --arg policy "$policy_digest" --arg engine "$engine" '
		def identity($m; $c; $cd; $md): {profile: $c.profile, version: $c.version, sequence: $c.sequence, commit: $c.commit, compatibility_sha256: $cd, manifest_sha256: $md};
		($sc[0].engines[] | select(.migrations.engine == $engine)) as $s | ($tc[0].engines[] | select(.migrations.engine == $engine)) as $t |
		{schema: "hikyo.dev/recovery-bridge/v1",
		 source: identity($sm[0]; $sc[0]; $sd; $smd), target: identity($tm[0]; $tc[0]; $td; $tmd),
		 source_policy_sha256: $policy, target_policy_sha256: $policy,
		 source_migrations: $s.migrations, target_migrations: $t.migrations,
		 source_schema_sha256: $s.schema_sha256, target_schema_sha256: $t.schema_sha256, mode: "maintenance"}')
	check "$digest ($engine): every field equals the independent derivation" equal "$(jq -S . "$statement")" "$expected"
	check "$digest ($engine): source migrations are a strict prefix of the target's" jq -e '(.source_migrations.entries | length) < (.target_migrations.entries | length) and (.target_migrations.entries[:(.source_migrations.entries | length)] == .source_migrations.entries)' "$statement"
	check "$digest ($engine): target is a newer stable release" jq -e '.target.sequence > .source.sequence and .source.profile == "stable/v1" and .target.profile == "stable/v1"' "$statement"
done
check 'exactly two new bridges, one per engine' equal "$count:$(for d in $new_bridges; do jq -r '.source_migrations.engine' "$new/bridges/$d/statement.json"; done | sort | tr '\n' ,)" '2:postgres,sqlite,'
check 'no bridge directory is uncatalogued' equal "$(find "$new/bridges" -mindepth 1 -maxdepth 1 -exec basename {} \; | sort | tr '\n' ' ')" "$(jq -r '.bridges[]' "$new/catalog.json" | sort | tr '\n' ' ')"

if [ "$failures" -ne 0 ]; then
	printf '\n%s check(s) FAILED. Do not sign or merge.\n' "$failures"
	exit 1
fi
printf '\nAll checks passed.\n'
