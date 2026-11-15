#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
"$script_dir/../ci/test-core-packages_test.sh"
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
    if [ "${FIXTURE_PUBLICATION:-false}" = true ]; then
      if [ "$FIXTURE_MODE" = post-list-failed ] && [ -f "$FIXTURE_SCRATCH/published" ]; then exit 1; fi
      if [ -f "$FIXTURE_SCRATCH/downloaded" ]; then
        case "$FIXTURE_MODE" in
          recheck-lookup-failed) exit 1 ;;
          recheck-missing) printf '[[]]\n'; exit 0 ;;
          replaced-release) printf '[[{"id":2,"tag_name":"v1.0.0"}]]\n'; exit 0 ;;
        esac
      fi
      case "$FIXTURE_MODE" in
        lookup-failed) exit 1 ;;
        missing-release) printf '[[]]\n' ;;
        malformed-pages) printf '[{}]\n' ;;
        malformed-release-rows) printf '[[null,{}, {"id":1,"tag_name":"v1.0.0"}]]\n' ;;
        wrong-row-tag-type) printf '[[{"id":99,"tag_name":{}},{"id":1,"tag_name":"v1.0.0"}]]\n' ;;
        invalid-unrelated-id) printf '[[{"id":-1,"tag_name":"v9.9.9"},{"id":1,"tag_name":"v1.0.0"}]]\n' ;;
        duplicate-release) printf '[[{"id":1,"tag_name":"v1.0.0"}],[{"id":2,"tag_name":"v1.0.0"}]]\n' ;;
        invalid-release-id) printf '[[{"id":"1","tag_name":"v1.0.0"}]]\n' ;;
        fractional-release-id) printf '[[{"id":1.5,"tag_name":"v1.0.0"}]]\n' ;;
        zero-release-id) printf '[[{"id":0,"tag_name":"v1.0.0"}]]\n' ;;
        *) printf '[[{"id":99,"tag_name":"v9.9.9"}],[{"id":1,"tag_name":"v1.0.0"}]]\n' ;;
      esac
      exit 0
    fi
    case "$FIXTURE_MODE" in
      list-failed) exit 1 ;;
      unsynced) printf '[[{"draft":false,"tag_name":"v1.0.0"}]]\n' ;;
      outstanding-draft) printf '[[{"draft":true,"tag_name":"v1.0.0"}]]\n' ;;
      prerelease-published) printf '[[{"draft":false,"tag_name":"v1.2.0-rc.1"},{"draft":false,"tag_name":"v1.1.0"},{"draft":false,"tag_name":"v1.0.0"}]]\n' ;;
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
  'api repos/owner/repo/actions/workflows/ci-main.yml/runs?'*)
    conclusion=success
    [ "$FIXTURE_MODE" != red-ci ] || conclusion=failure
    jq -nc --arg commit "$FIXTURE_COMMIT" --arg conclusion "$conclusion" '{total_count:1,workflow_runs:[{id:1,head_sha:$commit,head_branch:"main",event:"workflow_dispatch",path:".github/workflows/ci-main.yml",status:"completed",conclusion:$conclusion}]}' ;;
  'api repos/owner/repo/actions/workflows/ci.yml/runs?'*)
    printf '{"total_count":0,"workflow_runs":[]}\n' ;;
  'api repos/owner/repo/actions/workflows/release.yml/runs?'*)
    # Lookup of the tag authority (a single page).
    jq -nc --arg commit "$FIXTURE_COMMIT" '{workflow_runs:[{id:7,event:"push",head_sha:$commit,head_branch:"v1.0.0",status:"completed",conclusion:"success",run_number:1,run_attempt:1}]}' ;;
  'api --paginate --slurp repos/owner/repo/actions/workflows/release.yml/runs?'*)
    # Build lookup of the publication helper (all pages).
    conclusion=success path=.github/workflows/release.yml
    [ "$FIXTURE_MODE" != red-release ] || conclusion=failure
    [ "$FIXTURE_MODE" != prefix-build-path ] || path=.github/workflows/release.yml.evil
    jq -nc --arg commit "$FIXTURE_COMMIT" --arg conclusion "$conclusion" --arg path "$path" '[{workflow_runs:[{id:7,path:$path,event:"push",head_sha:$commit,head_branch:"v1.0.0",status:"completed",conclusion:$conclusion,run_number:1,run_attempt:1}]}]' ;;
  'api repos/owner/repo/actions/runs/7')
    # The run a person pushed: the tag authority and a bound build check read this record.
    conclusion=success
    [ "$FIXTURE_MODE" != red-bound-run ] || conclusion=failure
    jq -nc --arg commit "$FIXTURE_COMMIT" --arg conclusion "$conclusion" '{id:7,path:".github/workflows/release.yml",event:"push",head_branch:"v1.0.0",head_sha:$commit,status:"completed",conclusion:$conclusion,repository:{full_name:"owner/repo"},actor:{id:1,type:"User",login:"maintainer"},triggering_actor:{id:1,type:"User",login:"maintainer"}}' ;;
  'api repos/owner/repo/releases/tags/v1.0.0')
    printf 'gh: Not Found (HTTP 404)\n' >&2; exit 1 ;;
  'api repos/owner/repo/releases/1' | 'api repos/owner/repo/releases/2')
    id=1 release_id=${2##*/} tag=v1.0.0 draft=true immutable=true
    [ ! -f "$FIXTURE_SCRATCH/published" ] || draft=false
    if [ "$FIXTURE_MODE" = public-mutable ]; then draft=false; immutable=false; fi
    if [ "$FIXTURE_MODE" = changed-draft ] && [ -f "$FIXTURE_SCRATCH/downloaded" ]; then id=2; fi
    [ "$FIXTURE_MODE" != wrong-selected-id ] || release_id=2
    [ "$FIXTURE_MODE" != wrong-selected-tag ] || tag=v2.0.0
    if [ "$FIXTURE_MODE" = recheck-wrong-tag ] && [ -f "$FIXTURE_SCRATCH/downloaded" ]; then tag=v2.0.0; fi
    if [ -f "$FIXTURE_SCRATCH/published" ]; then
      case "$FIXTURE_MODE" in
        post-wrong-id) release_id=2 ;;
        post-wrong-tag) tag=v2.0.0 ;;
        post-draft) draft=true ;;
        post-mutable) immutable=false ;;
      esac
    fi
    jq -nc --argjson release "$release_id" --arg tag "$tag" --argjson id "$id" --argjson draft "$draft" --argjson immutable "$immutable" '{id:$release,tag_name:$tag,draft:$draft,immutable:(($draft|not) and $immutable),assets:[{id:$id,name:"release-manifest.json",size:1,updated_at:"2026-01-01"}]}' ;;
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
  *' run -p 2 ./scripts/ci/source-proof --repository owner/repo --mode main --commit '*)
    args=$*
    [ "${args##* --commit }" = "$FIXTURE_COMMIT --authority-commit $FIXTURE_COMMIT" ]
    [ "$FIXTURE_MODE" != red-ci ]
    exit 0 ;;
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
export GH_BIN="$scratch/bin/gh" GO_BIN="$scratch/bin/go" COSIGN_BIN="$scratch/bin/cosign"
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
# A person's prerelease that trust never recorded blocks the next candidate; once recorded, and
# even as the trust's highest release while the latest stable is older, nothing is out of sync.
printf '{"releases":[{"version":"1.0.0"},{"version":"1.1.0"}],"highest_release":"1.1.0"}\n' >"$scratch/prerelease-metadata.json"
if FIXTURE_MODE=prerelease-published "$script_dir/require-synced-trust.sh" owner/repo "$scratch/prerelease-metadata.json" >"$scratch/error" 2>&1; then
  printf 'stable fixture: an unsynchronized human prerelease was accepted\n' >&2; exit 1
