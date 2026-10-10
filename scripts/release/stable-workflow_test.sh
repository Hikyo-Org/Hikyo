#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-stable-workflow.XXXXXX")
trap 'rm -rf "$scratch"' EXIT HUP INT TERM
mkdir "$scratch/bin" "$scratch/dist"
commit=1111111111111111111111111111111111111111
export FIXTURE_COMMIT=$commit FIXTURE_MODE=valid FIXTURE_SCRATCH="$scratch"
cat >"$scratch/bin/gh" <<'SH'
#!/bin/sh
set -eu
case "$*" in
  'api --paginate --slurp repos/owner/repo/releases?per_page=100')
    case "$FIXTURE_MODE" in
      unsynced) printf '[[{"draft":false,"tag_name":"v1.0.0"}]]\n' ;;
      outstanding-draft) printf '[[{"draft":true,"tag_name":"v1.0.0"}]]\n' ;;
      *) printf '[[]]\n' ;;
    esac ;;
  'api repos/owner/repo/immutable-releases')
    enabled=true
    [ "$FIXTURE_MODE" != mutable ] || enabled=false
    jq -nc --argjson enabled "$enabled" '{enabled:$enabled}' ;;
  'api repos/owner/repo/git/ref/tags/v1.0.0')
    type=tag
    [ "$FIXTURE_MODE" != lightweight ] || type=commit
    jq -nc --arg type "$type" '{object:{type:$type,sha:("2"*40)}}' ;;
  'api repos/owner/repo/git/tags/'*)
    verified=true commit=$FIXTURE_COMMIT
    [ "$FIXTURE_MODE" != unsigned ] || verified=false
    [ "$FIXTURE_MODE" != wrong-commit ] || commit=3333333333333333333333333333333333333333
    jq -nc --argjson verified "$verified" --arg commit "$commit" '{tag:"v1.0.0",object:{type:"commit",sha:$commit},verification:{verified:$verified,reason:"valid"}}' ;;
  'api repos/owner/repo/actions/workflows/ci.yml/runs?'*)
    conclusion=success
    [ "$FIXTURE_MODE" != red-ci ] || conclusion=failure
    jq -nc --arg commit "$FIXTURE_COMMIT" --arg conclusion "$conclusion" '{workflow_runs:[{head_sha:$commit,head_branch:"main",event:"workflow_dispatch",path:".github/workflows/ci.yml",status:"completed",conclusion:$conclusion}]}' ;;
  'api repos/owner/repo/actions/workflows/release.yml/runs?'*)
    conclusion=success
    [ "$FIXTURE_MODE" != red-release ] || conclusion=failure
    jq -nc --arg commit "$FIXTURE_COMMIT" --arg conclusion "$conclusion" '{workflow_runs:[{head_sha:$commit,head_branch:"v1.0.0",status:"completed",conclusion:$conclusion,run_number:1,run_attempt:1}]}' ;;
  'api repos/owner/repo/releases/tags/v1.0.0')
    id=1 draft=true immutable=true
    [ ! -f "$FIXTURE_SCRATCH/published" ] || draft=false
    if [ "$FIXTURE_MODE" = public-mutable ]; then draft=false; immutable=false; fi
    if [ "$FIXTURE_MODE" = changed-draft ] && [ -f "$FIXTURE_SCRATCH/downloaded" ]; then id=2; fi
    jq -nc --argjson id "$id" --argjson draft "$draft" --argjson immutable "$immutable" '{id:1,tag_name:"v1.0.0",draft:$draft,immutable:(($draft|not) and $immutable),assets:[{id:$id,name:"release-manifest.json",size:1,updated_at:"2026-01-01"}]}' ;;
  'release download '*)
    for last; do :; done
    cp "$FIXTURE_SCRATCH/manifest.json" "$last/release-manifest.json"
    touch "$FIXTURE_SCRATCH/downloaded" ;;
  'release edit '*) touch "$FIXTURE_SCRATCH/published" "$FIXTURE_SCRATCH/edited" ;;
  *) printf 'unexpected gh invocation: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$scratch/bin/go" <<'SH'
#!/bin/sh
set -eu
case "$*" in
  'run ./scripts/release/stable verify '*) ;;
  'run ./scripts/release/stable prepare '*)
    for last; do :; done
    printf '{}\n' >"$last/metadata.json"
    printf '{}\n' >"$last/catalog.json" ;;
  *) exit 2 ;;
esac
[ "$FIXTURE_MODE" != bad-signature ]
SH
cat >"$scratch/bin/cosign" <<'SH'
#!/bin/sh
set -eu
[ "$FIXTURE_MODE" != bad-oci ] || exit 1
if [ "$1" = sign-blob ]; then
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --bundle ]; then printf 'oidc signature\n' >"$2"; break; fi
    shift
  done
fi
SH
chmod +x "$scratch/bin/gh" "$scratch/bin/go" "$scratch/bin/cosign"
export GH_BIN="$scratch/bin/gh" COSIGN_BIN="$scratch/bin/cosign"
"$script_dir/require-signed-tag.sh" owner/repo v1.0.0 "$commit" >/dev/null
for FIXTURE_MODE in lightweight unsigned wrong-commit red-ci; do
  export FIXTURE_MODE
  if "$script_dir/require-signed-tag.sh" owner/repo v1.0.0 "$commit" >"$scratch/error" 2>&1; then
    printf 'stable fixture: invalid tag accepted: %s\n' "$FIXTURE_MODE" >&2; exit 1
  fi
