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
The later hosted CI repair is recorded separately below. This is ordinary
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

## PR review documentation corrections

Two documentation findings on PR 928 at head `0f1078e83` were independently
confirmed and corrected. The old Google OIDC citation returned HTTP 404. The
replacement [ID-token verification guidance](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token)
returned HTTP 200 and explicitly describes Google's authority for Gmail,
verified Workspace addresses with `hd`, and the third-party-address limitation
without `hd`. The general OIDC article did not establish that specific claim.

The CLI example now records `--name example` at login and selects
`whoami --instance example`. The current command help accepts `--name REF` for
login and `--instance REF` for whoami. Source inspection confirmed that
`originReference` stores the chosen name and `authenticatedResolvedTarget`
looks up that reference directly in the trust store. The example URL is not a
stored reference. Documentation checking passed with zero errors, warnings or
hints, the production documentation build produced 78 pages, and the ledger
check passed. This correction changed no authentication policy or product code;
all earlier failed fixture attempts remain recorded above.

## Hosted CI conservation failure and repair

Full candidate run [38865811493](https://github.com/Hikyo-Org/Hikyo/actions/runs/38865811493)
on head `0f1078e83` failed in race shard 4. The observed failures were
`TestFullCandidateConservesBaselineCoverage` and
`TestCoverageInventoryRejectsExecutionReductions`, both rejecting the
unmutated workflow's execution configuration. No data-race report was present
in that job's log. The run later completed as cancelled by supersession;
remaining jobs were unfinished. Its failed job is not a successful hosted
acceptance result. Cancellation was requested for the subsequent run
[38867231228](https://github.com/Hikyo-Org/Hikyo/actions/runs/38867231228)
on documentation-only successor `21366c9ca`, which retained the same workflow
configuration. A fresh full candidate is required on the repair head.

The same two conservation tests failed locally before the repair. The browser
mail fixture had added its build command, upload path, executable-mode argument
and flow environment variable, but the conservation adapter had not accounted
for those four additions. The earlier focused artifact-wiring tests passed
without exercising the complete frozen execution inventory. The earlier
118-package core run also preceded the final workflow additions and did not
verify this final conservation contract.

The adapter now requires exactly one correctly named owner for each addition
and its exact approved field value, then restores only that field's historical
value before checking the existing digests. Existing build commands, artifact
paths, conditions, error handling and all other execution configuration remain
covered by the frozen inventory. The historical baseline JSON is unchanged;
its SHA-256 remains
`b8dd91b4805a2db537d40ad359d38c48df649bf671abefb190c160dc9f8a5adf`.
Thirty-two new negative cases reject missing, malformed or duplicated additions,
missing or duplicated owners, skipped or fail-open owners, and changes to
original checks. The existing positive and execution-reduction tests remain.
The focused suite passed in 1.289 seconds. The complete
`go test -race -count=1 ./scripts/ci` suite then passed in 106.667 seconds,
and `go vet ./scripts/ci` passed. Parent source review found no actionable
issue in this narrow guard repair. No workflow check, test or timeout was
removed or relaxed; a successful hosted run on the repair head remains pending.

## Hosted floor startup refusal and repair

The required floor job in [run 38868172159](https://github.com/Hikyo-Org/Hikyo/actions/runs/38868172159/job/116865813823)
on head `8bbf159bd`, merge candidate
`2a247566407b486ff4539dd98b64eaf0e6fc05ec`, refused during development-server
boot. Its retained private `dev-server.log` records a bind failure for
`127.0.0.1:49762`: address already in use. The driver exited before readiness
in 1.15 seconds. Boot, idle, KDF, publish and rewrap fields remained unmeasured;
the zero fields do not establish measured zero resource usage. The actual
operator lane passed on the same source and binary, the direct scanner exited
successfully, and the floor container was not OOM-killed. Those partial results
do not make the composite pass. This was a startup refusal rather than an
observed performance-threshold exceedance.

The harness had fixed public and operational ports `49761` and `49762`, and
polled operational readiness before the server bound that port. A deterministic
native Linux reproduction restricted the isolated container's ephemeral range
to `49762..49762` while using the same Go HTTP transport pattern. The early
request self-connected, Go retried it, and a loopback `TIME_WAIT` socket remained
on `49762`. The subsequent server-style bind failed with address already in
use. Explicit client `LocalAddr` port zero also failed to prevent the collision.
A fresh control using target `19762` outside the same ephemeral range received
ordinary connection refusal and then bound successfully. The hosted artifact
contains no historical socket table, so the exact socket owner at its failure
cannot be recovered. The reproduction establishes a concrete reachable harness
failure mechanism; it does not invent missing historical socket evidence.
The earlier unobserved startup failure recorded in the #611 handoff retains
that uncertainty.

The isolated harness now uses public `19761` and operational `19762`, shares the
operational address with its readiness client, and checks the actual Linux
ephemeral range before launching the child. Missing, malformed or overlapping
range configuration refuses immediately. Fourteen deterministic regression
cases cover the original conflicting ports, the reproduced allocation,
independent public and operational boundaries, malformed settings and unassigned
listener ports. No retry hides a failed boot. The server workload, one-minute
readiness deadline, five-second idle sampling, floor thresholds, CPU factors,
required coverage and optional 600-minute benchmark policy are unchanged.

The complete `go test -race -count=1 ./internal/bench` suite passed in
1.406 seconds; `go vet ./internal/bench` and native Linux ARM64 tagged-floor
compilation passed. An actual-helper startup smoke used the final
`floorBootCommand` and a freshly built real `/hikyo` in a native Linux,
network-isolated, read-only container with four CPUs, 4 GiB and zero swap.
With ephemeral allocation restricted to `49762..49762`, it reached
`/readyz` HTTP 200 and shut down gracefully in 0.68 seconds; all ordinary bench
unit tests passed in the same container. The temporary overlay probe was not
added to the checkout. An initial all-unit probe omitted the unchanged
`derate.json` policy fixture: policy-dependent tests failed to open that file,
while the real startup smoke passed in 0.85 seconds. Mounting the existing
policy read-only repaired the probe setup; no test was removed or filtered.
A separate actual-command test with ephemeral allocation `19762..19762`
refused the operational-only overlap before child launch. These are local
harness and startup checks, not a complete hosted floor acceptance run.
Parent review found no actionable issue in the repair after strengthening the
operational-only regression case. Exact-candidate hosted floor proof is pending.

## Go feedback cache recovery

The older head `21366c9ca` Go feedback job in
[run 38867206870](https://github.com/Hikyo-Org/Hikyo/actions/runs/38867206870/job/116862676638)
missed both its exact bounded compiler cache and compatible prefix, then
exhausted the existing 220-second deadline during actual six-package compile
and link work. It reported no compiler diagnostic or test assertion failure.
The feedback handoffs explicitly document that a cold miss may exceed that
budget. This failure is separate from both the conservation and floor startup
failures above.

Head `8bbf159bd` initially failed the same cold-cache deadline in
[run 38868144627, attempt 1](https://github.com/Hikyo-Org/Hikyo/actions/runs/38868144627/job/116865641371).
The documented trusted-main seed [run 38868529623](https://github.com/Hikyo-Org/Hikyo/actions/runs/38868529623)
was dispatched without source,
timeout, selection or trust-policy changes. Parent inspection subsequently
observed a successful current-head Go feedback retry: the exact cache hit
restored in about 5.3 seconds, and selected compilation completed in 101 seconds.
This verifies that consumer's recovery, not a universal warm-cache latency
promise. Required floor acceptance remains a separate gate; the optional
benchmark budget was not bypassed.

## Hosted floor proof and browser status correction

The required floor job [116876709379](https://github.com/Hikyo-Org/Hikyo/actions/runs/38871565270/job/116876709379)
passed on head `8d7fd5213`, clean merge candidate
`fc4d37ae1490c8ec62cb7975f54b2d8175251380`. Parent inspection confirmed the
native ARM64 four-CPU, 4 GiB, zero-swap configuration, zero OOM counters and
passing same-source operator evidence. This supersedes the earlier pending
floor proof for that candidate; the earlier failed artifacts remain recorded.

The same candidate's [desktop browser group 3](https://github.com/Hikyo-Org/Hikyo/actions/runs/38871565270/job/116878146694)
finished with 74 passing tests and one failure in GitHub provider creation.
The trace shows successful real provider PUT and list GET responses. The save
assertion selected every status in the panel, encountering both the save
confirmation and the prior empty-list message while the asynchronous list
refresh completed. The trace's old paragraph was temporarily inert because
it was background content behind the closing editor. A controlled local
reproduction held only the real list GET response, preserved the real provider
write, and reproduced the strict mode failure with both paragraphs present
after inert cleanup. No persistent inert defect was established.

The assertion now selects the visible `Provider saved` status, matching the
existing mail-test and provider-delete assertion pattern. API advertisement,
reconfiguration, disabling and deletion checks remain intact. There is no
product change, retry, removed assertion or deadline increase. The temporary
response hold is diagnostic only and remains outside the final source.

The controlled original assertion failed in 7.2 seconds; the same controlled
flow passed with the corrected assertion in 6.9 seconds (2.6 minutes including
setup), retaining both status paragraphs while the real list response was held.
After removing the diagnostic interception, the complete provider flow passed
on desktop in 7.5 seconds (2.5 minutes including setup) and mobile in 5.6 seconds
(2.4 minutes including setup). Both normal runs kept the pinned assertions,
axe checks, dark/light screenshots and all downstream real API checks.

The complete web unit suite passed: 201 files and 1,756 tests in 13.61 seconds.
The canonical typecheck and deny-warnings lint passed, and the structural graph
refresh completed successfully. These filtered browser runs explicitly skip
the registry execution-closure gate. Full hosted validation on the subsequent
browser-repair head remains required; the floor success above belongs to
head `8d7fd5213`, not an untested successor.

## Delivery gates

The issue requires DCO and cryptographic signing for every commit, both-engine
schema and lifecycle coverage, the requested review rounds, a verified preview,
and the final hosted SQLite/PostgreSQL run. Report each gate's observed result
separately. A skipped review is not clean, and a queued merge or built site is
not a completed merge or published docs site.

ADRs and `docs/spec/open-items.md` remain outside the implementation edits.
The accepted residuals stay with their owning decisions; this work does not
silently convert them into new defaults or obligations.
