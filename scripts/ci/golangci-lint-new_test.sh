#!/bin/sh
# Fixture for golangci-lint-new.sh package selection. run-go-tool.sh is a stub
# that records its arguments, so nothing downloads or builds golangci-lint.
set -eu

script=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/golangci-lint-new.sh
root=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-golangci-fixture.XXXXXX")
trap 'rm -rf "$root"' EXIT HUP INT TERM
repo=$root/repo
out=$root/out
calls=$root/calls

fail() {
printf 'golangci-lint-new fixture failed: %s\n' "$*" >&2
	[ ! -f "$out" ] || cat "$out" >&2
	exit 1
}

# Fake go: `go list` reports a package as buildable when its directory holds a
# .go file whose name does not end in _ignored.go (standing in for build constraints).
mkdir -p "$root/bin"
cat >"$root/bin/go" <<'STUB'
#!/bin/sh
[ "$1" = list ] || exit 99
[ ! -f "$GO_LIST_FAIL" ] || { echo 'stub: go list broke' >&2; exit 3; }
for arg in "$@"; do dir=$arg; done
for f in "$dir"/*.go; do
	[ -e "$f" ] || continue
	case "$f" in *_ignored.go) ;; *) echo y; exit 0 ;; esac
done
STUB
chmod +x "$root/bin/go"
PATH=$root/bin:$PATH
GO_LIST_FAIL=$root/go-list-fail
export GO_LIST_FAIL

git init -q -b main "$repo"
cd "$repo"
git config user.name 'Fixture Author'
git config user.email 'fixture@example.com'
git config commit.gpgsign false
mkdir -p scripts/ci a b gone testdata/x docs-only ignored "with space" "café" desktop/internal/shell
cp "$script" scripts/ci/golangci-lint-new.sh
cat >scripts/ci/run-go-tool.sh <<STUB
#!/bin/sh
# The desktop module passes run from inside desktop/; mark those calls.
case "\$PWD" in
	*/desktop) echo "desktop: \$*" >>"$calls"; [ ! -f "$root/tool-fail-desktop" ] || exit 5 ;;
	*) echo "\$*" >>"$calls" ;;
esac
[ ! -f "$root/tool-fail" ] || exit 7
case "\$*" in
	*--new-from-merge-base*) [ ! -f "$root/tool-fail-new" ] || exit 7 ;;
	*) [ ! -f "$root/tool-fail-strict" ] || exit 9 ;;
