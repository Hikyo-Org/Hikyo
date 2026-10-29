# PR #1100: preserve the requested page after sign-in

[Pull request](https://github.com/Hikyo-Org/Hikyo/pull/1100).
Implementation commit: `758cfce77bfe1e766f177fb58fefda9419d5a297`.

## Result

Opening a protected link without a session now retains its pathname, query
string and fragment in the login continuation. Password, passkey, second-factor
and provider sign-in forward that destination, and mandatory factor enrolment
retains it until the gate lifts. Explicit CLI and workspace consent returns
retain precedence. Local sign-in refreshes the document for CSP.

The shared validator permits instance-local paths and rejects external or
malformed destinations, normalized protocol-relative paths, and login/signup
loops, including encoded route names. A direct login keeps its default landing.

## Verification

- Four router regressions failed before the fix, losing the continuation or
  landing on projects; all pass after the fix.
- Shared frontend feedback passed type checking, oxlint and all 2,097 unit tests
  across 219 files, with at most two workers.
- The production SPA build passed demo isolation and precompression checks.
- Compiled-server browser regressions passed three desktop and three mobile
  cases. Password plus MFA and a local OIDC provider both return to the exact
  machine-access URL, including query and fragment, after reloading the login
  page. These are filtered runs, not the full browser suite or production IdPs.
- Independent Standards and Spec source reviews completed. The test timing
  finding was resolved with an isolated account and TOTP ledger. Sensitive
  custody review supports the updated Login source pin; credentials, mutation
  lifetime, factor state, provider transaction binding and CSP reload persist.

The implementation remains subject to current-head hosted CI, unresolved review
resolution, protected merge-group proof, and the maintainer/adversarial security
review required by CONTRIBUTING.md. Merge authorization does not authorize
release or deployment.

## Reproduce

From the repository root with the documented pinned prerequisites and frozen
client/web dependencies installed:

```sh
VITEST_MAX_WORKERS=2 scripts/ci/fast-feedback.sh --working-tree --lane=frontend
pnpm --dir web run build
GOMAXPROCS=2 GOFLAGS=-p=2 pnpm --dir web exec playwright test --project=desktop e2e/flows/login.spec.ts --grep 'original deep link|anonymous authenticated-route'
GOMAXPROCS=2 GOFLAGS=-p=2 pnpm --dir web exec playwright test --project=mobile e2e/flows/login.spec.ts --grep 'original deep link|anonymous authenticated-route'
```

Expected: selected frontend checks pass and each browser invocation passes three
tests. The fixtures own and remove their isolated instances. Both completed runs
removed their processes, mail sinks and temporary stores. Generated SPA assets,
fixture credential files and disposable test output were removed after evidence
was preserved. Offline HTML and raw logs remain local review artifacts outside
Git.
