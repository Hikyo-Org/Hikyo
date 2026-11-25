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
trap 'chmod -R u+rwx "$scratch" 2>/dev/null; rm -rf "$scratch"' EXIT
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
# Production shape: the catalog and the stable policy both list the two legacy
# nightly bridges. Their statements are re-signed with the throwaway key (the real
# signatures belong to the real recovery key), so the base tree stays loadable.
jq --arg m "$(sha256_of "$scratch/base/metadata.json")" --arg p "$(sha256_of "$scratch/base/stable-policy.json")" \
	'.stable_metadata_sha256 = $m | .stable_policy_sha256 = $p' "$production/catalog.json" >"$scratch/base/catalog.json"
mkdir "$scratch/base/bridges"
for digest in $(jq -r '.bridges[]' "$scratch/base/catalog.json"); do
	mkdir "$scratch/base/bridges/$digest"
	cp "$production/bridges/$digest/statement.json" "$scratch/base/bridges/$digest/statement.json"
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$scratch/key/cosign.key" --bundle "$scratch/base/bridges/$digest/statement.sigstore.json" "$scratch/base/bridges/$digest/statement.json" >/dev/null 2>&1
done
legacy_first=$(jq -r '.bridges[0]' "$scratch/base/catalog.json")
for name in metadata catalog stable-policy; do
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$scratch/key/cosign.key" --bundle "$scratch/base/$name.sigstore.json" "$scratch/base/$name.json" >/dev/null 2>&1
done

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
# The two new bridges (the production-shaped base already lists two legacy ones).
new_bridges=$(jq -r --slurpfile base "$scratch/base/catalog.json" '.bridges - $base[0].bridges | .[]' "$scratch/new/catalog.json")
first=$(printf '%s\n' "$new_bridges" | sed -n 1p)
second=$(printf '%s\n' "$new_bridges" | sed -n 2p)
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
' (sqlite): every field but not_after equals the independent derivation' | ' (postgres): every field but not_after equals the independent derivation') printf 'refused by the re-derivation alone: a signed, catalogued statement with a wrong field\n' ;;
*) printf 'unexpected failures:\n%s\n' "$failed"; exit 1 ;;
esac
rm -rf "$scratch/t"

# expect_fail PATTERN DESCRIPTION COMMAND...: the verifier must fail and one of its
# FAIL lines must match PATTERN, so a negative is caught by the check it targets.
expect_fail() {
	pattern=$1 description=$2
	shift 2
	if "$@" >"$scratch/expect.out" 2>&1; then
		printf 'NOT REFUSED: %s\n' "$description"
		exit 1
	fi
	grep -q "^FAIL.*$pattern" "$scratch/expect.out" || { printf 'refused, but not by the targeted check (%s):\n' "$pattern"; grep '^FAIL' "$scratch/expect.out"; exit 1; }
	printf 'refused by %s: %s\n' "\"$pattern\"" "$description"
}

step '2b. faked target proofs: the unsigned verifier binds proofs to the base metadata'
cp -R "$proofs/rel110" "$scratch/fake110"
jq '.version = "9.9.9"' "$proofs/rel110/release-manifest.json" >"$scratch/fake110/release-manifest.json"
expect_fail 'metadata authorizes the target manifest' 'target proofs not authorized by the base metadata' \
	"$script_dir/verify-recovery-bridge.sh" unsigned "$scratch/base" "$scratch/statements" "$proofs/rel100" "$scratch/fake110"
jq --arg d "$first" '.bridges = [$d]' "$scratch/base/stable-policy.json" >"$scratch/listed-base-policy.json"
cp -R "$scratch/base" "$scratch/listed-base"
cp "$scratch/listed-base-policy.json" "$scratch/listed-base/stable-policy.json"
expect_fail 'the stable policy does not list it' 'a base policy that lists a statement' \
	"$script_dir/verify-recovery-bridge.sh" unsigned "$scratch/listed-base" "$scratch/statements" "$proofs/rel100" "$proofs/rel110"

