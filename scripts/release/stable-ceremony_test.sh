#!/bin/sh
set -eu
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-online-ceremony.XXXXXX")
trap 'rm -rf "$fixture"' EXIT HUP INT TERM
mkdir -p "$fixture/repo/scripts/release" "$fixture/repo/scripts/lib" "$fixture/repo/scripts/git" \
  "$fixture/repo/scripts/ci" "$fixture/repo/release/trust/nightly" "$fixture/bin" "$fixture/state"
cp "$script_dir/stable-ceremony.sh" "$fixture/repo/scripts/release/"
cp "$script_dir/../lib/release.sh" "$fixture/repo/scripts/lib/"
cp "$script_dir/release-channel.sh" "$fixture/repo/scripts/release/"
cat >"$fixture/repo/scripts/release/publish-desktop-draft.sh" <<'SH'
#!/bin/sh
set -eu
printf 'desktop helper %s state=%s\n' "$*" "${HIKYO_RELEASE_TRUST_STATE:-unset}" >>"$FIXTURE_LOG"
[ "$FIXTURE_MODE" != desktop-fails ] || [ "${1:-}" = --check-only ]
[ "$FIXTURE_MODE" != desktop-check-fails ] || [ "${1:-}" != --check-only ]
SH
chmod +x "$fixture/repo/scripts/release/publish-desktop-draft.sh"
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
  'show-ref --verify --quiet refs/heads/'*) case "$FIXTURE_MODE" in existing*|unrelated|different-policy|diverged|setup-ledger|sync-*) exit 0 ;; *) exit 1 ;; esac ;;
  'ls-remote --heads '*) case "$FIXTURE_MODE" in existing*|unrelated|different-policy|diverged|setup-ledger|sync-*) printf '2222222222222222222222222222222222222222 refs/heads/release/authorize-stable-workflow\n' ;; esac ;;
  'rev-parse refs/remotes/'*) printf '2222222222222222222222222222222222222222\n' ;;
  'rev-parse release/'*) if [ "$FIXTURE_MODE" = diverged ]; then printf '3333333333333333333333333333333333333333\n'; else printf '2222222222222222222222222222222222222222\n'; fi ;;
  'rev-parse '*|'merge-base '*) printf '1111111111111111111111111111111111111111\n' ;;
  'diff --name-only '*)
    case "$FIXTURE_MODE" in
      sync-*) printf 'release/trust/metadata.json\nrelease/trust/catalog.json\ndocs/status/ledger.json\ndocs/status/README.md\nREADME.md\n' ;;
      *) printf 'release/trust/stable-policy.json\nrelease/trust/stable-policy.sigstore.json\nrelease/trust/stable-trusted-root.json\n' ;;
    esac
    case "$FIXTURE_MODE" in unrelated|sync-unrelated) printf 'user-file\n' ;; setup-ledger) printf 'docs/status/ledger.json\n' ;; esac ;;
  'worktree add --detach '*)
    directory=$4
    mkdir "$directory"
    cp -R "$FIXTURE_SOURCE/." "$directory/"
    for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json; do
      printf 'published\n' >"$directory/release/trust/$name"
    done
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
    printf '{"source_commit":"1111111111111111111111111111111111111111"}\n' >"$directory/release-manifest.json"
    for name in metadata.json metadata.sigstore.json catalog.json catalog.sigstore.json; do
      printf 'published\n' >"$directory/$name"
    done ;;
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
for FIXTURE_MODE in unrelated setup-ledger different-policy diverged existing-push-fails existing; do
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
# A sync-trust retry accepts the coordinator's ledger commit but nothing else.
for FIXTURE_MODE in sync-unrelated sync-ledger; do
  export FIXTURE_MODE
  : >"$FIXTURE_LOG"
  if "$ceremony" sync-trust 1.1.0 >"$fixture/output" 2>&1; then
    [ "$FIXTURE_MODE" = sync-ledger ] || { cat "$fixture/output" >&2; exit 1; }
  else
    [ "$FIXTURE_MODE" != sync-ledger ] || { cat "$fixture/output" >&2; exit 1; }
    grep -F 'existing trust branch contains unrelated changes' "$fixture/output" >/dev/null
  fi
  if grep -E '^git (commit|add)|^gh pr create|--force' "$FIXTURE_LOG" >/dev/null; then
    printf 'ceremony fixture: sync-trust retry rewrote the trust PR\n' >&2; exit 1
  fi
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
# The staged desktop draft is checked before the CLI release goes public, and
# published strictly after it.
grep -F 'desktop helper --check-only Hikyo-Org/Hikyo 1.0.0 release/trust' "$FIXTURE_LOG" >/dev/null
grep -F 'desktop helper Hikyo-Org/Hikyo 1.0.0 release/trust' "$FIXTURE_LOG" >/dev/null
check_line=$(grep -n -F 'desktop helper --check-only' "$FIXTURE_LOG" | head -1 | cut -d: -f1)
cli_line=$(grep -n -F 'publication helper' "$FIXTURE_LOG" | head -1 | cut -d: -f1)
publish_line=$(grep -n -F 'desktop helper Hikyo-Org' "$FIXTURE_LOG" | head -1 | cut -d: -f1)
[ "$check_line" -lt "$cli_line" ] && [ "$cli_line" -lt "$publish_line" ] || {
  printf 'ceremony fixture: desktop check, CLI publication and desktop publication are out of order\n' >&2; exit 1
}
# An unready desktop draft stops publish before anything is public, and review too.
: >"$FIXTURE_LOG"
if printf 'publish v1.0.0 %s\n' "$reviewed" | FIXTURE_MODE=desktop-check-fails "$ceremony" publish 1.0.0 "$reviewed" >"$fixture/output" 2>&1; then
  printf 'ceremony fixture: an unready desktop draft did not stop publish\n' >&2; exit 1
