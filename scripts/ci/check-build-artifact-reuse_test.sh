#!/bin/sh
# GitHub expressions below are literal fixture text.
# shellcheck disable=SC2016
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$script_dir/../.." && pwd)
workflow="$repo_root/.github/workflows/ci.yml"
standalone_no_egress="$repo_root/.github/workflows/no-egress.yml"
fixture="$repo_root/web/e2e/fixtures/instance.ts"
mail_fixture="$repo_root/web/e2e/fixtures/mail.ts"
spa_builder="$repo_root/scripts/ci/build-spa.sh"

fail() {
	printf 'build artifact reuse fixture failed: %s\n' "$1" >&2
	exit 1
}

require_line() {
	file=$1
	expected=$2
	grep -F -- "$expected" "$file" >/dev/null ||
		fail "missing $expected in $(basename "$file")"
}

workflow_job_block() {
	job=$1
	awk -v header="  $job:" '
		$0 == header { found = 1 }
		found && $0 != header && $0 ~ /^  [A-Za-z0-9_-]+:/ { exit }
		# Two-space comments between jobs belong to neither job.
		found && ($0 == header || $0 ~ /^    /) { print }
	' "$workflow"
}

artifact_input_lines() {
	awk '
		/^        with:$/ { inputs = 1; next }
		/^        [^ ]/ { inputs = 0 }
		inputs { print }
	'
}

