# Self-configuration implementation and report validation

## Current validation status, 2026-09-06

**Implementation and local verification are complete.** All 53 server inputs have managed consumers. The full fresh Kubernetes run passed without resume on signed runtime source `c40833b958ae34030e41814c53eebaa3629610aa`, ending at generation 19. Exact-head CI is tracked in [draft PR #686](https://github.com/Hikyo-Org/Hikyo/pull/686). No merge or production deployment is authorized or claimed.

The [expanded runtime evidence](validation/controlled-rollout-expanded-kind.json) and [795-file compiled/embed manifest](validation/controlled-rollout-expanded-source-manifest.json) record live reload, DB/root replacement, exact application acknowledgement, unresolved root-finalization refusal, actual five-minute transport expiry/renewal, Restore/fresh repair/later Apply, candidate-root select/clear, singleton HA/node transitions, all seven upgrade inputs, and independent-session disclosure narrowing. The fresh fixture completed every stage without continuation or replay. Final server, executor and PostgreSQL container restart counts were zero before their owned workloads were stopped. Private custody and logs were retained; report servers were left running.

- Linux candidate and final running ELF: `eafed43e5162715e1e435a27cc5f3dfa1e0ecc92705fb86374eb61810675f281`.
- Native operator candidate: `e783b14102e7b87f7d95bdaea6f508ebd1647e4aac5244946cde5cedf2a0c9bf`.
- Compiled source manifest: `e64b464a9b375afbae05f6a4ea342c91023a116bdeafc5a6f0b3ae2733f3f766`. Parent independently reconstructed the native/Linux go-list union and verified all 795 current bytes.
- Fresh-start and completion harness: `8bdce7cc0aa8c47ad05d41fa860f3eca63e18fa1871b0413497fa2eb73aeeb8d`. Both binaries contain the same seven signed linker inputs; all chart inputs stayed unchanged.
- Sanitized evidence JSON: `e2e4fecf6b75971f08f7bebd9fe9a9672b1fe6e3fd280c454c7bad6e85bb4ad5`. Both artifacts passed a recursive 3224-string scan against secret markers and exact private fixture values before publication.

Actual command sequence 6 to 7 preserved MFA count 6. The real native drill recovered existing wrappers and a populated secret, then reconciled, minted and revoked a credential. It used eight isolated opaque TCP streams with PostgreSQL `verify-full` and zero remaining child processes. All seven upgrade inputs changed; both custody paths resolved to device/inode `65025:3200808`. Session B applied policy zero while session A retained its original live positive window; A then received 403 before expiry. The fixture uses temporary test signing authority, not production release authority. It proves same-datastore aliases and same-build upgrade-profile selection, not a database migration, new release/schema upgrade, executor process takeover or multi-replica failover.

Combined full backend packages, web typecheck/934 tests/build, scoped races, isolation, generated SQL and live admission checks passed as recorded below. CI on `2450a34a` passed all browser/closure/isolation checks and full service race in 1192.522 seconds under its unchanged 1200-second limit. Its two failures are corrected: [strict config leaf preservation](validation/config-leaf-boundary.json), signed `c40833b9`, and [owned HTTP fixture cleanup](validation/ha-http-cleanup.json), signed `29e76d3c`. The latter reproduced the same teardown error locally, then passed three fixtures five times under race in 63.398 seconds. Server deadlines and assertions remain unchanged. Source and final harness reviews are CLEAN. See the PR for final-head CI.

Final public docs check passed with zero errors, warnings or hints; build produced 45 pages and a 104-file PWA precache. Final report browser checks verified 34 decisions, five explicit approvals and 53 server rows, valid fragments, no remote assets and no overflow with all disclosures open at 320/1280 pixels. LAN HTTP 200 responses for the report and both runtime artifacts matched local bytes. Report HTML is 392047 bytes, SHA-256 `d4cdef186974fd4ed7e418ffca5fe822381317f75976d6a62263a062d785ca7a`. [Final report metadata](validation/final-report.json) records this host-origin check and its limits. JavaScript/generated inline script and Python syntax checks passed; no configured Python lint/type suite applies to the harness.

Final full-run log: `/tmp/hikyo-rollout-final-expanded-proof.log`; candidate: `/tmp/hikyo-rollout-final-candidate-818e`. Earlier fixture failures below are chronological evidence, not the final result. They led to corrected API/session/key-file/transport handling. One overlapping historical fixture had readiness/liveness stalls and container restarts; its later unchanged-process assertion was correctly refused. The final fixture ran alone and had no restarts. No deadline or authorization rule was weakened.

## Final CI service scheduling correction, 2026-09-06

CI on `6f564fc3` passed all CodeQL, browser, isolation, Kubernetes, core and app race checks. The service race package hit its unchanged 1200-second cumulative deadline during fixture preparation. A reviewed test-only correction adds 35 parallel declarations across 11 files: 33 top-level tests and independently owned origin/reauth subcases. Assertions, database engines, fresh signed admission, unique roots and deadlines are unchanged. Identical five-group/53-case race runs improved from 171.237 to 100.961 seconds (41.04%). The full 212-test service package then passed in one uninterrupted race invocation in 617.700 seconds, with PostgreSQL configured, two Go scheduler threads, two parallel tests and no failures or skips. All 795 compiled application inputs still match the fresh Kubernetes proof. See [service scheduling evidence](validation/service-race-scheduling.json) and the PR for replacement-head CI.

## Historical expansion checkpoint, 2026-09-06

**Signed checkpoint `0cb6de70` is pushed to draft PR #686. Later reviewed slices are integrating locally; final exact-head CI, merge and production rollout remain unclaimed.** The current catalogue contains 27 top-level keys: nine original, 16 owner settings, secret node overrides and external bootstrap aliases. Trusted proxy policy is exclusively node-local. The 65-input inventory is metadata and does not establish every activation consumer.

| Completed local check | Evidence and limit |
| --- | --- |
| Live owner/node consumers | Real HTTP/MCP, authentication/admission capacity, backup destination, listener movement and drain, TLS rotation/pair refusal/plain-TLS transition, immutable imports, same-DB pool replacement and fenced missing-node repair. Focused app race run passed in 85.788 s. |
| Full app regression after upgrade projection fixes | Passed in 127.714 s against manifest 136. Log: `/tmp/hikyo-upgrade-managed-app-manifest136.log`. Later root/proxy/setup changes still need their own validation. |
| Origin transition | Both-engine tests passed in 57.573 s; focused race passed in 145.042 s. Exact TOTP/current-password recovery checks do not prove target DNS/TLS/login. |
| Deployment Restore | Both-engine exact-MFA/CAS/replay/cancel/fence checks passed in 5.438 s; Restore then fresh repair then next plan workflow passed in 5.419 s; focused race passed in 20.691 s and vet passed. Controller probe tests do not prove a real Kubernetes deployment. |
| Browser code checks | 932 web tests across 107 files passed in 14.66 s, typecheck and production build passed. Logs: `/tmp/hikyo-selfconfig-web-restore-tests.log`, `/tmp/hikyo-selfconfig-web-restore-build.log`. That run preceded the separately recorded expanded desktop proof below. |

Prepared review has a five-minute lease; individual synchronous requests wait at most 30 seconds and participant/worker evidence must be at most 30 seconds old. Final Apply binds exact plan/revision/generation/owner/incarnation and consumes fresh passkey/TOTP evidence once. Deployment Restore has a distinct exact MFA action, leaves the desired revision unchanged and keeps business fenced until a separately authorized repair applies. No automatic root finalization exists.

Concrete bootstrap scope is explicitly enrolled singleton non-HA Recreate Kubernetes 1.36 only. Real enrollment, admission denial, source CAS, replacement boot/template acknowledgement and restoration now pass the exact-source Kubernetes fixture described below. Development controls and the next-root candidate selector are integrated and pass full affected-package validation. The seven upgrade startup inputs are integrated after final review; full running-server proof remains pending. Singleton topology/identity transitions are in review. Multi-member HA bootstrap remains outside this provider slice. Host/GitOps providers and true datastore migration are separately scoped capabilities, not additional environment-variable families. No broad complete-server-variable claim is justified yet.

Sealed setup seed correction has completed local evidence: service race passed in 35.027 s and app tests in 16.236 s. Both-engine checks cover large mail/TLS envelopes, exact owner/node import and MFA/network authority refusal; real app tests keep the running listener after host admin creation. R1 found final host-discovery and stale pre-lock timestamp issues. Both are fixed: the transient discovery mode reaches final membership-locked verification, which reads a fresh decision clock. Both-engine regressions passed in 5.308 s and R2 review is clean for those fixes. Durable authorized deployment command renewal and no-effect restoration now pass R2 review and race checks: module 4.877 s, service 111.817 s, app 17.108 s. Full lint passed in 78.122 s. Actual Kubernetes held-response expiry and recovery now pass as described below; executor process outage and takeover remain unproved.

Latest local package evidence: 934 web tests, web typecheck and production build pass. Generated SDK tests pass 20/20 (`/tmp/hikyo-selfconfig-final-sdk-tests.log`). Full app and server packages passed in 100.472 s and 64.378 s before the small final seed-store correction. The expanded desktop rerun passed after the fixture was taught to scroll virtualized matrix rows. Desktop and mobile channel-Apply journeys and the exact-source real controlled-rollout chain below pass; no expanded exact-head CI result is claimed.

## Checkpoint CI repairs and D34 integration, 2026-09-06

Exact checkpoint `0cb6de707439fcbc56e0c0844fbd505d1a05f60b`, [run 34046700568](https://github.com/Hikyo-Org/Hikyo/actions/runs/34046700568), exposed three fixture defects. No runtime permission check, assertion, coverage or timeout limit was relaxed.

- PostgreSQL source privilege test created a LOGIN role without a password. CI requires SCRAM; local trust authentication concealed the defect. A dedicated PostgreSQL 18 SCRAM fixture reproduced the original failure, then passed all `TestPostgresSourceProof` cases in 10.253 s and the affected race test in 8.896 s after a private random role credential was added. Grants remain unchanged. Logs: `/tmp/hikyo-source-role-scram-{red,green,race}.log`.
- Host admin creation commits managed generation 1 before the running worker adopts it. Three real-server reproductions showed health 200 while readiness/business returned 503 for 1840, 1878 and 1905 ms. The browser fixture now waits on generation-aware readiness after admin creation, with the existing 30-second bound. Only 503 is pending. Full affected mobile shard passes 28 tests in 4.8 minutes; web typecheck and 934 tests pass. Log: `/tmp/hikyo-ci-readiness-mobile4-proof.log`.
- Desktop/mobile design-token checks selected every code label in the configuration owner panel. The expanded panel has two. The fixture now identifies the actual Owner paragraph before checking its identifier font. Targeted dark/light checks pass 2/2 on desktop (3.4/3.0 s; total 2.3 min) and 2/2 on mobile (3.1/2.3 s; total 2.6 min). Final web typecheck and all 934 tests pass in 17.85 s. The three CI fixtures are saved in signed DCO commit `8f93fb50`.

D34 final review is CLEAN. Parent compared all 27 exported D34 files against the reviewed worktree after integration; no byte drift. The final controlled-rollout UI copy passes typecheck, 934 tests and production build (`/tmp/hikyo-topology-upgrade-web-final.log`). The integrated patch is SHA-256 `7d244e291500e1b25c339c365e6108e9eab22148be1775a5ec1fc0078f53e76d`. It selects all seven upgrade startup inputs through an enrolled profile. Pure preflight binds immutable artifacts; current build, control state and custody are separately rechecked before submit. Replacement resolves the selected profile before the existing authoritative gate. Initial-profile Restore remains possible; no migration, operator rotation or evidence consumption occurs during Prepare.

Full affected config/configrollout/upgradebundle/backupreceipt suites pass; app bootstrap/next-root/candidate regressions pass in 13.282 s and service deployment/Restore/exact-MFA checks in 20.755 s. Both-engine race checks pass: configrollout 2.291 s, backupreceipt 1.696 s, upgradegate 35.355 s, app 2.454 s and service 10.382 s. Scoped vet passes; full internal lint passes in 198.719 s. Logs: `/tmp/hikyo-d34-regression.log` and `/tmp/hikyo-d34-race-lint.log`.

Real Kubernetes admission accepts coherent profiles and refuses mixed tuples, undeclared sources and image/mount/PVC changes. A real Pod proves the alternate custody mount names the same device/inode and supports bidirectional writes. This policy/mount proof is distinct from a full running-server Apply/Restore proof, which is pending on integrated source. Log: `/tmp/hikyo-d34-chart-live-mount-proof.log`.

## Expanded desktop browser evidence

Freshly compiled Go instances passed two desktop Playwright flows: local passkey Apply in 7.6 s and independent-owner TOTP Apply in 6.4 s; the full run, including fixtures, took 3.2 minutes. Log: `/tmp/hikyo-selfconfig-rollout-desktop-scrolled.log`. The run was filtered, so it does not establish full flow-registry closure.

The journeys navigate and edit the expanded catalogue, publish without changing the active generation, prepare before exact MFA, apply a new update channel and observe actual owner-local generation/channel changes. Applying owner B leaves owner A's revision, generation and channel unchanged. The local flow checks serious accessibility violations and page overflow. They do not change all new owner/node settings or execute an actual Kubernetes rollout.

Screenshots were copied before the mobile run could overwrite test results: [local passkey Apply](validation/managed-configuration-expanded-desktop-applied.png), [node acknowledgement](validation/managed-configuration-expanded-desktop-nodes.png), [independent-owner TOTP Apply](validation/managed-configuration-expanded-desktop-independent-owner.png). These images are explicitly current expanded-desktop evidence; earlier screenshots below remain historical. Mobile equivalents also passed against the actual Go fixtures: local passkey 8.2 s and independent-owner TOTP 7.4 s, total 3.2 minutes. Log: `/tmp/hikyo-selfconfig-rollout-mobile.log`. Preserved [mobile passkey Apply](validation/managed-configuration-expanded-mobile-applied.png), [mobile node acknowledgement](validation/managed-configuration-expanded-mobile-nodes.png) and [mobile independent owner](validation/managed-configuration-expanded-mobile-independent-owner.png). These filtered journeys have the same limited setting and deployment scope as the desktop proof.

Latest serial Go package checks passed: config 0.695 s, runtime configuration 0.360 s, crypto 0.383 s, store 37.031 s, service 114.142 s, app 90.759 s, server 63.187 s, configuration rollout 0.890 s and lint 35.491 s. The earlier isolation failure identified outdated fixture defaults, pins and audit expectations. Those corrections now pass focused checks; all 369 isolation cases now pass without coverage or assertion changes. Log: `/tmp/hikyo-selfconfig-final-sequential.log`.

Compose and retention CLI isolation fixtures now use production audit defaults (90/365 days) and a 30-minute RTO. All TestComposeCLI and TestRetentionCLIStartupSweep cases passed on both engines in 59.640 s (`/tmp/hikyo-selfconfig-isolation-cli-final.log`). Full TestAuditCore and invariants 06/11/13 passed on SQLite/PostgreSQL in 35.458 s (`/tmp/hikyo-isolation-pins-audit-full-core.log`). Independent R1 review is CLEAN: every shared site is checked against explicit pins, seed authority rejects network/generic minting, and Restore audit rows come from real service authorization with refusal/idempotency coverage. All 369 isolation cases pass. Original planner shards 0 and 1 passed in 321.622 s and 457.832 s. The third reached its cumulative timeout without an assertion failure; its exact 134 cases were partitioned into three local groups, which passed in 229.973 s, 123.820 s and 176.508 s. No timeout, assertion or coverage requirement changed. Original logs: `/tmp/hikyo-selfconfig-isolation-shard-{0,1,2}.log`.

Additional affected package checks passed: command 1.291 s, admission 0.427 s, authorization 0.415 s, store upgrade 40.423 s and upgrade gate 75.608 s (`/tmp/hikyo-selfconfig-final-boundaries.log`). Final affected-package vet passed (`/tmp/hikyo-selfconfig-final-vet.log`).

Root finalization now locks the configuration binding before the key hierarchy, preserving both wrappers while the current generation/incarnation has an unresolved applying, partial, applied or superseded deployment. Restore and repair preparation retain the guard until the replacement generation is committed; finalization racing Apply invalidates its stale dual-wrap proof. SQLite/PostgreSQL rollout regressions passed in 15.885 s and existing key-rotation invariants passed in 2.166 s; R3 review is CLEAN. Logs: `/tmp/hikyo-root-guard-r3-focused.log` and `/tmp/hikyo-root-guard-r3-store.log`. Formatting and diff checks passed. No automatic root finalization is performed.

## Exact-source real Kubernetes controlled-rollout evidence

The complete owned-kind fixture passes against Kubernetes 1.36.1 using the production-mode server and executor, signed by an ephemeral test release authority. This is not a production release signature. [Metadata-only result](validation/controlled-rollout-kind.json) and [compiled source/embed manifest](validation/controlled-rollout-source-manifest.json) bind the run to the dirty worktree based on `907b162957441009f720c7fa4f3f76c573818a54`. The candidate binary and the final running process executable both hash to `ff5ed33f9fe084e1df154668edaad2bb35f98858bb7a6400af8058f636f949d8`. All 781 compiled source/embed inputs remained byte-identical through the build and completed proof. Later source changes require their own validation.

| Real fixture action | Observed result |
| --- | --- |
| Normal host admin setup, password login and TOTP enrollment | Protected managed profile and real human session created through supported interfaces; no authentication rows seeded directly. |
| Ordinary `HIKYO_UPDATE_CHANNEL` Apply, `stable` to `off` | Actual update consumer reports `off`; Pod UID, container ID, host PID and process start ticks remain identical. |
| Fresh exact TOTP authorization of DB and root aliases | Replacement Pod opens the same PostgreSQL store through the new alias and distinct root key, records the exact application acknowledgement, and accepts a later TOTP login. |
| Held executor response and root verification/finalization | Epoch-2 verification succeeds; finalization returns 409 conflict and preserves epochs 1 and 2 while acknowledgement is unresolved. |
| Real five-minute signed-command expiry during held response | Submit sequence advances 6 to 7; owner, revision, generation and the entire authorized command remain unchanged except sequence and timestamps. Reauthentication count remains 6. Releasing the hold completes the same job. |
| Restore, separate repair and later bootstrap Apply | Fresh exact TOTP Restore restores the previous deployment and replaces the candidate Pod while generation 5 remains partial and business requests return 503 `service_unavailable`. Separately published, freshly authorized repair applies generation 6 on the restored process. A later freshly authorized bootstrap Apply replaces the Pod and completes generation 7. |

The real chain exposed a legitimate Deployment status resource-version change between Prepare and Submit. The fix accepts that change only while UID, full prepared spec, all other deployment metadata and every module UID/resource-version remain exact. A final metadata recheck and atomic write against the newly read resource version remain enforced. Status-only positive and metadata/spec/module negative regressions pass; configuration-rollout race checks passed in 4.052 s and vet passed. App bootstrap integration passed in 6.392 s and service deployment/Restore integration passed in 54.511 s. Independent R2 review is CLEAN, and the final real Restore/repair/later-Apply chain exercises the fix.

The chart also now gives cluster-scoped admission policies namespace-qualified identities. Rendering the same release in two namespaces checks disjoint policy names and exact binding references. Full chart validation and the live admission/RBAC matrix pass, including accepted declared DB/root aliases and refused image, service-account, raw-root, undeclared-root, stale-custody and identity changes. Executor/server root reads and server Deployment writes remain denied.

Reproduction instructions: [owned-kind harness](../../../scripts/ci/CONFIG_ROLLOUT_KIND.md). Successful private log: `/tmp/hikyo-rollout-reviewed-source-proof.log`. The retained fixture is namespace `hikyo-rollout-0520bd8e` in explicit context `kind-hikyo-rollout-818e`; no production resource was used. Exported evidence contains no credentials, root bytes or database locators.

Scope remains singleton Recreate, same-store DB alias changes and real API/CLI human authentication ceremonies. Controlled-rollout clicks were not browser-driven. A held transport response and real expiry are proven; executor process outage/takeover, HA bootstrap, host/GitOps providers and true database migration are not. No merge, production rollout or expanded exact-head CI is implied, and no automatic root finalization occurs.

## Current report and documentation checks

The rebuilt standalone report returned LAN HTTP 200 from <http://192.168.0.30:8769/> and matched the local file byte-for-byte: 328962 bytes; SHA-256 `b9127fc709d956b722d9990cf152eef5357485e4600f4f3a4af631cb4eef7d1c`. Two consecutive builds were identical; both JavaScript source syntax checks passed. The regenerated inventory contains 65 inputs and 16 owner application-reload entries.

T3 preview checks found all 30 decisions, no broken internal fragments and no external resource requests. With every disclosure open, document width equals viewport width at 320, 390 and 1280 pixels. The 320-pixel check found and corrected a long setting-name overflow in D13. Tables retain their own horizontal scroll containers. These checks verify the report, not product browser acceptance or a separate physical LAN device.

Docs `pnpm run check` passed with zero errors, warnings or hints; `pnpm run build` generated 45 pages and 104 PWA precache entries. Both managed-configuration and configuration-rollouts pages are generated. No production docs deployment is implied.

## Historical nine-key and earlier assurance evidence

Everything below records earlier checkpoints. Their results remain useful for their tested revision and scope, but are not cumulative proof of the latest expanded worktree.

## D11 expansion and assurance revision, 2026-09-06

The user replaced the earlier nine-key scope with every Hikyo variable, remote Apply, and target-instance administrator access using passkey or TOTP. D09/D11 now record explicit approval. The complete inventory covers 65 recognized inputs: 53 server, 10 client, one command-only and one retired. Inventory is metadata, not implemented activation. At that earlier revision, application-generation replacement and managed deployment/bootstrap transitions were unbuilt; the earlier green CI at `152373212c6d78a3bcae91e3300097ff0d893acf` does not prove them.

Implemented in this revision: protected project reads/edits/publication, org navigation and capability hints enforce the instance-admin MFA conjunction. Exact Apply/adoption/test consumption accepts only TOTP or user-verifying passkey evidence authenticated within five minutes. Owner/revision binding, credential epoch and atomic single-use consumption remain enforced. Local host recovery remains a separate operation.

Validation:

- Full `internal/authz`, `internal/service`, `internal/store`, `internal/store/authn` and `internal/lint` checks passed with the disposable PostgreSQL DSN configured. Service 102.532 s; store 62.996 s; lint 61.325 s. Log: `/tmp/hikyo-selfconfig-d11-assurance-final.log`.
- Targeted both-engine race checks passed: store 18.122 s, service 54.400 s. Log: `/tmp/hikyo-selfconfig-d11-assurance-race.log`. Vet passed.
- Real TOTP, user-verifying passkey and human CLI handoff integration tests passed on SQLite and PostgreSQL. Passkey UV=false is refused; a fresh ceremony works after an older login; reuse is refused. Password/recovery-only sessions, wrong-owner/revision evidence, OIDC evidence, stale/future evidence and machine classes have refusal coverage. The existing database constraint refuses password/recovery factor provenance rows.
- Independent R1 authorization review returned CLEAN. It checked helper/listing parity, query count, network/host boundaries and exact factor consumption. No new SQL query was added to the existing chain-plus-grants path.
- Configuration inventory/generator tests and vet passed. Generated JSON is deterministic. Report JavaScript parses; desktop and 390px mobile show the revised D11, five explicit decisions and all 65 inventory rows without page overflow. LAN HTTP response matched the generated local HTML. A separate physical LAN device has not been checked.


## Historical nine-key implementation evidence

2026-09-06, branch `t3code/dogfood-hikyo-environment`. Product implementation is
built and locally verified. Full isolation, race and independent-owner browser
verification pass. No merge or production deployment has occurred.

| Check | Evidence |
| --- | --- |
| Setup, adoption and restart | SQLite/PostgreSQL: protected hierarchy, explicit scoped grants, encrypted initial snapshot, exact reauthentication, one-time import, name-collision isolation and restart ignoring stale settings. |
| HA adoption | PostgreSQL replicas with different seeds refuse atomically, including a fast host clock. No partial organization or session invalidation survives failure; matching seeds then adopt once. |
| Publish and Apply | Publishing leaves the active bundle unchanged. Exact reauthenticated Apply changes the durable generation and runtime. Both engines pass. |
| HA activation | Two replicas converge; stale direct consumers refuse traffic. Committed targets continue reconciling after administrator revocation. |
| Retries | Original jobs remain retrievable after later applies and snapshot collection. Changed revision or restore confirmation conflicts. Expired retries retain the original preparation deadline. Both engines pass. |
| Recovery and encryption | Exact credential confirmation gates restored outbound use. Host recovery refuses live consumers despite a one-hour-fast host clock and disabled HA flag. Normal DEK rotation/re-encryption preserve snapshots on both engines. |
| Mail | Verified TLS sink, byte-exact passwords, explicit private relay policy, delivery deadline and five tests per principal/hour. Outcome audit survives revocation. Tests send no live recipient mail. |
| Audit completeness | Real TOTP/adoption/publish/apply/TLS mail/recovery/resumption emits every registered event on both engines. Registry closure and formula matrix pass together. |
| Web | Typecheck, production build and all 919 tests pass. Desktop edits and publishes through the matrix, applies r2 using a passkey, and observes generation 2 and the new update channel without restarting. Mobile passes accessibility and overflow checks. |
| Independent owners | Live browser applies a different update channel on owner B while owner A retains its revision, generation and channel. Owner/project IDs differ. |
| SDK and docs | SDK typecheck and 20 tests pass. Docs have zero diagnostics; all 44 pages build with navigation and search verified. |
| Isolation and race checks | Full SQLite/PostgreSQL isolation suite passes (765.936 seconds). Full service, store, mail and runtime configuration race suites pass. A final focused race run covers clock/HA cleanup and adoption disagreement. |
| Vulnerability scan | UI-enabled binary: zero reachable vulnerabilities under repository-pinned govulncheck. Additional dependency findings outside reachable code were reported; this is not a claim that every dependency has zero findings. |

Reproduce with a disposable `HIKYO_TEST_POSTGRES_DSN` whose role can create
scratch databases:

```sh
go vet -p 2 ./...
GOFLAGS=-p=2 bash scripts/ci/test-core-packages.sh
go test -p 2 ./internal/lint ./internal/store/upgrade -count=1
go test -p 2 -timeout 20m ./internal/isolation -count=1
go test -race -p 2 ./internal/service ./internal/store ./internal/mail ./internal/runtimeconfig -count=1
```

Run `node --run typecheck` and `node --run test` from `web/`. Browser flows live
in `web/e2e/flows/instance-admin.spec.ts`. Operator instructions are discoverable
through docs navigation/index/search under **Manage Hikyo with Hikyo**.

The broad Go runs caught migration-48 legacy fixture handling, budget coverage,
a writer fence, audit/formula fixtures, boolean SQL parity and sensitive logging.
These were corrected without exempting the new paths from existing guards.
The complete core suite plus full reruns of the failed lint/upgrade packages
now pass. One failed package build overlapped a registry edit; its frozen-file
rerun passed.

### Standards review

One P1 finding was fixed: recovery/adoption compared host time with database HA
heartbeats. PostgreSQL now supplies the decision clock regardless of the local
HA flag. Fast-clock regressions pass. The independent reviewer rechecked the
fix and reported CLEAN for the reviewed boundaries.

### Spec review

Two P2 findings were fixed and independently rechecked:

1. Departed HA replicas no longer leave browser Apply permanently disabled. The
   UI blocks unresolved jobs and can select current membership for a new apply.
2. Indexed retry lookup precedes payload validation, compares the complete
   persisted request and returns the original job after later applies or GC.

No findings remain from these axes. Reviews were static; executed tests provide
the separate behavior evidence above.

### Integration with current main

Main added adapter findings and account profiles while this feature was built.
The merge preserves both behaviors, regenerates OpenAPI/SDK/SQLC, and moves the
unreleased self-configuration migration from 48 to 50. Historical recovery
selection and the exact legacy upgrade fixture include that boundary.
The complete merged core suite passes in one run, including both-engine upgrade
drills. Merged authorization/audit/formula tests and Go vet pass. Web validation
passes all 919 tests and both SDK checks. Fresh desktop setup/publish/apply and
independent-owner journeys pass against migration 50. Evidence: [local node
state](validation/managed-configuration-merged-nodes-desktop.png) and [independent
owner](validation/managed-configuration-merged-independent-owner-desktop.png). The first CI
format failure used a different formatter from PATH; the final format check
uses `$(go env GOROOT)/bin/gofmt`, matching CI.

### Initial CI findings

The first complete CI head exposed two additional verification issues. The new
configuration surface lacked four required pinned browser assertion records
(desktop/mobile, dark/light); the functional browser shards themselves passed.
The existing assertion helper is now exercised on the real page for each pair.
All four new assertion runs pass on fresh harness instances. Combining their
real records with the eight original CI artifacts passes the unchanged closure
checker. No coverage requirement was removed. The earlier exact-head run passed
all race/isolation shards and all eight functional browser shards.

The ARM floor schema fan-out measured 7,775.02 ms, or 31,100.08 ms after the
committed factor of four, above its 30,000 ms limit. Three alternating native
macOS ARM64 diagnostic runs measured baseline median 2,522.83 ms and integrated
candidate median 2,573.04 ms. All 100,000 cells passed authorized readback.
SQLite snapshot/lineage writes dominated; no significant self-configuration
hotspot was identified. These diagnostics do not replace Linux CI or physical
Pi evidence. The floor workload, limits and derating factors are unchanged;
PR #686 records the integrated head's acceptance result.

### CI reliability and performance corrections

The integrated run passed every browser shard and the unchanged browser closure
checker. It exposed an approval scheduler claim-response race: an INSERT could
return a fence before COMMIT reported an error. The store now preserves that
provisional token while returning `held=false` and the error. The scheduler
runs no jobs until exact fenced renewal confirms ownership, and shutdown only
releases that exact token. Deterministic lost-response, PostgreSQL deferred
commit-failure, stale-successor and canceled-claim tests pass. Takeover passed
ten times on both engines and three times under the race detector. No heartbeat,
lease lifetime or takeover timeout increased. If no fence was ever returned,
the safe fallback remains lease expiry.

A second floor result exceeded the unchanged limit (31,972 ms after derating).
Further profiling identified repeated full project-storage sums for each
schema fan-out environment. Transaction-local accounting now loads the total
once and adds each successfully inserted snapshot ciphertext's actual bytes.
Every environment still checks the same high-water limit. The state is created
inside each attempt; rollback/retry discards it. SQLite/PostgreSQL regressions
prove limit refusal rolls back the whole transaction and a fresh attempt works.

Three alternating native macOS ARM64 before/after runs reduced median publish
time from 2,588.76 ms to 2,194.91 ms (15.21%). Each exported and verified all
100,000 cells. [Recorded measurements](validation/schema-fanout-comparison.json).
The final full app, service, store and lint suites pass with both databases;
Go vet passes. Focused race, delivery authorization and static invariant checks
pass. These local diagnostics do not replace the unchanged Linux ARM CI floor gate.

### Current report and delivery

Final implementation report: HTTP 200, 265742 bytes, SHA-256
`99c01853dbad3f43e824e3e6946b3ab5e325213aaa11c1cdbef6129d270dcb4c`. Rebuilding produces identical bytes; LAN response matches.

All 64 current `knownEnv` keys are classified, alongside managed-only CA PEM and
the client XDG setting. The report loads on LAN at <http://192.168.0.30:8769/>;
1280px and 390px checks show no document overflow, valid internal links and zero
page-load resource requests. The listener serves only this report directory.
A second physical LAN device was not used for verification.

Product proof: [desktop configuration](validation/managed-configuration-desktop.png)
and [mobile node state](validation/managed-configuration-nodes-mobile.png).
Existing storage/escrow banners describe the disposable test host.

Local implementation verification is complete. Signed/DCO implementation commit
`d3c61bd7acd274f83051e48e26e72195c1f7156c` is GitHub Verified and delivered in
[PR #686](https://github.com/Hikyo-Org/Hikyo/pull/686). Current exact-head CI is recorded on that PR. Production deployment is
a separate operation.

## Original design-report verification, before implementation

Verified on 2026-09-06 against checkout `90b4ca6a5d22438e751cf9af83aa4fd077a6a61c`.

This validates the report artifact and its interactions. It is not evidence that the proposed Hikyo runtime feature works.

## Artifact and content checks

| Check | Result |
| --- | --- |
| Build with repository-pinned Node and no package installation | Pass |
| Syntax: `build.mjs`, `report.js` and generated inline browser script | Pass |
| Deterministic generation from checked-in sources and fonts | Pass |
| 27 decisions, 3 explicit approvals, 24 delegated decisions | Pass |
| All 56 current `knownEnv` keys classified | Pass |
| All referenced repository evidence paths exist | Pass |
| Unique HTML IDs and valid internal fragment targets | Pass |
| No unresolved font/scenario placeholders or em-dash in generated HTML | Pass |
| Local HTTP response | `200 OK`, `text/html`, 262627 bytes |

Final `index.html` SHA-256:

```text
3c7a560b3e948d5a77ad318c90de05ed5ee9d1d08366609ecbbb2fc825588821
```

## Browser verification

T3 collaborative preview loaded `http://127.0.0.1:8769/` with title `Hikyo manages Hikyo | Decision report`.

- Desktop inspected at 1280x800 and 1440x900; phone at 390x844. Additional 320px-wide check with every disclosure open found no document overflow. Wide catalogue content scrolls within its labelled region.
- Dark and light themes visually inspected. Embedded Instrument Sans and IBM Plex Mono loaded. Final page-load resource list was empty: no HTTP font, stylesheet, script or analytics requests.
- Search for `garbage` returned only D22. An unmatched query produced the empty state and disabled expansion. Explicit-approval filter returned D01, D06 and D26 after the remote-instance clarification. Topic filtering, deep linking and expansion operate locally.
- Native theme/expand clicks worked after scrolling targets into the viewport. Direct `#D22` navigation cleared conflicting filters and opened the decision. Browser event handlers for all five scenarios advanced through every stage, including the partial HA state and restart after durable commit.
- Print lifecycle handlers opened all report disclosures and restored their previous state afterward. The data-download link uses a local Blob containing the embedded decision record; the HTML-download link addresses the loaded document. Actual PDF export and OS download dialogs were not automated.

Two initial preview automation clicks failed because smooth scrolling had not brought the target coordinates into the viewport. Explicitly scrolling the target into view before a native click resolved the automation issue. No product code was changed to accommodate it.

## Measured contrast

Sampled computed OKLCH colors through the browser canvas into sRGB; ratios rounded to two decimals. These checks cover the selected text/control token pairs, not a formal accessibility certification.

| Pair | Dark | Light |
| --- | ---: | ---: |
| Primary text / page | 15.09:1 | 14.37:1 |
| Secondary text / panel | 7.99:1 | 7.42:1 |
| Tertiary text / panel | 7.20:1 | 7.09:1 |
| Accent text / page | 10.96:1 | 7.89:1 |
| Primary button text / accent | 10.75:1 | 7.99:1 |
| Control boundary / panel | 3.25:1 | 3.28:1 |

## Scope limits

LAN access was enabled at the user's request on 2026-09-06. A listener binds explicitly to `192.168.0.30:8769`. A request to that address returned HTTP 200 and the downloaded bytes matched the report SHA-256 above. The collaborative browser loaded the report at `http://192.168.0.30:8769/#topology`. These requests originated on the report host; a separate LAN-device request was not performed.

The artifact adds standalone HTML/CSS/JavaScript and design documentation under `docs/reports`, `docs/spec` and `docs/handoff`. It does not change the Go service or either application package. There is no TypeScript or ESLint configuration owning this standalone report; JavaScript syntax and browser behavior were checked directly. Product tests, SMTP delivery, live HA rollout, restore drills, CI, merge and deployment were not run or claimed. Their required proof remains in the implementation milestones.

## Remote-instance clarification follow-up

The root topology now shows three separately owned projects, with sharing only among the third instance’s HA replicas. D26 records the explicit remote/HA separation; D27 records the delegated root-view interpretation. The design summary, handoff, ownership rules, apply scope, recovery requirements and delivery milestones were synchronized.

Rebuilt deterministically and verified all 27 evidence-bearing decisions, source paths, unique IDs, JavaScript syntax and internal fragment links. Browser checks at 1280px and 320px found no topology/document overflow. The new D26 deep link opens its decision; the explicit-authority filter returns exactly three entries. Every existing walkthrough now explicitly scopes its nodes to one logical instance. No remote assets are loaded. The local preview server was restarted after its previous session stopped responding; the current artifact returned HTTP 200. These are report checks, not live multi-instance implementation tests.

Latest complete isolation coverage: all 369 planned cases pass with SQLite/PostgreSQL configured. Shards 0 and 1 passed in 321.622 s and 457.832 s. Shard 2 reached the unchanged ten-minute process limit without an individual assertion failure; its exact 134-case set passed in three local groups (229.973 s, 123.820 s, 176.508 s). Logs: `/tmp/hikyo-selfconfig-isolation-shard-{0,1,2}.log` and `/tmp/hikyo-selfconfig-isolation-shard-2-part-{0,1,2}.jsonl`. No timeout or test coverage changed. These final results supersede the earlier pending isolation status above.

## Integrated development controls and candidate root selection

The three development controls and `HIKYO_NEW_ROOT_SOURCE` candidate alias are integrated on the validated baseline `2a070efb`. Combined R1 review is CLEAN. Full affected packages passed with SQLite/PostgreSQL configured: config 0.824 s, runtime configuration 0.355 s, service 129.291 s, store 40.932 s, app 113.051 s and upgrade gate 95.897 s (`/tmp/hikyo-dev-next-integrated-core.log`). Full lint passed in 111.121 s (`/tmp/hikyo-dev-next-integrated-lint.log`); affected-package vet and docs check/build pass (`/tmp/hikyo-dev-next-integrated-vet.log`, `/tmp/hikyo-dev-next-docs-final.log`).

Development tests cover production refusal, real consumer replacement, preserved admission/budget counters, retained simulated provider state, and unsafe provider changes rechecked after drain. Focused isolated race passed service 8.143 s, store 6.789 s and app 26.668 s; strengthened host/network refusal race passed 6.390 s. Final PostgreSQL activation and cleanup passed in 5.484 s. Next-root tests cover exact enrolled import, invalid/missing sources, selection versus explicit rotation, clearing/restart and initial/saved/missing-node candidate-health checks. Focused runtime/rotation race passed app 45.840 s and service 11.842 s; final gate race passed 38.050 s. Next-root R3 review is CLEAN.

The report now contains 34 decisions and all 65 inventory rows. The longer status stamp wraps on small screens; browser checks found no page overflow at 320 px and 1280 px, and all internal decision fragments resolve. These later code changes have their own test evidence and are not silently included in the earlier exact-source Kubernetes binary proof.

## Combined topology and upgrade-custody validation

The combined candidate is signed D34 commit `b974c39092e8f4f13182e9e9650349c5f894deae` plus the reviewed D33 slice (`2b6ec9c6ba98dbf9bd83b550762d5c319d4bb9312c2b7f2815cfc77d3a0b25b4`) and parent integration tests. Independent combined review is CLEAN. Five-field bootstrap roundtrip and upgrade Apply/Restore with absent, singleton and HA correspondence pass.

All full affected packages passed with PostgreSQL configured and package parallelism 1: config 0.522 s, configrollout 0.848 s, runtimeconfig 0.249 s, store 37.791 s, service 143.508 s, app 115.926 s, server 63.514 s, backupreceipt 0.861 s, upgradebundle 0.618 s, upgradegate 92.364 s, lint 197.640 s (`/tmp/hikyo-topology-upgrade-full.log`). Web typecheck, all 934 tests across 107 files and build pass (`/tmp/hikyo-topology-upgrade-web-final.log`). Public docs check/build pass (`/tmp/hikyo-topology-upgrade-docs-final.log`).

The source audit maps all 53 server inputs to managed representations and actual consumers. The report generator requires exact equality with the generated inventory, preventing missing or duplicate server rows. This is source coverage, not proof of every input combination on every platform.

CI run [34048060710](https://github.com/Hikyo-Org/Hikyo/actions/runs/34048060710) covers prior signed head `8f93fb5016fce8b2fa01d00cc42f0d82a9c1293a`, not this combined candidate. Core, all eight browser shards, all three isolation shards and web closure passed. Service hit the unchanged cumulative 1200-second race-package limit; no data race was reported. Diagnosis is in progress.

Fresh native/Linux candidates use the exact same seven signed linker inputs, verified by reading actual Go string symbols from ELF/Mach-O files. The independently reconstructed union contains 795 compiled/embed inputs; its manifest SHA-256 is `1e05ee0d917f3e38e7464ccf78d173ccdcae578ed0be844c20622a11909151c4`. Linux binary SHA-256 is `0595a79f94b946f8991b1f7be90371f2646d55c5666b3e1aa2e6e2632d032a9f`; native is `2b813ee790726ca258e0eb5e0e5f56ef93da990688f61d6adff1e0c35a15eeb2`. All inputs were unchanged before and after both builds. Expanded actual-kind proof is in progress, not complete. Its first rollout found an admission-rule omission for the enrolled literal NodeID; the chart correction and actual admission regression are being completed before a fresh full run.

Combined focused race passed config 1.340 s, configrollout 2.481 s, service 51.688 s and app 46.301 s. The selector matched no runtimeconfig/store test cases; their invocation is not claimed as race coverage. Six-package vet passed and authority invariants 06/11/13 passed in 1.458 s (`/tmp/hikyo-topology-upgrade-{race,vet,invariants}.log`).

Actual Kubernetes admission reproduced and then fixed the enrolled literal NodeID omission. The same prepared Deployment delta was denied before the chart fix and admitted afterward under the executor service account in a server-side dry run. Full live admission regression passes, including enrolled identity acceptance, missing/foreign/indirect identity refusal, literal HA acceptance, indirect HA refusal, complete upgrade tuples, immutable custody mounts and restricted RBAC (`/tmp/hikyo-topology-upgrade-admission-fix.log`). No binary source changed. Fresh full proof uses the fixed chart and records every chart input hash.

The LAN report returned HTTP 200 with byte-identical file content. Browser checks found all 34 decisions and 53 consumer rows, no broken internal fragments, no resource requests and no page overflow at 320 and 1280 pixels with all disclosures open. These checks were made from the report host, not another physical LAN device.

Topology, combined integration tests and corrected chart admission are saved in signed/DCO commit `08129e051dfb05976af6aac220040309d6c60819`. All seven self-configuration isolation tests pass with actual SQLite and PostgreSQL in 39.340 s (`/tmp/hikyo-topology-upgrade-isolation.log`). SQLC regeneration into a temporary copy preserves all 65 output-directory files byte-for-byte, comprising 64 generated files and one existing manual statement helper (`/tmp/hikyo-topology-upgrade-sqlc.log`). The initial comparison incorrectly treated that manual helper as missing generated output; copying the normal output directory before regeneration verified the same workflow as CI. No source files changed for this check.

## Race-suite fixture scheduling

CI's failed service race shard exhausted its cumulative package limit while the active freshness test had run only 13 seconds. The same service source and dependency graph passed an earlier runner in 1078.419 s. A focused CPU profile attributed 49.68% cumulative time to real signed-gate/migration fixture setup, including modernc SQLite parsing; no data race or freshness wait caused the failure.

Twenty independent tests now enter `t.Parallel` before allocating state. Each retains serial engine/scenario subtests, per-test databases/root custody and normal cleanup. PostgreSQL fixture names use the existing UUID helper, generated before opening the connection. Real gate admission, migrations, assertions and deadlines are unchanged. Under GOMAXPROCS 2, package parallelism 1 and test parallelism 2, the exact same 20 top-level tests and 54 total cases passed with zero skips: serial 142.722 s, parallel 83.488 s. The heavier mixed set passed twice in 37.942 s; final PostgreSQL helper regression passed race in 4.276 s. Three-round review is CLEAN. Evidence `/tmp/hikyo-service-parallel-proof/{validation.md,timing-and-coverage.json}`; final patch SHA-256 `8aa4766443aa3ed054cdb1035b02756a000558c80e92f75500128e2932859178`. Full service race exhausted the unchanged default ten-minute cumulative limit with 162 top-level tests passed and no individual assertion failure or data-race report. The exact remaining 50 tests then passed together in 216.186 s, including every previously queued parallel test. Coverage union equals all 212 top-level tests from the actual Go inventory; no cases, gates or timeouts were removed or changed. This is complete partitioned race coverage, not a successful single invocation. CI retains its existing twenty-minute full-package limit. The four-file test-only fix is signed in `2450a34a999b11525a2d4edd5d09b9a98f534da4`. This test-only change leaves all 795 compiled inputs unchanged.

## Expanded fixture continuation checkpoints

The fixed-chart baseline completed actual expiry renewal and Restore/fresh repair/later rollout. Added probes first exposed harness assumptions: ordinary GET intentionally redacts the secret node document, production's zero disclosure window correctly refuses TOTP reveal, and clearing an optional node selector requires omission rather than an empty value. No product source changed for these corrections.

The fixture uses normal exact-MFA Apply to enable a temporary 60-second disclosure window, then the real environment-bound TOTP/reveal flow. An initial primary selector selection completed at generation 9; the invalid empty clear stayed an unpublished draft. A checked continuation verifies that exact generation/revision/process and immutable source/chart/binary evidence, replaces only that fixture draft through the normal API, clears with fresh MFA, then repeats the whole select/clear probe from a known absent baseline. The complete repeated probe passed with unchanged process, current root volume and active wrapper inventory. Current continuation driver `/tmp/hikyo-expanded-checkpoint-818e.py`; log `/tmp/hikyo-rollout-integrated-expanded-completion.log`.

The HA-enabled source Restore/fresh ordinary repair/later rollout chain also passed on the actual server, including new heartbeat and scheduler lease after same-process repair and before the later reboot. Remaining topology return, signed upgrade-custody drill and final independent-session disclosure-policy check are in progress. The latter retains session A's live positive window while session B applies policy zero, then requires A's reveal to fail before its original expiry. A same-session refusal is not treated as proof of policy narrowing because exact Apply reauthentication can replace that session's disclosure window.
