#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-nightly-manifest-test.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
version=0.0.1-nightly.20260906.24.g90b4ca6a
commit=90b4ca6a5d22438e751cf9af83aa4fd077a6a61c
mkdir "$scratch/payloads"
for os in Linux Darwin Windows; do
 for arch in x86_64 arm64; do
  extension=tar.gz
  [ "$os" != Windows ] || extension=zip
  printf 'fixture archive\n' >"$scratch/payloads/hikyo_${version}_${os}_${arch}.$extension"
 done
done
for format in deb rpm apk archlinux; do
 for arch in amd64 arm64; do
  printf 'fixture package\n' >"$scratch/payloads/$(package_file_name "$version" "$format" "$arch")"
 done
done
for name in checksums.txt binary-provenance.json nightly-policy.json sigstore-trusted-root.json NIGHTLY-BUILD.txt; do
 printf 'fixture public bytes\n' >"$scratch/payloads/$name"
done
jq -n '{schema:"hikyo.dev/nightly-policy/v1"}' >"$scratch/payloads/nightly-policy.json"
jq -n --arg version "$version" --arg commit "$commit" '{schema:"hikyo.dev/upgrade-compatibility/v1",profile:"nightly/v1",version:$version,commit:$commit,sequence:24}' >"$scratch/payloads/upgrade-compatibility.json"
printf 'unbound\n' >"$scratch/payloads/extra.exe"
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'extra executable accepted\n' >&2; exit 1
fi
rm "$scratch/payloads/extra.exe"
mv "$scratch/payloads/checksums.txt" "$scratch/checksums.txt"
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'missing payload accepted\n' >&2; exit 1
fi
mv "$scratch/checksums.txt" "$scratch/payloads/checksums.txt"
"$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads"
jq -e '(.artifacts | length) == 20 and ([.artifacts[] | select(.kind == "binary")] | length) == 6 and ([.artifacts[] | select(.kind == "package")] | length) == 8' "$scratch/payloads/release-manifest.json" >/dev/null
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'existing manifest overwritten\n' >&2; exit 1
fi
rm "$scratch/payloads/release-manifest.json"
printf 'signed disk image arm64\n' >"$scratch/payloads/hikyo_${version}_Darwin_arm64.dmg"
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'partial macOS disk image pair accepted\n' >&2; exit 1
fi
printf 'signed disk image x86_64\n' >"$scratch/payloads/hikyo_${version}_Darwin_x86_64.dmg"
mv "$scratch/payloads/hikyo_${version}_Darwin_arm64.dmg" "$scratch/arm64.dmg"
ln -s "$scratch/arm64.dmg" "$scratch/payloads/hikyo_${version}_Darwin_arm64.dmg"
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'symlinked macOS disk image accepted\n' >&2; exit 1
fi
rm "$scratch/payloads/hikyo_${version}_Darwin_arm64.dmg"
mv "$scratch/arm64.dmg" "$scratch/payloads/hikyo_${version}_Darwin_arm64.dmg"
"$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads"
jq -e --arg sha "$(sha256_file "$scratch/payloads/hikyo_${version}_Darwin_arm64.dmg")" '
 (.artifacts | length) == 22 and
 ([.artifacts[] | select(.kind == "installer")] | length) == 2 and
 ([.artifacts[] | select(.kind == "installer" and .platform == "darwin/arm64" and .sha256 == $sha)] | length) == 1
' "$scratch/payloads/release-manifest.json" >/dev/null