step '6b. signed edits and structural tamperings: each is refused by its own integrity check'
# edit_t FILE JQ...: edit a file in the scratch copy; resign_t KEY: bind the catalog to the
# metadata digest and sign both with KEY, so the signatures are valid and only integrity can refuse.
edit_t() {
	file=$1
	shift
	jq "$@" "$scratch/t/$file" >"$scratch/t/$file.tmp"
	mv "$scratch/t/$file.tmp" "$scratch/t/$file"
}
resign_t() {
	jq --arg d "$(sha256_of "$scratch/t/metadata.json")" '.stable_metadata_sha256 = $d' "$scratch/t/catalog.json" >"$scratch/t/catalog.tmp"
	mv "$scratch/t/catalog.tmp" "$scratch/t/catalog.json"
	for name in metadata catalog; do
		"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
			--key "$1" --bundle "$scratch/t/$name.sigstore.json" "$scratch/t/$name.json" >/dev/null 2>&1 </dev/null
	done
}
proposal_fails() { # PATTERN DESCRIPTION
	expect_fail "$1" "$2" "$script_dir/verify-recovery-bridge.sh" proposal "$scratch/base" "$scratch/t" "$proofs/rel100" "$proofs/rel110"
	chmod -R u+rwx "$scratch/t" 2>/dev/null
	rm -rf "$scratch/t"
}
recovery_key=$scratch/key/cosign.key
cp -R "$scratch/new" "$scratch/t"
edit_t catalog.json '.nightly_policies += ["00000000000000000000000000000000000000000000000000000000000000bb"]'
resign_t "$recovery_key"
proposal_fails 'catalog differs from the base only' 'a validly signed catalog with an extra nightly policy'
cp -R "$scratch/new" "$scratch/t"
edit_t metadata.json '.surprise = 1'
resign_t "$recovery_key"
proposal_fails 'metadata differs from the base only' 'a validly signed metadata with an extra field'
cp -R "$scratch/new" "$scratch/t"
edit_t metadata.json '.sequence += 1'
resign_t "$recovery_key"
proposal_fails 'metadata sequence is the base sequence plus one' 'a validly signed metadata that skips a sequence'
cp -R "$scratch/new" "$scratch/t"
edit_t catalog.json '.sequence += 1'
resign_t "$recovery_key"
proposal_fails 'catalog sequence is the base sequence plus one' 'a validly signed catalog that skips a sequence'
cp -R "$scratch/new" "$scratch/t"
# shellcheck disable=SC2016
edit_t catalog.json --arg d "$legacy_first" '.bridges -= [$d]'
resign_t "$recovery_key"
proposal_fails 'every catalogued bridge has a directory' 'a validly signed catalog that drops a legacy bridge but keeps its files'
cp -R "$scratch/new" "$scratch/t"
# shellcheck disable=SC2016
edit_t catalog.json --arg d "$legacy_first" '.bridges -= [$d]'
rm -rf "$scratch/t/bridges/$legacy_first"
resign_t "$recovery_key"
proposal_fails 'no replaced bridge is listed by the stable policy' 'a policy-listed legacy bridge dropped from the catalog with its files removed'
cp -R "$scratch/new" "$scratch/t"
rm "$scratch/t/stable-trusted-root.json"
proposal_fails 'no base trust file was removed' 'a base trust file deleted from the tree'
cp -R "$scratch/new" "$scratch/t"
cp "$scratch/key/other/cosign.pub" "$scratch/t/$(jq -r '.recovery.public_key' "$scratch/t/root.json")"
resign_t "$scratch/key/other/cosign.key"
proposal_fails 'only metadata, catalog' 'a swapped recovery key with root.json unchanged'
cp -R "$scratch/new" "$scratch/t"
rm "$scratch/t/$primary_pub"
ln -s "$scratch/base/$primary_pub" "$scratch/t/$primary_pub"
proposal_fails 'no symlinks' 'a primary key replaced by a symlink to identical bytes'
cp -R "$scratch/new" "$scratch/t"
printf 'x' >"$scratch/t/bridges/$first/extra.txt"
proposal_fails 'only metadata, catalog' 'an extra file in a new bridge directory'

