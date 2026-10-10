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
{"workflow_runs":[{"head_sha":"$head","head_branch":"main","event":"workflow_dispatch","path":".github/workflows/ci.yml","status":"completed","conclusion":"$conclusion"}]}
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

write_gh success "$commit"
cp "$fixture_dir/gh" "$fixture_dir/valid-gh"
for mutation in \
	'.workflow_runs[0].head_branch = "feature"' \
	'.workflow_runs[0].event = "pull_request"' \
	'.workflow_runs[0].event = "schedule"' \
	'.workflow_runs[0].path = ".github/workflows/cache-warm.yml"' \
	'.workflow_runs[0].status = "in_progress"' \
	'.workflow_runs[0].conclusion = "cancelled"' \
	'.workflow_runs = []'; do
	"$fixture_dir/valid-gh" | jq "$mutation" >"$fixture_dir/run.json"
	cat >"$fixture_dir/gh" <<EOF
#!/bin/sh
cat '$fixture_dir/run.json'
EOF
	chmod +x "$fixture_dir/gh"
	if GH_BIN="$fixture_dir/gh" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >/dev/null 2>&1; then
		printf 'green main fixture failed: invalid provenance accepted: %s\n' "$mutation" >&2
		exit 1
	fi
done
"$fixture_dir/valid-gh" | jq '.workflow_runs[0].event = "push"' >"$fixture_dir/run.json"
GH_BIN="$fixture_dir/gh" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >/dev/null

# The newest exact-SHA attempt governs proof; an older success cannot hide it.
"$fixture_dir/valid-gh" | jq '.workflow_runs = [(.workflow_runs[0] | .conclusion = "failure"), .workflow_runs[0]]' >"$fixture_dir/run.json"
if GH_BIN="$fixture_dir/gh" "$script_dir/require-green-main.sh" Hikyo-Org/hikyo "$commit" >/dev/null 2>&1; then
	printf 'green main fixture failed: older success hid a newer failure\n' >&2
	exit 1
fi

printf 'green main fixture: exact successful manual main CI and legacy push provenance required\n'