# A legacy policy cannot accidentally emit a dev-source manifest, even when
# callers provide successor inputs. The actual signature/policy verifier has
# separate real-Sigstore tests; these fixtures exercise the shell handoff.
rm "$scratch/payloads/release-manifest.json"
publisher=cccccccccccccccccccccccccccccccccccccccc
jq -n --arg commit "$commit" --arg publisher "$publisher" '{schema:"hikyo.dev/source-validation/v1",repository:"synthetic/hikyo",repository_id:123,commit:$commit,ref:"refs/heads/dev",workflow_path:".github/workflows/ci-dev.yml",event:"push",run_id:42,run_attempt:2,workflow_id:314,authority_commit:$publisher,configuration_sha256:("d"*64)}' >"$scratch/source-proof.json"
if HIKYO_NIGHTLY_PUBLISHER_COMMIT="$publisher" HIKYO_NIGHTLY_SOURCE_PROOF="$scratch/source-proof.json" \
 "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'legacy policy accepted dev publication inputs\n' >&2; exit 1
fi
jq -n '{schema:"hikyo.dev/nightly-policy/v2"}' >"$scratch/payloads/nightly-policy.json"
if "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'dev policy accepted missing publisher/source proof\n' >&2; exit 1
fi
printf '{}\n' >"$scratch/payloads/source-validation.json"
if HIKYO_NIGHTLY_PUBLISHER_COMMIT="$publisher" HIKYO_NIGHTLY_SOURCE_PROOF="$scratch/source-proof.json" \
 "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'substituted retained source receipt accepted\n' >&2; exit 1
fi
cp "$scratch/source-proof.json" "$scratch/payloads/source-validation.json"
mkdir "$scratch/bin"
cat >"$scratch/bin/go" <<'GO_FIXTURE'
#!/bin/sh
set -eu
[ "$1" = run ] && [ "$2" = ./scripts/release/nightly ] && [ "$3" = source-validation ]
shift 3
[ "$1" = --source-proof ] && [ "$3" = --source-commit ] && [ "$5" = --publisher-commit ] && [ "$7" = --out ] && [ "$#" -eq 8 ]
[ "${MANIFEST_FIXTURE_REFUSE:-false}" = false ] || exit 19
# shellcheck disable=SC1090
. "$MANIFEST_FIXTURE_RELEASE_LIB"
jq --arg digest "$(sha256_file "$2")" --arg source "$4" --arg publisher "$6" '
 select(.commit == $source and .authority_commit == $publisher) | . + {proof_sha256:$digest}
' "$2" >"$8"
GO_FIXTURE
chmod +x "$scratch/bin/go"
if PATH="$scratch/bin:$PATH" MANIFEST_FIXTURE_RELEASE_LIB="$script_dir/../lib/release.sh" MANIFEST_FIXTURE_REFUSE=true \
 HIKYO_NIGHTLY_PUBLISHER_COMMIT="$publisher" HIKYO_NIGHTLY_SOURCE_PROOF="$scratch/source-proof.json" \
 "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads" >/dev/null 2>&1; then
 printf 'canonical proof verifier refusal was ignored\n' >&2; exit 1
fi
[ ! -e "$scratch/payloads/release-manifest.json" ]
PATH="$scratch/bin:$PATH" MANIFEST_FIXTURE_RELEASE_LIB="$script_dir/../lib/release.sh" \
 HIKYO_NIGHTLY_PUBLISHER_COMMIT="$publisher" HIKYO_NIGHTLY_SOURCE_PROOF="$scratch/source-proof.json" \
 "$script_dir/create-nightly-manifest.sh" "$version" "$commit" 24 "$scratch/payloads"
jq -e --arg source "$commit" --arg publisher "$publisher" --arg digest "$(sha256_file "$scratch/source-proof.json")" '
 .schema == "hikyo.dev/nightly-manifest/v2" and .source_commit == $source and
 .publisher_commit == $publisher and .source_validation.commit == $source and
 .source_validation.authority_commit == $publisher and .source_validation.proof_sha256 == $digest and
 (.artifacts | length) == 23 and
 ([.artifacts[] | select(.name == "source-validation.json" and .kind == "build-provenance" and .sha256 == $digest)] | length) == 1
' "$scratch/payloads/release-manifest.json" >/dev/null
printf 'nightly manifest: historical inventory and canonical disk image pair bound; extra, missing, symlink, partial pair and overwrite refused\n'
