#!/bin/sh
# Print the import paths of the root-module packages whose tests the changes
# between BASE and REV can affect, one per line:
#   scripts/ci/go-affected-packages.sh BASE [REV]
# Run it in a tree that holds REV (the pre-push hook uses a detached worktree);
# `go list` reads that tree, git only supplies the changed file names.
# Every changed or deleted file counts for the nearest ancestor directory that
# is a package at BASE or REV (testdata, dot and underscore directories are not,
# so their files, .go included, reach the owning package, as do embedded files;
# a deleted file, or a whole deleted package, still counts). Files in a nested
# module (desktop/) count for nothing. Selected:
# those packages, packages that embed a changed file, packages that no longer
# load, every package whose test build depends on any of them, and all packages
# when go.mod or go.sum changes. Packages without tests are never printed.
# `go list ./...` already leaves out the nested desktop/ module and testdata,
# dot and underscore directories. Exit 0 with no output means nothing is
# affected; any failure to find that out exits non-zero.
#
# shortcut: tests that read repository files at runtime (os.ReadFile of a path
# that is not embedded) are invisible to this, except the explicit rules in
# extra_rules below; upgrade by adding a rule when such a test breaks unseen.
set -eu

[ "$#" -ge 1 ] && [ "$#" -le 2 ] || {
	printf 'usage: %s BASE [REV]\n' "$0" >&2
	exit 2
}
for tool in git go jq; do
	command -v "$tool" >/dev/null 2>&1 || {
		printf 'go-affected-packages: %s is required but not on PATH\n' "$tool" >&2
		exit 1
	}
done
base=$1
rev=${2:-HEAD}
root=$(git rev-parse --show-toplevel)
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-affected.XXXXXX")
trap 'rm -rf "$work"' EXIT
trap 'exit 130' HUP INT TERM

# quotePath=false keeps non-ASCII names verbatim; a name git must still quote
# (control characters, quotes, backslashes) cannot be matched, so it selects all.
git -c core.quotePath=false diff --name-only --no-renames "$base" "$rev" >"$work/changed"
[ -s "$work/changed" ] || exit 0

# Non-embedded files a test reads at runtime: "changed-path-prefix package-dir".
# The internal/store/queries rule serves only this script's own output: the
# pre-push runner leaves internal/isolation to CI.
extra_rules='
chart/hikyo/crds/ internal/operator/api/v1alpha1
internal/store/queries/ internal/isolation
docs/release/measurements/ internal/bench
'
printf '%s\n' "$extra_rules" | while read -r prefix dir; do
	[ -n "$prefix" ] || continue
	if grep -q "^$prefix" "$work/changed"; then printf '%s\n' "$dir"; fi
done >"$work/extra"

# Path segments that never make a Go package.
ignored='(^|/)(testdata|[._][^/]*)(/|$)'
git -c core.quotePath=false ls-tree -r --name-only "$base" >"$work/basefiles"
git -c core.quotePath=false ls-tree -r --name-only "$rev" >"$work/revfiles"
# Nested module roots ("dir/") at REV only: a module dissolved into the main one
# selects its packages; files of a deleted module select nothing.
{ grep '/go\.mod$' "$work/revfiles" || :; } | sed 's|go\.mod$||' | { grep -Ev "$ignored" || :; } | sort -u >"$work/nested"
# Package directories at BASE, for packages the change deletes.
{ grep '\.go$' "$work/basefiles" || :; } | sed 's|/[^/]*$||; t; s|.*||' | { grep -Ev "$ignored" || :; } | sort -u >"$work/basedirs"

all=false
if grep -Fxq -e go.mod -e go.sum "$work/changed" || grep -q '^"' "$work/changed"; then all=true; fi

cd "$root"
go list -test -e -json=ImportPath,Dir,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,Deps,Error,DepsErrors ./... >"$work/list"
[ -s "$work/list" ] || {
	printf 'go-affected-packages: go list found no packages\n' >&2
	exit 1
}
jq -rs --arg root "$root" --argjson all "$all" \
	--rawfile changed "$work/changed" --rawfile extra "$work/extra" --rawfile basedirs "$work/basedirs" --rawfile nested "$work/nested" '
	def lines: split("\n") | map(select(. != ""));
	def rel: if . == $root then "" else ltrimstr($root + "/") end;
	def dirname: split("/")[:-1] | join("/");
	[.[] | select((.ImportPath | contains(" [")) | not)] as $units
	| [$units[] | select(.ImportPath | endswith(".test") | not)] as $pkgs
	| ($pkgs | map(.Dir | rel) + ($basedirs | lines) | unique) as $pkgdirs
	# the nearest ancestor directory holding Go files; the root package only for root
	# files and root-level testdata, _ and . directories (never for docs/ and the like)
	| ($nested | lines) as $modules
	| def owner: . as $file | dirname as $d
		| if any($modules[]; . as $m | $file | startswith($m)) then null
		  else (if $d == "" then [""] else [$d | split("/") | range(length; 0; -1) as $n | .[:$n] | join("/")]
				+ (if $d | test("^(testdata|[._][^/]*)(/|$)") then [""] else [] end) end
			| map(select(. as $c | $pkgdirs | index($c) != null)) | first // null)
		  end;
	($changed | lines) as $files
	| (($files | map(owner | select(. != null))) + ($extra | lines)) as $dirs
	| [$pkgs[] | (.Dir | rel) as $dir
		| select(($dirs | index($dir)) != null
			or (.Error != null) or ((.DepsErrors // []) | length > 0)
			or ([(.EmbedFiles // [])[], (.TestEmbedFiles // [])[], (.XTestEmbedFiles // [])[]
				| (if $dir == "" then . else $dir + "/" + . end)] | any(. as $f | $files | index($f) != null)))
		| .ImportPath] as $touched
	| ([$units[] | select(.ImportPath | endswith(".test"))
		| select($all or ((.ImportPath | rtrimstr(".test")) as $pkg
			| ($touched | index($pkg)) != null
			or (.Error != null) or ((.DepsErrors // []) | length > 0)
			or ([(.Deps // [])[] | sub(" \\[.*$"; "")] | any(. as $d | $touched | index($d) != null))))
		| .ImportPath | rtrimstr(".test")]
	# a package that fails to load may have no test binary (say its only test file
	# has a broken build constraint); select it anyway so go test fails on it
	+ [$pkgs[] | select((.Error != null) or ((.DepsErrors // []) | length > 0)) | .ImportPath])
	| unique[]' <"$work/list" >"$work/out"
sort "$work/out"
