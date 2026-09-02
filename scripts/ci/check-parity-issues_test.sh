#!/bin/sh
set -eu

# Fixture for check-parity-issues.sh: a stub `gh` on PATH answers from
# PARITY_STUB, a space-separated list of `number=state` (state `pr` marks a
# pull request), and anything unlisted is an API error.

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
checker="$script_dir/check-parity-issues.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-parity-fixture.XXXXXX")
trap 'rm -rf "$work"' EXIT HUP INT TERM

mkdir "$work/bin"
cat >"$work/bin/gh" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
api) ;;
*) printf 'stub gh: unexpected subcommand %s\n' "$1" >&2; exit 64 ;;
esac
number=${2##*/}
for pair in $PARITY_STUB; do
	if [ "${pair%%=*}" = "$number" ]; then
		case "${pair#*=}" in
		pr) printf 'open true\n' ;;
		*) printf '%s false\n' "${pair#*=}" ;;
		esac
		exit 0
	fi
done
printf 'HTTP 404: Not Found\n' >&2
exit 1
EOF
chmod +x "$work/bin/gh"
PATH="$work/bin:$PATH"
export PATH
GH_REPO=fixture/repo
export GH_REPO

registry="$work/parity.yaml"
cat >"$registry" <<'EOF'
# a comment mentioning issue: 999 must not count
operations:
  alpha: {issue: 157}
  beta:  {webui: matrix}
  gamma: {issue: 568, note: "issue: 42 in prose is text, not a reference"}
  delta: {issue: 157}
EOF

expect_pass() {
	if ! out=$("$checker" "$registry" 2>&1); then
		printf 'parity fixture failed: %s\n%s\n' "$1" "$out" >&2
		exit 1
	fi
}

expect_fail() {
	if out=$("$checker" "$registry" 2>&1); then
		printf 'parity fixture failed: %s\n%s\n' "$1" "$out" >&2
		exit 1
	fi
	if ! printf '%s\n' "$out" | grep -F -- "$2" >/dev/null; then
		printf 'parity fixture failed: %s: expected %s in\n%s\n' "$1" "$2" "$out" >&2
		exit 1
	fi
}

# The prose reference to 42 must be a reference too: it is inside a row, and
# the extractor is deliberately simple. Feed it as open so the pass case holds.
PARITY_STUB='157=open 568=open 42=open' expect_pass 'all open accepted'
PARITY_STUB='157=open 568=closed 42=open' expect_fail 'closed issue refused' '#568 is closed'
PARITY_STUB='157=pr 568=open 42=open' expect_fail 'pull request refused' '#157 is a pull request'
PARITY_STUB='568=open 42=open' expect_fail 'unreadable issue refused' 'cannot read fixture/repo#157'

printf 'operations: {}\n' >"$registry"
PARITY_STUB='' expect_pass 'registry without issue rows accepted'

if "$checker" "$work/missing.yaml" >/dev/null 2>&1; then
	printf 'parity fixture failed: missing registry accepted\n' >&2
	exit 1
fi

printf 'parity issues fixture: open accepted; closed, pull request, unreadable and missing refused\n'
