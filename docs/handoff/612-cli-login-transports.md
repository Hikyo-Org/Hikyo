# #612: CLI browser login and device authorization

Issue: https://github.com/Hikyo-Org/Hikyo/issues/612. Governing decisions: api-cli-surface, ops-spec, #596 as amended by #604 and #589. The owner explicitly selected RFC 8628 compliance on 2026-10-15; #589 and #32 were reopened to record that ruling. The implementation starts from main `c12e7d39c9f227b0cb884ba69e4f1bd77c895f76`. After reviewing the local implementation and its disclosed cross-provider review limitation, the owner authorized signed commit, PR creation, review/CI repairs and merge through completion. Release and deployment remain outside scope. The evidence below records local validation; remote delivery status belongs to the linked PR and is not inferred from these results.

The necessity check found both browser and device login explicitly refused by the existing CLI, with those refusals pinned in its exit-code matrix. #607 was closed, and the MCP architecture research explicitly preserved #612 as a distinct CLI session transport. The issue remains necessary.

## Resulting behavior

`hikyo login <url>` opens the instance's ordinary staged login under a loopback handoff. `--provider <kind>:<slug>` preselects the provider; a bare slug works only when unambiguous across enabled kinds. Unknown and ambiguous providers refuse before a browser opens. `--as` belongs to `--local`; interactive browser/device login refuses without a terminal. The terminal-native local login remains available.

The listener binds an ephemeral port on a loopback IP literal, with socket reuse disabled before bind. It validates the exact callback host, path, state and query shape and closes the listener before redeeming. Only code and state cross that callback. Redemption requires the initiating PKCE S256 verifier and the exact redirect URI through the CLI's existing pinned transport.

The browser uses the existing password, TOTP, passkey, OIDC, SAML and OAuth2 ceremonies. A signed-in browser goes directly to explicit approval. The page names CLI, the instance, login purpose and the CLI session being created; it hides the sign-up door and sends `sign-in`. Provider transactions keep their existing bindings. The browser retains its session.

`--device` uses RFC 8628 form requests for device authorization and token polling. The device code stays secret; the user receives only the verification URL and an eight-character Crockford user code. Authorization expires after fifteen minutes. Polling starts at five seconds; each `slow_down` adds five seconds without a thirty-second cap. Exponential backoff is reserved for connection timeouts. Valid 429 `Retry-After` responses retain the transaction and slow polling within its expiry. Successful token responses contain `access_token`, `token_type: Bearer`, `expires_in` and Hikyo session metadata.

## Trust and storage boundaries

Migration 00075 creates server-owned `cli_login_handoffs` on SQLite and PostgreSQL. State, authorization/device code and user code are stored as verifiers. Approval stores the browser session id and a narrow assurance/provider snapshot. Redemption revalidates that session under its principal lock, including expiry, generation, restore epoch and provider invalidation, then atomically consumes the transaction.

The resulting `ArtifactCLISession` is a separate session with CLI idle/absolute clocks. It snapshots method, factors, authentication time, ceremony id and all provider provenance fields. It inherits no reauthentication windows or credential-establishment evidence. `sessions.cli_handoff_id` records its issuing transaction without changing the existing workspace origin/handoff pairing; active-session metadata exposes the issuing id through the established handoff field. Expired transaction rows are pruned at issuance.

All six endpoints remain inside the shared pre-authentication admission budget. Browser inspection and approval additionally require a browser artifact in the service. The API pins its human-session versus anonymous legs and excludes machine/developer credentials. Scalar form parameters must occur once in the request body: duplicate, missing, wrong-client, wrong-grant and JSON device requests cannot bypass schema validation. Login transport responses carry `Cache-Control: no-store` and `Pragma: no-cache`.

`meta.protocol_capabilities` advertises `loopback-handoff` and `device-code`. OpenAPI, generated Go/TypeScript, operation/parity registries, CLI help/goldens, route inventory, authz forwarding inventory, storage authority reasons and verifier classification were updated at their owners. The ops ADR has a dated owner amendment; its historical capped-doubling table is preserved and explicitly superseded.

