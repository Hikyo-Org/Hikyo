#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-green-main.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM
commit=176e6e67379d0675e6211f0491dc965cee4f1c5c

write_gh() {
	conclusion=$1
	head=$2
	cat >"$fixture_dir/gh" <<EOF
#!/bin/sh
cat <<'JSON'
{"workflow_runs":[{"head_sha":"$head","conclusion":"$conclusion"}]}
JSON
EOF
	chmod +x "$fixture_dir/gh"
}

write_gh success "$commit"
GH_BIN="$fixture_dir/gh" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >/dev/null

for case_name in failed wrong-head; do
	if [ "$case_name" = failed ]; then
		write_gh failure "$commit"
	else
		write_gh success aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
	fi
	if GH_BIN="$fixture_dir/gh" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >/dev/null 2>&1; then
		printf 'green main fixture failed: %s run accepted\n' "$case_name" >&2
		exit 1
	fi
done

printf 'green main fixture: exact successful main CI required\n'
