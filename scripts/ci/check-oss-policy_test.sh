#!/bin/sh
set -eu

CDPATH=
repo_root=$(cd -- "$(dirname "$0")/../.." && pwd)
fixture_dir=$(mktemp -d "${TMPDIR:-/tmp}/hikyo-oss-policy.XXXXXX")
trap 'rm -rf "$fixture_dir"' EXIT HUP INT TERM

"$repo_root/scripts/ci/check-oss-policy.sh" "$repo_root" "$repo_root/docs/site/dist"

mkdir -p "$fixture_dir/site/.well-known"
mkdir -p "$fixture_dir/release/repository"
mkdir -p "$fixture_dir/.github/ISSUE_TEMPLATE" "$fixture_dir/.github/workflows"
mkdir -p "$fixture_dir/docs/adr" "$fixture_dir/docs/site/src/content/docs/docs"
for path in README.md GOVERNANCE.md TRADEMARK.md CONTRIBUTING.md LICENSE; do
	cp "$repo_root/$path" "$fixture_dir/$path"
done
cp "$repo_root/release/repository/main-ci-gate.json" \
	"$fixture_dir/release/repository/main-ci-gate.json"
cp "$repo_root/release/repository/fallback-channel-test.json" \
	"$fixture_dir/release/repository/fallback-channel-test.json"
cp "$repo_root/.github/ISSUE_TEMPLATE/config.yml" \
	"$fixture_dir/.github/ISSUE_TEMPLATE/config.yml"
cp "$repo_root/.github/workflows/reliability.yml" \
	"$fixture_dir/.github/workflows/reliability.yml"
cp "$repo_root/docs/adr/oss-mechanics.md" "$fixture_dir/docs/adr/oss-mechanics.md"
cp "$repo_root/docs/adr/system-architecture.md" "$fixture_dir/docs/adr/system-architecture.md"
cp "$repo_root/docs/site/src/content/docs/docs/installation.mdx" \
	"$fixture_dir/docs/site/src/content/docs/docs/installation.mdx"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: missing SECURITY.md was accepted\n' >&2
	exit 1
fi

cp "$repo_root/SECURITY.md" "$fixture_dir/SECURITY.md"
mkdir -p "$fixture_dir/docs/site/public/.well-known"
cp "$repo_root/docs/site/public/.well-known/security.txt" \
	"$fixture_dir/docs/site/public/.well-known/security.txt"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: missing SUPPORT.md was accepted\n' >&2
	exit 1
fi

cp "$repo_root/SUPPORT.md" "$fixture_dir/SUPPORT.md"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: unserved O4-O6 policy pages were accepted\n' >&2
	exit 1
fi

cp -R "$repo_root/docs/site/dist/." "$fixture_dir/site/"
for path in favicon-hikyo-48.png apple-touch-icon.png; do
	cp "$repo_root/docs/site/public/$path" "$fixture_dir/docs/site/public/$path"
done
cp "$repo_root/docs/site/src/content/docs/docs/meta.json" \
	"$fixture_dir/docs/site/src/content/docs/docs/meta.json"
# Prove the complete synthetic fixture passes before interpreting refusals.
"$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site"

for page in docs security support governance implementation-status trademark contributing license; do
	sed "s#https://hikyo.app/$page/#https://example.invalid/$page/#g" \
		"$repo_root/docs/site/dist/$page/index.html" >"$fixture_dir/site/$page/index.html"
	if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >"$fixture_dir/canonical-refusal.log" 2>&1; then
		printf 'OSS policy fixture failed: noncanonical %s page was accepted\n' "$page" >&2
		exit 1
	fi
	grep -F '<link rel="canonical"' "$fixture_dir/canonical-refusal.log" >/dev/null
	cp "$repo_root/docs/site/dist/$page/index.html" "$fixture_dir/site/$page/index.html"
done

# Execute the real parsed policy check against complete YAML mutations. Tokens
# in environment values, comments or another step cannot grant job authority or
# replace the unique executable failure notification.
for mutation in env-permission wrong-permission inherited-permission scalar-permissions typed-permission decoy-notification step-env comment-body quoted-body false-condition missing-alert duplicate-alert duplicate-permission malformed-yaml; do
	cp "$repo_root/.github/workflows/reliability.yml" \
		"$fixture_dir/.github/workflows/reliability.yml"
	"${NODE_BIN:-node}" --input-type=module - "$fixture_dir/.github/workflows/reliability.yml" "$mutation" "$repo_root/docs/site/package.json" <<'JS'
