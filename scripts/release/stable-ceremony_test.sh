#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-online-ceremony.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir -p "$fixture/repo/scripts/release" "$fixture/repo/scripts/lib" "$fixture/repo/scripts/git" \
  "$fixture/repo/scripts/ci" "$fixture/repo/release/trust/nightly" "$fixture/bin" "$fixture/state"
cp "$script_dir/stable-ceremony.sh" "$fixture/repo/scripts/release/"
cp "$script_dir/../lib/release.sh" "$fixture/repo/scripts/lib/"
cat >"$fixture/repo/scripts/release/publish-stable-draft.sh" <<'SH'
#!/bin/sh
set -eu
printf 'publication helper %s state=%s\n' "$*" "$HIKYO_RELEASE_TRUST_STATE" >>"$FIXTURE_LOG"
SH
chmod +x "$fixture/repo/scripts/release/publish-stable-draft.sh"
printf 'trusted root\n' >"$fixture/repo/release/trust/nightly/trusted-root.json"
for helper in git/install-hooks.sh ci/check-commit-signatures.sh; do
  printf '#!/bin/sh\nexit 0\n' >"$fixture/repo/scripts/$helper"
  chmod +x "$fixture/repo/scripts/$helper"
done
cat >"$fixture/bin/git" <<'SH'
#!/bin/sh
set -eu
printf 'git %s\n' "$*" >>"$FIXTURE_LOG"
case "$*" in
  'status --porcelain') [ "$FIXTURE_MODE" != dirty ] || printf ' M user-file\n' ;;
  'fetch '*) ;;
  'config --get commit.gpgsign') printf 'true\n' ;;
  'show-ref --verify --quiet refs/heads/'*) case "$FIXTURE_MODE" in existing*|unrelated|different-policy|diverged) exit 0 ;; *) exit 1 ;; esac ;;
  'ls-remote --heads '*) case "$FIXTURE_MODE" in existing*|unrelated|different-policy|diverged) printf '2222222222222222222222222222222222222222 refs/heads/release/authorize-stable-workflow\n' ;; esac ;;
  'rev-parse refs/remotes/'*) printf '2222222222222222222222222222222222222222\n' ;;
  'rev-parse release/'*) if [ "$FIXTURE_MODE" = diverged ]; then printf '3333333333333333333333333333333333333333\n'; else printf '2222222222222222222222222222222222222222\n'; fi ;;
  'rev-parse '*|'merge-base '*) printf '1111111111111111111111111111111111111111\n' ;;
  'diff --name-only '*)
    printf 'release/trust/stable-policy.json\nrelease/trust/stable-policy.sigstore.json\nrelease/trust/stable-trusted-root.json\n'
    [ "$FIXTURE_MODE" != unrelated ] || printf 'user-file\n' ;;
  'worktree add --detach '*)
    directory=$4
    mkdir "$directory"
    cp -R "$FIXTURE_SOURCE/." "$directory/"
    printf '{"schema":"test-policy"}\n' >"$directory/release/trust/stable-policy.json"
    [ "$FIXTURE_MODE" != different-policy ] || printf '{"schema":"other-policy"}\n' >"$directory/release/trust/stable-policy.json"
    printf 'existing recovery signature\n' >"$directory/release/trust/stable-policy.sigstore.json"
    cp "$directory/release/trust/nightly/trusted-root.json" "$directory/release/trust/stable-trusted-root.json" ;;
  'worktree remove '*) ;;
  'push '*) [ "$FIXTURE_MODE" != existing-push-fails ] ;;
  *) printf 'unexpected git mutation: %s\n' "$*" >&2; exit 2 ;;
esac
SH
cat >"$fixture/bin/go" <<'SH'
#!/bin/sh
set -eu
printf 'go %s\n' "$*" >>"$FIXTURE_LOG"
case "$*" in
  *' run -p 2 ./scripts/ci/source-proof --repository owner/repo --mode main --commit '*) exit 0 ;;
  'run ./scripts/release/stable policy '*)
    for output; do :; done
    printf '{"schema":"test-policy"}\n' >"$output" ;;
  'run ./scripts/release/stable preflight '*)
    case "$FIXTURE_MODE" in inactive|invalid-signature) exit 1 ;; esac ;;
  'run ./scripts/release/stable verify '*) ;;
  *) printf 'unexpected go command\n' >&2; exit 2 ;;
esac
SH
cat >"$fixture/bin/gh" <<'SH'
#!/bin/sh
set -eu
printf 'gh %s\n' "$*" >>"$FIXTURE_LOG"
case "$*" in
  'api '*) printf 'true\n' ;;
  'pr list '*) printf '[{"url":"https://example.test/existing-pr"}]\n' ;;
  'release view '*) printf 'false\n' ;;
  'release download '*)
    for directory; do :; done
    printf '{"source_commit":"1111111111111111111111111111111111111111"}\n' >"$directory/release-manifest.json" ;;
  *) printf 'unexpected GitHub mutation\n' >&2; exit 2 ;;
