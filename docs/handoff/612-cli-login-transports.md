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

## Validation evidence

Initial evidence: `go test ./internal/cli -run '^TestExitCodeMatrix$' -count=1` passed against main, confirming the pre-implementation refusals. The first implementation lifecycle suite passed on both engines in 24.476 seconds, using actual password and virtual-authenticator passkey ceremonies, a signing OIDC fixture and the GitHub OAuth2 fixture's real start/callback exchange before approval and redemption.

The expanded `TestCLILogin*` suite covers exact redirect/proof rejection, replay and concurrent consumption, browser retention, provider snapshot invalidation, no inherited windows or establish evidence, device pacing past thirty seconds, concurrent durable slowdown increments, expiry, logout, principal generation, restore fencing, configured-origin refusal, and SAML's actual signed exchange. The wire suite drives the real router, form decoder, CSRF gate, admission limiter and both datastores with a controlled clock, including a legitimate pending flow that survives 429 and completes after more than a minute. Controlled-clock tests do not claim fifteen minutes of wall-clock waiting.

The CLI worker's complete CLI suite and focused race checks pass. Socket-option checks ran on macOS and in a disposable Linux container and observed reuse flags cleared; strict callback and early-closure cases passed on both. Windows was cross-compiled successfully; Windows runtime behavior was not exercised.

Parent independently passed the final existing service suite (92.052 seconds), authn store suite (0.677 seconds) and authz suite (0.881 seconds), including the completed failure-audit helper. The development compatibility declaration was regenerated from fresh catalogs and independently checked against a second fresh PostgreSQL database. No production compatibility artifact was changed. The full server and API suites passed in 65.894 and 2.212 seconds. The full structural lint package passed in 74.129 seconds. The final focused both-engine suite, `go test ./internal/isolation -run '^TestCLILogin' -count=1 -v`, passed in 49.647 seconds across nine roots and eighteen engine legs. All 135 generated files remained byte-identical through an exact sqlc/OpenAPI/authz/TypeScript refresh. Go import formatting passed for 1,972 handwritten files; generated TypeScript client typecheck and all 37 client tests passed.

The WebUI worker passed 199 files and 1,707 unit tests, typecheck, lint, four Storybook stories and the production build. Final validation explicitly selected repository-pinned Node 26.7.0 through `fnm exec --using=26.7.0`; earlier extra runs under the WebUI shim's Node 24.21.0 are not the final claim. The native embedded-SPA browser run passed all eight desktop scenarios in 3.0 minutes and all eight mobile scenarios in 2.8 minutes. The exact latest-source rebuild was byte-identical to the tested binary: SHA256 `02d5088a3c6b75c99bbbabec78aebb4738143bc054fa0bddc0832ab3caf49d24`. Parent independently inspected desktop/mobile dark and light screenshots and found no clipping or readability issue.

## Resolved failed attempts

The first schema-opening tests failed with `embedded migration bytes differ from verified build`; no lifecycle assertion had run. Regenerating and independently checking the development compatibility declaration resolved that gate. New SQL authority annotations initially omitted their reasons, then placed them in the wrong order; the canonical reason-before-annotation form fixes the structural check. New authz methods initially failed the preserved public-surface inventory; their intentional narrow signatures are now reviewed and pinned. API tests caught missing route pins, the named redemption-proof exemption and artifact-refusal responses; those declarations were added without weakening the checks.

The real malformed-form test exposed a repeated `client_id` accepted by the generic form decoder. The runtime validation owner now rejects duplicate scalar fields before decoding. A restore-epoch test initially expected only the unauthenticated sentinel; the actual outer upgrade-maintenance fence returned its stronger stale-operation refusal before issuance. The test accepts that refusal and still requires an empty token. Existing coverage was retained. R1 additionally found the missing failure-audit path and UI state that survived a changed authorization reference; both were repaired and covered before R2. A new audit-count test initially declared an `int` return for the harness's `int64` count; correcting the test type resolved that compile failure. The Go formatting tools disagreed on alignment within a large existing route-pin map; a blank grouping line preserves every entry and makes both formatters agree. No formatting gate was weakened.

## Review and delivery boundaries

The user requested delegation to Sol 6.1 at high effort, overriding the issue's historic model label. Independent adversarial R1 found missing failed-login audit emission and a UI authorization-state remount issue. Both were fixed, with retained regressions. Loopback proof/refusal failures now use the existing durable rollback-audit carrier; committed device denials and expiry record `auth.login` failure. Normal pending and slowdown responses do not create failed-login spam. The final both-engine failure-audit regression checks durable counts and absence of state, code, verifier and browser bearer in audit payloads. Independent R2 concluded CLEAN over the final 73-file implementation manifest `fdc958dfe396834cc32f65273fa1c375c41d40f6de3b6abac12d87e4ed2c6b05`; the local manifest is `/tmp/hikyo-612-r2-final-manifest.json`. Cross-provider review is unavailable under the recorded reviewer policy and is not reported as CLEAN. No remote CI, current-head PR review, merge or production/runtime deployment is claimed. Preview and both-engine evidence remain distinct.

## Completion addendum