## Initial implementation validation evidence

Initial evidence: `go test ./internal/cli -run '^TestExitCodeMatrix$' -count=1` passed against main, confirming the pre-implementation refusals. The first implementation lifecycle suite passed on both engines in 24.476 seconds, using actual password and virtual-authenticator passkey ceremonies, a signing OIDC fixture and the GitHub OAuth2 fixture's real start/callback exchange before approval and redemption.

The expanded `TestCLILogin*` suite covers exact redirect/proof rejection, replay and concurrent consumption, browser retention, provider snapshot invalidation, no inherited windows or establish evidence, device pacing past thirty seconds, concurrent durable slowdown increments, expiry, logout, principal generation, restore fencing, configured-origin refusal, and SAML's actual signed exchange. The wire suite drives the real router, form decoder, CSRF gate, admission limiter and both datastores with a controlled clock, including a legitimate pending flow that survives 429 and completes after more than a minute. Controlled-clock tests do not claim fifteen minutes of wall-clock waiting.

The CLI worker's complete CLI suite and focused race checks pass. Socket-option checks ran on macOS and in a disposable Linux container and observed reuse flags cleared; strict callback and early-closure cases passed on both. Windows was cross-compiled successfully; Windows runtime behavior was not exercised.

Parent independently passed the final existing service suite (92.052 seconds), authn store suite (0.677 seconds) and authz suite (0.881 seconds), including the completed failure-audit helper. The development compatibility declaration was regenerated from fresh catalogs and independently checked against a second fresh PostgreSQL database. No production compatibility artifact was changed. The full server and API suites passed in 65.894 and 2.212 seconds. The full structural lint package passed in 74.129 seconds. The final focused both-engine suite, `go test ./internal/isolation -run '^TestCLILogin' -count=1 -v`, passed in 49.647 seconds across nine roots and eighteen engine legs. All 135 generated files remained byte-identical through an exact sqlc/OpenAPI/authz/TypeScript refresh. Go import formatting passed for 1,972 handwritten files; generated TypeScript client typecheck and all 37 client tests passed.

The WebUI worker passed 199 files and 1,707 unit tests, typecheck, lint, four Storybook stories and the production build. Final validation explicitly selected repository-pinned Node 26.7.0 through `fnm exec --using=26.7.0`; earlier extra runs under the WebUI shim's Node 24.21.0 are not the final claim. The native embedded-SPA browser run passed all eight desktop scenarios in 3.0 minutes and all eight mobile scenarios in 2.8 minutes. The exact latest-source rebuild was byte-identical to the tested binary: SHA256 `02d5088a3c6b75c99bbbabec78aebb4738143bc054fa0bddc0832ab3caf49d24`. Parent independently inspected desktop/mobile dark and light screenshots and found no clipping or readability issue.

## Resolved failed attempts

The first schema-opening tests failed with `embedded migration bytes differ from verified build`; no lifecycle assertion had run. Regenerating and independently checking the development compatibility declaration resolved that gate. New SQL authority annotations initially omitted their reasons, then placed them in the wrong order; the canonical reason-before-annotation form fixes the structural check. New authz methods initially failed the preserved public-surface inventory; their intentional narrow signatures are now reviewed and pinned. API tests caught missing route pins, the named redemption-proof exemption and artifact-refusal responses; those declarations were added without weakening the checks.

The real malformed-form test exposed a repeated `client_id` accepted by the generic form decoder. The runtime validation owner now rejects duplicate scalar fields before decoding. A restore-epoch test initially expected only the unauthenticated sentinel; the actual outer upgrade-maintenance fence returned its stronger stale-operation refusal before issuance. The test accepts that refusal and still requires an empty token. Existing coverage was retained. R1 additionally found the missing failure-audit path and UI state that survived a changed authorization reference; both were repaired and covered before R2. A new audit-count test initially declared an `int` return for the harness's `int64` count; correcting the test type resolved that compile failure. The Go formatting tools disagreed on alignment within a large existing route-pin map; a blank grouping line preserves every entry and makes both formatters agree. No formatting gate was weakened.