esac
SH
cat >"$fixture/bin/signer" <<'SH'
#!/bin/sh
set -eu
printf 'signer called\n' >>"$FIXTURE_LOG"
printf 'fixture signature\n' >"$4"
SH
chmod +x "$fixture/bin/"*
export PATH="$fixture/bin:$PATH" FIXTURE_LOG="$fixture/log" FIXTURE_SOURCE="$fixture/repo"
export HIKYO_RELEASE_STATE_DIR="$fixture/state" HIKYO_RECOVERY_SIGNER="$fixture/bin/signer"
ceremony="$fixture/repo/scripts/release/stable-ceremony.sh"
export FIXTURE_MODE=valid
: >"$FIXTURE_LOG"
"$ceremony" --help >"$fixture/output"
grep -F 'no USB media or network disconnection' "$fixture/output" >/dev/null
[ ! -s "$FIXTURE_LOG" ] || { printf 'ceremony fixture: help performed external work\n' >&2; exit 1; }
for command in 'unknown' 'publish 1.0.0 badhash'; do
  # Intentional argument splitting: fixture commands contain no user data.
  # shellcheck disable=SC2086
  if "$ceremony" $command >"$fixture/output" 2>&1; then exit 1; fi
done
for FIXTURE_MODE in dirty inactive; do
  export FIXTURE_MODE
  if "$ceremony" build 1.0.0 >"$fixture/output" 2>&1; then exit 1; fi
done
export FIXTURE_MODE=valid
if printf 'decline\n' | "$ceremony" setup >"$fixture/output" 2>&1; then exit 1; fi
if grep -E '^signer|^git (commit|push|tag)|^gh' "$FIXTURE_LOG" >/dev/null; then
  printf 'ceremony fixture: refusal caused signing or publication\n' >&2; exit 1
fi
export FIXTURE_MODE=invalid-signature
if printf 'authorize stable release workflow\n' | "$ceremony" setup >"$fixture/output" 2>&1; then exit 1; fi
[ ! -e "$fixture/repo/release/trust/stable-policy.json" ] || exit 1
if grep -E '^git (commit|push|tag)|^gh' "$FIXTURE_LOG" >/dev/null; then exit 1; fi
for FIXTURE_MODE in unrelated different-policy diverged existing-push-fails existing; do
  export FIXTURE_MODE
  : >"$FIXTURE_LOG"
  if "$ceremony" setup >"$fixture/output" 2>&1; then
    [ "$FIXTURE_MODE" = existing ] || { cat "$fixture/output" >&2; exit 1; }
  else
    [ "$FIXTURE_MODE" != existing ] || { cat "$fixture/output" >&2; exit 1; }
  fi
  if grep -E '^signer|^git (commit|add|switch|reset|tag)|^gh pr create|--force' "$FIXTURE_LOG" >/dev/null; then
    printf 'ceremony fixture: retry signed, overwrote, or duplicated work\n' >&2; exit 1
  fi
  [ ! -e "$fixture/repo/release/trust/stable-policy.json" ] || exit 1
done
grep -F 'https://example.test/existing-pr' "$fixture/output" >/dev/null
export FIXTURE_MODE=valid HIKYO_RELEASE_TRUST_STATE="$fixture/persistent-trust.json"
: >"$FIXTURE_LOG"
"$ceremony" review 1.0.0 >"$fixture/output"
grep -F -- "--state $HIKYO_RELEASE_TRUST_STATE --latest --published" "$FIXTURE_LOG" >/dev/null
reviewed=$(awk '/Manifest SHA-256:/ {print $3}' "$fixture/output")
if FIXTURE_MODE=dirty "$ceremony" publish 1.0.0 "$reviewed" >"$fixture/output" 2>&1; then exit 1; fi
if "$ceremony" publish 1.0.0 "$(printf '%064d' 0)" >"$fixture/output" 2>&1; then exit 1; fi
if printf 'decline\n' | "$ceremony" publish 1.0.0 "$reviewed" >"$fixture/output" 2>&1; then exit 1; fi
if grep -F 'publication helper' "$FIXTURE_LOG" >/dev/null; then
  printf 'ceremony fixture: refusal reached publication helper\n' >&2; exit 1
fi
printf 'publish v1.0.0 %s\n' "$reviewed" | "$ceremony" publish 1.0.0 "$reviewed" >"$fixture/output"
grep -F "publication helper Hikyo-Org/Hikyo v1.0.0 $reviewed release/trust state=$HIKYO_RELEASE_TRUST_STATE" "$FIXTURE_LOG" >/dev/null
if grep -E '^gh workflow|^signer|^git (tag|commit|push)' "$FIXTURE_LOG" >/dev/null; then
  printf 'ceremony fixture: local publication signed or dispatched CI\n' >&2; exit 1
fi
if printf 'decline\n' | "$ceremony" homebrew 1.0.0 >"$fixture/output" 2>&1; then exit 1; fi
if grep -E '^gh (pr create|api --method)|^git (push|commit|tag)' "$FIXTURE_LOG" >/dev/null; then
  printf 'ceremony fixture: declined Homebrew publication mutated state\n' >&2; exit 1
fi
printf 'online ceremony fixture: help/refusals, no activation on invalid signing, safe existing-branch retries passed\n'
