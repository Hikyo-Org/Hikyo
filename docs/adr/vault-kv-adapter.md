# Hikyo Vault/OpenBao KV v2 adapter: a one-way destination over the deployment-module seam (ADR, proposed 2026-09-26)

> **Status: proposed with [#162](https://github.com/Hikyo-Org/Hikyo/issues/162).** It becomes operative when the [mvp-boundary](./mvp-boundary.md) amendment it depends on (§ 4.1 "Cloud secret-manager sync" row) completes the [oss-mechanics](./oss-mechanics.md) § Governance procedure: [#26](https://github.com/Hikyo-Org/Hikyo/issues/26) reopened, adversarial cross-model review, amendment recorded. The implementation ships behind that gate; the maintainer records the review outcome here.

Context: the [deployment-adapter](./deployment-adapter.md) ADR fixes the four-operation seam, the durable outbox, ledger ownership, adoption, and teardown. The [github-adapter](./github-adapter.md) ADR shows how a sibling adapter composes with it: **constraints transfer wholesale, mechanisms are re-derived per provider.** The [import-paths](./import-paths.md) ADR already admits Vault/OpenBao as an **import source** (file and live, read-only). This ADR adds Vault/OpenBao KV v2 as a **sync destination** and fixes what differs. Where it is silent, the deployment-adapter ADR's rule applies verbatim.

## Scope

In: one-way delivery of Hikyo-published revisions into a Vault or OpenBao **KV version 2** mount, for self-hosted and managed servers alike, including Vault Enterprise and OpenBao namespaces. Hikyo stays the source of truth; nothing is ever read back or merged.

Out (named, not deferred by silence): KV version 1 (no check-and-set, so no safe concurrency), transit and dynamic engines, Vault Agent, Vault as a Hikyo identity provider, JWT/OIDC login (below), one-secret-per-target layout (below), and version destruction (below). Managed cloud secret managers (AWS, GCP, Azure) remain governed by their own tickets.

## Destination class and addressing

A vault-kv adapter names one server: `origin = https://host[:port]` optionally followed by `/<namespace path>`. The namespace travels in the `X-Vault-Namespace` header, which both Vault Enterprise and OpenBao honor; `sys/health` is root-only and is sent without it. The adapter record accepts only the canonical origin spelling so one server and namespace cannot be configured twice. The operator egress policy is keyed by the bare `https://host[:port]`.

A target reuses the generic **repository** destination record, so the API's closed destination-kind enum does not grow: `destination_owner` is the **KV v2 mount path**, `destination_name` is the **path prefix**. Each key becomes **one KV secret** at `{mount}/{path prefix}/{name prefix}{canonical name}` holding a single field, `value`. Both classifications share that one tree, so effective names must be unique across the secret and config surfaces (case-insensitively, matching the ledger), and each must be a single path segment. Values must be UTF-8 (JSON cannot carry other bytes exactly) and at most 512 KiB (inside the default 1 MiB integrated-storage entry after escaping). The management sentinel `MANAGED_BY_HIKYO` is one secret, written first and removed last.

`destination_id` binds the target to the live mount: `sha256("hikyo-vault-kv" ‖ mount uuid (or accessor) ‖ path prefix)`, truncated to a positive int64. A mount that is disabled and re-enabled, or moved, has a new uuid, so every later write refuses with the destination-identity error. Including the path prefix keeps two targets on one mount distinct in the ledger's provider-name index.

## No value read, structurally

The client is hand-rolled `net/http` behind a closed `API` interface whose method set is pinned by a reflection test, and every request resolves through one operation registry that a second test scans:

| Operation | Endpoint | Returns |
|---|---|---|
| Health | `GET sys/health` | initialized, sealed, version |
| MountInfo | `GET sys/internal/ui/mounts/{mount}` | type, KV version, uuid, accessor |
| LookupSelf | `GET auth/token/lookup-self` | token expiry only (decoded) |
| ReadMetadata | `GET {mount}/metadata/{path}` | versions and custom metadata, **never data** |
| PatchCustomMetadata | `PATCH {mount}/metadata/{path}` (merge; keys set to null are removed) | nothing |
| WriteCAS | `POST {mount}/data/{path}` with `options.cas` | the new version |
| DeleteVersion | `POST {mount}/delete/{path}` with the inspected version | nothing (recoverable soft delete) |

Excluded, and refused by the tests: any `GET` or `LIST` of `{mount}/data/…`, `LIST` of anything, `destroy`, `undelete` of versions, `DELETE` of metadata, and `sys/raw`. The contract suite runs the adapter on an AppRole whose policy **denies every read of the data path**, so a passing run proves the server itself would refuse a value read. Custom metadata carries names and version numbers only: no value, digest, or verifier of a value is ever written to the destination.

## Ownership and concurrency: CAS plus a marker protocol

Ownership is the ledger (deployment-adapter ADR). The destination additionally carries three custom-metadata keys per managed path: `managed_by_hikyo = <target id>`, `hikyo_version` (the version Hikyo last wrote), and `hikyo_pending_version` (the version an in-flight write will produce). A write is:

1. **Inspect** (value-blind): read metadata. An absent path is creatable. A path marked by this target whose `current_version` equals `hikyo_version` is clean. A path marked by another target, or marked by this target with a `current_version` Hikyo did not produce, is **external movement**.
2. **Mark** an existing path: patch the marker and `hikyo_pending_version = current + 1`. An absent path is not marked yet: metadata is only ever merge-patched, never created or replaced, so a writer who creates the path concurrently keeps its custom metadata.
3. **Write with check-and-set** on the observed version (`cas = 0` for create). Every value-writing effect durably changes its held ledger row to `dispatched` before mutating the provider, including previously owned names. A process crash therefore cannot retain ordinary overwrite authority. A known CAS refusal never overwrites the winning writer; failed pending-marker withdrawal retains uncertain custody and stops for review. A lost PUT response is not evidence that Hikyo produced the pending version.
4. **Finalize**: patch the marker, record `hikyo_version`, and clear the pending marker. For a create this pins version 1, so an external write landing between the create and the finalize reads as movement on the next sync.

**Operator-review policy amendment (2026-10-01, explicitly approved):** automatic writer inference from pending versions is withdrawn. An unknown PUT result, process death before its journal outcome, an unmarked dispatched create, or legacy pending metadata stops all automatic sync and prune attempts for the held target. Matching `current == pending` or `current == pending - 1` does not prove which writer landed. The foreign current value, version history, metadata and held custody remain unchanged while the operator reviews. An acknowledged PUT response may complete `owned` custody even if metadata finalization fails; this conservative incomplete-metadata case also stops for review rather than inventing a replay proof.

A fresh operator plan observes the positive current provider version using metadata only and stores it inside the scoped conflict artifact. Explicit adoption requires the exact current target generation, origin/destination, scope, surface and name; it can update only this target's held Vault row, never a foreign global-name claim. The adopted artifact's conflict generation must be the immediate predecessor of the verified current generation because adoption advances once. The next write requires that observed version still match and uses a new CAS; it never finalizes an old pending version as Hikyo's. Changed versions, consumed artifacts, legacy artifacts without a witness, and zero/nonpositive witnesses refuse. SQLite parses provenance timestamps and fails closed on malformed evidence; ties consume consent conservatively. A successful journal outcome consumes adoption. No wall-clock artifact lifetime is newly invented: freshness is current generation, destination and provider version. An unknown create still absent has no positive witness; the operator must explicitly retain/release that target's custody and recreate the target before another create. No user artifact or provider value is silently deleted to recover.

Removing ownership markers also invalidates ordinary overwrite/prune custody,
even when the provider version did not change. Missing markers are not adoption
consent; a new exact version-bound operator decision is required.

Every sync re-delivers every owned value (the seam's converge rule), which creates a new KV version per key per sync. Hikyo cannot skip unchanged values without reading or fingerprinting them, and it does neither; operators bound history with the mount's `max_versions`.

## Retain, prune, destroy

- **Retain** (`--keep-remote`) releases ledger custody and leaves every path untouched.
- **Prune** (the default on removal, and for names that leave the selection) soft-deletes only the inspected known-owned version with `POST {mount}/delete/{path}`. A concurrent newer version is never deleted. History and metadata survive, and `vault kv undelete` recovers the value until the mount's own retention removes it. Known external movement releases custody with a recorded conflict and warning without deleting the value. Unacknowledged/crashed write custody is different: it is retained and stops the whole target for operator review, including teardown. Explicit `--keep-remote` remains the non-destructive release decision.
- **Destroy** is not linked. Hikyo never destroys versions or deletes metadata; an operator who needs irreversible removal uses their own Vault tooling after retaining or pruning. A later ceremony-gated destroy would be a new operation in this ADR, not a flag.

## Authentication

The adapter credential is one write-only sealed credential (`PurposeAdapter`) accepted only through no-echo TTY, `--stdin`, or `--value-file`; it never crosses argv, logs, audit payloads, or reads. It is either a bare token or JSON: `{"method":"token","token":…}` or `{"method":"approle","role_id":…,"secret_id":…,"mount":"approle"}`, each with optional `ca_pem` (replaces system roots, hostname still verified) and `spki_sha256` (base64 SHA-256 of the leaf SubjectPublicKeyInfo, checked on the leaf only and in addition to chain verification). Unknown fields are refused so a typo cannot drop a pin. Trust material rides inside the credential because the adapter record has no provider-config column; rotating trust is a credential replacement.

AppRole logs in once per outbox attempt, renews at most twice within that attempt when the lease nears expiry, and revokes its token when the attempt ends. There is no background renewal. A refused login or renewal is a provider-auth failure. An operator's static token is never revoked; its expiry is recorded from `lookup-self`. JWT/OIDC login is out of v1: Hikyo has no issuer for its own workload identity, and a caller-supplied JWT is a static bearer with extra steps.

## Loop safety

One tree cannot be both an import source and a sync destination of the same project. Import runs client-side, so the refusal sits at import start: a live `hikyo import --from vault` lists the project's adapters and refuses by name when an active vault-kv target shares its host, namespace, and mount and either path prefix contains the other. There is no override. A caller who cannot read the adapter list cannot prove the import is loop-safe and is refused too.

## Failure classes

| Condition | Class |
|---|---|
| Sealed or standby (`503`) | retryable before a value PUT; an unknown dispatched PUT stops for operator review |
| Rate limited (`429`) | rate limit, honoring `Retry-After` |
| `401`/`403`, refused login or renewal | provider auth |
| Mount missing, remounted, not KV v2 | destination identity or configuration refusal |
| CAS mismatch | conflict (external movement) |
| Other `4xx` on a write | definitive failure; a reservation is released |
| Transport error, `5xx` during a value PUT | `unknown`, held `dispatched`; fresh version-bound operator review is required |

Partial progress is per name: each path has its own INTENT and OUTCOME, and completed names are skipped when a job resumes.

### Amendment: version-bound prune

Pruning uses the explicit-version soft-delete endpoint because deleting the latest
version can erase an external write racing the metadata inspection. The provider
still cannot destroy or undelete versions. Policies grant update on the delete
path instead of delete on the data path.