fi
grep -F 'draft is not ready' "$fixture/output" >/dev/null
if grep -F 'publication helper' "$FIXTURE_LOG" >/dev/null; then
  printf 'ceremony fixture: the CLI release was published with an unready desktop draft\n' >&2; exit 1
fi
if FIXTURE_MODE=desktop-check-fails "$ceremony" review 1.0.0 >"$fixture/output" 2>&1; then
  printf 'ceremony fixture: review passed with an unready desktop draft\n' >&2; exit 1
fi
# A prerelease publishes no desktop release.
: >"$FIXTURE_LOG"
printf 'publish v0.9.0 %s\n' "$reviewed" | "$ceremony" publish 0.9.0 "$reviewed" >"$fixture/output"
grep -F 'publication helper Hikyo-Org/Hikyo v0.9.0' "$FIXTURE_LOG" >/dev/null
if grep -F 'desktop helper' "$FIXTURE_LOG" >/dev/null; then
  printf 'ceremony fixture: a prerelease reached desktop publication\n' >&2; exit 1
fi
# A desktop failure after the CLI release is public tells the maintainer to rerun.
: >"$FIXTURE_LOG"
if printf 'publish v1.0.0 %s\n' "$reviewed" | FIXTURE_MODE=desktop-fails "$ceremony" publish 1.0.0 "$reviewed" >"$fixture/output" 2>&1; then
  printf 'ceremony fixture: desktop failure was ignored\n' >&2; exit 1
fi
grep -F 'rerun this publish command' "$fixture/output" >/dev/null
if grep -F 'Public release verified' "$fixture/output" >/dev/null; then
  printf 'ceremony fixture: success reported after a desktop failure\n' >&2; exit 1
fi
if printf 'decline\n' | "$ceremony" homebrew 1.0.0 >"$fixture/output" 2>&1; then exit 1; fi
if grep -E '^gh (pr create|api --method)|^git (push|commit|tag)' "$FIXTURE_LOG" >/dev/null; then
  printf 'ceremony fixture: declined Homebrew publication mutated state\n' >&2; exit 1
fi
printf 'online ceremony fixture: help/refusals, no activation on invalid signing, safe existing-branch retries passed\n'
