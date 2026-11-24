#!/bin/sh
# Rehearse the recovery-bridge ceremony end to end with a throwaway key and a
# throwaway root (#1422). Nothing here reads, needs or produces production key
# material: the committed public trust tree is re-rooted under a key generated
# in a scratch directory, and the real published v1.0.0 and v1.1.0 proofs are
# authenticated by their own Sigstore signatures, never re-signed.
#
#   recovery-bridge-rehearsal.sh [PROOFS_DIR]
#
# PROOFS_DIR holds rel100/ and rel110/ with the published release-manifest.json,
# release-manifest.sigstore.json, release-candidate.json, upgrade-compatibility.json,
# build-provenance.json and build-provenance.json.sigstore.json. Without it the
# files are downloaded from the GitHub releases with gh.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
: "${COSIGN_BIN:=cosign}"
# The throwaway key has an empty password; a real key prompts the signer instead.
export COSIGN_PASSWORD=''
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
step() { printf '\n== %s\n' "$*"; }
sha256_of() { shasum -a 256 "$1" | cut -d' ' -f1; }

proofs=${1:-}
if [ -z "$proofs" ]; then
	proofs=$scratch/proofs
	for pair in rel100:v1.0.0 rel110:v1.1.0; do
		mkdir -p "$proofs/${pair%%:*}"
		gh release download "${pair#*:}" -R "${HIKYO_REPOSITORY:-Hikyo-Org/Hikyo}" -D "$proofs/${pair%%:*}" \
			-p release-manifest.json -p release-manifest.sigstore.json -p release-candidate.json \
			-p upgrade-compatibility.json -p build-provenance.json -p build-provenance.json.sigstore.json
	done
fi

step 'throwaway recovery key and test root (the production tree, re-rooted)'
mkdir "$scratch/key" "$scratch/base"
(cd "$scratch/key" && "$COSIGN_BIN" generate-key-pair >/dev/null)
production=$repo_root/release/trust
recovery_id=rehearsal-recovery
cp "$scratch/key/cosign.pub" "$scratch/base/recovery-rehearsal.pub"
primary_pub=$(jq -r '.bootstrap_primary.public_key' "$production/root.json")
cp "$production/$primary_pub" "$production/stable-trusted-root.json" "$scratch/base/"
jq --arg id "$recovery_id" --arg sha "$(sha256_of "$scratch/base/recovery-rehearsal.pub")" \
	'.recovery = {id: $id, public_key: "recovery-rehearsal.pub", sha256: $sha}' "$production/root.json" >"$scratch/base/root.json"
jq --arg id "$recovery_id" --arg sha "$(sha256_of "$scratch/base/recovery-rehearsal.pub")" \
	'.recovery = {id: $id, sha256: $sha} | .event.signed_by = $id | del(.source_commit, .release_tag)' "$production/metadata.json" >"$scratch/base/metadata.json"
cp "$production/stable-policy.json" "$scratch/base/stable-policy.json"
# The production catalog's two legacy nightly bridges are signed by the real
# recovery key, so the rehearsal catalog starts without bridges.
jq --arg m "$(sha256_of "$scratch/base/metadata.json")" --arg p "$(sha256_of "$scratch/base/stable-policy.json")" \
	'.stable_metadata_sha256 = $m | .stable_policy_sha256 = $p | .bridges = []' "$production/catalog.json" >"$scratch/base/catalog.json"
for name in metadata catalog stable-policy; do
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$scratch/key/cosign.key" --bundle "$scratch/base/$name.sigstore.json" "$scratch/base/$name.json" >/dev/null 2>&1
done
mkdir "$scratch/base/bridges"

ceremony=$script_dir/recovery-bridge.sh
step '1. author: derive unsigned statements from the published evidence'
"$ceremony" author "$scratch/base" "$proofs/rel100" "$proofs/rel110" "$scratch/statements"

step '1b. independent verifier on the unsigned statements, before any signing'
"$script_dir/verify-recovery-bridge.sh" unsigned "$scratch/base" "$scratch/statements" "$proofs/rel100" "$proofs/rel110"

step '2. sign: the recovery key signs each statement'
"$ceremony" sign "$scratch/key/cosign.key" "$scratch/statements"

step '3. reauthorize: recovery-shaped metadata and catalog; the stable policy is untouched'
"$ceremony" reauthorize "$scratch/key/cosign.key" "$scratch/base" "$scratch/statements" "$scratch/new"

step '4. independent verifier (jq, cosign and sha256 only)'
"$script_dir/verify-recovery-bridge.sh" proposal "$scratch/base" "$scratch/new" "$proofs/rel100" "$proofs/rel110"

step '5. the proposal is consumed by the same assembler operators use'
"$ceremony" assemble "$scratch/new" "$scratch/bundle" "$proofs/rel100" "$proofs/rel110"
jq -c '{releases: (.releases | length), bridges: (.bridges | length)}' "$scratch/bundle/index.json"

