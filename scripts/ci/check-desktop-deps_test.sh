#!/bin/sh
# Fixture for check-desktop-deps.sh: a throwaway repository plus a stub `go list`.
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

# The stub prints what `go list -deps -test` would: standard library, the
# desktop module itself, root packages (some as test variants), and a dependency.
cat >"$work/go" <<'STUB'
#!/bin/sh
[ ! -f "$STUB_LIST_FAIL" ] || exit 3
[ "$1" = list ] || exit 99
case "$*" in
*"-tags probe"*) if [ -f "$STUB_LIST_PROBE" ]; then cat "$STUB_LIST_PROBE"; else cat "$STUB_LIST"; fi ;;
*) cat "$STUB_LIST" ;;
esac
STUB
chmod +x "$work/go"
GO=$work/go
STUB_LIST=$work/list
STUB_LIST_FAIL=$work/list-fail
STUB_LIST_PROBE=$work/list-probe
export GO STUB_LIST STUB_LIST_PROBE STUB_LIST_FAIL

reset() {
	rm -rf "$work/repo" "$STUB_LIST_FAIL" "$STUB_LIST_PROBE"
	mkdir -p "$work/repo/desktop"
	printf 'module github.com/Hikyo-Org/hikyo\n\ngo 1.27\n\nrequire github.com/getkin/kin-openapi v0.1.0\n' >"$work/repo/go.mod"
	printf 'github.com/getkin/kin-openapi v0.1.0 h1:x\n' >"$work/repo/go.sum"
	printf 'module github.com/Hikyo-Org/hikyo/desktop\n\ngo 1.27\n\nrequire github.com/wailsapp/wails/v3 v3.1.0\n\nreplace github.com/Hikyo-Org/hikyo => ../\n' >"$work/repo/desktop/go.mod"
	cat >"$STUB_LIST" <<'LIST'
fmt
github.com/wailsapp/wails/v3/pkg/application
github.com/Hikyo-Org/hikyo/desktop
github.com/Hikyo-Org/hikyo/desktop/internal/shell
github.com/Hikyo-Org/hikyo/desktop_test [github.com/Hikyo-Org/hikyo/desktop.test]
github.com/Hikyo-Org/hikyo/internal/clientauth
github.com/Hikyo-Org/hikyo/internal/clienttrust [github.com/Hikyo-Org/hikyo/internal/clienttrust.test]
github.com/Hikyo-Org/hikyo/internal/clienttrust.test
github.com/Hikyo-Org/hikyo/internal/crypto
github.com/Hikyo-Org/hikyo/api
LIST
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
grep -F 'desktop/ stays out of the root module; 4 root packages' "$out" >/dev/null || fail 'success message wrong'

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
# does not list packages, and still guards the root module.
reset
rm -r "$work/repo/desktop"
: >"$STUB_LIST_FAIL"
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

reset
: >"$STUB_LIST_FAIL"
expect_refusal 'a go list failure' 'go list failed'

# A root package desktop/ compiles that the classifier does not select is the
# drift this check exists for.
reset
printf 'github.com/Hikyo-Org/hikyo/scripts/ci/gotools\n' >>"$STUB_LIST"
expect_refusal 'an import the classifier does not select' 'scripts/ci/gotools, but a change there does not select the desktop job'

# The default build can import what the probe build does not, and the reverse.
reset
printf 'github.com/Hikyo-Org/hikyo/scripts/ci/gotools\n' >>"$STUB_LIST"
sed '$d' "$STUB_LIST" >"$STUB_LIST_PROBE"
expect_refusal 'a default-build-only import' 'scripts/ci/gotools, but a change there does not select the desktop job'
reset
cp "$STUB_LIST" "$STUB_LIST_PROBE"
printf 'github.com/Hikyo-Org/hikyo/scripts/ci/gotools\n' >>"$STUB_LIST_PROBE"
expect_refusal 'a probe-build-only import' 'scripts/ci/gotools, but a change there does not select the desktop job'

# Only desktop's own external test package is skipped; a real root package of that name counts.
reset
printf 'github.com/Hikyo-Org/hikyo/desktop_test\n' >>"$STUB_LIST"
expect_refusal 'a real root package named desktop_test' 'desktop/ imports desktop_test, but a change there does not select the desktop job'

reset
printf 'fmt\ngithub.com/Hikyo-Org/hikyo/desktop\n' >"$STUB_LIST"
expect_refusal 'no root imports' 'imports no root packages'

printf 'check-desktop-deps fixture: root go.mod, go.work, replace and classifier coverage refusals passed\n'
