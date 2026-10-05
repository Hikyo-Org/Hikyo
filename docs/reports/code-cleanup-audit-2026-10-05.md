# Code cleanup audit, 2026-10-05

Audited commit: `5c3f969f692268e5c0bc7f5b9acc860f3bff0f48`.

**16 reviewed findings, including behavior defects, boundary hardening, and optional simplifications.** Three independent finder subagents inspected backend, frontend/SDK, and CLI/platform. A fourth skeptic checked remote routing, integer boundaries, error classification, and generated comparisons. At the user's request, a new independent verifier reviewed every finding, its counterarguments, and cleanup boundaries. The parent verified source, callers, relevant accepted ADRs, and selected executable probes.

This is the investigation snapshot at the audited commit. The subsequent authorized fixes and validation are recorded in the [implementation handoff](../handoff/code-cleanup-2026-10-05.md). Source links remain pinned to the pre-fix commit.

**Independent verification: 12 confirmed, 4 partially confirmed (F06, F08, F09, F15), none wholly disproven.** Partial findings were narrowed below. Confirmed static cleanup does not mean a runtime defect. Per-finding counterarguments, source evidence, and reproduction limits: [independent verification report](code-cleanup-verification-2026-10-05.md).

## Correctness and error handling

### F01, P2: Remote project-settings links select the home instance