## Initial review and delivery boundaries

The user requested delegation to Sol 6.1 at high effort, overriding the issue's historic model label. Independent adversarial R1 found missing failed-login audit emission and a UI authorization-state remount issue. Both were fixed, with retained regressions. Loopback proof/refusal failures now use the existing durable rollback-audit carrier; committed device denials and expiry record `auth.login` failure. Normal pending and slowdown responses do not create failed-login spam. The final both-engine failure-audit regression checks durable counts and absence of state, code, verifier and browser bearer in audit payloads. Independent R2 concluded CLEAN over the final 73-file implementation manifest `fdc958dfe396834cc32f65273fa1c375c41d40f6de3b6abac12d87e4ed2c6b05`; the manifest is retained with the private review artifacts. Cross-provider review is unavailable under the recorded reviewer policy and is not reported as CLEAN. At that checkpoint, no remote CI, current-head PR review, merge or production/runtime deployment was claimed. Preview and both-engine evidence remain distinct.

## Initial completion addendum

Final source review, generated freshness and the complete eighteen-leg both-engine run are complete. Scoped `go vet` passed for service, server, authn store, authz, CLI and API. Desktop and mobile browser acceptance are complete on the same current binary. All fixture listeners on ports 14801 through 14805 were closed after verification. There is no retained preview. The parent owns final review and delivery decisions.


## PR #924 review repairs and rebase

The branch was rebased onto `69475a27e85cd4e2ad823a557ebf9a5507bdd5d7` after PR #926 supplied the matrix query-identity optimization and consistent Undici fetch/dispatcher ownership. The upstream runtime files are preserved byte-for-byte; this PR retains its additional matrix identity, updated-value and cached-data permission-refusal regression. The original browser-performance failure is preserved as evidence. Controlled base/head measurements reproduced the inherited render cost; the rebased stock suite passes all four cases with unchanged median and maximum thresholds. Desktop save/live-update medians were 72.9/84.7 ms, mobile 45.6/35.9 ms. Artificial CPU-throttled trials are diagnostic evidence, not hosted acceptance.

The incoming Qodo findings are repaired at their owners. All six CLI login operations remain available during configuration recovery, while ordinary business operations retain their 503 fence. A real-HTTP regression on both SQLite and PostgreSQL covers both transports, authenticated browser approval, CSRF, PKCE and replay refusal. Full server, structural lint and API suites and scoped vet passed. Five punctuation replacements in canonical shared OpenAPI descriptions regenerate the client comments without wire changes; client typecheck, 37 tests and a 19-artifact generation fixed point passed.

Terminal login now uses local monotonic budgets derived from device `expires_in` or the established loopback lifetime, preserving earlier caller deadlines through redemption. Session timestamps are validated against their server-created timestamp and the server expiry is retained unchanged in custody. Actual mock-server regressions cover both transports with plus/minus 48-hour skew; deterministic-clock tests cover polling expiry and shorter caller deadlines. Full CLI tests, focused race checks, vet and Windows cross-compilation passed. Windows runtime remains untested.

The consent page relies on refreshed server transaction state and atomic approval for expiration, retaining timestamp format, immutable metadata, reference, callback, auth revision and cancellation checks. Tests reproduce the old refusal before the fix and cover server expiry refusal and valid skewed metadata. Existing AuthProvider clock behavior is outside this repair; these results do not establish arbitrary-clock support for every browser authentication ceremony. The misplaced route comment is corrected. The committed reproduction instructions below no longer expose workstation paths or depend on someone else's temporary files.