done
export FIXTURE_MODE=valid
printf '{"releases":[],"highest_release":null}\n' >"$scratch/metadata.json"
"$script_dir/require-synced-trust.sh" owner/repo "$scratch/metadata.json" >/dev/null
for mode in unsynced outstanding-draft; do
  if FIXTURE_MODE="$mode" "$script_dir/require-synced-trust.sh" owner/repo "$scratch/metadata.json" >"$scratch/error" 2>&1; then
    printf 'stable fixture: conflicting release accepted: %s\n' "$mode" >&2; exit 1
  fi
done
jq -ncS --arg commit "$commit" '{version:"1.0.0",sequence:1,commit:$commit,key_id:"github-actions-stable",public_key:"stable-policy.json"}' >"$scratch/candidate.json"
printf 'artifact\n' >"$scratch/dist/file.tar.gz"
printf 'recovery signature\n' >"$scratch/dist/stable-policy.sigstore.json"
export GITHUB_REPOSITORY=owner/repo GITHUB_REF=refs/tags/v1.0.0 GITHUB_SHA=$commit GITHUB_RUN_ID=42 GITHUB_RUN_ATTEMPT=1
"$script_dir/create-build-provenance.sh" "$scratch/dist" "$scratch/candidate.json"
jq -e --arg commit "$commit" '
  ._type == "https://in-toto.io/Statement/v1" and .predicateType == "https://slsa.dev/provenance/v1" and
  ([.subject[].name] == ["file.tar.gz","stable-policy.sigstore.json"]) and
  .predicate.buildDefinition.resolvedDependencies[0].digest.gitCommit == $commit and
  .predicate.runDetails.builder.id == "https://github.com/owner/repo/.github/workflows/release.yml@refs/tags/v1.0.0"
' "$scratch/dist/build-provenance.json" >/dev/null
rm "$scratch/dist/build-provenance.json"
if GITHUB_REF=refs/heads/main "$script_dir/create-build-provenance.sh" "$scratch/dist" "$scratch/candidate.json" >"$scratch/error" 2>&1; then
  printf 'stable fixture: wrong provenance identity accepted\n' >&2; exit 1
fi
jq -nc --arg commit "$commit" '{version:"1.0.0",source_commit:$commit,artifacts:[{kind:"image",image:"ghcr.io/owner/repo",digest:("sha256:"+("2"*64))},{kind:"chart-digest",chart:"ghcr.io/owner/chart",digest:("sha256:"+("3"*64))}]}' >"$scratch/manifest.json"
# shellcheck disable=SC1091
. "$script_dir/../lib/release.sh"
digest=$(sha256_file "$scratch/manifest.json")
export PATH="$scratch/bin:$PATH"
for FIXTURE_MODE in bad-signature bad-oci red-release changed-draft mutable; do
  export FIXTURE_MODE
  rm -f "$scratch/downloaded" "$scratch/published"
  if "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >"$scratch/error" 2>&1; then
    printf 'stable fixture: unsafe publication accepted: %s\n' "$FIXTURE_MODE" >&2; exit 1
  fi
  [ ! -e "$scratch/published" ] || { printf 'stable fixture: failure published draft\n' >&2; exit 1; }
done
export FIXTURE_MODE=valid
rm -f "$scratch/downloaded"
if "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$(printf '%064d' 0)" "$scratch" >"$scratch/error" 2>&1; then
  printf 'stable fixture: unreviewed manifest published\n' >&2; exit 1
fi
[ ! -e "$scratch/published" ] || exit 1
"$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >/dev/null
[ -e "$scratch/published" ] || exit 1
rm "$scratch/edited"
for FIXTURE_MODE in public-mutable bad-signature bad-oci; do
  export FIXTURE_MODE
  if "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >"$scratch/error" 2>&1; then
    printf 'stable fixture: unsafe public retry accepted: %s\n' "$FIXTURE_MODE" >&2; exit 1
  fi
  [ ! -e "$scratch/edited" ] || { printf 'stable fixture: public retry edited release\n' >&2; exit 1; }
done
export FIXTURE_MODE=valid
if "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$(printf '%064d' 0)" "$scratch" >"$scratch/error" 2>&1; then
  printf 'stable fixture: public retry accepted different reviewed hash\n' >&2; exit 1
fi
"$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >/dev/null
[ ! -e "$scratch/edited" ] || { printf 'stable fixture: public retry edited release\n' >&2; exit 1; }
mkdir "$scratch/signing"
cp "$scratch/manifest.json" "$scratch/signing/release-manifest.json"
printf 'policy\n' >"$scratch/signing/stable-policy.json"
printf 'recovery signature\n' >"$scratch/signing/stable-policy.sigstore.json"
printf 'artifact\n' >"$scratch/signing/build-provenance.json"
export GITHUB_ACTIONS=true GITHUB_WORKFLOW_REF=owner/repo/.github/workflows/release.yml@refs/tags/v1.0.0
"$script_dir/sign-stable-draft.sh" "$scratch" "$scratch/signing" >/dev/null
for name in release-manifest.sigstore.json metadata.sigstore.json catalog.sigstore.json build-provenance.json.sigstore.json; do
  [ -f "$scratch/signing/$name" ] || { printf 'stable fixture: missing signing envelope %s\n' "$name" >&2; exit 1; }
done
[ "$(cat "$scratch/signing/stable-policy.sigstore.json")" = 'recovery signature' ] || exit 1
[ ! -e "$scratch/signing/stable-policy.sigstore.json.sigstore.json" ] || exit 1
printf 'stable workflow fixture: tag, CI, provenance and publication refusal cases passed\n'