nl=$(printf '\n_')
nl=${nl%_}
cp -R "$scratch/new" "$scratch/t"
cp "$scratch/new/metadata.json" "$scratch/t/metadata.json$nl"
proposal_fails 'plain name' 'a file whose name is an allowed name plus a newline'
cp -R "$scratch/new" "$scratch/t"
cp "$scratch/new/bridges/$first/statement.json" "$scratch/t/bridges/$first/statement.json$nl"
proposal_fails 'plain name' 'a newline-suffixed file inside a new bridge directory'
cp -R "$scratch/new" "$scratch/t"
mkdir "$scratch/t/locked"
printf 'x' >"$scratch/t/locked/extra"
ln -s extra "$scratch/t/locked/link"
chmod 000 "$scratch/t/locked"
proposal_fails 'is readable' 'an unreadable directory hiding an extra file and a symlink'
cp -R "$scratch/new" "$scratch/t"
mkdir "$scratch/t/empty-root"
proposal_fails 'no empty directories' 'an extra empty directory at the root'
cp -R "$scratch/new" "$scratch/t"
mkdir "$scratch/t/bridges/$first/empty"
proposal_fails 'no empty directories' 'an extra empty directory inside a new bridge directory'
# dupkey FILE TEXT: prepend a key to the top-level object, so FILE then repeats it. BSD and GNU safe.
dupkey() {
	sed "1s/{/{$2,/" "$1" >"$1.tmp"
	mv "$1.tmp" "$1"
}
sign_t() { # NAME: sign a scratch file without rewriting it
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$recovery_key" --bundle "$scratch/t/$1.sigstore.json" "$scratch/t/$1.json" >/dev/null 2>&1 </dev/null
}
# The earlier duplicate may be a scalar or a container; jq --stream leaf paths alone miss containers.
for dup_value in '"schema":"duplicate"' '"releases":[]' '"event":null' '"event":{"zz":1}'; do
	cp -R "$scratch/new" "$scratch/t"
	dupkey "$scratch/t/metadata.json" "$dup_value"
	resign_t "$recovery_key"
	proposal_fails 'metadata has no duplicate keys' "a validly signed metadata that repeats $dup_value"
done
for dup_value in '"schema":"duplicate"' '"bridges":[]'; do
	cp -R "$scratch/new" "$scratch/t"
	dupkey "$scratch/t/catalog.json" "$dup_value"
	sign_t catalog
	proposal_fails 'catalog has no duplicate keys' "a validly signed catalog that repeats $dup_value"
done
for dup_value in '"mode":"restart"' '"source":{}'; do
	cp -R "$scratch/new" "$scratch/t"
	cp "$scratch/new/bridges/$first/statement.json" "$scratch/dup-statement.json"
	dupkey "$scratch/dup-statement.json" "$dup_value"
	dup=$(sha256_of "$scratch/dup-statement.json")
	mkdir "$scratch/t/bridges/$dup"
	cp "$scratch/dup-statement.json" "$scratch/t/bridges/$dup/statement.json"
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$recovery_key" --bundle "$scratch/t/bridges/$dup/statement.sigstore.json" "$scratch/t/bridges/$dup/statement.json" >/dev/null 2>&1 </dev/null
	rm -rf "$scratch/t/bridges/$first"
	# shellcheck disable=SC2016
	edit_t catalog.json --arg old "$first" --arg new "$dup" '.bridges |= map(if . == $old then $new else . end)'
	resign_t "$recovery_key"
	proposal_fails 'the statement has no duplicate keys' "a signed, catalogued statement that repeats $dup_value"
	rm -f "$scratch/dup-statement.json"
