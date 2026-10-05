# Independent verification of cleanup findings

Date: 2026-10-05. Commit: `5c3f969f692268e5c0bc7f5b9acc860f3bff0f48`.

One new subagent independently reviewed all F01-F16, attempted concrete counterarguments, traced callers and accepted decisions, and reran selected probes. The parent checked disputed claims and reran the actual generated HTTP/SSE decoder probe. Application code was not changed during that investigation. Subsequent fixes are recorded in the [implementation handoff](../handoff/code-cleanup-2026-10-05.md).

**Result: 12 CONFIRMED, 4 PARTIALLY CONFIRMED, 0 wholly disproven, 0 left UNPROVEN.** This is source verification plus selected reproductions, not universal proof that every proposed removal is desirable or that all runtime scenarios were exercised. Static redundancy and dead internals remain cleanup observations rather than runtime failures.

The [main audit report](code-cleanup-audit-2026-10-05.md) contains source links, cleanup scope, and regression requirements. This document records the independent verdicts and counterarguments.

## Behavior and boundary findings

### F01: CONFIRMED, P2, remote settings misrouting

Evidence: `web/src/routes/HistoryDrawer.tsx:249` preserves remote query; `Matrix.tsx:1026` drops it; `app/App.tsx:89` renders ProjectSettings without WorkspaceScope. `api/settings.ts:95,787` uses singleton transport; `api/client.ts:97` configures same-origin cookies.

Counterargument tested: matching project names might resolve on either instance. Backend resolves opaque IDs, not display names: `internal/server/hierarchy.go:219`, `internal/store/authn/authn.go:245`, and `internal/store/queries/sqlite/authn.sql:12-17`. Distinct IDs cause refusal. Matching org/project IDs plus home-session authorization can expose and mutate home settings. Matching names alone is insufficient.

Narrow cleanup: external anchor to the remote-origin settings page, retaining local router links. Wrapping settings alone does not fix singleton API hooks. Source/caller proof is complete; no two-instance browser reproduction ran.

### F02: CONFIRMED, P2, nightly download ordering and duplicate transfer

Evidence: `internal/selfupdate/selfupdate.go:122-141` downloads checksum/archive before prerelease dispatch. `nightly_download.go:54-119` authenticates manifest before its own download. First archive is discarded.

Counterarguments tested: payload might be reused or Apply might always use cache. Neither is true. Existing cache may reduce duplicate transfer, but cannot eliminate the first premature download. Overlay rerun observed one archive before invalid-signature refusal and two on successful uncached preparation. Invalid release installation remains refused.

Narrow cleanup: dispatch prerelease before stable download logic while preserving early executable-replaceability checks.

### F03: CONFIRMED, P2, case-sensitive credential scrub

Evidence: `internal/importer/spawn.go:200-203` uses case-sensitive prefix; SanitizedEnv and WithSanitized share it. `internal/cli/compose_run.go:481` normalizes case. `.goreleaser.yaml:29` includes Windows. `docs/adr/import-paths.md:89` requires sanitized connector subprocesses.

Counterargument tested: Windows enumeration might normalize names to uppercase. Installed Go 1.27 `src/syscall/env_windows.go:80-103` preserves environment-block spellings. Mac overlay demonstrated mixed-case values surviving direct and process-global filtering. Windows execution remains untested; its case-insensitive credential lookup makes this relevant when ambient credentials have mixed-case names.

Narrow cleanup: normalize shared predicate while preserving original names for restoration and keeping non-Hikyo connector credentials.

### F04: CONFIRMED, P2, precision loss before bigint validation

Evidence: `clients/ts/openapi-ts.config.ts` has no lossless integer parser; `web/vite.config.ts:44-52` binds app aliases to current generated modules. Singleton/workspace config supplies no parser override. Generated HTTP `client/client.gen.ts:159` and SSE `core/serverSentEvents.gen.ts:189` use JSON.parse. Transformers run after parsing. `zod.gen.ts:398` coerces the already-decoded number.

Counterargument tested: an SDK transformer/runtime option might preserve original integer tokens. Current configuration does not. A probe executed both actual generated clients against wire literal `9007199254740993`; both produced `9007199254740992`. BigInt then retains that rounded value. Some int64 endpoints can instead fail bounds after rounding, so not every unsafe integer necessarily succeeds.

Narrow cleanup: owned generator/runtime parser boundary, including SSE; regenerate and correct the history no-loss comment. Actual generated Zod execution did not run because its dependency is absent.

### F05: CONFIRMED, P2 boundary gap, adapter conversion

