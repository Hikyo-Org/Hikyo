#!/bin/sh
set -eu

[ "$#" -eq 2 ] || {
	printf 'usage: %s PREVIOUS_COMMIT CURRENT_COMMIT\n' "$0" >&2
	exit 2
}

previous=$1
current=$(git rev-parse --verify --end-of-options "$2^{commit}")
if [ -n "$previous" ]; then
	previous=$(git rev-parse --verify --end-of-options "$previous^{commit}")
	range="$previous..$current"
	printf '\n## Changes since the previous nightly\n\n'
	if [ -n "${GITHUB_REPOSITORY:-}" ]; then
		printf '[Full comparison](%s/%s/compare/%s...%s)\n\n' \
			"${GITHUB_SERVER_URL:-https://github.com}" "$GITHUB_REPOSITORY" "$previous" "$current"
	fi
else
	range=$current
	printf '\n## Changes in the first nightly\n\n'
	printf 'No previous published nightly exists; listing reachable commit history.\n\n'
fi

changes=$(git log --no-merges --format='%h %s' "$range" --)
if [ -z "$changes" ]; then
	printf 'No new non-merge commits.\n'
	exit 0
fi

breaking=$(git log --format=%h --grep='^BREAKING[ -]CHANGE:' "$range" --)
printf '%s\n' "$changes" | BREAKING=$breaking awk '
BEGIN {
	# "!" and "?" cannot be commit types, so a "breaking:" or "other:" subject cannot claim them.
	sections = split("! feat fix perf revert refactor test docs ci build chore style ?", order, " ")
	title["!"] = "Breaking changes (major)"
	title["feat"] = "Features (minor)"
	title["fix"] = "Fixes (patch)"
	title["perf"] = "Performance (patch)"
	title["revert"] = "Reverts (patch)"
	title["refactor"] = "Refactoring (no bump)"
	title["test"] = "Tests (no bump)"
	title["docs"] = "Documentation (no bump)"
	title["ci"] = "CI (no bump)"
	title["build"] = "Build (no bump)"
	title["chore"] = "Chores (no bump)"
	title["style"] = "Style (no bump)"
	title["?"] = "Other (unclassified)"
	footers = split(ENVIRON["BREAKING"], hashes, "\n")
	for (i = 1; i <= footers; i++) footer[hashes[i]] = 1
}
{
	hash = $1
	text = substr($0, length(hash) + 2)
	type = "?"
	scope = ""
	bang = 0
	if (match(text, /^[A-Za-z][A-Za-z0-9_-]*(\([^)]*\))?!?: */)) {
		prefix = substr(text, 1, RLENGTH)
		candidate = prefix
		sub(/[(!:].*/, "", candidate)
		candidate = tolower(candidate)
		bang = prefix ~ /!: *$/
		if (candidate in title) {
			type = candidate
			# A subject with nothing after its prefix keeps the prefix, so its bullet is not blank.
			if (length(text) > RLENGTH) {
				text = substr(text, RLENGTH + 1)
				if (match(prefix, /\([^)]*\)/)) scope = substr(prefix, RSTART + 1, RLENGTH - 2)
			}
		}
	} else if (text ~ /^Revert /) {
		type = "revert"
	}
	if (bang || hash in footer) type = "!"
	# A scope such as "*" would break the bold markup.
	if (scope != "") text = (scope ~ /^[A-Za-z0-9_#.\/-]+$/ ? "**" scope ":** " : scope ": ") text
	items[type] = items[type] "- " text " (" hash ")\n"
}
END {
	if ("!" in items) bump = "major"
	else if ("feat" in items) bump = "minor"
	else if ("fix" in items || "perf" in items || "revert" in items) bump = "patch"
	else if ("?" in items) bump = "unknown"
	else bump = "none"
	printf "Suggested SemVer bump from commit types: **%s**. The nightly version itself stays on the latest stable base.\n", bump
	for (i = 1; i <= sections; i++) {
		if (order[i] in items) printf "\n### %s\n\n%s", title[order[i]], items[order[i]]
	}
}'