done
cp -R "$scratch/new" "$scratch/t"
mkdir "$scratch/t/noexec"
printf 'x' >"$scratch/t/noexec/extra"
chmod 644 "$scratch/t/noexec"
proposal_fails 'is readable' 'a mode-644 directory that BSD find would not descend into'
cp -R "$scratch/new" "$scratch/t"
cp "$scratch/new/metadata.json" "$scratch/t/caf$(printf '\303\251')"
case $(uname) in Darwin) utf8=en_US.UTF-8 ;; *) utf8=C.UTF-8 ;; esac
LC_ALL=$utf8 proposal_fails 'plain name' 'a non-ASCII file name under a UTF-8 locale (the verifier must pin LC_ALL=C)'
rm -f "$scratch/dup-statement.json"

step '6c. expiry: a signed, catalogued statement with a bad not_after is refused by the not_after check'
# forge_statement JQ_FILTER PATTERN DESCRIPTION: replace the first new bridge by a validly
# signed, catalogued edit of it, so only the statement's own checks can refuse it.
forge_statement() {
	cp -R "$scratch/new" "$scratch/t"
	jq -S "$1" "$scratch/new/bridges/$first/statement.json" >"$scratch/forged-sorted.json"
	forged=$(sha256_of "$scratch/forged-sorted.json")
	mkdir "$scratch/t/bridges/$forged"
	cp "$scratch/forged-sorted.json" "$scratch/t/bridges/$forged/statement.json"
	rm -rf "$scratch/t/bridges/$first"
	(cd "$scratch/t/bridges/$forged" && "$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$recovery_key" --bundle statement.sigstore.json statement.json >/dev/null 2>&1 </dev/null)
	# shellcheck disable=SC2016
	edit_t catalog.json --arg old "$first" --arg new "$forged" '.bridges |= map(if . == $old then $new else . end)'
	resign_t "$recovery_key"
	proposal_fails "$2" "$3"
}
# The honest statement carries a not_after inside the window.
jq -e '.schema == "hikyo.dev/recovery-bridge/v2" and (.not_after | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$"))' "$scratch/new/bridges/$first/statement.json" >/dev/null
printf 'ok: the authored statement is v2 with a canonical not_after\n'
forge_statement '.not_after = "2020-01-01T00:00:00Z"' 'not_after is canonical UTC, in the future' 'an expired not_after'
forge_statement 'del(.not_after)' 'not_after is canonical UTC, in the future' 'a missing not_after'
forge_statement '.not_after = "tomorrow"' 'not_after is canonical UTC, in the future' 'a malformed not_after'
forge_statement '.not_after = "2099-02-30T00:00:00Z"' 'not_after is canonical UTC, in the future' 'a not_after that is not a real date'
forge_statement '.not_after = "2099-01-01T00:00:00Z"' 'not_after is canonical UTC, in the future' 'a not_after more than 30 days out (an unbounded bridge)'
forge_statement '.not_after = "2099-01-01T01:00:00+01:00"' 'not_after is canonical UTC, in the future' 'a not_after with an offset'
# A leap second is not canonical: jq normalizes it, so only the round trip catches it.
VERIFY_NOW=$(jq -n '"2099-12-31T23:00:00Z" | fromdateiso8601')
export VERIFY_NOW
forge_statement '.not_after = "2099-12-31T23:59:60Z"' 'not_after is canonical UTC, in the future' 'a leap-second not_after inside the window'
unset VERIFY_NOW
forge_statement '.schema = "hikyo.dev/recovery-bridge/v1" | del(.not_after)' 'every field but not_after equals' 'a downgrade to the v1 statement without expiry'