Evidence: `web/src/api/adapters.ts:297,365-367` uses unchecked Number; live UI callers at `routes/Adapters.tsx:1726,1764` pass generated bigint response fields. Unsafe wire values can reach these wrappers through F04. Numeric probe confirmed rounding.

Counterargument tested: server optimistic checks prevent incorrect binding. They do protect writes: `internal/service/adapters.go:781` rejects update-generation mismatch; `:1950` compares adoption generation/repository/destination before adoption writes. The supported consequence is precision-dependent refusal, not demonstrated wrong-destination mutation. Extreme generations are operationally unlikely; external-provider IDs are a separate boundary.

Narrow cleanup: checked conversion with field-appropriate minimums. F04 must be fixed separately; outbound checks cannot recover an already-rounded response.

### F06: PARTIALLY CONFIRMED, P3 corruption hardening

Evidence: ignored decode error at `internal/service/pki_certificates.go:97`; list at `:716` and show consume permissive view. Normal renewal at `:567` propagates decode failure, while already-renewed successor branch at `:538` uses the same permissive helper.

Counterargument tested: storage invariant makes malformed data unreachable. Normal `encodeSANs` at `:83-93`, called at `:225`, emits valid typed JSON. Ordinary issuance does not produce malformed SANs. Both `00064_pki.sql:106` migrations store nonnull TEXT without JSON-shape enforcement, so corruption/manual writes or inconsistent imported storage can trigger it.

The overlay supplies malformed SAN text directly to the view helper and observes a partial successful view. It does not reproduce corrupt storage through the database/API. This is P3 corruption-only hardening, not an ordinary P2 issuance defect.

Narrow cleanup: validate at a boundary that preserves custody. Blindly adding an error after `settleLeaf` at `:476-489` can lose a display-once generated private key.

### F07: CONFIRMED, P2 error-classification drift

Evidence: `web/src/api/definitions-bundle.ts:168-183` labels every non-ApiError connection failure. `api/client.ts:240-246` can reject unexpected success status/schema after network action. `DefinitionsBundlePanel.tsx:115,223` renders this classifier.

Counterargument tested: errors might all precede apply or be file-input errors. File errors have separate handling at panel `:127-132`; apply response parsing follows the request/server action. Malformed successful response or uncertain transport outcome can therefore receive misleading retry guidance.

Narrow cleanup: shared transport/contract classifier and action-specific refresh-before-retry guidance. Preserve domain, scanner, and widening refusals.

## Redundancy and dead-code findings

### F08: PARTIALLY CONFIRMED, P3 test consolidation

Evidence: `internal/isolation/contract_test.go:43,78,129` compares route/class/primary metadata with OpenAPI-derived wire facts. `internal/authz/gen/main.go:402-439` generates those facts.

Counterargument tested: tests might independently inspect the router. They do not, despite names/comments. However, they remain useful generator-output checks: stale output or a broken renderer can fail them. They are not meaningless assertions.

Relevant independent coverage: CI freshness at `.github/workflows/ci.yml:502-510`, negative fixtures at `authz/gen/main_test.go:39`, freshness fixture at `:149`, and actual bidirectional router walk at `isolation/invariants_test.go:43-65,95-100`. `docs/adr/generated-boundaries.md:110-114` explicitly permits removing same-source comparisons.

Narrow cleanup: remove only duplicated generation checks or rename/reclassify them. Preserve independent formula/policy/audit checks and actual-router coverage.

### F09: PARTIALLY CONFIRMED, P3 optional defensive simplification

Evidence: `internal/authz/verify.go:33-37` rejects noncanonical/typed-nil proof. `VerifyEvent:75-82` succeeds through Verify then repeats the same concrete-type/nil check.

Counterarguments tested: embedded forged proof, typed nil, concurrent replacement of local interface. Verify rejects the first two. Safe Go cannot replace this local interface's dynamic type through that call because it is passed by value. Branch is statically unreachable today.

The second guard can nevertheless retain defensive value if Verify changes. No authorization defect was found and no removal is required for safety. Optional private helper can return verified canonical proof and scope. Never replace boundary verification with an unchecked assertion.

### F10: CONFIRMED, P3 typed-call simplification

Evidence: `web/src/api/client.ts:297-304` compares bodyless operation identity with whoami. `operations.gen.ts:1472` defines body-bearing whoami. Separate module-private classes/private brands at `operationsBuilder.ts:142-161` exclude it from valid BodylessOperation arguments.

