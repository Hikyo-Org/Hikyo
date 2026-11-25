#!/bin/sh
# Print the import paths of the root-module packages whose tests the changes
# between BASE and REV can affect, one per line:
#   scripts/ci/go-affected-packages.sh BASE [REV]
# Run it in a tree that holds REV (the pre-push hook uses a detached worktree);
# `go list` reads that tree, git only supplies the changed file names.
# Selected: packages with a changed .go file in their directory, packages that
# embed a changed non-Go file, every package whose test build depends on one of
# those, and all packages when go.mod or go.sum changes. Packages without tests
# are never printed. `go list ./...` already leaves out the nested desktop/
# module and testdata, dot and underscore directories.
#
# shortcut: tests that read repository files at runtime (os.ReadFile of a path
# that is not embedded) are invisible to this, except the explicit rules in
# extra_rules below; upgrade by adding a rule when such a test breaks unseen.
set -eu

[ "$#" -ge 1 ] && [ "$#" -le 2 ] || {
	printf 'usage: %s BASE [REV]\n' "$0" >&2
	exit 2
}
base=$1
rev=${2:-HEAD}
root=$(git rev-parse --show-toplevel)

changed=$(git diff --name-only --no-renames "$base" "$rev") || exit 1
[ -n "$changed" ] || exit 0

# Non-embedded files a test reads at runtime: "changed-path-prefix package-dir".
extra_rules='
chart/hikyo/crds/ internal/operator/api/v1alpha1
internal/store/queries/ internal/isolation
docs/release/measurements/ internal/bench
'
extra=$(printf '%s\n' "$changed" | while read -r file; do
	printf '%s\n' "$extra_rules" | while read -r prefix dir; do
		[ -n "$prefix" ] || continue
		case "$file" in
			("$prefix"*) printf '%s/x.extra\n' "$dir" ;;
		esac
	done
done)
changed=$(printf '%s\n%s\n' "$changed" "$extra")

all=false
if printf '%s\n' "$changed" | grep -Fxq -e go.mod -e go.sum; then all=true; fi

cd "$root"
go list -test -e -json=ImportPath,Dir,EmbedFiles,TestEmbedFiles,XTestEmbedFiles,Deps ./... |
	jq -rs --arg root "$root" --argjson all "$all" --arg changed "$changed" '
	def rel: if . == $root then "" else ltrimstr($root + "/") end;
	def dirname: split("/")[:-1] | join("/");
	($changed | split("\n") | map(select(. != ""))) as $files
	| ($files | map(select(endswith(".go") or endswith(".extra")) | dirname)) as $godirs
	| ($files | map(select(endswith(".go") | not))) as $others
	| [.[] | select((.ImportPath | contains(" [")) | not)] as $units
	| [$units[] | select(.ImportPath | endswith(".test") | not)
		| (.Dir | rel) as $dir
		| select(($godirs | index($dir)) != null
			or ([(.EmbedFiles // [])[], (.TestEmbedFiles // [])[], (.XTestEmbedFiles // [])[]
				| (if $dir == "" then . else $dir + "/" + . end)] | any(. as $f | $others | index($f) != null)))
		| .ImportPath] as $touched
	| $units[] | select(.ImportPath | endswith(".test"))
	| select($all or ((.ImportPath | rtrimstr(".test")) as $pkg
		| ($touched | index($pkg)) != null
		or ([(.Deps // [])[] | sub(" \\[.*$"; "")] | any(. as $d | $touched | index($d) != null))))
	| .ImportPath | rtrimstr(".test")' | sort
