# Handoff: #156 Transit (versioned cryptographic operations)

Issue: https://github.com/Hikyo-Org/Hikyo/issues/156 (blocked-by #75 and #146,
both merged). Normative contract: [`docs/adr/transit.md`](../adr/transit.md).
Declared amendments (pointer banners in this change):
[`mvp-boundary.md`](../adr/mvp-boundary.md) (Encryption-as-a-service promoted
In; HSM management and Vault replacement stay Out),
[`encryption-model.md`](../adr/encryption-model.md) (envelope kind `transit`,
material under the project DEK, HKDF's second job),
[`permission-model.md`](../adr/permission-model.md) (`crypto-use`,
`crypto-manage`), [`tenant-isolation.md`](../adr/tenant-isolation.md) (the
scheduler's transit doors). WebUI follow-up: #820.

## ADR gate

The MVP boundary listed "Encryption-as-a-service API" as an explicit non-goal
returnable only through a new map. The owner approved redrawing that map on
2026-09-26 and chose the issue handoff's recommendations. The
[oss-mechanics](../adr/oss-mechanics.md) cross-provider adversarial review of the
amendment is **owed** and not claimed here; the reopen/re-close ceremony on the
governance tickets is the human's.

## Decisions (taken; divergences from the issue handoff comment flagged)

1. **Environment-scoped keys** (ADR D1). The handoff proposed deriving keys per
   `(org, project, key)`. Divergence: keys belong to one environment, because a
   workload is granted only at environment depth, so a project-scoped key would
   be unreachable by the principals transit exists for.
2. **Material under the project DEK, not a new tier-3 purpose** (ADR D3).
   Divergence from "tier-3 keys with a new purpose `managed`": a tier-3 row per
   customer key would make `rotate-master-key` proportional to customer keys and
   put customer lifecycle inside the fenced hierarchy. Sealing each version's 32
   random bytes as a `project_field` under the project DEK gets `rotate-dek`,
   `reencrypt`, `rotate-master-key` and crypto-shred for free. The per-operation
   key is `HKDF-SHA256(material, LP("hikyo/transit-key/v1", alg, org, project,
   env, key, version))`.
3. **Closed algorithms**: `xchacha20-poly1305`, `ed25519`, `hmac-sha256`. The
   handoff's fourth, `datakey-256`, is an operation on encryption keys rather
   than an algorithm (sizes 128/256/512).
4. **Plaintext data keys need a per-key opt-in, not a reauth ceremony.**
   Divergence from "reveal-class ceremony": machines cannot perform a ceremony,
   envelope encryption is exactly the machine use case, and a plaintext data
   key is freshly generated rather than a stored Hikyo secret (decrypt already
   returns caller-supplied plaintext). `datakey-plaintext` must be named in the
   key's immutable allowed-operations set; the CLI routes it through the print
   triad.
5. **Atoms**: `crypto-use@environment` (workload and automation allowlists) and
   `crypto-manage@project` (human-only). No template seeds either. Inspection
   is `read@environment`, audited-none.
6. **Per-key caller entries** only narrow `crypto-use` (ADR D7). Stored in
   `transit_key_callers`; principal ids carry no FK (a stale entry matches no
   one; ids are never reused).
7. **Lifecycle** (ADR D6): `active`, `retired`, `disabled`, `pending-deletion`,
   `destroyed`, plus the orthogonal `compromised_through_version` mark.
   `cancel-deletion` lands in `disabled`. The purge is the hourly
   `transit_purge` scheduler job; automatic rotation is `transit_rotation`.
8. **Policy refusals are durable.** A refusal after the formula passed is
   recorded as a denied `transit.operation` through `CaptureAudit`, which
   survives the rollback the refusal causes (the reveal-gate precedent in
   `service/keys.go`). The payload carries only the closed cause.
9. **Rate limit**: named budget category `transit` (600/min per principal,
   6000/min per org). Under HA the service charges the #146 shared admission
   counters (`Coordination.BumpWindow`) instead of the per-node budget, failing
   closed when they cannot be read.
10. **Custody seam** (`internal/transit`): operation-level interface, `Software`
    provider, test-only mock in `internal/transit/transittest` (a boundary test
    forbids any non-test import of it). The production registry holds software
    only, so `custody: external` is refused at creation.
11. **`hikyo doctor`** is unchanged: it reads `RetentionHealth`, and widening
    that frozen response for transit was not worth it when three label-free
    gauges (`hikyo_transit_keys_live`, `..._rotation_due`,
    `..._pending_deletion`, guarded by `hikyo_transit_gauges_known`) already
    carry the health signal.

## What was built

- `internal/crypto/transit.go`: the transit envelope kind (8), derivation,
  AEAD/Ed25519/HMAC operations, the `hikyo:vN:<base64url>` wire codec (strict
  and canonical), data keys, bounds. Frozen known-answer vectors (derivation
  and MAC cross-checked against an independent HKDF/HMAC); fuzz targets
  `FuzzParseTransitValue` (found and fixed a non-canonical CR/LF acceptance,
  seed kept in `testdata/fuzz`) and `FuzzTransitDecrypt`.
- `internal/transit`: custody seam, software provider, conformance suite for
  both providers.
- Migration `00062_transit_keys` (both engines), `store.TransitRepo`
  (proof-bound, env chain from the proof), `store.TransitRuntime` (gauges),
  `buildcompat/development.json` regenerated against the pinned PostgreSQL 18
  image, upgrade-drill reversal extended.
  Rebased after #821 (`00060`, sealed webhook) and #824 (`00061`, SSH
  certificates) landed: transit is `00062` and API revision 7.
- `authz`: seven operations, 19 store ops, scheduler doors; `audit`: seven
  closed event types with a forbidden-content test; `domain`: the two atoms.
- `service.Transit`: management, the data-plane core `useKey`, `RotateDue`,
  `PurgeDue`; reencrypt walks `transit_key_versions`.
- API revision 7: 15 operations under
  `.../environments/{environment}/transit-keys`, handlers, 503 mapping for an
  unavailable custody provider, regenerated Go and TypeScript clients,
  no-proxy pins, parity rows (`issue: 820`).
- CLI `hikyo transit key ...` and `hikyo transit encrypt|decrypt|rewrap|datakey|
  sign|verify|hmac|hmac-verify`; inputs from stdin or files only.
- Docs: `docs/site/.../transit.mdx`, CLI reference row, WebUI access catalogue.

## Tests and gates

- `internal/isolation/transit_e2e_test.go`, both engines (sqlite and the CI
  PostgreSQL 18 image) and both custody providers: every operation, rotation,
  rewrap, the version window, trim, every lifecycle refusal, compromise
  recovery, restart, negative authorization (no atom, sibling environment,
  cross-org, workload with `crypto-use` only, caller entries), creation
  refusals, external custody down (no fallback, no provider work), concurrent
  rotation (contiguous versions), scheduled rotation and purge with an injected
  clock, and an audit grep proving no plaintext, context or output reached the
  trail. `runTransitLifecycle` gives every `transit.*` event a real emitter for
  the registry-closure check.
- Pins updated: operation formulas, scheduler site set and shared doors,
  metrics registry, help golden, no-proxy surface, workload wire surface.

## Known gaps

- No real external custody provider (by design: HSM management stays Out until
  a user with an HSM triggers it).
- No WebUI (#820).
- `service` package: `TestSAMLMetadata*` fail in this sandbox on a clean tree
  too (the HTTPS egress proxy intercepts the test's TLS server); unrelated.