Fresh independent Sol 6.1 high review of the rebased repairs concluded CLEAN. Its 14-file implementation manifest excludes only this mutable handoff; canonical SHA256 is `077800dd45db51d3f45236568c26bc338433b2678cbf6c6d3f8824b49645d5a5`. Post-rebase Node 26.7.0 typecheck, lint, production build and all 1,713 unit tests pass. Cross-provider review remains skipped. Required hosted checks and exact merge-group validation are separate delivery gates.

Post-rebase native browser acceptance passed all nine scenarios on desktop and all nine on mobile, including a browser clock twenty minutes ahead and server-authoritative expiry refusal. Both campaigns used the same embedded-UI binary, SHA256 `289801a9fd9690495064f0824f5bc72d1a06618aff3f6e4f4b88fcf51dcaead6`. Fixture listeners were closed after validation.

## Jev advisory

The parent ran one advisory against the final local candidate, explicitly without a PR or remote CI and while mobile was pending. `desc_match` was 0.68 and `untested_logic` 0.47; no candidate threshold crossed. The `auth_or_tenant` risk probability was 0.97 with 0.96 confidence. Merge readiness was 0.99 on the 0-to-3 scale, confidence 0.61, with probabilities blocked 0.20, not-ready 0.61, nearly-ready 0.19 and ready 0.00. This describes the unrequested remote delivery gates and unavailable cross-provider ADR review; it does not replace the independent local review or create a blocker to the authorized local endpoint. No merge readiness is claimed. The advisory was not rerun merely to replace its mobile-pending snapshot.


## Browser evidence and reproduction

Run these commands from the repository root. Prerequisites are the installed repository dependencies and Playwright Chromium, `fnm` with Node 26.7.0 and repository-pinned pnpm 11.24.0, Go from `go.mod`, and five free fixture ports (14801 through 14805).

Build the production WebUI before the Go binary with embedded assets. The temporary directory is created for this run; retain `HIKYO_E2E_BINARY` in the same shell for both browser commands:

```sh
hikyo_cli_evidence_dir="$(mktemp -d)"
export HIKYO_E2E_BINARY="$hikyo_cli_evidence_dir/hikyo"
(cd web && fnm exec --using=26.7.0 node --run build) &&
  go build -tags ui -o "$HIKYO_E2E_BINARY" ./cmd/hikyo
test -x "$HIKYO_E2E_BINARY"
shasum -a 256 "$HIKYO_E2E_BINARY"
```

Both builds and the executable check must succeed before testing. The hash identifies this new build; source or toolchain changes need not reproduce the historical hash recorded above.

Run desktop first, then mobile in the same shell. The fixture owns their server lifecycle and the projects share its temporary authentication files:

```sh
(cd web && HIKYO_E2E_PORT=14801 HIKYO_E2E_PORT_B=14802 HIKYO_E2E_PORT_TLS=14803 HIKYO_E2E_PORT_OPERATIONAL=14804 HIKYO_E2E_PORT_OPERATIONAL_B=14805 fnm exec --using=26.7.0 pnpm exec playwright test --project=desktop login.spec.ts --grep 'CLI login handoff')
```

```sh
(cd web && HIKYO_E2E_PORT=14801 HIKYO_E2E_PORT_B=14802 HIKYO_E2E_PORT_TLS=14803 HIKYO_E2E_PORT_OPERATIONAL=14804 HIKYO_E2E_PORT_OPERATIONAL_B=14805 fnm exec --using=26.7.0 pnpm exec playwright test --project=mobile login.spec.ts --grep 'CLI login handoff')
```

The repository fixture owns both server instances and their security-sensitive cookie/CSP behavior. These are disposable local fixtures, not a production deployment. The initial runs each reported eight passing tests. Local logs and temporary binaries are non-durable evidence; repeat the commands for the current revision.

The real browser paths cover local password plus TOTP, virtual-authenticator passkey, signed-fixture OIDC loopback and redemption, and device approval with cumulative ten/fifteen-second slowdown and redemption. The browser OAuth2 case mocks only discovery/start to assert forced `sign-in`, hidden sign-up and preserved return after refusal. Real GitHub-profile start/callback, approval, redemption and provider invalidation run on both actual datastores in the service suite. These are separate evidence boundaries. No real external provider account or physical authenticator was used.