app_artifact_step() {
	job=$1
	expected_step=$2
	expected_action=$3
	# Bound checks to the actual app transfer step. Other uploads may correctly
	# retain attempt-specific reports. Include every hikyo-app transfer too, so
	# a second consumer with a different name cannot hide behind a correct one.
	# Downloads without an explicit separate name can select the app through a
	# pattern or the action default (all artifacts), so count those as well.
	artifact_step=$(awk -v expected_step="$expected_step" '
		function flush() {
			if (index(step, "      - name: " expected_step "\n") == 1 || app ||
				(download && !named)) { printf "%s", step }
		}
		/^      - / { flush(); step = ""; app = 0; inputs = 0; download = 0; named = 0 }
		/^        [^ ]/ { inputs = 0 }
		/^        with:$/ { inputs = 1 }
		/^      - uses: actions\/download-artifact@|^        uses: actions\/download-artifact@/ { download = 1 }
		inputs && /^          name:[[:space:]]*[^[:space:]#]/ { named = 1 }
		inputs && /^          name:[[:space:]]*(""|\047\047|null|Null|NULL|~)[[:space:]]*(#.*)?$/ { named = 0 }
		inputs && /^          name:[[:space:]]+["\047]?hikyo-app-/ { app = 1 }
		{ if (step != "" || $0 ~ /^      - /) { step = step $0 "\n" } }
		END { flush() }
	')
	[ "$(printf '%s\n' "$artifact_step" | awk '/^      - / { count++ } END { print count+0 }')" -eq 1 ] ||
		fail "$job must contain exactly one app artifact step"
	[ "$(printf '%s\n' "$artifact_step" | artifact_input_lines | sed -n '/^          name:/p')" = '          name: hikyo-app-${{ github.run_id }}' ] ||
		fail "$job app artifact name is not stable across partial reruns"
	[ "$(printf '%s\n' "$artifact_step" | sed -n '/^        uses:/p')" = "        uses: $expected_action" ] ||
		fail "$job app artifact does not use the repository-pinned transfer action"
	printf '%s\n' "$artifact_step"
}

upload_action='actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # v4.6.2'
download_action='actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1'
app_block=$(workflow_job_block app-build)
# The no-egress probe rides in web-go, which already downloads the app build.
no_egress_block=$(workflow_job_block web-go)
release_block=$(workflow_job_block release-snapshot)
web_block=$(workflow_job_block web)
source_block=$(workflow_job_block source-artifact-security)

[ -n "$app_block" ] || fail 'app-build job is missing'
printf '%s\n' "$app_block" | grep -F 'run: ./scripts/ci/build-spa.sh --verify' >/dev/null ||
	fail 'app-build does not verify and build the SPA through the shared script'
printf '%s\n' "$app_block" | grep -F 'go build -tags ui -o ci-artifacts/hikyo-ui ./cmd/hikyo' >/dev/null ||
	fail 'app-build does not produce the release-shaped CI binary'
printf '%s\n' "$app_block" | grep -F 'go build -o ci-artifacts/oidctest-idp ./internal/oidctest/cmd' >/dev/null ||
	fail 'app-build does not produce the browser IdP helper'
printf '%s\n' "$app_block" | grep -F 'go build -o ci-artifacts/mail-sink ./internal/mailtest/cmd' >/dev/null ||
	fail 'app-build does not produce the browser mail helper'
app_upload=$(printf '%s\n' "$app_block" | app_artifact_step app-build 'Upload app build for downstream validation' "$upload_action")
app_upload_inputs=$(printf '%s\n' "$app_upload" | artifact_input_lines)
printf '%s\n' "$app_upload_inputs" | grep -F 'ci-artifacts/oidctest-idp' >/dev/null ||
	fail 'app-build does not upload the browser IdP helper'
printf '%s\n' "$app_upload_inputs" | grep -F 'ci-artifacts/mail-sink' >/dev/null ||
	fail 'app-build does not upload the browser mail helper'
printf '%s\n' "$app_upload_inputs" | grep -Fx '          overwrite: true' >/dev/null ||
	fail 'app-build artifact cannot be replaced by a full workflow rerun'

printf '%s\n' "$no_egress_block" | grep -F 'run: ./scripts/ci/no-egress.sh' >/dev/null ||
	fail 'no-egress probe is missing from web-go'
printf '%s\n' "$no_egress_block" | grep -F 'needs: [changes, app-build]' >/dev/null ||
	fail 'no-egress does not depend on app-build'
printf '%s\n' "$no_egress_block" | app_artifact_step web-go 'Download the exact app build' "$download_action" >/dev/null
printf '%s\n' "$no_egress_block" | grep -F 'run: chmod +x ci-artifacts/hikyo-ui' >/dev/null ||
	fail 'no-egress does not restore execute permission after artifact download'
printf '%s\n' "$no_egress_block" | grep -F 'HIKYO_NO_EGRESS_BIN: ${{ github.workspace }}/ci-artifacts/hikyo-ui' >/dev/null ||
	fail 'no-egress does not pass the downloaded binary to the probe'
if printf '%s\n' "$no_egress_block" | grep -F 'go build' >/dev/null; then
	fail 'no-egress repeats app-build compilation'
fi
[ ! -e "$standalone_no_egress" ] || fail 'standalone no-egress workflow still duplicates app-build'

printf '%s\n' "$web_block" | grep -F 'needs: [changes, app-build]' >/dev/null ||
	fail 'web does not depend on app-build'
printf '%s\n' "$web_block" | app_artifact_step web 'Download the exact app build' "$download_action" >/dev/null
printf '%s\n' "$web_block" | grep -F 'run: chmod +x ci-artifacts/hikyo-ui' >/dev/null ||
	fail 'web does not restore execute permission after artifact download'
printf '%s\n' "$web_block" | grep -F 'chmod +x ci-artifacts/hikyo-ui ci-artifacts/oidctest-idp ci-artifacts/mail-sink' >/dev/null ||
	fail 'web does not restore every artifact executable mode'
printf '%s\n' "$web_block" | grep -F 'HIKYO_E2E_OIDC_BINARY: ${{ github.workspace }}/ci-artifacts/oidctest-idp' >/dev/null ||
	fail 'web does not pass the prebuilt IdP helper to the browser harness'
printf '%s\n' "$web_block" | grep -F 'HIKYO_E2E_MAIL_BINARY: ${{ github.workspace }}/ci-artifacts/mail-sink' >/dev/null ||
	fail 'web does not pass the prebuilt mail helper to the browser harness'
if printf '%s\n' "$web_block" | grep -E 'actions/setup-go|go build|\.cache/go-build|~/go/pkg/mod' >/dev/null; then
	fail 'a browser shard still provisions Go or compiles a helper'
fi
printf '%s\n' "$web_block" | grep -F 'group: [1, 2, 3, 4]' >/dev/null ||
	fail 'browser matrix is not split across four groups'
printf '%s\n' "$web_block" | grep -F 'specs: e2e/flows/matrix.spec.ts' >/dev/null ||
	fail 'heavy matrix flow does not have its own browser group'

if printf '%s\n' "$web_block" |
	grep -E 'pnpm run (typecheck|test|build)|pnpm build' >/dev/null; then
	fail 'a browser shard repeats app-build frontend work'
fi
# The source campaign is another consumer of the same immutable app build.
printf '%s\n' "$source_block" | app_artifact_step source-artifact-security 'Download the exact app build' "$download_action" >/dev/null
printf '%s\n' "$release_block" | grep -F 'needs: changes' >/dev/null ||
	fail 'release snapshot is serialized behind app-build'
printf '%s\n' "$release_block" | grep -F 'run: ./scripts/ci/build-spa.sh' >/dev/null ||
	fail 'parallel release snapshot does not build its embedded SPA through the shared script'

require_line "$spa_builder" 'cd "$repo_root/web"'
require_line "$spa_builder" 'pnpm --dir ../clients/ts install --frozen-lockfile'
require_line "$spa_builder" 'pnpm install --frozen-lockfile'
require_line "$spa_builder" 'pnpm run typecheck'
require_line "$spa_builder" 'pnpm run test'
require_line "$spa_builder" 'pnpm run build'

require_line "$workflow" 'name: Upload unsigned development snapshot'
upload_block=$(sed -n \
	'/name: Upload unsigned development snapshot/,/name: Save release-snapshot Go cache/p' \
	"$workflow")
printf '%s\n' "$upload_block" | grep -F "if: (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'" >/dev/null ||
	fail 'development snapshot upload is not guarded on trusted main'
for expected in \
	'name: hikyo-development-${{ github.sha }}' \
	'dist/hikyo_*_Darwin_*.tar.gz' \
	'dist/hikyo_*_Linux_*.tar.gz' \
	'dist/hikyo_*_Windows_*.zip' \
	'dist/checksums.txt' \
	'dist/DEVELOPMENT-SNAPSHOT.txt' \
	'retention-days: 14'
do
	printf '%s\n' "$upload_block" | grep -F "$expected" >/dev/null ||
		fail "development snapshot upload is missing $expected"
done
validate_line=$(grep -nF 'name: Classify complete snapshot manifest' "$workflow" | cut -d: -f1)
upload_line=$(grep -nF 'name: Upload unsigned development snapshot' "$workflow" | cut -d: -f1)
[ -n "$validate_line" ] && [ "$validate_line" -lt "$upload_line" ] ||
	fail 'development snapshot is uploaded before non-mutating manifest validation'
require_line "$fixture" "process.env.HIKYO_E2E_BINARY"
require_line "$fixture" "process.env.HIKYO_E2E_OIDC_BINARY"
require_line "$mail_fixture" "process.env['HIKYO_E2E_MAIL_BINARY']"

if grep -Eq 'gh release|action-gh-release' "$workflow"; then
	fail 'ordinary CI publishes a GitHub Release and bypasses the signing ceremony'
fi

"$repo_root/scripts/ci/check-no-egress-lifecycle_test.sh"

printf 'build artifact reuse fixture: one app build feeds browser shards and no-egress while release downloads stay parallel\n'
