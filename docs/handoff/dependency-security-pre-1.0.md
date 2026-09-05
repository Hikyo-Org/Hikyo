# Pre-1.0 docs dependency security handoff

Base: `f4175a5d`. Branch: `fix/one-zero-dependency-security`. No commit or push; parent owns review, signed delivery and merge on green.

## Change

`docs/site/pnpm-lock.yaml` moves the sole `fast-uri` resolution from 3.1.5 to 3.1.7 through existing Ajv 8.20.0. No manifest, override, direct dependency, major version, or unrelated package version changes.

Live GitHub alerts #9-12 name GHSA-5jgf-p345-68v8, GHSA-f65p-4m7j-42xc, GHSA-fph4-wmhf-6fwf and GHSA-jqff-g426-hqxp, first patched in 3.1.6. Upstream [3.1.7](https://github.com/fastify/fast-uri/releases/tag/v3.1.7) additionally fixes GHSA-qw65-cvwx-89v3 and GHSA-58mr-gqgx-xq4g. Staying on the compatible v3 release line is recommended.

The package manager generated the update with `pnpm update fast-uri --depth 100 --lockfile-only`. It also refreshed unrelated optional-peer metadata. Only its three unmodified fast-uri hunks were retained: package version/integrity, Ajv dependency edge, and snapshot. No integrity hash was authored or modified by hand. Frozen installation subsequently passed against that exact minimal lockfile.

## Evidence

- Pinned Node 26.7.0, pnpm 11.24.0.
- Frozen install passes and peer check reports no issues.
- `pnpm why fast-uri` finds exactly one version, 3.1.7; routes are Workbox build and Astro checker development dependencies through Ajv.
- `pnpm audit --json` reports zero vulnerabilities across 955 total dependencies at all severities. JSON: `/tmp/hikyo-dependency-audit.json`.
- Canonical `scripts/ci/verify-docs.sh` passed with `POSTHOG_REQUIRED=true` and repository public analytics variables; 39-page static build, 98-file precache, required analytics, browser offline navigation and all policy/fallback fixtures passed; details in the [HTML report](../reports/1.0/dependency-security.html). Log: `/tmp/hikyo-dependency-docs.log`.

This fixes a known vulnerable build dependency. The dependency graph is not proof of an exploitable Go-server request path. GitHub alert closure remains a post-merge check.

## Next parent action

Review the four-line lockfile diff and validation report. Include these files in the signed 1.0 readiness PR, merge only after exact-head CI passes, then query Dependabot to confirm alerts #9-12 closed on the default branch.