The private paired Markdown/HTML report retains the original unit, Storybook, production-build and desktop/mobile logs, plus all four desktop/mobile theme screenshots. These local artifacts are not repository dependencies or portable build inputs. The HTML embeds the screenshots for offline reading. A browser fixture initially mixed builtin fetch with an Undici 8 agent; using one module's fetch, agent and Response type fixed its actual network regressions. Parallel per-instance compilation initially starved the startup health deadline; the final run used the fixture's existing supported prebuilt-binary path, not a relaxed timeout.

The private report also retains the exact source manifest and marks the earlier assessment superseded. This committed handoff contains the reproducible commands and evidence boundaries without private workstation paths. No production state was changed.

## Merge-queue repair evidence

Merge-group run 38803109736 attempt 1 tested PR head `79e5f1bf2497cf27298b53a49769459f05e204ae` with base `69475a27e85cd4e2ad823a557ebf9a5507bdd5d7`, producing candidate `7a4c480b08d352be6f3043d298f3e22c04df882e`. Its failures exposed stale fixture and inventory expectations. Core, offline Cosign and all eight failing race shards stopped at the legacy restore fixture's migration-74 suffix. The fixture now explicitly admits migration 75, refuses any CLI transaction or session provenance before reversal, and preserves the immutable legacy prefix and final catalog verification. A new subprocess regression fails without either evidence guard on both engines. This is test-only fixture construction, not a product downgrade mechanism.

Linux nanosecond clock values exposed a test comparison against the transient login return rather than the persisted approving session. The snapshot assertion now preserves full assurance equality against that durable identity, including its canonical microsecond timestamp. An explicit 123-nanosecond clock reproduces the old failure on both engines, including hosts whose ordinary clock already has microsecond precision. Product authentication behavior is unchanged.

The annotated-query inventory adds sixteen reviewed transaction pins and updates four session-query hashes for CLI provenance; existing principal filters are unchanged and no pin was removed. Four exact preparation routes now carry reasons in the audit exemption inventory. Their start, metadata and approval stages issue no completed session, and the closed vocabulary defines no separate CLI-login preparation event. A new both-engine regression proves that each transport emits exactly one successful auth.login and auth.session_created at redemption, both naming the new CLI session; existing failure-audit coverage remains intact. The CLI transport exemption now names all three login paths.

All isolation invariants, the complete CLI-login lifecycle group, audit completeness and closure, and successful/failed login audit tests passed on SQLite and PostgreSQL. Fixture race tests passed. Broader local app validation additionally found a real early-maintenance listener defect: bootstrap retains explicit operational flags and environment values in managed node inputs, but maintenance boot used the default field before normal configuration resolution. A fixture-only environment override was tested and failed. The canonical configuration owner now resolves and validates only the early operational binding, and maintenance uses it without reading seed files or runtime configuration. Normal managed-node precedence and default bindings are preserved. Regression tests cover both upgrade-refusal branches, flag-over-environment precedence, fallback, invalid addresses rejected before bind, and absence of tenant-serving resources. The original packaged integration fixtures remain unchanged.

Final local validation of the repaired source passed: the full app suite in 365.547 seconds with SQLite, PostgreSQL and required real Cosign v3.1.3; the full configuration suite; all isolation invariants; the complete CLI-login lifecycle and success/failure audit groups; fixture race checks; structural lint; scoped vet and import formatting. Actual automatic and unattended packaged upgrade scenarios passed using their original fixtures while the competing local listener remained running. Independent Sol 6.1 high review of the final nine implementation/test files concluded CLEAN; its canonical manifest SHA256 is `42d8e24f5cc6ba888a515a00fbbb8da93b9494e9e40fecf111c2b3f11daf52cb`. The earlier environment-only attempt failed and is superseded. Cross-provider review remains skipped. Fresh hosted merge-group validation is still required.