Final source review, generated freshness and the complete eighteen-leg both-engine run are complete. Scoped `go vet` passed for service, server, authn store, authz, CLI and API. Desktop and mobile browser acceptance are complete on the same current binary. All fixture listeners on ports 14801 through 14805 were closed after verification. There is no retained preview. The parent owns final review and delivery decisions.


## Jev advisory

The parent ran one advisory against the final local candidate, explicitly without a PR or remote CI and while mobile was pending. `desc_match` was 0.68 and `untested_logic` 0.47; no candidate threshold crossed. The `auth_or_tenant` risk probability was 0.97 with 0.96 confidence. Merge readiness was 0.99 on the 0-to-3 scale, confidence 0.61, with probabilities blocked 0.20, not-ready 0.61, nearly-ready 0.19 and ready 0.00. This describes the unrequested remote delivery gates and unavailable cross-provider ADR review; it does not replace the independent local review or create a blocker to the authorized local endpoint. No merge readiness is claimed. The advisory was not rerun merely to replace its mobile-pending snapshot.


## Browser evidence and reproduction

Prerequisites are the installed repository dependencies and Playwright Chromium, `fnm` with Node 26.7.0 and repository-pinned pnpm 11.24.0, five free fixture ports (14801 through 14805), and a current executable built with the matching production WebUI assets and the `ui` tag. At report correction, `/tmp/hikyo-612-ui-e2e` still existed and its SHA256 was independently rechecked as `02d5088a3c6b75c99bbbabec78aebb4738143bc054fa0bddc0832ab3caf49d24`. That temporary path is not a durable artifact: the browser commands below require it to exist; do not run them against an absent or stale binary.

If the binary or matching assets are absent, rebuild from this checkout in the following order. These are the production WebUI build and the fixture's supported Go build with embedded assets:

```sh
cd /Users/developwent/.t3/worktrees/wenv/t3-c2352a7d/web
fnm exec --using=26.7.0 node --run build
cd /Users/developwent/.t3/worktrees/wenv/t3-c2352a7d
go build -tags ui -o /tmp/hikyo-612-ui-e2e ./cmd/hikyo
```

Both build commands must exit successfully. A rebuild after source or toolchain changes is new evidence and is not covered by the recorded binary hash. Before using the retained binary, check it explicitly; the first command must succeed and the second must print the SHA256 above:

```sh
test -x /tmp/hikyo-612-ui-e2e
shasum -a 256 /tmp/hikyo-612-ui-e2e
```

The final validated browser commands were run sequentially from `web/`. Desktop:

```sh
cd /Users/developwent/.t3/worktrees/wenv/t3-c2352a7d/web
HIKYO_E2E_BINARY=/tmp/hikyo-612-ui-e2e HIKYO_E2E_PORT=14801 HIKYO_E2E_PORT_B=14802 HIKYO_E2E_PORT_TLS=14803 HIKYO_E2E_PORT_OPERATIONAL=14804 HIKYO_E2E_PORT_OPERATIONAL_B=14805 fnm exec --using=26.7.0 pnpm exec playwright test --project=desktop login.spec.ts --grep 'CLI login handoff'
```

Mobile, after desktop finishes:

```sh
cd /Users/developwent/.t3/worktrees/wenv/t3-c2352a7d/web
HIKYO_E2E_BINARY=/tmp/hikyo-612-ui-e2e HIKYO_E2E_PORT=14801 HIKYO_E2E_PORT_B=14802 HIKYO_E2E_PORT_TLS=14803 HIKYO_E2E_PORT_OPERATIONAL=14804 HIKYO_E2E_PORT_OPERATIONAL_B=14805 fnm exec --using=26.7.0 pnpm exec playwright test --project=mobile login.spec.ts --grep 'CLI login handoff'
```

The expected result is eight passing scenarios per project. The repository fixture owns both server instances and their security-sensitive cookie/CSP behavior; these were disposable local fixtures, not a production deployment.

The real browser paths cover local password plus TOTP, virtual-authenticator passkey, signed-fixture OIDC loopback and redemption, and device approval with cumulative ten/fifteen-second slowdown and redemption. The browser OAuth2 case mocks only discovery/start to assert forced `sign-in`, hidden sign-up and preserved return after refusal. Real GitHub-profile start/callback, approval, redemption and provider invalidation run on both actual datastores in the service suite. These are separate evidence boundaries. No real external provider account or physical authenticator was used.

UI logs are `/tmp/hikyo-612-web-unit-node26-final.log`, `/tmp/hikyo-612-storybook-node26-final.log`, `/tmp/hikyo-612-web-build-node26-final.log`, `/tmp/hikyo-612-desktop-node26-final.log` and `/tmp/hikyo-612-mobile-node26-final.log`. Desktop/mobile screenshots in both themes are `/tmp/hikyo-612-cli-{desktop,mobile}-{dark,light}.png`; the offline HTML report embeds them. A browser fixture initially mixed builtin fetch with an Undici 8 agent; using one module's fetch, agent and Response type fixed its actual network regressions. Parallel per-instance compilation initially starved the startup health deadline; the final run used the fixture's existing supported prebuilt-binary path, not a relaxed timeout.

The final paired chat artifact is `/Users/developwent/.codex/artifacts/hikyo-612-20261015/implementation.md` and `implementation.html`. The source manifest is retained alongside as `review-r2-manifest.json`. The earlier assessment is explicitly marked superseded. No production state was changed.