step '6d. revocation list: recovery-signed, root-bound, monotonic; the apply-time checker obeys it'
check_out() { # WANT DESCRIPTION COMMAND...: the command succeeds and prints WANT
	want=$1 description=$2
	shift 2
	out=$("$@" 2>&1) || { printf 'FAILED: %s\n%s\n' "$description" "$out"; exit 1; }
	case $out in *"$want"*) printf 'ok: %s\n' "$description" ;; *) printf 'FAILED: %s: got %s\n' "$description" "$out"; exit 1 ;; esac
}
check_refused() { # WANT DESCRIPTION COMMAND...: the command fails and its error names WANT
	want=$1 description=$2
	shift 2
	if out=$("$@" 2>&1); then printf 'NOT REFUSED: %s\n' "$description"; exit 1; fi
	case $out in *"$want"*) printf 'refused: %s\n' "$description" ;; *) printf 'refused, but not as expected (%s): %s\n' "$want" "$out"; exit 1 ;; esac
}
pin_root=$(sha256_of "$scratch/base/root.json")
pin_key=$(sha256_of "$scratch/base/recovery-rehearsal.pub")
"$ceremony" revocation-list "$recovery_key" "$scratch/new" 1 "$scratch/list1" "$first" >/dev/null
"$ceremony" revocation-list "$recovery_key" "$scratch/new" 2 "$scratch/list2" >/dev/null
"$script_dir/verify-recovery-bridge.sh" revocation-list "$scratch/new" "$scratch/list1" 0 "$pin_root" "$pin_key" >/dev/null
printf 'ok: the verifier accepts the first list\n'
list_fails() { # PATTERN DESCRIPTION: the list in scratch/t must be refused by the targeted check
	expect_fail "$1" "$2" "$script_dir/verify-recovery-bridge.sh" revocation-list "$scratch/new" "$scratch/t" "${3:-0}" "$pin_root" "$pin_key"
	rm -rf "$scratch/t"
}
mkdir "$scratch/t"
cp "$scratch/list1/recovery-revocations.json" "$scratch/t/"
"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
	--key "$scratch/key/other/cosign.key" --bundle "$scratch/t/recovery-revocations.sigstore.json" "$scratch/t/recovery-revocations.json" >/dev/null 2>&1 </dev/null
list_fails 'list signature verifies' 'a list signed by a different key'
cp -R "$scratch/list1" "$scratch/t"
list_fails 'sequence is the prior sequence plus one' 'an older or equal list sequence' 1
cp -R "$scratch/list1" "$scratch/t"
jq '.trust_root_sha256 = "00000000000000000000000000000000000000000000000000000000000000cc"' "$scratch/list1/recovery-revocations.json" >"$scratch/t/recovery-revocations.json"
"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
	--key "$recovery_key" --bundle "$scratch/t/recovery-revocations.sigstore.json" "$scratch/t/recovery-revocations.json" >/dev/null 2>&1 </dev/null
list_fails 'bound to the pinned trust root' 'a validly signed list bound to another root'
cp -R "$scratch/list1" "$scratch/t"
printf ' ' >>"$scratch/t/recovery-revocations.json"
list_fails 'list signature verifies' 'a list edited after signing'
"$ceremony" revocation-list "$recovery_key" "$scratch/new" 3 "$scratch/list-unsorted" "$first" "$second" >/dev/null
mkdir "$scratch/t"
jq '.revoked |= reverse' "$scratch/list-unsorted/recovery-revocations.json" >"$scratch/t/recovery-revocations.json"
"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
	--key "$recovery_key" --bundle "$scratch/t/recovery-revocations.sigstore.json" "$scratch/t/recovery-revocations.json" >/dev/null 2>&1 </dev/null