import fs from "node:fs";
import { createRequire } from "node:module";
const require = createRequire(process.argv[4]);
const { parse, stringify } = require("yaml");
const path = process.argv[2];
const workflow = parse(fs.readFileSync(path, "utf8"));
const channel = workflow.jobs["fallback-channel"];
const alert = channel.steps.find((step) => step.name === "Surface fallback-channel degradation");
switch (process.argv[3]) {
  case "env-permission":
    delete channel.permissions.issues;
    channel.env = { DECOY: "issues: write" };
    break;
  case "wrong-permission": channel.permissions.issues = "read"; break;
  case "inherited-permission": delete channel.permissions; workflow.permissions.issues = "write"; break;
  case "scalar-permissions": channel.permissions = "write-all"; break;
  case "typed-permission": channel.permissions.issues = true; break;
  case "decoy-notification":
    channel.steps.push({ name: "Unrelated decoy", if: "failure()", run: alert.run });
    alert.if = "false";
    alert.run = "echo notification removed";
    break;
  case "step-env":
    alert.env.DECOY = "Fallback security channel health check failed";
    alert.run = "echo notification removed";
    break;
  case "comment-body": alert.run = alert.run.split("\n").map((line) => "# " + line).join("\n"); break;
  case "quoted-body": alert.run = "printf '%s\n' '" + alert.run.replaceAll("'", "'\\''") + "'"; break;
  case "false-condition": alert.if = "false && failure()"; break;
  case "missing-alert": channel.steps = channel.steps.filter((step) => step !== alert); break;
  case "duplicate-alert": channel.steps.push({ ...alert }); break;
  case "duplicate-permission":
    fs.writeFileSync(path, stringify(workflow).replace("      issues: write", "      issues: read\n      issues: write"));
    process.exit(0);
  case "malformed-yaml": fs.writeFileSync(path, "jobs: [unterminated"); process.exit(0);
  default: throw new Error("unknown workflow policy mutation");
}
fs.writeFileSync(path, stringify(workflow));
JS
	if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >"$fixture_dir/workflow-refusal.log" 2>&1; then
		printf 'OSS policy fixture failed: %s was accepted\n' "$mutation" >&2
		exit 1
	fi
	grep -F 'OSS policy gate: fallback-channel workflow:' "$fixture_dir/workflow-refusal.log" >/dev/null || {
		cat "$fixture_dir/workflow-refusal.log" >&2
		printf 'OSS policy fixture failed: %s failed outside the workflow policy assertion\n' "$mutation" >&2
		exit 1
	}
done
cp "$repo_root/.github/workflows/reliability.yml" \
	"$fixture_dir/.github/workflows/reliability.yml"

for mutation in old-context feedback-context extra-context loose-status disabled signatures unresolved-threads; do
	cp "$repo_root/release/repository/main-ci-gate.json" \
		"$fixture_dir/release/repository/main-ci-gate.json"
	"${NODE_BIN:-node}" -e '
const fs = require("node:fs");
const path = process.argv[1];
const gate = JSON.parse(fs.readFileSync(path, "utf8"));
const status = gate.rules.find((rule) => rule.type === "required_status_checks").parameters;
switch (process.argv[2]) {
  case "old-context": status.required_status_checks[0].context = "ci-required"; break;
  case "feedback-context": status.required_status_checks[0].context = "feedback"; break;
  case "extra-context": status.required_status_checks.push({context:"ci-required"}); break;
  case "loose-status": status.strict_required_status_checks_policy = true; break;
  case "disabled": gate.enforcement = "disabled"; break;
  case "signatures": gate.rules = gate.rules.filter((rule) => rule.type !== "required_signatures"); break;
  case "unresolved-threads": gate.rules.find((rule) => rule.type === "pull_request").parameters.required_review_thread_resolution = false; break;
  default: throw new Error("unknown policy mutation");
}
fs.writeFileSync(path, JSON.stringify(gate));
' "$fixture_dir/release/repository/main-ci-gate.json" "$mutation"
	if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >"$fixture_dir/policy-refusal.log" 2>&1; then
		printf 'OSS policy fixture failed: %s was accepted\n' "$mutation" >&2
		exit 1
	fi
	grep -F 'OSS policy gate:' "$fixture_dir/policy-refusal.log" >/dev/null || {
		cat "$fixture_dir/policy-refusal.log" >&2
		printf 'OSS policy fixture failed: %s failed outside the policy assertion\n' "$mutation" >&2
		exit 1
	}
