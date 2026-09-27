# Handoff: #162 Vault/OpenBao KV v2 synchronization adapter

Issue: https://github.com/Hikyo-Org/Hikyo/issues/162. Base: `338251a` (main,
2026-09-26). A third compiled-in provider, `vault-kv`, over the #65 seam and
the #157 multi-target workflow. Design authority:
[`docs/adr/vault-kv-adapter.md`](../adr/vault-kv-adapter.md) (proposed; the
mvp-boundary § 4.1 amendment it depends on still needs the #26 governance
review, see "Open").

## What landed

- **Provider kind** `vault-kv` in `adapter.SupportedProviders`, the app module
  factory (10 s request deadline so five sequential requests fit the write
  lease; egress policy keyed by the bare `https://host:port`), the store's
  create validation (now `adapter.ParseProvider`, no hard-coded pair), the CLI,
  the OpenAPI `AdapterProvider` open enum (+ `apigen`, `clients/ts`), and the
  web create form. `TestProviderSetIsPinnedAcrossSurfaces` pins the compiled
  set against OpenAPI and the newest provider CHECK of both engines.
- **Migration 00060** (both engines): widens `adapters.provider` only. SQLite
  rebuilds `adapters` exactly as 00054 left it. KV addressing reuses the
  repository destination (`destination_owner` = mount, `destination_name` =
  path prefix), so the closed `AdapterDestinationKind` response enum did not
  grow (freeze-safe). `internal/buildcompat/development.json` regenerated
  against PostgreSQL 18.6 (diff: the two 00060 entries and both schema
  digests); the legacy upgrade drill reverses 00060 (Postgres: narrow the
  CHECK; SQLite: the existing 00025 `adapters` restore already covers it).
- **`internal/adapter/vaultkv`**: hand-rolled client behind a closed `API`
  (Health, MountInfo, LookupSelf, ReadMetadata,
  PatchCustomMetadata, WriteCAS, DeleteVersion) and one operation registry.
  No data GET/LIST, no destroy, no metadata delete, no sys/raw: pinned by a
  reflection test plus a source scan. Credential is a bare token or JSON
  (`token` | `approle`, optional `ca_pem`, `spki_sha256`), unknown fields
  refused, errors never echo input. AppRole logs in per attempt, renews at most
  twice, revokes on `Forget`; static tokens are never revoked.
  `sys/health` is sent without the namespace header (root-only on OpenBao).
- **Module**: value-blind Plan from metadata; Sync with the CAS + marker
  protocol (`managed_by_hikyo`, `hikyo_version`, `hikyo_pending_version`):
  mark (existing paths only; creates write with `cas = 0` first), write with
  `cas`, finalize; replay decides landed/not-landed from
  metadata alone. Unowned paths refuse before any mutation; adopted (claimed,
  unmarked) paths are taken over with CAS on the observed version; a claimed
  desired path that moved is a conflict that keeps its claim; an undesired
  path that moved is released with a conflict and a target warning, never
  deleted (so removal cannot wedge). Prune = soft delete of the current
  version. `destination_id` = hash(mount uuid, path prefix) so remounts
  refuse. Only one sentinel (both surfaces share one tree).
- **Service**: `InspectTarget --format workflow` renders KV paths
  (`adapter.VaultKVMapping`). Origin must be spelled canonically.
- **CLI**: `--provider vault-kv`, `--mount`/`--path` (map to the repository
  destination; mixing with `--owner/--repo/GitHub routing` is refused) on
  `adapter create|update` and `adapter target add`; help golden refreshed.
- **Loop safety**: live `hikyo import --from vault` lists the project's
  adapters and refuses by name when an active `vault-kv` target overlaps (same
  host, namespace, mount; either path prefix contains the other). A caller who
  cannot list adapters is refused (fail closed). `importer.Result.Namespace`
  carries the live namespace (never persisted).
- **Docs**: ADR, mvp-boundary amendment row, deployment-adapter banner, ADR
  index, `deployment-adapters.mdx` section, CLI reference.

## Tests