step '6. the verifier refuses every tampering'
refused() { # DESCRIPTION
	if "$script_dir/verify-recovery-bridge.sh" proposal "$scratch/base" "$scratch/t" "$proofs/rel100" "$proofs/rel110" >/dev/null 2>&1; then
		printf 'NOT REFUSED: %s\n' "$1"
		exit 1
	fi
	printf 'refused: %s\n' "$1"
	rm -rf "$scratch/t"
}
first=$(jq -r '.bridges[0]' "$scratch/new/catalog.json")
cp -R "$scratch/new" "$scratch/t"
jq '.mode = "restart"' "$scratch/new/bridges/$first/statement.json" >"$scratch/t/bridges/$first/statement.json"
refused 'a statement edited after signing'
cp -R "$scratch/new" "$scratch/t"
jq '.target_schema_sha256 = .source_schema_sha256' "$scratch/new/bridges/$first/statement.json" >"$scratch/t/bridges/$first/statement.json"
refused 'a statement with a wrong schema digest'
cp -R "$scratch/new" "$scratch/t"
rm -rf "$scratch/t/bridges/$first"
refused 'a bridge listed in the catalog but absent'
cp -R "$scratch/new" "$scratch/t"
jq '.bridges += ["00000000000000000000000000000000000000000000000000000000000000aa"]' "$scratch/new/catalog.json" >"$scratch/t/catalog.json"
refused 'an uncatalogued or unsigned catalog change'
cp -R "$scratch/new" "$scratch/t"
jq --arg d "$first" '.bridges += [$d]' "$scratch/new/stable-policy.json" >"$scratch/t/stable-policy.json"
refused 'a stable policy edited to list a bridge'
cp -R "$scratch/new" "$scratch/t"
(cd "$scratch/key" && mkdir other && cd other && "$COSIGN_BIN" generate-key-pair >/dev/null 2>&1)
"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
	--key "$scratch/key/other/cosign.key" --bundle "$scratch/t/bridges/$first/statement.sigstore.json" "$scratch/t/bridges/$first/statement.json" >/dev/null 2>&1 </dev/null
refused 'a statement signed by a different key'

# A statement with one wrong field that is properly signed and catalogued: every
# signature and digest check passes, so only the independent re-derivation catches it.
cp -R "$scratch/new" "$scratch/t"
jq '.target_schema_sha256 = .source_schema_sha256' "$scratch/new/bridges/$first/statement.json" >"$scratch/forged.json"
forged=$(jq -S . "$scratch/forged.json" >"$scratch/forged-sorted.json" && sha256_of "$scratch/forged-sorted.json")
mkdir "$scratch/t/bridges/$forged"
cp "$scratch/forged-sorted.json" "$scratch/t/bridges/$forged/statement.json"
rm -rf "$scratch/t/bridges/$first"
(cd "$scratch/t/bridges/$forged" && "$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
	--key "$scratch/key/cosign.key" --bundle statement.sigstore.json statement.json >/dev/null 2>&1)
jq --arg old "$first" --arg new "$forged" '.bridges |= map(if . == $old then $new else . end)' "$scratch/new/catalog.json" >"$scratch/t/catalog.json"
"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
	--key "$scratch/key/cosign.key" --bundle "$scratch/t/catalog.sigstore.json" "$scratch/t/catalog.json" >/dev/null 2>&1
if "$script_dir/verify-recovery-bridge.sh" proposal "$scratch/base" "$scratch/t" "$proofs/rel100" "$proofs/rel110" >"$scratch/forged.out" 2>&1; then
	printf 'NOT REFUSED: a signed, catalogued statement with a wrong field\n'
	exit 1
fi
failed=$(grep '^FAIL' "$scratch/forged.out" | sed 's/^FAIL [0-9a-f]*//' | sort -u)
case $failed in
' (sqlite): every field equals the independent derivation' | ' (postgres): every field equals the independent derivation') printf 'refused by the re-derivation alone: a signed, catalogued statement with a wrong field\n' ;;
*) printf 'unexpected failures:\n%s\n' "$failed"; exit 1 ;;
esac
rm -rf "$scratch/t"

step '7. revocation: a new recovery-signed snapshot drops the bridges'
second=$(jq -r '.bridges[1]' "$scratch/new/catalog.json")
# A bridge the stable policy lists needs a policy rotation, so revoke refuses it.
cp -R "$scratch/new" "$scratch/policy-listed"
jq --arg d "$first" '.bridges = [$d]' "$scratch/new/stable-policy.json" >"$scratch/policy-listed/stable-policy.json"
if "$ceremony" revoke "$scratch/key/cosign.key" "$scratch/policy-listed" "$scratch/refused-revoke" "$first" >/dev/null 2>&1; then
	printf 'NOT REFUSED: revoking a policy-listed bridge\n'
	exit 1
fi
printf 'refused: revoking a bridge the stable policy lists\n'
"$ceremony" revoke "$scratch/key/cosign.key" "$scratch/new" "$scratch/revoked" "$first" "$second"
"$script_dir/verify-recovery-bridge.sh" revocation "$scratch/new" "$scratch/revoked" "$first" "$second"
"$ceremony" assemble "$scratch/revoked" "$scratch/revoked-bundle" "$proofs/rel100" "$proofs/rel110"
jq -c '{releases: (.releases | length), bridges: (.bridges | length)}' "$scratch/revoked-bundle/index.json"
# Limit: revocation binds only instances whose persisted floor has advanced past
# the prior snapshot. An instance that never loaded the newer snapshot still
# accepts an older bundle carrying the bridge, and statements have no expiry.

printf '\nRehearsal complete: ceremony steps, verifier and assembler agree; all tamperings refused.\n'
