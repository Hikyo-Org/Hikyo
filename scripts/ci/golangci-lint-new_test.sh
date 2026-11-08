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
mkdir -p scripts/ci a b gone testdata/x docs-only ignored "with space" "café"
cp "$script" scripts/ci/golangci-lint-new.sh
cat >scripts/ci/run-go-tool.sh <<STUB
#!/bin/sh
echo "\$*" >>"$calls"
[ ! -f "$root/tool-fail" ] || exit 7
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
echo module >go.mod
echo sum >go.sum
echo cfg >.golangci.yml
echo v1 >scripts/ci/golangci-lint-version
echo doc >README.md
git add -A
git commit -q -m base

change() { # FILE...: commit a change to each file on a fresh branch off main
	branch=b$$-$(git rev-list --count --all)
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
expect_call() {
	want="golangci-lint run -j 2 --new-from-merge-base=main $1"
	[ "$(cat "$calls")" = "$want" ] || fail "run-go-tool got '$(cat "$calls")', want '$want'"
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

# 4d. A go list failure aborts instead of silently dropping the package.
change a/a.go
: >"$GO_LIST_FAIL"
if run; then fail 'go list failure was swallowed'; fi
[ "$status" = 64 ] || fail "go list failure exited $status, want 64"
grep -F 'go list failed' "$out" >/dev/null || fail 'go list failure message missing'
rm "$GO_LIST_FAIL"

# 5. Inputs that affect every package lint everything.
for f in go.mod go.sum .golangci.yml scripts/ci/golangci-lint-version scripts/ci/golangci-lint-new.sh scripts/ci/run-go-tool.sh; do
	change "$f"
	run || fail "$f change failed"
	expect_call './...'
done

# 6. The linter's failure status is the script's.
change a/a.go
: >"$root/tool-fail"
if run; then fail 'linter failure was swallowed'; fi
[ "$status" = 7 ] || fail "status $status, want 7"
rm "$root/tool-fail"

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
case "$(cat "$calls")" in
	"golangci-lint run -j 2 --config "*"/.golangci.yml --new-from-merge-base=main ./...") ;;
	*) fail "config-less run-go-tool got '$(cat "$calls")'" ;;
esac

printf 'golangci-lint-new fixture: selects changed package directories, lints everything for config changes, skips deletions and testdata, propagates failure\n'
