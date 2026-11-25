#!/bin/sh
# Fixture for go-affected-packages.sh in a throwaway module: store embeds a .sql,
# app imports store in its tests only, util stands alone, desktop/ is a nested module.
set -eu

script=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/go-affected-packages.sh
root=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-affected-fixture.XXXXXX")
trap 'rm -rf "$root"' EXIT HUP INT TERM
cd "$root"
git init -q .
git config user.name Fixture
git config user.email fixture@example.com
git config commit.gpgsign false
mkdir -p store app util desktop docs
printf 'module example.com/m\n\ngo 1.21\n' >go.mod
printf '' >go.sum
printf 'package store\n\nimport _ "embed"\n\n//go:embed data.sql\nvar SQL string\n' >store/store.go
echo 'select 1;' >store/data.sql
printf 'package store\n' >store/store_test.go
printf 'package app\n' >app/app.go
printf 'package app\n\nimport (\n\t"testing"\n\n\t"example.com/m/store"\n)\n\nfunc TestX(t *testing.T) { _ = store.SQL }\n' >app/app_test.go
printf 'package util\n' >util/util.go
printf 'package util\n\nimport "testing"\n\nfunc TestY(t *testing.T) {}\n' >util/util_test.go
printf 'module example.com/desktop\n\ngo 1.21\n' >desktop/go.mod
printf 'package desktop\n' >desktop/d.go
echo docs >docs/a.md
git add -A
git commit -q -m base

n=0
expect() { # LABEL EXPECTED(space separated, sorted) FILE...: change FILEs, select, compare
	label=$1 want=$2
	shift 2
	n=$((n + 1))
	git switch -q -c "case$n" main 2>/dev/null || git switch -q -c "case$n" master
	for f in "$@"; do
		if [ "$f" = go.mod ]; then echo '// x' >>go.mod; else echo "// $n" >>"$f"; fi
	done
	git commit -q -am "$label"
	got=$("$script" "$(git rev-parse HEAD^)" HEAD | sed 's|example.com/m/||; s|example.com/m|root|' | tr '\n' ' ' | sed 's/ $//')
	[ "$got" = "$want" ] || { printf 'affected-packages fixture failed: %s: got "%s", want "%s"\n' "$label" "$got" "$want" >&2; exit 1; }
	git switch -q - 2>/dev/null || true
	git checkout -q "$(git rev-list --max-parents=0 HEAD)"
}

expect 'a .go change selects only that package and its dependents' 'util' util/util.go
expect 'an embedded .sql change selects the embedder and its reverse dependencies' 'app store' store/data.sql
expect 'a .go change in a dependency selects the dependents' 'app store' store/store.go
expect 'go.mod selects every package' 'app store util' go.mod
expect 'docs alone select nothing' '' docs/a.md
expect 'the nested desktop module selects nothing' '' desktop/d.go
printf 'go-affected-packages fixture: .go, embedded, reverse-dependency, go.mod, docs and nested-module changes select the right packages\n'
