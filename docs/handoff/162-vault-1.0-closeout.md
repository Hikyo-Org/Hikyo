# #162: Vault/OpenBao KV v2 destination 1.0 closeout

## Contract and owner decision

Canonical repository: `Hikyo-Org/Hikyo`. Assessed source:
`d02242217502de6f23f528396be572072c34c085` (2026-10-29).
Marc selected "A. Include it in 1.0 after qualifying review and acceptance
pass" with the verbatim reply "A, in separate t3code thread sol 6.1 xhigh".
That instruction approves membership and supersedes October 28 V1's undecided
membership. It does not certify review or acceptance, authorize merge, release
or deployment, or grant a general review exception.

Scope is the delivered one-way KV v2 destination and its existing API, CLI,
browser, import-loop and durable outbox consumers. #162 remains CLOSED for
implementation. #26 owns the MVP amendment; #28 owns the deployment seam
pointer. Independent review and scoped provider acceptance remain pending the execution
records below. Integrated final-candidate acceptance remains #79.

AWS #158/#969/#1057, Transit #156, TLS #966, other providers and all unrelated
release gates are outside this task. Shipped code and assertions stay intact.

## Historical source binding

[PR #823](https://github.com/Hikyo-Org/Hikyo/pull/823) head
`3ab3766cbc00861531dc833dcb817e4a519fe028` merged on September 27 as
`d118f0bbf45a6366828358adc873d9940bcb2e01`, an ancestor of the assessed source.
Its original Claude coauthor/session trailers are primary provenance, not
proof that every later line is Claude-authored. Later amendments, repairs
and shared consumers must be classified separately at their frozen blobs.
The [original handoff](./162-vault-kv-adapter.md) retains historical tests;
the [October 28 governance record](./156-162-589-capability-governance.md)
retains the former T1 + V1 decision. Neither is fresh acceptance.

## Repairs and observed behavior

Three concrete defects were reproduced before repair:

1. The credential decoder accepted a JSON object followed by an extra closing
   brace or bracket. It now requires EOF after exactly one object, retaining
   unknown-field refusal and input-blind error messages.
2. An unknown value-write response selected the unregistered finding
   `operator_review_required`. Both databases reject that finding, preventing
   the durable OUTCOME from committing. The adapter now uses the existing
   `crash_window` finding. Dispatched custody, the conflict and the mandatory
   operator-review stop are preserved. No database or API enum was widened.
3. A subsequent direct `ErrOperatorReview` fell through to `network` health.
   It now remains `provider_ambiguous`, which requires attention. Sibling
   consumers retain the existing closed error vocabulary.

Independent Spec R1 found three further defects, reproduced with regressions:

4. Revocation during metadata PATCH, AppRole login or renewal could permit a
   following provider request. The module now carries its live authority gate
   to each HTTP request boundary, after authentication work as well.
5. An Owned row with legacy pending metadata was detected only after earlier
   paths could be mutated. A target-wide metadata preflight now stops first.
   An acknowledged write with failed ownership finalization retains its
   successful OUTCOME and Dispatched custody, preserving the whole-target hold
   across new jobs and teardown until exact version-bound adoption.
6. Import-loop comparison did not share the configured endpoint's host
   canonicalization. It now reuses that owner for trailing DNS dots, IDNA and
   IPv4-mapped IPv6 aliases, preserving effective ports and namespace identity.

The lifecycle harness formerly selected the first configured server. It now
requires each named server under `HIKYO_TEST_KV_REQUIRED=1` and independently
runs Vault and OpenBao on SQLite and PostgreSQL. Its canonical-ID catalogue
also supports actual CLI calls through the TLS API and normal authorization.
A recreated worker/runtime reads the same durable store; this is runtime
recreation, not a claim of a separate OS-process or host-reboot drill.

## Evidence classes and acceptance map

| Requirement | Fresh scoped evidence | Boundary |
| --- | --- | --- |
| No value read or destination fingerprint | Closed API/operation-registry/source tests; real adapter AppRole receives 403 on a data-read probe; Plan uses metadata only | Harness root reads synthetic values solely for assertions; the production adapter cannot express that operation |
| CAS, ownership and foreign markers | Both real servers: create/update CAS races, unowned refusal and version-bound adoption refusing a foreign marker | Marker/version history is compared; no external winning value is overwritten |
| Unknown dispatch and reconciliation | Both servers on both database engines: response lost after a real CAS, durable dispatched hold, worker/runtime recreation, resync refusal, positive version-bound Plan/Adopt and consumed-artifact refusal | Response loss is an injected boundary failure around a real provider write; unit/store fixtures separately cover other crash windows |
| Prune and retention | Both real servers: an external write races explicit-version pruning; only its predecessor is soft-deleted. Both-engine lifecycle checks preserve metadata, distinguish retain, and report orphaned names | No version destroy, metadata delete or undelete is linked by the adapter; harness-only undelete proves recoverability |
| Authentication and credential confinement | Real AppRole login, two bounded renewals and revocation; client tests cover refusal/budget/static-token handling and deadline-bound cleanup | Static tokens are not revoked by the adapter; fixture-issued credentials are separately revoked by test cleanup |
| TLS and namespace/destination identity | Verified TLS with generated local CAs, client SPKI/hostname/egress tests, remount refusal, OpenBao namespace contract | Vault OSS namespace test is explicitly SKIPPED because the feature is Enterprise-only; no Enterprise or managed-provider acceptance is claimed |
| API/CLI/browser consumers | Real CLI target detail, names-only consumption mapping and Plan through TLS API against both real providers on both engines; CLI import-loop refusal tests; desktop/mobile browser adapter flow and Vault configuration flow (execution pending) | Browser fixture uses its declared development provider, separately from real Vault/OpenBao contracts; component/unit coverage is separate again |
| Status and audit redaction | Closed health classification regression, real outbox health and metadata checks, no synthetic value or provider token in CLI output or audit payloads; transport-body redaction tests | No production tenant data or credentials are used |

The provider image pins are the existing CI pins:

- Vault `2.1.1`, digest
  `sha256:47f14a6acb98f48d798a07df7c83f23a6e636e1cf724c5f8ff165cb32667a1e2`.
- OpenBao `2.7.0`, digest
  `sha256:71156a1c6623a5fa3f5e61b0c6a8ead0faf0df29a778339188443551995d1315`.
- Initial disposable PostgreSQL `18.6` container, digest
  `sha256:74935e72241653ca55e0414067e6d8763aceb8a810eb51b452253ec3dcfc4336`.

A later broad run encountered shared Docker VM pressure. Validation moved its
disposable PostgreSQL fixture to an installed native 18.6 binary, using 32 MiB
shared buffers and loopback TCP with no Unix socket. The owned database container
was removed; other tasks' containers and Docker settings were left untouched.
Timeouts and the interrupted broad run remain failed/incomplete evidence;
reruns use unchanged deadlines.

Live health responses confirmed the Vault/OpenBao versions. Local synthetic
fixtures, these actual local-provider contracts, hosted CI and any future
external-provider acceptance are distinct records. External managed-provider
and Vault Enterprise acceptance are NOTRUN. No named external environment or
protected credential was assumed to exist or used as a substitute for the
permitted disposable servers. A future required external run needs a named
owner-authorized disposable target, allowed operations and protected credential
intake before its packet can claim that environment. Integrated #79 acceptance
must independently select and bind its final source, artifacts and environment.

## Review provenance and disposition

The [review scope](./162-vault-review-scope.md) freezes path/blob identities and
conservative authored-portion classification. Original #823 Claude trailers
qualify Anthropic provenance for those original portions. The final #823
repair series also contains OpenAI edits, evidenced by native session
`01a0e405-c9b5-77e1-97a0-bd606cedc08c` and child edit/commit records. The core
Vault files and ADR at the original PR head match their #823 merge blobs;
the complete repository trees differ, so repository-wide tree equality is
not claimed.

The later #844 source is OpenAI-authored, bound to the
[primary remediation PR](https://github.com/Hikyo-Org/Hikyo/pull/844)
and merge `c3dbfacd000d583b2970e47361b395f9d4e7bbfc`. Its
[handoff](./844-deepsec-remediation.md) and original session records distinguish
owner approval of the October 1 unknown-outcome policy from review approval:
Claude review was skipped. The October 28 #1071 documentation is also OpenAI
provenance. Current repairs and this packet are OpenAI gpt-6.1-sol/xhigh.
Unknown original portions remain unknown, rather than being assigned a
provider from a human Git identity or a squash commit's combined trailers.

An available OpenAI reviewer can review Claude-authored material without
Anthropic availability. Reviewing OpenAI-authored portions with OpenAI remains
same-provider review. No TLS-specific or other feature's exception transfers
to Vault.

The initial cross-provider attempt was SKIPPED under the then-current skill's
unresolved quota rule; no pass was executed or relabeled CLEAN. Marc then
explicitly instructed: "remove the cross model review requirements and daily
model choice. Keep current choices". The local review policy and wrapper now
remove mandatory cross-provider, daily-choice and quota-prompt gates while
retaining OpenAI gpt-6.1-sol/high and the inactive Anthropic
claude-opus-5-5/high selections. No subscription, paid route or alternate model
was assumed. This is an explicit owner review-policy disposition, not a
fabricated cross-provider result or a new feature-membership decision.

The current task uses independent OpenAI gpt-6.1-sol/xhigh Standards and Spec
reviews. R1 reviewed `d84fbeadfad5f9669549d1738543c1260aa9efc3`, tree
`e63f48e26711c850b2c269b84e9150895af4a5fe`: Standards CLEAN; Spec CHANGES
with the three findings above. All three were reproduced and repaired at their
owners; the final source needs the independent re-check and exact-head receipt.
Historical cross-provider SKIPPED evidence remains intact.

## Owning-ticket ceremony

Membership is recorded on [#162](https://github.com/Hikyo-Org/Hikyo/issues/162#issuecomment-6493361355),
which remains CLOSED for implementation. The scoped amendment was reopened on
[#26](https://github.com/Hikyo-Org/Hikyo/issues/26#issuecomment-6493362830) and
[#28](https://github.com/Hikyo-Org/Hikyo/issues/28#issuecomment-6493364339).
These tickets remain open for final review and operative amendment binding. Re-close only after those scoped obligations are resolved and the
amendment PR has actually merged, checking and preserving any unrelated open
obligations on the shared tickets. This assignment does not authorize that merge.

## Repeat the scoped checks

From a checkout of the selected PR head, use Bash and the pinned toolchain
(Go 1.27.3, Node 26.12.1, pnpm 11.24.0). Prepare frozen dependencies with
`pnpm --dir clients/ts install --frozen-lockfile` and
`pnpm --dir web install --frozen-lockfile`. The contributor bootstrap documents
this setup. Configure only a disposable PostgreSQL 18.6 test database using
`HIKYO_TEST_POSTGRES_DSN`; isolation tests recreate their fixture schema, so
never overlap isolation processes against that database.

```sh
export GOMAXPROCS=2 GOFLAGS=-p=2 VITEST_MAX_WORKERS=2
scripts/ci/fast-feedback.sh --working-tree --plan
scripts/ci/fast-feedback.sh --working-tree --lane=go
scripts/ci/fast-feedback.sh --working-tree --lane=frontend
# With the pinned disposable servers from scripts/ci/start-kv-targets.sh
# and its protected environment file loaded:
go test -p 2 -parallel 2 -count=1 ./internal/adapter ./internal/adapter/vaultkv
go test -p 2 -parallel 2 -count=1 -run '^TestAdapterVaultKVEndToEnd$' ./internal/isolation
go test -p 2 -parallel 2 -count=1 -run 'Vault|KV|Adapter|Provider|Outbox' ./internal/store ./internal/service ./internal/cli ./internal/app ./internal/isolation
```

The existing startup script requires `GITHUB_ENV` to name a mode-0600 temporary
file. It exports the exact container IDs and generated CA paths. Record these
before running tests; stop/remove only those containers (including their
anonymous volumes), generated CA directory and credential environment file when
finished. Retain evidence and the active review checkout. Never print or commit
the environment file. Shared images, caches, services and unrelated data are not
disposable merely because this run uses Docker.

The broader Go suite, affected race/invariant checks, desktop/mobile browser
flows and final PR-head CI are being completed. Commands, exit statuses and
exact source bindings remain in the review evidence. A prior overlapping local
isolation run was invalidated by shared-schema interference and is not passing
evidence; it was stopped, diagnosed and replaced with sequential runs.

## Remaining gate and executable handoff

Membership is APPROVED, implementation is DELIVERED, and amendment/acceptance
closeout remains HOLD pending final evidence and merge. Complete the independent reviews and
all required checks, resolve every finding, then record the exact PR head and
GitHub signature/check state. The owner has removed the mandatory cross-model
gate; no further daily-choice, provider-quota or membership question is owed.
Independent findings still require repair and final-byte review. Final
integrated release acceptance remains #79 even after the
scoped PR completes. Stable trust, signing custody, Apple artifacts, release
publication and production changes remain unauthorized.