list_fails 'sorted, unique' 'a validly signed list that is not sorted' 2
rm -rf "$scratch/list-unsorted"
# sign_list DIR: re-sign the list in DIR with the recovery key (a signed edit).
sign_list() {
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$recovery_key" --bundle "$1/recovery-revocations.sigstore.json" "$1/recovery-revocations.json" >/dev/null 2>&1 </dev/null
}
# edit_list JQ...: a validly signed edit of list1 in scratch/t, so only the list checks can refuse it.
edit_list() {
	mkdir "$scratch/t"
	jq "$@" "$scratch/list1/recovery-revocations.json" >"$scratch/t/recovery-revocations.json"
	sign_list "$scratch/t"
}
edit_list '.extra = 1'
list_fails 'exactly the schema' 'a validly signed list with an extra field'
edit_list 'del(.issued_at)'
list_fails 'exactly the schema' 'a validly signed list without issued_at'
edit_list '.schema = "hikyo.dev/recovery-revocations/v2"'
list_fails 'exactly the schema' 'a validly signed list with another schema'
edit_list '.issued_at = "2020-01-01T00:00:00Z"'
list_fails 'not older than the newest catalogued bridge' 'a validly signed list issued before the bridge existed'
edit_list '.issued_at = "2099-01-01T00:00:00Z"'
list_fails 'issued_at is canonical UTC' 'a validly signed list issued in the future'
edit_list '.issued_at = "2026-01-01T01:00:00+01:00"'
list_fails 'issued_at is canonical UTC' 'a validly signed list whose issued_at has an offset'
mkdir "$scratch/t"
cp "$scratch/list1/recovery-revocations.json" "$scratch/t/"
dupkey "$scratch/t/recovery-revocations.json" '"revoked":[]'
sign_list "$scratch/t"
list_fails 'the list has no duplicate keys' 'a validly signed list that repeats a key'
# The verifier takes its key and root from the committed pins, not from the tree it is handed.
cp -R "$scratch/new" "$scratch/rerooted"
cp "$scratch/key/other/cosign.pub" "$scratch/rerooted/$(jq -r '.recovery.public_key' "$scratch/rerooted/root.json")"
jq --arg sha "$(sha256_of "$scratch/key/other/cosign.pub")" '.recovery.sha256 = $sha' "$scratch/new/root.json" >"$scratch/rerooted/root.json"
ISSUED_AT=$(date +%s) COSIGN_PASSWORD='' "$ceremony" revocation-list "$scratch/key/other/cosign.key" "$scratch/rerooted" 1 "$scratch/list-rerooted" "$first" >/dev/null
expect_fail 'trust root is the committed one' 'a list checked against a tree whose root and recovery key were swapped' \
	"$script_dir/verify-recovery-bridge.sh" revocation-list "$scratch/rerooted" "$scratch/list-rerooted" 0 "$pin_root" "$pin_key"
rm -rf "$scratch/rerooted" "$scratch/list-rerooted"
# Same root, but the recovery key file itself swapped: only the key pin can refuse it.
cp -R "$scratch/new" "$scratch/rekeyed"
cp "$scratch/key/other/cosign.pub" "$scratch/rekeyed/$(jq -r '.recovery.public_key' "$scratch/rekeyed/root.json")"
expect_fail 'recovery public key is the committed one' 'a list checked against a swapped recovery key file under the committed root' \
	"$script_dir/verify-recovery-bridge.sh" revocation-list "$scratch/rekeyed" "$scratch/list1" 0 "$pin_root" "$pin_key"
rm -rf "$scratch/rekeyed"
# A list is signed at publication and on revocation, never on a schedule, so the verifier
# has no maximum age: it refuses only a list that predates the newest bridge. This one is
# 8 days old, so it is stale to the checker below and, as the bridge was authored today,
# predates the bridge too.
ISSUED_AT=$(($(date +%s) - 8 * 86400)) "$ceremony" revocation-list "$recovery_key" "$scratch/new" 1 "$scratch/list-old" "$first" >/dev/null
mkdir "$scratch/t"
cp "$scratch/list-old/recovery-revocations.json" "$scratch/list-old/recovery-revocations.sigstore.json" "$scratch/t/"
list_fails 'not older than the newest catalogued bridge' 'a list issued eight days ago, before the bridge was authored'
# The real apply-time checker, run on the ceremony's own lists. With no network the
# fallback is the bridge's signed expiry; a reachable list revokes; replays are refused.
mkdir "$scratch/state"
check_out 'outcome=unreachable-fell-back-to-expiry sequence=0 revoked=[]' 'unreachable with nothing known falls back to expiry' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state" - "$first"
check_out "outcome=checked sequence=1 revoked=[$first]" 'a reachable list revokes the digest' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state" "$scratch/list1" "$first" "$second"
check_out "outcome=unreachable-fell-back-to-expiry sequence=1 revoked=[$first]" 'an offline installation keeps the last verified list' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state" - "$first" "$second"
check_out 'outcome=checked sequence=2 revoked=[]' 'a newer list advances the floor' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state" "$scratch/list2" "$first"
check_refused 'rollback' 'replaying the older list' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state" "$scratch/list1" "$first"
mkdir "$scratch/t"
cp "$scratch/list1/recovery-revocations.json" "$scratch/t/"
"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
	--key "$scratch/key/other/cosign.key" --bundle "$scratch/t/recovery-revocations.sigstore.json" "$scratch/t/recovery-revocations.json" >/dev/null 2>&1 </dev/null
