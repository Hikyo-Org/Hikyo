#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
script="$repo_root/scripts/release/nightly-changelog.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
export GIT_AUTHOR_NAME=Fixture GIT_AUTHOR_EMAIL=fixture@example.test
export GIT_COMMITTER_NAME=Fixture GIT_COMMITTER_EMAIL=fixture@example.test
export GITHUB_REPOSITORY=example/hikyo GITHUB_SERVER_URL=https://github.com

commit() {
	git -c commit.gpgsign=false commit -q --allow-empty "$@"
}

fail() {
	printf 'nightly changelog: %s\n' "$1" >&2
	exit 1
}

mkdir "$tmp/bin"
git init -q "$tmp/repo"
cd "$tmp/repo"
commit -m 'old change'
previous=$(git rev-parse HEAD)
commit -m 'feat: new nightly feature'
current=$(git rev-parse HEAD)
commit -m 'later change'

notes=$("$script" "$previous" "$current")
printf '%s\n' "$notes" | grep -F -- '- new nightly feature (' >/dev/null
printf '%s\n' "$notes" | grep -F "https://github.com/example/hikyo/compare/$previous...$current" >/dev/null
if printf '%s\n' "$notes" | grep -E 'old change|later change' >/dev/null; then
	echo 'nightly changelog includes commits outside the release range' >&2
	exit 1
fi
"$script" '' "$current" | grep -F 'old change' >/dev/null
empty=$("$script" "$current" "$current")
printf '%s\n' "$empty" | grep -F 'No new non-merge commits.' >/dev/null
if printf '%s\n' "$empty" | grep -E 'SemVer|^- ' >/dev/null; then
	fail 'an empty range still reports changes'
fi
if "$script" missing "$current" >/dev/null 2>&1; then
	echo 'nightly changelog accepts a missing predecessor' >&2
	exit 1
fi
if "$script" "$previous" missing >/dev/null 2>&1; then
	echo 'nightly changelog accepts a missing current commit' >&2
	exit 1
fi

# Oldest commit first, deliberately out of section order.
base=$(git rev-parse HEAD)
commit -m 'not a conventional subject'
commit -m 'wibble: an unknown type stays whole'
commit -m 'wibble!: an unknown type can still break'
commit -m 'docs: mention the footer' -m 'see BREAKING CHANGE: mid-line is not a footer'
commit -m 'fix(api):'
commit -m 'feat(*): an odd scope stays plain'
commit -m 'FEAT(Web): an uppercase type still counts'
commit -m 'chore: rename the flag' -m 'BREAKING-CHANGE: the hyphen form counts.'
commit -m 'other: cannot claim the unclassified section'
commit -m 'breaking: cannot claim the major section'
commit -m 'style: format the sources'
commit -m 'chore(deps): bump a dependency'
commit -m 'build: pin the toolchain'
commit -m 'ci: tune the runners'
commit -m 'docs: describe the groups'
commit -m 'test(web): cover the page'
commit -m 'refactor: split the module'
commit -m 'Revert "feat: old idea"'
commit -m 'perf(api): cache the lookup'
commit -m 'fix(cli): keep the flag'
commit -m 'feat(web): add the page'
commit -m 'feat(api)!: drop the old route'
commit -m 'fix: rename the setting' -m 'BREAKING CHANGE: the old name is refused.'
head=$(git rev-parse HEAD)
actual=$("$script" "$base" "$head" | sed -E 's/ \([0-9a-f]{7,}\)$//' | grep -E '^(Suggested SemVer|### |- )')
expected=$(cat <<'NOTES'
Suggested SemVer bump from commit types: **major**. The nightly version itself stays on the latest stable base.
### Breaking changes (major)
- rename the setting
- **api:** drop the old route
- rename the flag
- wibble!: an unknown type can still break
### Features (minor)
- **web:** add the page
- **Web:** an uppercase type still counts
- *: an odd scope stays plain
### Fixes (patch)
- **cli:** keep the flag
- fix(api):
### Performance (patch)
- **api:** cache the lookup
### Reverts (patch)
- Revert "feat: old idea"
### Refactoring (no bump)
- split the module
### Tests (no bump)
- **web:** cover the page
### Documentation (no bump)
- describe the groups
- mention the footer
### CI (no bump)
- tune the runners
### Build (no bump)
- pin the toolchain
### Chores (no bump)
- **deps:** bump a dependency
### Style (no bump)
- format the sources
### Other (unclassified)
- breaking: cannot claim the major section
- other: cannot claim the unclassified section
- wibble: an unknown type stays whole
- not a conventional subject
NOTES
)
[ "$actual" = "$expected" ] || fail "unexpected grouping:
$actual"

# Blank lines around headings are what make the sections render as Markdown.
before=$(git rev-parse HEAD)
commit -m 'fix: the second section'
commit -m 'feat: the first section'
actual=$("$script" "$before" HEAD | sed -E 's/ \([0-9a-f]{7,}\)$//' | sed '1,/^\[Full comparison\]/d')
expected=$(cat <<'NOTES'

Suggested SemVer bump from commit types: **minor**. The nightly version itself stays on the latest stable base.

### Features (minor)

- the first section

### Fixes (patch)

- the second section
NOTES
)
[ "$actual" = "$expected" ] || fail "unexpected layout:
$actual"

before=$(git rev-parse HEAD)
git checkout -q -b side
commit -m 'chore: the side change'
git checkout -q -
git -c commit.gpgsign=false merge -q --no-ff -m 'feat: the merge subject' side
notes=$("$script" "$before" HEAD)
printf '%s\n' "$notes" | grep -F 'the side change' >/dev/null || fail 'merged work is missing'
if printf '%s\n' "$notes" | grep -F 'the merge subject' >/dev/null; then
	fail 'a merge commit is listed'
fi

for case in 'feat: a|minor' 'fix: a|patch' 'perf: a|patch' 'Revert "x"|patch' 'revert: a|patch' 'docs: a|none' 'plain|unknown'; do
	before=$(git rev-parse HEAD)
	commit -m "${case%|*}"
	"$script" "$before" HEAD | grep -F "from commit types: **${case##*|}**" >/dev/null || fail "wrong bump for: ${case%|*}"
done

# An unclassified subject is unknown only when nothing typed outranks it.
for case in 'docs: a|unknown' 'fix: a|patch' 'feat: a|minor'; do
	before=$(git rev-parse HEAD)
	commit -m 'plain'
	commit -m "${case%|*}"
	"$script" "$before" HEAD | grep -F "from commit types: **${case##*|}**" >/dev/null || fail "wrong precedence for: ${case%|*}"
done

# A failed step must fail the workflow step, not publish truncated notes.
printf '#!/bin/sh\nexit 1\n' >"$tmp/bin/awk"
chmod +x "$tmp/bin/awk"
if PATH="$tmp/bin:$PATH" "$script" "$base" "$head" >/dev/null 2>&1; then
	fail 'a failed awk still succeeds'
fi
rm "$tmp/bin/awk"
real_git=$(command -v git)
for args in '--format=%h %s' '--grep='; do
	printf '#!/bin/sh\ncase "$*" in *"%s"*) exit 1 ;; esac\nexec "%s" "$@"\n' "$args" "$real_git" >"$tmp/bin/git"
	chmod +x "$tmp/bin/git"
	if PATH="$tmp/bin:$PATH" "$script" "$base" "$head" >/dev/null 2>&1; then
		fail "a failed git log with $args still succeeds"
	fi
done
printf 'nightly changelog: exact range, first release, empty range, invalid refs, grouping, layout, merges, bumps and failures passed\n'
