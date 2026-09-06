# Self-configuration implementation and report validation

## Current implementation evidence

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