check_refused 'recovery-bridge:' 'a list with a bad signature (the endpoint answered, so fail closed)' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state" "$scratch/t" "$first"
rm -rf "$scratch/t"
check_out 'outcome=checked sequence=2 revoked=[]' 'the refusals left the remembered floor unchanged' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state" "$scratch/list2" "$first"

# A replayed old list: authentic, but its age exceeds the window, so it never counts as
# checked. The checker falls back to the bridge's own not_after (entries still apply).
mkdir "$scratch/state-stale"
check_out "outcome=stale-fell-back-to-expiry sequence=1 revoked=[$first]" 'an old, validly signed list is stale, never checked' \
	"$ceremony" check-revocations "$scratch/new" "$scratch/state-stale" "$scratch/list-old" "$first" "$second"
rm -rf "$scratch/state-stale" "$scratch/list-old"

step '6e. re-sign: a lapsed bridge is replaced in the catalog, never kept beside its replacement'
resigned_not_after=$(jq -nr '(now | floor) + 1728000 | todate')
"$ceremony" author "$scratch/new" "$proofs/rel100" "$proofs/rel110" "$scratch/statements2" "$resigned_not_after"
"$script_dir/verify-recovery-bridge.sh" unsigned "$scratch/new" "$scratch/statements2" "$proofs/rel100" "$proofs/rel110" >/dev/null
"$ceremony" sign "$scratch/key/cosign.key" "$scratch/statements2" >/dev/null
# Keeping the old digests beside the new ones puts two bridges on one pair: one lapsed
# bridge would then break every plan from the bundle, so the verifier refuses the tree.
"$ceremony" reauthorize "$scratch/key/cosign.key" "$scratch/new" "$scratch/statements2" "$scratch/resign-keep" >/dev/null
expect_fail 'no new bridge repeats the pair of a bridge that stays catalogued' 're-signing without replacing the old digests' \
	"$script_dir/verify-recovery-bridge.sh" proposal "$scratch/new" "$scratch/resign-keep" "$proofs/rel100" "$proofs/rel110"
"$ceremony" reauthorize "$scratch/key/cosign.key" "$scratch/new" "$scratch/statements2" "$scratch/resign" "$first" "$second" >/dev/null
"$script_dir/verify-recovery-bridge.sh" proposal "$scratch/new" "$scratch/resign" "$proofs/rel100" "$proofs/rel110" >/dev/null
printf 'ok: a re-sign that replaces the old digests is accepted\n'
jq -e --arg a "$first" --arg b "$second" '(.bridges | index($a) | not) and (.bridges | index($b) | not)' "$scratch/resign/catalog.json" >/dev/null
test ! -e "$scratch/resign/bridges/$first"
"$ceremony" assemble "$scratch/resign" "$scratch/resign-bundle" "$proofs/rel100" "$proofs/rel110" >/dev/null
# Dropping a bridge with no replacement is a revocation, not a re-sign.
"$ceremony" revoke "$scratch/key/cosign.key" "$scratch/new" "$scratch/dropped" "$first" >/dev/null
expect_fail 'replaced by a new bridge for the same engine, source and target' 'a proposal that drops a bridge without replacing it' \
	"$script_dir/verify-recovery-bridge.sh" proposal "$scratch/new" "$scratch/dropped" "$proofs/rel100" "$proofs/rel110"
