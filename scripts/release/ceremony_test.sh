#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-ceremony-test.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM

mkdir -p "$fixture_dir/bin" "$fixture_dir/state"
cat >"$fixture_dir/bin/uname" <<'EOF'
#!/bin/sh
printf 'Darwin\n'
EOF
chmod +x "$fixture_dir/bin/uname"

output=$(printf '1\n1.0.0-alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run)

printf '%s\n' "$output" | grep -F '1. One-time trust bootstrap' >/dev/null
printf '%s\n' "$output" | grep -F 'Dry run: bootstrap 1.0.0-alpha.0' >/dev/null
[ ! -e "$fixture_dir/state/hikyo/release-ceremony/1.0.0-alpha.0/state.json" ]

printf 'release ceremony fixture: dry-run bootstrap is mutation-free\n'

publish_output=$(printf '5\n1.0.0-alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run)

printf '%s\n' "$publish_output" | grep -F 'Dry run: publish 1.0.0-alpha.0' >/dev/null
printf '%s\n' "$publish_output" | grep -F 'would require exact typed confirmation: publish v1.0.0-alpha.0' >/dev/null

tag_output=$(printf '3\n1.0.0-alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run)
printf '%s\n' "$tag_output" | grep -F 'Would verify production trust and release fixtures before the immutable tag.' >/dev/null

if printf '1\n1.0.0_alpha.0\n' | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" --dry-run >"$fixture_dir/invalid.out" 2>"$fixture_dir/invalid.err"
then
	printf 'release ceremony fixture: invalid SemVer was accepted\n' >&2
	exit 1
fi
grep -F 'invalid SemVer 1.0.0_alpha.0' "$fixture_dir/invalid.err" >/dev/null

printf 'release ceremony fixture: publish warning and SemVer refusal passed\n'

for fake_command in cosign diskutil hdiutil route; do
	cat >"$fixture_dir/bin/$fake_command" <<'EOF'
#!/bin/sh
case "$(basename "$0")" in
	route) exit 1 ;;
	*) exit 0 ;;
esac
EOF
	chmod +x "$fixture_dir/bin/$fake_command"
done
mkdir -p "$fixture_dir/primary-a" "$fixture_dir/primary-b" \
	"$fixture_dir/recovery-a" "$fixture_dir/recovery-b"

if printf '1\n1.0.0-alpha.0\n%s\n%s\n%s\n%s\nwrong\n' \
	"$fixture_dir/primary-a" "$fixture_dir/primary-b" \
	"$fixture_dir/recovery-a" "$fixture_dir/recovery-b" | env \
	PATH="$fixture_dir/bin:$PATH" \
	XDG_STATE_HOME="$fixture_dir/state" \
	"$script_dir/ceremony.sh" >"$fixture_dir/refusal.out" 2>"$fixture_dir/refusal.err"
then
	printf 'release ceremony fixture: wrong confirmation was accepted\n' >&2
	exit 1
fi
grep -F 'confirmation did not match; nothing changed' "$fixture_dir/refusal.err" >/dev/null
[ ! -e "$fixture_dir/primary-a/primary-1.key" ]
[ ! -e "$fixture_dir/state/hikyo/release-ceremony/1.0.0-alpha.0/state.json" ]

printf 'release ceremony fixture: destructive bootstrap confirmation is fail-closed\n'