Evidence: [HistoryDrawer policy link](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/routes/HistoryDrawer.tsx#L249), [Matrix empty-environment link](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/routes/Matrix.tsx#L1026), [route composition](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/app/App.tsx#L89), [project read hook](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/settings.ts#L95), [retention mutation](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/settings.ts#L787).

History preserves `?remote=`; Matrix drops it. Both navigate to `ProjectSettings`, which has no `WorkspaceScope`. Its project/retention/environment hooks use the home singleton client. The query parameter alone cannot change transport. Distinct remote IDs yield missing-project errors; matching IDs, such as after cloning a backup, plus home authorization can expose local settings and allow local changes.

**Cleanup:** In remote context, link to the settings page on `workspace.origin` using an ordinary anchor. Keep local router links for local context. An intentional extension of embedded remote administration requires provider-aware settings hooks, cache ownership, navigation, and session boundaries; wrapping the component alone is insufficient. The multi-instance ADR does not forbid future administration, but current routing does not implement it.

**Validation:** Click both links from an actual remote workspace. Assert destination origin and no home project API reads/writes. Include a fixture where both instances have identical IDs. Existing local href assertions do not cover this.

### F02, P2: Nightly Apply fetches payload before authentication, then fetches it twice

Evidence: [Apply download and prerelease dispatch](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/selfupdate/selfupdate.go#L122), [metadata-first nightly path](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/selfupdate/nightly_download.go#L54), [existing PrepareNightly test](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/selfupdate/nightly_download_test.go#L98).

`Apply` fetches checksums and the native archive before its prerelease branch. The nightly preparation path then authenticates metadata and downloads the same archive again. The first archive is discarded.

**Reproduced:** An invalid manifest signature caused one native archive download before refusal. A successful nightly Apply caused two downloads of the same archive. Authentication still prevents installation of the invalid release; this finding concerns ordering, wasted transfer, and bypass of the intended metadata-first download policy.

**Cleanup:** Dispatch prerelease processing before the common stable checksum/archive path. Keep executable replaceability checks before any state-writing download.

**Validation:** Exercise `Apply`, asserting zero executable downloads on invalid signature, one native archive on valid input, and no second archive request from cached preparation. Preserve stable-channel verification and Linux staging behavior.

### F03, P2: Importer scrub retains mixed-case Hikyo credential variables

Evidence: [Stripped predicate](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/importer/spawn.go#L200), [CLI normalized predicate](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/cli/compose_run.go#L481), [import subprocess contract](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/docs/adr/import-paths.md#L89), [Windows release target](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/.goreleaser.yaml#L29).

Importer uses case-sensitive `HasPrefix(name, "HIKYO_")`; CLI normalizes case. Both `SanitizedEnv` and `WithSanitized` depend on the importer predicate. Windows treats environment names case-insensitively, so mixed-case ambient credentials remain available to connector subprocesses.

**Reproduced:** Synthetic `hikyo_token` and `HiKyO_ROOT_KEY` survived direct filtering; a synthetic process `hikyo_token` survived `WithSanitized`. This probe ran on macOS; Windows runtime was not exercised.

**Cleanup:** Normalize namespace matching centrally while retaining original names and values for restoration. Keep non-Hikyo connector credentials intact.

**Validation:** Uppercase, lowercase, mixed-case, unrelated variables, and restoration after errors/panics. Add a Windows execution case.

### F04, P2: JSON decoding loses int64 precision before bigint validation

Evidence: [HTTP JSON decoder](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/clients/ts/src/generated/client/client.gen.ts#L159), [generated bigint coercion](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/clients/ts/src/generated/zod.gen.ts#L398), [SSE JSON decoder](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/clients/ts/src/generated/core/serverSentEvents.gen.ts#L189), [generator configuration](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/clients/ts/openapi-ts.config.ts#L13), [incorrect no-loss comment](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/history.ts#L109).

Generated HTTP/SSE code uses standard JSON parsing. Large numeric literals become rounded JavaScript numbers before `z.coerce.bigint()` sees them. Converting that number to bigint preserves the rounded value, not the original wire integer.

**Executed numeric proof:**
```text
wire revision:       9007199254740993
JSON.parse revision: 9007199254740992
BigInt(parsed):      9007199254740992
```

This is a boundary defect, not evidence of a current large-ID production incident. The independent verifier executed the current generated HTTP and SSE decoders with the raw numeric literal; both returned the rounded value. The parent reran this probe with the same result. Generated-Zod execution remains unavailable because its dependency is absent; the schema source confirms downstream bigint coercion.

**Cleanup:** At the owned generator/runtime boundary, preserve integer literals exactly or reject unsafe numeric inputs before accepting bigint fields. Cover HTTP and SSE, then regenerate. Do not hand-edit generated files. Correct the history comment.

**Validation:** Raw wire JSON around `2^53` and int64 endpoints through actual runtime plus generated schema: exact bigint or explicit refusal. Test ordinary numeric fields independently.

### F05, P2: Adapter requests convert bigint identifiers without safe-range checks

Evidence: [update generation conversion](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/adapters.ts#L297), [adoption conversions](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/adapters.ts#L365), [checked history conversion](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/history.ts#L114), [checked self-configuration conversion](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/selfConfig.ts#L105).

Update/adopt wrappers call `Number()` on expected generation, target generation, destination ID, and repository ID. `Number(9007199254740993n)` becomes `9007199254740992`. Other features already refuse unsafe conversion.

**Cleanup:** Reuse a shared checked bigint-to-request-number conversion with field-appropriate minimums. This cannot recover response precision already lost in F04; address both boundaries.

**Validation:** Unsafe inputs refuse before fetch; safe limits retain exact payloads. The server rejects mismatched update generations (`internal/service/adapters.go:781`) and compares adoption generation/repository/destination before adoption writes (`:1950`). Precision-dependent refusal is supported; wrong-destination adoption is not demonstrated. Very large generations are also unlikely in normal operation.

### F06, P3: Certificate views silently ignore invalid stored SAN JSON

Evidence: [view decoder](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/pki_certificates.go#L95), [renewal decoder](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/pki_certificates.go#L567).

`pkiCertificateView` discards the result of `json.Unmarshal`. List/show callers receive empty or partially decoded SAN fields. The normal renewal path propagates the same decoding error; its already-renewed successor branch also uses the permissive view helper. Error handling therefore depends on the path taken through the same record.

**Reproduced:** A direct view-helper probe supplied `{"dns":["kept.example"],"ip":42}` and received the DNS value with an empty IP list. This did not exercise a corrupt row through the database/API. Normal issuance uses `encodeSANs`, which produces valid JSON. This is corruption hardening, not a demonstrated defect in normal issuance. The trigger requires corrupt or invalid persisted metadata.

**Cleanup:** Decode through an error-returning helper and propagate failures through view callers. Plan error handling around issuance carefully so a new post-commit formatting error does not discard a display-once private key.

**Validation:** Invalid JSON and incorrect field types fail loudly in list/show; normal issue/list/show/renew and display-once custody continue to work.

### F07, P2: Bundle operations call contract errors connection failures

Evidence: [bundle classifier](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/definitions-bundle.ts#L168), [shared transport classifier](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/client.ts#L325), [check/plan error display](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/routes/DefinitionsBundlePanel.tsx#L115), [apply error display](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/routes/DefinitionsBundlePanel.tsx#L223).

Every non-`ApiError`, including response-schema errors and unexpected success statuses, produces “Check your connection and try again.” A mutation can have completed before response parsing fails. Current wording cannot distinguish that uncertainty from a network failure.

**Cleanup:** Retain feature-specific domain refusals; use `transportRefusalText` for generic transport/contract failures. After an uncertain apply result, instruct status refresh before retry. File-input errors have separate handling and should retain their own guidance.

**Validation:** Network rejection, malformed 200 response, stale-plan 409, and invalid post-apply response have distinct wording. No automatic replay.

## Same-source and statically redundant checks

### F08, P3: Contract tests compare generated metadata with its own source

Evidence: [route/class comparisons](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/isolation/contract_test.go#L43), [route-primary inclusion](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/isolation/contract_test.go#L129), [wire generation](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/authz/gen/main.go#L402), [actual router walk](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/isolation/invariants_test.go#L43), [accepted cleanup decision](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/docs/adr/generated-boundaries.md#L110).

`api.Operations()` owns OpenAPI routes, classes, and primary operations. `renderWire` generates these same facts into the wire registry. The tests compare these generated facts with their source, rather than observing the router.

They can detect stale output or a generator bug, but dedicated generation freshness and negative fixtures already own that duty. Their names/comments overstate independent runtime coverage.

**Cleanup:** Remove redundant route/class comparisons and primary-inclusion check, or explicitly classify any retained check as generation validation. Preserve independent operation-formula comparison, machine eligibility, MFA policy checks, audit invariants, and `TestInvariant01ClassificationTotality`, which walks both actual routers.

**Validation:** Generation freshness and negative fixtures remain green. A removed/added runtime route still fails actual-router coverage.

### F09, P3: VerifyEvent repeats a condition already guaranteed by Verify

Evidence: [canonical proof check](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/authz/verify.go#L33), [duplicate check](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/authz/verify.go#L80).

A successful `Verify(p, op, tok)` guarantees the local interface contains a nonnil canonical `*proof`. Immediately repeating `!ok || c == nil` cannot select its error branch.

**Disposition:** Optional simplification, not a runtime or authorization defect. A local fail-closed guard can be valuable defensive documentation. Removing it is justified only if the replacement retains one clear canonical-proof boundary.

**Cleanup:** Keep one authoritative boundary check. A private helper returning the verified canonical proof and scope avoids both duplicate checking and a second assertion. Preserve event authorization after verification.

**Validation:** Nil, forged, foreign-transaction, ended-transaction, operation mismatch, and unauthorized audit-event cases remain refused.

### F10, P3: Bodyless transport contains an impossible whoami exemption

Evidence: [bodyless transport](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/client.ts#L297), [identity comparison](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/web/src/api/client.ts#L77), [body-bearing whoami descriptor](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/clients/ts/src/generated/operations.gen.ts#L1472), [private descriptor brands](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/clients/ts/src/operationsBuilder.ts#L142).

`ok()` accepts branded `BodylessOperation`; `whoamiOp` is a differently branded `BodyOperation`. Reference comparison with whoami is always false for a valid argument. Consequently its conditional epoch capture, conditional reconciliation, and identity exemption in 401 handling add impossible paths.

**Cleanup:** Capture/reconcile epoch unconditionally in `ok`; remove its identity exemption. Keep whoami exemptions in body-bearing `parsed` and `parsedPick`.

**Validation:** Bodyless session-epoch tests and compile-time rejection of passing whoami to `ok`. Preserve remote-client refusal behavior.

## Dead code and legacy residue

### F11, P3: SCIM carries unused authority-change accounting and stale policy comments

Evidence: [dead method](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/scim_origin_release.go#L111), [dead counter](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/scim_origin_release.go#L91), [counter increment](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/scim_origin_release.go#L279), [stale algorithm header](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/scim_origin_release.go#L18), [operative SCIM amendment](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/docs/adr/scim-provisioning.md#L3).

Whole-repository search finds no `AuthorityChanged()` callers/tests. `RowsRevoked` is read only by that method. The header still claims session-generation advances; the operative October 1 amendment explicitly preserves instance-wide sessions during SCIM org changes.

**Cleanup:** Delete method, counter, increment; rewrite stale comments around current org-scoped authorization. Do not reintroduce session-generation bumps.

**Validation:** Preserve `TestSCIMOrgChangesPreserveUnrelatedOrgSession`, truthful audit payloads, and lockout-retention behavior.

### F12, P3: SCIM payload's revoked argument does nothing

Evidence: [callers](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/scim_origin_release.go#L344), [unused parameter](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/scim_origin_release.go#L367), [discard statement](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/scim_origin_release.go#L392).

Two callers pass false/true, but `scimGrantPayload` only uses `revoked` in `_ = revoked`. Actual revoked-event differences are added by `grantRevokedEvent`.

**Cleanup:** Remove argument and discard statement. Keep event-specific `origins_remaining` and `sessions_revoked` fields at their existing owner.

**Validation:** Existing SCIM event-payload checks; no new test merely mirroring parameter deletion.

### F13, P3: Public router takes an unused readiness checker

Evidence: [public constructor](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/server/server.go#L96), [production allocation](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/app/generation.go#L420).

`NewPublic` never reads `ready`; `New` only forwards it. Production constructs a `service.System` for this discarded argument. Operational readiness has its own live checker.

**Cleanup:** Remove stale argument and update callers. Keep `NewOperational` readiness and the public/operational listener separation.

**Validation:** Router and runtime-status construction, operational health/readiness, and public absence of operational endpoints.

### F14, P3: BootstrapPending has no caller

Evidence: [unused service entry point](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/bootstrap.go#L224).

Whole-repository search finds only its declaration/comment. The comment promises CLI use that does not exist. This is an internal service API, not an external module compatibility obligation.

**Cleanup:** Remove the unused method. Preserve the actual bootstrap flow and its live account-count guards; do not delete shared generated queries solely because this wrapper disappears.

**Validation:** Bootstrap flows and package compilation.

### F15, P3: Retired updater keeps executable configuration and unread callbacks

Evidence: [retired execution config](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/updater/updater.go#L100), [unused executor fields](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/updater/updater.go#L200), [unused control-server fields](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/updater/protocol.go#L34), [refusal entry point](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/updater/serve.go#L12), [boot refusal](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/app/app.go#L324).

The retired configuration entry point `LoadConfig` has only test callers. It still calls `Config.Validate`, which calls `Command.validate`; those helpers are therefore transitively test-reachable, not literally caller-free. `Executor.Runner/Progress` and `ControlServer.Executor/Log/Context` are never read. Production refuses updater startup and does not wire a control executor.

**Cleanup:** Remove unread fields and retired executable configuration after reducing refusal tests to actual refusal behavior. Preserve explicit refusal entrypoints, historical Job/Backend/State/Phase JSON shapes, historical journal reads/acknowledgements, and authenticated API refusal behavior. These are explicitly retained compatibility interfaces/tests, rather than a currently wired production helper. `docs/handoff/638-legacy-updater-retirement.md:23-33` records that retention. Do not remove the entire package, `Executor.Config.Backend`, or the direct command-refusal door.

**Validation:** Retirement still starts no process, mutates no legacy job, and opens no helper socket; historical formats remain readable.

### F16, P3: Internal helpers carry unused policy/context arguments

Evidence:
- [liveChallenge](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/login_challenge.go#L137): unused `ctx`, `az`.
- [stageResetRefusal](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/credential_reset.go#L164): unused `now`.
- [LowerEffectiveWindow](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/reauth.go#L317): unused `now`.
- [leaseDisclosureGate](https://github.com/Hikyo-Org/Hikyo/blob/5c3f969f692268e5c0bc7f5b9acc860f3bff0f48/internal/service/dynamic.go#L600): unused `projectScope`.

**Cleanup:** Remove only unused inputs from owning helpers and callers. Avoid broader policy restructuring. Retain the time/context/authorization values still consumed by surrounding operations.

**Validation:** Compilation plus existing challenge, reset, effective-window, and lease mint/renew regressions. No tests needed merely to verify shorter signatures.

## Recommended cleanup order

1. Correct remote settings ownership and importer credential scrubbing.
2. Correct nightly download ordering and shared integer boundaries.
3. Correct SAN decoding and uncertain-operation error messages.
4. Remove tautologies and dead internals with behavior-preserving scoped changes.

Generated outputs remain generator-owned. Migration/historical-schema compatibility, private resolver allowlists, named prototype branches, and intentionally local-only admin surfaces are not blanket removal targets.

## Verification performed

| Check | Result |
|---|---|
| `go test ./internal/authz ./internal/authz/gen -run 'TestVerify\|TestSystemProof\|TestWire' -count=1` | Passed |
| `go run ./internal/authz/gen -check` | Passed, no output writes |
| `go test ./internal/updater ./internal/importer -run 'TestExecutorRefuses\|TestDirectLegacy\|TestSubprocessEnvironment' -count=1` | Passed |
| Temporary Go-overlay probes: nightly Apply, importer scrub, malformed SAN view | Passed by observing the defects |
| Current generated HTTP/SSE decoders plus bigint request conversion | Demonstrated rounding; rerun by independent verifier and parent |

Original local diagnostic overlays observed defective behavior. They were temporary investigation tools, not fix regressions. The implementation handoff identifies the committed regression tests that replace them.

Scope limits: broad source/caller audit with selective deep traces, not exhaustive proof of absence of dead code. No whole-repository suite, browser/two-instance reproduction, or Windows runtime test ran. Frontend/SDK dependencies are absent, so no component runner or generated-Zod runtime assertion ran. Actual generated HTTP/SSE decoding was reproduced through native Node, then checked against generated schema source.

Graph navigation was rebuilt at the audited commit: 52,211 nodes, 145,520 edges. The extractor skipped 68 unsupported files and reported 17 Astro parser gaps. Graph edges were navigation only; findings were checked against source.

## Candidates not promoted

Windows importer starts a process before assigning its job object; source documents an escape window. Whole-tree deadline behavior needs a Windows descendant-process reproduction before a firm severity claim.

`ParseProviderRef`/`ResolveProviderRef` have test-only code callers, but a named migration handoff/spec requires checking the intended CLI adoption before removal. A test-only SQLite restore-control wrapper can move into test code; its engine validation should remain.

Story-only UI components, unused breakpoint metadata, and explicit prototype branches lacked enough evidence to call them defective production behavior.