fi
printf '{"releases":[{"version":"1.0.0"},{"version":"1.1.0"},{"version":"1.2.0-rc.1"}],"highest_release":"1.2.0-rc.1"}\n' >"$scratch/prerelease-metadata.json"
FIXTURE_MODE=prerelease-published "$script_dir/require-synced-trust.sh" owner/repo "$scratch/prerelease-metadata.json" >/dev/null ||
  { printf 'stable fixture: a synchronized prerelease as highest release was refused\n' >&2; exit 1; }
# Trust that names a highest release nobody published is out of sync too.
printf '{"releases":[{"version":"1.0.0"},{"version":"1.5.0"}],"highest_release":"1.5.0"}\n' >"$scratch/prerelease-metadata.json"
if FIXTURE_MODE=unsynced "$script_dir/require-synced-trust.sh" owner/repo "$scratch/prerelease-metadata.json" >"$scratch/error" 2>&1; then
  printf 'stable fixture: a trust highest release that is not published was accepted\n' >&2; exit 1
fi
# A failed listing is not "unsynchronized": it exits 4 so callers fail instead of deciding.
rc=0
FIXTURE_MODE=list-failed "$script_dir/require-synced-trust.sh" owner/repo "$scratch/metadata.json" >"$scratch/error" 2>&1 || rc=$?
[ "$rc" = 4 ] || { printf 'stable fixture: a failed release listing exited %s, want 4\n' "$rc" >&2; exit 1; }
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
export FIXTURE_PUBLICATION=true
for FIXTURE_MODE in bad-signature bad-oci red-release prefix-build-path changed-draft mutable lookup-failed missing-release malformed-pages malformed-release-rows wrong-row-tag-type invalid-unrelated-id duplicate-release invalid-release-id fractional-release-id zero-release-id wrong-selected-id wrong-selected-tag replaced-release recheck-lookup-failed recheck-missing recheck-wrong-tag; do
  export FIXTURE_MODE
  rm -f "$scratch/downloaded" "$scratch/published"
  if "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >"$scratch/error" 2>&1; then
    printf 'stable fixture: unsafe publication accepted: %s\n' "$FIXTURE_MODE" >&2; exit 1
  fi
  [ ! -e "$scratch/published" ] || { printf 'stable fixture: failure published draft\n' >&2; exit 1; }
