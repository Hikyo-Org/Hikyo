#!/bin/sh
# GitHub expressions and embedded shell snippets below are literal fixture text.
# shellcheck disable=SC2016
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
workflow="$repo_root/.github/workflows/ci.yml"
workflows_dir="$repo_root/.github/workflows"
tool_modules="$repo_root/scripts/ci/go-tool-modules.txt"

fail() {
	printf 'cache policy fixture failed: %s\n' "$1" >&2
	exit 1
}

require_line() {
	file=$1
	expected=$2
	grep -F -- "$expected" "$file" >/dev/null ||
		fail "missing $expected in $(basename "$file")"
}

workflow_job_block() {
	file=$1
	job=$2
	awk -v header="  $job:" '
		$0 == header { found = 1 }
		found && $0 != header && $0 ~ /^  [A-Za-z0-9_-]+:/ { exit }
		found { print }
	' "$file"
}

set --
for candidate in "$workflows_dir"/*.yml "$workflows_dir"/*.yaml; do
	[ -f "$candidate" ] || continue
	set -- "$@" "$candidate"
done
[ "$#" -gt 0 ] || fail 'no GitHub workflow files found'

# Hikyo deliberately uses the free GitHub-hosted pool. Keep runner placement
# reviewable instead of allowing a third-party label to return unnoticed.
for file in "$@"; do
	if grep -iE 'blacksmith|buildjet|self-hosted' "$file" >/dev/null; then
		fail "non-GitHub runner reference in $(basename "$file")"
	fi
	expected_runner=ubuntu-latest
	case $(basename "$file") in
		ops-floor.yml)
			# The secret-free operations lane measures both native CPU floors.
			# Keep its matrix closed to these two GitHub-hosted architectures.
			expected_runner='${{ matrix.runner }}'
			runners=$(sed -n 's/^[[:space:]]*runner:[[:space:]]*//p' "$file" | sort)
			[ "$runners" = "$(printf '%s\n' ubuntu-24.04 ubuntu-24.04-arm)" ] ||
				fail 'operations floor runner matrix must contain exactly the native GitHub pair'
			require_line "$file" '          cache: false'
			if grep -E 'actions/cache(@|/)' "$file" >/dev/null; then
				fail 'operations floor workflow must not use shared caches'
			fi
			;;
		floor-acceptance.yml | operator-floor.yml)
			# Native arm64 acceptance is an explicit release obligation. These
			# manual jobs never share the ordinary x86 build caches.
			expected_runner=ubuntu-24.04-arm
			require_line "$file" '  workflow_dispatch:'
			awk '
				/^on:/ { active = 1; if ($0 != "on:") bad = 1; next }
				active && /^[^[:space:]#]/ { active = 0 }
				active && /^  [^ #]/ { if ($0 != "  workflow_dispatch:") bad = 1; else manual = 1 }
				END { if (bad || !manual) exit 1 }
			' "$file" || fail "release floor workflow must be manual-only"
			require_line "$file" '          cache: false'
			if grep -E 'actions/cache(@|/)' "$file" >/dev/null; then
				fail "release floor workflow must not use shared caches"
			fi
			;;
	esac
	if grep -E '^[[:space:]]+runs-on:' "$file" |
		sed 's/^[[:space:]]*runs-on:[[:space:]]*//' |
		grep -Fxv "$expected_runner" >/dev/null; then
		fail "runner other than $expected_runner in $(basename "$file")"
	fi
done

# Run IDs make every immutable Actions cache an exact miss and force a fresh
# archive upload. Artifact and concurrency names may still use run IDs.
if grep -E '^[[:space:]]+key: go-.*github\.(run_id|run_attempt)' \
	"$@" >/dev/null; then
	fail 'Go cache key contains a run ID or run attempt'
fi

# Validate every cache reader by policy rather than mirroring a fixed reader
# count or every job name. A newly added reader is covered automatically.
grep -F 'path: ~/go/pkg/mod' "$@" >/dev/null || fail 'no Go module cache reader found'
grep -F 'path: ~/.cache/go-build' "$@" >/dev/null || fail 'no Go build cache reader found'
for file in "$@"; do
	awk '
		function validate() {
			if (index(block, "actions/cache/restore@") == 0) return
			if (index(block, "path: ~/go/pkg/mod") > 0) {
				if (index(block, "key: go-mod-v2-${{ runner.os }}-${{ hashFiles(\047go.mod\047, \047go.sum\047, \047scripts/ci/go-tool-modules.txt\047) }}") == 0 ||
					index(block, "restore-keys: go-mod-v2-${{ runner.os }}-") == 0) bad = 1
			}
			if (index(block, "path: ~/.cache/go-build") > 0) {
				if (index(block, "key: go-") == 0 ||
					index(block, "-v2-") == 0 ||
					index(block, "${{ runner.os }}") == 0 ||
					index(block, "${{ runner.arch }}") == 0 ||
					index(block, "${{ steps.runner-cache-abi.outputs.value }}") == 0 ||
					index(block, "hashFiles(\047go.mod\047, \047go.sum\047") == 0 ||
					index(block, "restore-keys:") == 0) bad = 1
			}
		}
		/^[[:space:]]+- name:/ { validate(); block = "" }
		{ block = block $0 "\n" }
		END { validate(); if (bad) exit 1 }
	' "$file" || fail "incompatible Go cache reader in $(basename "$file")"
done

require_line "$tool_modules" 'github.com/rhysd/actionlint@v1.7.12'
require_line "$tool_modules" 'golang.org/x/vuln@v1.7.0'
require_line "$workflow" 'done < "$GITHUB_WORKSPACE/scripts/ci/go-tool-modules.txt"'
require_line "$workflow" 'go mod download all'
require_line "$workflow" 'run: ./scripts/ci/run-go-tool.sh actionlint'
require_line "$workflow" './scripts/ci/run-go-tool.sh govulncheck -mode=binary'
require_line "$workflow" 'run: ./scripts/ci/export-runner-cache-abi.sh'
require_line "$workflow" 'id: fuzz-cache-generation'
require_line "$workflow" 'run: echo "value=$(date -u +%G-W%V)" >>"$GITHUB_OUTPUT"'
grep -F 'key: go-fuzz-v2-' "$workflow" |
	grep -F 'steps.fuzz-cache-generation.outputs.value' >/dev/null ||
	fail 'fuzz cache does not rotate by weekly generation'
grep -F 'key: go-release-snapshot-v2-' "$workflow" |
	grep -F "hashFiles('go.mod', 'go.sum', '.goreleaser.yaml')" >/dev/null ||
	fail 'release-snapshot cache does not include GoReleaser configuration'
require_line "$repo_root/.github/workflows/race-isolation.yml" 'name: Restore race Go cache'

# Browser jobs run inside the exact Playwright image matching both lockfiles.
# No live apt mirror belongs on the critical path.
web_playwright=$(jq -r '.devDependencies["@playwright/test"]' "$repo_root/web/package.json")
docs_playwright=$(jq -r '.devDependencies["@playwright/test"]' "$repo_root/docs/site/package.json")
[ "$web_playwright" = "$docs_playwright" ] ||
	fail 'web and docs Playwright versions differ'
playwright_image="mcr.microsoft.com/playwright:v${web_playwright}-noble@sha256:dcc5531e97840b9b5e794f2814476b21571c5124a3fca2267d73041f56e7580e"
ci_docs_block=$(workflow_job_block "$workflow" docs)
ci_web_block=$(workflow_job_block "$workflow" web)
pages_docs_block=$(workflow_job_block "$repo_root/.github/workflows/docs.yml" build)
release_docs_block=$(workflow_job_block "$repo_root/.github/workflows/release.yml" docs)
for block in "$ci_docs_block" "$pages_docs_block" "$release_docs_block"; do
	printf '%s\n' "$block" | grep -F "image: $playwright_image" >/dev/null ||
		fail 'a verify-docs job does not own the exact digest-pinned Playwright image'
	printf '%s\n' "$block" | grep -F 'run: ./scripts/ci/verify-docs.sh' >/dev/null ||
		fail 'expected verify-docs invocation is outside its pinned-image job'
done
printf '%s\n' "$ci_web_block" | grep -F "image: $playwright_image" >/dev/null ||
	fail 'web browser job does not own the exact digest-pinned Playwright image'
printf '%s\n' "$release_docs_block" | grep -F 'contents: read' >/dev/null ||
	fail 'release docs validation inherited workflow write permissions'
if grep -F 'playwright install-deps' "$@" >/dev/null ||
	grep -F 'playwright install --with-deps' "$repo_root/scripts/ci/verify-docs.sh" >/dev/null; then
	fail 'live Playwright OS dependency installation remains on the CI path'
fi

# Every cache writer must be trusted-main-only and must skip an upload after an
# exact hit. This covers Go, module, and Playwright writers alike.
for file in "$@"; do
	awk \
		-v main_guard="github.event_name == 'push' && github.ref == 'refs/heads/main'" \
		-v hit_guard="outputs.cache-hit != 'true'" '
		/^[[:space:]]+- name:/ { block = "" }
		{ block = block $0 "\n" }
		/actions\/cache\/save@/ {
			if (index(block, main_guard) == 0 || index(block, hit_guard) == 0) {
				bad = 1
			}
		}
		END {
			if (bad) exit 1
		}
	' "$file" || fail "cache writer without trusted-main exact-miss guard in $(basename "$file")"
done

printf 'cache policy fixture: GitHub runners, stable Go keys, and trusted exact-miss writers verified\n'