esac
STUB
chmod +x scripts/ci/*.sh
echo 'package a' >a/a.go
echo 'package a' >a/extra.go
echo 'package b' >b/b.go
echo 'package d' >docs-only/d.go
echo readme >docs-only/README.md
echo 'package i' >ignored/i_ignored.go
echo 'package s' >"with space/s.go"
echo 'package c' >"café/c.go"
echo 'package gone' >gone/g.go
echo 'package x' >testdata/x/x.go
echo 'package main' >main.go
echo 'package main' >desktop/main.go
echo 'package shell' >desktop/internal/shell/s.go
echo 'module desktop' >desktop/go.mod
echo sum >desktop/go.sum
echo doc >desktop/README.md
echo module >go.mod
echo sum >go.sum
echo cfg >.golangci.yml
echo strict >.golangci-strict.yml
echo v1 >scripts/ci/golangci-lint-version
echo doc >README.md
git add -A
git commit -q -m base

change() { # FILE...: commit a change to each file on a fresh branch off main
	change_count=$((${change_count:-0} + 1))
	branch=b$$-$change_count
	git switch -q -c "$branch" main
	for f in "$@"; do
		mkdir -p "$(dirname "$f")"
		echo "# x" >>"$f"
		git add "$f"
	done
	git commit -q -m change
}
run() { # exit status of the script against main, output in $out, args in $calls
	: >"$calls"
	set +e
	sh scripts/ci/golangci-lint-new.sh main >"$out" 2>&1
	status=$?
	set -e
	return "$status"
}
desktop_new="desktop: golangci-lint run -j 2 --config ../.golangci.yml --new-from-merge-base=main ./..."
# The desktop module gets the same two passes as the root: new code, then strict.
desktop_calls="$desktop_new
desktop: golangci-lint run -j 2 --config ../.golangci-strict.yml ./..."
expect_call() {
	# Two passes over the same packages: new code only, then the strict rules on whole packages.
	want="golangci-lint run -j 2 --new-from-merge-base=main $1
golangci-lint run -j 2 --config .golangci-strict.yml $1"
	# The root passes come first; the desktop pass, when any, follows them.
	[ "$(grep -v '^desktop: ' "$calls")" = "$want" ] || fail "run-go-tool got '$(cat "$calls")', want '$want'"
}
expect_desktop_calls() {
	[ "$(grep '^desktop: ' "$calls")" = "$desktop_calls" ] || fail "desktop pass was '$(grep '^desktop: ' "$calls")', want '$desktop_calls'"
	[ "$(tail -n 2 "$calls")" = "$desktop_calls" ] || fail 'desktop passes did not follow the root passes'
}

# 1. No Go inputs: nothing runs, exit 0.
change README.md
run || fail 'doc-only change failed'
[ ! -s "$calls" ] || fail 'doc-only change ran the linter'
grep -F 'nothing to lint' "$out" >/dev/null || fail 'doc-only message missing'

# 2. Changed Go files select their directories, deduplicated and sorted.
change b/b.go a/a.go a/new.go
run || fail 'go change failed'
expect_call './a ./b'

# 3. A root-level file selects the root package; testdata is never selected.
change main.go testdata/x/x.go
run || fail 'root change failed'
expect_call '.'

# 4. A deleted file in a deleted directory is skipped; a deleted file in a live directory is not.
git switch -q -c del main
git rm -q gone/g.go a/a.go
git commit -q -m delete
run || fail 'delete failed'
expect_call './a'

# 4b. A directory whose last Go file was deleted (a README remains), or whose
# remaining files all fail build constraints, is not selected; a mix keeps the rest.
git switch -q -c nogo main
git rm -q docs-only/d.go
echo '# x' >>ignored/i_ignored.go
git add ignored/i_ignored.go
git commit -q -m 'no go files left'
run || fail 'no-go-files change failed'
[ ! -s "$calls" ] || fail "ran the linter on '$(cat "$calls")'"
grep -F 'nothing to lint' "$out" >/dev/null || fail 'nothing-to-lint message missing'
git rm -q b/b.go
git commit -q -m 'delete b'
echo '# x' >>a/a.go
git add a/a.go
git commit -q -m 'edit a'
run || fail 'mixed change failed'
expect_call './a'

# 4c. Directory names with spaces and non-ASCII characters stay single arguments.
change "with space/s.go" "café/c.go"
run || fail 'odd names failed'
expect_call "./café ./with space"

# 4d. A file moved to another package directory lints both: the old directory
# holds whatever the move left unused.
git switch -q -c moved main
git mv a/extra.go b/extra.go
git commit -q -m 'move extra.go'
run || fail 'rename failed'
expect_call './a ./b'

# 4e. A go list failure aborts instead of silently dropping the package.
change a/a.go
: >"$GO_LIST_FAIL"
if run; then fail 'go list failure was swallowed'; fi
[ "$status" = 64 ] || fail "go list failure exited $status, want 64"
grep -F 'go list failed' "$out" >/dev/null || fail 'go list failure message missing'
rm "$GO_LIST_FAIL"

# 5. Inputs that affect every package lint everything.
for f in go.mod go.sum .golangci.yml .golangci-strict.yml scripts/ci/golangci-lint-version scripts/ci/golangci-lint-new.sh scripts/ci/run-go-tool.sh; do
	change "$f"
	run || fail "$f change failed"
	expect_call './...' && expect_desktop_calls
done

# 6. The linter's failure status is the script's.
change a/a.go
: >"$root/tool-fail"
if run; then fail 'linter failure was swallowed'; fi
[ "$status" = 7 ] || fail "status $status, want 7"
rm "$root/tool-fail"

# 6b. A failure in pass 1 still runs pass 2 and fails the script; a failure in
# pass 2 alone fails it too; the label lines announce both passes.
: >"$root/tool-fail-new"
if run; then fail 'new-code pass failure was swallowed'; fi
[ "$status" = 7 ] || fail "new-code pass failure exited $status, want 7"
[ "$(wc -l <"$calls" | tr -d ' ')" = 2 ] || fail "a failing pass 1 skipped pass 2: '$(cat "$calls")'"
sed -n 2p "$calls" | grep -F -e '--config .golangci-strict.yml' >/dev/null || fail 'pass 2 after a pass 1 failure is not the strict run'
rm "$root/tool-fail-new"
: >"$root/tool-fail-strict"
if run; then fail 'strict pass failure was swallowed'; fi
[ "$status" = 9 ] || fail "strict pass failure exited $status, want 9"
rm "$root/tool-fail-strict"
run || fail 'passing run failed'
grep -F 'golangci-lint-new: new code since main (./a)' "$out" >/dev/null || fail 'new-code label missing'
grep -F 'golangci-lint-new: strict rules on whole packages (./a)' "$out" >/dev/null || fail 'strict label missing'
[ "$(sed -n 1p "$calls" | grep -c -e '--new-from-merge-base')" = 1 ] || fail 'pass 1 lost --new-from-merge-base'
[ "$(sed -n 2p "$calls" | grep -c -e '--new-from-merge-base')" = 0 ] || fail 'strict pass has --new-from-merge-base'

# 7. A base with no merge base fails clearly and runs nothing.
other=$(git commit-tree -m other "$(git hash-object -t tree /dev/null)")
: >"$calls"
if sh scripts/ci/golangci-lint-new.sh "$other" >"$out" 2>&1; then fail 'unrelated base accepted'; fi
grep -F 'no merge base' "$out" >/dev/null || fail 'merge base message missing'
sh scripts/ci/golangci-lint-new.sh "$other" >/dev/null 2>&1 || [ "$?" = 64 ] || fail 'missing merge base must exit 64'
[ ! -s "$calls" ] || fail 'ran the linter without a merge base'

# 8. A tree without its own .golangci.yml gets the script checkout's config
# (reached through --config); one with it is left to find its own.
git switch -q -c noconfig main
git rm -q .golangci.yml
echo '# x' >>a/a.go
git add a/a.go
git commit -q -m 'no config'
run || fail 'config-less tree failed'
case "$(sed -n 1p "$calls")" in
	"golangci-lint run -j 2 --config "*"/.golangci.yml --new-from-merge-base=main ./...") ;;
	*) fail "config-less run-go-tool got '$(sed -n 1p "$calls")'" ;;
esac
sed -n 2p "$calls" | grep -F -x 'golangci-lint run -j 2 --config .golangci-strict.yml ./...' >/dev/null || fail "strict pass of the config-less tree got '$(sed -n 2p "$calls")'"

# 8b. A tree without its own .golangci-strict.yml skips the strict pass with a note.
git switch -q -c nostrict main
git rm -q .golangci-strict.yml
echo '# x' >>a/a.go
git add a/a.go
git commit -q -m 'no strict config'
run || fail 'strict-config-less tree failed'
# Removing the strict config is itself a lint input change, so desktop is linted
# too, and without the config its strict pass is skipped as well.
[ "$(cat "$calls")" = "golangci-lint run -j 2 --new-from-merge-base=main ./...
$desktop_new" ] || fail "strict-config-less run-go-tool got '$(cat "$calls")'"
grep -F 'no .golangci-strict.yml in this tree; skipping the strict pass' "$out" >/dev/null || fail 'strict skip note missing'

# 9. A failing `git diff` is a setup failure (64), never "nothing to lint":
# the shim fails the Nth diff call and passes everything else through.
real_git=$(command -v git)
mkdir -p "$root/shim"
cat >"$root/shim/git" <<STUB
#!/bin/sh
for arg in "\$@"; do
	if [ "\$arg" = diff ]; then
		n=\$(cat "$root/diff-calls" 2>/dev/null || echo 0)
		echo \$((n + 1)) >"$root/diff-calls"
		[ "\$((n + 1))" != "\$FAIL_DIFF" ] || { echo 'stub: git diff broke' >&2; exit 1; }
	fi
done
exec "$real_git" "\$@"
STUB
chmod +x "$root/shim/git"
change a/a.go
for nth in 1 2 3; do
	: >"$calls"
	rm -f "$root/diff-calls"
	set +e
	FAIL_DIFF=$nth PATH=$root/shim:$PATH sh scripts/ci/golangci-lint-new.sh main >"$out" 2>&1
	status=$?
	set -e
	[ "$status" = 64 ] || fail "failing git diff #$nth exited $status, want 64"
	grep -F 'git diff failed' "$out" >/dev/null || fail "failing git diff #$nth message missing"
	[ ! -s "$calls" ] || fail "failing git diff #$nth still ran the linter"
	grep -F 'nothing to lint' "$out" >/dev/null && fail "failing git diff #$nth read as nothing to lint"
done

# 10. A change inside the nested desktop module never reaches `go list` or the root
# pass (both fail on a path outside the root module); desktop is linted from inside it.
change desktop/internal/shell/s.go desktop/main.go
: >"$GO_LIST_FAIL"
run || fail 'desktop-only change failed'
rm "$GO_LIST_FAIL"
[ "$(cat "$calls")" = "$desktop_calls" ] || fail "desktop-only change ran '$(cat "$calls")'"
grep -F 'golangci-lint-new: linting new code since main (desktop/...)' "$out" >/dev/null || fail 'desktop label missing'

# 10b. Only desktop/go.mod or desktop/go.sum changing still lints desktop.
for f in desktop/go.mod desktop/go.sum; do
	change "$f"
	run || fail "$f change failed"
	[ "$(cat "$calls")" = "$desktop_calls" ] || fail "$f change ran '$(cat "$calls")'"
done

# 10c. A docs file under desktop/ lints nothing.
change desktop/README.md
run || fail 'desktop doc change failed'
[ ! -s "$calls" ] || fail 'desktop doc change ran the linter'

# 10d. Root and desktop changes together: root pass first, then desktop's.
change a/a.go desktop/main.go
run || fail 'mixed root and desktop change failed'
expect_call './a'
expect_desktop_calls

# 10e. A desktop failure fails the script, and a root failure is reported first.
change desktop/main.go
: >"$root/tool-fail-desktop"
if run; then fail 'desktop lint failure was swallowed'; fi
[ "$status" = 5 ] || fail "desktop lint failure exited $status, want 5"
change a/a.go desktop/main.go
: >"$root/tool-fail"
if run; then fail 'root failure was swallowed beside desktop'; fi
[ "$status" = 7 ] || fail "root failure exited $status, want 7"
# Root new code and strict, then desktop's two: a failing root pass skips none of them.
[ "$(wc -l <"$calls" | tr -d ' ')" = 4 ] || fail 'a failing root pass skipped the desktop pass'
rm "$root/tool-fail-desktop" "$root/tool-fail"
# A strict finding in desktop alone fails the script.
change desktop/main.go
: >"$root/tool-fail-strict"
if run; then fail 'desktop strict failure was swallowed'; fi
[ "$status" = 9 ] || fail "desktop strict failure exited $status, want 9"
rm "$root/tool-fail-strict"

# 10f. Where desktop/ does not exist (main, dev before the shell lands) a lint-input
# change lints the root only, and a deleted desktop file selects nothing.
git switch -q -c nodesktop main
git rm -q -r desktop
echo '# x' >>go.mod
git add go.mod
git commit -q -m 'no desktop'
run || fail 'desktop-less tree failed'
expect_call './...'
[ "$(grep -c '^desktop: ' "$calls")" = 0 ] || fail 'linted a desktop/ that does not exist'
git switch -q -c nodesktop-only main
git rm -q -r desktop
git commit -q -m 'drop desktop only'
run || fail 'desktop-only deletion failed'
[ ! -s "$calls" ] || fail "desktop deletion ran '$(cat "$calls")'"
grep -F 'nothing to lint' "$out" >/dev/null || fail 'desktop deletion message missing'

printf 'golangci-lint-new fixture: selects changed package directories, lints everything for config changes, skips deletions and testdata, propagates failure\n'
