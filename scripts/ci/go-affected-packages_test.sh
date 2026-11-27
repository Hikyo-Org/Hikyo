#!/bin/sh
# Fixture for go-affected-packages.sh in a throwaway module: store embeds a .sql,
# app imports store in its tests only, util stands alone, desktop/ is a nested module.
set -eu

script=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)/go-affected-packages.sh
root=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-affected-fixture.XXXXXX")
trap 'rm -rf "$root"' EXIT
trap 'exit 130' HUP INT TERM
cd "$root"
git init -q -b main .
git config user.name Fixture
git config user.email fixture@example.com
git config commit.gpgsign false
mkdir -p store/testdata/x app/sub util desktop/internal docs web gone internal/operator/api/v1alpha1 chart/hikyo/crds internal/lint/testdata/badnil
printf 'package x\n' >store/testdata/x/x.go
printf 'package lint\n' >internal/lint/lint.go
printf 'package lint\n\nimport "testing"\n\nfunc TestL(t *testing.T) {}\n' >internal/lint/lint_test.go
printf 'package badnil\n' >internal/lint/testdata/badnil/aliased.go
printf 'module example.com/sub\n\ngo 1.21\n' >app/sub/go.mod
printf 'package sub\n' >app/sub/s.go
printf 'package sub\n\nimport "testing"\n\nfunc TestS(t *testing.T) {}\n' >app/sub/s_test.go
printf 'package internal\n' >desktop/internal/i.go
printf 'module example.com/m\n\ngo 1.21\n' >go.mod
printf '' >go.sum
printf 'package m\n' >root.go
printf 'package store\n\nimport _ "embed"\n\n//go:embed data.sql\nvar SQL string\n' >store/store.go
echo 'select 1;' >store/data.sql
echo fixture >store/testdata/case.txt
printf 'package store\n' >store/store_test.go
printf 'package app\n' >app/app.go
printf 'package app\n\nimport (\n\t"testing"\n\n\t"example.com/m/store"\n)\n\nfunc TestX(t *testing.T) { _ = store.SQL }\n' >app/app_test.go
printf 'package util\n' >util/util.go
printf 'package util\n\nimport "testing"\n\nfunc TestY(t *testing.T) {}\n' >util/util_test.go
printf 'package gone\n' >gone/gone.go
printf 'package user\n\nimport _ "example.com/m/gone"\n' >web/user.go
printf 'package user\n\nimport "testing"\n\nfunc TestU(t *testing.T) {}\n' >web/user_test.go
printf 'package v1alpha1\n' >internal/operator/api/v1alpha1/v.go
printf 'package v1alpha1\n\nimport "testing"\n\nfunc TestV(t *testing.T) {}\n' >internal/operator/api/v1alpha1/v_test.go
echo crd >chart/hikyo/crds/a.yaml
printf 'module example.com/desktop\n\ngo 1.21\n' >desktop/go.mod
printf 'package desktop\n' >desktop/d.go
echo docs >docs/a.md
printf 'package m\n\nimport "testing"\n\nfunc TestR(t *testing.T) {}\n' >root_test.go
git add -A
git commit -q -m base

n=0
# case LABEL WANT(space separated, sorted) COMMAND...: run COMMAND on a new branch,
# commit, and compare the selection with WANT ("-" for nothing).
case_() {
	label=$1 want=$2
	shift 2
	n=$((n + 1))
	git checkout -q -b "case$n" main
	"$@"
	git add -A
	git commit -q -m "$label"
	out=$("$script" main HEAD) || { printf 'affected-packages fixture failed: %s: the selector exited non-zero\n' "$label" >&2; exit 1; }
	got=$(printf '%s\n' "$out" | sed 's|example.com/m/||; s|example.com/m|root|' | tr '\n' ' ' | sed 's/ $//')
	[ "${got:--}" = "$want" ] || { printf 'affected-packages fixture failed: %s: got "%s", want "%s"\n' "$label" "$got" "$want" >&2; exit 1; }
	git checkout -q main
}
touch_() { for f in "$@"; do echo "// $n" >>"$f"; done; }