- Unit: fake in-memory KV v2 (`module_test.go`) covering create, update,
  unowned refusal, adoption, external movement, CAS race, both ambiguous-write
  crash windows, ambiguous create (landed and not), unmarked-create replay,
  create and update CAS races (no metadata clobber, pending marker
  withdrawn), definitive failure, prune + reclaim,
  teardown ordering, remount, KV v1 refusal, value-blind Plan, sealed server,
  lookup-self refused by policy. Client tests over TLS with a SPKI pin
  (namespace/token headers, CAS body, merge-patch null, classification of
  400-CAS/400/403/503/429/404/oversize, pin mismatch, AppRole
  login/renew/revoke budget).
- Contract (`contract_external_test.go`): real Vault 2.1.1 and OpenBao 2.7.0
  dev servers over TLS, adapter on an AppRole whose policy denies data reads
  (server-enforced no-read proof), byte-exact delivery, ambiguous replay, CAS
  conflict, unowned refusal, release-not-prune of a moved path, soft delete
  then `undelete`, remount refusal, and OpenBao namespaces (Vault OSS skips
  namespaces by name).
- End to end (`internal/isolation/adapter_vaultkv_e2e_test.go`): service,
  store, outbox worker, real client and real server, SQLite and PostgreSQL:
  publish delivers, an external edit becomes a `conflict` error class without
  overwrite, removal soft-deletes the sentinel and keeps metadata, no audit
  payload carries plaintext.
- CI: `scripts/ci/start-kv-targets.sh` starts digest-pinned
  `hashicorp/vault:2.1.1` and `openbao/openbao:2.7.0` dev-TLS containers in
  `test_core`, exports `HIKYO_TEST_{VAULT,OPENBAO}_*` and
  `HIKYO_TEST_KV_REQUIRED=1`, then runs the contract and the e2e. Locally they
  skip loudly without the variables.

Local verification (this session): all of the above passed against Vault
2.1.1 (release binary) and OpenBao 2.7.0 (built from the v2.7.0 tag), with
`HIKYO_TEST_POSTGRES_DSN` on PostgreSQL 18.6. The CI container script itself
could not be exercised here (no Docker daemon).

## Deliberately out (named in the ADR)

KV v1; JWT/OIDC login; one-secret-per-target layout; version destroy and
metadata delete (never linked; a ceremony-gated destroy would be a new ADR
operation); skipping unchanged values (would need a read or a fingerprint).
Every converge adds one KV version per key; operators bound it with
`max_versions`.

## Open

- Governance: reopen #26 and run the adversarial review for the mvp-boundary
  § 4.1 amendment and the ADR, then flip "proposed" to operative.
- The web form reuses the generic target editor with KV labels; there is no
  KV-specific status copy beyond the provider name and hints.


## PR #823 reconciliation and adversarial inspection

Integrated main and the Cloudflare adapter (#822), preserving sealed-webhook,
Cloudflare and Vault provider registration, API enums, browser routing and tests.
Vault's additive provider migration is now 00063 on both engines and retains
all previous providers. Regenerated the development declaration against an
isolated PostgreSQL scratch database and adjusted the legacy restore drill to
reverse migrations 45 through 63.

Adversarial review found a prune race: deleting the latest version could delete
an external write after the ownership inspection. Prune now soft-deletes only
the inspected version through POST /delete with an explicit version list.
Regression coverage inserts an external write immediately before deletion and
proves that newer version remains live. A client test pins the exact request
path, method and version. Updated least-privilege policies and external contract
fixtures grant update on the delete path; data read, destruction and undelete
remain unavailable to the provider.

Browser credentials still use the sensitive state/mutation owners. The merged
provider labels and destination forms preserve both Vault mount/path and
Cloudflare Workers/Pages addressing. Sensitivity pins were refreshed after
reviewing these merged modules.

Validation: 1172 browser unit tests; browser typecheck/lint; generated client
20 tests and typecheck; Vault race tests; relevant adapter/CLI/store/service/app
checks and both-engine upgrade/restore fixtures. The coordinating task owns
remote CI and final merge. Cross-provider review was skipped by its quota gate.
