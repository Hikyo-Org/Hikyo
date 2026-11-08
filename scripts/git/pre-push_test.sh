#!/bin/sh
# Fixture for .githooks/pre-push: the signature check stays first, and a push
# that changes frontend inputs needs a Storybook pass for exactly those bytes,
# and one that changes Go inputs runs the golangci-lint ratchet.
# The signature check, the suite and the ratchet are stubs in a throwaway repository.
set -eu

hook=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)/.githooks/pre-push
root=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-prepush-fixture.XXXXXX")
trap 'rm -rf "$root"' EXIT HUP INT TERM
repo=$root/repo
zero=0000000000000000000000000000000000000000
out=$root/out

fail() {
	printf 'pre-push fixture failed: %s\n' "$*" >&2
	[ ! -f "$out" ] || cat "$out" >&2
	exit 1
}

git init -q "$repo"
cd "$repo"
git config user.name 'Fixture Author'
git config user.email 'fixture@example.com'
git config commit.gpgsign false
mkdir -p .githooks scripts/ci docs/site/scripts web clients/ts internal
cp "$hook" .githooks/pre-push
# The stubs record each call, and take their switches, in $logs outside the repository.
logs=$root/logs
mkdir -p "$logs"
cat >scripts/ci/check-commit-signatures.sh <<STUB
#!/bin/sh
echo sig >>"$logs/sig.log"
[ ! -f "$logs/sig-fail" ] || { echo 'stub: unsigned commit' >&2; exit 1; }
STUB
cat >scripts/ci/storybook-suite.sh <<STUB
#!/bin/sh
echo suite >>"$logs/suite.log"
[ ! -f "$logs/suite-edit" ] || echo edited >>web/a.txt
[ ! -f "$logs/suite-fail" ] || { echo 'stub: suite red' >&2; exit 1; }
STUB
# The ratchet stub records its base argument, the commit and work tree it runs
# in, fails like golangci-lint (exit 1, or the code in golangci-fail) when
# internal/x.go holds BAD or wip.go exists in that tree.
cat >scripts/ci/golangci-lint-new.sh <<STUB
#!/bin/sh
echo committed >>"$logs/marker.log"
echo "\$1 \$(git rev-parse HEAD) \$(pwd -P)" >>"$logs/golangci.log"
[ ! -f "$logs/golangci-fail" ] || exit "\$(cat "$logs/golangci-fail")"
! grep -q BAD internal/x.go 2>/dev/null || { echo 'stub: new issue' >&2; exit 1; }
# Like the real script: a tree with its own .golangci.yml uses it (no --config);
# otherwise the config next to this script is passed with --config.
if [ -f .golangci.yml ]; then cfg=.golangci.yml; echo tree >>"$logs/config.log"; else cfg=\$(dirname "\$0")/../../.golangci.yml; echo "--config \$cfg" >>"$logs/config.log"; fi
! grep -q BADCFG "\$cfg" 2>/dev/null || { echo 'stub: bad config' >&2; exit 1; }
[ ! -e wip.go ] || { echo 'stub: stray wip.go linted' >&2; exit 1; }
STUB
chmod +x .githooks/pre-push scripts/ci/*.sh
echo 26.0.0 >.nvmrc
echo one >web/a.txt
echo one >clients/ts/b.txt
echo one >docs/site/scripts/test-csp.mjs
echo one >internal/x.go
echo ok >.golangci.yml
echo '.env*' >.gitignore
echo one >package.json
git add -A
git commit -q -m base
git update-ref refs/remotes/hikyo-upstream/main HEAD
base=$(git rev-parse HEAD)
gitdir=$(git rev-parse --absolute-git-dir)

serial=0
commit_change() { # FILE: make the commit that touches FILE with unique content
	serial=$((serial + 1))
	echo "change $serial" >>"$1"
	git add "$1"
	git commit -q -m "change $1"
}
push() { # BRANCH [dir]: feed the hook like git does; capture output in $out
	oid=$(git rev-parse "$1")
	printf 'refs/heads/%s %s refs/heads/%s %s\n' "$1" "$oid" "$1" "$zero" | (cd "${2:-$repo}" && sh .githooks/pre-push origin url) >"$out" 2>&1
}
expect_ok() { push "$@" || fail "push of $1 was refused"; }
expect_refused() { # MESSAGE BRANCH [dir]
	message=$1
	shift
	if push "$@"; then fail "push of $1 was accepted"; fi
	grep -F -- "$message" "$out" >/dev/null || fail "expected refusal containing: $message"
}
count() { if [ -f "$logs/$1.log" ]; then wc -l <"$logs/$1.log" | tr -d ' '; else echo 0; fi; }
expect_count() { [ "$(count "$1")" = "$2" ] || fail "$1 ran $(count "$1") times, want $2"; }

# 1. A non-frontend push checks signatures and never runs the suite.
git switch -q -c go "$base"
commit_change internal/x.go
expect_ok go
expect_count sig 1
expect_count suite 0
expect_count golangci 1
[ "$(cut -d' ' -f1,2 "$logs/golangci.log")" = "refs/remotes/hikyo-upstream/main $(git rev-parse go)" ] || fail 'ratchet was not given the range base and the pushed commit'

# 2. A frontend push runs the suite once and records the pass; the same bytes skip it.
git switch -q -c fe "$base"
commit_change web/a.txt
expect_ok fe
expect_count suite 1
expect_ok fe
expect_count suite 1
grep -c . "$gitdir/hikyo-storybook-passes" | grep -qx 1 || fail 'one pass stamp expected'

# 3. Two refs with the same bytes in one push run the suite once.
git branch fe-twin fe
git switch -q -c fe-new fe
commit_change clients/ts/b.txt
git branch fe-new-twin
oid=$(git rev-parse fe-new)
printf 'refs/heads/fe-new %s refs/heads/fe-new %s\nrefs/heads/fe-new-twin %s refs/heads/fe-new-twin %s\n' "$oid" "$zero" "$oid" "$zero" |
	sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'twin push was refused'
expect_count suite 2

# 4. A dirty or untracked frontend file, or a different checkout, refuses.
commit_change web/a.txt
echo dirty >>web/a.txt
expect_refused 'check out and clean the branch' fe-new
git checkout -q -- web/a.txt
echo stray >web/untracked.txt
expect_refused 'check out and clean the branch' fe-new
rm web/untracked.txt
git switch -q go
expect_refused 'check out and clean the branch' fe-new
git switch -q fe-new
expect_count suite 2

# 5. A failing suite blocks the push and records nothing; a pass then clears it.
: >"$logs/suite-fail"
expect_refused 'the Storybook suite failed' fe-new
expect_count suite 3
expect_refused 'the Storybook suite failed' fe-new
expect_count suite 4
rm "$logs/suite-fail"
expect_ok fe-new
expect_count suite 5

# 6. HIKYO_PREPUSH_STORYBOOK=skip skips only the suite; other values are errors.
commit_change web/a.txt
(HIKYO_PREPUSH_STORYBOOK=skip && export HIKYO_PREPUSH_STORYBOOK && push fe-new) || fail 'skip push was refused'
grep -F 'skipping the Storybook suite' "$out" >/dev/null || fail 'skip warning missing'
expect_count suite 5
if (HIKYO_PREPUSH_STORYBOOK=1 && export HIKYO_PREPUSH_STORYBOOK && push fe-new); then fail 'invalid HIKYO_PREPUSH_STORYBOOK accepted'; fi
grep -F 'must be unset or "skip"' "$out" >/dev/null || fail 'invalid value message missing'
: >"$logs/sig-fail"
if (HIKYO_PREPUSH_STORYBOOK=skip && export HIKYO_PREPUSH_STORYBOOK && push fe-new); then fail 'skip bypassed the signature check'; fi
grep -F 'unsigned commit' "$out" >/dev/null || fail 'signature failure missing under skip'
rm "$logs/sig-fail"

# 7. Signatures run first: an unsigned frontend push never reaches the suite.
before=$(count suite)
: >"$logs/sig-fail"
expect_refused 'unsigned commit' fe-new
rm "$logs/sig-fail"
expect_count suite "$before"

# 8. Branch deletions are ignored entirely, even with a failing signature stub.
: >"$logs/sig-fail"
printf '(delete) %s refs/heads/gone %s\n' "$zero" "$base" | sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'deletion was refused'
# SHA-256 repositories send a 64-zero object for the deletion.
printf '(delete) %s%s refs/heads/gone %s\n' "$zero" "$(printf '%024d' 0)" "$base" | sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'SHA-256 deletion was refused'
rm "$logs/sig-fail"

# 9. Linked worktrees share the stamps and need their own clean checkout.
expect_ok fe-new
git worktree add -q "$root/linked" -b linked fe-new
expect_ok linked "$root/linked"
expect_count suite 6
git worktree remove --force "$root/linked"

# 10. The stamp file keeps only the last 50 passes.
i=0
while [ "$i" -lt 60 ]; do
	echo "old$i" >>"$gitdir/hikyo-storybook-passes"
	i=$((i + 1))
done
commit_change web/a.txt
expect_ok fe-new
[ "$(grep -c . "$gitdir/hikyo-storybook-passes")" = 50 ] || fail 'stamp file not bounded to 50'
expect_ok fe-new
expect_count suite 7

# 11. Pushing HEAD or a bare SHA is classified by the remote ref, so both are checked.
git switch -q go
: >"$logs/sig-fail"
printf 'HEAD %s refs/heads/go %s\n' "$(git rev-parse HEAD)" "$zero" | sh .githooks/pre-push origin url >"$out" 2>&1 && fail 'HEAD push skipped the signature check'
printf '%s %s refs/heads/go %s\n' "$(git rev-parse HEAD)" "$(git rev-parse HEAD)" "$zero" | sh .githooks/pre-push origin url >"$out" 2>&1 && fail 'SHA push skipped the signature check'
rm "$logs/sig-fail"
git switch -q fe-new
commit_change web/a.txt
before=$(count suite)
printf 'HEAD %s refs/heads/fe-new %s\n' "$(git rev-parse HEAD)" "$zero" | sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'HEAD frontend push refused'
expect_count suite $((before + 1))
commit_change web/a.txt
before=$(count suite)
printf '%s %s refs/heads/fe-new %s\n' "$(git rev-parse HEAD)" "$(git rev-parse HEAD)" "$zero" | sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'SHA frontend push refused'
expect_count suite $((before + 1))
# Tag-to-tag pushes stay unchecked; a branch pushed to a tag or review ref is checked.
: >"$logs/sig-fail"
printf 'refs/tags/v1 %s refs/tags/v1 %s\n' "$(git rev-parse HEAD)" "$zero" | sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'tag-to-tag push refused'
for target in refs/tags/y refs/for/main; do
	printf 'refs/heads/fe-new %s %s %s\n' "$(git rev-parse HEAD)" "$target" "$zero" | sh .githooks/pre-push origin url >"$out" 2>&1 && fail "branch pushed to $target skipped the signature check"
	grep -F 'unsigned commit' "$out" >/dev/null || fail "$target push did not reach the signature check"
done
rm "$logs/sig-fail"

# 12. Edits made while the suite runs are never stamped.
commit_change web/a.txt
before=$(count suite)
: >"$logs/suite-edit"
expect_refused 'changed while the Storybook suite ran' fe-new
rm "$logs/suite-edit"
git checkout -q -- web/a.txt
expect_count suite $((before + 1))
expect_ok fe-new
expect_count suite $((before + 2))

# 13. Untracked files in new directories and ignored .env files refuse.
commit_change web/a.txt
mkdir -p web/newdir
echo stray >web/newdir/file
expect_refused 'check out and clean the branch' fe-new
rm -r web/newdir
echo SECRET=1 >web/.env.local
expect_refused 'check out and clean the branch' fe-new
rm web/.env.local
mkdir -p clients/ts/sub
echo SECRET=1 >clients/ts/sub/.env
expect_refused 'check out and clean the branch' fe-new
rm -r clients/ts/sub
expect_ok fe-new

# 14. An inner failure (an unwritable stamp file) fails the hook, not just the suite.
commit_change web/a.txt
chmod a-w "$gitdir"
if [ -w "$gitdir" ]; then
	chmod u+w "$gitdir"
	printf 'pre-push fixture: skipping the unwritable-stamp case (running as a user who ignores permissions)\n' >&2
else
	if push fe-new; then chmod u+w "$gitdir"; fail 'unwritable stamp file did not fail the hook'; fi
	chmod u+w "$gitdir"
	grep -F 'Permission denied' "$out" >/dev/null || fail 'expected the stamp write error'
fi

# 15. The root package.json is a frontend input.
git switch -q go
before=$(count suite)
git switch -q -c pkg "$base"
commit_change package.json
expect_ok pkg
expect_count suite $((before + 1))

# 16. A mode-only change is a new key, and a non-executable committed suite is
# refused before any stamp or run: CI executes the committed file.
git switch -q -c mode pkg
chmod -x scripts/ci/storybook-suite.sh
git add scripts/ci/storybook-suite.sh
git commit -q -m 'drop the suite executable bit'
before=$(count suite)
expect_refused 'storybook-suite.sh as an executable file' mode
expect_count suite "$before"
# A core.fileMode=false clone ignores the working copy's executable bit, so the
# committed mode itself must refuse.
git switch -q -c mode-ignored pkg
git config core.fileMode false
git update-index --chmod=-x scripts/ci/storybook-suite.sh
git commit -q -m 'commit the suite without its executable bit'
expect_refused 'storybook-suite.sh as an executable file' mode-ignored
git config --unset core.fileMode
git checkout -q -- scripts/ci/storybook-suite.sh # the work tree still has the executable bit the index dropped
expect_count suite "$before"

# 17. The golangci-lint ratchet runs on Go changes only, and failure, opt-out
# and an invalid opt-out behave like the Storybook gate.
git switch -q go
before=$(count golangci)
git switch -q -c gl "$base"
commit_change go.mod
expect_ok gl
expect_count golangci $((before + 1))
git switch -q pkg
expect_ok pkg
expect_count golangci $((before + 1))
git switch -q gl
commit_change internal/x.go
: >"$logs/golangci-fail"
echo 1 >"$logs/golangci-fail"
expect_refused 'reported issues' gl
grep -F 'scripts/ci/golangci-lint-new.sh refs/remotes/hikyo-upstream/main' "$out" >/dev/null || fail 'reproduce hint missing'
expect_count golangci $((before + 2))
echo 5 >"$logs/golangci-fail"
expect_refused 'reported issues or could not run' gl
echo 64 >"$logs/golangci-fail"
expect_refused 'could not start or finish its setup' gl
rm "$logs/golangci-fail"
(HIKYO_PREPUSH_GOLANGCI=skip && export HIKYO_PREPUSH_GOLANGCI && push gl) || fail 'golangci skip push was refused'
grep -F 'skipping the golangci-lint ratchet' "$out" >/dev/null || fail 'golangci skip warning missing'
expect_count golangci $((before + 4))
if (HIKYO_PREPUSH_GOLANGCI=1 && export HIKYO_PREPUSH_GOLANGCI && push gl); then fail 'invalid HIKYO_PREPUSH_GOLANGCI accepted'; fi
grep -F 'must be unset or "skip"' "$out" >/dev/null || fail 'invalid golangci value message missing'
# Signatures run first: a failing signature check never reaches the ratchet, with or without the opt-out.
before=$(count golangci)
: >"$logs/sig-fail"
expect_refused 'unsigned commit' gl
if (HIKYO_PREPUSH_GOLANGCI=skip && export HIKYO_PREPUSH_GOLANGCI && push gl); then fail 'golangci skip bypassed the signature check'; fi
rm "$logs/sig-fail"
expect_count golangci "$before"

# The ratchet lints exactly the pushed commit, in a temporary worktree.
# (C) a bad commit fixed only in the work tree is still refused.
commit_change internal/x.go
echo BAD >>internal/x.go
git add internal/x.go
git commit -q -m 'bad change'
git checkout -q HEAD~1 -- internal/x.go
expect_refused 'reported issues' gl
git checkout -q -- internal/x.go
git reset -q --hard HEAD~1
# (B) an untracked file with an issue does not refuse a good commit.
echo 'package x' >wip.go
expect_ok gl
# (D) a broken untracked file does not refuse it either.
echo 'this is not go' >wip.go
expect_ok gl
rm wip.go
# The lint ran on the pushed commit, in a worktree that is gone afterwards.
[ "$(tail -n 1 "$logs/golangci.log" | cut -d' ' -f2)" = "$(git rev-parse gl)" ] || fail 'ratchet did not lint the pushed commit'
lint_dir=$(tail -n 1 "$logs/golangci.log" | cut -d' ' -f3-)
[ "$lint_dir" != "$(pwd -P)" ] || fail 'ratchet ran in the checked-out tree'
[ ! -e "$lint_dir" ] || fail 'temporary worktree was left behind'
[ "$(git worktree list | wc -l | tr -d ' ')" = 1 ] || fail 'worktree registration left behind'
# A branch that is not checked out is linted, and refused when bad.
git branch -q gl-bad "$base"
git switch -q -c gl-other "$base"
git switch -q gl-bad
echo BAD >>internal/x.go
git add internal/x.go
git commit -q -m 'bad branch'
git switch -q gl-other
expect_refused 'reported issues' gl-bad
expect_ok gl
[ "$(tail -n 1 "$logs/golangci.log" | cut -d' ' -f2)" = "$(git rev-parse gl)" ] || fail 'non-checked-out push linted the wrong commit'
# Two branches in one push: each is linted at its own commit.
before=$(count golangci)
git branch gl-twin gl
git switch -q -c gl-two gl
commit_change internal/x.go
oid_a=$(git rev-parse gl-two)
oid_b=$(git rev-parse gl)
printf 'refs/heads/gl-two %s refs/heads/gl-two %s\nrefs/heads/gl %s refs/heads/gl %s\n' "$oid_a" "$zero" "$oid_b" "$zero" |
	sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'two-branch push was refused'
expect_count golangci $((before + 2))
[ "$(tail -n 2 "$logs/golangci.log" | cut -d' ' -f2 | tr '\n' ' ')" = "$oid_a $oid_b " ] || fail 'two-branch push did not lint each commit'

# 18. Real pushes from a linked worktree, where git exports GIT_DIR for that
# worktree to the hook: the ratchet still lints exactly the pushed commit, with
# the pushed commit's config and script, in a fresh directory per push.
git switch -q go
git init -q --bare "$root/remote.git"
git remote add bare "$root/remote.git"
git config core.hooksPath .githooks
git worktree add -q "$root/lw" -b lw-co "$base"
# The pushing checkout holds a different (dirty) copy of the script, marked
# "checkout"; the committed copy every branch carries is marked "committed".
{
	sed -n 1p "$root/lw/scripts/ci/golangci-lint-new.sh"
	echo "echo checkout >>\"$logs/marker.log\""
	sed 1d "$root/lw/scripts/ci/golangci-lint-new.sh" | grep -v '^echo committed'
} >"$root/lw-script"
cat "$root/lw-script" >"$root/lw/scripts/ci/golangci-lint-new.sh"
marker() { tail -n 1 "$logs/marker.log"; }
config() { tail -n 1 "$logs/config.log"; }
git branch -q other-good "$base"
git switch -q other-good
commit_change internal/x.go
git switch -q -c other-bad "$base"
commit_change internal/x.go
echo BAD >>internal/x.go
git add internal/x.go
git commit -q -m 'bad'
git switch -q go
lw_push() { (cd "$root/lw" && git push -q bare "$@") >"$out" 2>&1; }
last() { tail -n 1 "$logs/golangci.log" | cut -d' ' -f"$1"; }
good_oid=$(git rev-parse other-good)
if lw_push other-bad:other-bad; then fail 'bad commit pushed from a linked worktree was accepted'; fi
grep -F 'reported issues' "$out" >/dev/null || fail 'linked-worktree refusal message missing'
lw_push other-good:other-good || fail 'good commit pushed from a linked worktree was refused'
[ "$(last 2)" = "$good_oid" ] || fail "linked-worktree push linted $(last 2), want $good_oid"
[ "$(marker)" = committed ] || fail "ran the $(marker) copy of the ratchet script, want the pushed commit's"
# The lint tree is a fresh directory per push, and is gone afterwards.
first_dir=$(last 3-)
[ ! -e "$first_dir" ] || fail 'lint directory left behind'
[ "$(config)" = tree ] || fail "the pushed tree has a config, but the ratchet got '$(config)'"
git branch -q other-good2 other-good
lw_push other-good2:other-good2 || fail 'second linked-worktree push was refused'
[ "$(last 3-)" != "$first_dir" ] || fail 'two pushes shared a lint directory'
# A dirty .golangci.yml in the pushing checkout does not change the verdict...
echo BADCFG >>"$root/lw/.golangci.yml"
git branch -q other-good3 other-good
lw_push other-good3:other-good3 || fail 'dirty config in the pushing checkout changed the verdict'
[ "$(config)" = tree ] || fail "the pushed tree's own config was not used: '$(config)'"
git -C "$root/lw" checkout -q -- .golangci.yml
# ...but one committed on the pushed branch does.
git switch -q -c other-badcfg other-good
echo BADCFG >>.golangci.yml
git commit -q -am 'bad config'
git switch -q go
if lw_push other-badcfg:other-badcfg; then fail 'committed bad config was accepted'; fi
grep -F 'bad config' "$out" >/dev/null || fail 'the committed bad config was not what refused the push'
# A pushed commit without the script falls back to the pushing checkout's copy.
git switch -q -c other-noscript other-good
git rm -q scripts/ci/golangci-lint-new.sh
git commit -q -m 'drop the script'
git switch -q go
before=$(count golangci)
lw_push other-noscript:other-noscript || fail 'script-less commit was refused'
expect_count golangci $((before + 1))
[ "$(last 2)" = "$(git rev-parse other-noscript)" ] || fail 'fallback script linted the wrong commit'
[ "$(marker)" = checkout ] || fail "fallback ran the $(marker) copy, want the pushing checkout's"
# A pushed commit with neither the script nor a .golangci.yml: the fallback
# script gets --config pointing at the pushing checkout's real config file.
git switch -q -c other-noboth other-good
git rm -q scripts/ci/golangci-lint-new.sh .golangci.yml
git commit -q -m 'drop the script and the config'
git switch -q go
lw_push other-noboth:other-noboth || fail 'commit without script and config was refused'
cfg_arg=$(config)
case "$cfg_arg" in "--config "*) ;; *) fail "expected a --config argument, got '$cfg_arg'" ;; esac
cfg_path=${cfg_arg#--config }
[ -f "$cfg_path" ] || fail "--config points at a missing file: $cfg_path"
[ "$(cd "$(dirname "$cfg_path")" && pwd -P)" = "$(cd "$root/lw" && pwd -P)" ] || fail "--config is not the pushing checkout's config: $cfg_path"
# A committed script without its executable bit fails closed, as in CI, which
# runs it directly: the push is refused, not silently run through the fallback.
git switch -q -c other-nox other-good
chmod -x scripts/ci/golangci-lint-new.sh
git commit -q -am 'script not executable'
git switch -q go
before=$(count golangci)
if lw_push other-nox:other-nox; then fail 'a non-executable committed script was accepted'; fi
grep -F 'reported issues or could not run' "$out" >/dev/null || fail 'non-executable script refusal message missing'
expect_count golangci "$before"
git worktree remove --force "$root/lw"

printf 'pre-push fixture: signatures run first; the suite runs once per frontend key and is skipped on stamps, branch deletions, non-frontend pushes and explicit opt-out; dirty, foreign or failing runs refuse; the golangci-lint ratchet runs on Go changes only and honours its own opt-out\n'
