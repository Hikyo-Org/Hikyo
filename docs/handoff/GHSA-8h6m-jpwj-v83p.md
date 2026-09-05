# GHSA-8h6m-jpwj-v83p: browser session ownership

Private advisory fix, based on `33a213c`. Branch: `advisory-fix-1`.

## Ownership boundary

`web/src/api/sessionEpoch.ts` owns a monotonically increasing per-document
session epoch and abort signal. BroadcastChannel and storage events announce
session changes without sharing identity or credentials. The readable CSRF
companion cookie detects replacement before queued notifications arrive.

Root identity checks are bound to the cookie observed before the request.
Cookie changes during whoami invalidate the old owner and retry. Login, logout,
OIDC login, expiry, account replacement and peer notifications converge through
AuthProvider. New owners receive a new QueryClient and a keyed component tree;
old query/mutation caches and workspace bearers are cleared synchronously.

Every parsed/bodyless API operation checks ownership at launch, actual network
dispatch and completion. Old requests are aborted and late results rejected.
An operation that rotates cookies cannot release its result until whoami proves
session continuity. Disclosures are hidden during that check; verified
rotation preserves display-once component state, while replacement destroys it.
Overlapping checks cannot restore the old owner after an uncertain-cookie failure.

A local 401 prompts whoami only while authenticated. Proof failures leave a
verified live session intact; anonymous login failures remain local to the form.
Workspace 401 handling remains scoped to its remote session.

Workspace preparation and redemption capture both root and workspace ownership.
Stale preparation cannot open a popup or redeem; late redemption cannot install
or return a bearer. The client attempts bounded, credential-omitting remote
revocation of a discarded redemption. Network failure cannot undo local rejection.
Multi-stage WebAuthn and OIDC ceremonies also retain their initiating epoch.

## Expected account-security remints

Real browser validation found that account-security operations can issue a new
session ID. Preserving the active display-once callback requires more than a
same-principal comparison: only allowlisted, successful root-transport responses
can provide an expected replacement identity, parsed through generated schemas.
The returned principal and session ID must exactly match a fresh whoami, and the
principal must still match the initiating owner. Login, logout, remote responses
and unrelated same-principal session replacements cannot claim continuity.

## Local CI substitute

GitHub prevents CI integrations and status checks on temporary advisory forks.
Validation therefore runs locally against the exact commit and its embedded Go
binary, with a results comment on the private PR. This does not create or claim a
GitHub-hosted `ci-required` result.

The browser suite runs against two real local Hikyo instances and an OIDC test
provider. Desktop and mobile use isolated checkouts and ports. The dedicated
session-epoch browser test separately controls deferred responses and cross-tab
notifications. Results distinguish these browser tests from unit tests and from
hosted-only release, Linux egress, and Kubernetes jobs that were not executed.

## Validation

From `web/`, using Node 26.7.0 and frozen dependencies in `web/` and `clients/ts/`:

- `node --run typecheck`
- `node --run test` (exact-commit counts in the private PR validation comment)
- `node --run build`
- `pnpm exec playwright test --config e2e/session-epoch.config.ts`: two Chromium
  regressions, BroadcastChannel and storage fallback, real shared cookies/tabs
- `go test -count=1 -tags ui ./api/... ./internal/server/... ./internal/webui/...`
  and the same packages with `-race`
- Pinned `govulncheck -mode=binary` against the actual embedded executable
- `pnpm exec playwright test --project=desktop` and `--project=mobile`, each
  against that executable in isolated environments, including flow closure
- Actionlint, CI registry/build-artifact/required-job fixtures, DCO, signatures
- `git diff --check`

The standalone browser harness imports production AuthProvider, API transport,
query client and bearer store. API responses are mocked and deferred to control
races; this is browser ownership evidence, not backend authentication validation.
The workflow runs this browser regression in the desktop/group-1 web CI leg.

Unit regressions cover cache/disclosure teardown, late queries and mutations,
late whoami, overlapping identity checks, cookie rotation, proof versus session
401s, anonymous login refusal, OIDC notification and stale workspace revocation.
Existing protected-route fixtures now provide authenticated whoami responses.

No advisory publication, merge, production deployment or public branch update
is part of this fix.
