#!/usr/bin/env bash
# Isolated synthetic history exercises feedback without contributor commits.
set -euo pipefail
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
script="$script_dir/fast-feedback.sh"
work=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-fast-feedback-test.XXXXXX")
trap 'rm -rf "$work"' EXIT
repo="$work/repo"
git init -q "$repo"
cd "$repo"
export GIT_AUTHOR_NAME='Feedback Fixture' GIT_AUTHOR_EMAIL='fixture@example.invalid'
export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
# Caller revision/tooling variables refer to another repository. Fixture cases
# provide their own explicit values instead of inheriting the CI workflow pair.
unset FAST_FEEDBACK_HEAD FAST_FEEDBACK_TOOLS_DIR
commit_fixture() {
	local tree revision
	git add .
	tree=$(git write-tree)
	if git rev-parse --verify HEAD >/dev/null 2>&1; then
		revision=$(printf 'feedback fixture\n' | git commit-tree "$tree" -p HEAD)
	else
		revision=$(printf 'feedback fixture\n' | git commit-tree "$tree")
	fi
	git update-ref HEAD "$revision"
}
printf '# Fixture\n' >README.md
printf '26.7.0\n' >.nvmrc
printf '{"packageManager":"pnpm@11.24.0"}\n' >package.json
printf 'module example.invalid/feedback-fixture\n\ngo 1.27.2\n' >go.mod
commit_fixture
base=$(git rev-parse HEAD)
export FAST_FEEDBACK_BASE="$base"
run_feedback() { bash "$script" "$@" >"$work/output" 2>&1; }
refuse() {
	local expected=$1
	shift
	if run_feedback "$@"; then
		printf 'fast-feedback fixture: unexpectedly accepted %s\n' "$expected" >&2
		exit 1
	fi
	grep -F "$expected" "$work/output" >/dev/null || { cat "$work/output" >&2; exit 1; }
}

run_feedback
grep -F 'selected checks passed' "$work/output" >/dev/null
FAST_FEEDBACK_BASE=missing-feedback-ref refuse 'missing comparison base'
printf 'uncommitted\n' >dirty.txt
refuse 'checkout is dirty'
run_feedback --working-tree
printf 'untracked whitespace \n' >dirty.txt
refuse 'untracked file whitespace differs' --working-tree
rm dirty.txt

printf 'whitespace \n' >README.md
commit_fixture
refuse 'trailing whitespace'
printf '# Good again\n' >README.md
commit_fixture
run_feedback
FAST_FEEDBACK_HEAD="$base" refuse 'checkout differs from requested revision'
FAST_FEEDBACK_HEAD="$base" refuse 'checkout differs from requested revision' --working-tree

printf 'package fixture\n\nvar value=1\n' >'space name.go'
commit_fixture
refuse 'changed Go files need gofmt'
gofmt -w 'space name.go'
commit_fixture
run_feedback
grep -F 'Go formatting passed (1 files)' "$work/output" >/dev/null

cat >compile_test.go <<'EOF'
package fixture

import "testing"

func TestCompilation(t *testing.T) { missing(t) }
EOF
commit_fixture
refuse 'undefined: missing'
rm compile_test.go
commit_fixture
run_feedback

# An unchanged file moved between packages can break the old package. Both
# rename owners must be compiled, even when the destination compiles cleanly.
mkdir old new
printf 'package fixture\n\nconst Value = 1\n' >old/definition.go
printf 'package fixture\n\nfunc consumer() int { return Value }\n' >old/consumer.go
commit_fixture
rename_base=$(git rev-parse HEAD)
mv old/definition.go new/definition.go
FAST_FEEDBACK_BASE="$rename_base" refuse 'undefined: Value' --working-tree
commit_fixture
FAST_FEEDBACK_BASE="$rename_base" refuse 'undefined: Value'
rm -r old new
commit_fixture

mkdir web
printf 'export const value = 1;\n' >web/fixture.ts
commit_fixture
run_feedback --plan
jq -e '.web == true and .client == false and .go == true' "$work/output" >/dev/null
refuse 'client dependencies missing'

# Frontend package commands are reused and their failures cannot become green.
mkdir -p clients/ts/node_modules web/node_modules "$work/bin"
cat >"$work/bin/pnpm" <<'EOF'
#!/bin/sh
if [ "$1" = --version ]; then printf '11.24.0\n'; exit 0; fi
if [ "$4" = test ]; then printf 'frontend unit failure\n'; exit 43; fi
EOF
chmod +x "$work/bin/pnpm"
cat >"$work/bin/node" <<'EOF'
#!/bin/sh
test "$1" = --version
printf 'v26.7.0\n'
EOF
chmod +x "$work/bin/node"
PATH="$work/bin:$PATH" refuse 'frontend unit failure' --working-tree
rm -r clients web/node_modules