rm -rf "$scratch/resign-keep" "$scratch/resign" "$scratch/resign-bundle" "$scratch/dropped" "$scratch/statements2"

step '7. revocation: a new recovery-signed snapshot drops the new bridges, keeps the legacy ones'
# A bridge the stable policy lists needs a policy rotation, so revoke refuses it.
if "$ceremony" revoke "$scratch/key/cosign.key" "$scratch/new" "$scratch/refused-revoke" "$legacy_first" >/dev/null 2>&1; then
	printf 'NOT REFUSED: revoking a policy-listed (legacy) bridge\n'
	exit 1
fi
printf 'refused: revoking a bridge the stable policy lists\n'
"$ceremony" revoke "$scratch/key/cosign.key" "$scratch/new" "$scratch/revoked" "$first" "$second"
"$script_dir/verify-recovery-bridge.sh" revocation "$scratch/new" "$scratch/revoked" "$first" "$second"
jq -e --argjson n "$(jq '.bridges | length' "$scratch/base/catalog.json")" '(.bridges | length) == $n' "$scratch/revoked/catalog.json" >/dev/null

# An existing bridge file may not change in a proposal: only new bridge directories.
cp -R "$scratch/new" "$scratch/t"
printf ' ' >>"$scratch/t/bridges/$legacy_first/statement.json"
expect_fail 'only metadata, catalog' 'a proposal that edits an existing bridge file' \
	"$script_dir/verify-recovery-bridge.sh" proposal "$scratch/base" "$scratch/t" "$proofs/rel100" "$proofs/rel110"
rm -rf "$scratch/t"

# The revocation verifier carries the proposal's integrity checks: a tree with an
# added primary key file, and a re-rooted tree whose metadata and catalog are
# validly signed by another key, are refused by the integrity checks, not by luck.
cp -R "$scratch/revoked" "$scratch/t"
printf 'extra' >"$scratch/t/primary-extra.pub"
expect_fail 'only metadata, catalog' 'a revoke output with an added primary key file' \
	"$script_dir/verify-recovery-bridge.sh" revocation "$scratch/new" "$scratch/t" "$first" "$second"
rm -rf "$scratch/t"
cp -R "$scratch/revoked" "$scratch/t"
recovery_file=$(jq -r '.recovery.public_key' "$scratch/t/root.json")
cp "$scratch/key/other/cosign.pub" "$scratch/t/$recovery_file"
jq --arg sha "$(sha256_of "$scratch/t/$recovery_file")" '.recovery.sha256 = $sha' "$scratch/revoked/root.json" >"$scratch/t/root.json"
for name in metadata catalog; do
	"$COSIGN_BIN" sign-blob --yes --new-bundle-format=false --tlog-upload=false --use-signing-config=false \
		--key "$scratch/key/other/cosign.key" --bundle "$scratch/t/$name.sigstore.json" "$scratch/t/$name.json" >/dev/null 2>&1 </dev/null
done
expect_fail 'root.json is byte-identical' 'a re-rooted revoke output signed by another key' \
	"$script_dir/verify-recovery-bridge.sh" revocation "$scratch/new" "$scratch/t" "$first" "$second"
if grep -q '^FAIL.*signature verifies' "$scratch/expect.out"; then
	printf 'the re-rooted tree must pass the signature checks so only integrity catches it\n'
	exit 1
fi
rm -rf "$scratch/t"
"$ceremony" assemble "$scratch/revoked" "$scratch/revoked-bundle" "$proofs/rel100" "$proofs/rel110"
jq -c '{releases: (.releases | length), bridges: (.bridges | length)}' "$scratch/revoked-bundle/index.json"
# Residual limit: a snapshot revocation binds only instances whose persisted floor
# has advanced past the prior snapshot; the signed list and the 30-day not_after
# cover the rest (a withheld list still ends at the bridge's expiry).

printf '\nRehearsal complete: ceremony steps, verifier and assembler agree; all tamperings refused.\n'