done
# A build bound by HIKYO_RELEASE_RUN_ID is judged on that run, not on the newest run of the tag.
export FIXTURE_MODE=red-bound-run
rm -f "$scratch/downloaded" "$scratch/published"
if HIKYO_RELEASE_RUN_ID=7 "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >"$scratch/error" 2>&1; then
  printf 'stable fixture: a failed bound run was published\n' >&2; exit 1
fi
grep -F 'exact tag build must succeed' "$scratch/error" >/dev/null || { cat "$scratch/error" >&2; printf 'stable fixture: bound run refused for the wrong reason\n' >&2; exit 1; }
[ ! -e "$scratch/published" ] || { printf 'stable fixture: failed bound run published draft\n' >&2; exit 1; }
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
HIKYO_RELEASE_RUN_ID=7 "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >/dev/null
for FIXTURE_MODE in post-wrong-id post-wrong-tag post-draft post-mutable; do
  export FIXTURE_MODE
  rm -f "$scratch/downloaded" "$scratch/published" "$scratch/edited"
  if "$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >"$scratch/error" 2>&1; then
    printf 'stable fixture: invalid post-publication release accepted: %s\n' "$FIXTURE_MODE" >&2; exit 1
  fi
  [ -e "$scratch/published" ] || { printf 'stable fixture: post-publication check was not reached\n' >&2; exit 1; }
done
export FIXTURE_MODE=post-list-failed
rm -f "$scratch/downloaded" "$scratch/published" "$scratch/edited"
"$script_dir/publish-stable-draft.sh" owner/repo v1.0.0 "$digest" "$scratch" >/dev/null
[ -e "$scratch/published" ] || exit 1
export FIXTURE_MODE=valid
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