case_ 'a .go change selects the package and its dependents' 'util' touch_ util/util.go
case_ 'an embedded .sql change selects the embedder and its reverse dependencies' 'app store' touch_ store/data.sql
case_ 'a .go change in a dependency selects the dependents' 'app store' touch_ store/store.go
case_ 'a testdata file selects its package' 'app store' touch_ store/testdata/case.txt
case_ 'a .go file in testdata selects its package' 'app store' touch_ store/testdata/x/x.go
case_ 'a lint fixture selects the lint package' 'internal/lint' touch_ internal/lint/testdata/badnil/aliased.go
case_ 'a deleted lint fixture selects the lint package' 'internal/lint' git rm -q internal/lint/testdata/badnil/aliased.go
case_ 'a nested module inside a package selects nothing' '-' touch_ app/sub/s.go
case_ 'a nested module subdirectory selects nothing' '-' touch_ desktop/internal/i.go
case_ 'a nested module dissolved into the main module selects its package' 'app/sub' git rm -q app/sub/go.mod
case_ 'a root package file selects the root package' 'root' touch_ root_test.go
case_ 'a root testdata file selects the root package' 'root' sh -c 'mkdir -p testdata && echo x >testdata/case.txt'
case_ 'a non-ASCII testdata name selects its package' 'app store' sh -c 'echo x >store/testdata/Ã©.txt'
case_ 'a name git must quote selects every package' 'root app internal/lint internal/operator/api/v1alpha1 store util web' sh -c 'echo x >"store/testdata/q\"x.txt"'
case_ 'go.mod selects every package' 'root app internal/lint internal/operator/api/v1alpha1 store util web' touch_ go.mod
case_ 'go.sum selects every package' 'root app internal/lint internal/operator/api/v1alpha1 store util web' sh -c 'echo "example.com/x v1.0.0 h1:x" >>go.sum'
case_ 'docs alone select nothing' '-' touch_ docs/a.md
case_ 'a package whose only test file fails to load is still selected' 'util' sh -c 'printf "//go:build (\n\npackage util\n" >util/util_test.go'
case_ 'the nested desktop module selects nothing' '-' touch_ desktop/d.go
case_ 'a deleted embedded file selects the embedder' 'app store' git rm -q store/data.sql
case_ 'a deleted package selects what still imports it' 'web' git rm -q -r gone
case_ 'a CRD file selects the package whose tests read it' 'internal/operator/api/v1alpha1' touch_ chart/hikyo/crds/a.yaml

# A failing selector must fail, not print "nothing affected".
git checkout -q -b broken main
echo x >util/util.go.txt
git add -A
git commit -q -m x
printf 'not a go.mod\n' >go.mod
if "$script" main HEAD >out 2>err; then
	echo 'affected-packages fixture failed: an unparsable go.mod selected nothing and exited 0' >&2
	exit 1
fi
git checkout -q -- go.mod
# A missing tool: a PATH with git and the shell but no jq.
mkdir -p "$root/nojq"
for tool in git go sh grep sed sort mktemp rm dirname cat tr; do
	ln -s "$(command -v "$tool")" "$root/nojq/$tool"
done
if PATH="$root/nojq" "$script" main HEAD >out 2>err; then
	echo 'affected-packages fixture failed: a missing jq exited 0' >&2
	exit 1
fi
grep -F 'jq is required' err >/dev/null || { echo 'affected-packages fixture failed: no message for a missing jq' >&2; exit 1; }
# go list failing for another reason (a stub go that errors).
mkdir -p "$root/badgo"
printf '#!/bin/sh\nexit 3\n' >"$root/badgo/go"
chmod +x "$root/badgo/go"
if PATH="$root/badgo:$PATH" "$script" main HEAD >out 2>err; then
	echo 'affected-packages fixture failed: a failing go list exited 0' >&2
	exit 1
fi
printf 'go-affected-packages fixture: .go, embedded, testdata, deleted, root, go.mod, go.sum, extra-rule, docs and nested-module cases select the right packages; a failing go list, go.mod or missing jq fails\n'