done
cp "$repo_root/release/repository/main-ci-gate.json" \
	"$fixture_dir/release/repository/main-ci-gate.json"
sed 's/^Expires:.*/Expires: 2000-08-09T00:00:00Z/' \
	"$fixture_dir/docs/site/public/.well-known/security.txt" \
	>"$fixture_dir/security-expired.txt"
mv "$fixture_dir/security-expired.txt" \
	"$fixture_dir/docs/site/public/.well-known/security.txt"
cp "$fixture_dir/docs/site/public/.well-known/security.txt" \
	"$fixture_dir/site/.well-known/security.txt"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: elapsed source security.txt expiry was accepted\n' >&2
	exit 1
fi

cp "$repo_root/docs/site/public/.well-known/security.txt" \
	"$fixture_dir/docs/site/public/.well-known/security.txt"
cp "$fixture_dir/docs/site/public/.well-known/security.txt" \
	"$fixture_dir/site/.well-known/security.txt"
sed 's#Hikyo-Org/Hikyo/security/advisories/new#wrong/repository/security/advisories/new#' \
	"$fixture_dir/.github/ISSUE_TEMPLATE/config.yml" \
	>"$fixture_dir/config-wrong.yml"
mv "$fixture_dir/config-wrong.yml" "$fixture_dir/.github/ISSUE_TEMPLATE/config.yml"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: misrouted issue-chooser PVR link was accepted\n' >&2
	exit 1
fi

cp "$repo_root/.github/ISSUE_TEMPLATE/config.yml" \
	"$fixture_dir/.github/ISSUE_TEMPLATE/config.yml"
sed 's/ci-queue-required/removed-required-context/' \
	"$fixture_dir/release/repository/main-ci-gate.json" \
	>"$fixture_dir/main-ci-gate-missing-aggregate.json"
mv "$fixture_dir/main-ci-gate-missing-aggregate.json" \
	"$fixture_dir/release/repository/main-ci-gate.json"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: missing ci-queue-required context was accepted\n' >&2
	exit 1
fi

cp "$repo_root/release/repository/main-ci-gate.json" \
	"$fixture_dir/release/repository/main-ci-gate.json"
sed '/issues: write/d' "$fixture_dir/.github/workflows/reliability.yml" \
	>"$fixture_dir/security-channel-unprivileged.yml"
mv "$fixture_dir/security-channel-unprivileged.yml" \
	"$fixture_dir/.github/workflows/reliability.yml"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: silent fallback-channel failure was accepted\n' >&2
	exit 1
fi

cp "$repo_root/.github/workflows/reliability.yml" \
	"$fixture_dir/.github/workflows/reliability.yml"
awk '
	/issues: write/ { next }
	{ print }
	$0 == "  go-patch:" {
		print "    permissions:"
		print "      contents: read"
		print "      issues: write"
	}
' "$fixture_dir/.github/workflows/reliability.yml" >"$fixture_dir/channel-writer-moved.yml"
mv "$fixture_dir/channel-writer-moved.yml" "$fixture_dir/.github/workflows/reliability.yml"
if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: unrelated issue writer substituted for fallback channel\n' >&2
	exit 1
fi
cp "$repo_root/.github/workflows/reliability.yml" \
	"$fixture_dir/.github/workflows/reliability.yml"
sed "s/Homebrew does \*\*not\*\* verify Hikyo's pinned signing root/Homebrew verifies downloads/" \
	"$fixture_dir/docs/site/src/content/docs/docs/installation.mdx" \
	>"$fixture_dir/installation-trust-erased.mdx"
mv "$fixture_dir/installation-trust-erased.mdx" \
	"$fixture_dir/docs/site/src/content/docs/docs/installation.mdx"

if "$repo_root/scripts/ci/check-oss-policy.sh" "$fixture_dir" "$fixture_dir/site" >/dev/null 2>&1; then
	printf 'OSS policy fixture failed: erased Homebrew trust boundary was accepted\n' >&2
	exit 1
fi

printf 'OSS policy fixture: governance, disclosure, and package-manager trust boundaries passed\n'
