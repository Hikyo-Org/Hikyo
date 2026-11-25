#!/bin/sh
# Fixture for check-desktop-deps.sh: a throwaway repository.
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
script=$script_dir/check-desktop-deps.sh
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-desktop-deps.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM
out=$work/out

fail() {
	printf 'check-desktop-deps fixture failed: %s\n' "$*" >&2
	[ ! -f "$out" ] || cat "$out" >&2
	exit 1
}

reset() {
	rm -rf "$work/repo"
	mkdir -p "$work/repo/desktop"
	printf 'module github.com/Hikyo-Org/hikyo\n\ngo 1.27\n\nrequire github.com/getkin/kin-openapi v0.1.0\n' >"$work/repo/go.mod"
	printf 'github.com/getkin/kin-openapi v0.1.0 h1:x\n' >"$work/repo/go.sum"
	printf 'module github.com/Hikyo-Org/hikyo/desktop\n\ngo 1.27\n\nrequire github.com/wailsapp/wails/v3 v3.1.0\n\nreplace github.com/Hikyo-Org/hikyo => ../\n' >"$work/repo/desktop/go.mod"
}

run() { # exit status of the script on the fixture repository, output in $out
	set +e
	"$script" "$work/repo" >"$out" 2>&1
	status=$?
	set -e
	return "$status"
}
expect_refusal() { # LABEL TEXT
	if run; then fail "$1 was accepted"; fi
	grep -F -e "$2" "$out" >/dev/null || fail "$1 refusal lacks '$2'"
}

reset
run || fail 'a clean layout was refused'
grep -F 'desktop/ stays out of the root module' "$out" >/dev/null || fail 'success message wrong'

reset
printf 'github.com/wailsapp/wails/v3 v3.1.0\n' >>"$work/repo/go.mod"
expect_refusal 'Wails in the root go.mod' 'desktop-only dependency'

reset
printf 'github.com/wailsapp/wails/v3 v3.1.0 h1:y\n' >>"$work/repo/go.sum"
expect_refusal 'Wails in the root go.sum' 'desktop-only dependency'

for module in github.com/ebitengine/purego github.com/go-ole/go-ole; do
	reset
	printf '%s v1.0.0 h1:y\n' "$module" >>"$work/repo/go.sum"
	expect_refusal "$module in the root go.sum" 'desktop-only dependency'
done

reset
: >"$work/repo/go.work"
expect_refusal 'go.work' 'go.work joins desktop'

reset
printf 'require github.com/Hikyo-Org/hikyo/desktop v0.0.0\n' >>"$work/repo/go.mod"
expect_refusal 'a root requirement on desktop' 'references the desktop module'

reset
printf 'replace github.com/Hikyo-Org/hikyo/desktop => ./desktop\n' >>"$work/repo/go.mod"
expect_refusal 'a root replace of desktop' 'references the desktop module'

reset
sed '/^replace/d' "$work/repo/desktop/go.mod" >"$work/gomod" && mv "$work/gomod" "$work/repo/desktop/go.mod"
expect_refusal 'a missing replace' 'must keep: replace'

reset
sed 's|=> ../|=> ../../elsewhere|' "$work/repo/desktop/go.mod" >"$work/gomod" && mv "$work/gomod" "$work/repo/desktop/go.mod"
expect_refusal 'a replace to another directory' 'must keep: replace'

# The grouped replace form is the same mapping.
reset
printf 'module github.com/Hikyo-Org/hikyo/desktop\n\ngo 1.27\n\nreplace (\n\tgithub.com/other/x => ../x // note\n\tgithub.com/Hikyo-Org/hikyo => ../\n)\n' >"$work/repo/desktop/go.mod"
run || fail 'a grouped replace was refused'
reset
printf 'module github.com/Hikyo-Org/hikyo/desktop\n\nreplace (\n\tgithub.com/Hikyo-Org/hikyo => ../elsewhere\n)\n' >"$work/repo/desktop/go.mod"
expect_refusal 'a grouped replace to another directory' 'must keep: replace'

# Go sources without the module manifest must not pass unvalidated.
reset
rm "$work/repo/desktop/go.mod"
printf 'package main\n' >"$work/repo/desktop/main.go"
expect_refusal 'desktop Go sources without go.mod' 'desktop/go.mod is missing'
reset
rm "$work/repo/desktop/go.mod"
printf 'doc\n' >"$work/repo/desktop/README.md"
run || fail 'a desktop/ without Go sources or go.mod was refused'

# Where desktop/ does not exist yet (main before the shell lands) the check passes,
# and still guards the root module.
reset
rm -r "$work/repo/desktop"
run || fail 'a tree without desktop/ was refused'
grep -F 'no desktop/ module here' "$out" >/dev/null || fail 'absent-desktop message wrong'
reset
rm -r "$work/repo/desktop"
printf 'github.com/wailsapp/wails/v3 v3.1.0\n' >>"$work/repo/go.mod"
expect_refusal 'Wails in the root go.mod without desktop/' 'desktop-only dependency'
reset
rm -r "$work/repo/desktop"
: >"$work/repo/go.work"
expect_refusal 'go.work without desktop/' 'go.work joins desktop'

printf 'check-desktop-deps fixture: root go.mod, go.work and replace refusals passed\n'
