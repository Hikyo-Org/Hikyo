#!/bin/sh
# Fixture for .githooks/pre-push: the signature check stays first, and a push
# that changes frontend inputs needs a Storybook pass for exactly those bytes.
# The signature check and the suite are stubs in a throwaway repository.
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
# The stubs record each call in the repository's untracked-by-hook logs under .git.
cat >scripts/ci/check-commit-signatures.sh <<'STUB'
#!/bin/sh
echo sig >>"$(git rev-parse --git-dir)/sig.log"
[ ! -f "$(git rev-parse --git-dir)/sig-fail" ] || { echo 'stub: unsigned commit' >&2; exit 1; }
STUB
cat >scripts/ci/storybook-suite.sh <<'STUB'
#!/bin/sh
echo suite >>"$(git rev-parse --git-dir)/suite.log"
[ ! -f "$(git rev-parse --git-dir)/suite-fail" ] || { echo 'stub: suite red' >&2; exit 1; }
STUB
chmod +x .githooks/pre-push scripts/ci/*.sh
echo 26.0.0 >.nvmrc
echo one >web/a.txt
echo one >clients/ts/b.txt
echo one >docs/site/scripts/test-csp.mjs
echo one >internal/x.go
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
count() { if [ -f "$gitdir/$1.log" ]; then wc -l <"$gitdir/$1.log" | tr -d ' '; else echo 0; fi; }
expect_count() { [ "$(count "$1")" = "$2" ] || fail "$1 ran $(count "$1") times, want $2"; }

# 1. A non-frontend push checks signatures and never runs the suite.
git switch -q -c go "$base"
commit_change internal/x.go
expect_ok go
expect_count sig 1
expect_count suite 0

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
: >"$gitdir/suite-fail"
expect_refused 'the Storybook suite failed' fe-new
expect_count suite 3
expect_refused 'the Storybook suite failed' fe-new
expect_count suite 4
rm "$gitdir/suite-fail"
expect_ok fe-new
expect_count suite 5

# 6. HIKYO_PREPUSH_STORYBOOK=skip skips only the suite; other values are errors.
commit_change web/a.txt
(HIKYO_PREPUSH_STORYBOOK=skip && export HIKYO_PREPUSH_STORYBOOK && push fe-new) || fail 'skip push was refused'
grep -F 'skipping the Storybook suite' "$out" >/dev/null || fail 'skip warning missing'
expect_count suite 5
if (HIKYO_PREPUSH_STORYBOOK=1 && export HIKYO_PREPUSH_STORYBOOK && push fe-new); then fail 'invalid HIKYO_PREPUSH_STORYBOOK accepted'; fi
grep -F 'must be unset or "skip"' "$out" >/dev/null || fail 'invalid value message missing'
: >"$gitdir/sig-fail"
if (HIKYO_PREPUSH_STORYBOOK=skip && export HIKYO_PREPUSH_STORYBOOK && push fe-new); then fail 'skip bypassed the signature check'; fi
grep -F 'unsigned commit' "$out" >/dev/null || fail 'signature failure missing under skip'
rm "$gitdir/sig-fail"

# 7. Signatures run first: an unsigned frontend push never reaches the suite.
before=$(count suite)
: >"$gitdir/sig-fail"
expect_refused 'unsigned commit' fe-new
rm "$gitdir/sig-fail"
expect_count suite "$before"

# 8. Branch deletions are ignored entirely, even with a failing signature stub.
: >"$gitdir/sig-fail"
printf 'refs/heads/gone %s refs/heads/gone %s\n' "$zero" "$base" | sh .githooks/pre-push origin url >"$out" 2>&1 || fail 'deletion was refused'
rm "$gitdir/sig-fail"

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

printf 'pre-push fixture: signatures run first; the suite runs once per frontend key and is skipped on stamps, branch deletions, non-frontend pushes and explicit opt-out; dirty, foreign or failing runs refuse\n'
