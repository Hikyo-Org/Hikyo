# CI path filters and CodeQL deduplication

Status: opened as [PR #537](https://github.com/Hikyo-Org/Hikyo/pull/537) from
`t3code/ci-path-filters-and-codeql-dedupe`.

## CI decision

Pull-request validation follows dependency closure rather than running every
domain for every path:

- `web/**` selects both the six-leg Playwright matrix and release-shaped app
  checks.
- Go, API, and generated-client changes skip the Playwright matrix but retain
  the `ui`-tagged Go tests, binary `govulncheck`, no-egress probe, and release
  snapshot.
- Docs, chart, release, and other existing scoped classes retain their current
  plans.
- Main pushes, unknown paths, workflow changes, and classifier/checker changes
  still select the full plan.

The split uses `web` for browser coverage and `web-go` for release-shaped app
coverage. The trusted base-branch controller and sole `ci-required` aggregate
remain unchanged.

On trusted run `33484082011`, the six browser legs consumed about 38 runner
minutes and extended the critical path by about seven minutes beyond app-build
and `web-go`. Non-web changes now avoid that browser cost.

## CodeQL decision

GitHub-managed default setup is the only active CodeQL scanner. It remains
configured for Actions, Go, and JavaScript/TypeScript with the default query
suite and weekly schedule.

Repository advanced-setup workflow `.github/workflows/codeql-analysis.yml` was
deleted. Legacy workflow registrations `343243580` (`codeql.yml`) and
`343264535` (`Security Scan`) were disabled manually. The latter had emitted
startup failures with no jobs because managed default setup already owned the
scan.

`scripts/release/configure-repository.sh` now enables and asserts default setup.
`scripts/ci/check-codeql-default-setup_test.sh` rejects future checked-in CodeQL
actions or configuration that disables managed setup.

## Verification

- changed-path classifier fixture: passed
- required-jobs plan/result fixture: passed
- CI registry/workflow bijection: 7 tests passed
- CodeQL default-setup fixture: passed
- actionlint: passed
- ShellCheck on changed shell files: passed
- trusted-controller, artifact-reuse, cache-policy, and nightly-release
  fixtures: passed
- live GitHub state: managed `CodeQL` active; both legacy workflows disabled

## Exact-head CI follow-up

Trusted run `33486637847` passed every non-browser job and five of six browser
legs at `07f94e02`. Desktop group 1 exposed an existing OIDC reauthentication
race: its post-login `/api/v1/auth/methods` refetch received `429` with
`Retry-After: 5`, and `retry: false` left the ceremony permanently
passkey-only. The captured response had already returned the configured
provider before login, and `whoami` confirmed the OIDC session, ruling out
fixture setup and identity propagation.

The public auth-method discovery query now retries only a `429`, once, after
the server's bounded numeric `Retry-After`; authorization refusals and all
global query defaults remain non-retrying. The regression test forces the
first discovery read to return `429`, and the original desktop OIDC flow passed
three consecutive focused runs after the fix.
