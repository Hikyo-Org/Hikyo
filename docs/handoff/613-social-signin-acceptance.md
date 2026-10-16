# Issue 613: social sign-in acceptance

Entry point: [issue 613](https://github.com/Hikyo-Org/Hikyo/issues/613).
The [social sign-in specification](../spec/social-signin.md) indexes the locked
[owner resolution 589](https://github.com/Hikyo-Org/Hikyo/issues/589) and its
owning decisions. Criterion A7 in [the MVP boundary](../adr/mvp-boundary.md)
governs acceptance. This work closes evidence and integration gaps across
issues 606 through 612; it does not amend their decisions.

The paired [self-contained HTML handoff](613-social-signin-acceptance.html)
contains the same report. Implementation evidence below is distinct from final
acceptance, hosted CI, publication and deployment. Until the remaining checks
are recorded, this handoff is not a declaration that A7 has passed.

## Requirement and evidence map

| Requirement | Executable or operator evidence | Acceptance status |
| --- | --- | --- |
| Staged login and sign-in/signup doors | `web/e2e/flows/login.spec.ts`, social-signin prototype iteration 2 | Focused desktop/mobile signup doors passed; remaining family covered by existing flows, full hosted execution pending |
| Signup request, mail sink, verification and fresh-org naming | `web/e2e/flows/login.spec.ts`, `internal/isolation/local_signup_e2e_test.go` | Real desktop/mobile SMTP flow and focused both-engine lifecycle passed; full standard isolation selection passed with recorded optional skips |
| Members policy editor | `web/e2e/flows/members.spec.ts` | Existing editor flows retained; full hosted execution pending |
| Account-security establishment and password set | `web/e2e/flows/account.spec.ts` | Focused desktop/mobile establishment and password flow passed in both schemes |
| OAuth2 provider CRUD, org origin filter and org rename | `web/e2e/flows/instance-admin.spec.ts`, `web/e2e/flows/settings.spec.ts` and their S3 registry entries | Focused CRUD and origin-filter flows passed in both schemes; existing org-rename flow retained, full hosted execution pending |
| Axe, contrast and coarse-pointer targets | S3 closed flow registry assertions at desktop/mobile viewports and both schemes | Focused five-flow desktop/mobile assertions passed in both schemes; full registry execution closure pending |
| Federated CLI browser/device handoff | `internal/isolation/cli_social_acceptance_test.go`, `web/e2e/flows/login.spec.ts`, issue 612 transport fixtures | Actual CLI browser-handoff OIDC acceptance passed both engines; existing device tests retained, full standard isolation selection passed with recorded optional skips |
| Signup, mail-test, token lifetime, establishment window, send deadline and fresh-org cap | `internal/conformance/boundregistry_test.go`, `TestReconciledBoundsMatchOpsSpecValues` and executable fixtures | Final focused registry and pin run passed in 5.469 seconds; fresh-org cap passed both engines |
| Every registration and OAuth2 audit event emitted | `runRegistrationLifecycle` and `every_registered_type_is_actually_emitted` in `internal/isolation/audit_e2e_test.go` | Focused both-engine `TestAuditCore` passed |
| Committed mail intent without outcome | `runLocalSignupInterruptedMailOutcome`: real signup and TLS SMTP delivery, injected outcome-insert failure, then real resend | Focused both-engine test passed; datastore fault injection, no process-kill claim |
| New operation parity | `api/parity.yaml`, executable parity checks | Parity issue checker passed; unrelated issue references 807, 819 and 820 remain open |
| Full suite on SQLite and PostgreSQL | Repository full-suite CI | Hosted final-head result pending |
| Entra tenant rows and optional claims, Google client rules, GitHub app | `docs/site/src/content/docs/docs/social-signin.mdx` | Source and official provider documentation checked; local site verification passed |
| Mail variables and explicit test send | `docs/site/src/content/docs/docs/signup-mail.mdx` and managed configuration guide | Source checked; local site verification passed |
| Session rebuild and retired JIT fold release notes | `docs/site/src/content/docs/docs/upgrades.mdx` | Matched to migrations 44, 57 and 58 on both engines |
| Human-access and administration ledger | `docs/status/ledger.json`, generated root README and status README | Canonical regeneration and 35-entry check passed |
| Published docs site | Site deployment workflow and served pages | Publication pending; local build is not publication |

## Operator documentation

The social sign-in runbook covers tenant-GUID Entra issuers, one provider row and
callback per tenant, ID-token optional claims, and strict JSON boolean email
assertions. It documents Google's web client and exact callback requirements,
the accepted third-party-email assertion limitation, and local assurance for
disclosure. GitHub uses the supported GitHub.com OAuth App profile, pinned scope
and PKCE, write-only secrets and explicit registration admission.

The mail runbook distinguishes bootstrap variables from applied managed values,
active settings from candidate revision tests, and configured status from actual
SMTP submission and inbox delivery. It includes TLS and CIDR controls, shared
five-per-hour test admission, one concurrent send, the 15-second deadline,
24-hour single-use verification, durable intent/outcome events and resend after
an interrupted send. It preserves the managed configuration and restore fences.

Release notes follow the shipped migrations, rather than the synthesis's
historical migration numbers. Migration 57 preserves sessions and closes
disclosure reauthentication windows on both engines. Migrations 57 and 58 clear
in-flight OIDC transactions. Migration 44 already retired the old JIT policy;
migration 58 performs no second fold or audit cleanup. Existing explicit
identities remain, and admitting unknown identities needs an explicit policy.

## Verification record

`node scripts/ci/check-doc-status.mjs --write` regenerated the root README and
status README; `node scripts/ci/check-doc-status.mjs --check` verified all 35
entries. `pnpm --dir docs/site run verify` passed the Astro typecheck, content
and asset tests, production build (78 pages), OSS policy, sanitized-prototype
and PWA assertions, CSP and LLM document checks, and offline browser acceptance.
PostHog validation reported analytics disabled because deployment configuration
was not required. This is a local site result, not publication evidence.
The paired HTML rendered in the T3 headless browser at widths 390 and 1280,
with no document-width overflow and no remote assets. Its evidence table scrolls
within the page on narrow screens.

Focused backend evidence reported by the implementing worker:

- `TestLocalSignupInterruptedMailOutcome` and `TestAuditCore` passed on both
  engines in 45.784 seconds. The signup service commits the mail intent and
  completes delivery to the TLS sink; a datastore trigger then refuses the
  `mail_outcome` insert. The test asserts that the unmatched durable intent
  remains and a real resend produces a separate successful outcome. This is an
  outcome-persistence fault, not a seeded event or killed process.
- `TestCLISocialProviderAcceptance` passed on both engines in 3.229 seconds. It
  calls the actual CLI dispatcher with `login`, `--provider oidc:acceptance` and
  a named instance; protected trust custody, the TLS router, OIDC exchange,
  browser cookies, explicit approval, the loopback callback and persisted CLI
  session provenance participate. The first fixture attempt omitted `--name`
  and correctly failed the trust gate; the fixture was corrected. This is a
  real CLI integration using an in-repository provider fixture, not live
  upstream provider authorization.
- `TestLocalSignupFreshOrgCap` passed on both engines in 5.352 seconds. Two
  registrations are admitted before either creates an organisation; sequential
  verification proves a live cap recheck of previously admitted pending tokens.
  This fixture does not exercise simultaneous verification goroutines. A refused
  verification writes no account or organisation and leaves its pending token
  unspent; freeing an organisation slot permits that same token to finish.
  The initial fixture incorrectly expected no email for an admitted request
  while the cap was full and failed on both engines. The locked social-signin
  specification section 4 requires one synchronous send per admitted request;
  the state-creating verification checks the cap. The fixture was corrected to
  assert the send followed by verification refusal. Product behavior was not
  relaxed to make the test pass.
- `TestBoundRegistry` and `TestReconciledBoundsMatchOpsSpecValues` initially
  passed in 5.283 seconds. The final focused registry and pin run, including the
  additional cap fixture registration, passed in 5.469 seconds.
- `go vet ./internal/isolation ./internal/conformance ./internal/service`
  passed. The parity issue checker passed and confirmed that existing unrelated
  references to issues 807, 819 and 820 are still open.

The canonical core runner completed with exit zero and 118 packages containing
successful tests, using the PostgreSQL DSN as well as SQLite. The application
package took 422.622 seconds and the repository lint package 56.030 seconds.
The inventory preceded the new mail-sink command and lifecycle tests, which were
then covered separately by `go test -race ./internal/mailtest/... ./internal/mail
-count=1`: the mail-sink package passed in 1.307 seconds, mail in 1.636 seconds,
and the command compiled.

The final frontend unit run after the asynchronous cleanup repair passed
201 files and 1,756 tests in 18.46 seconds. Typecheck and lint passed again;
the production build passed before that fixture-only repair. Five focused acceptance flows
passed on desktop (4.6 minutes) and mobile (4.4 minutes): real TLS SMTP signup,
resend, token replay and fresh-org creation without a session; OIDC signup and
origin filtering; social-only establishment and password changes; operator test
mail; and GitHub provider creation, reconfiguration and deletion. Dark and light
states include the pinned assertion set and axe checks. Forty-two screenshots
were retained in the delivery session. Independent visual inspection covered
the desktop chooser, password mismatch, signup verification and origin filter,
and mobile signup, password mismatch and GitHub provider editor; affected fields,
text and actions fit without observed clipping. These filtered runs explicitly
skip the full registry execution-closure gate. They do not establish that every
pre-existing browser flow executed on this candidate.

Earlier browser attempts exposed fixture API-shape and locator errors. Their
logs and traces were retained; assertions and pre-existing tests were preserved.
A new SMTP subprocess test initially compiled Go inside Vitest's ordinary
five-second test budget. A fresh Go cache with two compiler workers reproduced
a timeout in 8.681 seconds. Fragmented SMTP, trusted TLS, recipient preservation
and dot unescaping now live in the transport owner's Go test
`TestSinkAcceptsFragmentedSMTPAndUnstuffsDots`; the actual browser mail flows
exercise the TypeScript command bridge. No test timeout, package test command,
Vite setup or fast-feedback toolchain policy was enlarged. The existing account
flow keeps its original timeout.

CI builds the mail-sink helper once with the app artifact, uploads it, restores
its executable mode and passes `HIKYO_E2E_MAIL_BINARY` to browser shards. The
artifact-reuse fixture and mutations removing each of those four requirements
passed; focused workflow tests passed in 1.931 seconds.

Independent review round 1 found the cold-build timeout and incomplete pinned
coverage of social-account states. Both were repaired. Round 2 reported CLEAN
with no actionable findings and independently exercised the Go sink under the
race detector ten times, the actual TypeScript command bridge including cleanup
and missing-executable refusal, workflow artifact mutations, the documentation
ledger and diff hygiene. The parent independently matched the reviewed 30-file
manifest hash `276139d75be5d582d1f80a9b3a05dcf74e224457c66ecbbb545d306bc1275bb5`.
The later account-test timeout removal was separately reviewed as a
strengthening. Parent source review then found that global fixture cleanup did
not await the new asynchronous mail child shutdown. All flow/security setup and
teardown callers now await shutdown; setup errors retain cleanup errors with
aggregate context. The sink waits for child close and complete pipe drainage
before removing its directory. Three focused unit regressions cover awaited
cleanup and error preservation. Four focused files with 18 tests passed in
2.77 seconds, followed by the complete final unit suite above. A normal desktop
setup, login-to-establishment route and teardown smoke passed (one test,
3.0 minutes including setup); the teardown wrapper asserted that published mail
state and its private directory were gone after awaiting cleanup. No mail child
remained afterward. A missing executable also exercised actual startup refusal
and awaited cleanup. The wrapper files were temporary and removed. The operator runbook's instance and GitHub
panel labels were also corrected against the actual UI. Round 3 reviewed that final lifecycle delta and reported CLEAN, with no
actionable findings. It independently passed six focused lifecycle tests and
actual subprocess checks for output drainage, forced termination, repeated
shutdown and spawn failure; it confirmed that every caller awaits cleanup and
that errors remain preserved. Its 36-file manifest hash was
`f1b1d17987523540c74cf616426446b2004010341e220cd20127ea49d52c0740`.
Subsequent handoff edits record verification evidence only. This is ordinary
implementation review, with no cross-provider-review claim.

Graphify updated successfully. Its existing 17 Astro partial-extraction warnings
remain recorded; graph relationships are navigational evidence, not runtime
proof.

All three canonical isolation shards passed, running sequentially with
SQLite and PostgreSQL 18.6 plus digest-pinned Moto and the canonical 18-minute
per-shard timeout. The complete planner selection contained 611 top-level tests:
605 passed and six optional tests skipped, with no failures. Shards took
545.459 seconds (195 passes, one top-level skip), 574.283 seconds (196 passes,
four skips) and 516.259 seconds (214 passes, one skip). The required Moto
emulator lifecycle ran successfully.

The complete top-level skip inventory is:

- `TestForgejoRealLifecycle`: external Forgejo fixture not configured.
- `TestAWSSecretsManagerRealSmoke`: live AWS credentials not configured.
- `TestFrozenGeneratedClientAgainstCurrentServer`: the separately provisioned
  generated-client lane was not enabled through its frozen client root.
- `TestGitLabRealLifecycle`: external GitLab fixture not configured.
- `TestMCPDeploymentFixture`: explicit disposable deployment fixture not enabled.
- `TestMCPCodexNative`: native Codex interoperability opt-in not enabled.

Two additional nested skips are the SQLite and PostgreSQL children of
`TestAdapterVaultKVEndToEnd`, because the external Vault fixture was not
configured. The SQLite child of
`TestTransitAdmissionLockAllowsForeignKeyChecksAndSerializesAdmissions` is
intentionally inapplicable to its PostgreSQL locking assertion. No database
engine was skipped because of a missing PostgreSQL DSN.

The core inventory and first isolation shard preceded the final test-helper
lifecycle extraction. The final helper has independent race coverage above;
a final exact-source rerun of `TestAuditCore`,
`TestCLISocialProviderAcceptance`, `TestLocalSignupInterruptedMailOutcome` and
`TestLocalSignupFreshOrgCap` passed on SQLite and PostgreSQL in 45.344 seconds,
with no skipped cases. The final registry and numeric-pin checks passed in
5.314 seconds. This closes the local test-helper snapshot difference without
presenting the earlier broad run as an exact final-source hosted result.
Hosted CI and documentation publication remain separate pending gates.

No real Google, Entra or GitHub account authorization, external SMTP inbox
delivery, hosted CI, release, production deployment or public docs publication
is established by this report's source inspection. Provider and mail fixtures
prove their stated local protocols and lifecycle behavior only.

## Delivery gates

The issue requires DCO and cryptographic signing for every commit, both-engine
schema and lifecycle coverage, the requested review rounds, a verified preview,
and the final hosted SQLite/PostgreSQL run. Report each gate's observed result
separately. A skipped review is not clean, and a queued merge or built site is
not a completed merge or published docs site.

ADRs and `docs/spec/open-items.md` remain outside the implementation edits.
The accepted residuals stay with their owning decisions; this work does not
silently convert them into new defaults or obligations.
