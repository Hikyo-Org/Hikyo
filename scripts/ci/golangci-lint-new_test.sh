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

git init -q -b main "$repo"
cd "$repo"
git config user.name 'Fixture Author'
git config user.email 'fixture@example.com'
git config commit.gpgsign false
mkdir -p scripts/ci a b gone testdata/x
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
	[ "$(cat "$calls")" = "golangci-lint run -j 2 --new-from-merge-base=main $1" ] || fail "run-go-tool got '$(cat "$calls")', want packages: $1"
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
[ ! -s "$calls" ] || fail 'ran the linter without a merge base'

printf 'golangci-lint-new fixture: selects changed package directories, lints everything for config changes, skips deletions and testdata, propagates failure\n'