Counterargument tested: structural construction or spread-cloning might produce a valid whoami bodyless descriptor. Private brands prevent this in well-typed code. Unsafe casts or untyped calls fall outside the stated contract.

No runtime trigger exists for valid typed inputs. Remove identity exemption only from ok; preserve parsed/parsedPick identity behavior and remote-refusal ownership.

### F11: CONFIRMED, P3 dead accounting and stale comments

Evidence: full hidden-tree search excluding navigation/report outputs finds AuthorityChanged only declared at `internal/service/scim_origin_release.go:111`. RowsRevoked is only declared, read there, and incremented at `:279`. No interface/reflection dispatch found.

Counterargument tested: old handoff might require generation accounting. `docs/adr/scim-provisioning.md:3` operative October 1 amendment supersedes old session invalidation policy. Current release logic preserves instance sessions. Header and `:85-88,107-110` comments are stale.

Narrow cleanup: private method/counter/increment and comments. No runtime defect shown; retain current org-scoped behavior and truthful audit.

### F12: CONFIRMED, P3 unused boolean

Evidence: `scimGrantPayload:367-392` uses revoked only through `_ = revoked`; callers at `:344,352` pass false/true.

Counterargument tested: boolean might control event output elsewhere. grantRevokedEvent owns remaining-origin and sessions-revoked fields independently. Remove only argument/discard; retain caller-owned differences. No behavior defect.

### F13: CONFIRMED, P3 constructor cleanup

Evidence: `internal/server/server.go:96-175` never reads ready. New at `:91` only forwards it. `internal/app/generation.go:420` constructs discarded service.System.

Counterargument tested: registration/closure might consume readiness indirectly. Public router has no such use. Live checker at generation `:405,425` feeds NewOperational independently. No public readiness defect shown.

Narrow cleanup: argument and callers/allocation only; preserve operational probes and listener split.

### F14: CONFIRMED, P3 unused internal API

Evidence: full hidden-tree search finds only BootstrapPending comment/declaration at `internal/service/bootstrap.go:222-224`. No reflection/linkname dispatch or generation directive found.

Counterargument tested: exported method or comment's promised CLI consumer might create a compatibility requirement. Actual CLI has no caller; method belongs to internal/service and no accepted future consumer was demonstrated.

Remove wrapper only; retain actual bootstrap guards and shared queries. No behavior defect.

### F15: PARTIALLY CONFIRMED, P3 narrow retirement cleanup

Evidence: unread executor/control fields confirmed. Config entry callers are test-only, but its internal call chain is LoadConfig -> Config.Validate -> Command.validate. These helpers are transitively test-reachable, not all literally test-called.

`cmd/hikyo/main.go:433` genuinely calls refusal-only updater.Run; app boot rejects socket at `app.go:324`. No production Config/Executor/ControlServer/NewClient construction found.

Counterargument tested: retirement might require retaining the entire API. `docs/handoff/638-legacy-updater-retirement.md:23-33` explicitly preserves independent refusal doors and historical job/journal interfaces. `protocol.go:61-112` and service `updates.go:70,199` retain historical reads/acknowledgements, although current boot does not wire a control client.

Narrow cleanup: unread fields and retired execution-policy parser chain. Preserve direct refusals, historical formats, compatibility tests, and used Executor.Config.Backend. Describe retained compatibility accurately; do not remove whole package.

### F16: CONFIRMED, P3 unused helper inputs

Bodies confirm unused arguments: `liveChallenge:137` ctx/az; `stageResetRefusal:164` now; `LowerEffectiveWindow:317` now; `leaseDisclosureGate:600` projectScope. Live call sites exist in challenge/reset/settings/dynamic paths.

Counterargument tested: unused names might reveal missing security/time checks. Surrounding operations consume authorization/context/time; these helpers use remaining inputs. No missing security policy was proved.

Narrow cleanup: remove arguments/update callers, without policy restructuring. Signature simplification only.

## Executed evidence and limits

Three existing Go-overlay probes reran and observed nightly premature/double download, mixed-case credential retention, and partial malformed-SAN view. Actual generated HTTP/SSE decoder probe observed rounding. Selected authz/generator tests, `go run ./internal/authz/gen -check`, and the full internal/updater suite passed.

Original diagnostics were temporary local overlays and a generated-decoder probe. Committed fix regressions and reproducible package commands are listed in the implementation handoff.

Passing diagnostic overlays means the defect was observed, not that behavior is healthy. No browser/two-instance, Windows-runtime, generated-Zod-runtime, or whole-repository suite ran. A source-confirmed optional simplification is not proof its removal improves security or maintainability.
