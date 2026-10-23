# PR #1028 dependency compatibility

PR: <https://github.com/Hikyo-Org/Hikyo/pull/1028>

## Failure and repair

Merge-group run `39905629751` failed in `validation / docs` at the
`pnpm peers check` step. Astro 7.3.3 requires `@astrojs/markdown-remark ^7.3.0`,
but the direct dependency remained pinned at 7.2.4. The same peer check
reproduced the failure locally on the original PR head.

Pin the direct dependency to 7.3.0 and regenerate the docs lockfile with
pnpm 11.24.0. Preserve the existing Astro 7.3.3 and ip-address 10.7.4 updates.
The older markdown-remark entry remains necessary for MDX's own dependency.
Keep peer validation enabled.

## Local verification

Using Node 26.7.0 and pnpm 11.24.0, these passed after the repair:

- Frozen installs and peer checks in `docs/site` and `scripts/mcp-conformance`.
- `pnpm --dir docs/site run verify`: Astro typecheck (zero errors), content and
  asset tests, site build, OSS policy, PWA, analytics, CSP, LLM index, and offline
  browser checks. Local analytics verification used the optional analytics path;
  CI exercises the required configuration.
- `scripts/ci/fast-feedback.sh --working-tree`, selected from the full PR diff.
- `git diff --check`.

## Completion boundary

Local checks do not establish merge-group acceptance. Require fresh checks on
the repaired head and successful protected merge queue validation before merge.
Release and deployment are outside this repair's scope. If Dependabot closes
the PR, reopen it and explain that the human compatibility repair is retained.