# Deletions remain range-whitespace checked but are not handed to formatters.
rm web/fixture.ts
rmdir web
commit_fixture
run_feedback

# Deleting a file that existed on the comparison base still selects its owner.
mkdir -p web
printf 'export const before = 1;\n' >web/before.ts
commit_fixture
deletion_base=$(git rev-parse HEAD)
rm web/before.ts
commit_fixture
FAST_FEEDBACK_BASE="$deletion_base" refuse 'client dependencies missing'
rmdir web

mkdir -p .github/workflows scripts/ci
printf 'name: fixture\n' >.github/workflows/fixture.yml
cat >scripts/ci/run-go-tool.sh <<'EOF'
#!/bin/sh
test "$1" = actionlint
printf 'pinned actionlint invoked\n'
exit 23
EOF
chmod +x scripts/ci/run-go-tool.sh
commit_fixture
# The workflow check's failure must propagate after changed-shell validation.
refuse 'pinned actionlint invoked'

# Tooling can come from a separate immutable checkout while workflow inputs
# remain the tested checkout. Verify both the owner working directory and data.
mkdir -p "$work/tooling/scripts/ci"
cat >"$work/tooling/scripts/ci/run-go-tool.sh" <<'EOF'
#!/bin/sh
test "$1" = actionlint
test "$PWD" = "$FIXTURE_TOOLS_ROOT"
test "$2" = "$FIXTURE_SOURCE_ROOT/.github/workflows/fixture.yml"
printf 'separate tooling owner and exact source workflow verified\n'
exit 24
EOF
chmod +x "$work/tooling/scripts/ci/run-go-tool.sh"
FIXTURE_TOOLS_ROOT="$work/tooling" FIXTURE_SOURCE_ROOT="$repo" \
	FAST_FEEDBACK_TOOLS_DIR="$work/tooling/scripts/ci" \
	refuse 'separate tooling owner and exact source workflow verified'

# Docs-only inputs use the real checker from the tooling owner against source.
rm -rf .github scripts/ci
printf '%s\n' "$(node --version | sed 's/^v//')" >.nvmrc
commit_fixture
docs_base=$(git rev-parse HEAD)
for path in SECURITY.md SUPPORT.md GOVERNANCE.md TRADEMARK.md CONTRIBUTING.md LICENSE docs/status/README.md docs/release/signing.md docs/release/online-signing.md docs/release/legacy-offline-signing.md install/upgrade-nightly.sh; do
	mkdir -p "$(dirname "$path")"
	printf '# Fixture\n\nContent\n' >"$path"
done
printf '#!/bin/sh\nexit 0\n' >install/upgrade-nightly.sh
mkdir -p docs/site/scripts
printf 'throw new Error("source generator executed");\n' >docs/site/scripts/prepare-content.mjs
printf '# Contributing\n\n[Feedback](scripts/dev/README.md)\n' >CONTRIBUTING.md
commit_fixture
FAST_FEEDBACK_BASE="$docs_base" run_feedback --plan
jq -e '.docs == true and .web == false and .client == false' "$work/output" >/dev/null
FAST_FEEDBACK_BASE="$docs_base" FAST_FEEDBACK_TOOLS_DIR="$script_dir" run_feedback
[ ! -e docs/site/public ] && [ ! -e docs/site/src ]
printf '# Contributing\n\n[Broken](missing.md)\n' >CONTRIBUTING.md
commit_fixture
FAST_FEEDBACK_BASE="$docs_base" FAST_FEEDBACK_TOOLS_DIR="$script_dir" refuse 'relative links have no site mapping'
[ ! -e docs/site/public ] && [ ! -e docs/site/src ]
# Each consumed policy/site/runtime input, including deletions, selects docs.
selection_base=$(git rev-parse HEAD)
for path in SECURITY.md SUPPORT.md GOVERNANCE.md TRADEMARK.md CONTRIBUTING.md LICENSE docs/status/README.md docs/release/signing.md docs/release/online-signing.md docs/release/legacy-offline-signing.md install/upgrade-nightly.sh docs/site/scripts/prepare-content.mjs .nvmrc package.json pnpm-lock.yaml pnpm-workspace.yaml; do
	if [ -f "$path" ]; then
		mv "$path" "$work/input-backup"
		FAST_FEEDBACK_BASE="$selection_base" run_feedback --working-tree --plan
		mv "$work/input-backup" "$path"
	else
		printf 'fixture\n' >"$path"
		FAST_FEEDBACK_BASE="$selection_base" run_feedback --working-tree --plan
		rm "$path"
	fi
	jq -e '.docs == true' "$work/output" >/dev/null
done

printf 'fast-feedback fixture: clean/ref/dirty/whitespace/Go/filename/dependency/deletion/workflow/docs failure cases passed\n'
