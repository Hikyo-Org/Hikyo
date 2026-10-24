# CLI sign-in with enrolled MFA

The owner approved removing the terminal password-login exception on
2026-10-24. This amends the original artifact-scope decision in
[#760's handoff](./760-second-factor-at-signin.md), as recorded in the
[human authentication ADR](../adr/human-auth.md#second-factor-policy).
After reviewing the local result, the owner authorized signed commit, push,
pull-request creation, review and CI repairs, and merge through completion.
Release and deployment remain outside scope. The evidence below records local
validation; it does not establish hosted CI or merge outcomes.

## Behavior

`hikyo login <instance-url>` already uses browser sign-in and explicit consent
to issue a separate CLI session. `--device` supports approval from another
device. These transports reuse the existing password, authenticator, passkey,
and provider ceremonies.

An account with an enrolled local factor cannot obtain a password-only CLI
session through `--local`. After verifying its password, the server refuses
that request with the existing 400 response and browser/device guidance.
Wrong credentials retain their uniform 401 response. Unenrolled accounts keep
the existing optional-policy behavior or required-policy enrollment gate.

CLI approval and redemption check live local-factor enrollment under the
principal lock. A local-password browser session with inadequate assurance
cannot approve a valid request. Redemption checks both the current browser
session and the approved assurance snapshot before consuming the handoff.
Disabled passkeys and credentials from superseded epochs do not count as
standing factors. Provider-specific assurance rules remain unchanged.

If an existing password-only browser session needs MFA, consent shows the
specific sign-in recovery message. The existing logout and sign-in flows
preserve the pending CLI reference and provider hint. After completing MFA,
the user must explicitly approve again. Failed logout retains the session
and a retry control. Invalid, expired, spent, or already-approved requests
retain their generic 403 response.

Login assurance crosses the handoff; operation-specific proof windows do not.
Sensitive operations still require their independent fresh proofs. There are
no new authentication ceremonies, artifacts, endpoints, or database migrations.

## Evidence and boundaries

Regression tests reproduced the previous password-only CLI mint on SQLite
and PostgreSQL. UI regressions reproduced the prior recovery loop before the
recovery change.

The final real HTTP batch passed on both engines: 115 test results, no
failures or skips, including logout, password challenge, TOTP completion,
approval of the same pending request, separate CLI mint, provider provenance,
invalid-reference refusal, and absence of operation proof windows. Related
access, audit, developer/disclosure, and scanning consumers are included.

The focused frontend batch passed type checking, lint, and 119 tests covering
CLI consent, provider login, and session transitions. CLI Login/Help checks
and scoped Go vet passed. Independent source review found and resolved the
browser recovery gap; the follow-up review found no actionable findings.

The final complete CLI, service, server, authorization, and API package checks
passed. All 15 repository-invariant roots passed (64 test results), followed
by successful working-tree Go feedback. The earlier full lint-package timeout
below remains a separate result.

Desktop browser setup exposed an enrolled seed administrator still using
terminal password login after grant invalidation. Its canonical fixture
helper and enrolled consumers now use the real browser MFA ceremony and
PKCE-bound CLI handoff with their existing spent-code ledger. A second setup
attempt exposed the required-enrollment gate on the initial unenrolled
administrator. The helper preserves that original restricted CLI bootstrap
path without invoking handoff approval. Neither setup attempt executed tests.

The final fixture protocol/lifecycle selection passes 13 tests, including
restricted bootstrap, artifact/token validation, replay recovery, binding
refusal, partial allocation, and combined protocol/logout/disposal failures.
The final fixture-owner batch after the bootstrap repair passed TypeScript,
full web lint, and all 50 tests across nine fixture/security files.
Independent R5 source review found no actionable findings in the combined
changes.

Actual browser acceptance passed all 10 CLI handoff scenarios on desktop
(3.7 minutes) and all 10 on mobile (3.5 minutes), with one worker and separate
fixture setup/teardown for each viewport. The new recovery regression passed
on both: real refusal, logout, preserved query/provider, password plus TOTP,
the same pending transaction, explicit renewed consent, distinct CLI
redemption, and retained browser session. The campaign also covers device
consent in both themes, clock/reuse refusal, cancellation, and password,
virtual-passkey, and signed-fixture OIDC loopback sign-in. All six recovery
screenshots were inspected for readable text and visible controls. Filtered
registry closure was skipped under the existing filtered-run contract.

Both campaigns used embedded-UI binary SHA256
`253bb48f114afb2d3630f982e00386fa64fe6a16cf3d00fcddfcaa841b6d9ee6`.
Browser test source SHA256 was
`f104371cc33d6ba8839e65009ca6f6ca4c815c93ffea5075cf073cfa9037e453`.
The existing [browser transport handoff](./612-cli-login-transports.md#browser-evidence-and-reproduction)
documents the fixture-owned reproduction workflow. Use the current repository
toolchain pins and include the new recovery case in the CLI handoff selection.
All five fixture ports closed after teardown, with no owned app or OIDC
processes left running.

The initial local validation candidate starts at
`885f460e6425d8c81edb07e411a814386c33dc6f`. A sorted manifest of the 24
changed non-documentation files, encoded as SHA256, two spaces,
repository-relative path, and newline for each file, has SHA256
`e41b748892d5dc208e1385eac607123c715304b3f5098f97a1fd909b895a13b9`.
The disposable PostgreSQL fixture was removed after validation.

Before PR publication, the signed change was rebased onto current main
`da68e6767e8f5f3d884af5ef06e252ca5260a0f0` without conflicts. Complete
frontend feedback exposed the required sensitivity-inventory hash refresh
for `routes/CLILogin.tsx`. Only that source's pin was updated after its R5
sensitivity review; the inventory test and mutation boundary were preserved.

The earlier complete lint-package invocation hit its unchanged 600-second
cumulative timeout. Its then-current disposable-database test had run for
12 seconds. This is not a full lint pass, and no timeout was increased.
Docker-backed PostgreSQL fixtures also failed with confirmed disk exhaustion;
the final both-engine HTTP batch used a disposable, bounded tmpfs fixture
with durability enabled. No unrelated containers or volumes were pruned.

The configured Jev advisory was refreshed after the fixture changes, against
a local candidate while checks were pending. Description match was 0.73 and
untested-logic probability 0.28;
neither crossed its candidate threshold. Auth/tenant risk probability was
0.99. Merge readiness was 1.21 on the 0-to-3 scale, confidence 0.65, with
blocked/not-ready/nearly-ready/ready probabilities 0.07/0.65/0.28/0.00.
At probe time there was no PR or remote CI. The advisory is not a gate or
merge authority.
Cross-provider review was skipped because the Anthropic provider is disabled.

Temporary fixture accounts, local checks, and source review do not establish
real external-provider, hardware-authenticator, hosted-CI, merge, or production
acceptance. Evidence logs and the offline HTML companion are local chat
artifacts, not repository dependencies.
